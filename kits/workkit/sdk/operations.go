package sdk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	o "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/operationsv1"
	r "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/readv1"
	s "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/setupv1"
	y "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/syncv1"
	api "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/v1alpha1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/engine"
	check "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/setup"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"
)

func OperationContractSchema(kind string) (map[string]any, error) {
	records := map[string]any{"draft": o.Draft{}, "desired": o.Desired{}, "capability": o.Capability{}, "policy": o.Policy{}, "enrollment": o.Enrollment{}, "observation": o.Observation{}, "store": o.Store{}, "qualification": o.Qualification{}, "sync-configuration": y.Configuration{}, "sync-state": y.State{}, "sync-plan": y.Plan{}, "sync-run": y.Run{}, "sync-invocation": y.Invocation{}, "setup-handoff": SetupHandoff{}}
	v, ok := records[kind]
	if !ok {
		return nil, errors.New("unknown operation contract")
	}
	schema := readSchemaType(reflect.TypeOf(v))
	schema["$schema"] = "https://json-schema.org/draft/2020-12/schema"
	schema["$id"] = "https://workkit.example/spec/operations/v1/" + kind + ".schema.json"
	if strings.HasPrefix(kind, "sync-") {
		schema["$id"] = "https://workkit.example/spec/sync/v1/" + kind + ".schema.json"
	}
	version := o.Version
	if strings.HasPrefix(kind, "sync-") {
		version = y.Version
	}
	schema["properties"].(map[string]any)["version"] = map[string]any{"const": version}
	restrictOperationFlags(schema)
	return schema, nil
}
func restrictOperationFlags(m map[string]any) {
	if p, ok := m["properties"].(map[string]any); ok {
		for _, n := range []string{"authorizationGranted", "executionReady", "migrationActivated", "productionActivated"} {
			if _, ok := p[n]; ok {
				p[n] = map[string]any{"const": false}
			}
		}
	}
	for _, v := range m {
		if n, ok := v.(map[string]any); ok {
			restrictOperationFlags(n)
		}
	}
}

type PreparedOperation struct {
	draft o.Draft
	raw   []byte
	files map[string][]byte
}

