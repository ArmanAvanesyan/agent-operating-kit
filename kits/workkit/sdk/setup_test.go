package sdk

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	r "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/readv1"
	s "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/setupv1"
	api "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/v1alpha1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/engine"
	check "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/setup"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/readsource"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const setupAt = "2026-10-26T00:00:01Z"

func setupFixture(t *testing.T, p string) (s.Observation, map[string][]byte, string, []byte, []byte, []byte) {
	t.Helper()
	root := "../testdata/setup/" + p
	o, a, e := LoadSetupObservation(context.Background(), root)
	if e != nil {
		t.Fatal(e)
	}
	read := func(n string) []byte {
		b, e := os.ReadFile(root + "/" + n)
		if e != nil {
			t.Fatal(e)
		}
		return b
	}
	return o, a, root, read("desired.json"), read("mapping.json"), read("mapping-context.json")
}
func TestSetupFiveProviderFixturesAndPlans(t *testing.T) {
	for _, p := range []string{"github", "azure-devops", "jira-cloud", "jira-datacenter", "linear"} {
		t.Run(p, func(t *testing.T) {
			o, _, root, d, m, c := setupFixture(t, p)
			r, e := EvaluateSetup(o, d, setupAt)
			if e != nil || r.Status != "ok" {
				t.Fatalf("setup: %+v %v", r, e)
			}
			r, e = EvaluateMapping(m, c, o, o, setupAt)
			if e != nil || r.Status != "ok" {
				t.Fatalf("mapping: %+v %v", r, e)
			}
			out := filepath.Join(readTestDir(t), "plan")
			if _, e = PlanSetup(context.Background(), root, root+"/desired.json", setupAt, out); e != nil {
				t.Fatal(e)
			}
			if r, e = ValidateSetupPlan(context.Background(), out, setupAt); e != nil || r.Status != "ok" {
				t.Fatalf("validate: %+v %v", r, e)
			}
			if _, e = PlanSetup(context.Background(), root, root+"/desired.json", setupAt, out); e == nil {
				t.Fatal("output reuse")
			}
			r, e = ValidateSetupPlan(context.Background(), out, "2026-10-26T01:00:00Z")
			if e != nil || r.Status != "blocked" {
				t.Fatal("staleness lost")
			}
		})
	}
}
func TestSetupEvidenceTamperingAndScope(t *testing.T) {
	o, files, _, _, _, _ := setupFixture(t, "github")
	for _, change := range []func(*s.Observation){func(o *s.Observation) { o.Entities[1].Type = "number" }, func(o *s.Observation) { o.Scope.Provider = "jira-cloud" }, func(o *s.Observation) { o.ConfigurationSHA256 = strings.Repeat("a", 64) }, func(o *s.Observation) { o.Entities[1].Required = true }, func(o *s.Observation) { o.Entities[1].ParentID = "foreign" }, func(o *s.Observation) { o.Entities[1].Options = []s.Option{{ID: "fake", Name: "fake"}} }, func(o *s.Observation) { o.Coverage[0].CompletePointer = "/missing" }} {
		copy := cloneRead(o)
		change(&copy)
		if e := WriteSetupObservation(context.Background(), copy, files, filepath.Join(readTestDir(t), "out")); e == nil {
			t.Fatal("tampered normalized metadata accepted")
		}
	}
	root := filepath.Join(readTestDir(t), "out")
	if e := WriteSetupObservation(context.Background(), o, files, root); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(root+"/assets/native.json", []byte(`{}`), 0600); e != nil {
		t.Fatal(e)
	}
	if _, _, e := LoadSetupObservation(context.Background(), root); e == nil {
		t.Fatal("changed raw bytes accepted")
	}
}
func TestSetupPlanningNarrowAdditiveRules(t *testing.T) {
	o, _, _, d, _, _ := setupFixture(t, "github")
	var desired api.DesiredSetup
	if e := setupDocument(d, "DesiredSetup", &desired); e != nil {
		t.Fatal(e)
	}
	desired.Spec.Fields[0].NativeId = nil
	r, e := EvaluateSetup(o, engine.Encode(desired), setupAt)
	if e != nil || r.Status != "blocked" {
		t.Fatal("name reuse")
	}
	desired.Spec.Fields[0].Name = "New field"
	r, e = EvaluateSetup(o, engine.Encode(desired), setupAt)
	if e != nil || r.Status != "partial" || len(r.Changes) != 1 {
		t.Fatal("new field draft", r, e)
	}
	desired.Spec.AllowDestructive = true
	if _, e = EvaluateSetup(o, engine.Encode(desired), setupAt); e == nil {
		t.Fatal("destructive flag")
	}
	azure, _, _, ad, _, _ := setupFixture(t, "azure-devops")
	for i := range azure.Entities {
		if azure.Entities[i].Kind == "process" {
			azure.Entities[i].NativeType = "system"
		}
	}
	r, e = EvaluateSetup(azure, ad, setupAt)
	if e != nil || r.Status != "blocked" {
		t.Fatal("system process")
	}
	partial := cloneRead(o)
	partial.Coverage = nil
	r, e = EvaluateSetup(partial, d, setupAt)
	if e != nil || r.Status != "blocked" {
		t.Fatal("partial promoted")
	}
}
func TestSetupMappingEnumOwnershipAndFreshness(t *testing.T) {
	o, _, _, _, raw, _ := setupFixture(t, "github")
	var m api.Mapping
	if e := setupDocument(raw, "Mapping", &m); e != nil {
		t.Fatal(e)
	}
	m.Spec.Fields[0].SourceFieldId = "field-priority"
	m.Spec.Fields[0].TargetFieldId = "field-priority"
	m.Spec.Fields[0].Canonical = "priority"
	mraw := engine.Encode(m)
	contextFor := func(a, b s.Observation, m []byte) []byte {
		return engine.Encode(s.MappingContext{Version: s.Version, Kind: "MappingContext", MappingSHA256: SHA256(m), SourceSHA256: check.Hash(a), DestinationSHA256: check.Hash(b), RequiredFields: []string{"priority"}})
	}
	dest := cloneRead(o)
	for i := range dest.Entities {
		if dest.Entities[i].ID == "field-priority" {
			dest.Entities[i].Options = []s.Option{{ID: "other", Name: "Other"}}
		}
	}
	r, e := EvaluateMapping(mraw, contextFor(o, dest, mraw), o, dest, setupAt)
	if e != nil || r.Status != "blocked" {
		t.Fatal("invalid enum identity accepted")
	}
	m.Spec.Fields[0].ValueMap = map[string]json.RawMessage{"opt-low": json.RawMessage(`"other"`), "opt-high": json.RawMessage(`"other"`)}
	mraw = engine.Encode(m)
	r, e = EvaluateMapping(mraw, contextFor(o, dest, mraw), o, dest, setupAt)
	if e != nil || r.Status != "ok" {
		t.Fatal(r, e)
	}
	m.Spec.Fields[0].LossPolicy = "explicit-transform"
	mraw = engine.Encode(m)
	r, e = EvaluateMapping(mraw, contextFor(o, dest, mraw), o, dest, setupAt)
	if e != nil || r.Status != "blocked" {
		t.Fatal("transform accepted")
	}
	if setupValueType("integer", []byte(`1.5`)) || !setupValueType("number", []byte(`9007199254740993.00000000000001`)) {
		t.Fatal("numeric narrowing/rounding")
	}
}
func TestSetupP2ProjectionRemainsQualificationOnly(t *testing.T) {
	for _, p := range []string{"github", "azure-devops", "linear", "jira"} {
		t.Run(p, func(t *testing.T) {
			capture, _ := importFixture(t, p, "connection")
			out := filepath.Join(readTestDir(t), "metadata")
			o, e := New(nil).DiscoverSetup(context.Background(), capture, out)
			if e != nil {
				t.Fatal(e)
			}
			if !o.QualificationOnly {
				t.Fatal("capture promoted")
			}
			loaded, _, e := LoadSetupObservation(context.Background(), out)
			if e != nil {
				t.Fatal(e)
			}
			unknown := false
			for _, c := range loaded.Coverage {
				if !c.Complete {
					unknown = true
				}
			}
			if !unknown {
				t.Fatal("unobserved catalog fabricated")
			}
		})
	}
}

