package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/runtime/records"
	ledger "github.com/ArmanAvanesyan/agent-operating-kit/internal/runtime/store"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
	accounting "github.com/ArmanAvanesyan/agent-operating-kit/internal/usage/store"
)

var replayID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

// Replay applies a strict synthetic event scenario to fresh private SQLite
// stores. It never launches a host and returns only deterministic JSON artifacts.
func Replay(path string, asOf time.Time) (map[string][]byte, records.Report, error) {
	report := NewReport()
	report.Status = "replay-failed"
	report.RuntimeGate = "synthetic-replay-only"
	if asOf.IsZero() {
		return nil, report, errors.New("replay as-of time is required")
	}
	raw, err := ReadFile(path)
	if err != nil {
		return nil, report, err
	}
	if err = records.ValidateJSON(raw, "replay"); err != nil {
		return nil, report, fmt.Errorf("scenario contract: %w", err)
	}
	var scenario records.Scenario
	if err = usage.Decode(raw, &scenario); err != nil {
		return nil, report, err
	}
	if scenario.Version != records.Version || len(scenario.Events) == 0 || len(scenario.Manifest.Steps) == 0 {
		return nil, report, errors.New("scenario version, events, and steps are required")
	}
	// Replays are permanently synthetic: reject production dispatch policy even
	// though no route or host implementation is loaded by this package.
	if scenario.Policy.AllowDispatch {
		return nil, report, errors.New("scenario policy cannot enable dispatch")
	}
	if !replayID.MatchString(scenario.Policy.Authority) || !replayID.MatchString(scenario.Manifest.ID) {
		return nil, report, errors.New("scenario authority and run identity must be safe identifiers")
	}
	if err = validateScenario(scenario); err != nil {
		return nil, report, err
	}
	work, err := os.MkdirTemp("", "aok-runtime-replay-")
	if err != nil {
		return nil, report, err
	}
	defer os.RemoveAll(work)
	work, err = filepath.EvalSymlinks(work)
	if err != nil {
		return nil, report, err
	}
	if err = os.Chmod(work, 0700); err != nil {
		return nil, report, err
	}
	policy := scenario.Policy
	policy.AllowDispatch = false
	policy.StagingRoot = filepath.Join(work, "staging")
	policy.AccountingPath = filepath.Join(work, "accounting.sqlite")
	policy.RepositoryRoots = []string{filepath.Join(work, "synthetic-repositories")}
	policy.Hosts = nil
	if err = os.MkdirAll(policy.RepositoryRoots[0], 0700); err != nil {
		return nil, report, err
	}
	if err = ValidatePolicy(policy); err != nil {
		return nil, report, fmt.Errorf("synthetic replay policy: %w", err)
	}
	policySHA := usage.Digest(scenario.Policy)
	stateStore, err := ledger.Create(filepath.Join(work, "runtime"), policy, policySHA)
	if err != nil {
		return nil, report, err
	}
	defer stateStore.Close()
	accountStore, err := accounting.Open(policy.AccountingPath, policy.Authority)
	if err != nil {
		return nil, report, err
	}
	defer accountStore.Close()
	ctx := context.Background()
	firstAt, err := time.Parse(time.RFC3339Nano, scenario.Events[0].At)
	if err != nil {
		return nil, report, fmt.Errorf("event 1 timestamp: %w", err)
	}
	manifestSHA := usage.Digest(scenario.Manifest)
	if _, err = stateStore.Submit(ctx, "replay-submit-"+scenario.Manifest.ID, scenario.Manifest, manifestSHA, firstAt.Add(-time.Nanosecond)); err != nil {
		return nil, report, fmt.Errorf("submit synthetic manifest: %w", err)
	}
	steps := map[string]records.Step{}
	tokens := map[string]records.Token{}
	for _, spec := range scenario.Manifest.Steps {
		steps[spec.ID] = records.Step{ID: spec.ID, Phase: "pending"}
	}
	var usagePolicy *usage.Policy
	trace := make([]replayTraceEvent, 0, len(scenario.Events))
	var previous time.Time
	for i, event := range scenario.Events {
		at, e := time.Parse(time.RFC3339Nano, event.At)
		if e != nil {
			return nil, report, fmt.Errorf("event %d timestamp: %w", i+1, e)
		}
		if !previous.IsZero() && at.Before(previous) {
			return nil, report, fmt.Errorf("event %d timestamp moves backwards", i+1)
		}
		if at.After(asOf.UTC()) {
			return nil, report, fmt.Errorf("event %d is after replay as-of time", i+1)
		}
		previous = at
		if event.Payload == nil || len(event.Payload) == 0 || string(event.Payload) == "null" {
			return nil, report, fmt.Errorf("event %d requires a payload", i+1)
		}
		key := fmt.Sprintf("replay-%06d-%s", i+1, event.Operation)
		trace = append(trace, replayTraceEvent{Sequence: i + 1, Operation: event.Operation, StepID: event.StepID, At: at.UTC().Format(time.RFC3339Nano), PayloadSHA256: usage.Hash(event.Payload)})
		switch event.Operation {
		case "ingest":
			var p ingestPayload
			if e = strictPayload(event.Payload, &p); e != nil {
				return nil, report, fmt.Errorf("event %d ingest payload: %w", i+1, e)
			}
			export, up, e := ingestFixture(p)
			if e != nil {
				return nil, report, fmt.Errorf("event %d fixture ingest: %w", i+1, e)
			}
			if scenario.Policy.UsagePolicy.SHA256 != usage.PolicyHash(up) {
				return nil, report, fmt.Errorf("event %d usage policy does not match scenario policy binding", i+1)
			}
			if _, e = accountStore.Ingest(ctx, key, export, up, at); e != nil {
				return nil, report, fmt.Errorf("event %d accounting ingest: %w", i+1, e)
			}
			usagePolicy = &up
		case "claim":
			step, ok := steps[event.StepID]
			if !ok || step.Phase != "pending" {
				return nil, report, fmt.Errorf("event %d claims unknown or non-pending step", i+1)
			}
			var p claimPayload
			if e = strictPayload(event.Payload, &p); e != nil || !replayID.MatchString(p.Owner) || !replayID.MatchString(p.AttemptID) {
				return nil, report, fmt.Errorf("event %d has invalid claim payload", i+1)
			}
			token, claimErr := stateStore.Claim(ctx, key, scenario.Manifest.ID, event.StepID, p.Owner, p.AttemptID, at)
			if claimErr != nil {
				return nil, report, fmt.Errorf("event %d claim: %w", i+1, claimErr)
			}
			step.Token, step.Attempts, step.Phase = token, 1, "claimed"
			steps[event.StepID], tokens[event.StepID] = step, token
		case "checkpoint":
			step, token, e := claimedStep(steps, tokens, event.StepID, i+1)
			if e != nil {
				return nil, report, e
			}
			var p checkpointPayload
			if e = strictPayload(event.Payload, &p); e != nil || p.Workspace == "" || p.Checkpoint.Version != records.Version || p.Checkpoint.ID == "" || p.Checkpoint.SnapshotRoot == "" || !p.Checkpoint.Quiescent || len(p.Checkpoint.Digest) != 64 || !replayHex(p.Checkpoint.Digest) || p.Checkpoint.AttemptID != token.AttemptID || p.Checkpoint.InputRevision != scenario.Manifest.InputRevision || p.Checkpoint.Workspace != p.Workspace {
				return nil, report, fmt.Errorf("event %d has invalid synthetic checkpoint", i+1)
			}
			step.Workspace, step.Checkpoint = p.Workspace, &p.Checkpoint
			step, e = stateStore.Update(ctx, key, token, "staged", step, at)
			if e != nil {
				return nil, report, fmt.Errorf("event %d checkpoint: %w", i+1, e)
			}
			steps[event.StepID] = step
		case "reserve":
			step, token, e := claimedStep(steps, tokens, event.StepID, i+1)
			if e != nil {
				return nil, report, e
			}
			if usagePolicy == nil {
				return nil, report, fmt.Errorf("event %d reserve precedes fixture ingest", i+1)
			}
			var p reservePayload
			if e = strictPayload(event.Payload, &p); e != nil || !replayID.MatchString(p.ReservationID) || p.Demand.ConfigurationID == "" {
				return nil, report, fmt.Errorf("event %d has invalid reservation payload", i+1)
			}
			leaseSeconds := p.LeaseSeconds
			if leaseSeconds == 0 {
				leaseSeconds = policy.LeaseSeconds
			}
			if leaseSeconds < 1 || leaseSeconds > policy.LeaseSeconds {
				return nil, report, fmt.Errorf("event %d lease exceeds synthetic policy", i+1)
			}
			step, e = stateStore.Update(ctx, key+"-intent", token, "reservation-intent", step, at)
			if e != nil {
				return nil, report, fmt.Errorf("event %d reservation intent: %w", i+1, e)
			}
			accountState, e := accountStore.ReadSnapshot(ctx)
			if e != nil {
				return nil, report, e
			}
			until := at.Add(time.Duration(leaseSeconds) * time.Second).UTC().Format(time.RFC3339Nano)
			hold, e := accountStore.Reserve(ctx, accounting.ReserveRequest{Key: key, ID: p.ReservationID, Owner: token.Owner, AttemptID: token.AttemptID, ExpectedRevision: accountState.Export.LedgerRevision, LeaseUntil: until, Demand: p.Demand}, *usagePolicy, at)
			if e != nil {
				return nil, report, fmt.Errorf("event %d reserve: %w", i+1, e)
			}
			step.Demand = &p.Demand
			step.Reservation = hold.Reservation
			accountToken := accounting.Token{ID: hold.Reservation.ID, Owner: hold.Reservation.Owner, Epoch: hold.Reservation.AuthorityEpoch, Fence: hold.Reservation.Fence}
			step.AccountingToken = &accountToken
			step, e = stateStore.Update(ctx, key, token, "reserved", step, at)
			if e != nil {
				return nil, report, fmt.Errorf("event %d runtime reserve: %w", i+1, e)
			}
			steps[event.StepID] = step
		case "confirm-launch":
			step, token, e := claimedStep(steps, tokens, event.StepID, i+1)
			if e != nil {
				return nil, report, e
			}
			if usagePolicy == nil || step.AccountingToken == nil || step.Demand == nil {
				return nil, report, fmt.Errorf("event %d launch lacks a reservation", i+1)
			}
			var p emptyPayload
			if e = strictPayload(event.Payload, &p); e != nil {
				return nil, report, fmt.Errorf("event %d launch payload: %w", i+1, e)
			}
			if _, e = accountStore.ConfirmLaunch(ctx, accounting.Transition{Key: key, Token: *step.AccountingToken, ProfileSHA256: step.Demand.ProfileSHA256}, *usagePolicy, at); e != nil {
				return nil, report, fmt.Errorf("event %d confirm launch: %w", i+1, e)
			}
			step, e = stateStore.Update(ctx, key, token, "launch-intent", step, at)
			if e != nil {
				return nil, report, fmt.Errorf("event %d launch intent: %w", i+1, e)
			}
			steps[event.StepID] = step
		case "running", "awaiting-verification":
			step, token, e := claimedStep(steps, tokens, event.StepID, i+1)
			if e != nil {
				return nil, report, e
			}
			var p emptyPayload
			if e = strictPayload(event.Payload, &p); e != nil {
				return nil, report, fmt.Errorf("event %d %s payload: %w", i+1, event.Operation, e)
			}
			phase := event.Operation
			step, e = stateStore.Update(ctx, key, token, phase, step, at)
			if e != nil {
				return nil, report, fmt.Errorf("event %d %s: %w", i+1, phase, e)
			}
			steps[event.StepID] = step
		case "cancel":
			var p cancelPayload
			if e = strictPayload(event.Payload, &p); e != nil || strings.TrimSpace(p.Reason) == "" {
				return nil, report, fmt.Errorf("event %d has invalid cancellation payload", i+1)
			}
			if e = stateStore.Cancel(ctx, key, scenario.Manifest.ID, p.Reason, at); e != nil {
				return nil, report, fmt.Errorf("event %d cancel: %w", i+1, e)
			}
		case "recover":
			if usagePolicy == nil {
				return nil, report, fmt.Errorf("event %d recovery precedes fixture ingest", i+1)
			}
			var p recoverPayload
			if e = strictPayload(event.Payload, &p); e != nil || !replayID.MatchString(p.Owner) {
				return nil, report, fmt.Errorf("event %d has invalid recovery payload", i+1)
			}
			if _, e = accountStore.Recover(ctx, key, p.Owner, *usagePolicy, at); e != nil {
				return nil, report, fmt.Errorf("event %d accounting recovery: %w", i+1, e)
			}
			if e = stateStore.Recover(ctx, key+"-runtime", scenario.Manifest.ID, at); e != nil {
				return nil, report, fmt.Errorf("event %d runtime recovery: %w", i+1, e)
			}
		default:
			return nil, report, fmt.Errorf("event %d has unsupported operation %q", i+1, event.Operation)
		}
	}
	state, err := stateStore.Snapshot(ctx)
	if err != nil {
		return nil, report, err
	}
	normalizeReplayState(&state)
	report.Status = "replayed"
	report.Reasons = append(report.Reasons, "synthetic offline replay only; no host was launched", "P8 acceptance remains false")
	report.Accepted = false
	report.State = &state
	traceBytes, err := json.MarshalIndent(trace, "", "  ")
	if err != nil {
		return nil, report, err
	}
	traceBytes = append(traceBytes, '\n')
	files := map[string][]byte{"report.json": Encode(report), "eventtrace.json": traceBytes}
	return files, report, nil
}

