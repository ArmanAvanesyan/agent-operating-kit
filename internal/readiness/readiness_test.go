package readiness

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/verification"
)

func readyTempDir(t *testing.T) string {
	t.Helper()
	d, e := os.MkdirTemp("/private/tmp", "aok-readiness-")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = os.RemoveAll(d) })
	return d
}

func testRoute() RouteManifest {
	return RouteManifest{Version: Version, ID: "route", Host: "codex", HostVersion: "1", HostSHA256: strings.Repeat("a", 64), OS: "linux", Arch: "amd64", Transport: "app-server", ControlPolicySHA256: strings.Repeat("b", 64), ConfigurationSHA256: strings.Repeat("c", 64), ProfileSHA256: strings.Repeat("d", 64), Account: "acct", AuthMode: "native", Pool: "pool", VerifierSHA256: strings.Repeat("e", 64), RubricSHA256: strings.Repeat("f", 64), EnvironmentSHA256: strings.Repeat("0", 64), Provider: "github", Origin: "https://example.invalid", NativeTenant: "tenant", Container: "repo", ItemKind: "issue", Operation: "update", API: "v1", ProviderAuth: "app", DeliveryMilestone: "merge"}
}
func testPolicy() Policy {
	p := Policy{Version: Version, ID: "policy", MaxEvidenceAgeSeconds: 172800, Requirements: []GateRequirement{}}
	for _, id := range mandatoryGateIDs {
		host := "codex"
		applicability := "applicable"
		if id == "p6.claude" {
			host = "claude-code"
			applicability = "not-applicable"
		}
		scope := scopeFor(id)
		if id == "p6.claude" {
			scope["host"] = host
		}
		p.Requirements = append(p.Requirements, GateRequirement{ID: id, Validator: expectedValidator(id), Required: true, EvidenceOrigin: "live", Applicability: applicability, Scope: scope, Bindings: []EvidenceBinding{}})
	}
	return p
}
func scopeFor(id string) map[string]string {
	all := routeScope(testRoute())
	out := map[string]string{}
	for _, k := range scopeDimensions(id) {
		out[k] = all[k]
	}
	return out
}
func trustedInputs(root string, p Policy) Inputs {
	d, _ := Digest(p)
	return Inputs{EvidenceRoot: root, PolicyPin: PolicyPin{SHA256: d, Principal: "ops", Role: "readiness-policy-authority"}, PolicyAuthority: PolicyAuthorityFunc(func(p Policy, pin PolicyPin) error {
		d, _ := Digest(p)
		if d != pin.SHA256 || pin.Principal != "ops" || pin.Role != "readiness-policy-authority" {
			return errors.New("untrusted test pin")
		}
		return nil
	})}
}
func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(path, b, 0600); e != nil {
		t.Fatal(e)
	}
}

func TestAssessDefaultsBlockAndIsDeterministic(t *testing.T) {
	m, p := testRoute(), testPolicy()
	now := time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)
	in := trustedInputs("", p)
	a, e := Assess(m, p, in, nil, now)
	if e != nil {
		t.Fatal(e)
	}
	b, e := Assess(m, p, in, nil, now)
	if e != nil {
		t.Fatal(e)
	}
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	if string(ja) != string(jb) {
		t.Fatal("report differs for same inputs")
	}
	if a.Status != "blocked" || len(a.Gates) != len(mandatoryGateIDs)+1 {
		t.Fatalf("unexpected report: %s %d", a.Status, len(a.Gates))
	}
	if a.AuthorizationGranted || a.ExecutionReady || len(a.InputSHA256s) < 2 {
		t.Fatal("offline report granted authority or omitted input digests")
	}
	for _, g := range a.Gates {
		if g.ID == "policy-trust" {
			if g.Status != "passed" {
				t.Fatalf("external test authority did not pass: %s", g.Status)
			}
			continue
		}
		if g.ID == "p6.claude" {
			if g.Status != "not-applicable" {
				t.Fatalf("wrong host applicability: %s", g.Status)
			}
			continue
		}
		if g.Status != "unknown" {
			t.Fatalf("missing adapter must be unknown, got %s", g.Status)
		}
	}
}