type setupFixtureTrust struct {
	mu      sync.Mutex
	used    map[string]bool
	revoked bool
	failAt  int
	calls   int
}

func (t *setupFixtureTrust) Current(ctx context.Context, v s.Verification, p []byte) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.calls++
	if t.revoked || t.failAt > 0 && t.calls == t.failAt {
		return errors.New("revoked")
	}
	return ctx.Err()
}
func (t *setupFixtureTrust) Consume(ctx context.Context, v s.Verification) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.used[v.ID] {
		return errors.New("replayed")
	}
	t.used[v.ID] = true
	return ctx.Err()
}
func setupVerifyFixture(t *testing.T, write bool) (*Service, VerifySetupRequest, *setupFixtureTrust, s.Verification) {
	t.Helper()
	ctx := context.Background()
	o, assets, _, desired, mapping, _ := setupFixture(t, "github")
	o.ObservedAt = time.Now().UTC().Format(time.RFC3339Nano)
	o.QualificationOnly = false
	root := readTestDir(t)
	observation := root + "/observation"
	if e := WriteSetupObservation(ctx, o, assets, observation); e != nil {
		t.Fatal(e)
	}
	o, _, _ = LoadSetupObservation(ctx, observation)
	for _, dir := range []string{"resources", "assets", "outputs"} {
		if e := os.Mkdir(root+"/"+dir, 0700); e != nil {
			t.Fatal(e)
		}
	}
	writeFile := func(n string, b []byte) string {
		p := root + "/" + n
		if e := os.WriteFile(p, b, 0600); e != nil {
			t.Fatal(e)
		}
		return p
	}
	mp := writeFile("resources/mapping.json", mapping)
	writeFile("resources/profile.json", assets["assets/profile.json"])
	dp := writeFile("desired.json", desired)
	var m api.Mapping
	_ = setupDocument(mapping, "Mapping", &m)
	mc := engine.Encode(s.MappingContext{Version: s.Version, Kind: "MappingContext", MappingSHA256: SHA256(mapping), SourceSHA256: check.Hash(o), DestinationSHA256: check.Hash(o), RequiredFields: []string{"title"}})
	cp := writeFile("context.json", mc)
	workspace := api.Workspace{APIVersion: api.Version, Kind: "Workspace", Metadata: api.Metadata{Id: "fixture", Revision: 1}, Spec: api.WorkspaceSpec{Connections: []api.Ref{o.ConnectionRef}, Bindings: []api.Ref{}, SyncRoutes: []api.Ref{}, NativeProfiles: []api.Ref{}}}
	wp := writeFile("workspace.json", engine.Encode(workspace))
	expected := ""
	if write {
		expected = SHA256(engine.Encode(workspace))
	}
	now := time.Now().UTC()
	pub, key, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	policy := s.Policy{Version: s.Version, Kind: "SetupPolicy", NotBefore: now.Add(-time.Minute).Format(time.RFC3339Nano), ExpiresAt: now.Add(4 * time.Minute).Format(time.RFC3339Nano), MaxAgeSeconds: 120, Issuers: []r.Issuer{}, RevokedIDs: []string{}, AllowedScopes: []s.Scope{o.Scope}}
	_ = pub
	_ = key
	_ = policy
	q := VerifySetupRequest{Observation: observation, Desired: dp, Mapping: mp, MappingContext: cp, Out: root + "/outputs/verify", Workspace: wp, Root: root, ExpectedWorkspaceSHA256: expected, WriteBinding: write}
	evaluation, e := EvaluateSetup(o, desired, now.Format(time.RFC3339Nano))
	if e != nil {
		t.Fatal(e)
	}
	evaluation.Inputs["mapping"] = SHA256(mapping)
	evaluation.Inputs["mappingContext"] = SHA256(mc)
	evaluation.Inputs["sourceObservation"] = check.Hash(o)
	policy.Issuers = []r.Issuer{{ID: "fixture", PublicKey: base64.StdEncoding.EncodeToString(pub)}}
	praw := engine.Encode(policy)
	q.Policy = writeFile("policy.json", praw)
	v := s.Verification{Version: s.Version, Kind: "SetupVerification", Purpose: "setup-verification", ID: "fixture-one", Issuer: "fixture", Scope: o.Scope, Inputs: evaluation.Inputs, ImplementationSHA256: engine.ImplementationSHA256, PolicySHA256: SHA256(praw), OutputPath: q.Out, WorkspaceSHA256: expected, IssuedAt: now.Add(-time.Second).Format(time.RFC3339Nano), ExpiresAt: now.Add(time.Minute).Format(time.RFC3339Nano), OperationalMetadata: true}
	if write {
		v.ConfigurationRoot = q.Root
		v.WorkspacePath = q.Workspace
	}
	v.CandidateSHA256 = SetupCandidateSHA256(v.Inputs, v.Scope, v.ID)
	v.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(key, SetupVerificationSigningBytes(v)))
	q.Verification = writeFile("verification.json", engine.Encode(v))
	trust := &setupFixtureTrust{used: map[string]bool{}}
	return New(nil).WithSetupTrust(trust), q, trust, v
}
func TestSetupSignedVerificationSelectionAndReplay(t *testing.T) {
	svc, q, trust, v := setupVerifyFixture(t, true)
	receipt, e := svc.VerifySetup(context.Background(), q)
	if e != nil || !receipt.Selected || receipt.Binding.Spec.Status != "verified" {
		t.Fatalf("selection %+v %v", receipt, e)
	}
	if receipt.Report.AuthorizationGranted || receipt.Report.ProductionActivated || receipt.Report.MigrationActivated {
		t.Fatal("authority laundering")
	}
	if _, e = engine.Resolve(context.Background(), q.Workspace, q.Root); e != nil {
		t.Fatal("selected workspace does not resolve", e)
	}
	if e = trust.Consume(context.Background(), v); e == nil {
		t.Fatal("replay")
	}
	if _, e = svc.VerifySetup(context.Background(), q); e == nil {
		t.Fatal("output reuse")
	}
}
func TestSetupProofCannotUseCallerPolicyWithoutProtectedTrust(t *testing.T) {
	_, q, _, _ := setupVerifyFixture(t, true)
	old, _ := os.ReadFile(q.Workspace)
	receipt, e := New(nil).VerifySetup(context.Background(), q)
	if e != nil || receipt.Selected || receipt.Binding.Spec.Status == "verified" || receipt.Report.Status != "blocked" {
		t.Fatalf("untrusted %+v %v", receipt, e)
	}
	after, _ := os.ReadFile(q.Workspace)
	if string(old) != string(after) {
		t.Fatal("workspace changed")
	}
}
func TestSetupSelectionConflictsAndRevocation(t *testing.T) {
	for _, mode := range []string{"manual", "locked", "revoked", "expired", "wrong-root", "changed-input"} {
		t.Run(mode, func(t *testing.T) {
			svc, q, trust, _ := setupVerifyFixture(t, true)
			switch mode {
			case "manual":
				os.WriteFile(q.Workspace, []byte(`{}`), 0600)
			case "locked":
				os.WriteFile(q.Root+"/.workkit-binding.lock", []byte("unresolved prior attempt"), 0600)
			case "revoked":
				trust.revoked = true
			case "expired":
				var v s.Verification
				b, _ := os.ReadFile(q.Verification)
				DecodeRead(b, &v)
				v.ExpiresAt = time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano)
				os.WriteFile(q.Verification, engine.Encode(v), 0600)
			case "wrong-root":
				q.ExpectedWorkspaceSHA256 = strings.Repeat("a", 64)
			case "changed-input":
				var d api.DesiredSetup
				b, _ := os.ReadFile(q.Desired)
				setupDocument(b, "DesiredSetup", &d)
				d.Spec.Fields = nil
				os.WriteFile(q.Desired, engine.Encode(d), 0600)
			}
			old, _ := os.ReadFile(q.Workspace)
			r, e := svc.VerifySetup(context.Background(), q)
			if e == nil && r.Selected {
				t.Fatal("gate bypass", mode)
			}
			after, _ := os.ReadFile(q.Workspace)
			if string(old) != string(after) {
				t.Fatal("failed selection changed workspace")
			}
		})
	}
}

