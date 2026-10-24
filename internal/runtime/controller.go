package runtime

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/evaluation"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/evidence"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/hostbridge"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/operations"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/planning"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/runtime/records"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/runtime/staging"
	ledger "github.com/ArmanAvanesyan/agent-operating-kit/internal/runtime/store"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/selection"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
	accounting "github.com/ArmanAvanesyan/agent-operating-kit/internal/usage/store"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/verification"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Controller struct {
	methodologyAuthority MethodologyAuthority
	Root                 string
	Store                *ledger.Store
	Policy               records.Policy
	Clock                records.Clock
	Runner               hostbridge.Runner
	acceptancePolicy     *verification.VerificationPolicy
	acceptanceGuard      ledger.AcceptanceGuard
	pilotActivation      *operations.Activation
	pilotTrials          map[string]PilotTrialBinding
	pilotRevalidate      func(context.Context) error
	trialConsume         func(context.Context) error
	trialValidateDemand  func(usage.Demand, evaluation.EstimateArtifact, selection.TaskProfile, time.Time) error
}

// PilotTrialBinding binds a stable preregistered logical trial to the complete
// captured run. Replacing an authority does not mint another launch allowance.
type PilotTrialBinding struct {
	RunID               string
	StepID              string
	TrialID             string
	ManifestSHA256      string
	HostPolicySHA256    string
	ConfigurationSHA256 string
	ProfileSHA256       string
	NativeRunner        bool
}

// ConfigurePilotActivation is opt-in and never relaxes normal P7 admission.
// The caller obtains activation from an independently qualified external witness.
func (c *Controller) ConfigurePilotActivation(ctx context.Context, a *operations.Activation, routeScope string, trials []PilotTrialBinding) error {
	if a == nil || routeScope == "" || a.ScopeSHA256() != routeScope || len(trials) == 0 {
		return fmt.Errorf("qualified activation and trial bindings required")
	}
	if err := a.Check(ctx); err != nil {
		return err
	}
	bindings := map[string]PilotTrialBinding{}
	ids := map[string]bool{}
	for _, b := range trials {
		if b.RunID == "" || b.StepID == "" || b.TrialID == "" || len(b.ManifestSHA256) != 64 || len(b.HostPolicySHA256) != 64 || len(b.ConfigurationSHA256) != 64 || len(b.ProfileSHA256) != 64 || !b.NativeRunner || ids[b.TrialID] {
			return fmt.Errorf("invalid or duplicate pilot trial binding")
		}
		key := b.RunID + "\x00" + b.StepID
		if _, exists := bindings[key]; exists {
			return fmt.Errorf("duplicate pilot step")
		}
		bindings[key] = b
		ids[b.TrialID] = true
	}
	c.pilotActivation = a
	c.pilotTrials = bindings
	return nil
}

func Init(root, policyPath string) (*Controller, error) {
	p, sha, err := ReadPolicy(policyPath)
	if err != nil {
		return nil, err
	}
	s, err := ledger.Create(root, p, sha)
	if err != nil {
		return nil, err
	}
	abs, _ := filepath.Abs(root)
	return &Controller{Root: abs, Store: s, Policy: p, Clock: records.RealClock{}, Runner: hostbridge.NativeRunner{}}, nil
}
func Open(root string) (*Controller, error) {
	s, err := ledger.Open(root)
	if err != nil {
		return nil, err
	}
	p, err := s.Policy()
	if err == nil {
		err = ValidatePolicy(p)
	}
	if err != nil {
		s.Close()
		return nil, err
	}
	abs, _ := filepath.Abs(root)
	return &Controller{Root: abs, Store: s, Policy: p, Clock: records.RealClock{}, Runner: hostbridge.NativeRunner{}}, nil
}

// ConfigureAcceptance explicitly opts this runtime into the versioned P8
// projection. The caller must source generations from its protected trust and
// revocation authority; absence keeps all dependency acceptance closed.
func (c *Controller) configureAcceptance(ctx context.Context, policy verification.VerificationPolicy, trustGeneration, revocationGeneration int64) error {
	if err := verification.ValidatePolicy(policy); err != nil {
		return err
	}
	if err := c.Store.EnableAcceptanceProjection(ctx); err != nil {
		return err
	}
	policySHA, err := verification.Digest(policy)
	if err != nil {
		return err
	}
	if err := c.Store.SetAcceptanceAuthorityGenerations(ctx, trustGeneration, revocationGeneration, policySHA); err != nil {
		return err
	}
	c.acceptancePolicy = &policy
	c.acceptanceGuard = ledger.AcceptanceGuard{TrustGeneration: trustGeneration, RevocationGeneration: revocationGeneration, PolicySHA256: policySHA}
	return nil
}

// ConfigureAcceptanceAuthority requires a fresh signed projection of the
// protected trust and revocation generations before enabling dependent work.
func (c *Controller) ConfigureAcceptanceAuthority(ctx context.Context, policy verification.VerificationPolicy, authority records.AcceptanceAuthorityState) error {
	if err := verifyAcceptanceAuthority(policy, authority, c.now()); err != nil {
		return err
	}
	if err := c.configureAcceptance(ctx, policy, authority.TrustGeneration, authority.RevocationGeneration); err != nil {
		return err
	}
	validUntil, _ := time.Parse(time.RFC3339Nano, authority.ValidUntil)
	c.acceptanceGuard.AuthorityValidUntil = validUntil
	return nil
}

