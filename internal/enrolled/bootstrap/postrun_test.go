package bootstrap

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/runtime"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/runtime/records"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/runtime/staging"
	ledger "github.com/ArmanAvanesyan/agent-operating-kit/internal/runtime/store"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
	accounting "github.com/ArmanAvanesyan/agent-operating-kit/internal/usage/store"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/verification"
)

type postRunTransport func(*http.Request) (*http.Response, error)

type postRunFixtureLineage struct{}

func (postRunFixtureLineage) VerifyCandidateLineage(m verification.CandidateManifest) error {
	if m.Origin != "runtime" || m.RunID != "run-7" || m.StepID != "step-2" || m.AttemptID != "attempt-3" {
		return errors.New("fixture lineage mismatch")
	}
	return nil
}

func (f postRunTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func postRunResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func postRunFixture(t *testing.T) (verification.Candidate, verification.VerificationPolicy, verification.ControlProof) {
	t.Helper()
	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	asset := []byte("candidate created after host run\n")
	if err := os.WriteFile(filepath.Join(source, "answer.txt"), asset, 0600); err != nil {
		t.Fatal(err)
	}
	assetHash := sha256.Sum256(asset)
	inputHash := sha256.Sum256([]byte("frozen-input"))
	lineage := func(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
	manifest := verification.CandidateManifest{Version: verification.Version, ID: "run-result", Origin: "runtime", Repository: "fixture/repo", BaseRevision: "base", SourceRevision: "head", PlanSHA256: lineage("plan"), GoalSHA256: lineage("goal"), AssignmentSHA256: lineage("assignment"), InputSHA256: hex.EncodeToString(inputHash[:]), ConfigurationSHA256: lineage("configuration"), ProfileSHA256: lineage("profile"), PreparedReceiptSHA256: lineage("prepared"), HostReceiptSHA256: lineage("host"), CheckpointSHA256: lineage("checkpoint"), AuthorPrincipal: "fixture-agent", RunID: "run-7", StepID: "step-2", AttemptID: "attempt-3", Fence: 2, CandidateRevision: 1, Quiescent: true, Assets: []verification.Asset{{Path: "answer.txt", SHA256: hex.EncodeToString(assetHash[:]), Role: "deliverable"}}}
	candidate, err := verification.ImportRuntimeCandidate(manifest, source, filepath.Join(root, "candidate"), postRunFixtureLineage{})
	if err != nil {
		t.Fatal(err)
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	proofKey := sha256.Sum256([]byte("proof-issuer-key"))
	specKey := sha256.Sum256([]byte("spec"))
	spec := verification.VerifierSpec{ID: "command", Kind: "command", ImplementationSHA256: hex.EncodeToString(specKey[:]), RubricSHA256: hex.EncodeToString(specKey[:]), Argv: []string{"/bin/true"}, Environment: map[string]string{}, TimeoutSeconds: 10, MaxOutputBytes: 1024}
	policy := verification.VerificationPolicy{Version: verification.Version, ID: "postrun-policy", MaxAgeSeconds: 300, Issuers: []verification.TrustedIssuer{{ID: "controls", Role: "controls", PublicKey: base64.StdEncoding.EncodeToString(pub), ExecutorSHA256: hex.EncodeToString(proofKey[:])}}, Verifiers: []verification.VerifierSpec{spec}, Criteria: []verification.Criterion{{ID: "command-result", Kind: "command", Required: true, VerifierID: spec.ID}}}
	policyDigest, _ := verification.Digest(policy)
	specDigest, _ := verification.Digest(spec)
	now := time.Now().UTC().Truncate(time.Second)
	proof, err := verification.SignControlProof(verification.ControlProof{Version: verification.Version, PolicySHA256: policyDigest, VerifierSHA256: specDigest, CandidateSHA256: candidate.Digest, SnapshotSHA256: candidate.SnapshotSHA256, Scope: "candidate-offline", ExecutorSHA256: hex.EncodeToString(proofKey[:]), ObservedAt: now.Format(time.RFC3339), ValidUntil: now.Add(time.Minute).Format(time.RFC3339), Controls: []string{"filesystem-isolation", "readonly-candidate", "network-deny", "credential-isolation", "time-bound", "output-bound"}, Issuer: "controls"}, priv)
	if err != nil {
		t.Fatal(err)
	}
	return candidate, policy, proof
}

// postRunLedgerCandidate is deliberately assembled through the real P7 store,
// checkpoint, capture and controller lineage APIs. The verifier tests never
// rely on a fake lineage implementation.
func postRunLedgerCandidate(t *testing.T) (verification.Candidate, string) {
	t.Helper()
	ctx := context.Background()
	base := t.TempDir()
	runtimeRoot := filepath.Join(base, "runtime")
	workspace := filepath.Join(base, "workspace")
	if err := os.Mkdir(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	body := []byte("real runtime candidate\n")
	if err := os.WriteFile(filepath.Join(workspace, "answer.txt"), body, 0600); err != nil {
		t.Fatal(err)
	}
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", workspace}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=PostRun Test", "GIT_AUTHOR_EMAIL=postrun@example.invalid", "GIT_COMMITTER_NAME=PostRun Test", "GIT_COMMITTER_EMAIL=postrun@example.invalid")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	runGit("init", "-q")
	runGit("add", "answer.txt")
	runGit("commit", "-q", "-m", "base")
	cmd := exec.Command("git", "-C", workspace, "rev-parse", "HEAD")
	gitHead, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	inputRevision := strings.TrimSpace(string(gitHead))
	planRaw, err := os.ReadFile(filepath.Join("..", "..", "planning", "testdata", "corpus", "plan.json"))
	if err != nil {
		t.Fatal(err)
	}
	goalRaw, err := os.ReadFile(filepath.Join("..", "..", "planning", "testdata", "corpus", "goal.json"))
	if err != nil {
		t.Fatal(err)
	}
	assignmentRaw := []byte("postrun fixture assignment\n")
	now := time.Now().UTC().Truncate(time.Second)
	policyRuntime := records.Policy{Version: records.Version, Authority: "postrun-test", MaxConcurrent: 1, LeaseSeconds: 30, HeartbeatSeconds: 5, MaxAttempts: 2, StagingRoot: filepath.Join(base, "staging"), AccountingPath: filepath.Join(base, "accounting.db"), RepositoryRoots: []string{base}}
	s, err := ledger.Create(runtimeRoot, policyRuntime, "postrun-runtime-policy")
	if err != nil {
		t.Fatal(err)
	}
	manifest := records.Manifest{Version: records.Version, ID: "run-postrun", Repository: "fixture/repo", InputRevision: inputRevision, Corpus: "corpus", Plan: "corpus/plan.json", Evidence: "evidence.json", Assets: []records.Asset{{Path: "corpus/plan.json", SHA256: usage.Hash(planRaw)}, {Path: "corpus/goal.json", SHA256: usage.Hash(goalRaw)}, {Path: "assignment.json", SHA256: usage.Hash(assignmentRaw)}}, Steps: []records.StepSpec{{ID: "step-postrun", Assignment: "assignment.json"}}}
	manifestBytes, _ := json.Marshal(manifest)
	manifestSHA := usage.Hash(manifestBytes)
	if _, err = s.Submit(ctx, "postrun-submit", manifest, manifestSHA, now); err != nil {
		t.Fatal(err)
	}
	putRuntimeFixture(t, filepath.Join(runtimeRoot, "runs", manifest.ID, "inputs", "corpus", "plan.json"), planRaw)
	putRuntimeFixture(t, filepath.Join(runtimeRoot, "runs", manifest.ID, "inputs", "corpus", "goal.json"), goalRaw)
	putRuntimeFixture(t, filepath.Join(runtimeRoot, "runs", manifest.ID, "inputs", "assignment.json"), assignmentRaw)
	token, err := s.Claim(ctx, "postrun-claim", manifest.ID, "step-postrun", "fixture-worker", "attempt-postrun", now)
	if err != nil {
		t.Fatal(err)
	}
	cp, err := staging.Checkpoint(ctx, workspace, token.AttemptID, inputRevision, false)
	if err != nil {
		t.Fatal(err)
	}
	demand := usage.Demand{Version: usage.Version, ConfigurationID: usage.Hash([]byte("configuration")), ProfileSHA256: usage.Hash([]byte("profile")), Source: "selection", ValidUntil: now.Add(time.Minute).Format(time.RFC3339Nano), Amounts: []usage.Amount{{ConstraintID: usage.Hash([]byte("constraint")), Generation: "all", Unit: "requests", Amount: "1"}}}
	hold := &usage.Reservation{ID: "postrun-hold", AuthorityEpoch: "epoch", Owner: "owner", AttemptID: token.AttemptID, State: "in-flight", Fence: 1}
	step := records.Step{ID: "step-postrun", Token: token, Attempts: 1, Workspace: cp.Workspace, Checkpoint: &cp, Reservation: hold, AccountingToken: &accounting.Token{ID: hold.ID, Owner: hold.Owner, Epoch: hold.AuthorityEpoch, Fence: hold.Fence}, DecisionSHA256: usage.Hash([]byte("decision")), PreparedSHA256: usage.Hash([]byte("prepared")), Demand: &demand}
	for i, phase := range []string{"staged", "reservation-intent", "reserved", "launch-intent", "running"} {
		if phase == "launch-intent" {
			step.LaunchPossible = true
		}
		step, err = s.Update(ctx, "postrun-transition-"+phase, token, phase, step, now.Add(time.Duration(i+1)*time.Second))
		if err != nil {
			t.Fatal(err)
		}
	}
	cp, err = staging.Checkpoint(ctx, workspace, token.AttemptID, inputRevision, true)
	if err != nil {
		t.Fatal(err)
	}
	step.Checkpoint, step.StopObserved, step.HostReceiptSHA256 = &cp, true, usage.Hash([]byte("host-receipt"))
	step, err = s.Update(ctx, "postrun-awaiting-verification", token, "awaiting-verification", step, now.Add(6*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	acceptPub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	projectionPub, projectionPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	policy := verification.VerificationPolicy{Version: verification.Version, ID: "postrun-lineage-policy", MaxAgeSeconds: 300, Criteria: []verification.Criterion{{ID: "deliverable", Kind: "asset-present", Required: true, Expected: "answer.txt"}}, AuthorBindings: []verification.AuthorBinding{{AssignmentSHA256: usage.Hash(assignmentRaw), Principal: "fixture-worker"}}, Issuers: []verification.TrustedIssuer{{ID: "acceptor", Role: "acceptance", PublicKey: base64.StdEncoding.EncodeToString(acceptPub)}, {ID: "projection", Role: records.AcceptanceAuthorityRole, PublicKey: base64.StdEncoding.EncodeToString(projectionPub)}}}
	ctrl := &runtime.Controller{Root: runtimeRoot, Store: s, Policy: policyRuntime, Clock: postRunFixedClock{now}}
	policySHA, _ := verification.Digest(policy)
	authority := records.AcceptanceAuthorityState{Version: records.AcceptanceAuthorityVersion, PolicySHA256: policySHA, TrustGeneration: 1, RevocationGeneration: 1, ObservedAt: now.Format(time.RFC3339Nano), ValidUntil: now.Add(4 * time.Minute).Format(time.RFC3339Nano), Issuer: "projection"}
	unsigned, _ := json.Marshal(authority)
	authority.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(projectionPriv, unsigned))
	if err := ctrl.ConfigureAcceptanceAuthority(ctx, policy, authority); err != nil {
		t.Fatal(err)
	}
	state, err := s.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	assets := make([]verification.Asset, len(cp.Assets))
	for i, a := range cp.Assets {
		assets[i] = verification.Asset{Path: a.Path, SHA256: a.SHA256, Executable: a.Executable, Role: "deliverable"}
	}
	cm := verification.CandidateManifest{Version: verification.Version, ID: "postrun-ledger-candidate", Origin: "runtime", Repository: manifest.Repository, BaseRevision: inputRevision, SourceRevision: cp.BaseRevision, PlanSHA256: usage.Hash(planRaw), GoalSHA256: usage.Hash(goalRaw), AssignmentSHA256: usage.Hash(assignmentRaw), InputSHA256: manifestSHA, ConfigurationSHA256: demand.ConfigurationID, ProfileSHA256: demand.ProfileSHA256, PreparedReceiptSHA256: step.PreparedSHA256, HostReceiptSHA256: step.HostReceiptSHA256, CheckpointSHA256: cp.Digest, AuthorPrincipal: "fixture-worker", RunID: manifest.ID, StepID: step.ID, AttemptID: token.AttemptID, Fence: uint64(token.Fence), CandidateRevision: uint64(state.Runs[0].Revision), Quiescent: true, Assets: assets}
	candidate, err := ctrl.CaptureCandidate(ctx, cm, workspace, filepath.Join(base, "candidate"))
	if err != nil {
		t.Fatalf("capture ledger-backed candidate: %v", err)
	}
	if err := ctrl.VerifyCandidateLineage(candidate.Manifest); err != nil {
		t.Fatalf("fixture lineage invalid: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	return candidate, runtimeRoot
}

type postRunFixedClock struct{ at time.Time }

func (c postRunFixedClock) Now() time.Time { return c.at }
func putRuntimeFixture(t *testing.T, path string, body []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
}

func postRunGrant(candidate verification.Candidate, proof verification.ControlProof) *PostRunExecutionGrant {
	now := time.Now().UTC().Truncate(time.Second)
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	g := &PostRunExecutionGrant{Version: PostRunExecutionVersion, Purpose: "post-run-verification-only", ParentID: "pilot-parent", OperationID: "verify-run-7-attempt-3", IssuedAt: now.Format(time.RFC3339Nano), ExpiresAt: now.Add(4 * time.Minute).Format(time.RFC3339Nano), CandidateSHA256: candidate.Digest, ProofSHA256: mustDigest(proof), AnchorEndpoint: "https://anchor.fixture.test/v1/anchor", AnchorPublicKey: base64.StdEncoding.EncodeToString(pub), AnchorTokenRef: "anchor-token", ExpectedTipSHA256: strings.Repeat("e", 64)}
	g.AnchorStreamID = postRunStream(*g)
	return g
}

func postRunVerifierForCandidate(t *testing.T, candidate verification.Candidate) (verification.VerificationPolicy, verification.ControlProof, records.AcceptanceAuthorityState) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	projectionPub, projectionPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	executor := sha256.Sum256([]byte("post-run test verifier executor"))
	specHash := sha256.Sum256([]byte("post-run test verifier"))
	spec := verification.VerifierSpec{ID: "command", Kind: "command", ImplementationSHA256: hex.EncodeToString(specHash[:]), RubricSHA256: hex.EncodeToString(specHash[:]), Argv: []string{"/bin/true"}, Environment: map[string]string{}, TimeoutSeconds: 10, MaxOutputBytes: 1024}
	policy := verification.VerificationPolicy{Version: verification.Version, ID: "postrun-policy", MaxAgeSeconds: 300, AuthorBindings: []verification.AuthorBinding{{AssignmentSHA256: candidate.Manifest.AssignmentSHA256, Principal: candidate.Manifest.AuthorPrincipal}}, Issuers: []verification.TrustedIssuer{{ID: "controls", Role: "controls", PublicKey: base64.StdEncoding.EncodeToString(pub), ExecutorSHA256: hex.EncodeToString(executor[:])}, {ID: "projection", Role: records.AcceptanceAuthorityRole, PublicKey: base64.StdEncoding.EncodeToString(projectionPub)}}, Verifiers: []verification.VerifierSpec{spec}, Criteria: []verification.Criterion{{ID: "command-result", Kind: "command", Required: true, VerifierID: spec.ID}}}
	pd, err := verification.Digest(policy)
	if err != nil {
		t.Fatal(err)
	}
	sd, err := verification.Digest(spec)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	proof, err := verification.SignControlProof(verification.ControlProof{Version: verification.Version, PolicySHA256: pd, VerifierSHA256: sd, CandidateSHA256: candidate.Digest, SnapshotSHA256: candidate.SnapshotSHA256, Scope: "candidate-offline", ExecutorSHA256: hex.EncodeToString(executor[:]), ObservedAt: now.Format(time.RFC3339), ValidUntil: now.Add(time.Minute).Format(time.RFC3339), Controls: []string{"filesystem-isolation", "readonly-candidate", "network-deny", "credential-isolation", "time-bound", "output-bound"}, Issuer: "controls"}, priv)
	if err != nil {
		t.Fatal(err)
	}
	authority := records.AcceptanceAuthorityState{Version: records.AcceptanceAuthorityVersion, PolicySHA256: pd, TrustGeneration: 2, RevocationGeneration: 2, ObservedAt: now.Format(time.RFC3339Nano), ValidUntil: now.Add(4 * time.Minute).Format(time.RFC3339Nano), Issuer: "projection"}
	authorityBytes, _ := json.Marshal(authority)
	authority.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(projectionPriv, authorityBytes))
	return policy, proof, authority
}

func TestPostRunBindsFreshCandidateAndRejectsOldProof(t *testing.T) {
	candidate, policy, proof := postRunFixture(t)
	input := candidate.Manifest.InputSHA256
	r := &PostRunRecords{CandidatePath: candidate.BundlePath, StepID: candidate.Manifest.StepID, AttemptID: candidate.Manifest.AttemptID, CandidateInputSHA256: input, Policy: policy}
	if err := validatePostRunCandidate(candidate, "run-7", r); err != nil {
		t.Fatalf("actual newly produced candidate rejected: %v", err)
	}
	old := proof
	old.CandidateSHA256 = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if old.CandidateSHA256 == candidate.Digest || old.SnapshotSHA256 != candidate.SnapshotSHA256 {
		t.Fatal("fixture did not model an old candidate-bound proof")
	}
	if old.CandidateSHA256 == candidate.Digest {
		t.Fatal("stale proof candidate accepted")
	}

	changed := candidate
	changed.Manifest.StepID = "different-step"
	if validatePostRunCandidate(changed, "run-7", r) == nil {
		t.Fatal("changed candidate lineage accepted")
	}
	changed = candidate
	changed.Manifest.AttemptID = "different-attempt"
	if validatePostRunCandidate(changed, "run-7", r) == nil {
		t.Fatal("changed attempt accepted")
	}
	changed = candidate
	changed.Manifest.Origin = "fixture"
	if validatePostRunCandidate(changed, "run-7", r) == nil {
		t.Fatal("non-runtime fixture candidate accepted for post-run")
	}
}

func TestPostRunUploadsActualCandidateThenRunsOnlyBoundVerifier(t *testing.T) {
	candidate, runtimeRoot := postRunLedgerCandidate(t)
	policy, proof, authority := postRunVerifierForCandidate(t, candidate)
	dir := t.TempDir()
	policyPath := filepath.Join(dir, "policy.json")
	policyBytes, _ := json.Marshal(policy)
	if err := os.WriteFile(policyPath, policyBytes, 0600); err != nil {
		t.Fatal(err)
	}
	policyDigest, _ := verification.Digest(policy)
	r := &PostRunRecords{Stage: "verify-candidate", CandidatePath: candidate.BundlePath, PolicyPath: policyPath, RuntimeRoot: runtimeRoot, StepID: candidate.Manifest.StepID, AttemptID: candidate.Manifest.AttemptID, CandidateInputSHA256: candidate.Manifest.InputSHA256, Policy: policy, PolicySHA256: policyDigest, AcceptanceAuthority: authority, Proof: proof, Execution: postRunGrant(candidate, proof), AuthReference: "verifier-token", OutputPath: filepath.Join(dir, "report")}
	w := &WorkerConfig{RunID: candidate.Manifest.RunID, Mode: "verify-candidate", PostRun: r, Verifier: WorkerVerifier{Endpoint: "https://verifier.fixture.test/v1/execute"}}
	c := Config{Version: ConfigVersion, Service: "worker", Worker: w, SecretEnv: map[string]string{"verifier-token": "AOK_POSTRUN_TEST_TOKEN", "anchor-token": "AOK_POSTRUN_ANCHOR_TOKEN"}}
	t.Setenv("AOK_POSTRUN_TEST_TOKEN", "fixture-bearer")
	t.Setenv("AOK_POSTRUN_ANCHOR_TOKEN", "fixture-anchor")
	var paths []string
	proofDigest, err := verification.Digest(proof)
	if err != nil {
		t.Fatal(err)
	}
	transport := postRunTransport(func(req *http.Request) (*http.Response, error) {
		paths = append(paths, req.URL.Path)
		if req.Header.Get("Authorization") != "Bearer fixture-bearer" {
			t.Fatal("protected credential missing")
		}
		switch req.URL.Path {
		case "/v1/candidates":
			if req.Header.Get("X-AOK-Candidate-SHA256") != candidate.Digest || req.Header.Get("X-AOK-Snapshot-SHA256") != candidate.SnapshotSHA256 {
				t.Fatal("actual candidate identity was not uploaded")
			}
			if req.Header.Get("X-AOK-Control-Proof") == "" {
				t.Fatal("candidate proof missing on upload")
			}
			_, _ = io.Copy(io.Discard, req.Body)
			b, _ := json.Marshal(map[string]string{"candidateSha256": candidate.Digest, "snapshotSha256": candidate.SnapshotSHA256})
			return postRunResponse(http.StatusCreated, string(b)), nil
		case "/v1/execute":
			var q map[string]any
			if json.NewDecoder(req.Body).Decode(&q) != nil || q["candidateSha256"] != candidate.Digest || q["snapshotSha256"] != candidate.SnapshotSHA256 || q["proofSha256"] != proofDigest {
				t.Fatal("verifier invocation lost candidate proof binding")
			}
			b, _ := json.Marshal(map[string]any{"protocol": verification.RemoteProtocol, "proofSha256": proofDigest, "exitCode": 0, "stdoutBase64": base64.StdEncoding.EncodeToString([]byte("ok"))})
			return postRunResponse(http.StatusOK, string(b)), nil
		default:
			t.Fatalf("unexpected remote path %s", req.URL.Path)
			return nil, nil
		}
	})
	anchor := &qualificationTestAnchor{tip: r.Execution.ExpectedTipSHA256}
	run, err := buildPostRunWorkerWithDependencies(c, w, transport, anchor)
	if err != nil {
		t.Fatalf("build post-run worker: %v", err)
	}
	defer run.Close()
	var out, errout bytes.Buffer
	if code := run.Run(context.Background(), &out, &errout); code != 0 {
		t.Fatalf("post-run code=%d paths=%v stdout=%s stderr=%s", code, paths, out.String(), errout.String())
	}
	if len(paths) != 2 || paths[0] != "/v1/candidates" || paths[1] != "/v1/execute" {
		t.Fatalf("upload/execute sequence %v", paths)
	}
	if strings.Contains(out.String(), `"accepted":true`) || !strings.Contains(out.String(), "invocation-observed") {
		t.Fatalf("zero-exit report implied acceptance: %s", out.String())
	}
	before := len(paths)
	if code := run.Run(context.Background(), &out, &errout); code == 0 {
		t.Fatal("single-use execution grant replay succeeded")
	}
	if len(paths) != before {
		t.Fatalf("replay reached remote verifier: %v", paths)
	}
	renewed := *r.Execution
	renewed.ExpectedTipSHA256 = postRunConsumeDigest(renewed)
	r.Execution = &renewed
	if _, err := buildPostRunWorkerWithDependencies(c, w, transport, anchor); err == nil {
		t.Fatal("same parent/operation restarted from consumed stable tip")
	}
}

func TestPostRunRejectsStaleProofAndCanceledRunDoesNotCallRemote(t *testing.T) {
	candidate, runtimeRoot := postRunLedgerCandidate(t)
	policy, proof, authority := postRunVerifierForCandidate(t, candidate)
	dir := t.TempDir()
	policyPath := filepath.Join(dir, "policy.json")
	pb, _ := json.Marshal(policy)
	_ = os.WriteFile(policyPath, pb, 0600)
	pd, _ := verification.Digest(policy)
	r := &PostRunRecords{Stage: "verify-candidate", CandidatePath: candidate.BundlePath, PolicyPath: policyPath, RuntimeRoot: runtimeRoot, StepID: candidate.Manifest.StepID, AttemptID: candidate.Manifest.AttemptID, CandidateInputSHA256: candidate.Manifest.InputSHA256, Policy: policy, PolicySHA256: pd, AcceptanceAuthority: authority, Proof: proof, Execution: postRunGrant(candidate, proof), AuthReference: "ref", OutputPath: filepath.Join(dir, "report")}
	w := &WorkerConfig{RunID: candidate.Manifest.RunID, Mode: "verify-candidate", PostRun: r, Verifier: WorkerVerifier{Endpoint: "https://verifier.fixture.test/v1/execute"}}
	c := Config{Version: ConfigVersion, Service: "worker", Worker: w, SecretEnv: map[string]string{"ref": "AOK_POSTRUN_CANCEL_TOKEN", "anchor-token": "AOK_POSTRUN_ANCHOR_TOKEN"}}
	t.Setenv("AOK_POSTRUN_CANCEL_TOKEN", "token")
	t.Setenv("AOK_POSTRUN_ANCHOR_TOKEN", "anchor")
	stale := *r
	stale.Proof.CandidateSHA256 = strings.Repeat("a", 64)
	w.PostRun = &stale
	if _, err := buildPostRunWorkerWithDependencies(c, w, postRunTransport(func(*http.Request) (*http.Response, error) { t.Fatal("stale proof reached remote"); return nil, nil }), &qualificationTestAnchor{tip: r.Execution.ExpectedTipSHA256}); err == nil {
		t.Fatal("old candidate proof accepted")
	}
	w.PostRun = r
	anchor := &qualificationTestAnchor{tip: r.Execution.ExpectedTipSHA256}
	r.OutputPath = filepath.Join(runtimeRoot, "output-alias")
	if _, err := buildPostRunWorkerWithDependencies(c, w, postRunTransport(func(*http.Request) (*http.Response, error) {
		t.Fatal("runtime output alias reached remote")
		return nil, nil
	}), anchor); err == nil {
		t.Fatal("runtime ledger root accepted as an output parent")
	}
	r.OutputPath = filepath.Join(dir, "report")
	calls := 0
	run, err := buildPostRunWorkerWithDependencies(c, w, postRunTransport(func(*http.Request) (*http.Response, error) { calls++; return nil, errors.New("unexpected remote call") }), anchor)
	if err != nil {
		t.Fatal(err)
	}
	defer run.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if code := run.Run(ctx, io.Discard, io.Discard); code == 0 {
		t.Fatal("canceled invocation succeeded")
	}
	if calls != 0 {
		t.Fatalf("canceled verifier made %d remote calls", calls)
	}
	anchor.loseAdvanceResponse = true
	if code := run.Run(context.Background(), io.Discard, io.Discard); code == 0 {
		t.Fatal("uncertain anchor consume succeeded")
	}
	if calls != 0 {
		t.Fatalf("uncertain consume reached verifier %d times", calls)
	}
	anchor.loseAdvanceResponse = false
	if code := run.Run(context.Background(), io.Discard, io.Discard); code == 0 || calls != 0 {
		t.Fatalf("lost consume was retried or reached verifier: code=%d calls=%d", code, calls)
	}
}

func TestPostRunRejectsChangedBundleAndForgedAcceptance(t *testing.T) {
	candidate, policy, _ := postRunFixture(t)
	path := filepath.Join(candidate.BundlePath, "answer.txt")
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("changed after candidate capture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := verification.LoadCandidate(candidate.BundlePath); err == nil {
		t.Fatal("changed candidate bytes accepted")
	}

	receipt := verification.AcceptanceReceipt{Version: verification.Version, CandidateSHA256: candidate.Digest, SnapshotSHA256: candidate.SnapshotSHA256, RunID: candidate.Manifest.RunID, StepID: candidate.Manifest.StepID, AttemptID: candidate.Manifest.AttemptID, InputSHA256: candidate.Manifest.InputSHA256, Disposition: "accepted", Issuer: "acceptance", IssuedAt: time.Now().UTC().Format(time.RFC3339), ValidUntil: time.Now().UTC().Add(time.Minute).Format(time.RFC3339), Signature: "forged"}
	if verification.VerifyAcceptance(receipt, policy.Issuers, time.Now().UTC(), time.Minute) == nil {
		t.Fatal("forged external acceptance receipt accepted")
	}
}

func TestPostRunAcceptsExternalReceiptAndBuildsExactLineageHandoff(t *testing.T) {
	candidate, policy, _ := postRunFixture(t)
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	policy.Criteria = []verification.Criterion{{ID: "deliverable", Kind: "asset-present", Required: true, Expected: "answer.txt"}, {ID: "repository", Kind: "scope-equals", Required: true, Expected: "fixture/repo"}}
	policy.Issuers = append(policy.Issuers, verification.TrustedIssuer{ID: "external-acceptor", Role: "acceptance", PublicKey: base64.StdEncoding.EncodeToString(pub)})
	now := time.Now().UTC().Truncate(time.Second)
	receipt, err := verification.IssueAcceptance(candidate, policy, nil, nil, "external-acceptor", now, "", priv)
	if err != nil {
		t.Fatalf("external acceptance fixture: %v", err)
	}
	r := &PostRunRecords{Policy: policy, Receipt: receipt}
	if err := validatePostRunAcceptance(candidate, r, now); err != nil {
		t.Fatalf("valid external receipt rejected: %v", err)
	}
	destination := sha256.Sum256([]byte("preserved-destination-snapshot"))
	handoff, err := verification.BuildHandoff(candidate, receipt, mustDigest(receipt), hex.EncodeToString(destination[:]), nil)
	if err != nil {
		t.Fatalf("handoff for exact accepted lineage: %v", err)
	}
	if handoff.CandidateSHA256 != candidate.Digest || handoff.SourceSnapshotSHA256 != candidate.SnapshotSHA256 || handoff.AcceptanceSHA256 != mustDigest(receipt) {
		t.Fatalf("handoff lost accepted lineage: %+v", handoff)
	}
	bad := receipt
	bad.CandidateSHA256 = strings.Repeat("a", 64)
	r.Receipt = bad
	if validatePostRunAcceptance(candidate, r, now) == nil {
		t.Fatal("forged receipt binding accepted")
	}
}

func TestPostRunCredentialResolverHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	resolver := verification.RemoteSecretResolverFunc(func(ctx context.Context, ref string) (string, error) {
		if ctx.Err() != nil || ref != "protected-ref" {
			return "", ErrBlocked
		}
		return "fixture", nil
	})
	if _, err := resolver.Resolve(ctx, "protected-ref"); err == nil {
		t.Fatal("canceled remote operation proceeded")
	}
}