func TestSetupSelectionInterruptedBeforeRenameRetainsHold(t *testing.T) {
	svc, q, trust, _ := setupVerifyFixture(t, true)
	trust.failAt = 3
	old, _ := os.ReadFile(q.Workspace)
	r, e := svc.VerifySetup(context.Background(), q)
	if e == nil || r.Selected {
		t.Fatal("interrupted selection succeeded")
	}
	after, _ := os.ReadFile(q.Workspace)
	if string(old) != string(after) {
		t.Fatal("old workspace lost")
	}
	if _, e = os.Stat(q.Root + "/.workkit-binding.lock"); e != nil {
		t.Fatal("uncertain lock released")
	}
	if _, e = engine.Resolve(context.Background(), q.Workspace, q.Root); e != nil {
		t.Fatal("preserved workspace invalid", e)
	}
}
func TestSetupCrossProcessAbandonedLock(t *testing.T) {
	if root := os.Getenv("WK_P4_SYNTHETIC_LOCK_ROOT"); root != "" {
		f, e := os.OpenFile(root+"/.workkit-binding.lock", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			t.Fatal(e)
		}
		f.Write([]byte("disposable abandoned operation"))
		f.Sync()
		f.Close()
		return
	}
	svc, q, _, _ := setupVerifyFixture(t, true)
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, exe, "-test.run=^TestSetupCrossProcessAbandonedLock$")
	child.Env = []string{"WK_P4_SYNTHETIC_LOCK_ROOT=" + q.Root}
	if out, e := child.CombinedOutput(); e != nil {
		t.Fatal(e, string(out))
	}
	old, _ := os.ReadFile(q.Workspace)
	if _, e = svc.VerifySetup(ctx, q); e == nil {
		t.Fatal("abandoned process lock bypassed")
	}
	after, _ := os.ReadFile(q.Workspace)
	if string(old) != string(after) {
		t.Fatal("locked workspace changed")
	}
}
func TestSetupClosedContractsAndUnsafeAssets(t *testing.T) {
	o, assets, _, _, _, _ := setupFixture(t, "github")
	raw := engine.Encode(o)
	for _, bad := range [][]byte{append([]byte(`{"version":"bad",`), raw[1:]...), []byte(`{"version":"workkit-setup/v2"}`), []byte(`{"version":"workkit-setup/v1","unknown":1}`)} {
		var got s.Observation
		if e := DecodeRead(bad, &got); e == nil && check.Validate(got) == nil {
			t.Fatal("malformed/unsupported observation accepted")
		}
	}
	root := readTestDir(t)
	if e := WriteSetupObservation(context.Background(), o, assets, root+"/observation"); e != nil {
		t.Fatal(e)
	}
	var changed s.Observation
	DecodeRead(mustSetupRead(t, root+"/observation/bundle.json"), &changed)
	changed.Assets[0].Path = "../outside"
	os.WriteFile(root+"/observation/bundle.json", engine.Encode(changed), 0600)
	if _, _, e := LoadSetupObservation(context.Background(), root+"/observation"); e == nil {
		t.Fatal("asset traversal")
	}
	if e := os.Symlink(root+"/observation", root+"/alias"); e == nil {
		if _, _, e := LoadSetupObservation(context.Background(), root+"/alias"); e == nil {
			t.Fatal("symlink accepted")
		}
	}
}
func mustSetupRead(t *testing.T, p string) []byte {
	t.Helper()
	b, e := os.ReadFile(p)
	if e != nil {
		t.Fatal(e)
	}
	return b
}

