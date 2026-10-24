package runtime

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/hostbridge"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/runtime/records"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/runtime/staging"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
	accounting "github.com/ArmanAvanesyan/agent-operating-kit/internal/usage/store"
	"path/filepath"
	"time"
)

// Reconcile requires an independently trusted, current, exact-attempt recovery
// observation. A task-authored stop boolean or arbitrary actuals are not proof.
func (c *Controller) Reconcile(ctx context.Context, id, path string) (records.Report, error) {
	report := NewReport()
	raw, err := ReadFile(path)
	if err != nil {
		return report, err
	}
	var ev records.RecoveryEvidence
	if err = Decode(raw, "recovery", &ev); err != nil {
		return report, err
	}
	state, err := c.Store.Snapshot(ctx)
	if err != nil {
		return report, err
	}
	run, err := findRun(state, id)
	if err != nil {
		return report, err
	}
	step, err := findStep(run, ev.StepID)
	if err != nil {
		return report, err
	}
	if err = verifyRecoveryEvidence(c.Policy, run, step, ev, c.now()); err != nil {
		return report, err
	}
	if step.Demand == nil {
		return report, fmt.Errorf("attempt demand is unavailable for recovery")
	}
	prelaunch := step.HostReceiptSHA256 == "" && ev.Receipt.Path == "" && ev.Receipt.SHA256 == "" && ev.Transcript.Path == "" && ev.Transcript.SHA256 == ""
	if !prelaunch {
		rb, e := readAsset(filepath.Dir(path), ev.Receipt)
		if e != nil {
			return report, e
		}
		tb, e := readAsset(filepath.Dir(path), ev.Transcript)
		if e != nil {
			return report, e
		}
		var receipt hostbridge.Receipt
		if e = hostbridge.Decode(rb, "receipt", &receipt); e != nil {
			return report, e
		}
		var tr hostbridge.Transcript
		if e = hostbridge.Decode(tb, "transcript", &tr); e != nil {
			return report, e
		}
		if receipt.AttemptID != ev.AttemptID || receipt.TranscriptSHA256 != usage.Hash(tb) || receipt.ConfigurationID != step.Demand.ConfigurationID || receipt.PreparedSHA256 != step.PreparedSHA256 || receipt.SessionID == "" {
			return report, fmt.Errorf("recovery observation does not bind the original host attempt")
		}
	}
	if err = c.Store.Recover(ctx, newID("recover-"), id, c.now()); err != nil {
		return report, err
	}
	state, err = c.Store.Snapshot(ctx)
	if err != nil {
		return report, err
	}
	run, err = findRun(state, id)
	if err != nil {
		return report, err
	}
	step, err = findStep(run, ev.StepID)
	if err != nil {
		return report, err
	}
	account, err := accounting.Open(c.Policy.AccountingPath, c.Policy.Authority)
	if err != nil {
		return report, err
	}
	defer account.Close()
	up, _, err := usage.ReadPolicy(c.Policy.UsagePolicy.Path)
	if err != nil {
		return report, err
	}
	if _, err = readAsset("", c.Policy.UsagePolicy); err != nil {
		return report, err
	}
	as, err := account.ReadSnapshot(ctx)
	if err != nil {
		return report, err
	}
	wantedID := "hold-" + ev.AttemptID
	if step.AccountingToken != nil {
		wantedID = step.AccountingToken.ID
	}
	r, ok := findAttemptReservation(as.Export.Reservations, wantedID, ev.AttemptID, c.Policy.Authority, *step.Demand, up)
	if !ok || (r.Owner != step.Token.Owner && r.Owner != c.Policy.Authority) {
		return report, fmt.Errorf("matching durable reservation absent; no unrelated hold can authorize recovery")
	}
	hold := &r
	until, _ := time.Parse(time.RFC3339Nano, hold.LeaseUntil)
	if !c.now().Before(until) && (hold.State == "held" || hold.State == "in-flight" || hold.State == "uncertain") {
		oldToken := reservationTokenBeforeRecovery(hold, step)
		result, err := account.RecoverReservation(ctx, accountingRecoveryKey(oldToken), oldToken, c.Policy.Authority, up, c.now())
		if err != nil {
			return report, err
		}
		hold = result.Reservation
	}
	if hold == nil {
		return report, fmt.Errorf("reservation recovery returned no durable hold")
	}
	atoken := accounting.Token{ID: hold.ID, Owner: hold.Owner, Epoch: hold.AuthorityEpoch, Fence: hold.Fence}
	if hold.State == "held" { // Durable held state proves ConfirmLaunch never committed.
		result, err := account.ReleaseUnlaunched(ctx, accounting.Transition{Key: unlaunchedReleaseKey(atoken), Token: atoken}, up, c.now())
		if err != nil {
			return report, err
		}
		hold = result.Reservation
	} else if hold.State == "in-flight" || hold.State == "uncertain" {
		if prelaunch {
			return blockedReport(report, "P3 confirms launch may have occurred; usage reconciliation required"), nil
		}
		if len(ev.Actual) == 0 {
			return blockedReport(report, "known stop does not resolve missing usage; uncertain hold retained"), nil
		}
		result, err := account.Reconcile(ctx, accounting.Transition{Key: settlementKey(atoken, ev.Actual), Token: atoken, Actual: ev.Actual}, up, c.now())
		if err != nil {
			return report, err
		}
		hold = result.Reservation
	}
	step.AccountingToken = &atoken
	step.Reservation = hold
	step.StopObserved = true
	if prelaunch {
		if hold.State != "released" || len(ev.Actual) != 0 {
			return blockedReport(report, "unlaunched recovery requires a durable released hold and no usage actuals"), nil
		}
		step.AccountingToken = &atoken
		step.Reservation = hold
		step.StopObserved = true
		step.LaunchPossible = false
		cp, e := staging.CheckpointFor(ctx, step.Workspace, step.Token.AttemptID, run.Manifest.InputRevision, run.Manifest.Repository, true)
		if e != nil {
			return report, e
		}
		step.Checkpoint = &cp
		step.Reason = "signed recovery evidence and durable released hold confirm no host launch; explicit resume required"
		if _, e = c.Store.ReconcileUnlaunched(ctx, newID("unlaunched-"), step.Token, step, c.now()); e != nil {
			return report, e
		}
		return c.Status(ctx, id)
	}
	cp, err := staging.CheckpointFor(ctx, step.Workspace, step.Token.AttemptID, run.Manifest.InputRevision, run.Manifest.Repository, true)
	if err != nil {
		return report, err
	}
	step.Checkpoint = &cp
	step.Reason = "independent recovery observation confirmed stop and accounting disposition; explicit resume required"
	step.LaunchPossible = false
	if _, err = c.Store.Reconcile(ctx, newID("reconciled-"), step.Token, step, c.now()); err != nil {
		return report, err
	}
	return c.Status(ctx, id)
}

