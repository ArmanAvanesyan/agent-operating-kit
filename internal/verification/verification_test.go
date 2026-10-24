package verification

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func hash(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func fixture(t *testing.T) (CandidateManifest, VerificationPolicy, string) {
	t.Helper()
	root := t.TempDir()
	_ = os.WriteFile(filepath.Join(root, "answer.txt"), []byte("done"), 0644)
	m := CandidateManifest{Version: Version, ID: "cand-1", Origin: "manual", Repository: "acme/repo", BaseRevision: "base", SourceRevision: "head", Assets: []Asset{{Path: "answer.txt", SHA256: hash([]byte("done")), Role: "deliverable"}}}
	p := VerificationPolicy{Version: Version, ID: "policy", MaxAgeSeconds: 3600, Criteria: []Criterion{{ID: "deliverable", Kind: "asset-present", Required: true, Expected: "answer.txt"}, {ID: "scope", Kind: "scope-equals", Required: true, Expected: "acme/repo"}}}
	return m, p, root
}
func TestStrictDecodeAndManifestValidation(t *testing.T) {
	m, _, _ := fixture(t)
	b, _ := json.Marshal(m)
	if _, e := DecodeManifest(b); e != nil {
		t.Fatal(e)
	}
	if _, e := DecodeManifest(append(b[:len(b)-1], []byte(`,"surprise":true}`)...)); e == nil {
		t.Fatal("unknown field accepted")
	}
	if _, e := DecodeManifest([]byte(`{"version":"aok-verification/v1","id":"a","id":"b"}`)); e == nil {
		t.Fatal("duplicate key accepted")
	}
	m.Assets = append(m.Assets, m.Assets[0])
	if ValidateManifest(m) == nil {
		t.Fatal("duplicate path accepted")
	}
}
func TestSigningHelpersRejectMalformedPrivateKeys(t *testing.T) {
	valid := make(ed25519.PrivateKey, ed25519.PrivateKeySize)
	badKeys := []ed25519.PrivateKey{nil, make(ed25519.PrivateKey, ed25519.SeedSize), valid}
	for _, key := range badKeys {
		if _, e := SignVerification(VerificationReceipt{}, key); e == nil {
			t.Fatal("SignVerification accepted malformed key")
		}
		if _, e := SignReview(ReviewReceipt{}, key); e == nil {
			t.Fatal("SignReview accepted malformed key")
		}
		if _, e := SignControlProof(ControlProof{}, key); e == nil {
			t.Fatal("SignControlProof accepted malformed key")
		}
		if _, e := SignAcceptance(AcceptanceReceipt{}, key); e == nil {
			t.Fatal("SignAcceptance accepted malformed key")
		}
	}
}
func TestCandidateIDCannotEscapePortableStore(t *testing.T) {
	m, _, _ := fixture(t)
	for _, id := range []string{"../escape", "a/b", "CON", "NUL.txt", ".."} {
		m.ID = id
		if ValidateManifest(m) == nil {
			t.Fatalf("unsafe candidate ID accepted: %q", id)
		}
	}
}

type lineageOK struct{ allow bool }

func (v lineageOK) VerifyCandidateLineage(CandidateManifest) error {
	if !v.allow {
		return errors.New("not present in P7 authority")
	}
	return nil
}
func TestRuntimeOriginRequiresTrustedP7Lineage(t *testing.T) {
	m, _, src := fixture(t)
	m.Origin = "runtime"
	m.RunID = "run"
	m.StepID = "step"
	m.AttemptID = "attempt"
	m.Fence = 3
	m.CandidateRevision = 1
	m.Quiescent = true
	m.AuthorPrincipal = "author"
	for _, target := range []*string{&m.PlanSHA256, &m.GoalSHA256, &m.AssignmentSHA256, &m.InputSHA256, &m.ConfigurationSHA256, &m.ProfileSHA256, &m.PreparedReceiptSHA256, &m.HostReceiptSHA256, &m.CheckpointSHA256} {
		*target = hash([]byte("lineage"))
	}
	if _, e := ImportCandidate(m, src, filepath.Join(t.TempDir(), "bundle")); e == nil {
		t.Fatal("runtime origin accepted without trusted P7 verifier")
	}
	if _, e := ImportRuntimeCandidate(m, src, filepath.Join(t.TempDir(), "bundle"), lineageOK{}); e == nil {
		t.Fatal("runtime lineage verifier denial ignored")
	}
	if _, e := ImportRuntimeCandidate(m, src, filepath.Join(t.TempDir(), "bundle"), lineageOK{allow: true}); e != nil {
		t.Fatal(e)
	}
}

func TestAcceptanceRequiresSignedPinnedVerifierAndIndependentReview(t *testing.T) {
	m, p, src := fixture(t)
	m.AuthorPrincipal = "author"
	m.AssignmentSHA256 = hash([]byte("assignment"))
	c, e := ImportCandidate(m, src, filepath.Join(t.TempDir(), "bundle"))
	if e != nil {
		t.Fatal(e)
	}
	vpub, vpriv, _ := ed25519.GenerateKey(rand.Reader)
	rpub, rpriv, _ := ed25519.GenerateKey(rand.Reader)
	apub, apriv, _ := ed25519.GenerateKey(rand.Reader)
	p.Verifiers = []VerifierSpec{{ID: "tests", Kind: "command", ImplementationSHA256: hash([]byte("impl")), RubricSHA256: hash([]byte("rubric")), Toolchain: "go1.22", TimeoutSeconds: 3, MaxOutputBytes: 1024}}
	p.Criteria = append(p.Criteria, Criterion{ID: "tests-pass", Kind: "command", Required: true, VerifierID: "tests", Independent: true})
	p.AuthorBindings = []AuthorBinding{{AssignmentSHA256: m.AssignmentSHA256, Principal: "author"}}
	p.Issuers = []TrustedIssuer{{ID: "tests", Role: "verifier", Principal: "test-bot", PublicKey: base64.StdEncoding.EncodeToString(vpub)}, {ID: "reviewer", Role: "reviewer", Principal: "reviewer-user", PublicKey: base64.StdEncoding.EncodeToString(rpub)}, {ID: "acceptor", Role: "acceptance", PublicKey: base64.StdEncoding.EncodeToString(apub)}}
	pd, _ := Digest(p)
	envDigest, _ := Digest(struct {
		Toolchain   string
		Environment map[string]string
	}{p.Verifiers[0].Toolchain, p.Verifiers[0].Environment})
	specDigest, _ := Digest(p.Verifiers[0])
	issuedAt := time.Now().UTC()
	vr := VerificationReceipt{ID: "vr1", CandidateSHA256: c.Digest, PolicySHA256: pd, VerifierID: "tests", Principal: "test-bot", ImplementationSHA256: p.Verifiers[0].ImplementationSHA256, VerifierSpecSHA256: specDigest, RubricSHA256: p.Verifiers[0].RubricSHA256, EnvironmentSHA256: envDigest, ObservedAt: issuedAt.Format(time.RFC3339), SnapshotBefore: c.SnapshotSHA256, SnapshotAfter: c.SnapshotSHA256, Results: []PredicateResult{{CriterionID: "tests-pass", Result: "pass", Evidence: []string{"tests.xml"}}}}
	vr, e = SignVerification(vr, vpriv)
	if e != nil {
		t.Fatal(e)
	}
	rr := ReviewReceipt{ID: "review-1", CandidateSHA256: c.Digest, SnapshotSHA256: c.SnapshotSHA256, PolicySHA256: pd, Principal: "reviewer-user", Disposition: "pass", Issuer: "reviewer", ObservedAt: time.Now().UTC().Format(time.RFC3339)}
	rr, e = SignReview(rr, rpriv)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = IssueAcceptance(c, p, []VerificationReceipt{vr}, nil, "acceptor", time.Now().UTC(), "", apriv); e == nil {
		t.Fatal("independent review was omitted")
	}
	a, e := IssueAcceptance(c, p, []VerificationReceipt{vr}, []ReviewReceipt{rr}, "acceptor", time.Now().UTC(), "", apriv)
	if e != nil {
		t.Fatal(e)
	}
	if len(a.VerificationIDs) != 1 {
		t.Fatalf("verification refs missing: %+v", a)
	}
	failed := vr
	failed.ID = "vr-failed"
	failed.Results = []PredicateResult{{CriterionID:"tests-pass",Result:"fail",Evidence:[]string{"tests.xml"}}}
	failed, _ = SignVerification(failed, vpriv)
	for _, receipts := range [][]VerificationReceipt{{failed,vr},{vr,failed}} {
		if _, err := IssueAcceptance(c,p,receipts,[]ReviewReceipt{rr},"acceptor",time.Now().UTC(),"",apriv); err == nil { t.Fatal("conflicting receipt order changed acceptance") }
	}
	p2 := p
	p2.Issuers = append([]TrustedIssuer(nil), p.Issuers...)
	p2.Issuers[1].Principal = "test-bot"
	pd2, _ := Digest(p2)
	vr2 := vr
	vr2.PolicySHA256 = pd2
	vr2.Signature = ""
	vr2, _ = SignVerification(vr2, vpriv)
	rr2 := rr
	rr2.PolicySHA256 = pd2
	rr2.Principal = "test-bot"
	rr2.Signature = ""
	rr2, _ = SignReview(rr2, rpriv)
	if _, e = IssueAcceptance(c, p2, []VerificationReceipt{vr2}, []ReviewReceipt{rr2}, "acceptor", time.Now().UTC(), "", apriv); e == nil {
		t.Fatal("reviewer with verifier principal accepted despite a different signing key")
	}
	if _, e = IssueAcceptance(c, p, []VerificationReceipt{vr}, []ReviewReceipt{rr}, "acceptor", issuedAt.Add(-time.Second), "", apriv); e == nil {
		t.Fatal("future signed evidence accepted")
	}
	if _, e = IssueAcceptance(c, p, []VerificationReceipt{vr}, []ReviewReceipt{rr}, "acceptor", issuedAt.Add(2*time.Hour), "", apriv); e == nil {
		t.Fatal("stale signed evidence accepted")
	}
	vr.Results[0].Result = "fail"
	if _, e = IssueAcceptance(c, p, []VerificationReceipt{vr}, []ReviewReceipt{rr}, "acceptor", time.Now().UTC(), "", apriv); e == nil {
		t.Fatal("tampered verifier receipt accepted")
	}
}

type fakeControls struct {
	proof      ControlProof
	called     bool
	block      bool
	started    chan struct{}
	mutatePath string
}

func (f *fakeControls) Proof() ControlProof { return f.proof }
func (f *fakeControls) Execute(ctx context.Context, argv []string, env map[string]string, timeout time.Duration, limit int64) (int, []byte, error) {
	f.called = true
	if f.started != nil {
		close(f.started)
	}
	if len(argv) == 0 || timeout <= 0 || limit <= 0 {
		return 1, nil, context.Canceled
	}
	if f.mutatePath != "" {
		_ = os.Chmod(f.mutatePath, 0600)
		_ = os.WriteFile(f.mutatePath, []byte("mutated during verifier"), 0600)
	}
	if f.block {
		<-ctx.Done()
		return 1, nil, ctx.Err()
	}
	select {
	case <-ctx.Done():
		return 1, nil, ctx.Err()
	default:
	}
	return 0, []byte("ok"), nil
}
func TestCommandRequiresSignedBoundControlProof(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	p := VerificationPolicy{Version: Version, ID: "p", MaxAgeSeconds: 3600, Verifiers: []VerifierSpec{{ID: "v", Kind: "command", ImplementationSHA256: hash([]byte("i")), RubricSHA256: hash([]byte("r")), Argv: []string{"check"}, TimeoutSeconds: 2, MaxOutputBytes: 10}}, Criteria: []Criterion{{ID: "c", Kind: "command", VerifierID: "v", Required: true}}, Issuers: []TrustedIssuer{{ID: "control", Role: "controls", ExecutorSHA256: hash([]byte("executor")), PublicKey: base64.StdEncoding.EncodeToString(pub)}}}
	pd, _ := Digest(p)
	sd, _ := Digest(p.Verifiers[0])
	now := time.Now().UTC()
	proof, _ := SignControlProof(ControlProof{PolicySHA256: pd, VerifierSHA256: sd, ExecutorSHA256: hash([]byte("executor")), ObservedAt: now.Format(time.RFC3339), ValidUntil: now.Add(time.Minute).Format(time.RFC3339), Scope: "candidate-offline", Controls: []string{"filesystem-isolation", "readonly-candidate", "network-deny", "credential-isolation", "time-bound", "output-bound"}, Issuer: "control"}, priv)
	ctl := &fakeControls{proof: proof}
	m, _, src := fixture(t)
	candidate, e := ImportCandidate(m, src, filepath.Join(t.TempDir(), "bundle"))
	if e != nil {
		t.Fatal(e)
	}
	proof.CandidateSHA256 = candidate.Digest
	proof.SnapshotSHA256 = candidate.SnapshotSHA256
	proof, _ = SignControlProof(proof, priv)
	ctl.proof = proof
	out, code, e := RunCommand(context.Background(), candidate, p, "v", ctl)
	if e != nil || code != 0 || string(out) != "ok" || !ctl.called {
		t.Fatalf("runner result %q %d %v", out, code, e)
	}
	ctl.proof.PolicySHA256 = "wrong"
	ctl.called = false
	if _, _, e = RunCommand(context.Background(), candidate, p, "v", ctl); e == nil || ctl.called {
		t.Fatal("wrong proof launched verifier")
	}
	for _, tc := range []struct {
		name            string
		observed, until time.Time
	}{{"stale", now.Add(-time.Hour), now.Add(-time.Minute)}, {"future", now.Add(time.Hour), now.Add(2 * time.Hour)}} {
		t.Run(tc.name, func(t *testing.T) {
			bad := proof
			bad.PolicySHA256 = pd
			bad.ObservedAt = tc.observed.Format(time.RFC3339)
			bad.ValidUntil = tc.until.Format(time.RFC3339)
			bad, _ = SignControlProof(bad, priv)
			ctl.proof = bad
			ctl.called = false
			if _, _, e := RunCommand(context.Background(), candidate, p, "v", ctl); e == nil || ctl.called {
				t.Fatal("stale or future control proof launched verifier")
			}
		})
	}
	validProof := func(policy VerificationPolicy) ControlProof {
		policyDigest, _ := Digest(policy)
		specDigest, _ := Digest(policy.Verifiers[0])
		q := proof
		q.PolicySHA256 = policyDigest
		q.VerifierSHA256 = specDigest
		q.ObservedAt = time.Now().UTC().Format(time.RFC3339)
		q.ValidUntil = time.Now().UTC().Add(time.Minute).Format(time.RFC3339)
		q.CandidateSHA256 = candidate.Digest
		q.SnapshotSHA256 = candidate.SnapshotSHA256
		q, _ = SignControlProof(q, priv)
		return q
	}
	pTimeout := p
	pTimeout.Verifiers = append([]VerifierSpec(nil), p.Verifiers...)
	pTimeout.Verifiers[0].TimeoutSeconds = 1
	tc := &fakeControls{proof: validProof(pTimeout), block: true, started: make(chan struct{})}
	timeoutDone := make(chan error, 1)
	go func() {
		_, _, err := RunCommand(context.Background(), candidate, pTimeout, "v", tc)
		timeoutDone <- err
	}()
	<-tc.started
	if err := <-timeoutDone; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("bounded verifier timeout not enforced: %v", err)
	}
	cancelCtx, cancel := context.WithCancel(context.Background())
	cancelControls := &fakeControls{proof: validProof(p), block: true, started: make(chan struct{})}
	cancelDone := make(chan error, 1)
	go func() { _, _, err := RunCommand(cancelCtx, candidate, p, "v", cancelControls); cancelDone <- err }()
	<-cancelControls.started
	cancel()
	if err := <-cancelDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("verifier cancellation not enforced: %v", err)
	}
	mutation := &fakeControls{proof: validProof(p), mutatePath: filepath.Join(candidate.BundlePath, "answer.txt")}
	if _, _, err := RunCommand(context.Background(), candidate, p, "v", mutation); err == nil {
		t.Fatal("source mutation during verifier was accepted")
	}
}
func TestImportCreateOnlyAndTamperDetection(t *testing.T) {
	m, _, src := fixture(t)
	store := filepath.Join(t.TempDir(), "bundle")
	c, e := ImportCandidate(m, src, store)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = ImportCandidate(m, src, store); e == nil {
		t.Fatal("overwrite accepted")
	}
	assetPath := filepath.Join(c.BundlePath, "answer.txt")
	_ = os.Chmod(assetPath, 0600)
	_ = os.WriteFile(assetPath, []byte("changed"), 0600)
	if _, e = LoadCandidate(c.BundlePath); e == nil {
		t.Fatal("mode mutation accepted")
	}
	_, policy, _ := fixture(t)
	if _, e = Check(c, policy); e == nil {
		t.Fatal("check used stale candidate snapshot")
	}
}
func TestImportRejectsSymlinkAndUnlistedSource(t *testing.T) {
	m, _, src := fixture(t)
	_ = os.Remove(filepath.Join(src, "answer.txt"))
	_ = os.Symlink("/etc/hosts", filepath.Join(src, "answer.txt"))
	if _, e := ImportCandidate(m, src, filepath.Join(t.TempDir(), "bundle")); e == nil {
		t.Fatal("symlink accepted")
	}
	m, _, src = fixture(t)
	_ = os.WriteFile(filepath.Join(src, "extra"), []byte("x"), 0600)
	if _, e := ImportCandidate(m, src, filepath.Join(t.TempDir(), "bundle")); e == nil {
		t.Fatal("unlisted file accepted")
	}
}
func TestPureCheckAndSignedAcceptanceBinding(t *testing.T) {
	m, p, src := fixture(t)
	c, e := ImportCandidate(m, src, filepath.Join(t.TempDir(), "bundle"))
	if e != nil {
		t.Fatal(e)
	}
	report, e := Check(c, p)
	if e != nil || report.Disposition != "accepted" {
		t.Fatalf("report=%+v err=%v", report, e)
	}
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	p.Issuers = []TrustedIssuer{{ID: "acceptor", Role: "acceptance", PublicKey: base64.StdEncoding.EncodeToString(pub)}}
	r, e := IssueAcceptance(c, p, nil, nil, "acceptor", time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC), "", priv)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = IssueAcceptance(c, p, nil, nil, "acceptor", time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC), "2026-10-24T02:00:00Z", priv); e == nil {
		t.Fatal("acceptance validity extended beyond evidence")
	}
	issuer := []TrustedIssuer{{ID: "acceptor", Role: "acceptance", PublicKey: base64.StdEncoding.EncodeToString(pub)}}
	if e = VerifyAcceptance(r, issuer, time.Date(2026, 10, 24, 1, 0, 0, 0, time.UTC), 24*time.Hour); e != nil {
		t.Fatal(e)
	}
	if e = ValidateAcceptanceBinding(r, c, p); e != nil {
		t.Fatal(e)
	}
	missingCoverage := r
	missingCoverage.Results = nil
	missingCoverage.VerificationIDs = nil
	missingCoverage.VerificationSHA256s = nil
	missingCoverage.ReviewSHA256s = nil
	missingCoverage.Signature = ""
	missingCoverage, _ = SignAcceptance(missingCoverage, priv)
	if e = ValidateAcceptanceBinding(missingCoverage, c, p); e == nil {
		t.Fatal("signed acceptance without criterion coverage was accepted")
	}
	r.Fence++
	if VerifyAcceptance(r, issuer, time.Date(2026, 10, 24, 1, 0, 0, 0, time.UTC), 24*time.Hour) == nil {
		t.Fatal("tampered signature accepted")
	}
}
func TestMissingCriterionBlocksAndCannotIssue(t *testing.T) {
	m, p, src := fixture(t)
	c, e := ImportCandidate(m, src, filepath.Join(t.TempDir(), "bundle"))
	if e != nil {
		t.Fatal(e)
	}
	p.Criteria = append(p.Criteria, Criterion{ID: "missing", Kind: "asset-present", Required: true, Expected: "not-there"})
	r, e := Check(c, p)
	if e != nil || r.Disposition != "blocked" {
		t.Fatalf("%+v %v", r, e)
	}
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	if _, e = IssueAcceptance(c, p, nil, nil, "i", time.Now(), "", priv); e == nil {
		t.Fatal("blocked report accepted")
	}
}
func TestCommandNeedsQualifiedControlsAndBoundedContext(t *testing.T) {
	p := VerificationPolicy{Version: Version, ID: "p", MaxAgeSeconds: 3600, Verifiers: []VerifierSpec{{ID: "v", Kind: "command", ImplementationSHA256: "sha", RubricSHA256: "rubric", Argv: []string{"check"}, TimeoutSeconds: 2, MaxOutputBytes: 10}}, Criteria: []Criterion{{ID: "c", Kind: "command", Required: true, VerifierID: "v"}}}
	if _, _, e := RunCommand(context.Background(), Candidate{}, p, "v", nil); e == nil {
		t.Fatal("missing controls accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, e := RunCommand(ctx, Candidate{}, p, "v", nil); e == nil {
		t.Fatal("cancelled run accepted")
	}
}
func TestHandoffRejectsChangedTreeAndPublishingActions(t *testing.T) {
	m, p, src := fixture(t)
	c, e := ImportCandidate(m, src, filepath.Join(t.TempDir(), "bundle"))
	if e != nil {
		t.Fatal(e)
	}
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	p.Issuers = []TrustedIssuer{{ID: "issuer", Role: "acceptance", PublicKey: base64.StdEncoding.EncodeToString(pub)}}
	a, _ := IssueAcceptance(c, p, nil, nil, "issuer", time.Now().UTC(), "", priv)
	ad, _ := Digest(a)
	h, e := BuildHandoff(c, a, ad, "other-tree", nil)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = ValidateHandoff(h); e == nil {
		t.Fatal("changed destination accepted")
	}
	h.DestinationSnapshotSHA256 = c.SnapshotSHA256
	h.Actions = append(h.Actions, "push")
	if _, e = ValidateHandoff(h); e == nil {
		t.Fatal("publish action accepted")
	}
}
