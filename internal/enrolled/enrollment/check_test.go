package enrollment

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/enrolled/bootstrap"
)

func put(t *testing.T, root, p string, b []byte) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(p))
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(path, b, 0600); e != nil {
		t.Fatal(e)
	}
}
func tempDir(t *testing.T) string {
	t.Helper()
	d, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func fixture(t *testing.T) string {
	t.Helper()
	root := filepath.Join(tempDir(t), "prepared")
	if err := Prepare(root); err != nil {
		t.Fatal(err)
	}
	return root
}
func TestCheckReportsImmutableAndUnverifiedAndNeverQualifies(t *testing.T) {
	root := fixture(t)
	at := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	r, e := Check(root, at)
	if e != nil {
		t.Fatal(e)
	}
	if r.Status != "blocked" || !r.OfflineOnly || r.AuthorizationGranted || r.ExecutionReady || r.ProductionEligible {
		t.Fatalf("unsafe report: %+v", r)
	}
	if !hasFinding(r, "manifest", "valid") || !hasFinding(r, "draft", "valid") || !hasFinding(r, "capacity", "missing") || !hasFinding(r, "input:repository", "missing-value") || !hasFinding(r, "secret-reference:github-read-token", "missing-value") {
		t.Fatalf("missing expected findings: %+v", r.Findings)
	}
	for _, f := range r.Findings {
		if f.Stage == "" {
			t.Fatalf("finding has no stage: %+v", f)
		}
	}
	if !strings.Contains(finding(r, "capacity").Detail, "live qualification remains pending") {
		t.Fatal(finding(r, "capacity"))
	}
	for _, id := range []string{"live:controls", "live:identity", "live:capability", "live:capacity", "live:verification", "live:runtime", "live:authorization", "live:platform", "live:provider-read", "live:provider-operation", "live:witness", "live:budget", "live:stop-monitor", "live:quality", "production:activation"} {
		if !hasFinding(r, id, "pending-external-qualification") {
			t.Fatalf("missing explicit live gate %s", id)
		}
	}
}
func TestCheckRequiresAsOfAndRejectsSymlinkRoot(t *testing.T) {
	root := fixture(t)
	if _, err := Check(root, time.Time{}); err == nil {
		t.Fatal("zero as-of time accepted")
	}
	link := filepath.Join(tempDir(t), "linked-root")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	r, err := Check(link, time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if !hasFinding(r, "manifest", "missing") {
		t.Fatal(r.Findings)
	}
}
func TestCheckRejectsDuplicateManifestJSON(t *testing.T) {
	root := fixture(t)
	put(t, root, "manifest.json", []byte(`{"version":"aok-enrollment-preparation/v1","version":"aok-enrollment-preparation/v1"}`))
	r, e := Check(root, time.Now())
	if e != nil {
		t.Fatal(e)
	}
	if !hasFinding(r, "manifest", "invalid") {
		t.Fatal(r.Findings)
	}
}
func TestCheckRejectsUnsafeDraftPathsAndSymlinks(t *testing.T) {
	root := fixture(t)
	m := Manifest{Version: Version, Purpose: checkPurpose, Drafts: []Asset{{"../escape", "abcd"}}, Required: Requirements()}
	b, _ := json.Marshal(m)
	put(t, root, "manifest.json", b)
	outside := filepath.Join(tempDir(t), "secret")
	if e := os.WriteFile(outside, []byte("x"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.MkdirAll(filepath.Join(root, "inputs"), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink(outside, filepath.Join(root, "inputs", "bootstrap-root.ed25519")); e != nil {
		t.Fatal(e)
	}
	r, e := Check(root, time.Now())
	if e != nil {
		t.Fatal(e)
	}
	if !hasFinding(r, "manifest", "invalid") || !hasFinding(r, "public-root", "missing") {
		t.Fatal(r.Findings)
	}
}
func TestCheckAcceptsSignatureOnlyWithAsOfExpiry(t *testing.T) {
	root := fixture(t)
	pub, priv, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	put(t, root, "inputs/bootstrap-root.ed25519", pub)
	at := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	cfg := bootstrap.Config{Version: bootstrap.ConfigVersion, Service: "anchor", SecretEnv: map[string]string{}, Anchor: &bootstrap.AnchorServiceConfig{}}
	payload, _ := json.Marshal(cfg)
	env := bootstrap.Envelope{Version: bootstrap.EnvelopeVersion, IssuedAt: at.Add(-time.Hour).Format(time.RFC3339Nano), ExpiresAt: at.Add(time.Hour).Format(time.RFC3339Nano), Payload: payload}
	env.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(priv, bootstrap.SigningBytes(env)))
	b, _ := json.Marshal(env)
	put(t, root, "signed/anchor.signed.json", b)
	r, e := Check(root, at)
	if e != nil {
		t.Fatal(e)
	}
	if !hasFinding(r, "anchor-config", "signature-valid-only") {
		t.Fatal(r.Findings)
	}
	r, e = Check(root, at.Add(2*time.Hour))
	if e != nil {
		t.Fatal(e)
	}
	if !hasFinding(r, "anchor-config", "invalid") {
		t.Fatal(r.Findings)
	}
}
func TestCheckWithholdsInventoryValuesAndDoesNotWrite(t *testing.T) {
	root := fixture(t)
	inv := Inventory{Version: Version, Repository: "canary-private-value", IssueNumber: "00", SecretReferences: map[string]string{"github-read-token": "opaque-secret-ref", "bad-key-canary": "private-value"}}
	b, _ := json.Marshal(inv)
	put(t, root, "operator-inputs.json", b)
	before, _ := os.ReadFile(filepath.Join(root, "operator-inputs.json"))
	r, e := Check(root, time.Now())
	if e != nil {
		t.Fatal(e)
	}
	after, _ := os.ReadFile(filepath.Join(root, "operator-inputs.json"))
	if string(before) != string(after) {
		t.Fatal("checker mutated input")
	}
	encoded, _ := json.Marshal(r)
	for _, secret := range []string{"canary-private-value", "opaque-secret-ref", "private-value", "bad-key-canary"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("inventory value leaked: %s", secret)
		}
	}
	if !hasFinding(r, "input:issueNumber", "invalid") || !hasFinding(r, "secret-reference:github-read-token", "present-unverified") || !hasFinding(r, "secret-references", "invalid") {
		t.Fatal(r.Findings)
	}
}
func hasFinding(r Report, id, status string) bool {
	for _, f := range r.Findings {
		if f.ID == id && f.Status == status {
			return true
		}
	}
	return false
}
func finding(r Report, id string) Finding {
	for _, f := range r.Findings {
		if f.ID == id {
			return f
		}
	}
	return Finding{}
}
