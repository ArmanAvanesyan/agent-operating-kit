package workkit

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/operations"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/worksystem/provision"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/writeback"
	o "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/operationsv1"
	r "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/readv1"
	s "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/setupv1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/sdk"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type wkWitness struct {
	mu    sync.Mutex
	epoch operations.Epoch
	used  map[string]bool
	fail  bool
}

func (w *wkWitness) Current(context.Context, string) (operations.Epoch, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.fail {
		return operations.Epoch{}, errors.New("external witness unavailable")
	}
	return w.epoch, nil
}
func (w *wkWitness) Consume(_ context.Context, c operations.Consumption) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.fail || w.used[c.LogicalID] || c.Boundary != "provider-send" || c.Epoch != w.epoch {
		return errors.New("spent or unsupported")
	}
	w.used[c.LogicalID] = true
	return nil
}

type wkWitnessVerifier struct{}

func (wkWitnessVerifier) VerifyWitness(context.Context, operations.AuthorityWitness, string, string, []byte, time.Time) error {
	return nil
}
func wkActivation(t *testing.T, store o.Store) (*operations.Activation, *wkWitness) {
	t.Helper()
	now := time.Now().UTC()
	w := &wkWitness{epoch: operations.Epoch{Authority: "synthetic-external", Holder: store.Owner, ScopeSHA256: MutationNamespaceSHA256(store), Generation: 1, ObservedAt: now.Add(-time.Second), ValidUntil: now.Add(time.Minute)}, used: map[string]bool{}}
	a, e := operations.Bind(context.Background(), w, wkWitnessVerifier{}, w.epoch.Authority, w.epoch.Holder, w.epoch.ScopeSHA256, []byte("synthetic-external-proof"), nil)
	if e != nil {
		t.Fatal(e)
	}
	return a, w
}

type wkProvisionJournal struct{ consumed, recorded int }

func (j *wkProvisionJournal) Consume(context.Context, string, string) error { j.consumed++; return nil }
func (j *wkProvisionJournal) Record(context.Context, string, string) error  { j.recorded++; return nil }
func TestWorkkitProvisioningUsesExistingActivationVocabulary(t *testing.T) {
	store := o.Store{Version: o.Version, Kind: "IntegrationJournal", ID: "synthetic-store", Path: "/synthetic/journal", Namespace: "new-synthetic", Owner: "workkit"}
	a, w := wkActivation(t, store)
	j := &wkProvisionJournal{}
	b := workkitProvisionBoundary{provision.ActivatedBoundary{Journal: j, Activation: a, RouteSHA256: MutationNamespaceSHA256(store)}}
	digest := sdk.SHA256([]byte("exact-input"))
	if e := b.Consume(context.Background(), "field-create", digest); e != nil {
		t.Fatal(e)
	}
	if j.consumed != 1 || !w.used["workkit-provision:field-create"] {
		t.Fatal("missing actual witness consumption")
	}
	if e := b.Consume(context.Background(), "field-create", digest); e == nil {
		t.Fatal("restored/replayed provisioning resend")
	}
	if j.consumed != 1 {
		t.Fatal("duplicate local consumption")
	}
	w.mu.Lock()
	w.fail = true
	w.mu.Unlock()
	if e := b.Consume(context.Background(), "other", digest); e == nil || j.consumed != 1 {
		t.Fatal("witness failure bypass")
	}
	for _, invalid := range []string{"provider-create", "sync-control", "sync-run-admission", "provisioning-send"} {
		if e := a.Consume(context.Background(), "invalid", invalid, digest); e == nil {
			t.Fatal("changed v1 interpretation", invalid)
		}
	}
}
func TestWorkkitStandardBootstrapAndReadOnlyInspection(t *testing.T) {
	ctx := context.Background()
	if _, e := (OperationBackend{}).OpenOperation(ctx, sdk.PreparedOperation{}, nil, nil); e == nil {
		t.Fatal("standard binary installed writer")
	}
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	j, e := writeback.OpenWorkkit(ctx, filepath.Join(root, "journal.db"), "new-disposable", "workkit", true)
	if e != nil {
		t.Fatal(e)
	}
	store := PublicOperationStore(j.Identity)
	j.Close()
	rows, e := (OperationBackend{}).InspectOperations(ctx, store, "")
	if e != nil || len(rows) != 0 {
		t.Fatal(rows, e)
	}
	store.ID = "forged"
	if _, e = (OperationBackend{}).InspectOperations(ctx, store, ""); e == nil {
		t.Fatal("forged store")
	}
}

