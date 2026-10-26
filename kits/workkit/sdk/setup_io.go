package sdk

import (
	"bytes"
	"context"
	"errors"
	r "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/readv1"
	s "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/setupv1"
	api "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/v1alpha1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/engine"
	check "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/setup"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
)

func SetupContractSchema(kind string) (map[string]any, error) {
	records := map[string]any{"observation": s.Observation{}, "report": s.Report{}, "policy": s.Policy{}, "verification": s.Verification{}, "mapping-context": s.MappingContext{}, "preview": s.Preview{}, "view-draft": s.ViewDraft{}, "view-inspection": s.ViewInspection{}, "binding-receipt": s.BindingReceipt{}}
	v, ok := records[kind]
	if !ok {
		return nil, errors.New("unknown setup contract")
	}
	schema := readSchemaType(reflect.TypeOf(v))
	schema["$schema"] = "https://json-schema.org/draft/2020-12/schema"
	schema["$id"] = "https://workkit.example/spec/setup/v1/" + kind + ".schema.json"
	props := schema["properties"].(map[string]any)
	if _, ok := props["version"]; ok {
		props["version"] = map[string]any{"const": s.Version}
	}
	for _, k := range []string{"authorizationGranted", "executionReady", "migrationActivated", "productionActivated"} {
		if _, ok := props[k]; ok {
			props[k] = map[string]any{"const": false}
		}
	}
	restrictSetupSchema(schema)
	return schema, nil
}
func SetupImplementationSHA256() string { return engine.ImplementationSHA256 }
func setupDocument(raw []byte, kind string, target any) error {
	format := "json"
	if !bytes.HasPrefix(bytes.TrimSpace(raw), []byte("{")) {
		format = "yaml"
	}
	d, e := Parse(raw, format)
	if e != nil || d.Kind() != kind {
		return failure("setup.document", "expected domain "+kind, 2, e)
	}
	if format == "json" {
		return DecodeRead(raw, target)
	}
	return d.Decode(target)
}
func LoadSetupObservation(ctx context.Context, root string) (s.Observation, map[string][]byte, error) {
	var o s.Observation
	raw, e := ReadLocal(ctx, filepath.Join(root, "bundle.json"))
	if e != nil {
		return o, nil, e
	}
	if e = DecodeRead(raw, &o); e != nil {
		return o, nil, e
	}
	files := map[string][]byte{}
	if e = check.Validate(o); e != nil {
		return o, nil, e
	}
	total := len(raw)
	seen := map[string]bool{}
	for _, a := range o.Assets {
		if seen[a.Path] || a.Path == "bundle.json" {
			return o, nil, errors.New("duplicate or circular setup asset")
		}
		seen[a.Path] = true
		p, e := ConfinedLocal(root, a.Path)
		if e != nil {
			return o, nil, e
		}
		b, e := ReadLocal(ctx, p)
		if e != nil || SHA256(b) != string(a.Sha256) {
			return o, nil, errors.New("setup asset integrity mismatch")
		}
		total += len(b)
		if total > engine.MaxTotalBytes {
			return o, nil, errors.New("setup aggregate bound")
		}
		files[a.Path] = b
	}
	if e = validateSetupProvenance(o, files); e != nil {
		return o, nil, e
	}
	return o, files, nil
}
func validateSetupProvenance(o s.Observation, files map[string][]byte) error {
	for _, a := range o.Assets {
		if !strings.HasPrefix(a.Path, "assets/") || filepath.ToSlash(filepath.Clean(a.Path)) != a.Path || strings.ContainsAny(a.Path, "\\:") {
			return errors.New("unsafe setup evidence path")
		}
		b, ok := files[a.Path]
		if !ok || SHA256(b) != string(a.Sha256) {
			return errors.New("listed observation asset mismatch")
		}
	}
	profileRaw, ok := files["assets/profile.json"]
	if !ok || SHA256(profileRaw) != string(o.ConnectionRef.Sha256) {
		return errors.New("profile evidence/hash missing")
	}
	var profile api.ConnectionProfile
	if setupDocument(profileRaw, "ConnectionProfile", &profile) != nil || profile.Metadata.Id != o.ConnectionRef.Id || profile.Metadata.Revision != o.ConnectionRef.Revision || profile.Spec.Provider != o.Scope.Provider || profile.Spec.Origin != o.Scope.Origin || profile.Spec.BasePath != o.Scope.BasePath || profile.Spec.Scope.TenantId != o.Scope.TenantID || profile.Spec.ApiVersion != o.Scope.APIVersion {
		return errors.New("setup profile scope mismatch")
	}
	hasContainer := false
	for _, id := range profile.Spec.Scope.ContainerIds {
		if id == o.Scope.ContainerID {
			hasContainer = true
		}
	}
	if !hasContainer {
		return errors.New("setup profile container mismatch")
	}
	configRaw, ok := files["assets/configuration.json"]
	if !ok || SHA256(configRaw) != o.ConfigurationSHA256 {
		return errors.New("configuration evidence mismatch")
	}
	var cfg r.Configuration
	if DecodeRead(configRaw, &cfg) != nil || cfg.Version != r.Version || cfg.ConnectionID != o.ConnectionRef.Id || cfg.Source.Version != "aok-tracker/v1" && cfg.Source.Version != "aok-tracker/v2" || SHA256(cfg.Profile) != string(o.ConnectionRef.Sha256) || providerName(cfg.Source.Provider) != o.Scope.Provider || cfg.Source.Origin != o.Scope.Origin || cfg.Source.NativeTenantID != o.Scope.TenantID || cfg.Source.ContainerID != o.Scope.ContainerID || cfg.Source.PrincipalPseudonym != o.Scope.Principal || cfg.Source.APIVersion != o.Scope.APIVersion {
		return errors.New("setup configuration scope mismatch")
	}
	if cfg.Source.Deployment != nil {
		if profile.Spec.Deployment == nil || profile.Spec.Deployment.ServerVersion != cfg.Source.Deployment.ServerVersion || string(profile.Spec.Deployment.TlsTrust.Sha256) != cfg.Source.Deployment.TLSTrustSHA256 {
			return errors.New("profile deployment pin mismatch")
		}
		if check.Hash(cfg.Source.Deployment) != o.Scope.DeploymentSHA256 || cfg.Source.Deployment.BasePath != o.Scope.BasePath {
			return errors.New("setup deployment mismatch")
		}
	} else if o.Scope.DeploymentSHA256 != "" || o.Scope.BasePath != "" {
		return errors.New("unexpected setup deployment")
	}
	for _, v := range o.Entities {
		a := v.Provenance.Asset
		b, ok := files[a.Path]
		if !ok || SHA256(b) != string(a.Sha256) {
			return errors.New("metadata evidence missing")
		}
		if _, e := check.Pointer(b, v.Provenance.Pointer); e != nil {
			return e
		}
		if v.Type != metadataType(v.NativeType) {
			return errors.New("metadata type conversion mismatch")
		}
		if v.ParentID != "" && v.ParentID != o.Scope.ContainerID && v.ParentID != o.Scope.TenantID {
			p, ok := v.Provenance.Fields["parentId"]
			got, e := check.Pointer(b, p)
			if !ok || e != nil || nativeString(got) != v.ParentID {
				return errors.New("metadata parent provenance mismatch")
			}
		}
		for field, want := range map[string]any{"options": v.Options, "required": v.Required, "fromStateId": v.FromStateID, "toStateId": v.ToStateID, "requiredFields": v.RequiredFields} {
			empty := field == "options" && len(v.Options) == 0 || field == "requiredFields" && len(v.RequiredFields) == 0 || field == "required" && !v.Required || field == "fromStateId" && v.FromStateID == "" || field == "toStateId" && v.ToStateID == ""
			if empty {
				if _, declared := v.Provenance.Fields[field]; !declared {
					continue
				}
			}
			p, ok := v.Provenance.Fields[field]
			got, e := check.Pointer(b, p)
			if !ok || e != nil || check.Hash(got) != check.Hash(want) {
				return errors.New("metadata semantic provenance mismatch: " + field)
			}
		}
		// Every asserted native identity/name/type is bound to an exact source leaf.
		for field, want := range map[string]string{"id": v.ID, "name": v.Name, "nativeType": v.NativeType} {
			if want == "" {
				continue
			}
			p, ok := v.Provenance.Fields[field]
			if !ok {
				return errors.New("field-level provenance missing")
			}
			got, e := check.Pointer(b, p)
			if e != nil || nativeString(got) != want {
				return errors.New("metadata field provenance mismatch")
			}
		}
	}
	for _, c := range o.Coverage {
		if !c.Complete {
			continue
		}
		b, ok := files[c.Evidence.Path]
		if !ok || SHA256(b) != string(c.Evidence.Sha256) {
			return errors.New("category completeness evidence missing")
		}
		v, e := check.Pointer(b, c.CompletePointer)
		if e != nil || v != true {
			return errors.New("category completeness evidence invalid")
		}
	}
	return nil
}
func WriteSetupObservation(ctx context.Context, o s.Observation, files map[string][]byte, out string) error {
	if e := check.Validate(o); e != nil {
		return e
	}
	if e := validateSetupProvenance(o, files); e != nil {
		return e
	}
	copyFiles := map[string][]byte{}
	o.Assets = []api.Asset{}
	names := []string{}
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		copyFiles[n] = append([]byte(nil), files[n]...)
		o.Assets = append(o.Assets, api.Asset{Path: n, Sha256: api.Sha(SHA256(files[n]))})
	}
	check.Sort(&o)
	copyFiles["bundle.json"] = engine.Encode(o)
	w, e := engine.ReserveBundle(ctx, out)
	if e != nil {
		return e
	}
	if e = w.Write(ctx, copyFiles); e != nil {
		return e
	}
	return w.Sync(ctx)
}
func setupReportFiles(v any, inputs map[string][]byte) map[string][]byte {
	files := map[string][]byte{}
	assets := []api.Asset{}
	names := []string{}
	for n := range inputs {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		p := "assets/" + n
		files[p] = inputs[n]
		assets = append(assets, api.Asset{Path: p, Sha256: api.Sha(SHA256(inputs[n]))})
	}
	files["report.json"] = engine.Encode(v)
	assets = append(assets, api.Asset{Path: "report.json", Sha256: api.Sha(SHA256(files["report.json"]))})
	files["bundle.json"] = engine.Encode(map[string]any{"version": s.Version, "kind": "SetupReportBundle", "assets": assets})
	return files
}
func writeSetupReport(ctx context.Context, out string, v any, inputs map[string][]byte) error {
	w, e := engine.ReserveBundle(ctx, out)
	if e != nil {
		return e
	}
	if e = w.Write(ctx, setupReportFiles(v, inputs)); e != nil {
		return e
	}
	return w.Sync(ctx)
}

