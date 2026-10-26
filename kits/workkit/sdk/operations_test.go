package sdk

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	o "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/operationsv1"
	r "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/readv1"
	s "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/setupv1"
	y "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/syncv1"
	api "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/v1alpha1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/engine"
	check "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/setup"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/readsource"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func operationFixture(t *testing.T, provider string) (PlanItemRequest, o.Draft) {
	t.Helper()
	ctx := context.Background()
	capture, _ := importFixture(t, provider, provider)
	if os.Getenv("AOK_TEST_WORKKIT_FRESH_FIXTURE") == "1" {
		capture = freshSyntheticOperationCapture(t, provider)
	}
	b, err := LoadReadBundle(ctx, capture)
	if err != nil {
		t.Fatal(err)
	}
	fixtureProvider := providerName(provider)
	meta, _, _, _, _, _ := setupFixture(t, fixtureProvider)
	root := readTestDir(t)
	write := func(n string, v any) string {
		p := filepath.Join(root, n)
		if e := os.WriteFile(p, engine.Encode(v), 0600); e != nil {
			t.Fatal(e)
		}
		return p
	}
	cfg := b.config
	var profile api.ConnectionProfile
	if DecodeRead(cfg.Profile, &profile) != nil {
		t.Fatal("profile")
	}
	ref := api.Ref{Kind: "ConnectionProfile", Id: profile.Metadata.Id, Revision: profile.Metadata.Revision, Sha256: api.Sha(SHA256(cfg.Profile))}
	scope := s.Scope{Provider: fixtureProvider, Origin: cfg.Source.Origin, TenantID: cfg.Source.NativeTenantID, ContainerID: cfg.Source.ContainerID, APIVersion: cfg.Source.APIVersion, Principal: cfg.Source.PrincipalPseudonym}
	native := engine.Encode(map[string]any{"complete": true, "entities": []map[string]any{{"id": scope.ContainerID, "parentId": scope.TenantID, "name": "container", "nativeType": ""}, {"id": "title", "parentId": scope.ContainerID, "name": "Title", "nativeType": "text", "options": []any{}}}})
	asset := api.Asset{Path: "assets/native.json", Sha256: api.Sha(SHA256(native))}
	meta.ConnectionRef = ref
	meta.ConfigurationSHA256 = SHA256(b.configBytes)
	meta.CaptureSHA256 = b.Digest()
	meta.Scope = scope
	meta.ObservedAt = *b.manifest.OriginalObservedAt
	meta.Transport.Principal = scope.Principal
	meta.Entities = []s.Entity{{Kind: "container", ID: scope.ContainerID, ParentID: scope.TenantID, Name: "container", Type: "unknown", Provenance: s.Provenance{Asset: asset, Pointer: "/entities/0", Fields: map[string]string{"id": "/entities/0/id", "parentId": "/entities/0/parentId", "name": "/entities/0/name", "nativeType": "/entities/0/nativeType"}}}, {Kind: "field", ID: "title", ParentID: scope.ContainerID, Name: "Title", Type: "string", NativeType: "text", Options: []s.Option{}, Provenance: s.Provenance{Asset: asset, Pointer: "/entities/1", Fields: map[string]string{"id": "/entities/1/id", "parentId": "/entities/1/parentId", "name": "/entities/1/name", "nativeType": "/entities/1/nativeType", "options": "/entities/1/options"}}}}
	meta.Coverage = []s.Coverage{{Category: "containers", Status: "observed", Complete: true, Evidence: asset, CompletePointer: "/complete"}, {Category: "fields", Status: "observed", Complete: true, Evidence: asset, CompletePointer: "/complete"}}
	meta.Assets = nil
	obs := filepath.Join(root, "metadata")
	if err = WriteSetupObservation(ctx, meta, map[string][]byte{"assets/native.json": native, "assets/profile.json": cfg.Profile, "assets/configuration.json": b.configBytes}, obs); err != nil {
		t.Fatal(err)
	}
	meta, _, err = LoadSetupObservation(ctx, obs)
	if err != nil {
		t.Fatal(err)
	}
	mapping := api.Mapping{APIVersion: api.Version, Kind: "Mapping", Metadata: api.Metadata{Id: "mapping", Revision: 1}, Spec: api.MappingSpec{Fields: []api.MappingSpecFieldsItem{{Canonical: "title", SourceFieldId: "title", TargetFieldId: "title", Owner: "source", LossPolicy: "reject", ValueMap: map[string]json.RawMessage{}}}, Lifecycle: []api.MappingSpecLifecycleItem{}, PersonMapping: []api.MappingSpecPersonMappingItem{}}}
	mappingRaw := engine.Encode(mapping)
	binding := api.WorkBinding{APIVersion: api.Version, Kind: "WorkBinding", Metadata: api.Metadata{Id: "binding", Revision: 1}, Spec: api.WorkBindingSpec{ConnectionRef: ref, MappingRef: api.Ref{Kind: "Mapping", Id: "mapping", Revision: 1, Sha256: api.Sha(SHA256(mappingRaw))}, Scope: api.Scope{TenantId: scope.TenantID, ContainerIds: []string{scope.ContainerID}}, Status: "verified", Checks: []api.WorkBindingSpecChecksItem{{Name: "fixture-only", Status: "satisfied"}}, Evidence: []api.Asset{{Path: "fixture.json", Sha256: api.Sha(SHA256([]byte("fixture-only")))}}}}
	at, _ := time.Parse(time.RFC3339Nano, meta.ObservedAt)
	desired := o.Desired{Version: o.Version, Kind: "DesiredChange", LogicalKey: "synthetic-comment-1", Operation: "comment", ParentID: scope.ContainerID, ItemID: b.records[0].Identity.NativeID, Fields: map[string]json.RawMessage{"body": json.RawMessage(`"synthetic comment"`)}, DependsOn: []string{}}
	cap := o.Capability{AuthenticationMode: b.config.Source.Auth.Mode, ConfigurationSHA256: b.manifest.ConfigurationSHA256, Transport: b.manifest.Transport, Version: o.Version, Kind: "OperationCapability", Scope: scope, Operation: "comment", Supported: true, Qualification: "fixture-only", ObservedAt: meta.ObservedAt, Evidence: []api.Asset{}}
	policy := o.Policy{Version: o.Version, Kind: "OperationPolicy", NotBefore: at.Add(-time.Minute).Format(time.RFC3339Nano), ExpiresAt: at.Add(time.Hour).Format(time.RFC3339Nano), AllowedScopes: []s.Scope{scope}, MaxOperations: 10, AllowedActions: []string{"apply", "reconcile", "cancel"}, Issuers: []r.Issuer{}, RevokedIDs: []string{}}
	q := PlanItemRequest{Snapshot: capture, Observation: obs, Desired: write("desired.json", desired), Mapping: write("mapping.json", mapping), Binding: write("binding.json", binding), Capability: write("capability.json", cap), Policy: write("policy.json", policy), AsOf: at.Add(time.Second).Format(time.RFC3339Nano), Out: filepath.Join(root, "draft"), Operation: "comment"}
	d, err := PlanItemOperation(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	return q, d
}
func TestOperationsProviderDraftsAndTampering(t *testing.T) {
	for _, provider := range []string{"github", "azure-devops", "linear", "jira"} {
		t.Run(provider, func(t *testing.T) {
			q, d := operationFixture(t, provider)
			if provider == "azure-devops" {
				if d.Status != "blocked" {
					t.Fatal("Azure comments unsupported")
				}
			} else if d.Status != "draft" {
				t.Fatalf("%+v", d.Diagnostics)
			}
			p, e := PrepareOperation(context.Background(), q.Out)
			if e != nil || p.Digest() == "" {
				t.Fatal(e)
			}
			if _, e = PlanItemOperation(context.Background(), q); e == nil {
				t.Fatal("overwrite")
			}
			if e = os.WriteFile(filepath.Join(q.Out, "assets/comment.txt"), []byte("changed"), 0600); e != nil {
				t.Fatal(e)
			}
			if _, e = PrepareOperation(context.Background(), q.Out); e == nil {
				t.Fatal("asset tamper")
			}
		})
	}
}
func TestOperationsNoInputAndForgedSetupRejected(t *testing.T) {
	q, d := operationFixture(t, "github")
	d.Inputs = map[string]string{"setupPlan": "forged"}
	d.Assets = []api.Asset{}
	root := filepath.Join(filepath.Dir(q.Out), "forged")
	if e := WriteOperationDraft(context.Background(), d, map[string][]byte{}, root); e != nil {
		t.Fatal(e)
	}
	if _, e := PrepareOperation(context.Background(), root); e == nil {
		t.Fatal("unpinned prepared operation")
	}
}

