package v2

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/hostdriver"
	legacy "github.com/ArmanAvanesyan/agent-operating-kit/internal/readiness"
)

func readyV2FixtureReport(t *testing.T, route Route, manifestSHA string) legacy.Report {
	t.Helper()
	now := time.Now().UTC()
	gates := []legacy.GateResult{}
	for _, id := range UniversalGateIDs(route) {
		gates = append(gates, legacy.GateResult{ID: id, Required: true, Status: "passed", EvidenceSHA256: []string{strings.Repeat("b", 64)}})
	}
	return legacy.Report{Version: legacy.Version, ManifestSHA256: manifestSHA, PolicySHA256: strings.Repeat("c", 64), AsOf: now.Format(time.RFC3339Nano), EarliestValidUntil: now.Add(time.Minute).Format(time.RFC3339Nano), Gates: gates, Status: "ready-offline"}
}

func TestAdmitOfflineNeverGrantsAndNewHostTransportBlocks(t *testing.T) {
	hash := strings.Repeat("a", 64)
	base := legacy.RouteManifest{Version: legacy.Version, ID: "route-1", Host: "cursor", HostVersion: "1", HostSHA256: hash, OS: "darwin", Arch: "arm64", Transport: "cursor-agent-stream-json", ControlPolicySHA256: hash, ConfigurationSHA256: hash, ProfileSHA256: hash, Account: "a", AuthMode: "native-login", Pool: "pool", VerifierSHA256: hash, RubricSHA256: hash, EnvironmentSHA256: hash}
	h, err := legacy.Digest(base)
	if err != nil {
		t.Fatal(err)
	}
	d, _ := hostdriver.Lookup("cursor")
	r := Route{Version: Version, Legacy: base, DriverSHA256: d.Digest, Protocol: d.Protocol, PreparedSHA256: strings.Repeat("c", 64), PolicySHA256: strings.Repeat("d", 64), LaunchSHA256: strings.Repeat("e", 64), InputSHA256: strings.Repeat("f", 64)}
	report := readyV2FixtureReport(t, r, h)
	a, err := Evaluate(r, report)
	if err != nil {
		t.Fatal(err)
	}
	if a.Status != "blocked" || a.AuthorizationGranted || a.Reason == "" {
		t.Fatalf("offline report unexpectedly admitted: %+v", a)
	}
	verifier := AuthorityFunc(func(Route, legacy.Report, json.RawMessage) error { return nil })
	a, err = Admit(r, report, []byte(`{"proof":true}`), verifier)
	if err != nil {
		t.Fatal(err)
	}
	if a.Status != "eligible" || a.AuthorizationGranted {
		t.Fatalf("unexpected protected readiness result %+v", a)
	}
}

func TestAdmitRequiresExactCompiledDriverAndProtectedProof(t *testing.T) {
	hash := strings.Repeat("a", 64)
	base := legacy.RouteManifest{Version: legacy.Version, ID: "route-1", Host: "codex", HostVersion: "0.162.1", HostSHA256: hash, OS: "darwin", Arch: "arm64", Transport: "codex-app-server-rpc", ControlPolicySHA256: hash, ConfigurationSHA256: hash, ProfileSHA256: hash, Account: "a", AuthMode: "native-login", Pool: "pool", VerifierSHA256: hash, RubricSHA256: hash, EnvironmentSHA256: hash}
	h, _ := legacy.Digest(base)
	d, _ := hostdriver.Lookup("codex")
	r := Route{Version: Version, Legacy: base, DriverSHA256: d.Digest, Protocol: d.Protocol, PreparedSHA256: strings.Repeat("c", 64), PolicySHA256: strings.Repeat("d", 64), LaunchSHA256: strings.Repeat("e", 64), InputSHA256: strings.Repeat("f", 64)}
	report := readyV2FixtureReport(t, r, h)
	if a, _ := Admit(r, report, []byte(`{}`), AuthorityFunc(func(Route, legacy.Report, json.RawMessage) error { return nil })); a.Status != "eligible" {
		t.Fatalf("%+v", a)
	}
	if a, _ := Admit(r, report, nil, AuthorityFunc(func(Route, legacy.Report, json.RawMessage) error { return nil })); a.Status != "blocked" {
		t.Fatal("missing proof accepted")
	}
	r.DriverSHA256 = "wrong"
	if a, _ := Admit(r, report, []byte(`{}`), AuthorityFunc(func(Route, legacy.Report, json.RawMessage) error { return nil })); a.Status != "blocked" {
		t.Fatal("wrong driver accepted")
	}
}

func TestDecodeRouteStrict(t *testing.T) {
	var r Route
	if DecodeStrict([]byte(`{"version":"aok-readiness/v2","unexpected":1}`), &r) == nil {
		t.Fatal("unknown field accepted")
	}
	if DecodeStrict([]byte(`{"version":"aok-readiness/v2","version":"aok-readiness/v2"}`), &r) == nil {
		t.Fatal("duplicate key accepted")
	}
}

func TestAdmitRequiresFreshPassedUniversalGates(t *testing.T) {
	hash := strings.Repeat("a", 64)
	base := legacy.RouteManifest{Version: legacy.Version, ID: "route", Host: "codex", HostVersion: "1.2.3", HostSHA256: hash, OS: "darwin", Arch: "arm64", Transport: "codex-app-server-rpc", ControlPolicySHA256: hash, ConfigurationSHA256: hash, ProfileSHA256: hash, Account: "acct", AuthMode: "native-login", Pool: "pool", VerifierSHA256: hash, RubricSHA256: hash, EnvironmentSHA256: hash}
	manifestHash, _ := legacy.Digest(base)
	d, _ := hostdriver.Lookup("codex")
	r := Route{Version: Version, Legacy: base, DriverSHA256: d.Digest, Protocol: d.Protocol, PreparedSHA256: hash, PolicySHA256: hash, LaunchSHA256: hash, InputSHA256: hash}
	verifier := AuthorityFunc(func(Route, legacy.Report, json.RawMessage) error { return nil })
	report := readyV2FixtureReport(t, r, manifestHash)
	if a, err := Admit(r, report, []byte(`{}`), verifier); err != nil || a.Status != "eligible" {
		t.Fatalf("valid gates blocked: %+v %v", a, err)
	}
	report.Gates = report.Gates[:len(report.Gates)-1]
	if a, _ := Admit(r, report, []byte(`{}`), verifier); a.Status == "eligible" {
		t.Fatal("missing universal gate admitted")
	}
	report = readyV2FixtureReport(t, r, manifestHash)
	report.EarliestValidUntil = time.Now().Add(-time.Second).Format(time.RFC3339Nano)
	if a, _ := Admit(r, report, []byte(`{}`), verifier); a.Status == "eligible" {
		t.Fatal("expired readiness report admitted")
	}
}
