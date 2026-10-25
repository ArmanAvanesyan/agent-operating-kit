package sdk

import (
	"context"
	"encoding/json"
	a "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/accessv1"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestForgedAuthReceiptCannotRetainAuthority(t *testing.T) {
	raw := accessConfig(t)
	f := accessInputs(t, raw, "auth login")
	r, e := New(nil).WithAccessBackend(&authFixture{}).RunAccess(context.Background(), "auth login", raw, f.policy, f.signed(), f.out)
	if e != nil {
		t.Fatal(e)
	}
	r.Binding.AuthorizationGranted = true
	r.Binding.ExecutionReady = true
	b, e := InspectAuth(raw, accessJSON(r), time.Now())
	if e == nil || b.AuthorizationGranted || b.ExecutionReady {
		t.Fatal("forged authority escaped")
	}
}
func TestHistoricalQualificationAndEarlyErrorsStayUnverified(t *testing.T) {
	raw := accessConfig(t)
	f := accessInputs(t, raw, "auth login")
	r, e := New(nil).WithAccessBackend(&authFixture{}).RunAccess(context.Background(), "auth login", raw, f.policy, f.signed(), f.out)
	if e != nil {
		t.Fatal(e)
	}
	r.Binding.LiveQualification = "qualified"
	if b, e := InspectAuth(raw, accessJSON(r), time.Now()); e == nil || b.LiveQualification == "qualified" {
		t.Fatal("qualification laundered")
	}
	r.Binding.LiveQualification = "pending"
	r.Binding.ScopedReadVerified = true
	r.Binding.ObservedAt = "invalid"
	b, e := InspectAuth(raw, accessJSON(r), time.Now())
	if e == nil || b.ScopedReadVerified || b.AuthorizationGranted || b.ExecutionReady || b.Status != "unknown" {
		t.Fatal("early error retained claims")
	}
}
func TestConnectionRegistryVersionsManualConflictAndIdentity(t *testing.T) {
	ctx := context.Background()
	config := accessConfig(t)
	var c a.Configuration
	json.Unmarshal(config, &c)
	root := readTestDir(t)
	one, e := ChangeConnections(ctx, nil, config, "", c.ConnectionID, "add", "2026-10-26T00:00:00Z", filepath.Join(root, "one"))
	if e != nil {
		t.Fatal(e)
	}
	raw := accessJSON(one)
	two, e := ChangeConnections(ctx, raw, nil, SHA256(raw), c.ConnectionID, "remove", "2026-10-26T00:01:00Z", filepath.Join(root, "two"))
	if e != nil || len(two.Connections) != 0 || two.MigrationActivated || two.ProductionActivated {
		t.Fatal(e)
	}
	if _, e = ChangeConnections(ctx, append(raw, ' '), nil, SHA256(raw), c.ConnectionID, "remove", "2026-10-26T00:02:00Z", filepath.Join(root, "bad")); e == nil {
		t.Fatal("manual change ignored")
	}
	one.Connections[0].ID = "wrong"
	bad := accessJSON(one)
	if _, e = ChangeConnections(ctx, bad, nil, SHA256(bad), "wrong", "remove", "2026-10-26T00:02:00Z", filepath.Join(root, "mismatch")); e == nil {
		t.Fatal("wrong registry identity")
	}
}
func TestAccessStateAndUncertainEvidenceKeepLock(t *testing.T) {
	config := accessConfig(t)
	f := accessInputs(t, config, "auth login")
	var p a.Policy
	json.Unmarshal(f.policy, &p)
	backend := &authFixture{entered: make(chan bool, 1), release: make(chan bool)}
	s := New(nil).WithAccessBackend(backend)
	done := make(chan error, 1)
	go func() {
		_, e := s.RunAccess(context.Background(), "auth login", config, f.policy, f.signed(), f.out)
		done <- e
	}()
	<-backend.entered
	// Force create-only completion failure after a successful external operation.
	if e := os.WriteFile(filepath.Join(f.out, "bundle.json"), []byte("occupied"), 0600); e != nil {
		t.Fatal(e)
	}
	close(backend.release)
	if e := <-done; e == nil {
		t.Fatal("evidence overwrite accepted")
	}
	entries, _ := os.ReadDir(p.LedgerRoot)
	locked := false
	for _, entry := range entries {
		if len(entry.Name()) > 11 && entry.Name()[:11] == "connection-" {
			locked = true
		}
	}
	if !locked {
		t.Fatal("uncertain successful effect released lock")
	}
}
func TestReadSnapshotUnaffectedByInputPathReplacement(t *testing.T) {
	out := filepath.Join(readTestDir(t), "snapshot")
	_, _, _, cfg, p, en, _ := enrollmentFixture(t, out, basicReadSource())
	backend := &readFixtureBackend{transport: &readFixtureTransport{}}
	manifest, e := New(nil).WithReadBackend(backend).CaptureReadSnapshot(context.Background(), cfg, p, en, "capture", "", out)
	if e != nil || !manifest.Complete {
		t.Fatal(e)
	}
	cfg[0] = 'x'
	if _, e = LoadReadBundle(context.Background(), out); e != nil {
		t.Fatal("caller slice mutated capture", e)
	}
}