type operationTestDriver struct{ applied int }

func (d *operationTestDriver) Current(context.Context, PreparedOperation, o.Enrollment, []byte) error {
	return nil
}
func (d *operationTestDriver) Apply(_ context.Context, p PreparedOperation, e o.Enrollment) (o.Observation, error) {
	d.applied++
	return o.Observation{Version: o.Version, Kind: "OperationObservation", OperationID: p.draft.ID, Digest: p.Digest(), Store: e.Store, State: "readback-verified"}, nil
}
func (d *operationTestDriver) Reconcile(_ context.Context, p PreparedOperation, e o.Enrollment) (o.Observation, error) {
	return o.Observation{Version: o.Version, Kind: "OperationObservation", OperationID: p.draft.ID, Digest: p.Digest(), Store: e.Store, State: "readback-verified"}, nil
}
func (d *operationTestDriver) Cancel(_ context.Context, p PreparedOperation, e o.Enrollment) (o.Observation, error) {
	return o.Observation{Version: o.Version, Kind: "OperationObservation", OperationID: p.draft.ID, Digest: p.Digest(), Store: e.Store, State: "readback-verified"}, nil
}
func TestOperationSessionSignedScopeAndSingleUse(t *testing.T) {
	q, _ := operationFixture(t, "github")
	p, e := PrepareOperation(context.Background(), q.Out)
	if e != nil {
		t.Fatal(e)
	}
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	now := time.Now().UTC()
	var cap o.Capability
	_ = DecodeRead(p.files["assets/capability.json"], &cap)
	policy := o.Policy{Version: o.Version, Kind: "OperationPolicy", NotBefore: now.Add(-time.Minute).Format(time.RFC3339Nano), ExpiresAt: now.Add(time.Hour).Format(time.RFC3339Nano), Issuers: []r.Issuer{{ID: "independent", PublicKey: base64.StdEncoding.EncodeToString(pub)}}, AllowedScopes: []s.Scope{cap.Scope}, MaxOperations: 1, AllowedActions: []string{"reconcile"}, RevokedIDs: []string{}}
	raw := engine.Encode(policy)
	en := o.Enrollment{Version: o.Version, Kind: "OperationEnrollment", Purpose: "operation-action", ID: "one-use", Issuer: "independent", OperationSHA256: p.Digest(), Store: o.Store{Version: o.Version, Kind: "IntegrationJournal", Path: filepath.Join(readTestDir(t), "store.db"), ID: "store", Namespace: "fixture", Owner: "test"}, Action: "reconcile", PolicySHA256: SHA256(raw), ImplementationSHA256: OperationsImplementationSHA256(), IssuedAt: now.Add(-time.Second).Format(time.RFC3339Nano), ExpiresAt: now.Add(time.Minute).Format(time.RFC3339Nano), OutputPath: filepath.Join(readTestDir(t), "out")}
	en.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(key, OperationEnrollmentSigningBytes(en)))
	driver := &operationTestDriver{}
	session, e := NewOperationSession(context.Background(), p, raw, engine.Encode(en), driver)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = session.Run(context.Background()); e != nil {
		t.Fatal(e)
	}
	if _, e = session.Run(context.Background()); e == nil {
		t.Fatal("session reused")
	}
	en.Action = "apply"
	if _, e = NewOperationSession(context.Background(), p, raw, engine.Encode(en), driver); e == nil {
		t.Fatal("changed authority")
	}
	policy.AllowedScopes[0].Principal = "other"
	if _, e = NewOperationSession(context.Background(), p, engine.Encode(policy), engine.Encode(en), driver); e == nil {
		t.Fatal("foreign scope")
	}
}
func TestSyncProjectionFailClosedAndPending(t *testing.T) {
	target := r.Identity{Provider: "azure-devops", Origin: "https://dev.azure.com", TenantID: "tenant", ContainerID: "project", NativeID: "42", ItemKind: "work-item"}
	want := o.Desired{LogicalKey: "logical", Operation: "update-fields", ItemID: "42"}
	id := operationID(want.LogicalKey, target, want.Operation)
	c := y.Configuration{Correlations: []y.Correlation{{ID: "link", Destination: target}}}
	p := PreparedSync{plan: y.Plan{Status: "ready", Complete: true, RouteSHA256: "route", Proposals: []o.Desired{want}, Updates: []y.Baseline{{CorrelationID: "link", Field: "title", Source: syncCell(json.RawMessage(`"new"`), true), Destination: syncCell(json.RawMessage(`"new"`), true), ConfirmedOperationID: id}}}, raw: []byte("plan"), files: map[string][]byte{"assets/configuration.json": engine.Encode(c)}}
	prior := y.State{Version: y.Version, Kind: "SyncState", Initialized: true, RouteSHA256: "route", Revision: 1, Baselines: []y.Baseline{{CorrelationID: "link", Field: "title", Source: syncCell(json.RawMessage(`"old"`), true), Destination: syncCell(json.RawMessage(`"old"`), true)}}, Unresolved: []string{}, CompletedWatermark: "old"}
	pending, e := PendingSyncState(p, prior)
	if e != nil || pending.PendingPlanSHA256 != p.Digest() || len(pending.Unresolved) != 1 || pending.CompletedWatermark != "old" {
		t.Fatal(pending, e)
	}
	if _, e = PendingSyncState(p, pending); e == nil {
		t.Fatal("duplicate admission")
	}
	paused, e := ProjectSyncState(p, pending, "sync-pause", nil)
	if e != nil || !paused.Paused || len(paused.Unresolved) != 1 {
		t.Fatal(paused, e)
	}
	settled, e := ProjectSyncState(p, paused, "sync-reconcile", []string{id})
	if e != nil || !settled.Paused || len(settled.Unresolved) != 0 || settled.Baselines[0].ConfirmedOperationID != id {
		t.Fatal(settled, e)
	}
	prior.Paused = true
	if _, e = PendingSyncState(p, prior); e == nil {
		t.Fatal("paused admission")
	}
	if equalCell(syncCell(nil, false), syncCell(json.RawMessage("null"), true)) {
		t.Fatal("missing equals null")
	}
	_ = check.Hash(settled)
}

