package verifier

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/enrolled/artifacts"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/verification"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type fixtureExec struct{ calls int }

func (f *fixtureExec) Execute(ctx context.Context, c verification.Candidate, argv []string, env map[string]string, timeout time.Duration, limit int64) (int, []byte, error) {
	f.calls++
	return 7, []byte("fixture output"), nil
}

type serviceFixture struct {
	h      http.Handler
	c      verification.Candidate
	proof  verification.ControlProof
	spec   verification.VerifierSpec
	policy verification.VerificationPolicy
	exec   *fixtureExec
}

func newServiceFixture(t *testing.T) serviceFixture {
	t.Helper()
	src := t.TempDir()
	body := []byte("source fixture\n")
	sum := sha256.Sum256(body)
	_ = os.WriteFile(filepath.Join(src, "README.txt"), body, 0600)
	m := verification.CandidateManifest{Version: verification.Version, ID: "candidate", Origin: "fixture", Repository: "local/fixture", BaseRevision: "base", SourceRevision: "head", Assets: []verification.Asset{{Path: "README.txt", SHA256: hex.EncodeToString(sum[:]), Role: "source"}}}
	c, e := verification.ImportCandidate(m, src, filepath.Join(t.TempDir(), "bundle"))
	if e != nil {
		t.Fatal(e)
	}
	pub, priv, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	spec := verification.VerifierSpec{ID: "tests", Kind: "command", ImplementationSHA256: hash("impl"), RubricSHA256: hash("rubric"), Argv: []string{"/opt/check"}, Environment: map[string]string{"MODE": "offline"}, TimeoutSeconds: 3, MaxOutputBytes: 1024}
	issuer := verification.TrustedIssuer{ID: "control-issuer", Role: "controls", ExecutorSHA256: hash("executor"), PublicKey: base64.StdEncoding.EncodeToString(pub)}
	p := verification.VerificationPolicy{Version: verification.Version, ID: "policy", Issuers: []verification.TrustedIssuer{issuer}, Verifiers: []verification.VerifierSpec{spec}, Criteria: []verification.Criterion{{ID: "check", Kind: "command", Required: true, VerifierID: "tests", Independent: true}}, MaxAgeSeconds: 3600}
	pd, _ := verification.Digest(p)
	sd, _ := verification.Digest(spec)
	now := time.Now().UTC().Truncate(time.Second)
	proof, e := verification.SignControlProof(verification.ControlProof{PolicySHA256: pd, VerifierSHA256: sd, CandidateSHA256: c.Digest, SnapshotSHA256: c.SnapshotSHA256, Scope: "candidate-offline", ExecutorSHA256: issuer.ExecutorSHA256, ObservedAt: now.Add(-time.Minute).Format(time.RFC3339), ValidUntil: now.Add(2 * time.Minute).Format(time.RFC3339), Controls: []string{"filesystem-isolation", "readonly-candidate", "network-deny", "credential-isolation", "time-bound", "output-bound"}, Issuer: issuer.ID}, priv)
	if e != nil {
		t.Fatal(e)
	}
	store, e := artifacts.NewStore(filepath.Join(t.TempDir(), "artifact-store"), p, spec.ID)
	if e != nil {
		t.Fatal(e)
	}
	ex := &fixtureExec{}
	h, e := NewHandler(Config{Store: store, Policy: p, VerifierID: spec.ID, AuthReference: "broker-ref", Resolver: SecretResolverFunc(func(context.Context, string) (string, error) { return "fixture-bearer", nil }), Executor: ex, AllowInsecureForTests: true, AllowFixtureExecutorForTests: true})
	if e != nil {
		t.Fatal(e)
	}
	return serviceFixture{h: h, c: c, proof: proof, spec: spec, policy: p, exec: ex}
}
func hash(s string) string { x := sha256.Sum256([]byte(s)); return hex.EncodeToString(x[:]) }
func (f serviceFixture) request(method, path string, b []byte) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, bytes.NewReader(b))
	r.Header.Set("Authorization", "Bearer fixture-bearer")
	w := httptest.NewRecorder()
	f.h.ServeHTTP(w, r)
	return w
}
func (f serviceFixture) upload(t *testing.T) {
	t.Helper()
	archive, e := artifacts.Encode(f.c)
	if e != nil {
		t.Fatal(e)
	}
	r := httptest.NewRequest(http.MethodPost, "/v1/candidates", bytes.NewReader(archive))
	r.Header.Set("Authorization", "Bearer fixture-bearer")
	r.Header.Set("X-AOK-Candidate-SHA256", f.c.Digest)
	r.Header.Set("X-AOK-Snapshot-SHA256", f.c.SnapshotSHA256)
	proofBytes, _ := json.Marshal(f.proof)
	r.Header.Set("X-AOK-Control-Proof", base64.StdEncoding.EncodeToString(proofBytes))
	w := httptest.NewRecorder()
	f.h.ServeHTTP(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("upload status %d: %s", w.Code, w.Body.String())
	}
}

