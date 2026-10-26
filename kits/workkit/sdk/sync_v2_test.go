package sdk

import (
	"context"
	"encoding/json"
	"errors"
	r "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/readv1"
	s "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/setupv1"
	y "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/syncv1"
	z "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/syncv2"
	api "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/v1alpha1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/engine"
	check "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/setup"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/readsource"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func p7write(t *testing.T, root, name string, v any) string {
	t.Helper()
	path := filepath.Join(root, name)
	if e := os.WriteFile(path, engine.Encode(v), 0600); e != nil {
		t.Fatal(e)
	}
	return path
}
func p7capture(t *testing.T, side, title string) (string, *ReadBundle) {
	t.Helper()
	root := readTestDir(t)
	source := "../../../internal/tracker/testdata/offline/azure-devops"
	raw, e := os.ReadFile(filepath.Join(source, "manifest.json"))
	if e != nil {
		t.Fatal(e)
	}
	var manifest readsource.Manifest
	DecodeRead(raw, &manifest)
	manifest.Source.NativeTenantID = "tenant-" + side
	manifest.Source.ContainerID = side + "-guid"
	manifest.Source.ContainerKey = "acme/" + side
	manifest.Source.PrincipalPseudonym = "synthetic-p7-" + side
	for index := range manifest.Pages {
		p := &manifest.Pages[index]
		p.ObservedAt = "2026-10-26T06:00:00Z"
		if os.Getenv("AOK_TEST_P7_FRESH") == "1" {
			p.ObservedAt = time.Now().UTC().Add(-2 * time.Second).Format(time.RFC3339Nano)
		}
		p.Request.Path = strings.ReplaceAll(strings.ReplaceAll(p.Request.Path, "/projects/project", "/projects/"+side), "/acme/project/", "/acme/"+side+"/")
		raw, e := os.ReadFile(filepath.Join(source, p.Asset.Path))
		if e != nil {
			t.Fatal(e)
		}
		raw = []byte(strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(string(raw), "project-guid", side+"-guid"), "acme/project/", "acme/"+side+"/"), "Fixture issue", title))
		raw = []byte(strings.ReplaceAll(string(raw), `"project"`, `"`+side+`"`))
		p.Asset.SHA256 = SHA256(raw)
		path := filepath.Join(root, p.Asset.Path)
		os.MkdirAll(filepath.Dir(path), 0700)
		os.WriteFile(path, raw, 0600)
	}
	manifestPath := p7write(t, root, "manifest.json", manifest)
	configRaw := readConfig(t, side, manifest.Source)
	_, configuration, e := writeReadConfig(t, configRaw)
	if e != nil {
		t.Fatal(e)
	}
	out := filepath.Join(readTestDir(t), "capture")
	if _, e = ImportRead(context.Background(), configuration, manifestPath, out); e != nil {
		t.Fatal(e, errors.Unwrap(e))
	}
	bundle, e := LoadReadBundle(context.Background(), out)
	if e != nil {
		t.Fatal(e)
	}
	return out, bundle
}
func p7metadata(t *testing.T, b *ReadBundle) (string, s.Observation) {
	t.Helper()
	root := readTestDir(t)
	m, _, _, _, _, _ := setupFixture(t, "azure-devops")
	var profile api.ConnectionProfile
	DecodeRead(b.config.Profile, &profile)
	m.ConnectionRef = api.Ref{Kind: "ConnectionProfile", Id: profile.Metadata.Id, Revision: profile.Metadata.Revision, Sha256: api.Sha(SHA256(b.config.Profile))}
	m.ConfigurationSHA256 = b.manifest.ConfigurationSHA256
	m.CaptureSHA256 = b.Digest()
	m.Scope = s.Scope{Provider: "azure-devops", Origin: b.config.Source.Origin, TenantID: b.config.Source.NativeTenantID, ContainerID: b.config.Source.ContainerID, APIVersion: b.config.Source.APIVersion, Principal: b.config.Source.PrincipalPseudonym}
	m.ObservedAt = *b.manifest.OriginalObservedAt
	m.Transport = b.manifest.Transport
	m.Revision = b.Digest()
	native := engine.Encode(map[string]any{"complete": true, "entities": []map[string]any{{"id": m.Scope.ContainerID, "parentId": m.Scope.TenantID, "name": "container", "nativeType": ""}, {"id": "System.Title", "parentId": m.Scope.ContainerID, "name": "Title", "nativeType": "text", "options": []any{}}}})
	asset := api.Asset{Path: "assets/native.json", Sha256: api.Sha(SHA256(native))}
	m.Entities = []s.Entity{{Kind: "container", ID: m.Scope.ContainerID, ParentID: m.Scope.TenantID, Name: "container", Type: "unknown", Provenance: s.Provenance{Asset: asset, Pointer: "/entities/0", Fields: map[string]string{"id": "/entities/0/id", "parentId": "/entities/0/parentId", "name": "/entities/0/name", "nativeType": "/entities/0/nativeType"}}}, {Kind: "field", ID: "System.Title", ParentID: m.Scope.ContainerID, Name: "Title", Type: "string", NativeType: "text", Options: []s.Option{}, Provenance: s.Provenance{Asset: asset, Pointer: "/entities/1", Fields: map[string]string{"id": "/entities/1/id", "parentId": "/entities/1/parentId", "name": "/entities/1/name", "nativeType": "/entities/1/nativeType", "options": "/entities/1/options"}}}}
	m.Coverage = []s.Coverage{{Category: "containers", Status: "observed", Complete: true, Evidence: asset, CompletePointer: "/complete"}, {Category: "fields", Status: "observed", Complete: true, Evidence: asset, CompletePointer: "/complete"}}
	m.Assets = nil
	out := filepath.Join(root, "metadata")
	if e := WriteSetupObservation(context.Background(), m, map[string][]byte{"assets/native.json": native, "assets/profile.json": b.config.Profile, "assets/configuration.json": b.configBytes}, out); e != nil {
		t.Fatal(e)
	}
	loaded, _, e := LoadSetupObservation(context.Background(), out)
	if e != nil {
		t.Fatal(e)
	}
	return out, loaded
}
func p7fixture(t *testing.T, left, right, owner string, initialized bool) (SyncV2Request, z.State) {
	t.Helper()
	root := readTestDir(t)
	source, sb := p7capture(t, "left", left)
	dest, db := p7capture(t, "right", right)
	sm, src := p7metadata(t, sb)
	dm, dst := p7metadata(t, db)
	mapping := func(id string) api.Mapping {
		return api.Mapping{APIVersion: api.Version, Kind: "Mapping", Metadata: api.Metadata{Id: id, Revision: 1}, Spec: api.MappingSpec{Fields: []api.MappingSpecFieldsItem{{Canonical: "title", SourceFieldId: "System.Title", TargetFieldId: "System.Title", Owner: "source", LossPolicy: "reject", ValueMap: map[string]json.RawMessage{}}}, Lifecycle: []api.MappingSpecLifecycleItem{}, PersonMapping: []api.MappingSpecPersonMappingItem{}}}
	}
	fm, rm := mapping("forward"), mapping("reverse")
	fraw, rraw := engine.Encode(fm), engine.Encode(rm)
	binding := func(meta s.Observation, m api.Mapping, raw []byte) api.WorkBinding {
		return api.WorkBinding{APIVersion: api.Version, Kind: "WorkBinding", Metadata: api.Metadata{Id: "binding-" + string(meta.ConnectionRef.Id), Revision: 1}, Spec: api.WorkBindingSpec{ConnectionRef: meta.ConnectionRef, MappingRef: api.Ref{Kind: "Mapping", Id: m.Metadata.Id, Revision: 1, Sha256: api.Sha(SHA256(raw))}, Scope: api.Scope{TenantId: meta.Scope.TenantID, ContainerIds: []string{meta.Scope.ContainerID}}, Status: "verified", Checks: []api.WorkBindingSpecChecksItem{{Name: "fixture-only", Status: "satisfied"}}, Evidence: []api.Asset{{Path: "fixture.json", Sha256: api.Sha(SHA256([]byte("fixture")))}}}}
	}
	sourceBinding, destBinding := binding(src, rm, rraw), binding(dst, fm, fraw)
	forwardContext := s.MappingContext{Version: s.Version, Kind: "MappingContext", MappingSHA256: SHA256(fraw), SourceSHA256: check.Hash(src), DestinationSHA256: check.Hash(dst), RequiredFields: []string{"title"}}
	reverseContext := s.MappingContext{Version: s.Version, Kind: "MappingContext", MappingSHA256: SHA256(rraw), SourceSHA256: check.Hash(dst), DestinationSHA256: check.Hash(src), RequiredFields: []string{"title"}}
	query := r.Query{Version: r.Version, Dialect: "portable", Expression: `title != "excluded"`, Fields: map[string]r.Field{"title": {Type: "string", ProviderFields: map[string]string{"left": "System.Title", "right": "System.Title"}}}, Limit: 100, MaxAgeSeconds: 300}
	qraw := engine.Encode(query)
	c := z.Configuration{Version: z.Version, Kind: "DirectionalSyncConfiguration", ID: "azure-two-scopes", Revision: 1, SourceConfigurationSHA256: sb.manifest.ConfigurationSHA256, DestinationConfigurationSHA256: db.manifest.ConfigurationSHA256, SourceBindingSHA256: SHA256(engine.Encode(sourceBinding)), DestinationBindingSHA256: SHA256(engine.Encode(destBinding)), ForwardMappingSHA256: SHA256(fraw), ReverseMappingSHA256: SHA256(rraw), SourceQuerySHA256: SHA256(qraw), DestinationQuerySHA256: SHA256(qraw), Rules: []z.Rule{{ID: "title", SourceField: "System.Title", DestinationField: "System.Title", Owner: owner, Category: "ordinary"}}, Correlations: []y.Correlation{{ID: "link", Source: sb.records[0].Identity, Destination: db.records[0].Identity}}}
	state := z.State{Version: z.Version, Kind: "DirectionalSyncState", RouteID: c.ID, ConfigurationSHA256: SHA256(engine.Encode(c)), Revision: 0, Initialized: initialized, Baselines: []z.Baseline{}, Unresolved: []string{}}
	if initialized {
		state.Revision = 1
		state.Baselines = []z.Baseline{{CorrelationID: "link", RuleID: "title", Source: syncCell(json.RawMessage(`"base"`), true), Destination: syncCell(json.RawMessage(`"base"`), true), ConfirmedOperations: []string{}}}
	}
	q := SyncV2Request{Configuration: p7write(t, root, "configuration.json", c), State: p7write(t, root, "state.json", state), Source: source, Destination: dest, SourceObservation: sm, DestinationObservation: dm, ForwardMapping: p7write(t, root, "forward.json", fm), ReverseMapping: p7write(t, root, "reverse.json", rm), ForwardContext: p7write(t, root, "forward-context.json", forwardContext), ReverseContext: p7write(t, root, "reverse-context.json", reverseContext), SourceBinding: p7write(t, root, "source-binding.json", sourceBinding), DestinationBinding: p7write(t, root, "destination-binding.json", destBinding), SourceQuery: p7write(t, root, "source-query.json", query), DestinationQuery: p7write(t, root, "destination-query.json", query), AsOf: "2026-10-26T06:00:01Z", Out: filepath.Join(root, "plan")}
	if os.Getenv("AOK_TEST_P7_FRESH") == "1" {
		q.AsOf = time.Now().UTC().Format(time.RFC3339Nano)
	}
	return q, state
}
func TestP7ThreeWayDirections(t *testing.T) {
	for _, tc := range []struct {
		name, left, right, owner, status, direction string
		changes, advances                           int
	}{{"unchanged", "base", "base", "shared", "ready", "", 0, 0}, {"forward", "left", "base", "shared", "ready", "forward", 1, 0}, {"reverse", "base", "right", "shared", "ready", "reverse", 1, 0}, {"equal-concurrent", "equal", "equal", "shared", "ready", "", 0, 1}, {"divergent", "left", "right", "shared", "conflict", "", 0, 0}, {"protected-reverse", "base", "right", "source", "conflict", "", 0, 0}, {"human", "left", "base", "human", "conflict", "", 0, 0}} {
		t.Run(tc.name, func(t *testing.T) {
			q, _ := p7fixture(t, tc.left, tc.right, tc.owner, true)
			p, e := PreviewSyncV2(context.Background(), q)
			if e != nil || p.Status != tc.status || len(p.Changes) != tc.changes || len(p.Advances) != tc.advances {
				t.Fatalf("%s changes%d advances%d diagnostics%+v conflicts%+v err%v", p.Status, len(p.Changes), len(p.Advances), p.Diagnostics, p.Conflicts, e)
			}
			if tc.direction != "" && p.Changes[0].Direction != tc.direction {
				t.Fatal("wrong direction")
			}
			prepared, e := PrepareSyncV2(context.Background(), q.Out)
			if e != nil || prepared.Digest() != SHA256(engine.Encode(p)) {
				t.Fatal("reproduction", e)
			}
			if p.AuthorizationGranted || p.ExecutionReady || p.MigrationActivated || p.ProductionActivated {
				t.Fatal("authority promoted")
			}
		})
	}
}
func TestP7ManualResolutionAndHardGates(t *testing.T) {
	q, _ := p7fixture(t, "left", "right", "shared", true)
	p, e := PreviewSyncV2(context.Background(), q)
	if e != nil || len(p.Conflicts) != 1 {
		t.Fatal(p, e)
	}
	resolution := z.Resolution{Version: z.Version, Kind: "ConflictResolution", PlanSHA256: SHA256(engine.Encode(p)), Choices: []z.Choice{{ConflictID: p.Conflicts[0].ID, Choice: "source"}}}
	path := p7write(t, filepath.Dir(q.State), "resolution.json", resolution)
	out := filepath.Join(filepath.Dir(q.State), "resolved")
	result, e := ResolveSyncV2(context.Background(), q.Out, path, q.AsOf, out)
	if e != nil || result.Plan.Status != "ready" || len(result.Plan.Changes) != 1 || result.Plan.Changes[0].Direction != "forward" {
		t.Fatal(result, e)
	}
	if _, e = PrepareSyncV2(context.Background(), out); e != nil {
		t.Fatal("resolved plan not admissible", e)
	}
	resolution.Choices[0].ConflictID = "foreign"
	os.WriteFile(path, engine.Encode(resolution), 0600)
	if _, e = ResolveSyncV2(context.Background(), q.Out, path, q.AsOf, ""); e == nil {
		t.Fatal("stale conflict accepted")
	}
}
func TestP7StateProjectionUncertaintyAndTampering(t *testing.T) {
	q, state := p7fixture(t, "left", "base", "shared", true)
	p, e := PreviewSyncV2(context.Background(), q)
	if e != nil {
		t.Fatal(e)
	}
	prepared, e := PrepareSyncV2(context.Background(), q.Out)
	if e != nil {
		t.Fatal(e)
	}
	pending, e := ProjectSyncV2State(prepared, state, "begin", nil)
	if e != nil || len(pending.Unresolved) != 1 {
		t.Fatal(pending, e)
	}
	unknown, e := ProjectSyncV2State(prepared, pending, "finish", nil)
	if e != nil || unknown.CompletedWatermark != "" || unknown.PendingPlanSHA256 == "" || len(unknown.Unresolved) != 1 {
		t.Fatal(unknown, e)
	}
	confirmed, e := ProjectSyncV2State(prepared, pending, "finish", pending.Unresolved)
	if e != nil || confirmed.PendingPlanSHA256 != "" || len(confirmed.Unresolved) != 0 {
		t.Fatal(confirmed, e)
	}
	p.Changes[0].Direction = "reverse"
	os.WriteFile(filepath.Join(q.Out, "report.json"), engine.Encode(p), 0600)
	if _, e = PrepareSyncV2(context.Background(), q.Out); e == nil {
		t.Fatal("direction tamper accepted")
	}
}

