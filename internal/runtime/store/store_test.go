package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/runtime/records"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/runtime/staging"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
	accounting "github.com/ArmanAvanesyan/agent-operating-kit/internal/usage/store"
)

func fixture(t *testing.T) (*Store, context.Context, time.Time) {
	t.Helper()
	ctx := context.Background()
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	root := filepath.Join(t.TempDir(), "ledger")
	s, err := Create(root, records.Policy{Version: "aok-runtime-policy/v1", Authority: "test-authority", MaxConcurrent: 1, LeaseSeconds: 30, MaxAttempts: 2}, "policy-sha")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, ctx, now
}

func submitOne(t *testing.T, s *Store, ctx context.Context, now time.Time) records.Run {
	t.Helper()
	m := records.Manifest{Version: records.Version, ID: "run-1", Steps: []records.StepSpec{{ID: "step-1", Assignment: "a", Host: "codex"}}}
	r, e := s.Submit(ctx, "submit-1", m, "manifest-sha", now)
	if e != nil {
		t.Fatal(e)
	}
	r2, e := s.Submit(ctx, "submit-1", m, "manifest-sha", now)
	if e != nil || r2.Manifest.ID != r.Manifest.ID {
		t.Fatalf("idempotent submit: %+v %v", r2, e)
	}
	return r
}

func TestClaimUpdateAndStaleFence(t *testing.T) {
	s, ctx, now := fixture(t)
	submitOne(t, s, ctx, now)
	tok, e := s.Claim(ctx, "claim-1", "run-1", "step-1", "worker", "attempt-1", now)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Check(ctx, tok, now.Add(time.Second)); e != nil {
		t.Fatal(e)
	}
	st := records.Step{ID: "step-1", Token: tok, Attempts: 1}
	out, e := s.Update(ctx, "stage-1", tok, "staged", st, now.Add(time.Second))
	if e != nil || out.Phase != "staged" {
		t.Fatalf("update: %+v %v", out, e)
	}
	if _, e = s.Update(ctx, "stage-2", records.Token{RunID: tok.RunID, StepID: tok.StepID, AttemptID: tok.AttemptID, Owner: tok.Owner, Epoch: tok.Epoch, Fence: tok.Fence - 1}, "reservation-intent", out, now.Add(2*time.Second)); !errors.Is(e, ErrBlocked) {
		t.Fatalf("stale token accepted: %v", e)
	}
	if _, e = s.Claim(ctx, "claim-1", "run-1", "step-1", "worker", "attempt-1", now); e != nil {
		t.Fatalf("exact request retry failed: %v", e)
	}
	if _, e = s.Claim(ctx, "claim-1", "run-1", "step-1", "other", "attempt-2", now); !errors.Is(e, ErrConflict) {
		t.Fatalf("changed idempotency request accepted: %v", e)
	}
}

func TestStoreClaimHelper(t *testing.T) {
	root := os.Getenv("AOK_RUNTIME_STORE_CHILD_ROOT")
	if root == "" {
		return
	}
	barrier := os.Getenv("AOK_RUNTIME_STORE_CHILD_BARRIER")
	deadline := time.Now().Add(10 * time.Second)
	for barrier != "" {
		if _, e := os.Stat(barrier); e == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("claim race barrier timed out")
		}
		time.Sleep(5 * time.Millisecond)
	}
	s, e := Open(root)
	if e != nil {
		t.Fatal(e)
	}
	if os.Getenv("AOK_RUNTIME_STORE_CHILD_CRASH") == "1" {
		tx, err := s.db.BeginTx(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec("UPDATE state SET payload='partial',digest='partial' WHERE id=1"); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec("INSERT INTO operations VALUES('half-written','x','x')"); err != nil {
			t.Fatal(err)
		}
		os.Exit(0) // Simulate process death with an uncommitted SQLite transaction.
	}
	defer s.Close()
	_, e = s.Claim(context.Background(), os.Getenv("AOK_RUNTIME_STORE_CHILD_KEY"), "run-1", os.Getenv("AOK_RUNTIME_STORE_CHILD_STEP"), "process", os.Getenv("AOK_RUNTIME_STORE_CHILD_ATTEMPT"), time.Date(2026, 10, 24, 12, 0, 1, 0, time.UTC))
	if e == nil {
		fmt.Println("CLAIMED")
		return
	}
	if errors.Is(e, ErrBlocked) {
		fmt.Println("BLOCKED")
		return
	}
	if errors.Is(e, ErrConflict) {
		fmt.Println("CONFLICT")
		return
	}
	t.Fatal(e)
}