func verifyAcceptanceAuthority(policy verification.VerificationPolicy, authority records.AcceptanceAuthorityState, now time.Time) error {
	if authority.Version != records.AcceptanceAuthorityVersion || authority.TrustGeneration < 1 || authority.RevocationGeneration < 1 {
		return fmt.Errorf("invalid acceptance authority state")
	}
	policySHA, err := verification.Digest(policy)
	if err != nil || policySHA != authority.PolicySHA256 {
		return fmt.Errorf("acceptance authority policy binding mismatch")
	}
	observed, err := time.Parse(time.RFC3339Nano, authority.ObservedAt)
	if err != nil {
		return fmt.Errorf("invalid acceptance authority observation time")
	}
	validUntil, err := time.Parse(time.RFC3339Nano, authority.ValidUntil)
	if err != nil || !validUntil.After(now) || observed.After(now) || validUntil.Sub(observed) > 5*time.Minute || now.Sub(observed) > 5*time.Minute {
		return fmt.Errorf("acceptance authority state is stale or outside its validity window")
	}
	unsigned := authority
	sigText := unsigned.Signature
	unsigned.Signature = ""
	message, err := json.Marshal(unsigned)
	if err != nil {
		return err
	}
	sig, err := base64.StdEncoding.DecodeString(sigText)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return fmt.Errorf("invalid acceptance authority signature")
	}
	for _, issuer := range policy.Issuers {
		if issuer.ID != authority.Issuer || issuer.Role != records.AcceptanceAuthorityRole {
			continue
		}
		key, decodeErr := base64.StdEncoding.DecodeString(issuer.PublicKey)
		if decodeErr == nil && len(key) == ed25519.PublicKeySize && ed25519.Verify(ed25519.PublicKey(key), message, sig) {
			return nil
		}
	}
	return fmt.Errorf("acceptance authority signature is not trusted")
}

func (c *Controller) AttachAcceptance(ctx context.Context, receipt verification.AcceptanceReceipt, candidate verification.CandidateManifest, asOf time.Time) error {
	if c.acceptancePolicy == nil {
		return fmt.Errorf("acceptance policy is not configured")
	}
	if err := c.CheckMethodology(ctx, candidate.RunID, candidate.StepID, "acceptance"); err != nil {
		return err
	}
	if err := c.VerifyCandidateLineage(candidate); err != nil {
		return err
	}
	return c.Store.AttachAcceptance(ctx, receipt, candidate, *c.acceptancePolicy, c.acceptanceGuard.TrustGeneration, c.acceptanceGuard.RevocationGeneration, asOf)
}

// CaptureCandidate imports an immutable runtime candidate only after the P7
// ledger proves the exact stopped, quiescent attempt and checkpoint lineage.
func (c *Controller) CaptureCandidate(ctx context.Context, manifest verification.CandidateManifest, sourceRoot, outputDir string) (verification.Candidate, error) {
	absSource, err := filepath.Abs(sourceRoot)
	if err != nil {
		return verification.Candidate{}, err
	}
	state, err := c.Store.Snapshot(ctx)
	if err != nil {
		return verification.Candidate{}, err
	}
	run, err := findRun(state, manifest.RunID)
	if err != nil {
		return verification.Candidate{}, err
	}
	step, err := findStep(run, manifest.StepID)
	if err != nil {
		return verification.Candidate{}, err
	}
	if step.Workspace == "" || absSource != step.Workspace {
		return verification.Candidate{}, fmt.Errorf("candidate source root is not the exact runtime workspace")
	}
	if step.Checkpoint == nil || staging.Verify(*step.Checkpoint) != nil || step.Checkpoint.SnapshotRoot == "" {
		return verification.Candidate{}, fmt.Errorf("candidate requires the exact verified immutable checkpoint snapshot")
	}
	candidate, err := verification.ImportRuntimeCandidate(manifest, step.Checkpoint.SnapshotRoot, outputDir, c)
	if err != nil {
		return verification.Candidate{}, err
	}
	if candidate.SnapshotSHA256 != runtimeSnapshotDigest(step.Checkpoint.Assets) {
		return verification.Candidate{}, fmt.Errorf("candidate inventory differs from the current runtime checkpoint")
	}
	return candidate, nil
}