// Developer fixture materialization is explicit and never touches a provider.
func TestP7ExportFixtures(t *testing.T) {
	out := os.Getenv("AOK_TEST_P7_EXPORT")
	if out == "" {
		t.Skip("explicit fixture output required")
	}
	if e := os.Mkdir(out, 0700); e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct{ name, left, right string }{{"forward", "changed", "base"}, {"reverse", "base", "changed"}, {"conflict", "left", "right"}, {"equal", "same", "same"}} {
		q, _ := p7fixture(t, tc.left, tc.right, "shared", true)
		if _, e := PreviewSyncV2(context.Background(), q); e != nil {
			t.Fatal(e)
		}
		p, e := PrepareSyncV2(context.Background(), q.Out)
		if e != nil {
			t.Fatal(e)
		}
		dir := filepath.Join(out, tc.name)
		for name, raw := range operationalReportFiles(z.Version, p.plan, p.files) {
			path := filepath.Join(dir, name)
			if e = os.MkdirAll(filepath.Dir(path), 0700); e != nil {
				t.Fatal(e)
			}
			if e = os.WriteFile(path, raw, 0600); e != nil {
				t.Fatal(e)
			}
		}
	}
}

func TestP7ExactComparisonMissingNullAndLargeDecimal(t *testing.T) {
	a := syncCell(json.RawMessage(`9007199254740993.000000000000000001`), true)
	b := syncCell(json.RawMessage(`9007199254740993.000000000000000002`), true)
	if equalCell(a, b) {
		t.Fatal("decimal rounded")
	}
	if !equalCell(syncCell(json.RawMessage(`1.00`), true), syncCell(json.RawMessage(`1e0`), true)) {
		t.Fatal("semantic number reorder")
	}
	if equalCell(syncCell(nil, false), syncCell(json.RawMessage(`null`), true)) {
		t.Fatal("missing became null")
	}
}
func TestP7UnknownTypedWritesStaleAndSubstitution(t *testing.T) {
	for _, category := range []string{"person", "relationship", "lifecycle"} {
		t.Run(category, func(t *testing.T) {
			q, _ := p7fixture(t, "changed", "base", "shared", true)
			var c z.Configuration
			raw, _ := os.ReadFile(q.Configuration)
			DecodeRead(raw, &c)
			c.Rules[0].Category = category
			raw = engine.Encode(c)
			os.WriteFile(q.Configuration, raw, 0600)
			var state z.State
			stateRaw, _ := os.ReadFile(q.State)
			DecodeRead(stateRaw, &state)
			state.ConfigurationSHA256 = SHA256(raw)
			os.WriteFile(q.State, engine.Encode(state), 0600)
			p, e := PreviewSyncV2(context.Background(), q)
			if e != nil || p.Status != "blocked" {
				t.Fatal("unsupported typed operation", p.Status, e)
			}
		})
	}
	q, _ := p7fixture(t, "changed", "base", "shared", true)
	q.AsOf = "2026-10-26T06:05:00Z"
	p, e := PreviewSyncV2(context.Background(), q)
	if e != nil || p.Status != "blocked" || p.Complete {
		t.Fatal("stale admitted", p.Status, e)
	}
	q, _ = p7fixture(t, "changed", "base", "shared", true)
	q.SourceBinding = q.DestinationBinding
	if _, e = PreviewSyncV2(context.Background(), q); e == nil {
		t.Fatal("endpoint binding substitution")
	}
}
