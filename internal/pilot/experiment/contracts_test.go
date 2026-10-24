package experiment

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/modelcatalog"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/pilot"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type revoker struct {
	err   error
	calls int
}

func (r *revoker) CheckExperimentGrantCurrent(Grant, time.Time) error { r.calls++; return r.err }

type guard struct {
	calls int
	used  map[string]bool
}

func (g *guard) ConsumeExperimentTrial(id, trial, scope string) error {
	g.calls++
	if g.used[id] {
		return os.ErrExist
	}
	g.used[id] = true
	return nil
}

func hx(c byte) string { return strings.Repeat(string(c), 64) }
func fixture(t *testing.T) (Bundle, TrustPolicy, time.Time) {
	t.Helper()
	pub, priv, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	testPrivateKey = priv
	now := time.Date(2026, 10, 24, 10, 0, 0, 0, time.UTC)
	m := Manifest{Version: ManifestVersion, ID: "exp-1", Purpose: Purpose, Risk: RiskStandard, FrozenAt: now.Add(-5 * time.Minute).Format(time.RFC3339), ConfigurationSHA256: hx('1'), ProfileSHA256: hx('2'), ProtocolSHA256: hx('3'), MaximumAttempts: 1, MaximumSteps: 1, MaximumSeconds: 60, MaximumConsumption: json.Number("5"), ConsumptionUnit: "tokens"}
	m.Route = pilot.Route{Host: "codex-app-server", HostVersion: "1.0", HostSHA256: hx('4'), Provider: "github", Platform: "darwin-arm64", Transport: "local", ControlPolicySHA256: hx('7'), ConfigurationSHA256: m.ConfigurationSHA256, ProfileSHA256: m.ProfileSHA256, RouteManifestSHA256: hx('8'), NativeTenant: "tenant", API: "issue-comment", Origin: "github.com", VerifierSHA256: hx('9'), RubricSHA256: hx('a'), EnvironmentSHA256: hx('b'), AccountScopeSHA256: hx('c'), Account: "acct", Operation: "comment", AuthMode: "dedicated"}
	m.Trial = pilot.TrialSpec{ID: "trial-1", Repository: "owner/repo", Account: "acct", InputSHA256: hx('d'), RouteSHA256: digest(m.Route), MaximumAttempts: 1, MaximumSeconds: 60, MaximumConsumption: json.Number("5"), ConsumptionUnit: "tokens"}
	g := Grant{Version: GrantVersion, ID: "grant-1", Issuer: "operator", Role: GrantRole, ManifestSHA256: ManifestDigest(m), TrialID: m.Trial.ID, RouteSHA256: m.Trial.RouteSHA256, InputSHA256: m.Trial.InputSHA256, ConfigurationSHA256: m.ConfigurationSHA256, ProfileSHA256: m.ProfileSHA256, ProtocolSHA256: m.ProtocolSHA256, MaximumAttempts: 1, MaximumSeconds: 60, MaximumConsumption: json.Number("4"), ConsumptionUnit: "tokens", IssuedAt: now.Add(-time.Minute).Format(time.RFC3339), ExpiresAt: now.Add(time.Hour).Format(time.RFC3339)}
	g = SignGrant(g, priv)
	b := Bundle{Version: BundleVersion, Manifest: m, Grant: g, Evidence: []EvidenceRef{{Kind: "selection", SHA256: hx('5'), ScopeSHA256: hx('6')}}}
	return b, TrustPolicy{Principal: "operator", Role: GrantRole, PublicKey: pub, MaxTTL: 2 * time.Hour, Revocations: &revoker{}}, now
}

func TestVerifyConsumeAndReplay(t *testing.T) {
	b, p, now := fixture(t)
	r, e := Verify(b, p, now)
	if e != nil {
		t.Fatal(e)
	}
	if !r.EvidenceOnly || r.AuthorizationGranted || r.ExecutionReady || r.ProductionEligible {
		t.Fatalf("unsafe report: %+v", r)
	}
	g := &guard{used: map[string]bool{}}
	if _, e = consumeAt(r, g, now); e != nil {
		t.Fatal(e)
	}
	if _, e = consumeAt(r, g, now); e == nil {
		t.Fatal("expected duplicate trial rejection")
	}
	if g.calls != 2 {
		t.Fatalf("calls=%d", g.calls)
	}
}

func TestConsumeRejectsMutatedOrExpiredReport(t *testing.T) {
	b, p, now := fixture(t)
	r, err := Verify(b, p, now)
	if err != nil {
		t.Fatal(err)
	}
	g := &guard{used: map[string]bool{}}
	mutated := r
	mutated.ScopeSHA256 = hx('f')
	if _, err = consumeAt(mutated, g, now); err == nil {
		t.Fatal("mutated report consumed")
	}
	mutated = r
	mutated.Evidence = append([]EvidenceRef(nil), r.Evidence...)
	mutated.Evidence[0].SHA256 = hx('e')
	if _, err = consumeAt(mutated, g, now); err == nil {
		t.Fatal("mutated evidence consumed")
	}
	if _, err = consumeAt(r, g, now.Add(2*time.Hour)); err == nil {
		t.Fatal("expired report consumed")
	}
	if g.calls != 0 {
		t.Fatalf("guard called for invalid report: %d", g.calls)
	}
}

