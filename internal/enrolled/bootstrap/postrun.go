package bootstrap

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"reflect"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/cli"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/enrolled/authority"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/enrolled/verifier"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/evidence"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/runtime"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/runtime/records"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/verification"
)

// PostRunRecords are signed operator inputs for the stages that follow a host
// run. CandidatePath names an already-created immutable bundle; it is loaded
// only when the post-run worker starts, never pre-bound to a future output.
type PostRunRecords struct {
	Stage                string                           `json:"stage"`
	CandidatePath        string                           `json:"candidatePath"`
	PolicyPath           string                           `json:"policyPath"`
	StepID               string                           `json:"stepId"`
	AttemptID            string                           `json:"attemptId"`
	PolicySHA256         string                           `json:"policySha256"`
	CandidateInputSHA256 string                           `json:"candidateInputSha256"`
	Policy               verification.VerificationPolicy  `json:"policy"`
	Proof                verification.ControlProof        `json:"proof,omitempty"`
	Execution            *PostRunExecutionGrant           `json:"execution,omitempty"`
	AuthReference        string                           `json:"authReference,omitempty"`
	OutputPath           string                           `json:"outputPath,omitempty"`
	Receipt              verification.AcceptanceReceipt   `json:"receipt,omitempty"`
	AcceptanceAuthority  records.AcceptanceAuthorityState `json:"acceptanceAuthority,omitempty"`
	RuntimeRoot          string                           `json:"runtimeRoot,omitempty"`
	DestinationSnapshot  string                           `json:"destinationSnapshot,omitempty"`
	ExistingReceipts     []verification.ExistingReceipt   `json:"existingReceipts,omitempty"`
	HandoffPath          string                           `json:"handoffPath,omitempty"`
}

const PostRunExecutionVersion = "aok-postrun-execution/v1"

// PostRunExecutionGrant is a narrow operator-signed, one-use authorization
// for exactly one candidate verification attempt. It grants no task acceptance.
type PostRunExecutionGrant struct {
	Version           string `json:"version"`
	Purpose           string `json:"purpose"`
	ParentID          string `json:"parentId"`
	OperationID       string `json:"operationId"`
	IssuedAt          string `json:"issuedAt"`
	ExpiresAt         string `json:"expiresAt"`
	CandidateSHA256   string `json:"candidateSha256"`
	ProofSHA256       string `json:"proofSha256"`
	AnchorEndpoint    string `json:"anchorEndpoint"`
	AnchorPublicKey   string `json:"anchorPublicKey"`
	AnchorTokenRef    string `json:"anchorTokenRef"`
	AnchorStreamID    string `json:"anchorStreamId"`
	ExpectedTipSHA256 string `json:"expectedTipSha256"`
}

func postRunStream(g PostRunExecutionGrant) string {
	return "postrun-" + usage.Digest(struct{ Parent, Operation string }{g.ParentID, g.OperationID})
}
func postRunConsumeDigest(g PostRunExecutionGrant) string {
	return usage.Digest(struct{ Domain, Parent, Operation, Purpose string }{"aok-postrun-consume/v1", g.ParentID, g.OperationID, g.Purpose})
}

// buildPostRunWorker is called only after BuildWorker has verified the signed
// bootstrap envelope and compiled root pin. It never needs activation quorum:
// the exact evidence, qualified endpoint, and trust keys are already enrolled.
func buildPostRunWorker(c Config, w *WorkerConfig) (*WorkerRun, error) {
	return buildPostRunWorkerWithDependencies(c, w, nil, nil)
}

// buildPostRunWorkerWithTransport is unexported so tests can exercise the
// complete upload/execute sequence without adding a serialized transport knob.
func buildPostRunWorkerWithTransport(c Config, w *WorkerConfig, transport http.RoundTripper) (*WorkerRun, error) {
	return buildPostRunWorkerWithDependencies(c, w, transport, nil)
}

