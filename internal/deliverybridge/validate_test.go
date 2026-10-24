package deliverybridge

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/verification"
)

var fixtureSequence atomic.Uint64

func TestPythonCanonicalGolden(t *testing.T) {
	// Produced by Python json.dumps(..., ensure_ascii=True, sort_keys=True,
	// indent=2) and hashlib.sha256 from the repository delivery engine.
	v := object{"alpha": "snowman ☃", "duration": json.Number("0.25"), "empty": object{}, "items": []any{json.Number("1"), true, nil}}
	b, e := pythonCanonical(v)
	if e != nil {
		t.Fatal(e)
	}
	const want = "bc946dc16b7dee83424c83c99c9a9acb1271c89ec1b3bdee896bea62577636c5"
	if shaHex(b) != want {
		t.Fatalf("Python canonical seal mismatch: %s\n%s", shaHex(b), b)
	}
	if _, e = pythonCanonical(object{"bad": json.Number("1e-5")}); e == nil {
		t.Fatal("unsupported exponent canonicalization must fail closed")
	}
}

func testFixture(t *testing.T) HandoffManifest {
	t.Helper()
	assetBytes := []byte("verified output")
	assetHash := shaHex(assetBytes)
	manifest := verification.CandidateManifest{Version: verification.Version, ID: fmt.Sprintf("candidate-%d", fixtureSequence.Add(1)), Origin: "manual", AuthorPrincipal: "author@example", Repository: "/repo", BaseRevision: repeat('a', 40), SourceRevision: repeat('b', 40), Assets: []verification.Asset{{Path: "result.txt", SHA256: assetHash, Role: "deliverable"}}}
	cd, _ := verification.Digest(manifest)
	snap, _ := verification.Digest([]struct {
		Path, SHA  string
		Executable bool
	}{{"result.txt", assetHash, false}})
	sourceRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(sourceRoot, "result.txt"), assetBytes, 0644); err != nil {
		t.Fatal(err)
	}
	storeRoot := filepath.Join(t.TempDir(), "candidate-store")
	if err := os.MkdirAll(storeRoot, 0700); err != nil {
		t.Fatal(err)
	}
	bundlePath := filepath.Join(storeRoot, manifest.ID)
	candidate, e := verification.ImportCandidate(manifest, sourceRoot, bundlePath)
	if e != nil {
		t.Fatal(e)
	}
	cd = candidate.Digest
	snap = candidate.SnapshotSHA256
	acceptPub, acceptPriv, _ := ed25519.GenerateKey(rand.Reader)
	reviewPub, reviewPriv, _ := ed25519.GenerateKey(rand.Reader)
	policy := verification.VerificationPolicy{Version: verification.Version, ID: "policy-one", MaxAgeSeconds: 300, AuthorBindings: []verification.AuthorBinding{{CandidateSHA256: cd, Principal: "author@example"}}, Criteria: []verification.Criterion{{ID: "asset", Kind: "asset-present", Required: true, Expected: "result.txt"}}, Issuers: []verification.TrustedIssuer{{ID: "accept-issuer", Role: "acceptance", PublicKey: base64.StdEncoding.EncodeToString(acceptPub)}, {ID: "review-issuer", Role: "reviewer", Principal: "reviewer@example", PublicKey: base64.StdEncoding.EncodeToString(reviewPub)}}}
	pd, _ := verification.Digest(policy)
	now := time.Now().UTC().Truncate(time.Second)
	accept, acceptErr := verification.IssueAcceptance(candidate, policy, nil, nil, "accept-issuer", now, now.Add(2*time.Minute).Format(time.RFC3339), acceptPriv)
	if acceptErr != nil {
		t.Fatal(acceptErr)
	}
	acceptBytes, _ := json.Marshal(accept)
	review := verification.ReviewReceipt{Version: verification.Version, ID: "review-one", CandidateSHA256: cd, SnapshotSHA256: snap, PolicySHA256: pd, Principal: "reviewer@example", Disposition: "approved", Issuer: "review-issuer", ObservedAt: now.Format(time.RFC3339)}
	sigBytes, _ := json.Marshal(review)
	review.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(reviewPriv, sigBytes))
	engineSnapshot := object{"schema_version": json.Number("1"), "kind": "source-snapshot", "repository": "/repo", "head": manifest.SourceRevision, "index_sha256": repeat('f', 64), "files": []any{object{"path": "result.txt", "type": "file", "sha256": assetHash, "mode": json.Number("420")}}, "included_ignored": []any{}, "snapshot_sha256": ""}
	engineSnapshot["snapshot_sha256"] = sealRecord(t, engineSnapshot, "snapshot_sha256")
	ss := engineSnapshot["snapshot_sha256"].(string)
	snapshotBytes, _ := json.Marshal(engineSnapshot)
	context := object{"schema_version": json.Number("1"), "kind": "delivery-context", "repository": "/repo", "baseline_revision": manifest.BaseRevision, "objective": "Preserve and hand off candidate", "files_in_scope": []any{"result.txt"}, "gate_commands": []any{[]any{"go", "test"}}, "review_policy": "independent", "authorized_actions": []any{"isolate"}, "actor_id": "author@example", "actor_context": "run-1", "environment_keys": []any{}, "include_ignored": []any{}, "preservation": []any{}, "gate_timeout_seconds": json.Number("60"), "github_repository": nil, "github_review_required": true, "integration_required_mode": nil, "initial_snapshot_sha256": snap, "context_sha256": ""}
	context["context_sha256"] = sealRecord(t, context, "context_sha256")
	env := object{"platform": "fixture-os", "python": "Python fixture", "git": "git version fixture", "selected_variables": object{}}
	gate := object{"schema_version": json.Number("1"), "kind": "gate-receipt", "context_sha256": context["context_sha256"], "snapshot_sha256": ss, "environment": env, "source_unchanged": true, "passed": true, "results": []any{object{"argv": []any{"go", "test"}, "returncode": json.Number("0"), "timed_out": false, "stdout_sha256": repeat('c', 64), "stderr_sha256": repeat('d', 64), "duration_seconds": json.Number("0.25")}}, "receipt_sha256": ""}
	gate["receipt_sha256"] = sealRecord(t, gate, "receipt_sha256")
	engineReview := object{"schema_version": json.Number("1"), "kind": "review-receipt", "context_sha256": context["context_sha256"], "reviewer": "reviewer@example", "reviewer_context": "review-session", "snapshot_sha256": ss, "outcome": "approved", "report": "review report hash", "receipt_sha256": ""}
	engineReview["receipt_sha256"] = sealRecord(t, engineReview, "receipt_sha256")
	ctxBytes, _ := json.Marshal(context)
	gateBytes, _ := json.Marshal(gate)
	reviewBytes, _ := json.Marshal(engineReview)
	return HandoffManifest{Version: Version, ID: "handoff-one", Candidate: candidate, Policy: policy, Acceptance: accept, AcceptanceBytes: acceptBytes, AsOf: now.Add(time.Second), IndependentReview: review, SourceSnapshotSHA256: ss, DestinationSnapshotSHA256: ss, SourceSnapshotBytes: snapshotBytes, DestinationSnapshotBytes: snapshotBytes, ContextBytes: ctxBytes, GateBytes: gateBytes, ReviewBytes: reviewBytes}
}