func (c *Controller) VerifyCandidateLineage(manifest verification.CandidateManifest) error {
	state, err := c.Store.Snapshot(context.Background())
	if err != nil {
		return err
	}
	run, err := findRun(state, manifest.RunID)
	if err != nil {
		return err
	}
	step, err := findStep(run, manifest.StepID)
	if err != nil {
		return err
	}
	// Candidate.InputSHA256 is the digest of the complete captured P7 run
	// manifest; git revisions remain separate BaseRevision/SourceRevision fields.
	if run.Cancelled || (step.Phase != "awaiting-verification" && step.Phase != "blocked") || !step.StopObserved || step.Checkpoint == nil || staging.Verify(*step.Checkpoint) != nil || !step.Checkpoint.Quiescent || !manifest.Quiescent || step.Token.AttemptID != manifest.AttemptID || uint64(step.Token.Fence) != manifest.Fence || uint64(run.Revision) != manifest.CandidateRevision || run.ManifestSHA256 != manifest.InputSHA256 || run.Manifest.Repository != manifest.Repository || manifest.SourceRevision != step.Checkpoint.BaseRevision || manifest.BaseRevision != run.Manifest.InputRevision || step.Checkpoint.BaseRevision == "" || step.Checkpoint.BaseRevision != run.Manifest.InputRevision || step.PreparedSHA256 != manifest.PreparedReceiptSHA256 || step.HostReceiptSHA256 != manifest.HostReceiptSHA256 || step.Checkpoint.Digest != manifest.CheckpointSHA256 {
		return fmt.Errorf("candidate does not bind current stopped P7 attempt, input and receipts")
	}
	if runtimeSnapshotDigest(step.Checkpoint.Assets) != runtimeCandidateSnapshotDigest(manifest.Assets) {
		return fmt.Errorf("candidate inventory does not match the current P7 checkpoint")
	}
	if c.acceptancePolicy == nil || step.Demand == nil {
		return fmt.Errorf("protected acceptance identity policy and persisted selection demand are required")
	}
	planRaw, err := c.capturedInput(run, run.Manifest.Plan)
	if err != nil || usage.Hash(planRaw) != manifest.PlanSHA256 {
		return fmt.Errorf("candidate plan digest does not match captured runtime plan")
	}
	assignmentPath := ""
	for _, spec := range run.Manifest.Steps {
		if spec.ID == manifest.StepID {
			assignmentPath = spec.Assignment
			break
		}
	}
	assignmentRaw, err := c.capturedInput(run, assignmentPath)
	if assignmentPath == "" || err != nil || usage.Hash(assignmentRaw) != manifest.AssignmentSHA256 {
		return fmt.Errorf("candidate assignment digest does not match captured runtime assignment")
	}
	goalRaw, err := c.capturedGoal(run, planRaw)
	if err != nil || usage.Hash(goalRaw) != manifest.GoalSHA256 {
		return fmt.Errorf("candidate goal digest does not match captured runtime goal")
	}
	if step.Demand.ConfigurationID != manifest.ConfigurationSHA256 || step.Demand.ProfileSHA256 != manifest.ProfileSHA256 {
		return fmt.Errorf("candidate configuration or profile does not match persisted selection demand")
	}
	trustedAuthor := false
	for _, binding := range c.acceptancePolicy.AuthorBindings {
		if binding.AssignmentSHA256 == manifest.AssignmentSHA256 && binding.Principal == manifest.AuthorPrincipal {
			trustedAuthor = true
			break
		}
	}
	if !trustedAuthor {
		return fmt.Errorf("candidate author is not bound by protected acceptance policy")
	}
	return nil
}

func (c *Controller) capturedInput(run records.Run, path string) ([]byte, error) {
	if path == "" || evidence.CheckPath(path) != nil {
		return nil, fmt.Errorf("invalid captured input path")
	}
	for _, asset := range run.Manifest.Assets {
		if asset.Path != path {
			continue
		}
		return readAsset(filepath.Join(c.runRoot(run.Manifest.ID), "inputs"), asset)
	}
	return nil, fmt.Errorf("runtime input was not captured: %s", path)
}

func (c *Controller) capturedGoal(run records.Run, planRaw []byte) ([]byte, error) {
	var plan struct {
		GoalRef struct{ Kind, ID, Version string } `json:"goalRef"`
	}
	if err := json.Unmarshal(planRaw, &plan); err != nil || plan.GoalRef.Kind != "GoalContract" || plan.GoalRef.ID == "" || plan.GoalRef.Version == "" {
		return nil, fmt.Errorf("captured plan has no valid goal reference")
	}
	for _, asset := range run.Manifest.Assets {
		if !strings.HasPrefix(asset.Path, strings.TrimSuffix(run.Manifest.Corpus, "/")+"/") {
			continue
		}
		raw, err := readAsset(filepath.Join(c.runRoot(run.Manifest.ID), "inputs"), asset)
		if err != nil {
			return nil, err
		}
		var resource struct {
			Kind     string                       `json:"kind"`
			Metadata struct{ ID, Version string } `json:"metadata"`
		}
		if json.Unmarshal(raw, &resource) == nil && resource.Kind == plan.GoalRef.Kind && resource.Metadata.ID == plan.GoalRef.ID && resource.Metadata.Version == plan.GoalRef.Version {
			return raw, nil
		}
	}
	return nil, fmt.Errorf("referenced goal is not in captured corpus")
}

func runtimeCandidateSnapshotDigest(assets []verification.Asset) string {
	items := make([]records.Asset, len(assets))
	for i, a := range assets {
		items[i] = records.Asset{Path: a.Path, SHA256: a.SHA256, Executable: a.Executable}
	}
	return runtimeSnapshotDigest(items)
}

func runtimeSnapshotDigest(assets []records.Asset) string {
	type item struct {
		Path, SHA  string
		Executable bool
	}
	copy := append([]records.Asset{}, assets...)
	sort.Slice(copy, func(i, j int) bool { return copy[i].Path < copy[j].Path })
	items := make([]item, len(copy))
	for i, a := range copy {
		items[i] = item{a.Path, a.SHA256, a.Executable}
	}
	return usage.Digest(items)
}