func buildPostRunWorkerWithDependencies(c Config, w *WorkerConfig, transport http.RoundTripper, anchor authority.Anchor) (*WorkerRun, error) {
	if err := validatePostRunConfig(c, time.Now().UTC()); err != nil {
		return nil, err
	}
	if w == nil || w.PostRun == nil || (w.Mode != "verify-candidate" && w.Mode != "accept-candidate" && w.Mode != "handoff-candidate") {
		return nil, ErrBlocked
	}
	r := w.PostRun
	if r.CandidatePath == "" || r.CandidateInputSHA256 == "" || r.Policy.ID == "" || verification.ValidatePolicy(r.Policy) != nil {
		return nil, ErrBlocked
	}
	candidate, err := verification.LoadCandidate(r.CandidatePath)
	if err != nil || validatePostRunCandidate(candidate, w.RunID, r) != nil {
		return nil, errors.New("post-run candidate does not match enrolled run and input")
	}
	policyDigest, err := verification.Digest(r.Policy)
	if err != nil || policyDigest != r.PolicySHA256 {
		return nil, errors.New("post-run policy digest differs from enrollment")
	}
	if w.Mode == "verify-candidate" {
		if r.Stage != "verify-candidate" || r.RuntimeRoot == "" || r.OutputPath == "" || r.AuthReference == "" || c.SecretEnv[r.AuthReference] == "" || !validHash(candidate.Digest) || !validHash(candidate.SnapshotSHA256) || postRunPathOverlap(r.RuntimeRoot, r.OutputPath) {
			return nil, ErrBlocked
		}
		if r.Proof.CandidateSHA256 != candidate.Digest || r.Proof.SnapshotSHA256 != candidate.SnapshotSHA256 {
			return nil, errors.New("post-run proof does not bind the actual candidate")
		}
		if anchor == nil {
			key, _ := decodeKey(r.Execution.AnchorPublicKey, ed25519.PublicKeySize)
			anchor = authority.AnchorClient{Endpoint: r.Execution.AnchorEndpoint, StreamID: r.Execution.AnchorStreamID, PublicKey: ed25519.PublicKey(key), Token: func(ctx context.Context) (string, error) { return resolveSecret(c, r.Execution.AnchorTokenRef) }}
		}
		commands := 0
		for _, spec := range r.Policy.Verifiers {
			if spec.Kind == "command" {
				commands++
				if spec.TimeoutSeconds > verification.MaxVerifierTimeoutSeconds {
					return nil, ErrBlocked
				}
			}
		}
		if commands != 1 {
			return nil, errors.New("post-run verification requires exactly one bounded command")
		}
		if err = validatePostRunPolicyFile(r); err != nil {
			return nil, err
		}
		resolver := verification.RemoteSecretResolverFunc(func(ctx context.Context, ref string) (string, error) {
			if ctx == nil || ctx.Err() != nil || ref != r.AuthReference {
				return "", ErrBlocked
			}
			return resolveSecret(c, ref)
		})
		route := verification.RemoteRoute{Endpoint: w.Verifier.Endpoint, AuthReference: r.AuthReference, Resolver: resolver, RoundTripper: transport}
		var spec verification.VerifierSpec
		for _, s := range r.Policy.Verifiers {
			if s.Kind == "command" {
				spec = s
			}
		}
		controls, err := verifier.NewUploadingControls(route, r.Proof, spec, candidate)
		if err != nil {
			return nil, fmt.Errorf("post-run verifier controls rejected: %w", err)
		}
		services := cli.Phase9Services{VerifierControls: func(ctx context.Context, c verification.Candidate, p verification.VerificationPolicy, s verification.VerifierSpec) (verification.Controls, error) {
			if ctx == nil || ctx.Err() != nil || c.Digest != candidate.Digest || c.SnapshotSHA256 != candidate.SnapshotSHA256 || usage.Digest(p) != usage.Digest(r.Policy) || usage.Digest(s) != usage.Digest(spec) {
				return nil, ErrBlocked
			}
			return controls, nil
		}}
		args := []string{"verify", "run", r.CandidatePath, "--policy", workerPolicyPath(w), "--out", r.OutputPath}
		return &WorkerRun{services: services, args: args, run: func(ctx context.Context, out, errout io.Writer) int {
			if ctx == nil || ctx.Err() != nil {
				return 1
			}
			// The root-pinned runtime ledger, rather than the manifest's origin
			// marker, is authoritative for candidate lineage. Recheck immediately
			// before the single-use consume and remote upload.
			controller, lineageErr := runtime.Open(r.RuntimeRoot)
			if lineageErr != nil {
				fmt.Fprintln(errout, "protected runtime lineage ledger unavailable")
				return 1
			}
			lineageErr = controller.ConfigureAcceptanceAuthority(ctx, r.Policy, r.AcceptanceAuthority)
			if lineageErr == nil {
				lineageErr = controller.VerifyCandidateLineage(candidate.Manifest)
			}
			_ = controller.Store.Close()
			if lineageErr != nil {
				fmt.Fprintf(errout, "candidate is not bound to the current stopped runtime attempt: %v\n", lineageErr)
				return 1
			}
			bounded, cancel := context.WithDeadline(ctx, mustPostRunExpiry(r.Execution))
			defer cancel()
			tip, err := anchor.Current(bounded, r.Execution.AnchorStreamID)
			next := postRunConsumeDigest(*r.Execution)
			if err != nil || tip != r.Execution.ExpectedTipSHA256 || tip == next {
				fmt.Fprintln(errout, "post-run authorization already consumed or unavailable")
				return 1
			}
			if err = anchor.Advance(bounded, r.Execution.AnchorStreamID, tip, next); err != nil {
				fmt.Fprintln(errout, "post-run authorization consumption uncertain; no retry")
				return 1
			}
			return cli.RunWithPhase10Services(bounded, args, out, errout, cli.Phase10Services{Phase9: services})
		}}, nil
	}
	if r.Stage != w.Mode || r.RuntimeRoot == "" || r.AcceptanceAuthority.Version != records.AcceptanceAuthorityVersion || r.Receipt.Signature == "" {
		return nil, ErrBlocked
	}
	if w.Mode == "handoff-candidate" && (r.HandoffPath == "" || r.DestinationSnapshot == "" || postRunPathOverlap(r.RuntimeRoot, r.HandoffPath)) {
		return nil, ErrBlocked
	}
	// Load and validate everything before opening mutable runtime state.
	if err := validatePostRunAcceptance(candidate, r, time.Now().UTC()); err != nil {
		return nil, err
	}
	if w.Mode == "handoff-candidate" && r.Receipt.Disposition != "accepted" {
		return nil, ErrBlocked
	}
	return &WorkerRun{run: func(ctx context.Context, out, errout io.Writer) int {
		if ctx == nil || ctx.Err() != nil {
			return 1
		}
		controller, e := runtime.Open(r.RuntimeRoot)
		if e != nil {
			return 1
		}
		defer controller.Store.Close()
		if e = controller.ConfigureAcceptanceAuthority(ctx, r.Policy, r.AcceptanceAuthority); e != nil {
			fmt.Fprintln(errout, "current protected acceptance/revocation projection rejected")
			return 1
		}
		if e = controller.AttachAcceptance(ctx, r.Receipt, candidate.Manifest, time.Now().UTC()); e != nil {
			fmt.Fprintln(errout, "accepted candidate lineage could not be projected")
			return 1
		}
		if w.Mode == "accept-candidate" {
			_, _ = fmt.Fprintln(out, "external acceptance attached to exact candidate lineage")
			return 0
		}
		h, e := verification.BuildHandoff(candidate, r.Receipt, usage.Digest(r.Receipt), r.DestinationSnapshot, r.ExistingReceipts)
		if e != nil {
			fmt.Fprintln(errout, e)
			return 1
		}
		b, e := json.MarshalIndent(h, "", "  ")
		if e != nil {
			return 1
		}
		b = append(b, '\n')
		if e = evidence.WriteFiles(r.HandoffPath, map[string][]byte{"handoff.json": b}, r.CandidatePath); e != nil {
			fmt.Fprintln(errout, e)
			return 1
		}
		_, _ = out.Write(b)
		return 0
	}}, nil
}