func TestSyncPinnedPreviewInitializationAndReconstruction(t *testing.T) {
	source, _ := operationFixture(t, "github")
	dest, _ := operationFixture(t, "linear")
	ctx := context.Background()
	a, _, e := LoadSetupObservation(ctx, source.Observation)
	if e != nil {
		t.Fatal(e)
	}
	b, _, e := LoadSetupObservation(ctx, dest.Observation)
	if e != nil {
		t.Fatal(e)
	}
	src, e := LoadReadBundle(ctx, source.Snapshot)
	if e != nil {
		t.Fatal(e)
	}
	dst, e := LoadReadBundle(ctx, dest.Snapshot)
	if e != nil {
		t.Fatal(e)
	}
	root := readTestDir(t)
	write := func(n string, v any) string {
		path := filepath.Join(root, n)
		if e := os.WriteFile(path, engine.Encode(v), 0600); e != nil {
			t.Fatal(e)
		}
		return path
	}
	read := func(p string) []byte {
		b, e := os.ReadFile(p)
		if e != nil {
			t.Fatal(e)
		}
		return b
	}
	var sb, db api.WorkBinding
	_ = DecodeRead(read(source.Binding), &sb)
	_ = DecodeRead(read(dest.Binding), &db)
	mc := s.MappingContext{Version: s.Version, Kind: "MappingContext", MappingSHA256: SHA256(read(source.Mapping)), SourceSHA256: check.Hash(a), DestinationSHA256: check.Hash(b), RequiredFields: []string{"title"}}
	query := r.Query{Version: r.Version, Dialect: "portable", ConnectionID: "", Expression: `title != "excluded"`, Fields: map[string]r.Field{"title": {Type: "string", ProviderFields: map[string]string{"github": "title"}}}, Limit: 100, MaxAgeSeconds: 300}
	queryRaw := engine.Encode(query)
	route := api.SyncRoute{APIVersion: api.Version, Kind: "SyncRoute", Metadata: api.Metadata{Id: "github-linear", Revision: 1}, Spec: api.SyncRouteSpec{Source: api.SyncRouteSpecSource{ConnectionRef: a.ConnectionRef, Scope: sb.Spec.Scope}, Destination: api.SyncRouteSpecDestination{ConnectionRef: b.ConnectionRef, Scope: db.Spec.Scope}, Mode: "one-way", Selector: api.SyncRouteSpecSelector{Dialect: query.Dialect, Expression: query.Expression}, MappingRef: sb.Spec.MappingRef, ConflictPolicy: "manual", DeletePolicy: "ignore", Trigger: "manual", Enabled: true}}
	c := y.Configuration{Version: y.Version, Kind: "SyncConfiguration", Route: route, SourceObservationSHA256: check.Hash(a), DestinationObservationSHA256: check.Hash(b), MappingContextSHA256: check.Hash(mc), SourceBindingSHA256: SHA256(read(source.Binding)), DestinationBindingSHA256: SHA256(read(dest.Binding)), QuerySHA256: SHA256(queryRaw), Correlations: []y.Correlation{{ID: "link", Source: src.records[0].Identity, Destination: dst.records[0].Identity}}, MaxOperations: 10}
	state := y.State{Version: y.Version, Kind: "SyncState", RouteSHA256: SyncRouteSHA256(c), Baselines: []y.Baseline{}, Unresolved: []string{}}
	q := SyncRequest{Configuration: write("configuration.json", c), State: write("state.json", state), Source: source.Snapshot, Destination: dest.Snapshot, SourceObservation: source.Observation, DestinationObservation: dest.Observation, Mapping: source.Mapping, MappingContext: write("mapping-context.json", mc), SourceBinding: source.Binding, DestinationBinding: dest.Binding, Query: write("query.json", query), AsOf: source.AsOf, Out: filepath.Join(root, "preview")}
	if os.Getenv("AOK_TEST_WORKKIT_FRESH_FIXTURE") == "1" {
		q.AsOf = time.Now().UTC().Format(time.RFC3339Nano)
	}
	p, e := PreviewSync(ctx, q)
	if e != nil {
		t.Fatal(e)
	}
	if p.Status != "baseline-candidate" {
		t.Fatalf("%s %+v %+v", p.Status, p.Diagnostics, p.Conflicts)
	}
	if export := os.Getenv("AOK_TEST_WORKKIT_SYNC_EXPORT"); export != "" {
		files, e := loadOperationalReport(ctx, q.Out, y.Version)
		if e != nil {
			t.Fatal(e)
		}
		if e = writeOperationalReport(ctx, export, y.Version, p, filesWithoutReports(files)); e != nil {
			t.Fatal(e)
		}
	}
	loaded, e := LoadSyncPlan(ctx, q.Out)
	if e != nil || check.Hash(loaded) != check.Hash(p) {
		t.Fatal("reconstruction", e)
	}
	prepared, e := PrepareSync(ctx, q.Out)
	if e != nil {
		t.Fatal(e)
	}
	next, e := ProjectSyncState(prepared, state, "sync-initialize", nil)
	if e != nil || !next.Initialized || len(next.Baselines) != 1 {
		t.Fatal(next, e)
	}
	// New synthetic source snapshot changes only the explicitly source-owned title.
	// The initialized baseline and route identity must remain stable across observations.
	sourceRoot := readTestDir(t)
	manifest := readsource.Manifest{Version: readsource.Version, Source: src.config.Source, Mode: "capture", Pages: cloneRead(src.capture.Snapshot.Pages)}
	for i := range manifest.Pages {
		page := &manifest.Pages[i]
		raw := append([]byte(nil), src.capture.Files[page.Asset.Path]...)
		if page.Request.Kind == "github-item" {
			var value map[string]any
			if json.Unmarshal(raw, &value) != nil {
				t.Fatal("source fixture")
			}
			value["title"] = "Changed fixture issue"
			raw = engine.Encode(value)
			page.Asset.SHA256 = SHA256(raw)
		}
		path := filepath.Join(sourceRoot, page.Asset.Path)
		if e = os.MkdirAll(filepath.Dir(path), 0700); e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(path, raw, 0600); e != nil {
			t.Fatal(e)
		}
	}
	manifestPath := filepath.Join(sourceRoot, "manifest.json")
	if e = os.WriteFile(manifestPath, engine.Encode(manifest), 0600); e != nil {
		t.Fatal(e)
	}
	cfgPath := filepath.Join(root, "source-configuration.json")
	if e = os.WriteFile(cfgPath, src.configBytes, 0600); e != nil {
		t.Fatal(e)
	}
	changedCapture := filepath.Join(readTestDir(t), "capture")
	if _, e = ImportRead(ctx, cfgPath, manifestPath, changedCapture); e != nil {
		t.Fatal(e)
	}
	changed, e := LoadReadBundle(ctx, changedCapture)
	if e != nil {
		t.Fatal(e)
	}
	anew := cloneRead(a)
	anew.Assets = nil
	anew.CaptureSHA256 = changed.Digest()
	anew.ConfigurationSHA256 = changed.manifest.ConfigurationSHA256
	_, native, e := LoadSetupObservation(ctx, q.SourceObservation)
	if e != nil {
		t.Fatal(e)
	}
	native["assets/configuration.json"] = changed.configBytes
	newMetadata := filepath.Join(readTestDir(t), "metadata")
	if e = WriteSetupObservation(ctx, anew, native, newMetadata); e != nil {
		t.Fatal(e)
	}
	anew, _, e = LoadSetupObservation(ctx, newMetadata)
	if e != nil {
		t.Fatal(e)
	}
	config2 := cloneRead(c)
	config2.SourceObservationSHA256 = check.Hash(anew)
	mc.SourceSHA256 = check.Hash(anew)
	config2.MappingContextSHA256 = check.Hash(mc)
	q.Configuration = write("configuration-2.json", config2)
	q.State = write("state-2.json", next)
	q.Source = changedCapture
	q.SourceObservation = newMetadata
	q.MappingContext = write("mapping-context-2.json", mc)
	q.Out = filepath.Join(root, "changed-preview")
	changedPlan, e := PreviewSync(ctx, q)
	if e != nil || changedPlan.RouteSHA256 != p.RouteSHA256 || changedPlan.Status != "ready" || len(changedPlan.Proposals) != 1 || len(changedPlan.Updates) != 1 {
		t.Fatalf("changed preview %s %v %+v", changedPlan.Status, e, changedPlan.Diagnostics)
	}
	if string(changedPlan.Proposals[0].Fields["title"]) != `"Changed fixture issue"` {
		t.Fatal("wrong mapped value")
	}
	if !equalCell(changedPlan.Candidate.Baselines[0].Source, next.Baselines[0].Source) {
		t.Fatal("unconfirmed source baseline advanced")
	}

	q.Out = filepath.Join(root, "stale")
	q.AsOf = "2026-10-24T12:10:00Z"
	p, e = PreviewSync(ctx, q)
	if e != nil {
		t.Fatal(e)
	}
	if p.Complete || p.Status != "blocked" {
		t.Fatal("stale baseline accepted", p.Status)
	}
}