// RevokeAcceptance closes the local projection under a newer externally
// obtained revocation generation. The caller then uses ConfigureAcceptance to
// supply the updated protected policy before any new receipt can attach.
func (c *Controller) RevokeAcceptance(ctx context.Context, runID, stepID string, trustGeneration, revocationGeneration int64) error {
	if err := c.Store.RevokeAcceptance(ctx, runID, stepID, trustGeneration, revocationGeneration, c.now()); err != nil {
		return err
	}
	c.acceptanceGuard = ledger.AcceptanceGuard{TrustGeneration: trustGeneration, RevocationGeneration: revocationGeneration}
	return nil
}
func (c *Controller) Close() error   { return c.Store.Close() }
func (c *Controller) now() time.Time { return c.Clock.Now().UTC() }
func newID(prefix string) string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("secure identity unavailable")
	}
	return prefix + hex.EncodeToString(b[:])
}
func (c *Controller) runRoot(id string) string { return filepath.Join(c.Root, "runs", id) }
func findRun(s records.State, id string) (records.Run, error) {
	for _, r := range s.Runs {
		if r.Manifest.ID == id {
			return r, nil
		}
	}
	return records.Run{}, fmt.Errorf("run not found")
}
func findStep(r records.Run, id string) (records.Step, error) {
	for _, s := range r.Steps {
		if s.ID == id {
			return s, nil
		}
	}
	return records.Step{}, fmt.Errorf("step not found")
}
func (c *Controller) Submit(ctx context.Context, path string) (records.Report, error) {
	r := NewReport()
	m, files, sha, err := CaptureManifest(path, c.Policy)
	if err != nil {
		return r, err
	}
	if within(m.Repository, c.Root) || within(c.Root, m.Repository) {
		return r, fmt.Errorf("runtime authority must be outside task repository")
	}
	root := c.runRoot(m.ID)
	// Capture first. An interrupted submission can replay the exact captured manifest;
	// conflicting bytes cannot replace an existing immutable capture.
	if _, err = os.Lstat(root); os.IsNotExist(err) {
		err = writeNew(root, files, m.Repository)
	} else if err == nil {
		err = VerifyCapture(root, m, sha)
	}
	if err != nil {
		return r, err
	}
	_, err = c.Store.Submit(ctx, "submit-"+m.ID, m, sha, c.now())
	if err != nil {
		return r, err
	}
	return c.Status(ctx, m.ID)
}
func (c *Controller) Status(ctx context.Context, id string) (records.Report, error) {
	r := NewReport()
	c.routeGates(&r)
	s, err := c.Store.Snapshot(ctx)
	if err != nil {
		return r, err
	}
	if id != "" {
		run, err := findRun(s, id)
		if err != nil {
			return r, err
		}
		s.Runs = []records.Run{run}
	}
	r.State = &s
	c.capacityViews(ctx, &r)
	r.Reasons = append(r.Reasons, "P6 scope qualification is evaluated separately per configured host route; P7 connected promotion remains separate", "P8 independent acceptance and tracker writeback remain pending")
	return r, nil
}
func (c *Controller) Validate(ctx context.Context) (records.Report, error) {
	r, err := c.Status(ctx, "")
	if err != nil {
		return r, err
	}
	for _, run := range r.State.Runs {
		if err = VerifyCapture(c.runRoot(run.Manifest.ID), run.Manifest, run.ManifestSHA256); err != nil {
			return r, err
		}
		for _, step := range run.Steps {
			if step.Checkpoint != nil {
				if err = staging.Verify(*step.Checkpoint); err != nil {
					return r, err
				}
			}
		}
	}
	return r, nil
}
func (c *Controller) Export(ctx context.Context, id, out string) error {
	r, err := c.Status(ctx, id)
	if err != nil {
		return err
	}
	files := map[string][]byte{"runtime-report.json": Encode(r)}
	run := r.State.Runs[0]
	if err = VerifyCapture(c.runRoot(id), run.Manifest, run.ManifestSHA256); err != nil {
		return err
	}
	files["manifest.json"], err = hostbridge.ReadAsset(c.runRoot(id), "manifest.json")
	if err != nil {
		return err
	}
	for _, a := range run.Manifest.Assets {
		b, err := readAsset(filepath.Join(c.runRoot(id), "inputs"), a)
		if err != nil {
			return err
		}
		files["inputs/"+a.Path] = b
	}
	return evidence.WriteFiles(out, files, c.Root)
}
func (c *Controller) Cancel(ctx context.Context, id, reason string) (records.Report, error) {
	r := NewReport()
	if reason == "" {
		return r, fmt.Errorf("cancellation reason required")
	}
	if err := c.Store.Cancel(ctx, newID("cancel-"), id, reason, c.now()); err != nil {
		return r, err
	}
	return c.Status(ctx, id)
}