func validatePostRunCandidate(candidate verification.Candidate, runID string, r *PostRunRecords) error {
	if r == nil || candidate.BundlePath == "" || candidate.Digest == "" || candidate.SnapshotSHA256 == "" || candidate.Manifest.Origin != "runtime" || verification.ValidateManifest(candidate.Manifest) != nil || candidate.Manifest.RunID != runID || candidate.Manifest.StepID != r.StepID || candidate.Manifest.AttemptID != r.AttemptID || candidate.Manifest.InputSHA256 != r.CandidateInputSHA256 {
		return ErrBlocked
	}
	return nil
}

func validatePostRunConfig(c Config, now time.Time) error {
	w := c.Worker
	if c.Service != "worker" || c.Authority != nil || c.Verifier != nil || w == nil || w.RunID == "" || w.PostRun == nil || w.Calibration != nil || w.Experiment != nil || w.GitHubRead != nil || w.CodexQuota != nil || w.GitHubWrite != nil || !reflect.DeepEqual(w.Authority, WorkerAuthority{}) || w.ProtectedTrustKey != "" {
		return ErrBlocked
	}
	r := w.PostRun
	if r.Stage != w.Mode || r.CandidatePath == "" || r.PolicyPath == "" || r.StepID == "" || r.AttemptID == "" || r.CandidateInputSHA256 == "" || r.PolicySHA256 == "" || verification.ValidatePolicy(r.Policy) != nil || validatePostRunPolicyFile(r) != nil {
		return ErrBlocked
	}
	pd, err := verification.Digest(r.Policy)
	if err != nil || pd != r.PolicySHA256 {
		return ErrBlocked
	}
	candidate, err := verification.LoadCandidate(r.CandidatePath)
	if err != nil || validatePostRunCandidate(candidate, w.RunID, r) != nil {
		return ErrBlocked
	}
	switch w.Mode {
	case "verify-candidate":
		if r.RuntimeRoot == "" || postRunPathOverlap(r.RuntimeRoot, r.OutputPath) || r.OutputPath == "" || r.AuthReference == "" || c.SecretEnv[r.AuthReference] == "" || r.AcceptanceAuthority.Version != records.AcceptanceAuthorityVersion || r.AcceptanceAuthority.Signature == "" || r.Proof.CandidateSHA256 != candidate.Digest || r.Proof.SnapshotSHA256 != candidate.SnapshotSHA256 || r.Proof.Scope != "candidate-offline" || r.Proof.Signature == "" || w.Verifier.Endpoint == "" || w.Verifier.AuthReference != "" || w.Verifier.Proof.Signature != "" || w.Verifier.Policy.ID != "" || validatePostRunExecution(c, r, candidate, now) != nil {
			return ErrBlocked
		}
		commands := 0
		for _, spec := range r.Policy.Verifiers {
			if spec.Kind == "command" {
				commands++
				if spec.TimeoutSeconds > verification.MaxVerifierTimeoutSeconds {
					return ErrBlocked
				}
				if err = verification.VerifyControlProof(r.Proof, pd, mustDigest(spec), r.Policy.Issuers, now); err != nil {
					return ErrBlocked
				}
			}
		}
		if commands != 1 || !validHTTPSPath(w.Verifier.Endpoint, "/v1/execute") {
			return ErrBlocked
		}
	case "accept-candidate", "handoff-candidate":
		if r.Execution != nil || r.RuntimeRoot == "" || r.Receipt.Signature == "" || r.AcceptanceAuthority.Version == "" || w.Verifier.Endpoint != "" || w.Verifier.AuthReference != "" || w.Verifier.Proof.Signature != "" || w.Verifier.Policy.ID != "" || w.Mode == "handoff-candidate" && (r.HandoffPath == "" || r.DestinationSnapshot == "" || postRunPathOverlap(r.RuntimeRoot, r.HandoffPath)) {
			return ErrBlocked
		}
	default:
		return ErrBlocked
	}
	return nil
}

