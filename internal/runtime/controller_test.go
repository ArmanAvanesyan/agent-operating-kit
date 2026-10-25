package runtime

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/hostbridge"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/planning"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/runtime/records"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/runtime/staging"
	ledger "github.com/ArmanAvanesyan/agent-operating-kit/internal/runtime/store"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
	accounting "github.com/ArmanAvanesyan/agent-operating-kit/internal/usage/store"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/verification"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fixedClock struct{ at time.Time }

func (c fixedClock) Now() time.Time { return c.at }
func put(t *testing.T, path string, b []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestAcceptanceAuthorityRequiresFreshTrustedSignature(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	policy := verification.VerificationPolicy{Version: verification.Version, ID: "p", Issuers: []verification.TrustedIssuer{{ID: "authority", PublicKey: base64.StdEncoding.EncodeToString(pub), Role: records.AcceptanceAuthorityRole}}}
	policySHA, err := verification.Digest(policy)
	if err != nil {
		t.Fatal(err)
	}
	authority := records.AcceptanceAuthorityState{Version: records.AcceptanceAuthorityVersion, PolicySHA256: policySHA, TrustGeneration: 3, RevocationGeneration: 8, ObservedAt: now.Format(time.RFC3339Nano), ValidUntil: now.Add(4 * time.Minute).Format(time.RFC3339Nano), Issuer: "authority"}
	unsigned, err := json.Marshal(authority)
	if err != nil {
		t.Fatal(err)
	}
	authority.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(priv, unsigned))
	if err = verifyAcceptanceAuthority(policy, authority, now); err != nil {
		t.Fatalf("valid authority rejected: %v", err)
	}
	if err = verifyAcceptanceAuthority(policy, authority, now.Add(6*time.Minute)); err == nil {
		t.Fatal("expired authority state accepted")
	}
	authority.RevocationGeneration++
	if err = verifyAcceptanceAuthority(policy, authority, now); err == nil {
		t.Fatal("modified authority state accepted")
	}
}