type setupProjectTransport struct{}

func (setupProjectTransport) Do(ctx context.Context, source readsource.Source, q readsource.Request) (readsource.Response, error) {
	bodies := map[string]string{
		"github-repository":     `{"node_id":"repo","name":"synthetic","owner":{"node_id":"tenant"}}`,
		"github-item":           `{"node_id":"item","number":1,"title":"Synthetic","body":null,"updated_at":"2026-10-26T00:00:00Z"}`,
		"github-projects":       `{"data":{"repository":{"id":"repo","projectsV2":{"nodes":[{"id":"project","title":"Synthetic project"}],"pageInfo":{"hasNextPage":false,"endCursor":null}}}}}`,
		"github-project-fields": `{"data":{"node":{"id":"project","fields":{"nodes":[{"__typename":"ProjectV2SingleSelectField","id":"field-priority","name":"Priority","options":[{"id":"low","name":"Low"}]},{"__typename":"ProjectV2Field","id":"field-number","name":"Budget","dataType":"NUMBER"},{"__typename":"ProjectV2Field","id":"field-text","name":"Text","dataType":"TEXT"}],"pageInfo":{"hasNextPage":false,"endCursor":null}}}}}`,
		"github-project-items":  `{"data":{"node":{"id":"project","items":{"nodes":[{"id":"project-item","content":{"id":"item","number":1}}],"pageInfo":{"hasNextPage":false,"endCursor":null}}}}}`,
		"github-item-fields":    `{"data":{"node":{"id":"project-item","fieldValues":{"nodes":[{"__typename":"ProjectV2ItemFieldSingleSelectValue","optionId":"low","field":{"id":"field-priority","name":"Priority"}},{"__typename":"ProjectV2ItemFieldNumberValue","number":9007199254740993.00000000000001,"field":{"id":"field-number","name":"Budget"}},{"__typename":"ProjectV2ItemFieldTextValue","text":null,"field":{"id":"field-text","name":"Text"}}],"pageInfo":{"hasNextPage":false,"endCursor":null}}}}}`}
	body, ok := bodies[q.Kind]
	if !ok {
		return readsource.Response{}, errors.New("unexpected typed provider request")
	}
	return readsource.Response{Status: 200, Body: []byte(body)}, ctx.Err()
}
func TestSetupNativeProjectsDiscoveryAndPreview(t *testing.T) {
	ctx := context.Background()
	source := basicReadSource()
	source.Scope.Projects = true
	capture, e := readsource.CaptureSource(ctx, source, "capture", nil, setupProjectTransport{})
	if e != nil {
		t.Fatal(e)
	}
	pageFiles := map[string][]byte{}
	for _, page := range capture.Snapshot.Pages {
		pageFiles[page.Asset.Path] = capture.Files[page.Asset.Path]
	}
	capture, e = readsource.ReconstructBytes(capture.Files["manifest.json"], pageFiles)
	if e != nil {
		t.Fatal(e)
	}
	config := readConfig(t, "native-project", source)
	transport := r.TransportIdentity{Kind: "captured-source", Version: "unverified", Principal: source.PrincipalPseudonym, AuthMode: source.Auth.Mode}
	_, files, e := buildReadBundle(config, capture, transport, source.ReadPolicySHA256, "")
	if e != nil {
		t.Fatal(e)
	}
	root := readTestDir(t)
	bundle := root + "/capture"
	if e = engine.WriteBundle(ctx, bundle, files, ""); e != nil {
		t.Fatal(e)
	}
	meta := root + "/metadata"
	if _, e = New(nil).DiscoverSetup(ctx, bundle, meta); e != nil {
		t.Fatal(e, errors.Unwrap(e))
	}
	o, _, e := LoadSetupObservation(ctx, meta)
	if e != nil {
		t.Fatal(e)
	}
	field, ok := check.Find(o, "field", "field-priority")
	if !ok || field.Type != "enum" || field.ParentID != "project" {
		t.Fatalf("native field projection %+v", field)
	}
	mapping := api.Mapping{APIVersion: api.Version, Kind: "Mapping", Metadata: api.Metadata{Id: "native-mapping", Revision: 1}, Spec: api.MappingSpec{Fields: []api.MappingSpecFieldsItem{}, Lifecycle: []api.MappingSpecLifecycleItem{}, PersonMapping: []api.MappingSpecPersonMappingItem{}}}
	for _, id := range []string{"field-priority", "field-number", "field-text"} {
		mapping.Spec.Fields = append(mapping.Spec.Fields, api.MappingSpecFieldsItem{Canonical: id, SourceFieldId: id, TargetFieldId: id, Owner: "source", LossPolicy: "reject", ValueMap: map[string]json.RawMessage{}})
	}
	run := func(m api.Mapping, out string) s.Preview {
		t.Helper()
		raw := engine.Encode(m)
		mp := root + "/mapping.json"
		os.WriteFile(mp, raw, 0600)
		mc := s.MappingContext{Version: s.Version, Kind: "MappingContext", MappingSHA256: SHA256(raw), SourceSHA256: check.Hash(o), DestinationSHA256: check.Hash(o), SourceContainerID: "project", DestinationContainerID: "project", RequiredFields: []string{"field-priority"}}
		cp := root + "/context.json"
		os.WriteFile(cp, engine.Encode(mc), 0600)
		preview, e := PreviewSetupMapping(ctx, mp, cp, meta, meta, bundle, time.Now().UTC().Format(time.RFC3339Nano), root+"/"+out)
		if e != nil {
			t.Fatal(e)
		}
		return preview
	}
	p := run(mapping, "preview")
	foundNumber, foundEnum, foundNull := false, false, false
	for _, v := range p.Values {
		if v.Canonical == "field-number" && string(v.Value) == "9007199254740993.00000000000001" && v.State == "proposed" {
			foundNumber = true
		}
		if v.Canonical == "field-priority" && string(v.Value) == `"low"` && v.State == "proposed" && len(v.SourcePointers) > 0 {
			foundEnum = true
		}
		if v.Canonical == "field-text" && string(v.Value) == "null" {
			foundNull = true
		}
	}
	if !foundNumber || !foundEnum || !foundNull {
		t.Fatalf("project values lost %+v", p)
	}
	mapping.Spec.Fields[0].Owner = "runtime"
	p = run(mapping, "protected")
	if p.Report.Status != "blocked" {
		t.Fatal("runtime-owned target proposed")
	}
	protected := false
	for _, v := range p.Values {
		if v.Canonical == "field-priority" && v.State == "protected" {
			protected = true
		}
	}
	if !protected {
		t.Fatal("ownership missing")
	}
}