func TestRejectTamperingExpiryRoleAndLimits(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Bundle, *TrustPolicy, time.Time)
	}{
		{"tampered", func(b *Bundle, _ *TrustPolicy, _ time.Time) { b.Grant.InputSHA256 = hx('7') }},
		{"wrong-role", func(_ *Bundle, p *TrustPolicy, _ time.Time) { p.Role = "pilot-promotion-authority" }},
		{"expired", func(b *Bundle, _ *TrustPolicy, n time.Time) {
			b.Grant.ExpiresAt = n.Add(-time.Second).Format(time.RFC3339)
			b.Grant = SignGrant(b.Grant, testPrivateKey)
		}},
		{"attempts", func(b *Bundle, _ *TrustPolicy, _ time.Time) { b.Manifest.MaximumAttempts = 2 }},
		{"steps", func(b *Bundle, _ *TrustPolicy, _ time.Time) { b.Manifest.MaximumSteps = 2 }},
		{"seconds", func(b *Bundle, _ *TrustPolicy, _ time.Time) { b.Manifest.MaximumSeconds = 61 }},
		{"risk", func(b *Bundle, _ *TrustPolicy, _ time.Time) { b.Manifest.Risk = "high" }},
		{"freeze-after-issue", func(b *Bundle, _ *TrustPolicy, n time.Time) {
			b.Manifest.FrozenAt = n.Format(time.RFC3339)
			b.Grant.ManifestSHA256 = ManifestDigest(b.Manifest)
			b.Grant = SignGrant(b.Grant, testPrivateKey)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, p, n := fixture(t)
			tt.mutate(&b, &p, n)
			if _, e := Verify(b, p, n); e == nil {
				t.Fatal("expected rejection")
			}
		})
	}
}

var testPrivateKey ed25519.PrivateKey

func TestStrictBoundedRead(t *testing.T) {
	b, _, _ := fixture(t)
	raw, e := json.Marshal(b)
	if e != nil {
		t.Fatal(e)
	}
	d, e := os.MkdirTemp("/private/tmp", "aok-experiment-")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = os.RemoveAll(d) })
	path := filepath.Join(d, "bundle.json")
	if e = os.WriteFile(path, raw, 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = ReadFile(path, Limits{MaxBytes: int64(len(raw))}); e != nil {
		t.Fatal(e)
	}
	if _, e = ReadFile(path, Limits{MaxBytes: 10}); e == nil {
		t.Fatal("expected size rejection")
	}
	dup := strings.Replace(string(raw), `"version":"`+BundleVersion+`"`, `"version":"`+BundleVersion+`","version":"`+BundleVersion+`"`, 1)
	if e = os.WriteFile(path, []byte(dup), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = ReadFile(path, Limits{}); e == nil {
		t.Fatal("expected duplicate-key rejection")
	}
	link := filepath.Join(d, "link.json")
	if e = os.Symlink(path, link); e != nil {
		t.Fatal(e)
	}
	if _, e = ReadFile(link, Limits{}); e == nil {
		t.Fatal("expected symlink rejection")
	}
}

func TestGrantVersionConfusionAndCurrentRevocation(t *testing.T) {
	b, p, n := fixture(t)
	b.Grant.Version = "aok-calibration-grant/v1"
	if _, e := Verify(b, p, n); e == nil {
		t.Fatal("accepted calibration grant")
	}
	b, p, n = fixture(t)
	rv := p.Revocations.(*revoker)
	rv.err = os.ErrPermission
	if _, e := Verify(b, p, n); e == nil || rv.calls != 1 {
		t.Fatal("revoked or uncertain grant accepted")
	}
}

func TestInMemoryNonJSONNumbersCannotCollideInSignedDigests(t *testing.T) {
	for _, bad := range []string{"1/2", "01", "+2", "NaN"} {
		b, _, _ := fixture(t)
		b.Manifest.MaximumConsumption = json.Number(bad)
		if ValidateManifest(b.Manifest) == nil {
			t.Fatalf("invalid JSON number %q admitted", bad)
		}
	}
}

func TestExperimentRecordsAreNotProductionContracts(t *testing.T) {
	b, _, _ := fixture(t)
	raw, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	var production pilot.Manifest
	if err = modelcatalog.Decode(raw, &production); err == nil {
		t.Fatal("experiment bundle decoded as production manifest")
	}
	var promotion pilot.PromotionGrant
	grantBytes, err := json.Marshal(b.Grant)
	if err != nil {
		t.Fatal(err)
	}
	if err = modelcatalog.Decode(grantBytes, &promotion); err == nil {
		t.Fatal("experiment grant decoded as promotion grant")
	}
}
