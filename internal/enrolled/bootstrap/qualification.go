package bootstrap

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sync"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/enrolled/authority"
	codexcollector "github.com/ArmanAvanesyan/agent-operating-kit/internal/enrolled/codex"
	githubcollector "github.com/ArmanAvanesyan/agent-operating-kit/internal/enrolled/github"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/evidence"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/hostbridge"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/modelcatalog"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
	accounting "github.com/ArmanAvanesyan/agent-operating-kit/internal/usage/store"
)

const QualificationVersion = "aok-qualification-bootstrap/v1"

// QualificationRecords is an operator-signed, one-use synthetic authorization.
// It is not an authority.Enrollment, readiness proof, acceptance, or pilot grant.
// No private signing keys are accepted, and observations never auto-promote.
type QualificationRecords struct {
	Version           string               `json:"version"`
	Purpose           string               `json:"purpose"`
	ParentID          string               `json:"parentId"`
	OperationID       string               `json:"operationId"`
	Action            string               `json:"action"`
	IssuedAt          string               `json:"issuedAt"`
	ExpiresAt         string               `json:"expiresAt"`
	AnchorEndpoint    string               `json:"anchorEndpoint"`
	AnchorPublicKey   string               `json:"anchorPublicKey"`
	AnchorTokenRef    string               `json:"anchorTokenRef"`
	AnchorStreamID    string               `json:"anchorStreamId"`
	ExpectedTipSHA256 string               `json:"expectedTipSha256"`
	OutputPath        string               `json:"outputPath"`
	GitHubWrite       *GitHubWriteRecords  `json:"githubWrite,omitempty"`
	GitHubRead        *GitHubReadConfig    `json:"githubRead,omitempty"`
	CodexQuota        *CodexQuotaConfig    `json:"codexQuota,omitempty"`
	Verifier          *VerifierConformance `json:"verifier,omitempty"`
	Host              *HostConformance     `json:"host,omitempty"`
}

// Matrix demand is an explicitly trusted operator upper bound for the ENTIRE
// fixed matrix. It is unmeasured evidence, never inferred from token counts.
// Unknown bounds block; the full reservation remains uncertain after use.
type HostConformance struct {
	DemandScope          string                    `json:"demandScope"`
	DemandEvidenceSHA256 string                    `json:"demandEvidenceSha256"`
	MaximumNativeTurns   int                       `json:"maximumNativeTurns"`
	MaximumTurnSeconds   int                       `json:"maximumTurnSeconds"`
	PolicyPath           string                    `json:"policyPath"`
	PolicySHA256         string                    `json:"policySha256"`
	AccountingPath       string                    `json:"accountingPath"`
	AccountingAuthority  string                    `json:"accountingAuthority"`
	UsagePolicy          usage.Policy              `json:"usagePolicy"`
	Reserve              accounting.ReserveRequest `json:"reserve"`
}

