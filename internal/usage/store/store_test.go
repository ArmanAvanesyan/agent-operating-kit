package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage/collectors"
)

var testAt = time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)

func fixture(t *testing.T) (usage.Export, usage.Policy, usage.Demand) {
	t.Helper()
	e, p, err := usage.Load("../testdata/export.json", "../testdata/policy.json")
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile("../testdata/demand.json")
	if err != nil {
		t.Fatal(err)
	}
	var d usage.Demand
	if err = usage.Decode(b, &d); err != nil {
		t.Fatal(err)
	}
	return e, p, d
}
func newStore(t *testing.T) (*Store, usage.Policy, usage.Demand) {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	s, err := Open(filepath.Join(dir, "ledger.sqlite"), "test-runtime")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	e, p, d := fixture(t)
	remaining := json.Number("10")
	used := json.Number("90")
	e.Quotas[0].Remaining = &remaining
	e.Quotas[0].Used = &used
	if _, err = s.Ingest(context.Background(), "initial", e, p, testAt); err != nil {
		t.Fatal(err)
	}
	return s, p, d
}
func reserve(t *testing.T, s *Store, p usage.Policy, d usage.Demand, id string) usage.Reservation {
	t.Helper()
	state, err := s.ReadSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.Reserve(context.Background(), ReserveRequest{Key: id, ID: id, Owner: "owner", AttemptID: "attempt", ExpectedRevision: state.Export.LedgerRevision, LeaseUntil: testAt.Add(30 * time.Second).Format(time.RFC3339), Demand: d}, p, testAt)
	if err != nil {
		t.Fatal(err)
	}
	return *r.Reservation
}
func token(r usage.Reservation) Token {
	return Token{ID: r.ID, Owner: r.Owner, Epoch: r.AuthorityEpoch, Fence: r.Fence}
}