func postRunPathOverlap(a, b string) bool {
	if a == "" || b == "" {
		return true
	}
	aa, errA := filepath.Abs(a)
	bb, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return true
	}
	aa, errA = filepath.EvalSymlinks(aa)
	bb, errB = filepath.EvalSymlinks(bb)
	if errA != nil || errB != nil {
		return filepath.Clean(aa) == filepath.Clean(bb) || isWithinPath(a, b) || isWithinPath(b, a)
	}
	return isWithinPath(aa, bb) || isWithinPath(bb, aa)
}

func isWithinPath(parent, child string) bool {
	rel, err := filepath.Rel(filepath.Clean(parent), filepath.Clean(child))
	return err == nil && (rel == "." || (rel != ".." && !filepath.IsAbs(rel) && !startsWithPathParent(rel)))
}

func startsWithPathParent(rel string) bool {
	return len(rel) >= 3 && rel[:3] == ".."+string(filepath.Separator)
}

func validatePostRunExecution(c Config, r *PostRunRecords, candidate verification.Candidate, now time.Time) error {
	if r == nil || r.Execution == nil {
		return ErrBlocked
	}
	g := r.Execution
	issued, ie := time.Parse(time.RFC3339Nano, g.IssuedAt)
	expires, ee := time.Parse(time.RFC3339Nano, g.ExpiresAt)
	proofDigest, pe := verification.Digest(r.Proof)
	if g.Version != PostRunExecutionVersion || g.Purpose != "post-run-verification-only" || !qualificationID.MatchString(g.ParentID) || !qualificationID.MatchString(g.OperationID) || ie != nil || ee != nil || issued.After(now) || !now.Before(expires) || !expires.After(issued) || expires.Sub(issued) > 5*time.Minute || g.CandidateSHA256 != candidate.Digest || pe != nil || g.ProofSHA256 != proofDigest || g.AnchorStreamID != postRunStream(*g) || !validHash(g.ExpectedTipSHA256) || g.ExpectedTipSHA256 == postRunConsumeDigest(*g) || !validHTTPSPath(g.AnchorEndpoint, "/v1/anchor") || c.SecretEnv[g.AnchorTokenRef] == "" {
		return ErrBlocked
	}
	if _, err := decodeKey(g.AnchorPublicKey, ed25519.PublicKeySize); err != nil {
		return ErrBlocked
	}
	return nil
}

