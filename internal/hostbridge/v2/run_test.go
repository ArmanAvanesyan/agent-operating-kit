package v2

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/hostbridge"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/hostdriver"
)

type testRunner struct{ starts int }

func (r *testRunner) Start(context.Context, hostbridge.Launch) (hostbridge.Process, error) {
	r.starts++
	return nil, errors.New("must not start in rejection test")
}

type testVerifier struct{ err error }

func (v testVerifier) Verify(context.Context, Scope, json.RawMessage) error { return v.err }

type testGuard struct{ err error }

func (g testGuard) Check(context.Context, string) error { return g.err }

func TestRunRejectsBadScopeBeforeRunnerSideEffect(t *testing.T) {
	d, _ := hostdriver.Lookup("cursor")
	runner := &testRunner{}
	scope := Scope{RunID: "run", Host: "cursor", DescriptorSHA256: d.Digest, PreparedSHA256: "prepared", PolicySHA256: "policy", ExecutableSHA256: "exe", Workspace: "/repo", HostVersion: "1", ExpiresAt: "2099-01-01T00:00:00Z"}
	p := hostdriver.Prepared{Host: "cursor", DescriptorSHA256: d.Digest, Digest: "prepared", PolicySHA256: "policy", Workspace: "/repo", MaxSeconds: 20, MaxBytes: 1024}
	req := RunRequest{RunID: "run", AttemptID: "attempt", Scope: scope, Proof: []byte(`{"signed":true}`), Prepared: p, Launch: hostbridge.Launch{TimeoutSeconds: 20}, Input: []byte(`{"task":"x"}`)}
	_, err := Run(context.Background(), req, runner, testVerifier{err: errors.New("bad signature/scope")}, testGuard{})
	if err == nil || runner.starts != 0 {
		t.Fatalf("err=%v starts=%d", err, runner.starts)
	}
	req.Scope.Workspace = "/other"
	_, err = Run(context.Background(), req, runner, testVerifier{}, testGuard{})
	if err == nil || runner.starts != 0 {
		t.Fatalf("scope mismatch err=%v starts=%d", err, runner.starts)
	}
}