func setupOutput(out string, roots ...string) error {
	a, e := filepath.Abs(out)
	if e != nil || out == "" {
		return errors.New("new output required")
	}
	for _, r := range roots {
		if r == "" {
			continue
		}
		base, e := filepath.Abs(r)
		if e != nil {
			return e
		}
		rel, e := filepath.Rel(base, a)
		if e == nil && (rel == "." || rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
			return errors.New("setup output overlaps immutable input or resources root")
		}
	}
	return nil
}

func loadSetupReportBundle(ctx context.Context, root string) (map[string][]byte, error) {
	raw, e := ReadLocal(ctx, filepath.Join(root, "bundle.json"))
	if e != nil {
		return nil, e
	}
	var seal struct {
		Version string      `json:"version"`
		Kind    string      `json:"kind"`
		Assets  []api.Asset `json:"assets"`
	}
	if DecodeRead(raw, &seal) != nil || seal.Version != s.Version || seal.Kind != "SetupReportBundle" || len(seal.Assets) < 1 || len(seal.Assets) > 1024 {
		return nil, errors.New("invalid setup report bundle")
	}
	files := map[string][]byte{}
	total := len(raw)
	for _, a := range seal.Assets {
		if _, ok := files[a.Path]; ok || a.Path == "bundle.json" || a.Path != "report.json" && !strings.HasPrefix(a.Path, "assets/") {
			return nil, errors.New("invalid report asset")
		}
		p, e := ConfinedLocal(root, a.Path)
		if e != nil {
			return nil, e
		}
		b, e := ReadLocal(ctx, p)
		if e != nil || SHA256(b) != string(a.Sha256) {
			return nil, errors.New("report asset integrity mismatch")
		}
		total += len(b)
		if total > engine.MaxTotalBytes {
			return nil, errors.New("report aggregate bounds")
		}
		files[a.Path] = b
	}
	again, e := ReadLocal(ctx, filepath.Join(root, "bundle.json"))
	if e != nil || !bytes.Equal(again, raw) {
		return nil, errors.New("report changed during validation")
	}
	return files, nil
}

func restrictSetupSchema(schema map[string]any) {
	if properties, ok := schema["properties"].(map[string]any); ok {
		for _, name := range []string{"authorizationGranted", "executionReady", "migrationActivated", "productionActivated"} {
			if _, ok := properties[name]; ok {
				properties[name] = map[string]any{"const": false}
			}
		}
		if _, ok := properties["version"]; ok {
			properties["version"] = map[string]any{"const": s.Version}
		}
	}
	for _, v := range schema {
		switch x := v.(type) {
		case map[string]any:
			restrictSetupSchema(x)
		case []any:
			for _, v := range x {
				if m, ok := v.(map[string]any); ok {
					restrictSetupSchema(m)
				}
			}
		}
	}
}