// Fresh times below describe newly generated synthetic observations, never a
// replayed historical observation or evidence of provider qualification.
func freshSyntheticOperationCapture(t *testing.T, provider string) string {
	t.Helper()
	ctx := context.Background()
	source := filepath.Join("../../../internal/tracker/testdata/offline", provider)
	raw, e := os.ReadFile(filepath.Join(source, "manifest.json"))
	if e != nil {
		t.Fatal(e)
	}
	var manifest readsource.Manifest
	if DecodeRead(raw, &manifest) != nil {
		t.Fatal("manifest")
	}
	manifest.Source.PrincipalPseudonym = "synthetic-workkit-p5"
	root := readTestDir(t)
	for i := range manifest.Pages {
		manifest.Pages[i].ObservedAt = time.Now().UTC().Add(-time.Second).Format(time.RFC3339Nano)
		asset := manifest.Pages[i].Asset
		raw, e := os.ReadFile(filepath.Join(source, asset.Path))
		if e != nil {
			t.Fatal(e)
		}
		path := filepath.Join(root, asset.Path)
		if e = os.MkdirAll(filepath.Dir(path), 0700); e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(path, raw, 0600); e != nil {
			t.Fatal(e)
		}
	}
	path := filepath.Join(root, "manifest.json")
	if e = os.WriteFile(path, engine.Encode(manifest), 0600); e != nil {
		t.Fatal(e)
	}
	config := readConfig(t, provider, manifest.Source)
	_, cfg, e := writeReadConfig(t, config)
	if e != nil {
		t.Fatal(e)
	}
	out := filepath.Join(readTestDir(t), "capture")
	if _, e = ImportRead(ctx, cfg, path, out); e != nil {
		t.Fatalf("%v: %v", e, errors.Unwrap(e))
	}
	return out
}

