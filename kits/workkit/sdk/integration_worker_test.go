package sdk

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	i "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/integrationv1"
	z "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/syncv2"
	api "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/v1alpha1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/engine"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type p7WorkerBackend struct {
	mu                     sync.Mutex
	plan                   PreparedSyncV2
	claims, records, sends int
	fence                  int64
	until                  time.Time
	revoked                bool
}

func (b *p7WorkerBackend) Current(ctx context.Context, _ i.Policy, _ i.Enrollment) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.revoked {
		return errors.New("revoked fixture authority")
	}
	return ctx.Err()
}
func (b *p7WorkerBackend) Claim(_ context.Context, _ i.Enrollment, _ string, expected int64) (int64, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.fence == 0 {
		b.fence = 1
	} else if expected != b.fence || time.Now().After(b.until) {
		return 0, errors.New("another/expired owner")
	}
	b.claims++
	b.until = time.Now().Add(30 * time.Second)
	return b.fence, nil
}
func (b *p7WorkerBackend) Check(ctx context.Context, _ i.Enrollment, _ string, fence int64) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.revoked || b.fence != fence || time.Now().After(b.until) {
		return errors.New("stale fence")
	}
	return ctx.Err()
}
func (b *p7WorkerBackend) Poll(ctx context.Context, _ i.Enrollment, _ string, guard func(context.Context) error) (PreparedSyncV2, error) {
	if e := guard(ctx); e != nil {
		return PreparedSyncV2{}, e
	}
	return b.plan, nil
}
func (b *p7WorkerBackend) Record(context.Context, i.Enrollment, PreparedSyncV2) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.records++
	return nil
}
func (b *p7WorkerBackend) Ingest(context.Context, i.Policy, i.Enrollment, i.EventEnvelope) (i.Report, error) {
	return i.Report{}, errors.New("not qualified")
}
func (b *p7WorkerBackend) Inspect(_ context.Context, e i.Enrollment, route string) (i.Report, error) {
	r := integrationReport("IntegrationStatus", e.Store)
	r.RouteID = route
	r.Fence = e.Fence
	return r, nil
}
func (b *p7WorkerBackend) Events(_ context.Context, e i.Enrollment, _ string, _ int64, _ bool) (i.Report, error) {
	r := integrationReport("NotificationReport", e.Store)
	r.Fence = e.Fence
	r.RouteID = e.RouteID
	return r, nil
}
func (b *p7WorkerBackend) State(context.Context, i.Enrollment, PreparedSyncV2) (z.State, error) {
	return z.State{}, errors.New("not qualified")
}
func (b *p7WorkerBackend) Commit(context.Context, i.Enrollment, PreparedSyncV2, z.State, []string, string) error {
	return errors.New("not qualified")
}
func (b *p7WorkerBackend) OpenOperation(context.Context, PreparedOperation, []byte, []byte) (*OperationSession, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sends++
	return nil, errors.New("not qualified")
}
func p7workerFixture(t *testing.T, socket string) (*Service, *p7WorkerBackend, string, string, string, string, i.Policy, i.Enrollment, ed25519.PrivateKey) {
	t.Helper()
	t.Setenv("AOK_TEST_P7_FRESH", "1")
	q, _ := p7fixture(t, "base", "base", "shared", true)
	if _, e := PreviewSyncV2(context.Background(), q); e != nil {
		t.Fatal(e)
	}
	prepared, e := PrepareSyncV2(context.Background(), q.Out)
	if e != nil {
		t.Fatal(e)
	}
	root := readTestDir(t)
	workspace := p7write(t, root, "workspace.json", api.Workspace{APIVersion: api.Version, Kind: "Workspace", Metadata: api.Metadata{Id: "fixture", Revision: 1}, Spec: api.WorkspaceSpec{Connections: []api.Ref{}, Bindings: []api.Ref{}, SyncRoutes: []api.Ref{}, NativeProfiles: []api.Ref{}}})
	ws, _ := os.ReadFile(workspace)
	p, en, key, _ := p7authority(t, SHA256([]byte("pending")), "worker-start")
	p.Routes = map[string]string{prepared.plan.RouteID: prepared.plan.ConfigurationSHA256}
	p.AllowedPurposes = []string{"worker-start", "local-inspect", "events-read", "events-ack", "submit-batch"}
	p.Limits.ProcessSeconds = 120
	config := i.Configuration{Version: i.Version, Kind: "IntegrationConfiguration", Store: en.Store, WorkspaceSHA256: SHA256(ws), Listen: socket, Routes: p.Routes, Limits: p.Limits}
	configPath := p7write(t, root, "worker.json", config)
	configRaw, _ := os.ReadFile(configPath)
	policy := p7write(t, root, "policy.json", p)
	praw, _ := os.ReadFile(policy)
	en.SubjectSHA256 = SHA256(configRaw)
	en.PolicySHA256 = SHA256(praw)
	en.RouteID = ""
	en.Fence = 0
	en.ProcessID = os.Getpid()
	en.ProcessStart, e = CurrentProcessStart()
	if e != nil {
		t.Fatal(e)
	}
	en.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(key, IntegrationSigningBytes(en)))
	enrollment := p7write(t, root, "enrollment.json", en)
	backend := &p7WorkerBackend{plan: prepared}
	return New(nil).WithIntegrationBackend(backend), backend, workspace, configPath, policy, enrollment, p, en, key
}
func TestP7ReviewFirstWorkerNoImplicitSends(t *testing.T) {
	socket := filepath.Join(readTestDir(t), "control.sock")
	svc, backend, workspace, config, policy, enrollment, _, _, _ := p7workerFixture(t, socket)
	worker, e := svc.newIntegrationWorker(context.Background(), workspace, config, policy, enrollment)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = worker.cycle(context.Background()); e != nil {
		t.Fatal(e)
	}
	if e = worker.heartbeat(context.Background()); e != nil {
		t.Fatal(e)
	}
	backend.mu.Lock()
	records, sends, claims := backend.records, backend.sends, backend.claims
	backend.mu.Unlock()
	if records != 1 || sends != 0 || claims != 2 {
		t.Fatal(records, sends, claims)
	}
	other, e := svc.newIntegrationWorker(context.Background(), workspace, config, policy, enrollment)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = other.cycle(context.Background()); e == nil {
		t.Fatal("duplicate process enrollment took route")
	}
	backend.mu.Lock()
	backend.revoked = true
	backend.mu.Unlock()
	if e = worker.heartbeat(context.Background()); e == nil {
		t.Fatal("revocation ignored")
	}
}
func TestP7IntegrationLimitsAndScopeAreNotAuthority(t *testing.T) {
	if _, _, e := New(nil).integrationAuthority(context.Background(), nil, nil, SHA256([]byte("x")), "operational-read"); e == nil {
		t.Fatal("ordinary binary installed read authority")
	}
	if NativeMutationUnavailable() == nil {
		t.Fatal("native mutation enabled")
	}
	q, _ := p7fixture(t, "left", "base", "shared", true)
	p, e := PreviewSyncV2(context.Background(), q)
	if e != nil {
		t.Fatal(e)
	}
	prepared, e := PrepareSyncV2(context.Background(), q.Out)
	if e != nil {
		t.Fatal(e)
	}
	at, _ := nativeTime(p.AsOf)
	if ValidateSyncV2Fresh(prepared, at.Add(5*time.Minute)) == nil {
		t.Fatal("old source/target capture renewed by plan")
	}
	raw, _ := os.ReadFile(q.State)
	var state z.State
	DecodeRead(raw, &state)
	state.ConfigurationSHA256 = SHA256([]byte("changed"))
	os.WriteFile(q.State, engine.Encode(state), 0600)
	q.Out = ""
	if _, e = PreviewSyncV2(context.Background(), q); e == nil {
		t.Fatal("route amendment dropped old state")
	}
}