func TestCrossProcessClaimsRespectSharedCapacity(t *testing.T) {
	s, _, now := fixture(t)
	m := records.Manifest{Version: records.Version, ID: "run-1", Steps: []records.StepSpec{{ID: "step-1"}, {ID: "step-2"}}}
	if _, e := s.Submit(context.Background(), "submit", m, "sha", now); e != nil {
		t.Fatal(e)
	}
	barrier := filepath.Join(t.TempDir(), "start-claims")
	child := func(step, attempt, key string) (*exec.Cmd, *bytes.Buffer) {
		t.Helper()
		cmd := exec.Command(os.Args[0], "-test.run=^TestStoreClaimHelper$")
		cmd.Env = append(os.Environ(), "AOK_RUNTIME_STORE_CHILD_ROOT="+s.root, "AOK_RUNTIME_STORE_CHILD_STEP="+step, "AOK_RUNTIME_STORE_CHILD_ATTEMPT="+attempt, "AOK_RUNTIME_STORE_CHILD_KEY="+key, "AOK_RUNTIME_STORE_CHILD_BARRIER="+barrier)
		buf := &bytes.Buffer{}
		cmd.Stdout = buf
		cmd.Stderr = buf
		if e := cmd.Start(); e != nil {
			t.Fatal(e)
		}
		return cmd, buf
	}
	c1, b1 := child("step-1", "attempt-1", "claim-1")
	c2, b2 := child("step-2", "attempt-2", "claim-2")
	if e := os.WriteFile(barrier, []byte("go"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := c1.Wait(); e != nil {
		t.Fatalf("child 1: %v %s", e, b1.String())
	}
	if e := c2.Wait(); e != nil {
		t.Fatalf("child 2: %v %s", e, b2.String())
	}
	if e := s.Close(); e != nil {
		t.Fatal(e)
	}
	results := b1.String() + b2.String()
	if strings.Count(results, "CLAIMED") != 1 || strings.Count(results, "BLOCKED")+strings.Count(results, "CONFLICT") != 1 {
		t.Fatalf("concurrent claims were not exclusive: %q", results)
	}
}

func TestCrashRollbackLeavesCommittedStateAndOperationsIntact(t *testing.T) {
	s, ctx, now := fixture(t)
	submitOne(t, s, ctx, now)
	root := s.root
	if e := s.Close(); e != nil {
		t.Fatal(e)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestStoreClaimHelper$")
	cmd.Env = append(os.Environ(), "AOK_RUNTIME_STORE_CHILD_ROOT="+root, "AOK_RUNTIME_STORE_CHILD_CRASH=1")
	if out, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("crash helper failed: %v %s", e, out)
	}
	reopened, e := Open(root)
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	state, e := reopened.Snapshot(ctx)
	if e != nil || len(state.Runs) != 1 || len(state.Events) != 1 {
		t.Fatalf("rollback damaged committed state: %+v %v", state, e)
	}
}

func TestRecoverBlocksPossibleLaunchAndFencesOldOwner(t *testing.T) {
	s, ctx, now := fixture(t)
	submitOne(t, s, ctx, now)
	tok, e := s.Claim(ctx, "claim", "run-1", "step-1", "worker", "attempt-1", now)
	if e != nil {
		t.Fatal(e)
	}
	st := records.Step{ID: "step-1", Token: tok, Attempts: 1}
	st, e = s.Update(ctx, "staged", tok, "staged", st, now.Add(time.Second))
	if e != nil {
		t.Fatal(e)
	}
	st, e = s.Update(ctx, "reserve-intent", tok, "reservation-intent", st, now.Add(2*time.Second))
	if e != nil {
		t.Fatal(e)
	}
	st, e = s.Update(ctx, "reserved", tok, "reserved", st, now.Add(3*time.Second))
	if e != nil {
		t.Fatal(e)
	}
	st.LaunchPossible = true
	st, e = s.Update(ctx, "launch-intent", tok, "launch-intent", st, now.Add(4*time.Second))
	if e != nil {
		t.Fatal(e)
	}
	// Claim expiry does not prove that a host never launched.
	if e = s.Recover(ctx, "recover", "run-1", now.Add(time.Minute)); e != nil {
		t.Fatal(e)
	}
	if e = s.Check(ctx, tok, now.Add(time.Minute)); !errors.Is(e, ErrBlocked) {
		t.Fatalf("recovered claim remained valid: %v", e)
	}
	d, e := s.Snapshot(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if got := d.Runs[0].Steps[0]; got.Phase != "blocked" || got.Token.Fence <= tok.Fence || got.HostReceiptSHA256 != "" || got.Reason == "" {
		t.Fatalf("unsafe recovery: %+v", got)
	}
	if _, e = s.Claim(ctx, "revive", "run-1", "step-1", "new-worker", "attempt-2", now.Add(time.Minute)); !errors.Is(e, ErrBlocked) {
		t.Fatalf("possibly launched attempt revived: %v", e)
	}
}

func TestReservationEvidenceFrozenAndCancelAllowsPreservationOnly(t *testing.T) {
	s, ctx, now := fixture(t)
	submitOne(t, s, ctx, now)
	tok, e := s.Claim(ctx, "claim", "run-1", "step-1", "worker", "attempt-1", now)
	if e != nil {
		t.Fatal(e)
	}
	st := records.Step{ID: "step-1", Token: tok, Attempts: 1, Workspace: "/private/stage"}
	st, e = s.Update(ctx, "staged", tok, "staged", st, now.Add(time.Second))
	if e != nil {
		t.Fatal(e)
	}
	st, e = s.Update(ctx, "reserve-intent", tok, "reservation-intent", st, now.Add(2*time.Second))
	if e != nil {
		t.Fatal(e)
	}
	st, e = s.Update(ctx, "reserved", tok, "reserved", st, now.Add(3*time.Second))
	if e != nil {
		t.Fatal(e)
	}
	mutated := st
	mutated.Workspace = "/other"
	if _, e = s.Update(ctx, "bad-evidence", tok, "launch-intent", mutated, now.Add(4*time.Second)); !errors.Is(e, ErrBlocked) {
		t.Fatalf("reserved evidence mutation accepted: %v", e)
	}
	if e = s.Cancel(ctx, "cancel", "run-1", "operator stop", now.Add(4*time.Second)); e != nil {
		t.Fatal(e)
	}
	if e = s.Check(ctx, tok, now.Add(5*time.Second)); !errors.Is(e, ErrBlocked) {
		t.Fatalf("cancelled run admitted work: %v", e)
	}
	if e = s.Renew(ctx, "renew", tok, now.Add(5*time.Second)); !errors.Is(e, ErrBlocked) {
		t.Fatalf("cancelled run renewed: %v", e)
	}
	st.StopObserved = true
	st.HostReceiptSHA256 = "candidate-receipt-sha"
	st, e = s.Update(ctx, "preserve", tok, "blocked", st, now.Add(5*time.Second))
	if e != nil {
		t.Fatalf("stopped candidate preservation rejected: %v", e)
	}
	if st.Phase != "blocked" || st.Accepted || st.HostReceiptSHA256 == "" {
		t.Fatalf("preservation promoted or lost candidate: %+v", st)
	}
}

func TestResultCheckpointReconcileAndResume(t *testing.T) {
	s, ctx, now := fixture(t)
	m := records.Manifest{Version: records.Version, ID: "run-1", InputRevision: "revision-1", Steps: []records.StepSpec{{ID: "step-1"}}}
	if _, e := s.Submit(ctx, "submit", m, "manifest-sha", now); e != nil {
		t.Fatal(e)
	}
	token, e := s.Claim(ctx, "claim-1", "run-1", "step-1", "worker-1", "attempt-1", now)
	if e != nil {
		t.Fatal(e)
	}
	workspace := filepath.Join(t.TempDir(), "workspace")
	if e = os.Mkdir(workspace, 0700); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(workspace, "output.txt"), []byte("candidate"), 0600); e != nil {
		t.Fatal(e)
	}
	initial, e := staging.Checkpoint(ctx, workspace, token.AttemptID, m.InputRevision, false)
	if e != nil {
		t.Fatal(e)
	}
	hold := &usage.Reservation{ID: "hold-1", AuthorityEpoch: "account-epoch", Owner: "account-owner", AttemptID: token.AttemptID, State: "in-flight", Fence: 1}
	at := &accounting.Token{ID: hold.ID, Owner: hold.Owner, Epoch: hold.AuthorityEpoch, Fence: hold.Fence}
	step := records.Step{ID: "step-1", Token: token, Attempts: 1, Workspace: initial.Workspace, Checkpoint: &initial, Reservation: hold, AccountingToken: at, DecisionSHA256: "decision-sha", PreparedSHA256: "prepared-sha"}
	step, e = s.Update(ctx, "stage", token, "staged", step, now.Add(time.Second))
	if e != nil {
		t.Fatal(e)
	}
	step, e = s.Update(ctx, "reservation-intent", token, "reservation-intent", step, now.Add(2*time.Second))
	if e != nil {
		t.Fatal(e)
	}
	step, e = s.Update(ctx, "reserved", token, "reserved", step, now.Add(3*time.Second))
	if e != nil {
		t.Fatal(e)
	}
	step.LaunchPossible = true
	step, e = s.Update(ctx, "launch-intent", token, "launch-intent", step, now.Add(4*time.Second))
	if e != nil {
		t.Fatal(e)
	}
	step, e = s.Update(ctx, "running", token, "running", step, now.Add(5*time.Second))
	if e != nil {
		t.Fatal(e)
	}
	resultCP, e := staging.Checkpoint(ctx, workspace, token.AttemptID, m.InputRevision, true)
	if e != nil {
		t.Fatal(e)
	}
	step.Checkpoint = &resultCP
	step.HostReceiptSHA256 = "host-receipt-sha"
	step.StopObserved = true
	step, e = s.Update(ctx, "result-checkpoint", token, "awaiting-verification", step, now.Add(6*time.Second))
	if e != nil {
		t.Fatalf("result checkpoint update rejected: %v", e)
	}
	var saved []byte
	if e = s.db.QueryRow("SELECT result FROM operations WHERE key='stage'").Scan(&saved); e != nil {
		t.Fatal(e)
	}
	var prior records.Step
	if e = json.Unmarshal(saved, &prior); e != nil {
		t.Fatal(e)
	}
	if prior.Checkpoint == nil || prior.Checkpoint.ID != initial.ID || prior.Checkpoint.ID == resultCP.ID {
		t.Fatal("prior checkpoint was not retained in immutable operation history")
	}
	step.Phase = "blocked"
	step.LaunchPossible = false
	step.Reservation = &usage.Reservation{ID: hold.ID, AuthorityEpoch: hold.AuthorityEpoch, Owner: "recovery-owner", AttemptID: hold.AttemptID, State: "released", Fence: 2}
	step.AccountingToken = &accounting.Token{ID: hold.ID, Owner: "recovery-owner", Epoch: hold.AuthorityEpoch, Fence: 2}
	step, e = s.Reconcile(ctx, "reconcile", token, step, now.Add(7*time.Second))
	if e != nil {
		t.Fatalf("valid exact-attempt reconciliation failed: %v", e)
	}
	if step.Phase != "blocked" || step.Accepted {
		t.Fatalf("reconciliation promoted result: %+v", step)
	}
	if e = s.Resume(ctx, "resume", "run-1", "step-1", resultCP.ID, now.Add(8*time.Second)); e != nil {
		t.Fatalf("eligible continuation rejected: %v", e)
	}
	next, e := s.Claim(ctx, "claim-2", "run-1", "step-1", "worker-2", "attempt-2", now.Add(9*time.Second))
	if e != nil {
		t.Fatal(e)
	}
	state, e := s.Snapshot(ctx)
	if e != nil {
		t.Fatal(e)
	}
	got := state.Runs[0].Steps[0]
	if next.AttemptID != "attempt-2" || got.Attempts != 2 || got.Checkpoint == nil || got.Checkpoint.ID != resultCP.ID || got.Reservation != nil || got.AccountingToken != nil || got.LaunchPossible || got.HostReceiptSHA256 != "" {
		t.Fatalf("new attempt failed to retain only continuation checkpoint: %+v", got)
	}
}

func TestReconcileUnlaunchedAdoptsExactReleasedHoldWithoutReceipt(t *testing.T) {
	s, ctx, now := fixture(t)
	m := records.Manifest{Version: records.Version, ID: "run-1", InputRevision: "revision-1", Steps: []records.StepSpec{{ID: "step-1"}}}
	if _, e := s.Submit(ctx, "submit", m, "manifest-sha", now); e != nil {
		t.Fatal(e)
	}
	token, e := s.Claim(ctx, "claim", "run-1", "step-1", "worker", "attempt-prelaunch", now)
	if e != nil {
		t.Fatal(e)
	}
	workspace := filepath.Join(t.TempDir(), "workspace")
	if e = os.Mkdir(workspace, 0700); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(workspace, "source.txt"), []byte("source"), 0600); e != nil {
		t.Fatal(e)
	}
	initial, e := staging.Checkpoint(ctx, workspace, token.AttemptID, m.InputRevision, false)
	if e != nil {
		t.Fatal(e)
	}
	step := records.Step{ID: "step-1", Token: token, Attempts: 1, Workspace: initial.Workspace, Checkpoint: &initial}
	step, e = s.Update(ctx, "staged", token, "staged", step, now.Add(time.Second))
	if e != nil {
		t.Fatal(e)
	}
	demand := usage.Demand{Version: usage.Version, ConfigurationID: "config-1", ProfileSHA256: "profile-sha", Source: "selection", ValidUntil: now.Add(10 * time.Minute).Format(time.RFC3339Nano), Amounts: []usage.Amount{{ConstraintID: "quota-1", Generation: "generation-1", Unit: "requests", Amount: json.Number("3")}}}
	step.Demand = &demand
	step.DecisionSHA256 = "decision-sha"
	step.PreparedSHA256 = "prepared-sha"
	step, e = s.Update(ctx, "reservation-intent", token, "reservation-intent", step, now.Add(2*time.Second))
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Recover(ctx, "expire", "run-1", now.Add(31*time.Second)); e != nil {
		t.Fatal(e)
	}
	state, e := s.Snapshot(ctx)
	if e != nil {
		t.Fatal(e)
	}
	step = state.Runs[0].Steps[0]
	token = step.Token
	checkpoint, e := staging.Checkpoint(ctx, workspace, token.AttemptID, m.InputRevision, true)
	if e != nil {
		t.Fatal(e)
	}
	step.Checkpoint = &checkpoint
	step.StopObserved = true
	reservation := &usage.Reservation{ID: "hold-" + token.AttemptID, Authority: "accounting", AuthorityEpoch: "account-epoch", Owner: "recovery-owner", AttemptID: token.AttemptID, ConfigurationID: demand.ConfigurationID, PolicySHA256: "policy-sha", SnapshotSHA256: "snapshot-sha", ProfileSHA256: demand.ProfileSHA256, DemandSHA256: usage.Digest(demand), DemandValidUntil: demand.ValidUntil, Fence: 2, LeaseUntil: now.Add(30 * time.Second).Format(time.RFC3339Nano), State: "released", Amounts: append([]usage.Amount{}, demand.Amounts...)}
	step.Reservation = reservation
	step.AccountingToken = &accounting.Token{ID: reservation.ID, Owner: reservation.Owner, Epoch: reservation.AuthorityEpoch, Fence: reservation.Fence}
	step, e = s.ReconcileUnlaunched(ctx, "unlaunched-recovery", token, step, now.Add(32*time.Second))
	if e != nil {
		t.Fatalf("scoped prelaunch recovery failed: %v", e)
	}
	if step.Phase != "blocked" || step.Accepted || step.LaunchPossible || !step.StopObserved || step.HostReceiptSHA256 != "" || step.Reservation.State != "released" {
		t.Fatalf("prelaunch recovery fabricated or promoted evidence: %+v", step)
	}
	if e = s.Cancel(ctx, "cancel-after-recovery", "run-1", "operator cancellation", now.Add(33*time.Second)); e != nil {
		t.Fatal(e)
	}
	if e = s.Resume(ctx, "resume-cancelled", "run-1", "step-1", checkpoint.ID, now.Add(34*time.Second)); !errors.Is(e, ErrBlocked) {
		t.Fatalf("cancelled run was revived: %v", e)
	}
}

func TestReconcileUnlaunchedRejectsPossibleLaunchAndReceipt(t *testing.T) {
	s, ctx, now := fixture(t)
	submitOne(t, s, ctx, now)
	token, e := s.Claim(ctx, "claim", "run-1", "step-1", "worker", "attempt-1", now)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Recover(ctx, "recover", "run-1", now.Add(time.Minute)); e != nil {
		t.Fatal(e)
	}
	state, e := s.Snapshot(ctx)
	if e != nil {
		t.Fatal(e)
	}
	step := state.Runs[0].Steps[0]
	token = step.Token
	step.LaunchPossible = true
	step.StopObserved = true
	if _, e = s.ReconcileUnlaunched(ctx, "unsafe-launch-possible", token, step, now.Add(time.Minute+time.Second)); !errors.Is(e, ErrBlocked) {
		t.Fatalf("possible launch accepted as unlaunched: %v", e)
	}
	step.LaunchPossible = false
	step.HostReceiptSHA256 = "invented-receipt"
	if _, e = s.ReconcileUnlaunched(ctx, "unsafe-host-receipt", token, step, now.Add(time.Minute+time.Second)); !errors.Is(e, ErrBlocked) {
		t.Fatalf("fabricated host receipt accepted: %v", e)
	}
}

func TestReconcileUnlaunchedAcceptsDurableReleaseWithoutTokenRotation(t *testing.T) {
	s, ctx, now := fixture(t)
	m := records.Manifest{Version: records.Version, ID: "run-1", InputRevision: "revision-1", Steps: []records.StepSpec{{ID: "step-1"}}}
	if _, e := s.Submit(ctx, "submit", m, "sha", now); e != nil {
		t.Fatal(e)
	}
	token, e := s.Claim(ctx, "claim", "run-1", "step-1", "worker", "attempt-1", now)
	if e != nil {
		t.Fatal(e)
	}
	workspace := filepath.Join(t.TempDir(), "workspace")
	if e = os.Mkdir(workspace, 0700); e != nil {
		t.Fatal(e)
	}
	cp, e := staging.Checkpoint(ctx, workspace, token.AttemptID, m.InputRevision, false)
	if e != nil {
		t.Fatal(e)
	}
	demand := usage.Demand{Version: usage.Version, ConfigurationID: "config", ProfileSHA256: "profile", Source: "selection", ValidUntil: now.Add(10 * time.Minute).Format(time.RFC3339Nano), Amounts: []usage.Amount{{ConstraintID: "q", Generation: "g", Unit: "requests", Amount: json.Number("1")}}}
	step := records.Step{ID: "step-1", Token: token, Attempts: 1, Workspace: cp.Workspace, Checkpoint: &cp, Demand: &demand, DecisionSHA256: "decision", PreparedSHA256: "prepared"}
	step, e = s.Update(ctx, "staged", token, "staged", step, now.Add(time.Second))
	if e != nil {
		t.Fatal(e)
	}
	step, e = s.Update(ctx, "reservation-intent", token, "reservation-intent", step, now.Add(2*time.Second))
	if e != nil {
		t.Fatal(e)
	}
	hold := &usage.Reservation{ID: "hold-" + token.AttemptID, Authority: "accounting", AuthorityEpoch: "acct-epoch", Owner: "owner", AttemptID: token.AttemptID, ConfigurationID: demand.ConfigurationID, PolicySHA256: "policy", SnapshotSHA256: "snapshot", ProfileSHA256: demand.ProfileSHA256, DemandSHA256: usage.Digest(demand), DemandValidUntil: demand.ValidUntil, Fence: 1, LeaseUntil: now.Add(30 * time.Second).Format(time.RFC3339Nano), State: "held", Amounts: demand.Amounts}
	step.Reservation = hold
	step.AccountingToken = &accounting.Token{ID: hold.ID, Owner: hold.Owner, Epoch: hold.AuthorityEpoch, Fence: hold.Fence}
	step, e = s.Update(ctx, "reserved", token, "reserved", step, now.Add(3*time.Second))
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Recover(ctx, "expire", "run-1", now.Add(31*time.Second)); e != nil {
		t.Fatal(e)
	}
	state, e := s.Snapshot(ctx)
	if e != nil {
		t.Fatal(e)
	}
	step = state.Runs[0].Steps[0]
	token = step.Token
	cp, e = staging.Checkpoint(ctx, workspace, token.AttemptID, m.InputRevision, true)
	if e != nil {
		t.Fatal(e)
	}
	step.Checkpoint = &cp
	step.StopObserved = true
	released := *hold
	released.State = "released"
	step.Reservation = &released
	step, e = s.ReconcileUnlaunched(ctx, "retry-release-reply", token, step, now.Add(32*time.Second))
	if e != nil {
		t.Fatalf("durably released unchanged P3 token rejected: %v", e)
	}
	if step.Reservation.State != "released" || step.AccountingToken.Fence != hold.Fence {
		t.Fatalf("release recovery rotated token or lost released state: %+v", step)
	}
}

func TestAuditCorruptionRejected(t *testing.T) {
	s, ctx, now := fixture(t)
	submitOne(t, s, ctx, now)
	if _, e := s.db.Exec("UPDATE audit SET previous='tampered' WHERE sequence=1"); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Snapshot(ctx); e == nil {
		t.Fatal("corrupt audit accepted")
	}
}

func TestOpenRequiresExistingPrivateRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "missing")
	if _, e := Open(root); !errors.Is(e, os.ErrNotExist) {
		t.Fatalf("Open created root: %v", e)
	}
}

