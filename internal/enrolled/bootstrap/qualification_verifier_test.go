package bootstrap

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/verification"
)

type qualificationVerifierTransport func(*http.Request) (*http.Response, error)

func TestQualificationVerifierValidationUsesInspectionInstant(t *testing.T) {
	c, q, _, proof := newQualificationVerifierFixture(t)
	at, err := time.Parse(time.RFC3339, proof.ObservedAt)
	if err != nil {
		t.Fatal(err)
	}
	if validateQualificationVerifier(c, &q, at.Add(15*time.Second)) != nil {
		t.Fatal("fresh proof rejected at explicit inspection instant")
	}
	if validateQualificationVerifier(c, &q, at.Add(2*time.Minute)) == nil {
		t.Fatal("expired proof accepted at explicit inspection instant")
	}
}

func (f qualificationVerifierTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func qualificationVerifierResponse(status int, value any) *http.Response {
	b, _ := json.Marshal(value)
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(b)))}
}

func newQualificationVerifierFixture(t *testing.T) (Config, QualificationRecords, verification.Candidate, verification.ControlProof) {
	t.Helper()
	runtimeCandidate, _, _ := postRunFixture(t)
	source := filepath.Join(filepath.Dir(runtimeCandidate.BundlePath), "source")
	manifest := runtimeCandidate.Manifest
	manifest.Origin = "fixture"
	candidate, err := verification.ImportCandidate(manifest, source, filepath.Join(t.TempDir(), "fixture-candidate"))
	if err != nil {
		t.Fatal(err)
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	executor := sha256.Sum256([]byte("fixture-remote-executor"))
	specHash := sha256.Sum256([]byte("fixture-spec"))
	spec := verification.VerifierSpec{ID: "command", Kind: "command", ImplementationSHA256: hex.EncodeToString(specHash[:]), RubricSHA256: hex.EncodeToString(specHash[:]), Argv: []string{"/bin/true"}, Environment: map[string]string{}, TimeoutSeconds: 5, MaxOutputBytes: 1024}
	policy := verification.VerificationPolicy{Version: verification.Version, ID: "qualification-fixture-policy", MaxAgeSeconds: 300, Issuers: []verification.TrustedIssuer{{ID: "fixture-controls", Role: "controls", PublicKey: base64.StdEncoding.EncodeToString(pub), ExecutorSHA256: hex.EncodeToString(executor[:])}}, Verifiers: []verification.VerifierSpec{spec}, Criteria: []verification.Criterion{{ID: "command-result", Kind: "command", Required: true, VerifierID: spec.ID}}}
	policyDigest, err := verification.Digest(policy)
	if err != nil {
		t.Fatal(err)
	}
	specDigest, err := verification.Digest(spec)
	if err != nil {
		t.Fatal(err)
	}
	observed := time.Now().UTC().Truncate(time.Second)
	proof, err := verification.SignControlProof(verification.ControlProof{Version: verification.Version, PolicySHA256: policyDigest, VerifierSHA256: specDigest, CandidateSHA256: candidate.Digest, SnapshotSHA256: candidate.SnapshotSHA256, Scope: "candidate-offline", ExecutorSHA256: hex.EncodeToString(executor[:]), ObservedAt: observed.Format(time.RFC3339), ValidUntil: observed.Add(time.Minute).Format(time.RFC3339), Controls: []string{"filesystem-isolation", "readonly-candidate", "network-deny", "credential-isolation", "time-bound", "output-bound"}, Issuer: "fixture-controls"}, priv)
	if err != nil {
		t.Fatal(err)
	}
	const authRef = "fixture-verifier-auth"
	c := Config{SecretEnv: map[string]string{authRef: "AOK_QUAL_VERIFIER_TOKEN"}}
	t.Setenv("AOK_QUAL_VERIFIER_TOKEN", "fixture-secret")
	q := QualificationRecords{Verifier: &VerifierConformance{CandidatePath: candidate.BundlePath, CandidateSHA256: candidate.Digest, SnapshotSHA256: candidate.SnapshotSHA256, Endpoint: "https://verifier.fixture.test/v1/execute", AuthReference: authRef, Policy: policy, VerifierID: spec.ID, Proof: proof}}
	return c, q, candidate, proof
}

func TestQualificationVerifierUploadsThenExecutesBoundFixture(t *testing.T) {
	c, q, candidate, proof := newQualificationVerifierFixture(t)
	proofDigest, _ := verification.Digest(proof)
	var paths []string
	transport := qualificationVerifierTransport(func(req *http.Request) (*http.Response, error) {
		paths = append(paths, req.URL.Path)
		if req.Header.Get("Authorization") != "Bearer fixture-secret" {
			t.Fatal("missing fixture auth")
		}
		switch req.URL.Path {
		case "/v1/candidates":
			if req.Header.Get("X-AOK-Candidate-SHA256") != candidate.Digest || req.Header.Get("X-AOK-Snapshot-SHA256") != candidate.SnapshotSHA256 || req.Header.Get("X-AOK-Control-Proof") == "" {
				t.Fatal("upload did not bind candidate, snapshot, and proof")
			}
			if _, err := io.Copy(io.Discard, req.Body); err != nil {
				t.Fatal(err)
			}
			return qualificationVerifierResponse(http.StatusCreated, map[string]string{"candidateSha256": candidate.Digest, "snapshotSha256": candidate.SnapshotSHA256}), nil
		case "/v1/execute":
			var payload map[string]any
			if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if payload["candidateSha256"] != candidate.Digest || payload["snapshotSha256"] != candidate.SnapshotSHA256 {
				t.Fatal("execute lost candidate binding")
			}
			return qualificationVerifierResponse(http.StatusOK, map[string]any{"protocol": verification.RemoteProtocol, "proofSha256": proofDigest, "exitCode": 0, "stdoutBase64": base64.StdEncoding.EncodeToString([]byte("ok"))}), nil
		default:
			t.Fatalf("unexpected verifier route %s", req.URL.Path)
			return nil, nil
		}
	})
	outputs, err := runQualificationVerifier(context.Background(), c, q, transport)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 || paths[0] != "/v1/candidates" || paths[1] != "/v1/execute" {
		t.Fatalf("unexpected upload/execute order: %v", paths)
	}
	var report map[string]any
	if err := json.Unmarshal(outputs["verifier-observation.json"], &report); err != nil {
		t.Fatal(err)
	}
	if report["observationOnly"] != true || report["authorizationGranted"] != false || report["executionReady"] != false || report["acceptanceGranted"] != false {
		t.Fatalf("qualification overstated authority: %+v", report)
	}
}

func TestQualificationVerifierRejectsWrongOriginAndTamperedProofBeforeRemote(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*QualificationRecords, verification.Candidate)
	}{
		{"runtime candidate", func(q *QualificationRecords, c verification.Candidate) {
			q.Verifier.CandidatePath = c.BundlePath
			q.Verifier.CandidateSHA256 = c.Digest
			q.Verifier.SnapshotSHA256 = c.SnapshotSHA256
		}},
		{"tampered proof", func(q *QualificationRecords, _ verification.Candidate) {
			q.Verifier.Proof.Signature = strings.Repeat("0", len(q.Verifier.Proof.Signature))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, q, candidate, _ := newQualificationVerifierFixture(t)
			if tc.name == "runtime candidate" {
				// The original freshly produced runtime bundle is intentionally substituted.
				runtime, _, _ := postRunFixture(t)
				tc.mutate(&q, runtime)
			} else {
				tc.mutate(&q, candidate)
			}
			called := false
			transport := qualificationVerifierTransport(func(*http.Request) (*http.Response, error) { called = true; return nil, nil })
			if _, err := runQualificationVerifier(context.Background(), c, q, transport); err == nil {
				t.Fatal("invalid candidate/proof accepted")
			}
			if called {
				t.Fatal("invalid candidate/proof reached remote verifier")
			}
		})
	}
}

func TestQualificationVerifierExpiredContextDoesNotUpload(t *testing.T) {
	c, q, _, _ := newQualificationVerifierFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	if _, err := runQualificationVerifier(ctx, c, q, qualificationVerifierTransport(func(*http.Request) (*http.Response, error) { calls++; return nil, nil })); err == nil {
		t.Fatal("canceled qualification unexpectedly succeeded")
	}
	if calls != 0 {
		t.Fatalf("canceled qualification made %d remote calls", calls)
	}
}