func TestHandlerRequiresBearerAndPinnedCommand(t *testing.T) {
	f := newServiceFixture(t)
	store, e := artifacts.NewStore(filepath.Join(t.TempDir(), "identity-required"), f.policy, f.spec.ID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = NewHandler(Config{Store: store, Policy: f.policy, VerifierID: f.spec.ID, AuthReference: "broker-ref", Resolver: SecretResolverFunc(func(context.Context, string) (string, error) { return "fixture-bearer", nil }), Executor: &fixtureExec{}}); e == nil {
		t.Fatal("production handler accepted executor without computed identity")
	}
	archive, e := artifacts.Encode(f.c)
	if e != nil {
		t.Fatal(e)
	}
	r := httptest.NewRequest(http.MethodPost, "/v1/candidates", bytes.NewReader(archive))
	r.Header.Set("Authorization", "Bearer wrong")
	r.Header.Set("X-AOK-Candidate-SHA256", f.c.Digest)
	r.Header.Set("X-AOK-Snapshot-SHA256", f.c.SnapshotSHA256)
	w := httptest.NewRecorder()
	f.h.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status %d", w.Code)
	}
	unsigned := httptest.NewRequest(http.MethodPost, "/v1/candidates", bytes.NewReader(archive))
	unsigned.Header.Set("Authorization", "Bearer fixture-bearer")
	unsigned.Header.Set("X-AOK-Candidate-SHA256", f.c.Digest)
	unsigned.Header.Set("X-AOK-Snapshot-SHA256", f.c.SnapshotSHA256)
	unsignedResponse := httptest.NewRecorder()
	f.h.ServeHTTP(unsignedResponse, unsigned)
	if unsignedResponse.Code != http.StatusForbidden {
		t.Fatalf("unsigned candidate upload status %d", unsignedResponse.Code)
	}
	f.upload(t)
	pd, _ := verification.Digest(f.proof)
	q := executeRequest{Protocol: verification.RemoteProtocol, Proof: f.proof, ProofSHA256: pd, CandidateSHA256: f.c.Digest, SnapshotSHA256: f.c.SnapshotSHA256, Argv: []string{"/opt/other"}, Environment: f.spec.Environment, TimeoutMillis: 3000, MaxOutputBytes: 1024}
	b, _ := json.Marshal(q)
	w = f.request(http.MethodPost, "/v1/execute", b)
	if w.Code != http.StatusForbidden || f.exec.calls != 0 {
		t.Fatalf("unpinned command reached backend: status=%d calls=%d", w.Code, f.exec.calls)
	}
}

func TestExecutionResponseDoesNotClaimAcceptance(t *testing.T) {
	f := newServiceFixture(t)
	f.upload(t)
	pd, _ := verification.Digest(f.proof)
	q := executeRequest{Protocol: verification.RemoteProtocol, Proof: f.proof, ProofSHA256: pd, CandidateSHA256: f.c.Digest, SnapshotSHA256: f.c.SnapshotSHA256, Argv: f.spec.Argv, Environment: f.spec.Environment, TimeoutMillis: 3000, MaxOutputBytes: f.spec.MaxOutputBytes}
	b, _ := json.Marshal(q)
	w := f.request(http.MethodPost, "/v1/execute", b)
	if w.Code != http.StatusOK || f.exec.calls != 1 {
		t.Fatalf("execution failed status=%d calls=%d body=%s", w.Code, f.exec.calls, w.Body.String())
	}
	var out map[string]any
	if json.Unmarshal(w.Body.Bytes(), &out) != nil {
		t.Fatal("invalid response")
	}
	if _, ok := out["acceptanceReceipt"]; ok {
		t.Fatal("execution endpoint manufactured acceptance")
	}
	if out["exitCode"] != float64(7) {
		t.Fatalf("fixture backend code lost: %v", out["exitCode"])
	}
}

func TestHandlerRejectsDuplicateJSONAndBadProof(t *testing.T) {
	f := newServiceFixture(t)
	w := f.request(http.MethodPost, "/v1/execute", []byte(`{"protocol":"aok-verifier-transport/v1","protocol":"aok-verifier-transport/v1"}`))
	if w.Code != http.StatusBadRequest || f.exec.calls != 0 {
		t.Fatalf("duplicate JSON accepted: %d", w.Code)
	}
	pd, _ := verification.Digest(f.proof)
	bad := f.proof
	bad.Signature = "bad"
	b, _ := json.Marshal(executeRequest{Protocol: verification.RemoteProtocol, Proof: bad, ProofSHA256: pd, CandidateSHA256: f.c.Digest, SnapshotSHA256: f.c.SnapshotSHA256, Argv: f.spec.Argv, Environment: f.spec.Environment, TimeoutMillis: 3000, MaxOutputBytes: f.spec.MaxOutputBytes})
	w = f.request(http.MethodPost, "/v1/execute", b)
	if w.Code != http.StatusForbidden || f.exec.calls != 0 {
		t.Fatalf("bad proof reached backend: status=%d calls=%d", w.Code, f.exec.calls)
	}
}