func sealRecord(t *testing.T, m object, field string) string {
	t.Helper()
	payload := object{}
	for k, v := range m {
		if k != field {
			payload[k] = v
		}
	}
	b, e := pythonCanonical(payload)
	if e != nil {
		t.Fatal(e)
	}
	return shaHex(b)
}
func repeat(c byte, n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = c
	}
	return string(b)
}

func TestValidateCreatesOnlyNonAuthorizingLocalDraft(t *testing.T) {
	m := testFixture(t)
	draft, r := Validate(m)
	if !r.Valid {
		t.Fatalf("invalid fixture: %v", r.Diagnostics)
	}
	if draft.AuthorizationGranted || draft.ExecutionReady || r.AuthorizationGranted || r.ExecutionReady {
		t.Fatal("draft granted authority")
	}
	if len(draft.Actions) != 2 || draft.Actions[0] != "preserve" || draft.Actions[1] != "prepare-isolated-handoff" {
		t.Fatalf("unexpected actions: %v", draft.Actions)
	}
	if len(draft.FilesInScope) != 1 || draft.FilesInScope[0] != "result.txt" {
		t.Fatalf("unexpected file scope: %v", draft.FilesInScope)
	}
}

func TestValidatesEmbeddedIntegrationProofAndRequiredMode(t *testing.T) {
	m := testFixture(t)
	var c object
	_ = strictDecode(m.ContextBytes, &c)
	c["integration_required_mode"] = "mock"
	c["context_sha256"] = ""
	c["context_sha256"] = sealRecord(t, c, "context_sha256")
	var g object
	_ = strictDecode(m.GateBytes, &g)
	g["context_sha256"] = c["context_sha256"]
	g["receipt_sha256"] = ""
	g["receipt_sha256"] = sealRecord(t, g, "receipt_sha256")
	var review object
	_ = strictDecode(m.ReviewBytes, &review)
	review["context_sha256"] = c["context_sha256"]
	review["receipt_sha256"] = ""
	review["receipt_sha256"] = sealRecord(t, review, "receipt_sha256")
	proof := object{"schema_version": json.Number("1"), "kind": "integration-proof", "context_sha256": c["context_sha256"], "snapshot_sha256": m.DestinationSnapshotSHA256, "environment": g["environment"], "specification": object{"required_mode": "mock", "services": []any{object{"name": "database", "mode": "mock", "version": "fixture-v1"}}, "timeout_seconds": json.Number("5")}, "mode": "mock", "services": []any{object{"name": "database", "mode": "mock", "endpoint": nil, "observed_version": "fixture-v1", "response_sha256": nil, "available": true}}, "gates_executed": true, "gates": g, "source_unchanged": true, "passed": true, "blockers": []any{}, "receipt_sha256": ""}
	proof["receipt_sha256"] = sealRecord(t, proof, "receipt_sha256")
	m.ContextBytes, _ = json.Marshal(c)
	m.GateBytes, _ = json.Marshal(g)
	m.ReviewBytes, _ = json.Marshal(review)
	m.IntegrationBytes, _ = json.Marshal(proof)
	draft, r := Validate(m)
	if !r.Valid {
		t.Fatalf("valid integration proof rejected: %v", r.Diagnostics)
	}
	if draft.EngineIntegrationSHA256 == "" {
		t.Fatal("integration proof digest missing")
	}
}

