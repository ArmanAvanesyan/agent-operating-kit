package sdk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	n "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/nativev1"
	o "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/operationsv1"
	api "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/v1alpha1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/engine"
	"path/filepath"
	"reflect"
	"strings"
)

// PlanNativeProgress delegates the final draft to the existing P5 planner. Native
// completion is only a source value; it supplies neither acceptance nor authority.
func PlanNativeProgress(ctx context.Context, bundle, linkPath string, q PlanItemRequest) (o.Draft, error) {
	report, e := InspectNative(ctx, bundle, q.AsOf)
	if e != nil {
		return o.Draft{}, e
	}
	if !report.Complete || report.Status != "ok" || report.Kind != "NativeObservation" && report.Kind != "NativeReconciliation" {
		return o.Draft{}, failure("native.progress-incomplete", "fresh complete observation required", 3, nil)
	}
	extras := map[string][]byte{}
	raw, e := ReadLocal(ctx, filepath.Join(bundle, "bundle.json"))
	if e != nil {
		return o.Draft{}, e
	}
	extras["assets/native-progress/bundle.json"] = raw
	var saved n.Report
	if e = DecodeRead(raw, &saved); e != nil {
		return o.Draft{}, e
	}
	for _, a := range saved.Assets {
		p, e := ConfinedLocal(bundle, a.Path)
		if e != nil {
			return o.Draft{}, e
		}
		b, e := ReadLocal(ctx, p)
		if e != nil || SHA256(b) != string(a.Sha256) {
			return o.Draft{}, errors.New("native progress evidence changed")
		}
		extras["assets/native-progress/"+a.Path] = b
	}
	link, e := ReadLocal(ctx, linkPath)
	if e != nil {
		return o.Draft{}, e
	}
	extras["assets/native-progress-link.json"] = link
	desired, e := ReadLocal(ctx, q.Desired)
	if e != nil {
		return o.Draft{}, e
	}
	var d o.Desired
	if e = DecodeRead(desired, &d); e != nil {
		return o.Draft{}, e
	}
	if d.Operation != "comment" && d.Operation != "update" {
		return o.Draft{}, NativeMutationUnavailable()
	}
	q.Operation = d.Operation
	check := cloneBytes(extras)
	check["assets/desired.json"] = desired
	for k, p := range map[string]string{"binding": q.Binding, "mapping": q.Mapping} {
		raw, e := ReadLocal(ctx, p)
		if e != nil {
			return o.Draft{}, e
		}
		check["assets/"+k+".json"] = raw
	}
	if e := validateNativeProgressAssets(check, q.AsOf); e != nil {
		return o.Draft{}, e
	}
	capture, e := LoadReadBundle(ctx, q.Snapshot)
	if e != nil {
		return o.Draft{}, e
	}
	found := false
	var parsedLink n.ProgressLink
	if DecodeRead(link, &parsedLink) != nil {
		return o.Draft{}, errors.New("invalid native link")
	}
	for _, record := range capture.records {
		identity := api.Identity{Provider: record.Identity.Provider, Origin: record.Identity.Origin, BasePath: record.Identity.BasePath, TenantId: record.Identity.TenantID, ContainerId: record.Identity.ContainerID, ItemKind: record.Identity.ItemKind, NativeId: record.Identity.NativeID}
		found = found || reflect.DeepEqual(identity, parsedLink.Target)
	}
	if !found {
		return o.Draft{}, errors.New("native progress target not in exact pinned capture")
	}
	return planItemOperation(ctx, q, extras)
}
func validateNativeProgressAssets(files map[string][]byte, asof string, targets ...api.Identity) error {
	linkRaw, has := files["assets/native-progress-link.json"]
	if !has {
		for key := range files {
			if strings.HasPrefix(key, "assets/native-progress/") {
				return errors.New("orphan native progress evidence")
			}
		}
		return nil
	}
	var link n.ProgressLink
	var report n.Report
	var d o.Desired
	var binding api.WorkBinding
	if DecodeRead(linkRaw, &link) != nil || DecodeRead(files["assets/native-progress/bundle.json"], &report) != nil || DecodeRead(files["assets/desired.json"], &d) != nil || setupDocument(files["assets/binding.json"], "WorkBinding", &binding) != nil {
		return errors.New("invalid native progress link")
	}
	if link.Version != n.Version || link.Kind != "NativeProgressLink" || link.ObservationSHA256 != SHA256(files["assets/native-progress/bundle.json"]) || link.ScopeSHA256 != SHA256(engine.Encode(report.Scope)) || link.BindingSHA256 != SHA256(files["assets/binding.json"]) || link.MappingSHA256 != SHA256(files["assets/mapping.json"]) || link.Target.NativeId != d.ItemID || link.Target.NativeId == "" || len(link.Rules) == 0 || len(link.Rules) > 128 {
		return errors.New("native progress exact work/binding mismatch")
	}
	for _, target := range targets {
		if !reflect.DeepEqual(target, link.Target) {
			return errors.New("native progress crosses full provider work identity")
		}
	}
	cfg := binding.Spec.Scope
	inScope := false
	for _, id := range cfg.ContainerIds {
		inScope = inScope || id == link.Target.ContainerId
	}
	if cfg.TenantId != link.Target.TenantId || !inScope {
		return errors.New("native progress crosses work scope")
	}
	// Rebuild the original report rather than trusting serialized entries or flags.
	nativeFiles := map[string][]byte{}
	for _, a := range report.Assets {
		b, ok := files["assets/native-progress/"+a.Path]
		if !ok || SHA256(b) != string(a.Sha256) {
			return errors.New("native progress asset mismatch")
		}
		nativeFiles[a.Path] = b
	}
	expected, e := nativeBuild(nativeFiles, report.Kind, report.AsOf)
	if e != nil {
		return e
	}
	expected.Assets = report.Assets
	if !bytes.Equal(engine.Encode(expected), engine.Encode(report)) {
		return errors.New("forged native progress report")
	}
	current, e := nativeBuild(nativeFiles, report.Kind, asof)
	if e != nil || current.Status != "ok" || !current.Complete {
		return errors.New("stale or partial progress evidence")
	}
	fields := map[string]json.RawMessage{}
	for _, rule := range link.Rules {
		if rule.EntryIndex < 0 || rule.EntryIndex >= len(report.Entries) || rule.Field == "" || rule.ExpectedState == "" || rule.ExpectedState == "unknown" || rule.ExpectedState != report.Entries[rule.EntryIndex].State || fields[rule.Field] != nil {
			return errors.New("unresolved/duplicate native progress mapping")
		}
		fields[rule.Field] = rule.Value
	}
	if !reflect.DeepEqual(fields, d.Fields) {
		return errors.New("desired values differ from explicit native progress mapping")
	}
	return nil
}