func mustPostRunExpiry(g *PostRunExecutionGrant) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, g.ExpiresAt)
	return t
}

func mustDigest(v any) string { d, _ := verification.Digest(v); return d }

func validatePostRunAcceptance(candidate verification.Candidate, r *PostRunRecords, now time.Time) error {
	if r == nil || verification.VerifyAcceptance(r.Receipt, r.Policy.Issuers, now, time.Duration(r.Policy.MaxAgeSeconds)*time.Second) != nil {
		return errors.New("external acceptance receipt is invalid or stale")
	}
	if verification.ValidateAcceptanceBinding(r.Receipt, candidate, r.Policy) != nil {
		return errors.New("external acceptance does not bind current candidate")
	}
	return nil
}

// workerPolicyPath is intentionally a file path from the signed record. The
// loader below checks that the file is byte-equivalent to the embedded policy.
func workerPolicyPath(w *WorkerConfig) string {
	if w == nil || w.PostRun == nil {
		return ""
	}
	return w.PostRun.PolicyPath
}

func validatePostRunPolicyFile(r *PostRunRecords) error {
	if r == nil || r.PolicyPath == "" {
		return ErrBlocked
	}
	b, err := readSafe(r.PolicyPath, verification.MaxPolicyFrameBytes, false)
	if err != nil {
		return ErrBlocked
	}
	p, err := verification.DecodePolicy(b)
	if err != nil || usage.Digest(p) != usage.Digest(r.Policy) {
		return ErrBlocked
	}
	return nil
}