func qualificationStream(q QualificationRecords) string {
	return "qualification-" + usage.Digest(struct{ Parent, Operation string }{q.ParentID, q.OperationID})
}
func qualificationConsumeDigest(q QualificationRecords) string {
	return usage.Digest(struct{ Version, Parent, Operation, Purpose string }{QualificationVersion, q.ParentID, q.OperationID, q.Purpose})
}
func qualificationEnd(q QualificationRecords) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, q.ExpiresAt)
}
func validateQualification(c Config, at time.Time) error {
	q := c.Qualification
	if c.Service != "qualification" || q == nil || c.Worker != nil || c.Authority != nil || c.Anchor != nil || c.Verifier != nil || c.Listen != "" || c.TLS.Certificate != "" || c.TLS.PrivateKey != "" {
		return ErrBlocked
	}
	issued, ie := time.Parse(time.RFC3339Nano, q.IssuedAt)
	end, ee := qualificationEnd(*q)
	if q.Version != QualificationVersion || q.Purpose != "synthetic-conformance-only" || !qualificationID.MatchString(q.ParentID) || !qualificationID.MatchString(q.OperationID) || ie != nil || ee != nil || issued.After(at) || !at.Before(end) || !end.After(issued) || end.Sub(issued) > 5*time.Minute || q.OutputPath == "" || q.AnchorStreamID != qualificationStream(*q) || !validHash(q.ExpectedTipSHA256) || !validHTTPSPath(q.AnchorEndpoint, "/v1/anchor") || c.SecretEnv[q.AnchorTokenRef] == "" {
		return ErrBlocked
	}
	if _, e := decodeKey(q.AnchorPublicKey, ed25519.PublicKeySize); e != nil {
		return ErrBlocked
	}
	if q.ExpectedTipSHA256 == qualificationConsumeDigest(*q) {
		return ErrBlocked
	}
	switch q.Action {
	case "github-read":
		g := q.GitHubRead
		if g == nil || q.Verifier != nil || q.GitHubWrite != nil || q.CodexQuota != nil || q.Host != nil || g.Collector.Policy == nil || c.SecretEnv[g.Credential.EnvReference] == "" || g.Credential.AuthReference != g.Collector.AuthReference || g.Credential.PrincipalPseudonym != g.Collector.Source.PrincipalPseudonym || !sameStrings(g.Credential.Scopes, []string{"issues:read", "metadata:read"}) || len(g.ReadAuthorizations) == 0 {
			return ErrBlocked
		}
		until, e := time.Parse(time.RFC3339Nano, g.Credential.ValidUntil)
		if e != nil || !at.Before(until) {
			return ErrBlocked
		}
	case "codex-quota":
		x := q.CodexQuota
		if x == nil || q.Verifier != nil || q.GitHubWrite != nil || q.GitHubRead != nil || q.Host != nil || x.Collector.SourceID != "qualification:"+usage.Digest(struct{ Parent, Operation string }{q.ParentID, q.OperationID}) || x.Binding.SourceID != x.Collector.SourceID || usage.ValidateBinding(x.Binding) != nil || usage.ValidatePolicy(x.Policy) != nil {
			return ErrBlocked
		}
	case "verifier-conformance":
		return validateQualificationVerifier(c, q, at)
	case "github-comment":
		if q.Verifier != nil {
			return ErrBlocked
		}
		return validateQualificationGitHub(c, q, at)
	case "host-conformance":
		h := q.Host
		if h == nil || q.Verifier != nil || h.DemandScope != "entire-matrix-upper-bound" || !validHash(h.DemandEvidenceSHA256) || h.MaximumNativeTurns != 10 || h.MaximumTurnSeconds != 30 || q.GitHubWrite != nil || q.GitHubRead != nil || q.CodexQuota != nil || !validHash(h.PolicySHA256) || h.PolicyPath == "" || h.AccountingPath == "" || h.AccountingAuthority == "" || usage.ValidatePolicy(h.UsagePolicy) != nil || usage.ValidateDemand(h.Reserve.Demand) != nil || h.Reserve.ID != q.OperationID || h.Reserve.AttemptID != q.OperationID {
			return ErrBlocked
		}
		for _, a := range h.Reserve.Demand.Amounts {
			n, e := usage.NumberRat(a.Amount)
			if e != nil || n.Sign() <= 0 {
				return ErrBlocked
			}
		}
	default:
		return ErrBlocked
	}
	return nil
}

func BuildQualification(configPath, rootPath, pin string, now time.Time) (*WorkerRun, error) {
	_, c, _, e := loadAndVerify(configPath, rootPath, pin, true, now)
	if e != nil {
		return nil, e
	}
	if e = validateServiceConfig(c, now); e != nil {
		return nil, e
	}
	return buildQualification(c, nil, configPath, rootPath)
}