type setupJiraMetadataTransport struct{}

func (setupJiraMetadataTransport) Do(ctx context.Context, source readsource.Source, q readsource.Request) (readsource.Response, error) {
	var body []byte
	switch q.Kind {
	case "jira-project":
		body = engine.Encode(map[string]any{"id": source.ContainerID, "key": source.ContainerKey, "name": "Synthetic project"})
	case "jira-types":
		body = []byte(`[{"id":"100","name":"Task","subtask":false}]`)
	case "jira-fields":
		body = []byte(`{"startAt":0,"maxResults":50,"total":1,"isLast":true,"values":[{"id":"summary","name":"Summary","schema":{"type":"string"}}]}`)
	default:
		return readsource.Response{}, errors.New("unexpected Jira metadata request")
	}
	return readsource.Response{Status: 200, Body: body}, ctx.Err()
}
func TestSetupNativeJiraSchemaProvenance(t *testing.T) {
	ctx := context.Background()
	raw, e := os.ReadFile("../../../internal/tracker/testdata/offline/jira/manifest.json")
	if e != nil {
		t.Fatal(e)
	}
	var sourceManifest readsource.Manifest
	if e = readsource.Decode(raw, "manifest", &sourceManifest); e != nil {
		t.Fatal(e)
	}
	source := sourceManifest.Source
	capture, e := readsource.CaptureSource(ctx, source, "discover", nil, setupJiraMetadataTransport{})
	if e != nil {
		t.Fatal(e)
	}
	pages := map[string][]byte{}
	for _, page := range capture.Snapshot.Pages {
		pages[page.Asset.Path] = capture.Files[page.Asset.Path]
	}
	capture, e = readsource.ReconstructBytes(capture.Files["manifest.json"], pages)
	if e != nil {
		t.Fatal(e)
	}
	config := readConfig(t, "native-jira", source)
	_, files, e := buildReadBundle(config, capture, r.TransportIdentity{Kind: "captured-source", Version: "unverified", Principal: source.PrincipalPseudonym, AuthMode: source.Auth.Mode}, source.ReadPolicySHA256, "")
	if e != nil {
		t.Fatal(e)
	}
	root := readTestDir(t)
	if e = engine.WriteBundle(ctx, root+"/capture", files, ""); e != nil {
		t.Fatal(e)
	}
	if _, e = New(nil).DiscoverSetup(ctx, root+"/capture", root+"/metadata"); e != nil {
		t.Fatal(e, errors.Unwrap(e))
	}
	o, _, e := LoadSetupObservation(ctx, root+"/metadata")
	if e != nil {
		t.Fatal(e)
	}
	field, ok := check.Find(o, "field", "summary")
	if !ok || field.Type != "string" || !strings.HasSuffix(field.Provenance.Fields["nativeType"], "/schema/type") {
		t.Fatal("native schema type/provenance missing", field)
	}
}