func (c *Controller) route(host string, at time.Time, attemptNumber int, stepID string) (hostbridge.Policy, error) {
	for _, r := range c.Policy.Hosts {
		routeAttempt := r.Attempt
		if routeAttempt == 0 {
			routeAttempt = 1
		}
		if r.Host != host || routeAttempt != attemptNumber || r.StepID != "" && r.StepID != stepID {
			continue
		}
		if _, err := readAsset("", r.Policy); err != nil {
			return hostbridge.Policy{}, err
		}
		p, err := hostbridge.ReadPolicy(r.Policy.Path)
		if err != nil {
			return p, err
		}
		if p.Host != host {
			return p, fmt.Errorf("host route mismatch")
		}
		if r.Qualification == nil {
			return p, fmt.Errorf("P6 %s live gate blocked: signed controls and live qualification required", host)
		}
		if _, err = readAsset("", *r.Qualification); err != nil {
			return p, err
		}
		q, err := hostbridge.LoadReceipt(filepath.Dir(r.Qualification.Path))
		if err != nil {
			return p, err
		}
		if filepath.Base(r.Qualification.Path) != "receipt.json" || q.Report.LiveGate != "qualified-live-synthetic" || q.Report.Status != "valid" || q.Host != p.Host || q.HostVersion != p.HostVersion || q.PolicySHA256 != hostbridge.PolicyHash(p) {
			return p, fmt.Errorf("P6 qualification does not bind current host policy")
		}
		if err = hostbridge.VerifyControls(p, at); err != nil {
			return p, err
		}
		return p, nil
	}
	return hostbridge.Policy{}, fmt.Errorf("no approved host route")
}
func (c *Controller) Dispatch(ctx context.Context, id string) (records.Report, error) {
	return c.dispatch(ctx, id, nil)
}

