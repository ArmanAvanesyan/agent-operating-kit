package toolisolation

import (
	"context"
	"strings"
	"sync"
	"testing"
)

type sessionAdmission struct{ deny bool }

func (g *sessionAdmission) CheckToolDispatch(context.Context, string) error {
	if g.deny {
		return context.Canceled
	}
	return nil
}

func TestSessionRequestsCannotChangeAuthority(t *testing.T) {
	p := Policy{Version: Version, Workspace: "/unmaterialized-workspace", Target: "pilot-output.txt", WorkerExecutable: "/protected/aok-tool-worker", WorkerSHA256: strings.Repeat("a", 64), TimeoutSeconds: 1}
	s, err := NewSession(p, &sessionAdmission{})
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{`{"operation":"read","path":"../private"}`, `{"operation":"create-target","policy":{}}`, `{"operation":"read","path":"ok","env":{"TOKEN":"canary"}}`, `{"operation":"read","operation":"create-target","path":"ok"}`, `{"operation":"shell","path":"ok"}`} {
		if _, err = s.Handle(context.Background(), []byte(raw)); err == nil {
			t.Fatal("untrusted message accepted")
		}
	}
	if s.attempted || s.reads != 0 {
		t.Fatal("invalid request consumed dispatch")
	}
}

func TestSessionConsumesAmbiguousCreateWithoutResend(t *testing.T) {
	p := Policy{Version: Version, Workspace: "/unmaterialized-workspace", Target: "pilot-output.txt", WorkerExecutable: "/protected/aok-tool-worker", WorkerSHA256: strings.Repeat("a", 64), TimeoutSeconds: 1}
	s, err := NewSession(p, &sessionAdmission{})
	if err != nil {
		t.Fatal(err)
	}
	p.Workspace = "/different" // Captured policy is a value, not caller-mutable state.
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, e := s.Handle(context.Background(), []byte(`{"operation":"create-target"}`)); e == nil {
				t.Error("unexpected execution")
			}
		}()
	}
	wg.Wait()
	if !s.attempted || s.policy.Workspace != "/unmaterialized-workspace" {
		t.Fatal("dispatch was not consumed or policy mutated")
	}
	if _, err = s.Handle(context.Background(), []byte(`{"operation":"create-target"}`)); err == nil || err.Error() != "create attempt already consumed" {
		t.Fatal("create was resent")
	}
}

func TestSessionRequiresAdmissionAndRechecksRevocation(t *testing.T) {
	p := Policy{Version: Version, Workspace: "/unmaterialized-workspace", Target: "pilot-output.txt", WorkerExecutable: "/protected/aok-tool-worker", WorkerSHA256: strings.Repeat("a", 64), TimeoutSeconds: 1}
	if _, err := NewSession(p, nil); err == nil {
		t.Fatal("missing admission accepted")
	}
	gate := &sessionAdmission{deny: true}
	s, err := NewSession(p, gate)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Handle(context.Background(), []byte(`{"operation":"create-target"}`)); err == nil || s.attempted {
		t.Fatal("revoked admission dispatched")
	}
}