func TestSignedRuntimeAcceptanceProjectionUnlocksOnlyCurrentDependency(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	base := t.TempDir()
	workspace := filepath.Join(base, "workspace")
	if err := os.Mkdir(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	body := []byte("runtime candidate\n")
	if err := os.WriteFile(filepath.Join(workspace, "answer.txt"), body, 0600); err != nil {
		t.Fatal(err)
	}
	gitRun := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", workspace}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Runtime Test", "GIT_AUTHOR_EMAIL=runtime-test@example.invalid", "GIT_COMMITTER_NAME=Runtime Test", "GIT_COMMITTER_EMAIL=runtime-test@example.invalid")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	gitRun("init", "-q")
	gitRun("add", "answer.txt")
	gitRun("commit", "-q", "-m", "checkpoint base")
	gitHeadCmd := exec.Command("git", "-C", workspace, "rev-parse", "HEAD")
	gitHead, err := gitHeadCmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	input := strings.TrimSpace(string(gitHead))
	if len(input) != 40 {
		t.Fatalf("fixture input revision is not a git commit: %q", input)
	}
	if err := os.WriteFile(filepath.Join(workspace, ".git", "inert-runtime-metadata"), []byte("ignored by candidate snapshot\n"), 0600); err != nil {
		t.Fatal(err)
	}
	hostSHA, prepSHA := strings.Repeat("b", 64), strings.Repeat("c", 64)
	hashLineage := func(s string) string { return usage.Hash([]byte(s)) }
	planRaw, err := os.ReadFile(filepath.Join("..", "planning", "testdata", "corpus", "plan.json"))
	if err != nil {
		t.Fatal(err)
	}
	goalRaw, err := os.ReadFile(filepath.Join("..", "planning", "testdata", "corpus", "goal.json"))
	if err != nil {
		t.Fatal(err)
	}
	assignmentRaw := []byte("runtime assignment for inspect\n")
	configSHA, profileSHA := hashLineage("config-id"), hashLineage("profile")
	policyRuntime := records.Policy{Version: "aok-runtime-policy/v1", Authority: "test", MaxConcurrent: 1, LeaseSeconds: 30, MaxAttempts: 2}
	s, err := ledger.Create(filepath.Join(base, "runtime"), policyRuntime, "runtime-policy")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	manifest := records.Manifest{Version: records.Version, ID: "runtime-run", Repository: "fixture/repo", InputRevision: input, Corpus: "corpus", Plan: "corpus/plan.json", Evidence: "evidence.json", Assets: []records.Asset{{Path: "corpus/plan.json", SHA256: usage.Hash(planRaw)}, {Path: "corpus/goal.json", SHA256: usage.Hash(goalRaw)}, {Path: "assignment.json", SHA256: usage.Hash(assignmentRaw)}}, Steps: []records.StepSpec{{ID: "inspect", Assignment: "assignment.json"}, {ID: "report"}}}
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	manifestSHA := usage.Hash(manifestBytes)
	if _, err = s.Submit(ctx, "submit", manifest, manifestSHA, now); err != nil {
		t.Fatal(err)
	}
	token, err := s.Claim(ctx, "claim-inspect", manifest.ID, "inspect", "worker", "attempt-1", now)
	if err != nil {
		t.Fatal(err)
	}
	cp, err := staging.Checkpoint(ctx, workspace, token.AttemptID, input, false)
	if err != nil {
		t.Fatal(err)
	}
	demand := usage.Demand{Version: usage.Version, ConfigurationID: configSHA, ProfileSHA256: profileSHA, Source: "selection", ValidUntil: now.Add(time.Minute).Format(time.RFC3339Nano), Amounts: []usage.Amount{{ConstraintID: "q", Generation: "g", Unit: "requests", Amount: json.Number("1")}}}
	hold := &usage.Reservation{ID: "hold-1", AuthorityEpoch: "account-epoch", Owner: "account-owner", AttemptID: token.AttemptID, State: "in-flight", Fence: 1}
	accountToken := &accounting.Token{ID: hold.ID, Owner: hold.Owner, Epoch: hold.AuthorityEpoch, Fence: hold.Fence}
	step := records.Step{ID: "inspect", Token: token, Attempts: 1, Workspace: cp.Workspace, Checkpoint: &cp, Reservation: hold, AccountingToken: accountToken, DecisionSHA256: hashLineage("decision"), PreparedSHA256: prepSHA, Demand: &demand}
	step, err = s.Update(ctx, "stage", token, "staged", step, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	step, err = s.Update(ctx, "reservation-intent", token, "reservation-intent", step, now.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	step, err = s.Update(ctx, "reserved", token, "reserved", step, now.Add(3*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	step.LaunchPossible = true
	step, err = s.Update(ctx, "launch-intent", token, "launch-intent", step, now.Add(4*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	step, err = s.Update(ctx, "running", token, "running", step, now.Add(5*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	cp, err = staging.Checkpoint(ctx, workspace, token.AttemptID, input, true)
	if err != nil {
		t.Fatal(err)
	}
	step.Checkpoint = &cp
	step.StopObserved = true
	step.HostReceiptSHA256 = hostSHA
	step, err = s.Update(ctx, "awaiting-verification", token, "awaiting-verification", step, now.Add(6*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	pubAccept, privAccept, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pubAuthority, privAuthority, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	c := &Controller{Root: filepath.Join(base, "runtime"), Store: s, Policy: policyRuntime, Clock: fixedClock{now}}
	policy := verification.VerificationPolicy{Version: verification.Version, ID: "runtime-policy", MaxAgeSeconds: 900, Criteria: []verification.Criterion{{ID: "deliverable", Kind: "asset-present", Required: true, Expected: "answer.txt"}, {ID: "scope", Kind: "scope-equals", Required: true, Expected: "fixture/repo"}}, AuthorBindings: []verification.AuthorBinding{{AssignmentSHA256: usage.Hash(assignmentRaw), Principal: "agent"}}, Issuers: []verification.TrustedIssuer{{ID: "acceptor", Role: "acceptance", PublicKey: base64.StdEncoding.EncodeToString(pubAccept)}, {ID: "projection", Role: records.AcceptanceAuthorityRole, PublicKey: base64.StdEncoding.EncodeToString(pubAuthority)}}}
	policySHA, _ := verification.Digest(policy)
	authority := records.AcceptanceAuthorityState{Version: records.AcceptanceAuthorityVersion, PolicySHA256: policySHA, TrustGeneration: 2, RevocationGeneration: 4, ObservedAt: now.Format(time.RFC3339Nano), ValidUntil: now.Add(4 * time.Minute).Format(time.RFC3339Nano), Issuer: "projection"}
	unsigned, _ := json.Marshal(authority)
	authority.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(privAuthority, unsigned))
	if err = c.ConfigureAcceptanceAuthority(ctx, policy, authority); err != nil {
		t.Fatalf("configure authority: %v", err)
	}
	put(t, filepath.Join(c.runRoot(manifest.ID), "inputs", "corpus/plan.json"), planRaw)
	put(t, filepath.Join(c.runRoot(manifest.ID), "inputs", "corpus/goal.json"), goalRaw)
	put(t, filepath.Join(c.runRoot(manifest.ID), "inputs", "assignment.json"), assignmentRaw)
	runtimeState, err := s.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	candidateAssets := make([]verification.Asset, len(cp.Assets))
	for i, a := range cp.Assets {
		candidateAssets[i] = verification.Asset{Path: a.Path, SHA256: a.SHA256, Executable: a.Executable, Role: "deliverable"}
	}
	candidateManifest := verification.CandidateManifest{Version: verification.Version, ID: "runtime-candidate", Origin: "runtime", Repository: "fixture/repo", BaseRevision: input, SourceRevision: cp.BaseRevision, PlanSHA256: usage.Hash(planRaw), GoalSHA256: usage.Hash(goalRaw), AssignmentSHA256: usage.Hash(assignmentRaw), InputSHA256: manifestSHA, ConfigurationSHA256: configSHA, ProfileSHA256: profileSHA, PreparedReceiptSHA256: prepSHA, HostReceiptSHA256: hostSHA, CheckpointSHA256: cp.Digest, AuthorPrincipal: "agent", RunID: manifest.ID, StepID: "inspect", AttemptID: "attempt-1", Fence: 1, CandidateRevision: uint64(runtimeState.Runs[0].Revision), Quiescent: true, Assets: candidateAssets}
	if err := os.WriteFile(filepath.Join(workspace, "answer.txt"), []byte("mutated after checkpoint\n"), 0600); err != nil {
		t.Fatal(err)
	}
	candidate, err := c.CaptureCandidate(ctx, candidateManifest, workspace, filepath.Join(base, "candidate-bundle"))
	if err != nil {
		t.Fatalf("capture candidate: %v", err)
	}
	captured, err := os.ReadFile(filepath.Join(candidate.BundlePath, "answer.txt"))
	if err != nil || string(captured) != string(body) {
		t.Fatalf("candidate did not use frozen checkpoint bytes: %q, %v", captured, err)
	}
	for name, tamper := range map[string]func(*verification.CandidateManifest){
		"plan":           func(m *verification.CandidateManifest) { m.PlanSHA256 = hashLineage("tampered-plan") },
		"goal":           func(m *verification.CandidateManifest) { m.GoalSHA256 = hashLineage("tampered-goal") },
		"assignment":     func(m *verification.CandidateManifest) { m.AssignmentSHA256 = hashLineage("tampered-assignment") },
		"configuration":  func(m *verification.CandidateManifest) { m.ConfigurationSHA256 = hashLineage("tampered-config") },
		"profile":        func(m *verification.CandidateManifest) { m.ProfileSHA256 = hashLineage("tampered-profile") },
		"author":         func(m *verification.CandidateManifest) { m.AuthorPrincipal = "untrusted" },
		"base":           func(m *verification.CandidateManifest) { m.BaseRevision = hashLineage("tampered-base") },
		"source":         func(m *verification.CandidateManifest) { m.SourceRevision = hashLineage("tampered-source") },
		"manifest-input": func(m *verification.CandidateManifest) { m.InputSHA256 = hashLineage("tampered-manifest") },
	} {
		bad := candidateManifest
		tamper(&bad)
		if err = c.VerifyCandidateLineage(bad); err == nil {
			t.Errorf("tampered %s lineage was accepted", name)
		}
	}
	receipt, err := verification.IssueAcceptance(candidate, policy, nil, nil, "acceptor", now, "", privAccept)
	if err != nil {
		t.Fatalf("issue acceptance: %v", err)
	}
	noProjectionGuard := ledger.AcceptanceGuard{Dependencies: []string{"inspect"}, TrustGeneration: 2, RevocationGeneration: 4, PolicySHA256: policySHA, AuthorityValidUntil: now.Add(4 * time.Minute)}
	if _, err = s.ClaimWithAcceptance(ctx, "claim-dependent-before-acceptance", manifest.ID, "report", "worker", "attempt-before", now.Add(7*time.Second), noProjectionGuard); !errors.Is(err, ledger.ErrBlocked) {
		t.Fatalf("dependency without attached acceptance admitted claim: %v", err)
	}
	if err = c.AttachAcceptance(ctx, receipt, candidateManifest, now); err != nil {
		t.Fatalf("attach acceptance: %v", err)
	}
	accepted := s.AcceptedStepsAt(ctx, manifest.ID, c.acceptanceGuard, now)
	if len(accepted) != 1 || accepted[0] != "inspect" {
		t.Fatalf("accepted projection missing: %v", accepted)
	}
	if got := s.AcceptedStepsAt(ctx, manifest.ID, c.acceptanceGuard, now.Add(10*time.Minute)); len(got) != 0 {
		t.Fatal("expired acceptance projection admitted")
	}
	stateAfterAcceptance, err := s.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stateAfterAcceptance.Runs[0].Steps[0].Accepted {
		t.Fatal("projection rewrote historical accepted:false runtime field")
	}
	corpus := filepath.Join("..", "planning", "testdata", "corpus")
	ready := planning.ReadyWithAcceptedDeps(filepath.Join(corpus, "plan.json"), filepath.Join(corpus, "evidence.json"), corpus, "2026-10-16T12:00:00Z", accepted)
	if ready.Status != "valid" || len(ready.EligibleSteps) != 1 || ready.EligibleSteps[0] != "report" {
		t.Fatalf("signed acceptance did not unlock exact dependent: %+v", ready)
	}
	wrongRun := receipt
	wrongRun.RunID = "other-run"
	if err = s.AttachAcceptance(ctx, wrongRun, candidateManifest, policy, 2, 4, now); err == nil {
		t.Fatal("wrong-run acceptance attached")
	}
	if err = c.RevokeAcceptance(ctx, manifest.ID, "inspect", 2, 5); err != nil {
		t.Fatal(err)
	}
	if got := s.AcceptedStepsAt(ctx, manifest.ID, c.acceptanceGuard, now); len(got) != 0 {
		t.Fatalf("revoked projection remained active: %v", got)
	}
	claimGuard := ledger.AcceptanceGuard{Dependencies: []string{"inspect"}, TrustGeneration: 2, RevocationGeneration: 5, AuthorityValidUntil: now.Add(4 * time.Minute)}
	if _, err = s.ClaimWithAcceptance(ctx, "claim-dependent-after-revoke", manifest.ID, "report", "worker-2", "attempt-2", now.Add(8*time.Second), claimGuard); !errors.Is(err, ledger.ErrBlocked) {
		t.Fatalf("revoked dependency admitted claim: %v", err)
	}
	if err = s.AttachAcceptance(ctx, receipt, candidateManifest, policy, 2, 5, now.Add(9*time.Second)); err == nil {
		t.Fatal("explicitly revoked receipt reattached under new generations")
	}
}
func controllerFixture(t *testing.T) (*Controller, string) {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(base, "repo")
	os.Mkdir(repo, 0700)
	source := filepath.Join("..", "planning", "testdata", "corpus")
	capture := filepath.Join(base, "capture")
	assets := []records.Asset{}
	if err = filepath.WalkDir(source, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(source, path)
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		name := "corpus/" + filepath.ToSlash(rel)
		put(t, filepath.Join(capture, filepath.FromSlash(name)), raw)
		assets = append(assets, records.Asset{Path: name, SHA256: usage.Hash(raw)})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	dummy := []byte(`{"fixture":"not a selection proof"}`)
	put(t, filepath.Join(capture, "task.json"), dummy)
	assets = append(assets, records.Asset{Path: "task.json", SHA256: usage.Hash(dummy)})
	hostPolicy := hostbridge.Policy{Version: hostbridge.Version, Host: "codex", HostVersion: hostbridge.CodexPinnedVersion, Executable: "/nonexistent/codex", ExecutableSHA256: strings.Repeat("a", 64), AuthMode: "native-login", WorkspaceRoots: []string{base}, Workspace: filepath.Join(base, "staging", "attempt-one"), Models: []string{"model-x"}, TimeoutSeconds: 30}
	hp := filepath.Join(base, "host-policy.json")
	put(t, hp, Encode(hostPolicy))
	p := records.Policy{Version: Version, Authority: "fixture-authority", RepositoryRoots: []string{repo}, StagingRoot: filepath.Join(base, "staging"), AccountingPath: filepath.Join(base, "account", "usage.db"), UsagePolicy: records.Asset{}, Hosts: []records.HostRoute{{Host: "codex", Policy: records.Asset{Path: hp, SHA256: usage.Hash(Encode(hostPolicy))}}}, AllowDispatch: true, MaxConcurrent: 1, MaxAttempts: 2, LeaseSeconds: 30, HeartbeatSeconds: 5}
	s, err := ledger.Create(filepath.Join(base, "runtime"), p, usage.Digest(p))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	c := &Controller{Root: filepath.Join(base, "runtime"), Store: s, Policy: p, Clock: fixedClock{time.Date(2026, 10, 16, 0, 0, 1, 0, time.UTC)}, Runner: panicRunner{}}
	inputs := records.SelectionInputs{Task: "task.json", Candidates: "task.json", Catalog: "catalog", CatalogPolicy: "task.json", Policy: "task.json", EvidencePolicy: "task.json"}
	m := records.Manifest{Version: Version, ID: "run-one", Repository: repo, InputRevision: "immutable-repository-revision", RepositoryAssets: []records.Asset{{Path: "tracked.txt", SHA256: usage.Hash([]byte("tracked"))}}, AllowedUntracked: []string{}, Corpus: "corpus", Plan: "corpus/plan.json", Evidence: "corpus/evidence.json", Steps: []records.StepSpec{{ID: "inspect", Assignment: "corpus/assignment.json", Host: "codex", Selection: inputs}}, Assets: assets}
	path := filepath.Join(capture, "run.json")
	put(t, path, Encode(m))
	if _, err = c.Submit(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	return c, path
}

type panicRunner struct{}

func (panicRunner) Start(context.Context, hostbridge.Launch) (hostbridge.Process, error) {
	panic("a blocked fixture must never launch a host")
}
func TestDispatchMissingLiveQualificationStartsNoHost(t *testing.T) {
	c, _ := controllerFixture(t)
	r, err := c.Dispatch(context.Background(), "run-one")
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != "blocked" || !strings.Contains(strings.Join(r.Reasons, " "), "P6") {
		t.Fatalf("missing live gate was not reported: %+v", r)
	}
	s, err := c.Store.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if s.Runs[0].Steps[0].Attempts != 0 {
		t.Fatal("unqualified host consumed an attempt")
	}
}
func TestCaptureTamperingCannotChangeDispatchedInputs(t *testing.T) {
	c, _ := controllerFixture(t)
	put(t, filepath.Join(c.runRoot("run-one"), "inputs", "corpus", "assignment.json"), []byte(`{}`))
	if _, err := c.Dispatch(context.Background(), "run-one"); err == nil {
		t.Fatal("tampered inputs admitted")
	}
}
func TestPlanBindingRejectsSubstitutedAssignmentAndDependentStep(t *testing.T) {
	c, _ := controllerFixture(t)
	state, _ := c.Store.Snapshot(context.Background())
	m := state.Runs[0].Manifest
	root := filepath.Join(c.runRoot(m.ID), "inputs")
	spec := m.Steps[0]
	dependencies, preconditioned, err := validatePlanBinding(root, m, spec)
	if err != nil || len(dependencies) > 0 || preconditioned {
		t.Fatalf("valid root step: %v %t %v", dependencies, preconditioned, err)
	}
	spec.ID = "report"
	dependencies, preconditioned, err = validatePlanBinding(root, m, spec)
	if err == nil && len(dependencies) == 0 && !preconditioned {
		t.Fatal("dependent step bypassed P8")
	}
	spec = m.Steps[0]
	spec.Assignment = "corpus/agent.json"
	if _, _, err = validatePlanBinding(root, m, spec); err == nil {
		t.Fatal("substituted assignment admitted")
	}
}
func TestFixtureQualifiedReceiptCannotAdmitNativeHost(t *testing.T) {
	c, _ := controllerFixture(t)
	hp, err := hostbridge.ReadPolicy(c.Policy.Hosts[0].Policy.Path)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(filepath.Dir(c.Root), "q")
	tx := hostbridge.Transcript{Events: []hostbridge.Event{}, ObservedTools: []string{}, UsageFrames: []json.RawMessage{}, Diagnostics: []hostbridge.Diagnostic{}}
	raw := Encode(tx)
	receipt := hostbridge.Receipt{Version: hostbridge.Version, Host: hp.Host, HostVersion: hp.HostVersion, PolicySHA256: hostbridge.PolicyHash(hp), TranscriptSHA256: usage.Hash(raw), Report: hostbridge.BaseReport()}
	receipt.Report.LiveGate = "fixture-qualified"
	receipt.Digest = usage.Digest(receipt)
	put(t, filepath.Join(dir, "transcript.json"), raw)
	rb := Encode(receipt)
	put(t, filepath.Join(dir, "receipt.json"), rb)
	c.Policy.Hosts[0].Qualification = &records.Asset{Path: filepath.Join(dir, "receipt.json"), SHA256: usage.Hash(rb)}
	if _, err = c.route("codex", c.now(), 1, "inspect"); err == nil {
		t.Fatal("fixture qualification admitted native runner")
	}
}
func TestRuntimeInputRevisionBindsFullStagedInventory(t *testing.T) {
	base := t.TempDir()
	inventory := []records.Asset{{Path: "source.go", SHA256: strings.Repeat("b", 64)}}
	rev := RepositoryRevision{Version: Version, Commit: "commit", InventorySHA256: inventoryDigest(inventory)}
	raw := Encode(rev)
	put(t, filepath.Join(base, "revision.json"), raw)
	task := map[string]any{"inputRevision": map[string]string{"path": "revision.json", "sha256": usage.Hash(raw)}}
	put(t, filepath.Join(base, "task.json"), Encode(task))
	m := records.Manifest{InputRevision: "commit", RepositoryAssets: inventory}
	cp := records.Checkpoint{BaseRevision: "commit", Assets: inventory}
	if err := verifyTaskRevision(base, records.SelectionInputs{Task: "task.json"}, m, cp, false); err != nil {
		t.Fatal(err)
	}
	cp.Assets = []records.Asset{{Path: "source.go", SHA256: strings.Repeat("c", 64)}}
	if err := verifyTaskRevision(base, records.SelectionInputs{Task: "task.json"}, m, cp, false); err == nil {
		t.Fatal("changed repository bytes admitted")
	}
}
