package operations

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
)

const CompletionLedgerVersion = "aok-completion-ledger/v1"
const maxCompletionLedgerBytes = 2 << 20
const maxCompletionGates = 128

var completionID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,95}$`)
var completionHash = regexp.MustCompile(`^[0-9a-f]{64}$`)

// CompletionEvidenceRef identifies external evidence without treating its
// presence or its self-reported fields as verification.
type CompletionEvidenceRef struct {
	ID            string    `json:"id"`
	SHA256        string    `json:"sha256"`
	Source        string    `json:"source"`
	ObservedAt    time.Time `json:"observedAt"`
	ValidUntil    time.Time `json:"validUntil"`
	RevocationRef string    `json:"revocationRef"`
	Revoked       bool      `json:"revoked"`
}

// CompletionGate is an inspection record, never an authorization grant.
// Status describes the whole gate; the five dimensions preserve where work is
// complete and where qualification remains blocked.
type CompletionGate struct {
	ID                    string                  `json:"id"`
	Phase                 string                  `json:"phase"`
	Scope                 string                  `json:"scope"`
	Purpose               string                  `json:"purpose"`
	Owner                 string                  `json:"owner"`
	Dependencies          []string                `json:"dependencies"`
	RequiredForFirstPilot bool                    `json:"requiredForFirstPilot"`
	Status                string                  `json:"status"`
	Implementation        string                  `json:"implementation"`
	Fixture               string                  `json:"fixture"`
	Live                  string                  `json:"live"`
	Platform              string                  `json:"platform"`
	Activation            string                  `json:"activation"`
	Evidence              []CompletionEvidenceRef `json:"evidence"`
	Accepted              bool                    `json:"accepted"`
	Blocker               string                  `json:"blocker"`
	NextAction            string                  `json:"nextAction"`
}

type CompletionLedger struct {
	Version              string           `json:"version"`
	AsOf                 time.Time        `json:"asOf"`
	DigestSHA256         string           `json:"digestSha256"`
	InspectionOnly       bool             `json:"inspectionOnly"`
	AuthorizationGranted bool             `json:"authorizationGranted"`
	ExecutionReady       bool             `json:"executionReady"`
	Gates                []CompletionGate `json:"gates"`
}

// BuildCompletionLedger returns a deterministic, offline inventory of the
// completion plan. The only accepted status is deliberately unavailable here:
// acceptance requires a separate protected evidence verifier, which this
// read-only catalog does not install or emulate.
func BuildCompletionLedger(asOf time.Time) (CompletionLedger, error) {
	var out CompletionLedger
	if asOf.IsZero() {
		return out, errors.New("completion ledger as-of time required")
	}
	asOf = asOf.UTC().Round(0)
	out = CompletionLedger{
		Version: CompletionLedgerVersion, AsOf: asOf, InspectionOnly: true,
		Gates: completionGateCatalog(),
	}
	for i := range out.Gates {
		g := &out.Gates[i]
		if g.RequiredForFirstPilot {
			g.Status = "blocked"
		} else {
			g.Status = "pending"
		}
	}
	digest, err := completionLedgerDigest(out)
	if err != nil {
		return CompletionLedger{}, err
	}
	out.DigestSHA256 = digest
	if err := ValidateCompletionLedger(out); err != nil {
		return CompletionLedger{}, err
	}
	return out, nil
}

func completionGateCatalog() []CompletionGate {
	const route = "codex-github-issue-comment"
	makeGate := func(id, phase, scope, purpose, owner, blocker, next string, required bool, deps ...string) CompletionGate {
		implementation, fixture, live, platform, activation := "partial", "pending", "blocked", "pending", "disabled"
		if !required {
			live = "pending"
		}
		return CompletionGate{ID: id, Phase: phase, Scope: scope, Purpose: purpose, Owner: owner,
			Dependencies: deps, RequiredForFirstPilot: required, Implementation: implementation,
			Fixture: fixture, Live: live, Platform: platform, Activation: activation,
			Evidence: []CompletionEvidenceRef{}, Accepted: false, Blocker: blocker, NextAction: next}
	}
	primary := func(id, phase, owner, blocker, next string, deps ...string) CompletionGate {
		gate := makeGate(id, phase, route, "first-controlled-pilot", owner, blocker, next, true, deps...)
		gate.Fixture = "partial"
		return gate
	}
	optional := func(id, phase, scope, owner, blocker, next string, deps ...string) CompletionGate {
		return makeGate(id, phase, scope, "independent-optional-route", owner, blocker, next, false, deps...)
	}
	gates := []CompletionGate{
		primary("scope-enrollment", "P9", "operator-enrollment", "Exact installed identity, finite caps, authority roles, and stop policy are absent.", "Freeze one route and obtain protected external enrollment."),
		primary("route-applicability", "P7/P9", "readiness-integration", "Protected enrolled v2 validators and current exact-operation evidence are absent.", "Install qualified v2 acceptance, handoff, provider-operation, and conditional publication validators.", "scope-enrollment"),
		primary("protected-services", "P9", "independent-service-operators", "Policy, qualification, identity, revocation, witness, and atomic-consumption services are absent.", "Deploy independently controlled services and register them through protected embedding APIs.", "scope-enrollment"),
		primary("codex-host-qualification", "P6", "host-qualification", "Codex live controls and native conformance remain blocked.", "Qualify exact installed version, controls, bounded execution, cancellation, and reconciliation.", "protected-services", "scope-enrollment"),
		primary("p3-collector-accounting", "P3", "capacity-qualification", "Live collector conformance and attributable demand evidence are absent.", "Qualify source identity, freshness, shared reservations, settlement, and concurrent claimants.", "scope-enrollment", "codex-host-qualification"),
		primary("github-issue-comment", "P5/P8", "provider-qualification", "GitHub scoped read, send, readback, and response-loss qualification is pending.", "Qualify exact issue identity and one comment send/readback under disposable enrollment.", "protected-services", "scope-enrollment"),
		primary("independent-acceptance-handoff", "P8", "verification-qualification", "Protected verifier and native preservation/recovery evidence are pending.", "Bind exact candidate and verifier; qualify independent outcomes and interrupted local handoff.", "protected-services", "codex-host-qualification"),
		primary("connected-ownership-recovery", "P7/P9", "runtime-qualification", "Connected claim, fence, accounting, launch-intent, settlement, outbox, and recovery drills are absent.", "Exercise one causal flow with an external witness and prove replay, crash, and recovery behavior.", "p3-collector-accounting", "codex-host-qualification", "independent-acceptance-handoff", "github-issue-comment"),
		primary("calibration-coordinator", "P4/P7/P9", "calibration-enrollment", "Purpose-specific coordinator is implemented; independent enrollment, bounds, services and live conformance remain absent.", "Qualify disposable calibration through existing P7 ownership and P3 accounting; retain quality abstention.", "protected-services", "codex-host-qualification", "p3-collector-accounting", "independent-acceptance-handoff"),
		primary("p4-independent-evidence", "P4/P9", "evidence-qualification", "Required independent quality and consumption cohorts have not been collected or audited.", "Freeze cohorts and policy, collect sufficient independent families, and run the locked audit.", "calibration-coordinator"),
		primary("experiment-coordinator", "P7/P9", "experiment-enrollment", "Distinct experiment coordinator is implemented; qualified readiness, services and signed enrollment are absent.", "Qualify an evidence-only exact-route experiment retaining ordinary P4 selection and every non-quality hard gate.", "route-applicability", "protected-services", "p4-independent-evidence"),
		primary("first-controlled-trial", "P9", "pilot-coordinator", "All upstream live route dependencies and experimental enrollment are pending.", "Run one finite trial; retain full causal evidence and reconcile every attempted effect.", "experiment-coordinator", "route-applicability", "protected-services", "codex-host-qualification", "p3-collector-accounting", "github-issue-comment", "independent-acceptance-handoff", "connected-ownership-recovery", "p4-independent-evidence"),
		makeGate("shadow-comparison-promotion", "P9", route, "production-promotion", "promotion-review", "Shadow observations, campaign acceptance, and external promotion grant are absent.", "Collect at least 100 eligible shadow opportunities, assess frozen comparisons, then seek narrow external promotion.", false, "first-controlled-trial"),
		optional("claude-code", "P6/P10", "claude-code-independent-route", "host-qualification", "Claude controls and remaining-window source are not qualified.", "Qualify its exact version, controls, stop/recovery, and quota source independently.", "protected-services"),
		optional("azure-devops", "P5/P10", "azure-devops-independent-route", "provider-qualification", "Scoped REST/process identity and operation-qualified transport are pending.", "Qualify read, revision-tested update/readback, and recovery; keep system-process customization refused.", "protected-services"),
		optional("linear", "P5/P10", "linear-independent-route", "provider-qualification", "Workspace/team/issue UUID scope and GraphQL recovery qualification are pending.", "Qualify partial errors, cursors, UUID comment, idempotency, readback, and recovery.", "protected-services"),
		optional("jira-cloud", "P5/P10", "jira-cloud-independent-route", "provider-qualification", "Current API, ADF, identity, and operation-specific qualification are pending.", "Qualify issue comment/readback independently; keep transitions separate.", "protected-services"),
		optionalFixture("opencode", "P6/P10", "opencode-independent-route", "host-qualification", "Fixture driver exists; live controls and accounting are absent.", "Qualify pinned protocol, controls, cancellation, accounting, and exact route.", "protected-services"),
		optionalFixture("cursor", "P6/P10", "cursor-independent-route", "host-qualification", "Fixture driver exists; live controls and accounting are absent.", "Qualify pinned protocol, controls, cancellation, accounting, and exact route.", "protected-services"),
		optionalFixture("gemini", "P6/P10", "gemini-independent-route", "host-qualification", "Fixture driver exists; live controls and accounting are absent.", "Qualify pinned protocol, controls, cancellation, accounting, and exact route.", "protected-services"),
		optionalFixture("openclaw", "P6/P10", "openclaw-independent-route", "host-qualification", "Fixture driver exists; live controls and accounting are absent.", "Qualify pinned protocol, controls, cancellation, accounting, and exact route.", "protected-services"),
		optional("agy", "P6/P10", "agy-independent-route", "host-qualification", "Bounded machine transport is not verified.", "Verify supported bounded protocol; otherwise retain unsupported execution.", "protected-services"),
		optional("hermes", "P6/P10", "hermes-independent-route", "host-qualification", "Bounded machine transport is not verified.", "Verify supported bounded protocol; otherwise retain unsupported execution.", "protected-services"),
		optional("methodology-profiles", "P10", "methodology-independent-enforcement", "methodology-qualification", "Protected methodology role/evidence services are pending.", "Qualify catalog gates, roles, revisions, and replay/staleness checks.", "protected-services"),
		optional("jira-data-center", "P5/P10", "jira-data-center-independent-route", "provider-qualification", "Separate instance/API/auth/TLS evidence and conditional-write support are pending.", "Qualify Cloud/DC identity, capture/projection, concurrency, readback, and recovery independently.", "protected-services"),
		optional("provider-provisioning", "P5/P10", "github-azure-provisioning", "provisioning-qualification", "Qualified writers and dedicated authority/enrollment are absent.", "Qualify additive plans, drift, consumed sends, readback, and recovery without destructive scope expansion.", "protected-services"),
		optional("windows", "P10", "windows-native-delivery", "platform-qualification", "Native Windows 11 amd64/local NTFS delivery evidence is absent.", "Qualify native privileges, ACL/reparse/streams/case/long paths/locks, interrupted preservation, and restore.", "protected-services"),
		optional("codex-cloud", "P6/P10", "codex-cloud-independent-route", "host-qualification", "Approved bounded unattended remote transport is absent.", "Qualify separate remote identity, workspace, controls, quota, cancellation, and reconciliation.", "protected-services"),
		optional("campaign-optimization", "P9/P10", "campaign-optimization-independent-purpose", "campaign-qualification", "A fixed-DAG route and frozen signed outcomes are absent.", "Keep optimization advisory; qualify frozen outcomes, audit split, bounded replay, and reviewed adoption.", "shadow-comparison-promotion"),
	}
	sort.Slice(gates, func(i, j int) bool { return gates[i].ID < gates[j].ID })
	return gates
}

func optionalFixture(id, phase, scope, owner, blocker, next string, deps ...string) CompletionGate {
	g := CompletionGate{ID: id, Phase: phase, Scope: scope, Purpose: "independent-optional-route", Owner: owner,
		Dependencies: deps, Status: "pending", Implementation: "partial", Fixture: "partial", Live: "pending",
		Platform: "pending", Activation: "disabled", Evidence: []CompletionEvidenceRef{}, Accepted: false,
		Blocker: blocker, NextAction: next}
	return g
}

func completionLedgerDigest(ledger CompletionLedger) (string, error) {
	ledger.DigestSHA256 = ""
	b, err := json.Marshal(ledger)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func ValidateCompletionLedger(ledger CompletionLedger) error {
	if ledger.Version != CompletionLedgerVersion || ledger.AsOf.IsZero() || ledger.AsOf.Location() != time.UTC || !ledger.InspectionOnly || ledger.AuthorizationGranted || ledger.ExecutionReady || len(ledger.Gates) == 0 || len(ledger.Gates) > maxCompletionGates || !completionHash.MatchString(ledger.DigestSHA256) {
		return errors.New("invalid completion ledger envelope")
	}
	ids := map[string]CompletionGate{}
	for _, g := range ledger.Gates {
		if !completionID.MatchString(g.ID) || g.Phase == "" || g.Scope == "" || g.Purpose == "" || g.Owner == "" || g.Blocker == "" || g.NextAction == "" || g.Accepted || g.Status == "accepted" {
			return fmt.Errorf("invalid or self-accepted completion gate %q", g.ID)
		}
		if !oneOf(g.Status, "blocked", "pending") || !oneOf(g.Implementation, "partial", "pending", "blocked") || !oneOf(g.Fixture, "partial", "pending", "blocked") || !oneOf(g.Live, "pending", "blocked") || !oneOf(g.Platform, "pending", "blocked") || !oneOf(g.Activation, "disabled", "pending", "blocked") {
			return fmt.Errorf("invalid completion state for gate %q", g.ID)
		}
		if g.RequiredForFirstPilot && g.Status != "blocked" {
			return fmt.Errorf("required pilot gate %q cannot be non-blocking in offline ledger", g.ID)
		}
		if len(g.Evidence) > 64 || len(g.Dependencies) > maxCompletionGates {
			return fmt.Errorf("completion gate %q exceeds evidence or dependency bounds", g.ID)
		}
		if _, exists := ids[g.ID]; exists {
			return fmt.Errorf("duplicate completion gate %q", g.ID)
		}
		ids[g.ID] = g
		seenDeps := map[string]bool{}
		for _, dep := range g.Dependencies {
			if !completionID.MatchString(dep) || dep == g.ID || seenDeps[dep] {
				return fmt.Errorf("invalid dependency on completion gate %q", g.ID)
			}
			seenDeps[dep] = true
		}
		for _, ev := range g.Evidence {
			if ev.ID == "" || len(ev.ID) > 128 || !completionHash.MatchString(ev.SHA256) || ev.Source == "" || ev.ObservedAt.IsZero() || ev.ObservedAt.After(ledger.AsOf) || ev.ValidUntil.IsZero() || !ev.ValidUntil.After(ev.ObservedAt) || ev.RevocationRef == "" {
				return fmt.Errorf("invalid evidence or revocation reference on completion gate %q", g.ID)
			}
		}
	}
	for _, g := range ledger.Gates {
		for _, dep := range g.Dependencies {
			if _, ok := ids[dep]; !ok {
				return fmt.Errorf("unknown dependency %q on completion gate %q", dep, g.ID)
			}
		}
	}
	if hasCompletionCycle(ledger.Gates) {
		return errors.New("completion gate dependency cycle")
	}
	digest, err := completionLedgerDigest(ledger)
	if err != nil || digest != ledger.DigestSHA256 {
		return errors.New("completion ledger digest mismatch")
	}
	return nil
}

func MarshalCompletionLedger(ledger CompletionLedger) ([]byte, error) {
	if err := ValidateCompletionLedger(ledger); err != nil {
		return nil, err
	}
	b, err := json.MarshalIndent(ledger, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

func ReadCompletionLedger(raw []byte) (CompletionLedger, error) {
	var ledger CompletionLedger
	if len(raw) == 0 || len(raw) > maxCompletionLedgerBytes {
		return ledger, errors.New("completion ledger size out of bounds")
	}
	if err := contracts.ValidateJSON(raw); err != nil {
		return ledger, fmt.Errorf("invalid completion ledger JSON: %w", err)
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&ledger); err != nil {
		return ledger, err
	}
	if err := ValidateCompletionLedger(ledger); err != nil {
		return ledger, err
	}
	return ledger, nil
}

func hasCompletionCycle(gates []CompletionGate) bool {
	deps := make(map[string][]string, len(gates))
	for _, g := range gates {
		deps[g.ID] = g.Dependencies
	}
	state := map[string]uint8{}
	var visit func(string) bool
	visit = func(id string) bool {
		if state[id] == 1 {
			return true
		}
		if state[id] == 2 {
			return false
		}
		state[id] = 1
		for _, dep := range deps[id] {
			if visit(dep) {
				return true
			}
		}
		state[id] = 2
		return false
	}
	for id := range deps {
		if visit(id) {
			return true
		}
	}
	return false
}

func oneOf(value string, choices ...string) bool {
	for _, choice := range choices {
		if value == choice {
			return true
		}
	}
	return false
}