func TestValidateRejectsTamperedReceiptsChangedDestinationAndMissingReview(t *testing.T) {
	t.Run("tampered-gate", func(t *testing.T) {
		m := testFixture(t)
		var g object
		_ = strictDecode(m.GateBytes, &g)
		g["passed"] = false
		m.GateBytes, _ = json.Marshal(g)
		_, r := Validate(m)
		if r.Valid {
			t.Fatal("tampered sealed gate accepted")
		}
	})
	t.Run("destination-changed", func(t *testing.T) {
		m := testFixture(t)
		m.DestinationSnapshotSHA256 = repeat('e', 64)
		_, r := Validate(m)
		if r.Valid {
			t.Fatal("changed destination accepted without new acceptance")
		}
	})
	t.Run("missing-authenticated-review", func(t *testing.T) {
		m := testFixture(t)
		m.IndependentReview = verification.ReviewReceipt{}
		_, r := Validate(m)
		if r.Valid {
			t.Fatal("missing authenticated review accepted")
		}
	})
	t.Run("publication-action", func(t *testing.T) {
		m := testFixture(t)
		var c object
		_ = strictDecode(m.ContextBytes, &c)
		c["authorized_actions"] = []any{"push"}
		c["context_sha256"] = ""
		c["context_sha256"] = sealRecord(t, c, "context_sha256")
		m.ContextBytes, _ = json.Marshal(c)
		var g object
		_ = strictDecode(m.GateBytes, &g)
		g["context_sha256"] = c["context_sha256"]
		g["receipt_sha256"] = ""
		g["receipt_sha256"] = sealRecord(t, g, "receipt_sha256")
		m.GateBytes, _ = json.Marshal(g)
		var rv object
		_ = strictDecode(m.ReviewBytes, &rv)
		rv["context_sha256"] = c["context_sha256"]
		rv["receipt_sha256"] = ""
		rv["receipt_sha256"] = sealRecord(t, rv, "receipt_sha256")
		m.ReviewBytes, _ = json.Marshal(rv)
		_, r := Validate(m)
		if r.Valid {
			t.Fatal("publishing action passed bridge")
		}
	})
}

func TestEngineSealMatchesPythonFixture(t *testing.T) {
	const raw = `{"alpha":"snowman \u2603","duration":0.25,"empty":{},"items":[1,true,null],"context_sha256":"bc946dc16b7dee83424c83c99c9a9acb1271c89ec1b3bdee896bea62577636c5"}`
	if _, e := decodeRecord([]byte(raw), "context_sha256"); e != nil {
		t.Fatal(e)
	}
}

func TestDeliveryReviewRequiresProtectedAuthorIdentity(t *testing.T) {
	m := testFixture(t)
	m.Policy.AuthorBindings = nil
	if err := verifyIndependentReview(m.IndependentReview, m.Candidate, m.Policy, m.AsOf); err == nil {
		t.Fatal("self-asserted author identity satisfied independent review")
	}
}