// transport injection is package-private and never a serialized option.
func buildQualification(c Config, anchor authority.Anchor, protected ...string) (*WorkerRun, error) {
	q := c.Qualification
	if q == nil || validateQualification(c, time.Now().UTC()) != nil {
		return nil, ErrBlocked
	}
	if anchor == nil {
		key, _ := decodeKey(q.AnchorPublicKey, ed25519.PublicKeySize)
		anchor = authority.AnchorClient{Endpoint: q.AnchorEndpoint, StreamID: q.AnchorStreamID, PublicKey: ed25519.PublicKey(key), Token: func(context.Context) (string, error) { return resolveSecret(c, q.AnchorTokenRef) }}
	}
	return &WorkerRun{run: func(ctx context.Context, out, errout io.Writer) int {
		if ctx == nil || validateQualification(c, time.Now().UTC()) != nil {
			return 1
		}
		end, _ := qualificationEnd(*q)
		bounded, cancel := context.WithDeadline(ctx, end)
		defer cancel()
		paths := append([]string{}, protected...)
		paths = append(paths, q.HostPath())
		if q.Verifier != nil {
			paths = append(paths, q.Verifier.CandidatePath)
		}
		if q.Host != nil {
			paths = append(paths, q.Host.AccountingPath)
		}
		if q.CodexQuota != nil {
			paths = append(paths, q.CodexQuota.Collector.Home)
		}
		if q.GitHubWrite != nil {
			paths = append(paths, q.GitHubWrite.OutboxPath, q.GitHubWrite.CommentAssetsRoot, q.GitHubWrite.EvidenceRoot)
		}
		writer, e := evidence.Reserve(q.OutputPath, paths...)
		if e != nil {
			fmt.Fprintln(errout, "qualification output unavailable")
			return 1
		}
		// A stable parent+operation has one pre-enrolled stream and one immutable
		// expected tip across renewal. CAS consumes before any native/provider IO.
		tip, e := anchor.Current(bounded, q.AnchorStreamID)
		if e != nil || tip != q.ExpectedTipSHA256 {
			fmt.Fprintln(errout, "qualification authorization already consumed or unavailable")
			return 1
		}
		next := qualificationConsumeDigest(*q)
		if e = anchor.Advance(bounded, q.AnchorStreamID, tip, next); e != nil {
			fmt.Fprintln(errout, "qualification consumption uncertain; no retry")
			return 1
		}
		files, e := runQualificationAction(bounded, c, *q)
		result := map[string]any{"version": QualificationVersion, "purpose": q.Purpose, "parentId": q.ParentID, "operationId": q.OperationID, "action": q.Action, "inputSha256": usage.Digest(q), "observationOnly": true, "authorizationGranted": false, "executionReady": false, "pilotAdmission": false, "productionEligible": false, "liveQualification": "independent-review-required", "status": "observed"}
		if e != nil {
			result["status"] = "blocked-or-incomplete"
		}
		b, _ := json.MarshalIndent(result, "", "  ")
		if files == nil {
			files = map[string][]byte{}
		}
		files["qualification-observation.json"] = append(b, '\n')
		if we := writer.Write(files); we != nil {
			fmt.Fprintln(errout, "qualification evidence preservation failed")
			return 1
		}
		fmt.Fprintln(out, string(b))
		if e != nil {
			fmt.Fprintln(errout, "bounded conformance incomplete; no promotion or retry")
			return 1
		}
		return 0
	}}, nil
}

// The entire config is already copied by strict root-signed decoding. Protected
// state paths are passed separately to the private non-overwriting writer.
func (q QualificationRecords) HostPath() string {
	if q.Host != nil {
		return q.Host.PolicyPath
	}
	return ""
}
func runQualificationAction(ctx context.Context, c Config, q QualificationRecords) (map[string][]byte, error) {
	switch q.Action {
	case "github-read":
		g := q.GitHubRead
		resolver := githubcollector.TokenResolverFunc(func(ctx context.Context, ref string) (githubcollector.Token, error) {
			if ref != g.Credential.AuthReference {
				return githubcollector.Token{}, ErrBlocked
			}
			v, e := resolveSecret(c, g.Credential.EnvReference)
			until, _ := time.Parse(time.RFC3339Nano, g.Credential.ValidUntil)
			return githubcollector.Token{Value: v, PrincipalPseudonym: g.Credential.PrincipalPseudonym, GrantRef: g.Credential.GrantRef, Scopes: g.Credential.Scopes, ValidUntil: until}, e
		})
		auth := githubcollector.AuthorityFunc(func(ctx context.Context, a githubcollector.Authorization) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			for _, expected := range g.ReadAuthorizations {
				if usage.Digest(a) == usage.Digest(expected) {
					return nil
				}
			}
			return ErrBlocked
		})
		collector, e := githubcollector.New(g.Collector, resolver, auth)
		if e != nil {
			return nil, e
		}
		captured, e := collector.Collect(ctx, "capture")
		if e != nil {
			return nil, e
		}
		raw, e := json.Marshal(captured)
		if e != nil {
			return nil, e
		}
		return map[string][]byte{"github-observation.json": raw}, nil
	case "codex-quota":
		x := q.CodexQuota
		guard := &qualificationCapture{q: q, account: x.Collector.AccountRef, source: x.Collector.SourceID}
		factory, e := codexcollector.New(x.Collector, guard, guard)
		if e != nil {
			return nil, e
		}
		capture, e := factory.CollectUsage(ctx, x.Binding, x.Policy, time.Now().UTC())
		if e != nil {
			return nil, e
		}
		return capture.Assets, nil
	case "verifier-conformance":
		return runQualificationVerifier(ctx, c, q, nil)
	case "github-comment":
		return runQualificationGitHub(ctx, c, &q)
	case "host-conformance":
		return qualifyHost(ctx, q)
	}
	return nil, ErrBlocked
}