// Calibration admission is an unexported purpose-specific path. Ordinary
// dispatch can never be switched into it by a manifest, flag, or context value.
func (c *Controller) dispatch(ctx context.Context, id string, calibration *calibrationAdmission) (records.Report, error) {
	report := NewReport()
	c.routeGates(&report)
	if c.pilotActivation != nil {
		if err := c.pilotActivation.Check(ctx); err != nil {
			return blockedReport(report, err.Error()), nil
		}
	}
	if !c.Policy.AllowDispatch {
		return blockedReport(report, "operator dispatch is disabled"), nil
	}
	state, err := c.Store.Snapshot(ctx)
	if err != nil {
		return report, err
	}
	run, err := findRun(state, id)
	if err != nil {
		return report, err
	}
	if run.Cancelled {
		return blockedReport(report, "run cancelled; no new admission"), nil
	}
	if err = VerifyCapture(c.runRoot(id), run.Manifest, run.ManifestSHA256); err != nil {
		return report, err
	}
	inputs := filepath.Join(c.runRoot(id), "inputs")
	acceptedSteps := []string(nil)
	if c.acceptancePolicy != nil {
		acceptedSteps = c.Store.AcceptedStepsAt(ctx, id, c.acceptanceGuard, c.now())
	}
	ready := planning.ReadyWithAcceptedDeps(filepath.Join(inputs, run.Manifest.Plan), filepath.Join(inputs, run.Manifest.Evidence), filepath.Join(inputs, run.Manifest.Corpus), c.now().Format(time.RFC3339), acceptedSteps)
	if ready.Status == "invalid" {
		return blockedReport(report, "planning corpus invalid"), nil
	}
	for _, spec := range run.Manifest.Steps {
		if !has(ready.EligibleSteps, spec.ID) {
			continue
		}
		if c.pilotActivation != nil {
			b, ok := c.pilotTrials[id+"\x00"+spec.ID]
			if !ok || b.ManifestSHA256 != run.ManifestSHA256 {
				return blockedReport(report, "pilot trial does not bind captured manifest"), nil
			}
		}
		step, _ := findStep(run, spec.ID)
		if step.Phase != "pending" && step.Phase != "blocked" {
			continue
		}
		dependencies, preconditioned, bindErr := validatePlanBinding(inputs, run.Manifest, spec)
		if bindErr != nil {
			return blockedReport(report, bindErr.Error()), nil
		}
		guard := c.acceptanceGuard
		guard.Dependencies = dependencies
		if preconditioned || len(dependencies) > 0 && (c.acceptancePolicy == nil || !c.Store.DependenciesActiveAt(ctx, id, guard, c.now())) {
			return blockedReport(report, "dependent/preconditioned steps require independent P8 evidence; supplied completed-step declarations cannot grant acceptance"), nil
		}
		if err := c.CheckMethodology(ctx, id, spec.ID, "admission"); err != nil {
			return blockedReport(report, err.Error()), nil
		}
		p, err := c.route(spec.Host, c.now(), step.Attempts+1, spec.ID)
		if err != nil {
			return blockedReport(report, err.Error()), nil
		}
		attempt := filepath.Base(p.Workspace)
		if !identifier.MatchString(attempt) || filepath.Dir(p.Workspace) != c.Policy.StagingRoot {
			return blockedReport(report, "approved host workspace must be the exact unoccupied attempt path under stagingRoot"), nil
		}
		priorCheckpoint := step.Checkpoint
		token, err := c.Store.ClaimWithAcceptance(ctx, "claim-"+attempt, id, spec.ID, newID("controller-"), attempt, c.now(), guard)
		if err != nil {
			return report, err
		}
		preparationCtx, stopPreparation := c.keepClaim(ctx, token)
		defer stopPreparation()
		step, err = findStepFromStore(ctx, c.Store, token)
		if err != nil {
			return report, err
		}
		fail := func(reason string) (records.Report, error) {
			step.Reason = reason
			_, e := c.Store.Update(context.Background(), newID("blocked-"), token, "blocked", step, c.now())
			if e != nil {
				return blockedReport(report, reason+"; state requires reconciliation"), e
			}
			return blockedReport(report, reason), nil
		}
		var cp records.Checkpoint
		if step.Attempts > 1 {
			if priorCheckpoint == nil || !priorCheckpoint.Quiescent || priorCheckpoint.InputRevision != run.Manifest.InputRevision {
				return fail("quiescent matching continuation checkpoint required")
			}
			taskBytes, readErr := hostbridge.ReadAsset(inputs, spec.Selection.Task)
			var task selection.TaskProfile
			if readErr != nil || usage.Decode(taskBytes, &task) != nil || !task.MidRun || !task.SafeCheckpoint {
				return fail("selection task protocol does not authorize safe-checkpoint continuation")
			}
			cp, err = staging.Continue(preparationCtx, *priorCheckpoint, attempt, run.Manifest.InputRevision)
		} else {
			cp, err = staging.StageWithOptions(preparationCtx, run.Manifest.Repository, c.Policy.StagingRoot, attempt, run.Manifest.InputRevision, staging.Options{AllowedUntracked: run.Manifest.AllowedUntracked})
		}
		if err != nil {
			return fail("staging failed: " + err.Error())
		}
		step.Workspace = cp.Workspace
		step.Checkpoint = &cp
		if cp.BaseRevision != run.Manifest.InputRevision || step.Attempts == 1 && inventoryDigest(cp.Assets) != inventoryDigest(run.Manifest.RepositoryAssets) {
			return fail("repository revision or complete source inventory drift")
		}
		if p.Workspace != cp.Workspace {
			return fail("host policy/control scope does not bind staged workspace")
		}
		step, err = c.Store.Update(ctx, "staged-"+attempt, token, "staged", step, c.now())
		if err != nil {
			return report, err
		}
		b, err := hostbridge.Prepare(filepath.Join(inputs, spec.Assignment), filepath.Join(inputs, run.Manifest.Corpus), p)
		if err != nil {
			return fail("host preparation blocked: " + err.Error())
		}
		if b.Prepared == nil {
			return fail("prepared assignment absent")
		}
		prepared := *b.Prepared
		step.PreparedSHA256 = prepared.Digest
		prepDir := filepath.Join(c.runRoot(id), "attempts", attempt, "prepared")
		if err = writeNew(prepDir, b.Files, cp.Workspace); err != nil {
			return fail(err.Error())
		}
		account, err := accounting.Open(c.Policy.AccountingPath, c.Policy.Authority)
		if err != nil {
			return fail("accounting authority unavailable")
		}
		defer account.Close()
		up, _, err := usage.ReadPolicy(c.Policy.UsagePolicy.Path)
		if err != nil {
			return fail(err.Error())
		}
		if _, err = readAsset("", c.Policy.UsagePolicy); err != nil {
			return fail(err.Error())
		}
		snapshot, err := account.ReadSnapshot(ctx)
		if err != nil {
			return fail(err.Error())
		}
		decision, err := selectCurrent(inputs, spec.Selection, snapshot.Export, c.Policy.UsagePolicy.Path, c.now())
		if err != nil {
			return fail("selection failed: " + err.Error())
		}
		if calibration == nil && decision.Disposition != "selected" {
			return fail("selection " + decision.Disposition + ": " + fmt.Sprint(decision.NextEvidence))
		}
		candidate, err := selectedCandidate(decision, prepared)
		var calibrationDemand *usage.Demand
		var calibrationUntil time.Time
		if calibration != nil {
			candidate, calibrationDemand, calibrationUntil, err = calibration.check(decision, prepared, snapshot.Export, up, c.now())
		}
		if p.AccountScope == "" || p.AccountScope != decision.AccountScope {
			return fail("selected account scope differs from native operator policy")
		}
		if err != nil {
			return fail(err.Error())
		}
		if err = verifyTaskRevision(inputs, spec.Selection, run.Manifest, cp, step.Attempts > 1); err != nil {
			return fail(err.Error())
		}
		demand := selection.MaterializeDemand(snapshot.Export, up, candidate.ConfigurationID, candidate.ProfileSHA256, *candidate.Estimate, c.now())
		if calibrationDemand != nil {
			demand = *calibrationDemand
		}
		step.Demand = &demand
		step.DecisionSHA256 = decision.Digest
		decisionUntil, _ := time.Parse(time.RFC3339Nano, decision.ValidUntil)
		if calibration != nil && calibrationUntil.Before(decisionUntil) {
			decisionUntil = calibrationUntil
		}
		taskRaw, readErr := hostbridge.ReadAsset(inputs, spec.Selection.Task)
		var selectedTask selection.TaskProfile
		if readErr != nil || usage.Decode(taskRaw, &selectedTask) != nil {
			return fail("task deadline unavailable")
		}
		if c.trialValidateDemand != nil {
			if err = c.trialValidateDemand(demand, *candidate.Estimate, selectedTask, c.now()); err != nil {
				return fail(err.Error())
			}
		}
		deadline, parseErr := time.Parse(time.RFC3339Nano, selectedTask.Deadline)
		if parseErr != nil {
			return fail("task deadline invalid")
		}
		if deadline.Before(decisionUntil) {
			decisionUntil = deadline
		}
		if !decisionUntil.After(c.now()) {
			return fail("task deadline or decision expired")
		}
		demandUntil, _ := time.Parse(time.RFC3339Nano, demand.ValidUntil)
		until := c.now().Add(time.Duration(c.Policy.LeaseSeconds) * time.Second)
		if decisionUntil.Before(until) {
			until = decisionUntil
		}
		if demandUntil.Before(until) {
			until = demandUntil
		}
		if !until.After(c.now()) {
			return fail("selection or demand expired")
		}
		if err = checkExecutionWindow(usage.Capacity(snapshot.Export, up, demand.ConfigurationID, &demand, c.now()), time.Duration(p.TimeoutSeconds)*time.Second, c.now()); err != nil {
			return fail(err.Error())
		}
		if err = writeNew(filepath.Join(c.runRoot(id), "attempts", attempt, "admission"), map[string][]byte{"selection.json": Encode(decision), "demand.json": Encode(demand)}, cp.Workspace); err != nil {
			return fail(err.Error())
		}
		step, err = c.Store.Update(ctx, "reserve-intent-"+attempt, token, "reservation-intent", step, c.now())
		if err != nil {
			return report, err
		}
		hold, err := account.Reserve(ctx, accounting.ReserveRequest{Key: "reserve-" + attempt, ID: "hold-" + attempt, Owner: token.Owner, AttemptID: attempt, ExpectedRevision: snapshot.Export.LedgerRevision, LeaseUntil: until.Format(time.RFC3339Nano), Demand: demand}, up, c.now())
		if err != nil {
			return fail("capacity reservation blocked: " + err.Error())
		}
		step.Reservation = hold.Reservation
		atoken := accounting.Token{ID: hold.Reservation.ID, Owner: hold.Reservation.Owner, Epoch: hold.Reservation.AuthorityEpoch, Fence: hold.Reservation.Fence}
		step.AccountingToken = &atoken
		step, err = c.Store.Update(ctx, "reserved-"+attempt, token, "reserved", step, c.now())
		if err != nil {
			return report, err
		}
		if preparationCtx.Err() != nil {
			return fail("claim renewal or preparation cancelled")
		}
		stopPreparation()
		return c.execute(ctx, run, spec, step, token, prepared, p, account, up, decisionUntil)
	}
	return blockedReport(report, "no eligible unexecuted step; accepted dependencies require P8 evidence"), nil
}
func blockedReport(r records.Report, reason string) records.Report {
	r.Status = "blocked"
	r.Reasons = append(r.Reasons, reason)
	return r
}
func has(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
func inventoryDigest(xs []records.Asset) string {
	copy := append([]records.Asset{}, xs...)
	sort.Slice(copy, func(i, j int) bool { return copy[i].Path < copy[j].Path })
	return usage.Digest(copy)
}
func findStepFromStore(ctx context.Context, s *ledger.Store, t records.Token) (records.Step, error) {
	state, err := s.Snapshot(ctx)
	if err != nil {
		return records.Step{}, err
	}
	r, err := findRun(state, t.RunID)
	if err != nil {
		return records.Step{}, err
	}
	return findStep(r, t.StepID)
}
func selectCurrent(root string, in records.SelectionInputs, exp usage.Export, policy string, at time.Time) (selection.SelectionDecision, error) {
	parent, err := os.MkdirTemp("", "aok-runtime-select-")
	if err != nil {
		return selection.SelectionDecision{}, err
	}
	defer os.RemoveAll(parent)
	real, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return selection.SelectionDecision{}, err
	}
	dir := filepath.Join(real, "snapshot")
	if err = evidence.WriteJSON(dir, "usage.json", exp); err != nil {
		return selection.SelectionDecision{}, err
	}
	return selection.Select(selection.Request{TaskPath: filepath.Join(root, in.Task), CandidatesPath: filepath.Join(root, in.Candidates), CatalogPath: filepath.Join(root, in.Catalog), CatalogPolicyPath: filepath.Join(root, in.CatalogPolicy), UsagePath: filepath.Join(dir, "usage.json"), UsagePolicyPath: policy, PolicyPath: filepath.Join(root, in.Policy), EvidencePolicyPath: filepath.Join(root, in.EvidencePolicy), AsOf: at})
}
func selectedCandidate(d selection.SelectionDecision, p hostbridge.Prepared) (selection.CandidateResult, error) {
	until, err := time.Parse(time.RFC3339Nano, d.ValidUntil)
	if err != nil || until.IsZero() || d.AuthorizationGranted || d.ExecutionReady {
		return selection.CandidateResult{}, fmt.Errorf("invalid advisory decision")
	}
	for _, candidate := range d.Candidates {
		if d.Inputs["assignment:"+candidate.ConfigurationID] == p.AssignmentSHA256 && candidate.ConfigurationID == d.ConfigurationID && candidate.AccountScope == d.AccountScope && candidate.ConfigurationID == p.ConfigurationID && candidate.ProfileSHA256 == p.ProfileSHA256 && candidate.Eligible && candidate.Estimate != nil {
			return candidate, nil
		}
	}
	return selection.CandidateResult{}, fmt.Errorf("selected configuration/profile differs from prepared assignment")
}