func TestPreparedOperationCopiesAndPinnedSyncAuthority(t *testing.T) {
	q, _ := operationFixture(t, "github")
	ctx := context.Background()
	p, e := PrepareOperation(ctx, q.Out)
	if e != nil {
		t.Fatal(e)
	}
	digest := p.Digest()
	d := p.Draft()
	d.Changes["body"] = json.RawMessage(`"forged"`)
	assets := p.Assets()
	assets["assets/comment.txt"][0] = 'X'
	raw := p.Bytes()
	raw[0] = 'x'
	if p.Digest() != digest || string(p.Assets()["assets/comment.txt"]) != "synthetic comment" {
		t.Fatal("mutable prepared object")
	}
	policy, enrollment := []byte("policy"), []byte("enrollment")
	bound := context.WithValue(ctx, syncAdmissionKey{}, syncAdmission{OperationDigest: digest, PolicyDigest: SHA256(policy), EnrollmentDigest: SHA256(enrollment)})
	if e = CheckPinnedSyncOperation(bound, p, policy, enrollment); e != nil {
		t.Fatal(e)
	}
	if e = CheckPinnedSyncOperation(bound, p, []byte("changed"), enrollment); e == nil {
		t.Fatal("policy changed after approved-set validation")
	}
	if e = CheckPinnedSyncOperation(bound, p, policy, []byte("changed")); e == nil {
		t.Fatal("enrollment changed after approved-set validation")
	}
	p.raw = append(p.raw, ' ')
	if e = CheckPinnedSyncOperation(bound, p, policy, enrollment); e == nil {
		t.Fatal("same-ID payload drift admitted")
	}
}