func TestAuthorityCallbackAndEarliestEvidenceExpiry(t *testing.T) {
	m, p := testRoute(), testPolicy()
	body := []byte("signed live proof")
	dir := readyTempDir(t)
	if err := os.WriteFile(filepath.Join(dir, "proof.json"), body, 0600); err != nil {
		t.Fatal(err)
	}
	p.Requirements[0].Bindings = []EvidenceBinding{{ID: "e", Path: "proof.json", SHA256: Hash(body), Issuer: "issuer", Role: "host-control-policy", ObservedAt: "2026-10-23T23:00:00Z", ValidUntil: "2026-10-24T00:30:00Z", Origin: "live", Scope: scopeFor("p6.codex")}}
	in := trustedInputs(dir, p)
	untrusted := in
	untrusted.PolicyAuthority = nil
	r, err := Assess(m, p, untrusted, nil, time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if gateByID(r, "policy-trust").Status != "blocked" {
		t.Fatal("digest pin passed without external authority callback")
	}
	in = trustedInputs(dir, p)
	adapters := Adapters{expectedValidator("p6.codex"): NativeValidatorFunc(func(ValidationInput) ValidationResult {
		return ValidationResult{Status: "passed", ValidUntil: "2026-10-24T00:20:00Z"}
	})}
	r, err = Assess(m, p, in, adapters, time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if r.EarliestValidUntil != "2026-10-24T00:20:00Z" {
		t.Fatalf("earliest validity not reported: %s", r.EarliestValidUntil)
	}
}

func TestNativeAdaptersRequireExternalTrust(t *testing.T) {
	a := NativeAdapters(NativeTrust{})
	if _, ok := a["native.host.codex"]; ok {
		t.Fatal("host adapter installed without control-root trust")
	}
	if _, ok := a["native.delivery-evidence"]; ok {
		t.Fatal("delivery adapter installed without issuer trust")
	}
	v := a["native.acceptance"]
	if v == nil {
		t.Fatal("acceptance validator inventory missing")
	}
	r := v.Validate(ValidationInput{})
	if r.Status != "unknown" {
		t.Fatalf("acceptance without external trust must stay unknown, got %s", r.Status)
	}
	projectionless := AcceptanceValidator(AcceptanceTrust{Issuers: []verification.TrustedIssuer{{ID: "trusted"}}, MaxAge: time.Hour}).Validate(ValidationInput{})
	if projectionless.Status != "unknown" {
		t.Fatalf("acceptance without protected current projection must stay unknown, got %s", projectionless.Status)
	}
}

func TestApplicableMandatoryGateCannotBeMadeNotApplicableByAdapter(t *testing.T) {
	dir := readyTempDir(t)
	body := []byte("live proof")
	if err := os.WriteFile(filepath.Join(dir, "proof.json"), body, 0600); err != nil {
		t.Fatal(err)
	}
	m, p := testRoute(), testPolicy()
	p.Requirements[0].Bindings = []EvidenceBinding{{ID: "proof", Path: "proof.json", SHA256: Hash(body), Issuer: "issuer", Role: "host", ObservedAt: "2026-10-23T23:00:00Z", ValidUntil: "2026-10-24T01:00:00Z", Origin: "live", Scope: scopeFor("p6.codex")}}
	a := Adapters{expectedValidator(p.Requirements[0].ID): NativeValidatorFunc(func(ValidationInput) ValidationResult {
		return ValidationResult{Status: "not-applicable", Reason: "malicious skip"}
	})}
	r, err := Assess(m, p, trustedInputs(dir, p), a, time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != "blocked" || gateByID(r, "p6.codex").Status != "blocked" {
		t.Fatalf("applicable mandatory gate was skipped: report=%s gate=%s", r.Status, gateByID(r, "p6.codex").Status)
	}
}

func TestAssessPolicyTrustAndFixtureCannotSatisfyLiveGate(t *testing.T) {
	dir := readyTempDir(t)
	body := []byte("fixture receipt")
	if e := os.WriteFile(filepath.Join(dir, "proof.json"), body, 0600); e != nil {
		t.Fatal(e)
	}
	m, p := testRoute(), testPolicy()
	p.Requirements[0].Bindings = []EvidenceBinding{{ID: "receipt", Path: "proof.json", SHA256: Hash(body), Issuer: "source", Role: "host-validator", ObservedAt: "2026-10-23T23:00:00Z", ValidUntil: "2026-10-24T01:00:00Z", Origin: "fixture", Scope: scopeFor("p6.codex")}}
	adapters := Adapters{expectedValidator(p.Requirements[0].ID): NativeValidatorFunc(func(ValidationInput) ValidationResult { return ValidationResult{Status: "passed"} })}
	in := trustedInputs(dir, p)
	r, e := Assess(m, p, in, adapters, time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC))
	if e != nil {
		t.Fatal(e)
	}
	if gateByID(r, "p6.codex").Status != "blocked" {
		t.Fatalf("fixture incorrectly passed live gate: %s", gateByID(r, "p6.codex").Status)
	}
	r, e = Assess(m, p, Inputs{EvidenceRoot: dir}, nil, time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC))
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, g := range r.Gates {
		if g.ID == "policy-trust" && g.Status == "blocked" {
			found = true
		}
	}
	if !found {
		t.Fatal("missing external policy pin did not block")
	}
}
func TestEvidenceTamperExpiryAndUnsafePathBlock(t *testing.T) {
	for _, tc := range []struct {
		name, path string
		expired    bool
		want       string
	}{{"tamper", "proof.json", false, "blocked"}, {"expired", "proof.json", true, "stale"}, {"traversal", "../proof.json", false, "blocked"}} {
		t.Run(tc.name, func(t *testing.T) {
			dir := readyTempDir(t)
			body := []byte("receipt")
			_ = os.WriteFile(filepath.Join(dir, "proof.json"), body, 0600)
			m, p := testRoute(), testPolicy()
			observed := "2026-10-23T23:00:00Z"
			until := ""
			if tc.expired {
				until = "2026-10-23T23:30:00Z"
			}
			if until == "" {
				until = "2026-10-25T00:00:00Z"
			}
			b := EvidenceBinding{ID: "receipt", Path: tc.path, SHA256: Hash(body), Issuer: "source", Role: "native", ObservedAt: observed, ValidUntil: until, Origin: "live", Scope: scopeFor("p6.codex")}
			if tc.name == "tamper" {
				b.SHA256 = strings.Repeat("1", 64)
			}
			p.Requirements[0].Bindings = []EvidenceBinding{b}
			if tc.name == "traversal" {
				if ValidatePolicy(p) == nil {
					t.Fatal("unsafe path accepted")
				}
				return
			}
			a := Adapters{expectedValidator(p.Requirements[0].ID): NativeValidatorFunc(func(ValidationInput) ValidationResult { return ValidationResult{Status: "passed"} })}
			r, e := Assess(m, p, trustedInputs(dir, p), a, time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC))
			if e != nil {
				t.Fatal(e)
			}
			if gateByID(r, "p6.codex").Status != tc.want {
				t.Fatalf("got %s want %s", gateByID(r, "p6.codex").Status, tc.want)
			}
		})
	}
}
func TestClosedSchemaAndPathRead(t *testing.T) {
	if _, _, e := ReadManifest("testdata/route.json"); e != nil {
		t.Fatal(e)
	}
	if _, _, e := ReadPolicy("testdata/policy.json"); e != nil {
		t.Fatal(e)
	}
	dir := readyTempDir(t)
	m := testRoute()
	path := filepath.Join(dir, "manifest.json")
	writeJSON(t, path, m)
	got, _, e := ReadManifest(path)
	if e != nil || got.ID != "route" {
		t.Fatalf("read failed: %v", e)
	}
	b, _ := os.ReadFile(path)
	if e = os.WriteFile(path, append(b, []byte(` {"surprise":true}`)...), 0600); e != nil {
		t.Fatal(e)
	}
	if _, _, e = ReadManifest(path); e == nil {
		t.Fatal("trailing JSON accepted")
	}
	dup := strings.Replace(string(b), `"id":"route"`, `"id":"route","id":"other"`, 1)
	_ = os.WriteFile(path, []byte(dup), 0600)
	if _, _, e = ReadManifest(path); e == nil {
		t.Fatal("duplicate key accepted")
	}
	target := filepath.Join(dir, "target.json")
	writeJSON(t, target, m)
	link := filepath.Join(dir, "link.json")
	if e = os.Symlink(target, link); e != nil {
		t.Fatal(e)
	}
	if _, _, e = ReadManifest(link); e == nil {
		t.Fatal("symlink accepted")
	}
}