func TestWorkkitSyncRealJournalActivationAndCrashLineage(t *testing.T) {
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	planRoot := filepath.Join(root, "fixture-plan")
	cmd := exec.Command("go", "test", "./kits/workkit/sdk", "-run", "^TestSyncPinnedPreviewInitializationAndReconstruction$", "-count=1")
	cmd.Dir = "../../.."
	cmd.Env = append(os.Environ(), "AOK_TEST_WORKKIT_FRESH_FIXTURE=1", "AOK_TEST_WORKKIT_SYNC_EXPORT="+planRoot, "TMPDIR="+root)
	if out, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("synthetic fixture generator %v: %s", e, out)
	}
	ctx := context.Background()
	p, e := sdk.PrepareSync(ctx, planRoot)
	if e != nil {
		t.Fatal(e)
	}
	files := p.Assets()
	var source, dest s.Observation
	if sdk.DecodeRead(files["assets/source-observation/bundle.json"], &source) != nil || sdk.DecodeRead(files["assets/destination-observation/bundle.json"], &dest) != nil {
		t.Fatal("scopes")
	}
	j, e := writeback.OpenWorkkit(ctx, filepath.Join(root, "journal.db"), "new-synthetic", "workkit", true)
	if e != nil {
		t.Fatal(e)
	}
	store := PublicOperationStore(j.Identity)
	j.Close()
	activation, witness := wkActivation(t, store)
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	now := time.Now().UTC()
	policy := o.Policy{Version: o.Version, Kind: "OperationPolicy", NotBefore: now.Add(-time.Minute).Format(time.RFC3339Nano), ExpiresAt: now.Add(time.Minute).Format(time.RFC3339Nano), Issuers: []r.Issuer{{ID: "synthetic-authority", PublicKey: base64.StdEncoding.EncodeToString(pub)}}, RevokedIDs: []string{}, AllowedScopes: []s.Scope{source.Scope, dest.Scope}, MaxOperations: 1, AllowedActions: []string{"sync-initialize", "sync-run", "sync-reconcile", "sync-pause", "sync-resume"}}
	policyRaw := mustJSON(policy)
	enroll := func(plan sdk.PreparedSync, id, action string) o.Enrollment {
		en := o.Enrollment{Version: o.Version, Kind: "OperationEnrollment", Purpose: "sync-action", ID: id, Issuer: "synthetic-authority", OperationSHA256: plan.Digest(), Store: store, Action: action, PolicySHA256: sdk.SHA256(policyRaw), ImplementationSHA256: sdk.OperationsImplementationSHA256(), IssuedAt: now.Add(-time.Second).Format(time.RFC3339Nano), ExpiresAt: now.Add(time.Minute).Format(time.RFC3339Nano), OutputPath: filepath.Join(root, id)}
		en.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(key, sdk.SyncEnrollmentSigningBytes(en)))
		return en
	}
	backend := SyncBackend{Bootstrap: func(context.Context, sdk.PreparedSync, o.Enrollment) (SyncRouteAuthority, error) {
		return SyncRouteAuthority{Activation: activation, Current: func(context.Context, sdk.PreparedSync, o.Enrollment, []byte) error { return nil }}, nil
	}}
	en := enroll(p, "initialize", "sync-initialize")
	prior, e := backend.CurrentSync(ctx, p, en, policyRaw)
	if e != nil {
		t.Fatal(e)
	}
	next, e := sdk.ProjectSyncState(p, prior, en.Action, nil)
	if e != nil {
		t.Fatal(e)
	}
	if e = backend.CommitSync(ctx, p, en, policyRaw, next, nil); e != nil {
		t.Fatal("real initialization", e)
	}
	if !witness.used["initialize:settle"] {
		t.Fatal("external witness was skipped")
	}
	if e = backend.CommitSync(ctx, p, en, policyRaw, next, nil); e == nil {
		t.Fatal("replayed initialization")
	}
	stateFile := filepath.Join(root, "state.json")
	if e = os.WriteFile(stateFile, mustJSON(next), 0600); e != nil {
		t.Fatal(e)
	}
	asset := func(n string) string { return filepath.Join(planRoot, "assets", n) }
	readyRoot := filepath.Join(root, "ready")
	plan, e := sdk.PreviewSync(ctx, sdk.SyncRequest{Configuration: asset("configuration.json"), State: stateFile, Source: asset("source"), Destination: asset("destination"), SourceObservation: asset("source-observation"), DestinationObservation: asset("destination-observation"), Mapping: asset("mapping.json"), MappingContext: asset("mapping-context.json"), SourceBinding: asset("source-binding.json"), DestinationBinding: asset("destination-binding.json"), Query: asset("query.json"), AsOf: time.Now().UTC().Format(time.RFC3339Nano), Out: readyRoot})
	if e != nil || plan.Status != "ready" {
		t.Fatal(plan.Status, e)
	}
	ready, e := sdk.PrepareSync(ctx, readyRoot)
	if e != nil {
		t.Fatal(e)
	}
	run := enroll(ready, "run", "sync-run")
	pending, e := backend.BeginSync(ctx, ready, run, policyRaw)
	if e != nil {
		t.Fatal("actual run admission", e)
	}
	if pending.PendingPlanSHA256 != ready.Digest() || !witness.used["run:admit"] {
		t.Fatal("lost durable admission")
	}
	if _, e = backend.BeginSync(ctx, ready, run, policyRaw); e == nil {
		t.Fatal("unchanged run retried")
	}
	// Simulated caller loss after admission: reopen, preserve pending lineage, then
	// settle through a fresh read-only purpose; no provider transport is installed.
	stored, e := backend.InspectSync(ctx, store, ready.Plan().RouteSHA256)
	if e != nil || stored.PendingPlanSHA256 != ready.Digest() {
		t.Fatal(stored, e)
	}
	reconcile := enroll(ready, "reconcile", "sync-reconcile")
	settled, e := sdk.ProjectSyncState(ready, stored, reconcile.Action, nil)
	if e != nil {
		t.Fatal(e)
	}
	if e = backend.CommitSync(ctx, ready, reconcile, policyRaw, settled, nil); e != nil {
		t.Fatal("read-only settlement", e)
	}
	if settled.PendingPlanSHA256 != "" || settled.CompletedWatermark != ready.Digest() {
		t.Fatal("incomplete settlement")
	}
}