func TestEffectfulCancellationRetainsUncertaintyCode(t *testing.T) {
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
		err := operationFailure("operation.uncertain", "inspect durable intent", 5, cause)
		var typed *Error
		if !errors.As(err, &typed) || typed.ExitCode != 5 || !errors.Is(err, cause) {
			t.Fatal("effectful uncertainty downcast", err)
		}
		before := operationFailure("operation.cancelled", "no send admitted", 1, cause)
		if !errors.As(before, &typed) || typed.ExitCode != 1 {
			t.Fatal("before-send cancellation", before)
		}
		legacy := failure("legacy.cancelled", "legacy", 5, cause)
		if !errors.As(legacy, &typed) || typed.ExitCode != 1 {
			t.Fatal("P1-P4 behavior changed", legacy)
		}
	}
}

func TestRepeatedUncertainObservationCannotSucceed(t *testing.T) {
	for _, state := range []string{"dispatch-intent", "response-observed", "unknown-awaiting-reconciliation"} {
		for _, cause := range []error{nil, context.Canceled, context.DeadlineExceeded} {
			e := operationOutcomeError(o.Observation{State: state, Attempts: 1}, cause, "apply")
			var typed *Error
			if !errors.As(e, &typed) || typed.ExitCode != 5 {
				t.Fatal("uncertain repeated operation succeeded", state, e)
			}
		}
	}
	if e := operationOutcomeError(o.Observation{State: "authorized"}, nil, "apply"); e == nil {
		t.Fatal("authorized is not dispatched")
	}
	if e := operationOutcomeError(o.Observation{State: "readback-verified", Attempts: 1}, nil, "apply"); e != nil {
		t.Fatal("confirmed idempotency", e)
	}
}