type setupNoMetadata struct{}

func (setupNoMetadata) ReadSetupMetadata(context.Context, MetadataRequest) (s.Observation, map[string][]byte, error) {
	return s.Observation{}, nil, errors.New("fixture not invoked")
}
func TestSetupFluentBackendPreservesProtectedPorts(t *testing.T) {
	trust := &setupFixtureTrust{}
	svc := New(nil).WithSetupTrust(trust).WithSetupMetadataReader(setupNoMetadata{}).WithReadBackend(&readFixtureBackend{})
	if svc.setupTrust != trust || svc.setupReader == nil || svc.reads == nil {
		t.Fatal("fluent setup dependency lost")
	}
}

func TestSetupNonStringValueMapsAreUnsupported(t *testing.T) {
	o, _, _, _, raw, _ := setupFixture(t, "github")
	var m api.Mapping
	setupDocument(raw, "Mapping", &m)
	for i := range o.Entities {
		if o.Entities[i].ID == "field-title" {
			o.Entities[i].Type = "number"
			o.Entities[i].NativeType = "number"
		}
	}
	m.Spec.Fields[0].ValueMap = map[string]json.RawMessage{"": json.RawMessage(`0`)}
	raw = engine.Encode(m)
	mc := engine.Encode(s.MappingContext{Version: s.Version, Kind: "MappingContext", MappingSHA256: SHA256(raw), SourceSHA256: check.Hash(o), DestinationSHA256: check.Hash(o), RequiredFields: []string{"title"}})
	r, e := EvaluateMapping(raw, mc, o, o, setupAt)
	if e != nil || r.Status != "blocked" {
		t.Fatal("numeric value map accepted", r, e)
	}
}
func TestSetupCapturedViewURLPolicy(t *testing.T) {
	scope := s.Observation{Scope: s.Scope{Provider: "jira-datacenter", Origin: "https://jira.synthetic.invalid", BasePath: "/jira"}}
	if !validSetupURL("https://jira.synthetic.invalid/jira/views/42", scope) {
		t.Fatal("scoped view URL blocked")
	}
	for _, url := range []string{"http://jira.synthetic.invalid/jira/v", "https://evil.invalid/jira/v", "https://user:password@jira.synthetic.invalid/jira/v", "https://jira.synthetic.invalid/jira/v?token=canary", "https://jira.synthetic.invalid/outside/v", "https://jira.synthetic.invalid/jira/../outside", "https://jira.synthetic.invalid/jira/%2e%2e/v"} {
		if validSetupURL(url, scope) {
			t.Fatal("unsafe view URL accepted")
		}
	}
}

func TestSetupSignatureCannotSelectCopiedWorkspace(t *testing.T) {
	svc, q, trust, _ := setupVerifyFixture(t, true)
	copyRoot := readTestDir(t)
	for _, dir := range []string{"resources", "assets"} {
		if e := os.Mkdir(copyRoot+"/"+dir, 0700); e != nil {
			t.Fatal(e)
		}
	}
	for _, rel := range []string{"resources/profile.json", "resources/mapping.json", "workspace.json"} {
		if e := os.WriteFile(copyRoot+"/"+rel, mustSetupRead(t, q.Root+"/"+rel), 0600); e != nil {
			t.Fatal(e)
		}
	}
	old := mustSetupRead(t, q.Workspace)
	q.Root = copyRoot
	q.Workspace = copyRoot + "/workspace.json"
	receipt, e := svc.VerifySetup(context.Background(), q)
	if e != nil || receipt.Selected || receipt.Report.Status != "blocked" {
		t.Fatal("copied workspace selected", receipt, e)
	}
	if string(old) != string(mustSetupRead(t, q.Workspace)) {
		t.Fatal("copied namespace changed")
	}
	if len(trust.used) != 0 {
		t.Fatal("wrong namespace consumed proof")
	}
}