type wkDelayedCreator struct {
	guard   func(context.Context) error
	resolve func()
	sent    *bool
	t       *testing.T
}

func (c wkDelayedCreator) Create(ctx context.Context, _ writeback.CreationPermit) (writeback.CreateResponse, error) {
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) > 30*time.Second {
		c.t.Fatal("create request deadline missing")
	}
	c.resolve()
	if err := c.guard(ctx); err != nil {
		return writeback.CreateResponse{}, err
	}
	*c.sent = true
	return writeback.CreateResponse{}, nil
}
func TestWorkkitCreateFinalGuardAfterCredentialResolution(t *testing.T) {
	allowed := true
	sent := false
	checks := 0
	guard := func(context.Context) error {
		checks++
		if !allowed {
			return errors.New("current create grant revoked/expired")
		}
		return nil
	}
	factory := func(g func(context.Context) error) (writeback.Creator, error) {
		return wkDelayedCreator{guard: g, resolve: func() { allowed = false }, sent: &sent, t: t}, nil
	}
	inner, e := factory(guard)
	if e != nil {
		t.Fatal(e)
	}
	outer := currentCreator{inner: inner, check: guard}
	if _, e = outer.Create(context.Background(), writeback.CreationPermit{}); e == nil || sent || checks != 2 {
		t.Fatal("delayed creator bypass", e, sent, checks)
	}
}

type wkDelayedProvisionWriter struct {
	guard   func(context.Context) error
	resolve func()
	sent    *bool
}

func (w wkDelayedProvisionWriter) Observe(context.Context, provision.Scope) (provision.Observation, error) {
	return provision.Observation{}, nil
}
func (w wkDelayedProvisionWriter) Readback(context.Context, provision.Scope, provision.Change) (provision.Resource, error) {
	return provision.Resource{}, nil
}
func (w wkDelayedProvisionWriter) Send(ctx context.Context, _ provision.Scope, _ provision.Change, _ string) error {
	w.resolve()
	if err := w.guard(ctx); err != nil {
		return err
	}
	*w.sent = true
	return nil
}
func TestWorkkitProvisionFinalGuardAfterCredentialResolution(t *testing.T) {
	allowed, sent := true, false
	checks := 0
	guard := func(context.Context) error {
		checks++
		if !allowed {
			return errors.New("current authority revoked")
		}
		return nil
	}
	inner := wkDelayedProvisionWriter{guard: guard, resolve: func() { allowed = false }, sent: &sent}
	writer := guardedProvisionWriter{inner: inner, guard: guard}
	if e := writer.Send(context.Background(), provision.Scope{}, provision.Change{}, "revision"); e == nil || sent || checks != 2 {
		t.Fatal("delayed setup send bypass", e, sent, checks)
	}
}