// RepositoryRevision gives P4's opaque input-revision asset concrete meaning for
// runtime admission without changing the selection contract.
type RepositoryRevision struct {
	Version         string `json:"version"`
	Commit          string `json:"commit"`
	InventorySHA256 string `json:"inventorySha256"`
}

func verifyTaskRevision(root string, in records.SelectionInputs, m records.Manifest, cp records.Checkpoint, continuation bool) error {
	raw, err := hostbridge.ReadAsset(root, in.Task)
	if err != nil {
		return err
	}
	var task selection.TaskProfile
	if err = usage.Decode(raw, &task); err != nil {
		return err
	}
	raw, err = readAsset(filepath.Dir(filepath.Join(root, in.Task)), records.Asset{Path: task.InputRevision.Path, SHA256: task.InputRevision.SHA256})
	if err != nil {
		return err
	}
	var revision RepositoryRevision
	if err = usage.Decode(raw, &revision); err != nil {
		return fmt.Errorf("runtime input revision must bind repository commit and complete inventory")
	}
	expectedInventory := inventoryDigest(cp.Assets)
	if continuation {
		expectedInventory = inventoryDigest(m.RepositoryAssets)
	}
	if revision.Version != Version || revision.Commit != m.InputRevision || revision.Commit != cp.BaseRevision || revision.InventorySHA256 != expectedInventory {
		return fmt.Errorf("task input revision differs from staged repository bytes")
	}
	return nil
}