// reservationMatchesAttempt prevents recovery from adopting a reservation that
// merely happens to share a model or profile with this attempt.
func reservationMatchesAttempt(r usage.Reservation, attemptID, authority string, demand usage.Demand, p usage.Policy) bool {
	return r.ID == "hold-"+attemptID && r.AttemptID == attemptID && r.Authority == authority &&
		r.ConfigurationID == demand.ConfigurationID && r.ProfileSHA256 == demand.ProfileSHA256 &&
		r.DemandSHA256 == usage.Digest(demand) && r.PolicySHA256 == usage.PolicyHash(p) &&
		usage.Digest(r.Amounts) == usage.Digest(demand.Amounts)
}

func reservationTokenBeforeRecovery(r *usage.Reservation, step records.Step) accounting.Token {
	if step.AccountingToken != nil {
		return *step.AccountingToken
	}
	// Reserve allocates fence 1. If its reply was lost, this reconstructs the
	// exact token used by the stable RecoverReservation operation on a retry.
	return accounting.Token{ID: r.ID, Owner: step.Token.Owner, Epoch: r.AuthorityEpoch, Fence: 1}
}

func findAttemptReservation(reservations []usage.Reservation, id, attemptID, authority string, demand usage.Demand, p usage.Policy) (usage.Reservation, bool) {
	for _, r := range reservations {
		if r.ID == id {
			if reservationMatchesAttempt(r, attemptID, authority, demand, p) {
				return r, true
			}
			return usage.Reservation{}, false
		}
	}
	return usage.Reservation{}, false
}

func accountingRecoveryKey(t accounting.Token) string {
	return "account-recover-" + usage.Digest(t)[:40]
}
func unlaunchedReleaseKey(t accounting.Token) string {
	return "unlaunched-release-" + usage.Digest(t)[:40]
}
func settlementKey(t accounting.Token, actual []usage.Amount) string {
	return "settlement-" + usage.Digest(struct {
		Token  accounting.Token
		Actual []usage.Amount
	}{t, actual})[:40]
}
func verifyRecoveryEvidence(policy records.Policy, run records.Run, step records.Step, ev records.RecoveryEvidence, now time.Time) error {
	if ev.Version != Version || ev.RunID != run.Manifest.ID || ev.ManifestSHA256 != run.ManifestSHA256 || ev.StepID != step.ID || ev.AttemptID != step.Token.AttemptID || !ev.StopObserved {
		return fmt.Errorf("recovery evidence scope/stop unknown")
	}
	observed, err := time.Parse(time.RFC3339Nano, ev.ObservedAt)
	if err != nil || observed.After(now) || now.Sub(observed) > 5*time.Minute {
		return fmt.Errorf("recovery evidence stale or future")
	}
	key, err := hex.DecodeString(policy.ReconciliationIssuers[ev.Issuer])
	if err != nil || len(key) != ed25519.PublicKeySize {
		return fmt.Errorf("recovery issuer not trusted by operator policy")
	}
	signature, err := hex.DecodeString(ev.Signature)
	if err != nil {
		return err
	}
	ev.Signature = ""
	payload, _ := json.Marshal(ev)
	if !ed25519.Verify(key, payload, signature) {
		return fmt.Errorf("recovery signature invalid")
	}
	return nil
}
func (c *Controller) Resume(ctx context.Context, id, checkpoint string) (records.Report, error) {
	report := NewReport()
	state, err := c.Store.Snapshot(ctx)
	if err != nil {
		return report, err
	}
	run, err := findRun(state, id)
	if err != nil {
		return report, err
	}
	if run.Cancelled {
		return blockedReport(report, "cancelled runs require a separately authorized new run"), nil
	}
	for _, step := range run.Steps {
		if step.Checkpoint == nil || step.Checkpoint.ID != checkpoint {
			continue
		}
		if !step.StopObserved || !step.Checkpoint.Quiescent || step.LaunchPossible || step.Reservation != nil && (step.Reservation.State != "settled" && step.Reservation.State != "released") {
			return blockedReport(report, "continuation blocked by unresolved process or usage"), nil
		}
		if err = staging.Verify(*step.Checkpoint); err != nil {
			return report, err
		}
		// Existing P6 exact-policy session resume is never weakened here. Continued
		// work uses a fresh prepared attempt/workspace with checkpoint provenance.
		if err = c.Store.Resume(ctx, newID("resume-"), id, step.ID, checkpoint, c.now()); err != nil {
			return report, err
		}
		return c.Dispatch(ctx, id)
	}
	return report, fmt.Errorf("checkpoint not found")
}