type ingestPayload struct {
	Export json.RawMessage `json:"export"`
	Policy json.RawMessage `json:"policy"`
}
type claimPayload struct {
	Owner     string `json:"owner"`
	AttemptID string `json:"attemptId"`
}
type checkpointPayload struct {
	Workspace  string             `json:"workspace"`
	Checkpoint records.Checkpoint `json:"checkpoint"`
}
type reservePayload struct {
	ReservationID string       `json:"reservationId"`
	LeaseSeconds  int          `json:"leaseSeconds,omitempty"`
	Demand        usage.Demand `json:"demand"`
}
type emptyPayload struct{}
type cancelPayload struct {
	Reason string `json:"reason"`
}
type recoverPayload struct {
	Owner string `json:"owner"`
}
type replayTraceEvent struct {
	Sequence      int    `json:"sequence"`
	Operation     string `json:"operation"`
	StepID        string `json:"stepId"`
	At            string `json:"at"`
	PayloadSHA256 string `json:"payloadSha256"`
}

func validateScenario(s records.Scenario) error {
	if len(s.Policy.RepositoryRoots) == 0 || s.Policy.MaxConcurrent < 1 || s.Policy.LeaseSeconds < 5 || s.Policy.LeaseSeconds > 30 || s.Policy.HeartbeatSeconds < 1 || s.Policy.HeartbeatSeconds*2 >= s.Policy.LeaseSeconds || s.Policy.MaxAttempts < 1 {
		return errors.New("scenario policy violates bounded runtime limits")
	}
	seen := map[string]bool{}
	for _, step := range s.Manifest.Steps {
		if !replayID.MatchString(step.ID) || seen[step.ID] {
			return errors.New("scenario step IDs must be unique safe identifiers")
		}
		seen[step.ID] = true
	}
	for i, event := range s.Events {
		if event.Operation == "" {
			return fmt.Errorf("event %d operation is required", i+1)
		}
		if event.StepID != "" && !seen[event.StepID] {
			return fmt.Errorf("event %d references unknown step", i+1)
		}
		if _, err := time.Parse(time.RFC3339Nano, event.At); err != nil {
			return fmt.Errorf("event %d has invalid timestamp", i+1)
		}
	}
	return nil
}

