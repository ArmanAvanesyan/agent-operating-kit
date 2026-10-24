package v2

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	hostv2 "github.com/ArmanAvanesyan/agent-operating-kit/internal/hostbridge/v2"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/hostdriver"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/readiness"
	readinessv2 "github.com/ArmanAvanesyan/agent-operating-kit/internal/readiness/v2"
)

type hostVerifier struct{ called bool }

func (v *hostVerifier) Verify(context.Context, hostv2.Scope, json.RawMessage) error {
	v.called = true
	return nil
}
func TestPilotV2BlocksUnintegratedHostBeforeRuntime(t *testing.T) {
	d, _ := hostdriver.Lookup("gemini")
	hash := strings.Repeat("a", 64)
	base := readiness.RouteManifest{Version: readiness.Version, ID: "route-1", Host: "gemini", HostVersion: "1", HostSHA256: hash, OS: "darwin", Arch: "arm64", Transport: d.Protocol, ControlPolicySHA256: hash, ConfigurationSHA256: hash, ProfileSHA256: hash, Account: "acct", AuthMode: "native-login", Pool: "pool", VerifierSHA256: hash, RubricSHA256: hash, EnvironmentSHA256: hash}
	h := strings.Repeat("a", 64)
	r := Request{Route: readinessv2.Route{Version: readinessv2.Version, Legacy: base, DriverSHA256: d.Digest, Protocol: d.Protocol, PreparedSHA256: h, PolicySHA256: h, LaunchSHA256: h, InputSHA256: h}, ReadinessAuthority: readinessv2.AuthorityFunc(func(readinessv2.Route, readiness.Report, json.RawMessage) error { return nil }), ReadinessProof: []byte(`{}`), HostScope: hostv2.Scope{Host: "gemini", DescriptorSHA256: d.Digest}, HostProof: []byte(`{}`)}
	v := &hostVerifier{}
	r.HostVerifier = v
	a, e := Admit(context.Background(), r)
	if e != nil {
		t.Fatal(e)
	}
	if a.Status != "blocked" || v.called {
		t.Fatalf("unexpected: %+v called=%t", a, v.called)
	}
}