func TestAcceptanceProjectionRequiresExplicitMigration(t *testing.T) {
	s, ctx, _ := fixture(t)
	var version int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 1 {
		t.Fatalf("new runtime store version = %d, err=%v", version, err)
	}
	if s.AcceptanceActive(ctx, "run-1", "step-1", 1, 1) {
		t.Fatal("acceptance appeared without migration")
	}
	if err := s.EnableAcceptanceProjection(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.SetAcceptanceAuthorityGenerations(ctx, 1, 1, "policy-a"); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 2 {
		t.Fatalf("migrated runtime store version = %d, err=%v", version, err)
	}
	reopened, err := Open(s.root)
	if err != nil {
		t.Fatal(err)
	}
	var reopenedVersion int
	if err = reopened.db.QueryRow("PRAGMA user_version").Scan(&reopenedVersion); err != nil || reopenedVersion != 2 {
		t.Fatalf("reopen changed schema version to %d: %v", reopenedVersion, err)
	}
	_ = reopened.Close()
	if err := s.SetAcceptanceAuthorityGenerations(ctx, 1, 2, "policy-a"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetAcceptanceAuthorityGenerations(ctx, 1, 1, "policy-a"); !errors.Is(err, ErrConflict) {
		t.Fatalf("generation rollback accepted: %v", err)
	}
	if err := s.SetAcceptanceAuthorityGenerations(ctx, 1, 2, "different-policy"); !errors.Is(err, ErrConflict) {
		t.Fatalf("policy change without generation advance accepted: %v", err)
	}
}

func TestAcceptanceDependencyProjectionPinsFenceAndGenerations(t *testing.T) {
	step := records.Step{ID: "prior", Phase: "blocked", StopObserved: true, Token: records.Token{RunID: "r", StepID: "prior", AttemptID: "a", Fence: 3}, Checkpoint: &records.Checkpoint{Quiescent: true, Assets: []records.Asset{{Path: "a", SHA256: "h"}}}}
	run := records.Run{Manifest: records.Manifest{ID: "r", InputRevision: "commit"}, ManifestSHA256: "input", Steps: []records.Step{step}}
	d := diskState{State: records.State{Runs: []records.Run{run}}, TrustGeneration: 4, RevocationGeneration: 7, AcceptancePolicySHA256: "policy", Acceptance: map[string]acceptanceProjection{"r/prior": {Candidate: "candidate", Snapshot: snapshotDigest(step.Checkpoint.Assets), Input: "input", Attempt: "a", Fence: 3, Trust: 4, Revocation: 7, PolicySHA: "policy", Active: true, ValidUntil: time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)}}}
	guard := AcceptanceGuard{Dependencies: []string{"prior"}, TrustGeneration: 4, RevocationGeneration: 7, PolicySHA256: "policy", AuthorityValidUntil: time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)}
	if !acceptanceDependencies(&d, "r", guard, time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)) {
		t.Fatal("valid current acceptance projection was rejected")
	}
	if acceptanceDependencies(&d, "r", guard, time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)) {
		t.Fatal("expired acceptance projection admitted dependency")
	}
	guard.RevocationGeneration++
	if acceptanceDependencies(&d, "r", guard, time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)) {
		t.Fatal("stale revocation generation admitted dependency")
	}
	guard.RevocationGeneration--
	d.State.Runs[0].Steps[0].Token.Fence++
	if acceptanceDependencies(&d, "r", guard, time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)) {
		t.Fatal("stale attempt fence admitted dependency")
	}
}