func gateByID(r Report, id string) GateResult {
	for _, g := range r.Gates {
		if g.ID == id {
			return g
		}
	}
	return GateResult{}
}
func TestPolicyCannotRemoveMandatoryGateOrUseUnknownValidator(t *testing.T) {
	p := testPolicy()
	p.Requirements = p.Requirements[1:]
	if ValidatePolicy(p) == nil {
		t.Fatal("missing mandatory gate accepted")
	}
	p = testPolicy()
	p.Requirements[0].Validator = "self-attested"
	if ValidatePolicy(p) == nil {
		t.Fatal("unknown validator accepted")
	}
	p = testPolicy()
	p.Requirements[0].EvidenceOrigin = "fixture"
	if ValidatePolicy(p) == nil {
		t.Fatal("fixture origin accepted for mandatory readiness gate")
	}
	p = testPolicy()
	p.Requirements[0].Scope = map[string]string{"host": "codex"}
	if ValidatePolicy(p) == nil {
		t.Fatal("partial mandatory route scope accepted")
	}
	p = testPolicy()
	p.Requirements[0].Bindings = []EvidenceBinding{{ID: "b", Path: "b.json", SHA256: strings.Repeat("1", 64), Issuer: "i", Role: "r", ObservedAt: "2026-10-24T00:00:00Z", ValidUntil: "2026-10-24T00:30:00Z", Origin: "live", Scope: scopeFor("p6.codex")}}
	p.Requirements[0].Bindings[0].Scope["provider"] = "conflict"
	if ValidatePolicy(p) == nil {
		t.Fatal("extra conflicting scope dimension accepted")
	}
}

func TestHostNotApplicableOnlyForDifferentRoute(t *testing.T) {
	m, p := testRoute(), testPolicy()
	p.Requirements[0].Applicability = "not-applicable"
	r, e := Assess(m, p, trustedInputs("", p), nil, time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC))
	if e != nil {
		t.Fatal(e)
	}
	if gateByID(r, "p6.codex").Status != "blocked" {
		t.Fatal("same-host not-applicable bypassed gate")
	}
}