func validatePlanBinding(root string, m records.Manifest, spec records.StepSpec) ([]string, bool, error) {
	raw, err := hostbridge.ReadAsset(root, m.Plan)
	if err != nil {
		return nil, false, err
	}
	var plan struct {
		Steps []struct {
			ID            string              `json:"id"`
			DependsOn     []string            `json:"dependsOn"`
			Preconditions []json.RawMessage   `json:"preconditions"`
			AssignmentRef contracts.Reference `json:"assignmentRef"`
		} `json:"steps"`
	}
	if err = json.Unmarshal(raw, &plan); err != nil {
		return nil, false, err
	}
	assignmentBytes, err := hostbridge.ReadAsset(root, spec.Assignment)
	if err != nil {
		return nil, false, err
	}
	resource, _, err := contracts.Parse(assignmentBytes)
	if err != nil {
		return nil, false, err
	}
	r, ok := resource.(contracts.Resource)
	if !ok {
		return nil, false, fmt.Errorf("assignment resource required")
	}
	typed, err := contracts.DecodeTyped(r)
	if err != nil {
		return nil, false, err
	}
	a, ok := typed.(contracts.AgentAssignment)
	if !ok {
		return nil, false, fmt.Errorf("assignment required")
	}
	for _, step := range plan.Steps {
		if step.ID == spec.ID {
			if step.AssignmentRef.Kind != "AgentAssignment" || step.AssignmentRef.ID != a.Metadata.ID || step.AssignmentRef.Version != a.Metadata.Version {
				return nil, false, fmt.Errorf("plan step does not bind exact assignment")
			}
			return step.DependsOn, len(step.Preconditions) > 0, nil
		}
	}
	return nil, false, fmt.Errorf("manifest step absent from plan")
}

// Gate reporting reads only configured local evidence; it never probes a host.
func (c *Controller) routeGates(report *records.Report) {
	for _, r := range c.Policy.Hosts {
		attempt := r.Attempt
		if attempt == 0 {
			attempt = 1
		}
		if _, err := c.route(r.Host, c.now(), attempt, r.StepID); err != nil {
			continue
		}
		if r.Host == "codex" {
			report.CodexLiveGate = "qualified-live-synthetic-configured-scope"
		} else if r.Host == "claude-code" {
			report.ClaudeLiveGate = "qualified-live-synthetic-configured-scope"
		}
	}
}

func (c *Controller) keepClaim(ctx context.Context, token records.Token) (context.Context, func()) {
	child, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	stopped := make(chan struct{})
	var once sync.Once
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(time.Duration(c.Policy.HeartbeatSeconds) * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-child.Done():
				return
			case <-ticker.C:
				if err := c.Store.Renew(child, newID("preparation-renew-"), token, c.now()); err != nil {
					cancel()
					return
				}
			}
		}
	}()
	return child, func() { once.Do(func() { close(done); <-stopped; cancel() }) }
}

func (c *Controller) capacityViews(ctx context.Context, report *records.Report) {
	if _, err := os.Lstat(c.Policy.AccountingPath); err != nil {
		report.Reasons = append(report.Reasons, "accounting snapshot unavailable; remaining capacity unknown")
		return
	}
	if _, err := readAsset("", c.Policy.UsagePolicy); err != nil {
		report.Reasons = append(report.Reasons, "usage policy unavailable; remaining capacity unknown")
		return
	}
	up, _, err := usage.ReadPolicy(c.Policy.UsagePolicy.Path)
	if err != nil {
		return
	}
	account, err := accounting.Open(c.Policy.AccountingPath, c.Policy.Authority)
	if err != nil {
		report.Reasons = append(report.Reasons, "accounting authority unavailable; remaining capacity unknown")
		return
	}
	defer account.Close()
	snapshot, err := account.ReadSnapshot(ctx)
	if err != nil {
		report.Reasons = append(report.Reasons, "accounting integrity unavailable; remaining capacity unknown")
		return
	}
	ids := map[string]bool{}
	for _, binding := range snapshot.Export.Bindings {
		for _, id := range binding.ConfigurationIDs {
			ids[id] = true
		}
	}
	ordered := []string{}
	for id := range ids {
		ordered = append(ordered, id)
	}
	sort.Strings(ordered)
	for _, id := range ordered {
		view := usage.CurrentConstraints(snapshot.Export, up, id, c.now())
		report.Capacity = append(report.Capacity, records.CapacityView{ConfigurationID: id, AsOf: view.AsOf, Disposition: view.Disposition, Status: view.Status, Balances: view.Balances})
	}
}