func TestSetupPostReadbackRecordFailureRemainsUncertain(t *testing.T) {
	for _, state := range []string{"readback-observed", "readback-verified", "unknown-awaiting-reconciliation"} {
		for _, cause := range []error{context.Canceled, context.DeadlineExceeded, errors.New("record failed")} {
			e := setupOperationOutcomeError(o.Observation{State: state}, cause)
			var typed *Error
			if !errors.As(e, &typed) || typed.ExitCode != 5 {
				t.Fatal("setup effect downcast", state, e)
			}
		}
	}
	if e := setupOperationOutcomeError(o.Observation{State: "unknown-awaiting-reconciliation"}, nil); e == nil {
		t.Fatal("nil unresolved setup succeeded")
	}
	if e := setupOperationOutcomeError(o.Observation{State: "readback-observed"}, nil); e != nil {
		t.Fatal("observed result", e)
	}
}

func TestLiveEvidenceCannotRenewFromRecentPlanTime(t *testing.T) {
	now := time.Now().UTC()
	scope := s.Scope{Provider: "github", Origin: "https://api.github.com", TenantID: "tenant", ContainerID: "repository", APIVersion: "2022-11-28", Principal: "synthetic"}
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	policy := o.Policy{Version: o.Version, Kind: "OperationPolicy", NotBefore: now.Add(-time.Minute).Format(time.RFC3339Nano), ExpiresAt: now.Add(time.Minute).Format(time.RFC3339Nano), Issuers: []r.Issuer{{ID: "synthetic", PublicKey: base64.StdEncoding.EncodeToString(pub)}}, AllowedScopes: []s.Scope{scope}, AllowedActions: []string{"sync-initialize", "apply"}, MaxOperations: 1, RevokedIDs: []string{}}
	policyRaw := engine.Encode(policy)
	sign := func(digest, purpose, action string) []byte {
		e := o.Enrollment{Version: o.Version, Kind: "OperationEnrollment", Purpose: purpose, ID: "new", Issuer: "synthetic", OperationSHA256: digest, Store: o.Store{Version: o.Version, Kind: "IntegrationJournal", Path: filepath.Join(readTestDir(t), "journal.db"), ID: "store", Namespace: "new", Owner: "workkit"}, Action: action, PolicySHA256: SHA256(policyRaw), ImplementationSHA256: OperationsImplementationSHA256(), IssuedAt: now.Add(-time.Second).Format(time.RFC3339Nano), ExpiresAt: now.Add(time.Minute).Format(time.RFC3339Nano), OutputPath: filepath.Join(readTestDir(t), "out")}
		e.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(key, mutationSigningBytes(e)))
		return engine.Encode(e)
	}
	old := now.Add(-8 * time.Minute).Format(time.RFC3339Nano)
	metadata := s.Observation{Scope: scope, ObservedAt: old}
	manifest := r.Manifest{OriginalObservedAt: &old}
	p := PreparedSync{raw: []byte("synthetic-plan"), plan: y.Plan{AsOf: now.Add(-4 * time.Minute).Format(time.RFC3339Nano), Proposals: []o.Desired{}}, files: map[string][]byte{"assets/source-observation/bundle.json": engine.Encode(metadata), "assets/destination-observation/bundle.json": engine.Encode(metadata), "assets/source/bundle.json": engine.Encode(manifest), "assets/destination/bundle.json": engine.Encode(manifest)}}
	if _, e := ValidateSyncAuthority(p, policyRaw, sign(p.Digest(), "sync-action", "sync-initialize")); e == nil {
		t.Fatal("recent plan renewed expired item/schema observations")
	}
	op := PreparedOperation{raw: []byte("synthetic-operation"), draft: o.Draft{Status: "draft", ObservedAt: now.Add(-270 * time.Second).Format(time.RFC3339Nano)}, files: map[string][]byte{"assets/capability.json": engine.Encode(o.Capability{Scope: scope, ObservedAt: old}), "assets/metadata/bundle.json": engine.Encode(metadata)}}
	if e := ValidateOperationAuthority(op, policyRaw, sign(op.Digest(), "operation-action", "apply")); e == nil {
		t.Fatal("fresh item observation renewed expired capability/schema")
	}
}