func strictPayload(raw json.RawMessage, target any) error { return usage.Decode(raw, target) }
func replayHex(s string) bool {
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

func ingestFixture(payload ingestPayload) (usage.Export, usage.Policy, error) {
	var export usage.Export
	var policy usage.Policy
	if len(payload.Export) == 0 || len(payload.Policy) == 0 {
		return export, policy, errors.New("export and policy payloads are required")
	}
	if err := usage.ValidateJSON(payload.Export, "export.json"); err != nil {
		return export, policy, err
	}
	if err := usage.ValidateJSON(payload.Policy, "policy.json"); err != nil {
		return export, policy, err
	}
	if err := usage.Decode(payload.Export, &export); err != nil {
		return export, policy, err
	}
	if err := usage.Decode(payload.Policy, &policy); err != nil {
		return export, policy, err
	}
	if err := usage.ValidatePolicy(policy); err != nil {
		return export, policy, err
	}
	// Fixture events carry structured JSON; bind the supplied export to the
	// canonical policy represented by that exact structured payload.
	canonicalPolicySHA := usage.PolicyHash(policy)
	if export.PolicySHA256 != canonicalPolicySHA {
		return usage.Export{}, usage.Policy{}, fmt.Errorf("fixture export policy digest mismatch: got %s want canonical %s", export.PolicySHA256, canonicalPolicySHA)
	}
	if err := usage.ValidateExport(export, policy); err != nil {
		return usage.Export{}, usage.Policy{}, err
	}
	return export, policy, nil
}

func claimedStep(steps map[string]records.Step, tokens map[string]records.Token, id string, event int) (records.Step, records.Token, error) {
	step, ok := steps[id]
	token, tokenOK := tokens[id]
	if !ok || !tokenOK || (step.Phase != "claimed" && step.Phase != "staged" && step.Phase != "reserved" && step.Phase != "launch-intent" && step.Phase != "running") {
		return records.Step{}, records.Token{}, fmt.Errorf("event %d requires a claimed active step", event)
	}
	return step, token, nil
}

func normalizeReplayState(s *records.State) {
	s.Epoch = "synthetic-runtime-epoch"
	s.Events = []records.Event{}
	for ri := range s.Runs {
		for si := range s.Runs[ri].Steps {
			step := &s.Runs[ri].Steps[si]
			if step.Token.Epoch != "" {
				step.Token.Epoch = s.Epoch
			}
			if step.AccountingToken != nil {
				step.AccountingToken.Epoch = "synthetic-accounting-epoch"
			}
			if step.Reservation != nil {
				step.Reservation.AuthorityEpoch = "synthetic-accounting-epoch"
			}
		}
	}
}