type qualificationCapture struct {
	q                QualificationRecords
	account, source  string
	active, complete bool
}

func (g *qualificationCapture) Acquire(ctx context.Context, account, source string) (codexcollector.CaptureGrant, error) {
	if ctx.Err() != nil || g.active || g.complete || account != g.account || source != g.source {
		return codexcollector.CaptureGrant{}, ErrBlocked
	}
	g.active = true
	end, _ := qualificationEnd(g.q)
	if t := time.Now().Add(5 * time.Second); t.Before(end) {
		end = t
	}
	return codexcollector.CaptureGrant{Ordinal: 1, Revision: 1, LeaseID: g.q.OperationID, AccountRef: account, SourceID: source, ValidUntil: end, ProofRef: g.q.ParentID, ProofSHA256: usage.Digest(g.q)}, nil
}
func (g *qualificationCapture) Check(ctx context.Context, s codexcollector.Scope) error {
	if ctx.Err() != nil || !g.active || g.complete || s.AccountRef != g.account || s.SourceID != g.source || s.Ordinal != 1 || s.Revision != 1 || s.LeaseID != g.q.OperationID || !time.Now().Before(s.ValidUntil) {
		return ErrBlocked
	}
	return nil
}
func (g *qualificationCapture) Complete(ctx context.Context, grant codexcollector.CaptureGrant, result codexcollector.CaptureResult) error {
	if ctx.Err() != nil || !g.active || g.complete || grant.LeaseID != g.q.OperationID || !validHash(result.SHA256) {
		return ErrBlocked
	}
	g.complete = true
	return nil
}