func TestP7ExecuteRejectsPolicyRouteRevisionBeforeState(t *testing.T) {
	svc, b, _, _, policy, _, p, en, key := p7workerFixture(t, filepath.Join(readTestDir(t), "control.sock"))
	p.Routes[b.plan.plan.RouteID] = SHA256([]byte("other-revision"))
	p.AllowedPurposes = append(p.AllowedPurposes, "sync-pause")
	os.WriteFile(policy, engine.Encode(p), 0600)
	en.RouteID = b.plan.plan.RouteID
	en.Fence = 1
	en.Purpose = "sync-pause"
	en.SubjectSHA256 = b.plan.Digest()
	en.PolicySHA256 = SHA256(engine.Encode(p))
	en.OutputPath = filepath.Join(readTestDir(t), "out")
	en.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(key, IntegrationSigningBytes(en)))
	enrol := p7write(t, readTestDir(t), "pause.json", en)
	planRoot := filepath.Join(readTestDir(t), "plan")
	if e := writeOperationalReport(context.Background(), planRoot, z.Version, b.plan.plan, b.plan.files); e != nil {
		t.Fatal(e)
	}
	if _, e := PrepareSyncV2(context.Background(), planRoot); e != nil {
		t.Fatal(e)
	}
	_, err := svc.ExecuteSyncV2(context.Background(), ExecuteSyncV2Request{Plan: planRoot, Policy: policy, Enrollment: enrol, Out: en.OutputPath, Action: "sync-pause"})
	if err == nil {
		t.Fatal("policy route revision accepted")
	}
	if _, e := os.Lstat(en.OutputPath); !os.IsNotExist(e) {
		t.Fatal("output reserved before scope rejection", e)
	}
}
