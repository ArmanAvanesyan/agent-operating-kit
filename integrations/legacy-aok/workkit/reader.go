// Package workkit adapts existing AOK validators to WorkKit's public import
// port. This integration has no provider transport, credential or sender path.
package workkit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/tracker"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/sdk"
)

type Reader struct{}

func encode(v any) []byte { b, _ := json.Marshal(v); return b }
func envelope(kind, id string, spec any, provenance any) []byte {
	return encode(map[string]any{"apiVersion": "workkit/v1alpha1", "kind": kind, "metadata": map[string]any{"id": id, "revision": 1}, "spec": spec, "extensions": map[string]any{"aok/original": provenance}})
}
func (c *Reader) Inspect(ctx context.Context, r sdk.SourceRequest) (sdk.LegacyCapture, error) {
	if e := ctx.Err(); e != nil {
		return sdk.LegacyCapture{}, e
	}
	switch r.Format {
	case tracker.Version, tracker.Version2:
		return trackerSource(ctx, r)
	case "aok/v1alpha1", "aok/v1alpha2":
		return workflowSource(ctx, r)
	default:
		return sdk.LegacyCapture{}, &sdk.Error{Code: "migration.unsupported", Message: "unsupported legacy source format", ExitCode: 3}
	}
}
func provider(p string) string {
	if p == "jira" {
		return "jira-cloud"
	}
	return p
}
func trackerSource(ctx context.Context, r sdk.SourceRequest) (sdk.LegacyCapture, error) {
	result := sdk.LegacyCapture{Files: map[string][]byte{}, Projections: [][]byte{}, Losses: []string{}, UnresolvedOperationIDs: []string{}}
	path := r.Source
	if st, e := os.Stat(path); e == nil && st.IsDir() {
		path = filepath.Join(path, "manifest.json")
	}
	// Preflight paths, bytes and cancellation through the kit's bounded local IO.
	raw, e := sdk.ReadLocal(ctx, path)
	if e != nil {
		return result, e
	}
	var manifest tracker.Manifest
	if e = tracker.Decode(raw, "manifest", &manifest); e != nil {
		return result, e
	}
	if manifest.Version != r.Format {
		return result, errors.New("source format/version mismatch")
	}
	root := filepath.Dir(path)
	result.Files["manifest.json"] = raw
	total := len(raw)
	if len(manifest.Pages) > 1000 {
		return result, errors.New("source page limit")
	}
	for _, page := range manifest.Pages {
		p, e := sdk.ConfinedLocal(root, page.Asset.Path)
		if e != nil {
			return result, e
		}
		b, e := sdk.ReadLocal(ctx, p)
		if e != nil {
			return result, e
		}
		total += len(b)
		if total > 64<<20 {
			return result, errors.New("source aggregate limit")
		}
		if sdk.SHA256(b) != page.Asset.SHA256 {
			return result, errors.New("source evidence hash mismatch")
		}
		result.Files[page.Asset.Path] = b
	}
	// Reconstruct using existing source-specific adapters, with no live transport.
	capture, e := tracker.Import(path)
	if e != nil {
		return result, e
	}
	if e = ctx.Err(); e != nil {
		return result, e
	}
	// Reject file drift across the existing parser's read rather than capturing a
	// mixture of the original preflight bytes and reconstructed snapshot.
	for name, b := range result.Files {
		p, e := sdk.ConfinedLocal(root, name)
		if e != nil {
			return result, e
		}
		again, e := sdk.ReadLocal(ctx, p)
		if e != nil || sdk.SHA256(again) != sdk.SHA256(b) {
			return result, errors.New("source changed during import")
		}
	}
	s := capture.Snapshot.Source
	at := capture.Snapshot.ObservedAt
	if at != "" {
		result.OriginalObservedAt = &at
	} else {
		result.Losses = append(result.Losses, "source has no original observation time; not replaced by inspection time")
	}
	prov := map[string]any{"format": r.Format, "sourceSHA256": sdk.SHA256(raw), "snapshotDigest": capture.Snapshot.Digest, "digestAlgorithm": "original AOK tracker implementation; retained without reinterpretation", "principalPseudonym": s.PrincipalPseudonym}
	result.Provenance = encode(prov)
	connection := map[string]any{"provider": provider(s.Provider), "origin": s.Origin, "basePath": "", "transport": "rest", "authRef": "unresolved/aok-import", "scope": map[string]any{"tenantId": s.NativeTenantID, "containerIds": []string{s.ContainerID}}, "apiVersion": s.APIVersion}
	result.Losses = append(result.Losses, "connection credential reference is unresolved and grants no access")
	if s.Provider == "github" {
		connection["transport"] = "gh"
	}
	if s.Provider == "linear" {
		connection["transport"] = "graphql"
	}
	if s.Deployment != nil {
		connection["basePath"] = s.Deployment.BasePath
		// A trust digest is not the original CA bytes. Never fabricate that asset.
		result.Losses = append(result.Losses, "Data Center connection projection requires original TLS trust asset; deployment pins retained in source")
	} else {
		result.Projections = append(result.Projections, envelope("ConnectionProfile", "aok-connection", connection, prov))
	}
	for _, item := range capture.Snapshot.Items {
		id := "item-" + sdk.SHA256(encode(item.Identity))[:24]
		identity := map[string]any{"provider": provider(s.Provider), "origin": s.Origin, "basePath": connection["basePath"], "tenantId": item.NativeTenantID, "containerId": item.Identity.ContainerID, "itemKind": item.Identity.ItemKind, "nativeId": item.Identity.RemoteID}
		kind := item.RevisionKind
		if kind == "azure-work-item-rev" {
			kind = "native-revision"
		}
		if kind == "observation-fingerprint" {
			kind = "fingerprint"
		}
		if kind == "" {
			kind = "none"
		}
		diagnostics := []any{}
		for _, field := range item.LossyFields {
			diagnostics = append(diagnostics, map[string]any{"code": "migration.loss-preserved", "field": field, "message": "original rich/provider content retained", "severity": "warning"})
		}
		if len(item.Relations) > 0 || len(item.ProjectFields) > 0 || len(item.Comments) > 0 || len(item.Transitions) > 0 {
			result.Losses = append(result.Losses, "relationships, project fields, comments or transitions retained in original assets; not flattened")
		}
		spec := map[string]any{"identity": identity, "revision": map[string]any{"kind": kind, "value": item.Revision, "observedAt": at}, "fields": item.Fields, "memberships": []any{}, "relations": []any{}, "complete": item.Complete && len(item.Relations) == 0 && len(item.ProjectFields) == 0 && len(item.Comments) == 0 && len(item.Transitions) == 0, "diagnostics": diagnostics}
		result.Projections = append(result.Projections, envelope("WorkItem", id, spec, map[string]any{"item": item, "source": prov}))
	}
	if !capture.Snapshot.Complete {
		result.Losses = append(result.Losses, "partial source coverage remains partial; checkpoint preserved")
	}
	return result, nil
}
func workflowSource(ctx context.Context, r sdk.SourceRequest) (sdk.LegacyCapture, error) {
	out := sdk.LegacyCapture{Files: map[string][]byte{}, Projections: [][]byte{}, Losses: []string{}, UnresolvedOperationIDs: []string{}}
	// P1 reads a single explicit record and only its typed evidence assets. It
	// does not recursively discover runtime, authority or credential records.
	raw, e := sdk.ReadLocal(ctx, r.Source)
	if e != nil {
		return out, e
	}
	resource, _, e := contracts.Parse(raw)
	if e != nil {
		return out, e
	}
	typed := resource.(contracts.Resource)
	if typed.APIVersion != r.Format {
		return out, errors.New("workflow version mismatch")
	}
	allowed := map[string]bool{"WorkItemBinding": true, "HostTaskBinding": true, "SyncOperation": true, "SyncReceipt": true}
	if !allowed[typed.Kind] {
		return out, &sdk.Error{Code: "migration.unsupported", Message: "workflow kind is outside P1 import coverage", ExitCode: 3}
	}
	root := filepath.Dir(r.Source)
	out.Files["record.json"] = raw
	var values map[string]json.RawMessage
	_ = json.Unmarshal(raw, &values)
	refs := []contracts.AssetRef{}
	switch typed.Kind {
	case "WorkItemBinding":
		var b contracts.WorkItemBinding
		if e = json.Unmarshal(raw, &b); e != nil {
			return out, e
		}
		refs = append(refs, b.Packet)
		out.Losses = append(out.Losses, "WorkBinding projection requires exact connection origin, mapping and observation time; original binding retained")
	case "HostTaskBinding":
		out.Losses = append(out.Losses, "NativeTaskBinding projection requires profile reference and original observation time; original session/assignment identity retained")
	case "SyncOperation":
		var op contracts.SyncOperation
		if e = json.Unmarshal(raw, &op); e != nil {
			return out, e
		}
		out.UnresolvedOperationIDs = append(out.UnresolvedOperationIDs, op.OperationID)
		if op.Change.Comment != nil {
			refs = append(refs, *op.Change.Comment)
		}
		out.Losses = append(out.Losses, "OperationPlan projection requires exact origin, revision kind and current policy/capability references; no send authority imported")
	case "SyncReceipt":
		var receipt contracts.SyncReceipt
		if e = json.Unmarshal(raw, &receipt); e != nil {
			return out, e
		}
		at := receipt.ObservedAt
		out.OriginalObservedAt = &at
		for _, ev := range append(receipt.DispatchEvidence, receipt.ReadbackEvidence...) {
			refs = append(refs, ev.Asset)
		}
		out.UnresolvedOperationIDs = append(out.UnresolvedOperationIDs, receipt.OperationID)
		// Preserve the original digest as provenance: it is not a WorkKit plan hash.
		out.Losses = append(out.Losses, "OperationReceipt projection lacks a WorkKit plan identity; original disposition and uncertain effects retained")
	}
	total := len(raw)
	for _, asset := range refs {
		p, e := sdk.ConfinedLocal(root, asset.Path)
		if e != nil {
			return out, e
		}
		b, e := sdk.ReadLocal(ctx, p)
		if e != nil {
			return out, e
		}
		total += len(b)
		if total > 64<<20 || sdk.SHA256(b) != asset.SHA256 {
			return out, errors.New("workflow evidence bounds/hash mismatch")
		}
		if previous, ok := out.Files[asset.Path]; ok && sdk.SHA256(previous) != sdk.SHA256(b) {
			return out, errors.New("source asset collision")
		}
		out.Files[asset.Path] = b
	}
	provenance := map[string]any{"format": r.Format, "kind": typed.Kind, "originalMetadata": typed.Metadata, "sourceSHA256": sdk.SHA256(raw), "conversion": "preservation-only; missing semantics explicitly reported"}
	for _, field := range []string{"operationDigest", "expectedRevision", "observedSourceRevision", "assignmentSha256", "observedAt", "disposition"} {
		if raw, ok := values[field]; ok {
			provenance[field] = raw
		}
	}
	out.Provenance = encode(provenance)
	// Keep deterministic ordering for preserved source references.
	sort.Strings(out.Losses)
	again, e := sdk.ReadLocal(ctx, r.Source)
	if e != nil || sdk.SHA256(again) != sdk.SHA256(raw) {
		return out, fmt.Errorf("workflow record changed")
	}
	return out, nil
}

var _ sdk.LegacyReader = (*Reader)(nil)

// No execution, sender or credential implementation is imported here.