func TestDomainOperationReceiptRetainsDetailedObservation(t *testing.T) {
	seen := map[string]bool{}
	for _, state := range []string{"prepared", "authorized", "dispatch-intent", "response-observed", "unknown-awaiting-reconciliation", "readback-verified", "rejected", "cancelled"} {
		observation := o.Observation{Version: o.Version, Kind: "OperationObservation", OperationID: "op-synthetic", Digest: SHA256([]byte("bundle")), State: state, ObservedAt: "2026-10-26T00:00:00Z", Evidence: []api.Asset{}}
		r, e := ProjectOperationReceipt(observation, SHA256([]byte("exact-domain-plan")))
		if e != nil {
			t.Fatal(state, e)
		}
		if seen[r.Metadata.Id] {
			t.Fatal("immutable observations share a receipt identity")
		}
		seen[r.Metadata.Id] = true
		var retained o.Observation
		if DecodeRead(r.Extensions["workkit/observation"], &retained) != nil || check.Hash(retained) != check.Hash(observation) {
			t.Fatal("lossy receipt")
		}
		if state == "unknown-awaiting-reconciliation" && r.Spec.Status != "unknown" {
			t.Fatal("unknown receipt promoted")
		}
		if r.Spec.PlanSha256 != api.Sha(SHA256([]byte("exact-domain-plan"))) {
			t.Fatal("wrong plan digest")
		}
	}
}