func qualifyHost(ctx context.Context, q QualificationRecords) (files map[string][]byte, err error) {
	h := q.Host
	raw, e := readSafe(h.PolicyPath, 4<<20, false)
	if e != nil || usage.Hash(raw) != h.PolicySHA256 {
		return nil, ErrBlocked
	}
	policy, e := hostbridge.ReadPolicy(h.PolicyPath)
	if e != nil || hostbridge.PolicyHash(policy) != h.PolicySHA256 || policy.Host != "codex" || policy.HostVersion != codexcollector.PinnedVersion || policy.AllowNativeTasks || policy.TimeoutSeconds > 30 || len(policy.Models) != 1 || h.Reserve.Demand.ConfigurationID != modelcatalog.ConfigurationID(policy.Configuration) {
		return nil, ErrBlocked
	}
	end, _ := qualificationEnd(q)
	for _, proof := range policy.Controls {
		t, e := time.Parse(time.RFC3339Nano, proof.ExpiresAt)
		if e != nil || t.Before(end) {
			return nil, ErrBlocked
		}
	}
	lease, e := time.Parse(time.RFC3339Nano, h.Reserve.LeaseUntil)
	if e != nil || lease.Before(end) {
		return nil, ErrBlocked
	}
	profile, pe := hostbridge.QualificationProfileDigest(policy)
	if pe != nil || h.Reserve.Demand.ProfileSHA256 != profile {
		return nil, ErrBlocked
	}
	store, e := accounting.Open(h.AccountingPath, h.AccountingAuthority)
	if e != nil {
		return nil, e
	}
	defer store.Close()
	reserved, e := store.Reserve(ctx, h.Reserve, h.UsagePolicy, time.Now().UTC())
	if e != nil || reserved.Reservation == nil {
		return nil, ErrBlocked
	}
	r := reserved.Reservation
	token := accounting.Token{ID: r.ID, Owner: r.Owner, Epoch: r.AuthorityEpoch, Fence: r.Fence}
	_, e = store.ConfirmLaunch(ctx, accounting.Transition{Key: q.OperationID + ":launch", Token: token, ProfileSHA256: h.Reserve.Demand.ProfileSHA256}, h.UsagePolicy, time.Now().UTC())
	if e != nil {
		return nil, e
	}
	// Qualification is not task acceptance and cannot derive actual subscription
	// percentages. Keep the complete preregistered matrix demand uncertain.
	defer func() {
		bounded, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, markErr := store.MarkUncertain(bounded, accounting.Transition{Key: q.OperationID + ":uncertain", Token: token}, h.UsagePolicy, time.Now().UTC())
		if markErr != nil {
			err = errors.New("accounting outcome unresolved; hold retained")
		}
	}()
	guarded := &qualificationHostRunner{policy: policy, store: store, token: token, usagePolicy: h.UsagePolicy, demand: h.Reserve.Demand}
	bundle, e := hostbridge.Qualify(ctx, policy, guarded)
	if e != nil {
		return bundle.Files, e
	}
	if bundle.Receipt == nil || bundle.Receipt.Report.Status == "blocked" {
		return bundle.Files, errors.New("native qualification incomplete")
	}
	return bundle.Files, nil
}

// Only the existing native P6 runner executes; this guard rechecks the P3 hold
// and current capacity at each synthetic start. It is not a pilot runner.
type qualificationHostRunner struct {
	mu          sync.Mutex
	starts      int
	policy      hostbridge.Policy
	store       *accounting.Store
	token       accounting.Token
	usagePolicy usage.Policy
	demand      usage.Demand
}

func (g *qualificationHostRunner) Start(ctx context.Context, l hostbridge.Launch) (hostbridge.Process, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if ctx.Err() != nil || l.Executable != g.policy.Executable || l.Dir != g.policy.Workspace || l.TimeoutSeconds > 30 || l.TimeoutSeconds < 1 {
		return nil, ErrBlocked
	}
	state, e := g.store.ReadSnapshot(ctx)
	if e != nil {
		return nil, e
	}
	found := false
	for i := range state.Export.Reservations {
		r := &state.Export.Reservations[i]
		if r.ID == g.token.ID {
			until, _ := time.Parse(time.RFC3339Nano, r.LeaseUntil)
			if r.Owner != g.token.Owner || r.Fence != g.token.Fence || r.AuthorityEpoch != g.token.Epoch || r.State != "in-flight" || !time.Now().Before(until) {
				return nil, ErrBlocked
			}
			r.State = "released"
			found = true
		}
	}
	if !found || state.Epoch != g.token.Epoch {
		return nil, ErrBlocked
	}
	cap := usage.Capacity(state.Export, g.usagePolicy, g.demand.ConfigurationID, &g.demand, time.Now().UTC())
	if cap.Disposition != "observed-fit" || cap.Status != "valid" {
		return nil, ErrBlocked
	}
	end := time.Now().Add(time.Duration(l.TimeoutSeconds) * time.Second)
	if dl, ok := ctx.Deadline(); ok && dl.Before(end) {
		end = dl
	}
	for _, b := range cap.Balances {
		if b.ResetAt != nil {
			reset, e := time.Parse(time.RFC3339Nano, *b.ResetAt)
			if e != nil || !end.Before(reset) {
				return nil, ErrBlocked
			}
		}
	}
	discovery := (len(l.Args) == 1 && l.Args[0] == "--version") || (len(l.Args) == 3 && l.Args[0] == "auth" && l.Args[1] == "status" && l.Args[2] == "--json")
	if !discovery {
		if g.starts >= 10 {
			return nil, ErrBlocked
		}
		g.starts++
	}
	return (hostbridge.NativeRunner{}).Start(ctx, l)
}

var qualificationID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
