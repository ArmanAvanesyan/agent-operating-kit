package v2

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	hostv2 "github.com/ArmanAvanesyan/agent-operating-kit/internal/hostbridge/v2"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/hostdriver"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/readiness"
	readinessv2 "github.com/ArmanAvanesyan/agent-operating-kit/internal/readiness/v2"
)

type verifier struct {
	called bool
	err    error
}

func (v *verifier) Verify(context.Context, hostv2.Scope, json.RawMessage) error {
	v.called = true
	return v.err
}

func request(host string) Request {
	d, _ := hostdriver.Lookup(host)
	h := strings.Repeat("a", 64)
	base := readiness.RouteManifest{Version: readiness.Version, ID: "route-1", Host: host, HostVersion: "1", HostSHA256: h, OS: "linux", Arch: "amd64", Transport: d.Protocol, ControlPolicySHA256: h, ConfigurationSHA256: h, ProfileSHA256: h, Account: "a", AuthMode: "native-login", Pool: "pool", VerifierSHA256: h, RubricSHA256: h, EnvironmentSHA256: h}
	return Request{RunID: "run-1", Route: readinessv2.Route{Version: readinessv2.Version, Legacy: base, DriverSHA256: d.Digest, Protocol: d.Protocol, PreparedSHA256: h, PolicySHA256: h, LaunchSHA256: h, InputSHA256: h}, ReadinessProof: []byte(`{"proof":true}`), ReadinessAuthority: readinessv2.AuthorityFunc(func(readinessv2.Route, readiness.Report, json.RawMessage) error { return nil }), HostScope: hostv2.Scope{RunID: "run-1", Host: host, DescriptorSHA256: d.Digest, PreparedSHA256: h, PolicySHA256: h, LaunchSHA256: h, InputSHA256: h, ExecutableSHA256: h, ProfileSHA256: h, ConfigurationSHA256: h, AccountScope: "a", AuthMode: "native-login", Workspace: "/repo", HostVersion: "1", ExpiresAt: "2099-01-01T00:00:00Z"}, HostProof: []byte(`{"signed":true}`)}
}

func readyReport(t *testing.T, r Request) readiness.Report {
	t.Helper()
	h, _ := readiness.Digest(r.Route.Legacy)
	now := time.Now().UTC()
	gates := []readiness.GateResult{}
	for _, id := range readinessv2.UniversalGateIDs(r.Route) {
		gates = append(gates, readiness.GateResult{ID: id, Required: true, Status: "passed", EvidenceSHA256: []string{strings.Repeat("d", 64)}})
	}
	return readiness.Report{Version: readiness.Version, ManifestSHA256: h, PolicySHA256: strings.Repeat("e", 64), AsOf: now.Format(time.RFC3339Nano), EarliestValidUntil: now.Add(time.Minute).Format(time.RFC3339Nano), Gates: gates, Status: "ready-offline"}
}

func TestAdmitUsesProtectedProofAndBlocksUnintegratedDriver(t *testing.T) {
	r := request("cursor")
	v := &verifier{}
	r.HostVerifier = v
	a, e := Admit(context.Background(), r)
	if e != nil {
		t.Fatal(e)
	}
	if a.Status != "blocked" || v.called {
		t.Fatalf("unexpected admission %+v verifierCalled=%t", a, v.called)
	}
	r = request("codex")
	d, _ := hostdriver.Lookup("codex")
	r.Readiness = readyReport(t, r)
	r.HostVerifier = &verifier{}
	r.ReadinessAuthority = readinessv2.AuthorityFunc(func(readinessv2.Route, readiness.Report, json.RawMessage) error { return nil })
	a, e = Admit(context.Background(), r)
	if e != nil || a.Status != "eligible" {
		t.Fatalf("admission %+v err=%v", a, e)
	}
	if r.HostScope.DescriptorSHA256 != d.Digest {
		t.Fatal("test did not bind current driver")
	}
}

func TestAdmitRejectsWrongScopeBeforeControllerDispatch(t *testing.T) {
	r := request("codex")
	r.Readiness = readyReport(t, r)
	r.ReadinessAuthority = readinessv2.AuthorityFunc(func(readinessv2.Route, readiness.Report, json.RawMessage) error { return nil })
	v := &verifier{err: errors.New("invalid signature")}
	r.HostVerifier = v
	r.HostScope.PolicySHA256 = "wrong"
	_, e := Admit(context.Background(), r)
	if e == nil || v.called {
		t.Fatalf("scope mismatch did not fail before proof check: %v %t", e, v.called)
	}
}

func TestAdmitBindsLaunchAndPreparedInputHashes(t *testing.T) {
	r := request("codex")
	r.Readiness = readyReport(t, r)
	r.HostVerifier = &verifier{}
	r.HostScope.LaunchSHA256 = strings.Repeat("b", 64)
	if _, err := Admit(context.Background(), r); err == nil {
		t.Fatal("launch mutation passed exact route scope")
	}
	r = request("codex")
	r.Readiness = readyReport(t, r)
	r.HostVerifier = &verifier{}
	r.HostScope.InputSHA256 = strings.Repeat("c", 64)
	if _, err := Admit(context.Background(), r); err == nil {
		t.Fatal("input mutation passed exact route scope")
	}
}