func (p PreparedOperation) Draft() o.Draft { return cloneRead(p.draft) }
func (p PreparedOperation) Digest() string { return SHA256(p.raw) }
func (p PreparedOperation) Bytes() []byte  { return append([]byte(nil), p.raw...) }
func (p PreparedOperation) Assets() map[string][]byte {
	m := map[string][]byte{}
	for n, b := range p.files {
		m[n] = append([]byte(nil), b...)
	}
	return m
}
func operationBlock(d *o.Draft, code, field, message string) {
	d.Status = "blocked"
	d.Diagnostics = append(d.Diagnostics, api.Diagnostic{Code: code, Field: field, Message: message, Severity: "error"})
}
func validateDraft(d o.Draft) error {
	if d.Version != o.Version || d.Kind != "OperationDraft" || d.ID == "" || d.LogicalKey == "" || d.AuthorizationGranted || d.ExecutionReady || d.MigrationActivated || d.ProductionActivated || len(d.Assets) > 1023 || len(d.Changes) > 128 || len(d.DependsOn) > 128 {
		return errors.New("invalid operation envelope")
	}
	if d.Status != "draft" && d.Status != "blocked" {
		return errors.New("draft status required")
	}
	if _, e := time.Parse(time.RFC3339Nano, d.ObservedAt); e != nil {
		return e
	}
	if d.ID != operationID(d.LogicalKey, d.Target, d.Operation) {
		return errors.New("operation logical ID mismatch")
	}
	if d.Plan.Kind != "OperationPlan" || d.Plan.Metadata.Id != d.ID || d.Plan.Spec.OperationId != api.Id(d.ID) || d.Plan.Spec.Target == nil || check.Hash(*d.Plan.Spec.Target) != check.Hash(api.Identity{Provider: d.Target.Provider, Origin: d.Target.Origin, BasePath: d.Target.BasePath, TenantId: d.Target.TenantID, ContainerId: d.Target.ContainerID, ItemKind: d.Target.ItemKind, NativeId: d.Target.NativeID}) {
		return errors.New("domain plan identity mismatch")
	}
	fields := map[string]json.RawMessage{}
	for _, c := range d.Plan.Spec.Changes {
		if _, ok := fields[c.FieldId]; ok {
			return errors.New("duplicate domain change")
		}
		fields[c.FieldId] = c.Value
	}
	if check.Hash(fields) != check.Hash(d.Changes) {
		return errors.New("domain changes mismatch")
	}
	return nil
}
func operationID(key string, target r.Identity, operation string) string {
	return "op-" + check.Hash(struct {
		Key       string
		Target    r.Identity
		Operation string
	}{key, target, operation})[:40]
}
func WriteOperationDraft(ctx context.Context, d o.Draft, files map[string][]byte, out string) error {
	files = cloneBytes(files)
	d.Assets = []api.Asset{}
	names := []string{}
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		d.Assets = append(d.Assets, api.Asset{Path: n, Sha256: api.Sha(SHA256(files[n]))})
	}
	if e := validateDraft(d); e != nil {
		return e
	}
	files["bundle.json"] = engine.Encode(d)
	w, e := engine.ReserveBundle(ctx, out)
	if e != nil {
		return e
	}
	if e = w.Write(ctx, files); e != nil {
		return e
	}
	return w.Sync(ctx)
}
func cloneBytes(files map[string][]byte) map[string][]byte {
	m := map[string][]byte{}
	for n, b := range files {
		m[n] = append([]byte(nil), b...)
	}
	return m
}
func PrepareOperation(ctx context.Context, root string) (PreparedOperation, error) {
	raw, e := ReadLocal(ctx, filepath.Join(root, "bundle.json"))
	if e != nil {
		return PreparedOperation{}, e
	}
	var d o.Draft
	if e = DecodeRead(raw, &d); e != nil {
		return PreparedOperation{}, e
	}
	if e = validateDraft(d); e != nil {
		return PreparedOperation{}, e
	}
	files := map[string][]byte{}
	total := len(raw)
	for _, a := range d.Assets {
		if !strings.HasPrefix(a.Path, "assets/") {
			return PreparedOperation{}, errors.New("unsafe draft asset")
		}
		if _, ok := files[a.Path]; ok {
			return PreparedOperation{}, errors.New("duplicate draft asset")
		}
		path, e := ConfinedLocal(root, a.Path)
		if e != nil {
			return PreparedOperation{}, e
		}
		b, e := ReadLocal(ctx, path)
		if e != nil || SHA256(b) != string(a.Sha256) {
			return PreparedOperation{}, errors.New("operation asset hash mismatch")
		}
		total += len(b)
		if total > engine.MaxTotalBytes {
			return PreparedOperation{}, errors.New("operation aggregate bound")
		}
		files[a.Path] = b
	}
	for key, path := range map[string]string{"desired": "assets/desired.json", "mapping": "assets/mapping.json", "binding": "assets/binding.json", "capability": "assets/capability.json", "policy": "assets/policy.json"} {
		if d.Inputs[key] != "" && SHA256(files[path]) != d.Inputs[key] {
			return PreparedOperation{}, errors.New("operation input mismatch")
		}
	}
	if d.Inputs["snapshot"] == "" {
		return PreparedOperation{}, errors.New("complete kind-specific pinned inputs required")
	}
	if d.Inputs["snapshot"] != "" {
		b, e := LoadReadBundle(ctx, filepath.Join(root, "assets/capture"))
		if e != nil || b.Digest() != d.Inputs["snapshot"] {
			return PreparedOperation{}, errors.New("pinned snapshot mismatch")
		}
		var wanted o.Desired
		var cap o.Capability
		var policy o.Policy
		var binding api.WorkBinding
		var mapping api.Mapping
		if DecodeRead(files["assets/desired.json"], &wanted) != nil || DecodeRead(files["assets/capability.json"], &cap) != nil || DecodeRead(files["assets/policy.json"], &policy) != nil || setupDocument(files["assets/binding.json"], "WorkBinding", &binding) != nil || setupDocument(files["assets/mapping.json"], "Mapping", &mapping) != nil {
			return PreparedOperation{}, errors.New("invalid pinned planning input")
		}
		meta, _, e := LoadSetupObservation(ctx, filepath.Join(root, "assets/metadata"))
		if e != nil || check.Hash(meta) != d.Inputs["observation"] {
			return PreparedOperation{}, errors.New("pinned metadata mismatch")
		}
		rebuilt, e := evaluateItemOperation(b, meta, wanted, mapping, binding, cap, policy, d.Inputs, d.Plan.Spec.PolicyRef, d.Plan.Spec.CapabilityRef)
		if e != nil {
			return PreparedOperation{}, e
		}
		rebuilt.Assets = d.Assets
		if check.Hash(rebuilt) != check.Hash(d) {
			return PreparedOperation{}, errors.New("operation differs from deterministic planning inputs")
		}
	}
	return PreparedOperation{d, append([]byte(nil), raw...), files}, nil
}