func TestAtomicLifecycleAndIdempotency(t *testing.T) {
	s, p, d := newStore(t)
	ctx := context.Background()
	state, _ := s.ReadSnapshot(ctx)
	req := ReserveRequest{Key: "reserve", ID: "first", Owner: "owner", AttemptID: "attempt", ExpectedRevision: state.Export.LedgerRevision, LeaseUntil: testAt.Add(30 * time.Second).Format(time.RFC3339), Demand: d}
	first, err := s.Reserve(ctx, req, p, testAt)
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.Reserve(ctx, req, p, testAt.Add(time.Second))
	if err != nil || usage.Digest(first) != usage.Digest(again) {
		t.Fatalf("idempotency: %v", err)
	}
	changed := req
	changed.Owner = "different"
	if _, err = s.Reserve(ctx, changed, p, testAt); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed request accepted: %v", err)
	}
	_ = reserve(t, s, p, d, "second") // A new reservation cannot invalidate the first token.
	tr := Transition{Key: "launch", Token: token(*first.Reservation), ProfileSHA256: d.ProfileSHA256}
	wrong := tr
	wrong.Key = "wrong-profile"
	wrong.ProfileSHA256 = strings.Repeat("f", 64)
	if _, err = s.ConfirmLaunch(ctx, wrong, p, testAt.Add(time.Second)); err == nil {
		t.Fatal("launched with another profile")
	}
	if _, err = s.ConfirmLaunch(ctx, tr, p, testAt.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ReleaseUnlaunched(ctx, Transition{Key: "bad-release", Token: tr.Token}, p, testAt.Add(2*time.Second)); err == nil {
		t.Fatal("released an in-flight reservation")
	}
	if _, err = s.Recover(ctx, "recovery", "recovery-owner", p, testAt.Add(31*time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Settle(ctx, Transition{Key: "stale-settlement", Token: tr.Token, Actual: d.Amounts}, p, testAt.Add(31*time.Second)); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale fence accepted: %v", err)
	}
	state, err = s.ReadSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var recovered usage.Reservation
	for _, r := range state.Export.Reservations {
		if r.ID == "first" {
			recovered = r
		}
		if r.ID == "second" && r.State != "released" {
			t.Fatal("unlaunched hold not released")
		}
	}
	if recovered.State != "uncertain" {
		t.Fatal("running lease did not remain uncertain")
	}
	actual := append([]usage.Amount{}, d.Amounts...)
	actual[0].Amount = json.Number("2")
	result, err := s.Reconcile(ctx, Transition{Key: "reconcile", Token: token(recovered), Actual: actual}, p, testAt.Add(32*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if result.Reservation.State != "settled" {
		t.Fatal(result)
	}
	again, err = s.Reconcile(ctx, Transition{Key: "reconcile", Token: token(recovered), Actual: actual}, p, testAt.Add(33*time.Second))
	if err != nil || usage.Digest(again) != usage.Digest(result) {
		t.Fatalf("reconciliation replay: %v", err)
	}
	state, err = s.ReadSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Export.Charges) != 1 || state.Export.Charges[0].Amount != "2" {
		t.Fatal("actual overrun lost")
	}
	cap := usage.Capacity(state.Export, p, d.ConfigurationID, &d, testAt.Add(32*time.Second))
	if len(cap.Balances) != 1 || cap.Balances[0].Held != "0" || cap.Balances[0].PendingCharges != "2" {
		t.Fatalf("double debit: %+v", cap)
	}
	if _, err = s.Ingest(ctx, "clock-back", usage.EmptyExport(), p, testAt); err == nil {
		t.Fatal("accepted backward clock or invalid input")
	}
}

func TestRecoverReservationScopesFenceAndIdempotencyToOneHold(t *testing.T) {
	s, p, d := newStore(t)
	ctx := context.Background()
	launched := reserve(t, s, p, d, "target-run-attempt")
	unrelated := reserve(t, s, p, d, "unrelated-run-attempt")
	launch := Transition{Key: "target-launch", Token: token(launched), ProfileSHA256: d.ProfileSHA256}
	if _, err := s.ConfirmLaunch(ctx, launch, p, testAt.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	before, err := s.ReadSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var otherBefore usage.Reservation
	for _, r := range before.Export.Reservations {
		if r.ID == unrelated.ID {
			otherBefore = r
		}
	}
	old := token(launched)
	key := "recover-one"
	result, err := s.RecoverReservation(ctx, key, old, "runtime-recovery", p, testAt.Add(31*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	got := result.Reservation
	if got == nil || got.ID != launched.ID || got.Owner != "runtime-recovery" || got.Fence != old.Fence+1 || got.State != "uncertain" {
		t.Fatalf("unsafe scoped recovery: %+v", got)
	}
	again, err := s.RecoverReservation(ctx, key, old, "runtime-recovery", p, testAt.Add(time.Minute))
	if err != nil || usage.Digest(result) != usage.Digest(again) {
		t.Fatalf("recovery replay changed: %v", err)
	}
	if _, err = s.RecoverReservation(ctx, "stale-recover", old, "another-owner", p, testAt.Add(time.Minute)); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale token recovered twice: %v", err)
	}
	after, err := s.ReadSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range after.Export.Reservations {
		if r.ID == unrelated.ID && usage.Digest(r) != usage.Digest(otherBefore) {
			t.Fatalf("unrelated hold changed: before=%+v after=%+v", otherBefore, r)
		}
	}
}

func TestRecoverReservationReleasesOnlyExpiredUnlaunchedHold(t *testing.T) {
	s, p, d := newStore(t)
	ctx := context.Background()
	target := reserve(t, s, p, d, "unlaunched")
	other := reserve(t, s, p, d, "other")
	result, err := s.RecoverReservation(ctx, "recover-unlaunched", token(target), "recovery-owner", p, testAt.Add(31*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if result.Reservation == nil || result.Reservation.State != "released" || result.Reservation.Fence != target.Fence+1 {
		t.Fatalf("unlaunched hold not fenced and released: %+v", result.Reservation)
	}
	state, err := s.ReadSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range state.Export.Reservations {
		if r.ID == other.ID && r.State != "held" {
			t.Fatalf("unrelated reservation was recovered: %+v", r)
		}
	}
}

func TestAllWindowAdmissionRollsBackAndLaunchIntentSurvivesRestart(t *testing.T) {
	ctx := context.Background()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	os.Chmod(dir, 0700)
	s, err := Open(filepath.Join(dir, "ledger.sqlite"), "test-runtime")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	e, p, d := fixture(t)
	second := e.Quotas[0]
	second.ID = "second-window"
	second.Constraint.LimitID = "monthly"
	second.Constraint.ID = usage.ConstraintID(second.Constraint)
	zero := json.Number("0")
	second.Remaining = &zero
	e.Quotas = append(e.Quotas, second)
	e.Bindings[0].ConstraintIDs = append(e.Bindings[0].ConstraintIDs, second.Constraint.ID)
	d.Amounts = append(d.Amounts, usage.Amount{ConstraintID: second.Constraint.ID, Generation: second.Generation, Unit: "requests", Amount: "1"})
	if _, err = s.Ingest(ctx, "initial-two-windows", e, p, testAt); err != nil {
		t.Fatal(err)
	}
	before, _ := s.ReadSnapshot(ctx)
	if _, err = s.Reserve(ctx, ReserveRequest{Key: "all-or-none", ID: "all-or-none", Owner: "owner", AttemptID: "attempt", ExpectedRevision: before.Export.LedgerRevision, LeaseUntil: testAt.Add(30 * time.Second).Format(time.RFC3339), Demand: d}, p, testAt); !errors.Is(err, ErrBlocked) {
		t.Fatalf("exhausted second window admitted: %v", err)
	}
	after, _ := s.ReadSnapshot(ctx)
	if usage.Digest(before) != usage.Digest(after) {
		t.Fatal("failed all-window reservation changed state")
	}
	// Use a separate bound store to simulate process loss after committed launch
	// intent. Restart must preserve the charge even if no output was observed.
	s2, p2, d2 := newStore(t)
	hold := reserve(t, s2, p2, d2, "launch-intent")
	if _, err = s2.ConfirmLaunch(ctx, Transition{Key: "launch-intent-commit", Token: token(hold), ProfileSHA256: d2.ProfileSHA256}, p2, testAt); err != nil {
		t.Fatal(err)
	}
	path := s2.path
	s2.Close()
	s3, err := Open(path, "test-runtime")
	if err != nil {
		t.Fatal(err)
	}
	defer s3.Close()
	if _, err = s3.Recover(ctx, "restart-recover", "new-owner", p2, testAt.Add(31*time.Second)); err != nil {
		t.Fatal(err)
	}
	state, err := s3.ReadSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if state.Export.Reservations[0].State != "uncertain" {
		t.Fatal("restart freed unconfirmed launch charge")
	}
	cap := usage.Capacity(state.Export, p2, d2.ConfigurationID, &d2, testAt.Add(31*time.Second))
	if cap.Balances[0].Held != "1" {
		t.Fatal("restart lost in-flight hold")
	}
}

func TestPolicyMigrationAndAtomicWindows(t *testing.T) {
	s, p, d := newStore(t)
	_ = reserve(t, s, p, d, "held")
	p2 := p
	p2.SafetyMargins = map[string]json.Number{d.Amounts[0].ConstraintID: "1"}
	state, _ := s.ReadSnapshot(context.Background())
	req := ReserveRequest{Key: "new-policy", ID: "new-policy", Owner: "owner", AttemptID: "attempt", ExpectedRevision: state.Export.LedgerRevision, LeaseUntil: testAt.Add(30 * time.Second).Format(time.RFC3339), Demand: d}
	if _, err := s.Reserve(context.Background(), req, p2, testAt); !errors.Is(err, ErrConflict) {
		t.Fatalf("policy changed under hold: %v", err)
	}
	bad := d
	bad.Amounts = append(append([]usage.Amount{}, d.Amounts...), usage.Amount{ConstraintID: strings.Repeat("a", 64), Generation: "window", Unit: "requests", Amount: "100"})
	req.Key = "bad-vector"
	req.ID = "bad-vector"
	req.Demand = bad
	if _, err := s.Reserve(context.Background(), req, p, testAt); err == nil {
		t.Fatal("accepted partial window vector")
	}
	after, _ := s.ReadSnapshot(context.Background())
	if after.Export.LedgerRevision != state.Export.LedgerRevision || len(after.Export.Reservations) != 1 {
		t.Fatal("failed transaction left state")
	}
}

func TestOldWindowSettlementCannotFreeNewWindowExposure(t *testing.T) {
	s, p, d := newStore(t)
	ctx := context.Background()
	hold := reserve(t, s, p, d, "spanning-reset")
	if _, err := s.ConfirmLaunch(ctx, Transition{Key: "launch-reset", Token: token(hold), ProfileSHA256: d.ProfileSHA256}, p, testAt); err != nil {
		t.Fatal(err)
	}
	e, _, _ := fixture(t)
	e.Quotas[0].ID = "new-window-snapshot"
	e.Quotas[0].Generation = "new-window"
	e.Quotas[0].Revision = 1
	if _, err := s.Ingest(ctx, "new-window-observation", e, p, testAt.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Settle(ctx, Transition{Key: "must-not-free", Token: token(hold), Actual: d.Amounts}, p, testAt.Add(2*time.Second)); !errors.Is(err, ErrConflict) {
		t.Fatalf("old-window settlement freed exposure: %v", err)
	}
	state, err := s.ReadSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if state.Export.Reservations[0].State != "in-flight" {
		t.Fatal("generation change released hold")
	}
	newDemand := d
	newDemand.Amounts = append([]usage.Amount{}, d.Amounts...)
	newDemand.Amounts[0].Generation = "new-window"
	cap := usage.Capacity(state.Export, p, d.ConfigurationID, &newDemand, testAt.Add(2*time.Second))
	if cap.Disposition == "observed-fit" {
		t.Fatal("new window ignored ongoing exposure")
	}
}

func TestCrossSourceWindowOrdinalsAreNotComparableAtSettlement(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	os.Chmod(dir, 0700)
	s, err := Open(filepath.Join(dir, "ledger.sqlite"), "test-runtime")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	e, p, d := fixture(t)
	source2 := p.Sources[0]
	source2.ID = "second-source"
	source2.Host = "claude-code"
	p.Sources = append(p.Sources, source2)
	b2 := e.Bindings[0]
	b2.ID = "second-binding"
	b2.SourceID = source2.ID
	b2.Host = source2.Host
	b2.StreamID = "second-stream"
	e.Bindings = append(e.Bindings, b2)
	e.PolicySHA256 = usage.PolicyHash(p)
	e.Quotas[0].Revision = 9
	if _, err = s.Ingest(ctx, "two-sources", e, p, testAt); err != nil {
		t.Fatal(err)
	}
	hold := reserve(t, s, p, d, "cross-source")
	if _, err = s.ConfirmLaunch(ctx, Transition{Key: "cross-source-launch", Token: token(hold), ProfileSHA256: d.ProfileSHA256}, p, testAt); err != nil {
		t.Fatal(err)
	}
	e.Quotas[0].ID = "second-source-new-window"
	e.Quotas[0].Origin.SourceID = source2.ID
	e.Quotas[0].Origin.Host = source2.Host
	e.Quotas[0].Revision = 1
	e.Quotas[0].Generation = "next-window"
	if _, err = s.Ingest(ctx, "second-source-observation", e, p, testAt.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Settle(ctx, Transition{Key: "cross-source-must-not-free", Token: token(hold), Actual: d.Amounts}, p, testAt.Add(2*time.Second)); !errors.Is(err, ErrConflict) {
		t.Fatalf("compared source-local revisions across streams: %v", err)
	}
}

func TestRestartPermissionsAndCorruption(t *testing.T) {
	s, p, d := newStore(t)
	_ = reserve(t, s, p, d, "held")
	path := s.path
	s.Close()
	reopened, err := Open(path, "test-runtime")
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	state, err := reopened.ReadSnapshot(context.Background())
	if err != nil || len(state.Export.Reservations) != 1 {
		t.Fatalf("restart: %v", err)
	}
	if _, err = Open(path, "different-runtime"); err == nil {
		t.Fatal("authority switch accepted")
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if info, e := os.Stat(path + suffix); e == nil && info.Mode().Perm()&0077 != 0 {
			t.Fatalf("non-private sidecar %s", suffix)
		}
	}
	if _, err = reopened.db.Exec("UPDATE audit SET previous_digest='tampered' WHERE sequence=2"); err != nil {
		t.Fatal(err)
	}
	if _, err = reopened.ReadSnapshot(context.Background()); err == nil {
		t.Fatal("corrupt audit accepted")
	}
}

func TestIdempotencyCorruptionBlocksReadsAndWrites(t *testing.T) {
	s, p, d := newStore(t)
	_ = reserve(t, s, p, d, "reserved")
	if _, err := s.db.Exec("UPDATE operations SET result='{}' WHERE key=?", usage.Hash([]byte("reserved"))); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReadSnapshot(context.Background()); err == nil {
		t.Fatal("corrupt operation cache accepted")
	}
	if _, err := s.Recover(context.Background(), "must-not-mutate", "recovery-owner", p, testAt.Add(time.Minute)); err == nil {
		t.Fatal("write appended to corrupt operation log")
	}
}

func TestSanitizedCaptureLeavesNoPrivateBytesInDatabaseOrSidecars(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	os.Chmod(dir, 0700)
	s, err := Open(filepath.Join(dir, "ledger.sqlite"), "test-runtime")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p := usage.Policy{Version: usage.Version, Sources: []usage.SourceRule{{ID: "capture", Host: "codex", HostVersion: "protocol-v2-fixture", AdapterVersion: collectors.AdapterVersion, AuthModes: []string{"chatgpt"}, Metrics: []string{"percentage"}, Authority: "openai", MaxAgeSeconds: 60}}, SafetyMargins: map[string]json.Number{}}
	b := usage.Binding{ID: "binding", PoolID: "pool", Provider: "openai", Service: "codex", Host: "codex", HostVersion: "protocol-v2-fixture", AuthMode: "chatgpt", SourceID: "capture", StreamID: "stream"}
	raw := []byte(`{"id":1,"result":{"rateLimits":{"limitId":"codex","primary":{"usedPercent":10,"windowDurationMins":300,"resetsAt":1793000000}},"accessToken":"CANARY_CREDENTIAL_6932","prompt":"CANARY_PRIVATE_PROMPT_3108","cwd":"/CANARY_PRIVATE_PATH_9255"}}`)
	ex, report := collectors.Normalize("codex", raw, b, p, testAt)
	if report.Status != "valid" {
		t.Fatalf("normalize: %+v", report)
	}
	if _, err = s.Ingest(context.Background(), "sanitized", ex, p, testAt); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		data, e := os.ReadFile(s.path + suffix)
		if errors.Is(e, os.ErrNotExist) {
			continue
		}
		if e != nil {
			t.Fatal(e)
		}
		for _, canary := range []string{"CANARY_CREDENTIAL_6932", "CANARY_PRIVATE_PROMPT_3108", "CANARY_PRIVATE_PATH_9255"} {
			if strings.Contains(string(data), canary) {
				t.Fatalf("private bytes in accounting file %s", suffix)
			}
		}
	}
}

func TestCancellationDoesNotMutateAndUnknownSchemaFailsClosed(t *testing.T) {
	s, p, d := newStore(t)
	before, _ := s.ReadSnapshot(context.Background())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Reserve(ctx, ReserveRequest{Key: "cancelled", ID: "cancelled", Owner: "owner", AttemptID: "attempt", ExpectedRevision: before.Export.LedgerRevision, LeaseUntil: testAt.Add(30 * time.Second).Format(time.RFC3339), Demand: d}, p, testAt); err == nil {
		t.Fatal("cancelled request committed")
	}
	after, _ := s.ReadSnapshot(context.Background())
	if usage.Digest(before) != usage.Digest(after) {
		t.Fatal("cancellation changed state")
	}
	path := s.path
	if _, err := s.db.Exec("PRAGMA user_version=999"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if next, err := Open(path, "test-runtime"); err == nil {
		next.Close()
		t.Fatal("unknown DB schema accepted")
	}
}

func TestCrashRollsBackUncommittedTransaction(t *testing.T) {
	s, _, _ := newStore(t)
	before, err := s.ReadSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestStoreChild$")
	cmd.Env = append(os.Environ(), "AOK_STORE_CHILD=crash", "AOK_STORE_PATH="+s.path)
	if err = cmd.Run(); err == nil {
		t.Fatal("crash child unexpectedly succeeded")
	}
	after, err := s.ReadSnapshot(context.Background())
	if err != nil || usage.Digest(before) != usage.Digest(after) {
		t.Fatalf("uncommitted state survived crash: %v", err)
	}
}

func TestHundredProcessReservations(t *testing.T) {
	if testing.Short() {
		t.Skip("multi-process conformance")
	}
	s, p, d := newStore(t)
	state, err := s.ReadSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	capacity := string(*state.Export.Quotas[0].Remaining)
	if capacity != "10" {
		t.Fatalf("fixture expected capacity10 got %s", capacity)
	}
	const n = 100
	var wg sync.WaitGroup
	outcomes := make(chan string, n)
	errs := make(chan string, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			cmd := exec.Command(os.Args[0], "-test.run=^TestStoreChild$")
			cmd.Env = append(os.Environ(), "AOK_STORE_CHILD=reserve", "AOK_STORE_PATH="+s.path, fmt.Sprintf("AOK_STORE_INDEX=%d", i))
			out, e := cmd.CombinedOutput()
			if e != nil {
				errs <- fmt.Sprintf("%v: %s", e, out)
				return
			}
			outcomes <- string(out)
		}(i)
	}
	wg.Wait()
	close(outcomes)
	close(errs)
	for e := range errs {
		t.Error(e)
	}
	winners := 0
	for out := range outcomes {
		if strings.Contains(out, "reserved") {
			winners++
		}
	}
	after, err := s.ReadSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if winners < 1 || winners > 10 || len(after.Export.Reservations) != winners {
		t.Fatalf("admission mismatch winners=%d holds=%d", winners, len(after.Export.Reservations))
	}
	cap := usage.Capacity(after.Export, p, d.ConfigurationID, &d, testAt)
	if cap.Balances[0].Held != json.Number(fmt.Sprint(winners)) {
		t.Fatalf("lost hold %v", cap)
	}
	t.Logf("100 competing processes: %d admitted, remainder blocked/conflicted/deadline-limited; no over-admission", winners)
}

func TestStoreChild(t *testing.T) {
	mode := os.Getenv("AOK_STORE_CHILD")
	if mode == "" {
		return
	}
	path := os.Getenv("AOK_STORE_PATH")
	if mode == "crash" {
		db, e := sqlOpenChild(path)
		if e != nil {
			os.Exit(8)
		}
		if _, e = db.Exec("BEGIN IMMEDIATE; UPDATE state SET digest='uncommitted'"); e != nil {
			os.Exit(9)
		}
		os.Exit(23)
	}
	s, err := Open(path, "test-runtime")
	if err != nil {
		if errors.Is(err, ErrConflict) {
			fmt.Println("conflict")
			return
		}
		t.Fatal(err)
	}
	defer s.Close()
	_, p, d := fixture(t)
	id := "worker-" + os.Getenv("AOK_STORE_INDEX")
	for attempt := 0; attempt < 10; attempt++ {
		state, e := s.ReadSnapshot(context.Background())
		if e != nil {
			t.Fatal(e)
		}
		_, e = s.Reserve(context.Background(), ReserveRequest{Key: id, ID: id, Owner: id, AttemptID: id, ExpectedRevision: state.Export.LedgerRevision, LeaseUntil: testAt.Add(30 * time.Second).Format(time.RFC3339), Demand: d}, p, testAt)
		if e == nil {
			fmt.Println("reserved")
			return
		}
		if errors.Is(e, ErrBlocked) {
			fmt.Println("blocked")
			return
		}
		if errors.Is(e, context.DeadlineExceeded) {
			// A race-instrumented contender may exhaust the unchanged store
			// deadline after acquiring the lock. Do not retry an uncertain
			// result: the parent still requires every persisted hold to match
			// an explicitly successful child and enforces the capacity ceiling.
			fmt.Println("deadline")
			return
		}
		if !errors.Is(e, ErrConflict) {
			t.Fatal(e)
		}
	}
	fmt.Println("conflict")
}

func TestSymlinksAndPublicDirectoryRejected(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err = Open(filepath.Join(dir, "ledger.sqlite"), "runtime"); err == nil {
		t.Fatal("public directory accepted")
	}
	os.Chmod(dir, 0700)
	outside := filepath.Join(dir, "outside")
	os.WriteFile(outside, []byte("keep"), 0600)
	link := filepath.Join(dir, "ledger.sqlite")
	if err = os.Symlink(outside, link); err != nil {
		t.Skip(err)
	}
	if _, err = Open(link, "runtime"); err == nil {
		t.Fatal("symlink accepted")
	}
	content, _ := os.ReadFile(outside)
	if string(content) != "keep" {
		t.Fatal("symlink target modified")
	}
}