type PlanItemRequest struct{ Snapshot, Observation, Desired, Mapping, Binding, BindingRoot, Capability, Policy, AsOf, Out, Operation string }

func PlanItemOperation(ctx context.Context, q PlanItemRequest) (o.Draft, error) {
	if e := setupOutput(q.Out, q.Snapshot, q.Observation); e != nil {
		return o.Draft{}, e
	}
	capture, e := LoadReadBundle(ctx, q.Snapshot)
	if e != nil {
		return o.Draft{}, e
	}
	meta, native, e := LoadSetupObservation(ctx, q.Observation)
	if e != nil {
		return o.Draft{}, e
	}
	files := map[string][]byte{}
	for k, p := range map[string]string{"desired": q.Desired, "mapping": q.Mapping, "binding": q.Binding, "capability": q.Capability, "policy": q.Policy} {
		b, e := ReadLocal(ctx, p)
		if e != nil {
			return o.Draft{}, e
		}
		files["assets/"+k+".json"] = b
	}
	var desired o.Desired
	var cap o.Capability
	var policy o.Policy
	var binding api.WorkBinding
	var mapping api.Mapping
	for _, input := range []struct {
		name, kind string
		target     any
	}{{"desired", "", &desired}, {"capability", "", &cap}, {"policy", "", &policy}, {"binding", "WorkBinding", &binding}, {"mapping", "Mapping", &mapping}} {
		raw := files["assets/"+input.name+".json"]
		var err error
		if input.kind == "" {
			err = DecodeRead(raw, input.target)
		} else {
			err = setupDocument(raw, input.kind, input.target)
		}
		if err != nil {
			return o.Draft{}, fmt.Errorf("invalid operation planning input %s: %w", input.name, err)
		}
	}

	if desired.Operation != q.Operation {
		return o.Draft{}, errors.New("command operation mismatch")
	}
	inputs := map[string]string{"snapshot": capture.Digest(), "observation": check.Hash(meta), "configuration": capture.manifest.ConfigurationSHA256, "asOf": q.AsOf}
	for _, k := range []string{"desired", "mapping", "binding", "capability", "policy"} {
		inputs[k] = SHA256(files["assets/"+k+".json"])
	}
	policyRef := api.Ref{Kind: "OperatorPolicy", Id: "policy-" + inputs["policy"][:24], Revision: 1, Sha256: api.Sha(inputs["policy"])}
	capRef := api.Ref{Kind: "CapabilityReport", Id: "capability-" + inputs["capability"][:24], Revision: 1, Sha256: api.Sha(inputs["capability"])}
	d, e := evaluateItemOperation(capture, meta, desired, mapping, binding, cap, policy, inputs, policyRef, capRef)
	if e != nil {
		return d, e
	}
	files["assets/metadata/bundle.json"] = engine.Encode(meta)
	for n, b := range native {
		files["assets/metadata/"+n] = b
	}
	files["assets/capture/bundle.json"] = capture.manifestRaw
	for _, a := range capture.manifest.Assets {
		path, e := ConfinedLocal(q.Snapshot, a.Path)
		if e != nil {
			return d, e
		}
		b, e := ReadLocal(ctx, path)
		if e != nil || SHA256(b) != a.SHA256 {
			return d, errors.New("capture changed during planning")
		}
		files["assets/capture/"+a.Path] = b
	}
	if desired.Operation == "comment" {
		var body string
		if json.Unmarshal(desired.Fields["body"], &body) == nil {
			files["assets/comment.txt"] = []byte(body)
		}
	}
	if q.BindingRoot != "" {
		for i, a := range binding.Spec.Evidence {
			p, e := ConfinedLocal(q.BindingRoot, a.Path)
			if e != nil {
				return d, e
			}
			raw, e := ReadLocal(ctx, p)
			if e != nil || SHA256(raw) != string(a.Sha256) {
				return d, errors.New("binding evidence changed")
			}
			evidence, e := loadSetupReportBundle(ctx, filepath.Dir(p))
			if e != nil {
				return d, e
			}
			for n, b := range evidence {
				files["assets/binding-evidence/"+string(rune('a'+i))+"/"+n] = b
			}
		}
	}
	if e = WriteOperationDraft(ctx, d, files, q.Out); e != nil {
		return d, e
	}
	prepared, e := PrepareOperation(ctx, q.Out)
	if e != nil {
		return d, e
	}
	return prepared.Draft(), nil
}
func evaluateItemOperation(b *ReadBundle, meta s.Observation, desired o.Desired, mapping api.Mapping, binding api.WorkBinding, cap o.Capability, policy o.Policy, inputs map[string]string, policyRef, capRef api.Ref) (o.Draft, error) {
	d := o.Draft{Version: o.Version, Kind: "OperationDraft", LogicalKey: desired.LogicalKey, Operation: desired.Operation, ParentID: desired.ParentID, Inputs: cloneRead(inputs), Changes: cloneRead(desired.Fields), DependsOn: append([]string{}, desired.DependsOn...), Status: "draft", Diagnostics: []api.Diagnostic{}, Assets: []api.Asset{}, RequiredConcurrency: "none"}
	at, e := time.Parse(time.RFC3339Nano, inputs["asOf"])
	if e != nil || desired.Version != o.Version || desired.Kind != "DesiredChange" || desired.LogicalKey == "" || len(desired.LogicalKey) > 256 || len(desired.Fields) > 128 {
		return d, errors.New("invalid desired change or explicit time")
	}
	if b.manifest.OriginalObservedAt == nil {
		return d, errors.New("source observation time missing")
	}
	d.ObservedAt = *b.manifest.OriginalObservedAt
	d.Target = r.Identity{Provider: meta.Scope.Provider, Origin: meta.Scope.Origin, BasePath: meta.Scope.BasePath, TenantID: meta.Scope.TenantID, ContainerID: meta.Scope.ContainerID, ItemKind: "container", NativeID: desired.ParentID}
	d.Revision = api.Revision{Kind: "observation-fingerprint", Value: b.Digest(), ObservedAt: api.Time(d.ObservedAt)}
	found := false
	for _, record := range b.records {
		if record.Identity.NativeID == desired.ItemID {
			if found {
				return d, errors.New("ambiguous target")
			}
			found = true
			d.Target = record.Identity
			d.Revision = api.Revision{Kind: record.Data.RevisionKind, Value: record.Data.Revision, ObservedAt: api.Time(d.ObservedAt)}
			if !record.Data.Complete || len(record.Data.LossyFields) > 0 {
				operationBlock(&d, "operation.incomplete", "snapshot", "complete lossless target required")
			}
		}
	}
	if desired.Operation != "create-item" && !found {
		operationBlock(&d, "operation.target", "itemId", "exact existing item required")
	}
	if desired.Operation == "create-item" && (desired.ItemID != "" || !desired.AllowCreate || desired.ParentID != meta.Scope.ContainerID) {
		operationBlock(&d, "operation.create", "parentId", "explicit existing parent and creation permission required")
	}
	observed, e := time.Parse(time.RFC3339Nano, d.ObservedAt)
	if e != nil || at.Before(observed) || at.Sub(observed) >= 5*time.Minute || !b.manifest.Complete {
		operationBlock(&d, "operation.stale", "snapshot", "complete fresh non-future snapshot required")
	}
	if meta.ConfigurationSHA256 != b.manifest.ConfigurationSHA256 || meta.Scope.Provider != providerName(b.config.Source.Provider) || meta.Scope.Principal != b.config.Source.PrincipalPseudonym {
		operationBlock(&d, "operation.scope", "observation", "metadata and target configuration differ")
	}
	metaFresh := check.NewReport("MetadataCheck", inputs["asOf"])
	check.Fresh(&metaFresh, meta, inputs["asOf"], 300)
	if metaFresh.Status != "ok" {
		operationBlock(&d, "operation.metadata-stale", "metadata", "fresh metadata required")
	}
	bindingScope := false
	for _, id := range binding.Spec.Scope.ContainerIds {
		if id == meta.Scope.ContainerID {
			bindingScope = true
		}
	}
	if !bindingScope || binding.Spec.Scope.TenantId != meta.Scope.TenantID || binding.Spec.MappingRef.Id != mapping.Metadata.Id || binding.Spec.MappingRef.Revision != mapping.Metadata.Revision || binding.Spec.Status != "verified" || binding.Spec.ConnectionRef != meta.ConnectionRef || binding.Spec.MappingRef.Sha256 != api.Sha(inputs["mapping"]) {
		operationBlock(&d, "operation.binding", "binding", "exact verified binding needed; current independent proof remains separate")
	}
	if cap.Version != o.Version || cap.Kind != "OperationCapability" || cap.Scope != meta.Scope || cap.AuthenticationMode != b.config.Source.Auth.Mode || cap.ConfigurationSHA256 != b.manifest.ConfigurationSHA256 || cap.Transport != b.manifest.Transport || cap.Operation != desired.Operation || !cap.Supported {
		operationBlock(&d, "operation.capability", "capability", "declared capability unavailable")
	}
	ct, e := time.Parse(time.RFC3339Nano, cap.ObservedAt)
	if e != nil || at.Before(ct) || at.Sub(ct) >= 5*time.Minute {
		operationBlock(&d, "operation.capability-stale", "capability", "fresh capability required")
	}
	allowed := false
	for _, scope := range policy.AllowedScopes {
		if scope == meta.Scope {
			allowed = true
		}
	}
	ps, e1 := time.Parse(time.RFC3339Nano, policy.NotBefore)
	pe, e2 := time.Parse(time.RFC3339Nano, policy.ExpiresAt)
	if policy.Version != o.Version || policy.Kind != "OperationPolicy" || policy.MaxOperations < 1 || policy.MaxOperations > 100 || !allowed || e1 != nil || e2 != nil || at.Before(ps) || !at.Before(pe) {
		operationBlock(&d, "operation.policy", "policy", "current declared scope/policy required; issuance trust remains separate")
	}
	switch desired.Operation {
	case "comment":
		var body string
		if len(desired.Fields) != 1 || json.Unmarshal(desired.Fields["body"], &body) != nil || body == "" || len(body) > 1<<20 || strings.Contains(body, "<!-- aok-op:") {
			operationBlock(&d, "operation.comment", "body", "bounded explicit comment required")
		}
		if meta.Scope.Provider != "github" && meta.Scope.Provider != "linear" && meta.Scope.Provider != "jira-cloud" {
			operationBlock(&d, "operation.unsupported", "provider", "concrete comment adapter unavailable")
		}
	case "update-fields":
		d.RequiredConcurrency = "provider-cas"
		if d.Revision.Kind != "native-revision" && d.Revision.Kind != "etag" || cap.Concurrency != "provider-cas" {
			operationBlock(&d, "operation.concurrency", "revision", "fingerprints and local locks do not establish CAS")
		}
		if meta.Scope.Provider != "azure-devops" {
			operationBlock(&d, "operation.unsupported", "provider", "concrete overwrite adapter unavailable")
		}
		for field, value := range desired.Fields {
			matched := false
			for _, m := range mapping.Spec.Fields {
				if m.TargetFieldId == field && m.Owner == "source" && m.LossPolicy != "explicit-transform" && ordinaryMappingCanonical(m.Canonical) && cap.FieldRoles[field] == "ordinary" {
					matched = true
				}
			}
			native, ok := check.Find(meta, "field", field)
			if !matched || !ok || native.ParentID != meta.Scope.ContainerID || !setupValueType(native.Type, value) {
				operationBlock(&d, "operation.field", field, "source-owned compatible observed field required")
			}
			lower := strings.ToLower(field)
			if strings.Contains(lower, "state") || strings.Contains(lower, "status") || strings.Contains(lower, "accept") {
				operationBlock(&d, "operation.acceptance", field, "completion/state updates require independently accepted fenced lineage; manual draft only")
			}
		}
	case "transition":
		d.RequiredConcurrency = "provider-cas"
		operationBlock(&d, "operation.acceptance", "transition", "current item prerequisites, acceptance and qualified transition adapter required")
	case "relate":
		operationBlock(&d, "operation.unsupported", "relationships", "exact qualified relation writer unavailable")
	case "create-item":
		for field, value := range desired.Fields {
			native, ok := check.Find(meta, "field", field)
			owned := false
			for _, m := range mapping.Spec.Fields {
				if m.TargetFieldId == field && m.Owner == "source" && m.LossPolicy != "explicit-transform" && ordinaryMappingCanonical(m.Canonical) && cap.FieldRoles[field] == "ordinary" {
					owned = true
				}
			}
			if !ok || !owned || !setupValueType(native.Type, value) {
				operationBlock(&d, "operation.create-field", field, "explicit source-owned compatible schema field required")
			}
		}
		d.Inputs["typeId"] = desired.TypeID
		d.Inputs["creationSchema"] = desired.SchemaSHA256
		_, knownType := check.Find(meta, "type", desired.TypeID)
		if desired.TypeID == "" || desired.SchemaSHA256 != check.Hash(meta) || !knownType || !check.Category(meta, "types") {
			operationBlock(&d, "operation.create-schema", "typeId", "explicit complete captured type/schema required")
		}
		if !cap.Supported {
			operationBlock(&d, "operation.unsupported", "create", "injected qualified creator required")
		}
	default:
		return d, errors.New("unsupported operation kind")
	}
	d.ID = operationID(d.LogicalKey, d.Target, d.Operation)
	target := api.Identity{Provider: d.Target.Provider, Origin: d.Target.Origin, BasePath: d.Target.BasePath, TenantId: d.Target.TenantID, ContainerId: d.Target.ContainerID, ItemKind: d.Target.ItemKind, NativeId: d.Target.NativeID}
	changes := []api.OperationPlanSpecChangesItem{}
	names := []string{}
	for n := range d.Changes {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		changes = append(changes, api.OperationPlanSpecChangesItem{FieldId: n, Value: d.Changes[n]})
	}
	deps := []api.Id{}
	for _, id := range d.DependsOn {
		if id == d.ID {
			return d, errors.New("self dependency")
		}
		deps = append(deps, api.Id(id))
	}
	revision := d.Revision
	if revision.Kind != "native-revision" && revision.Kind != "etag" && revision.Kind != "none" {
		revision.Kind = "fingerprint"
	}
	d.Plan = api.OperationPlan{APIVersion: api.Version, Kind: "OperationPlan", Metadata: api.Metadata{Id: d.ID, Revision: 1}, Spec: api.OperationPlanSpec{OperationId: api.Id(d.ID), ConnectionRef: &meta.ConnectionRef, Target: &target, Operation: d.Operation, Changes: changes, Precondition: api.OperationPlanSpecPrecondition{Revision: revision, RequiredConcurrency: d.RequiredConcurrency, MaxAgeSeconds: 300}, PolicyRef: policyRef, CapabilityRef: capRef, Evidence: []api.Asset{}, DependsOn: deps}}
	return d, nil
}

// Unknown/canonical lifecycle fields never obtain write authority from an opaque
// native ID. Protected capability evidence must independently bind field roles.
func ordinaryMappingCanonical(c string) bool {
	switch c {
	case "title", "description", "priority", "estimate":
		return true
	}
	return false
}
