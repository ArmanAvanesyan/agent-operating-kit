package runtime

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/runtime/records"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/testutil"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
	accounting "github.com/ArmanAvanesyan/agent-operating-kit/internal/usage/store"
)

func recoveryAccountingFixture(t *testing.T) (*accounting.Store, usage.Policy, usage.Demand, time.Time) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "offline", "replay.json"))
	if err != nil {
		t.Fatal(err)
	}
	var scenario records.Scenario
	if err = json.Unmarshal(data, &scenario); err != nil {
		t.Fatal(err)
	}
	var ingest ingestPayload
	if err = json.Unmarshal(scenario.Events[0].Payload, &ingest); err != nil {
		t.Fatal(err)
	}
	export, policy, err := ingestFixture(ingest)
	if err != nil {
		t.Fatal(err)
	}
	var reserve reservePayload
	for _, event := range scenario.Events {
		if event.Operation == "reserve" {
			if err = json.Unmarshal(event.Payload, &reserve); err != nil {
				t.Fatal(err)
			}
			break
		}
	}
	if reserve.Demand.ConfigurationID == "" {
		t.Fatal("reserve fixture event missing")
	}
	at, err := time.Parse(time.RFC3339Nano, scenario.Events[0].At)
	if err != nil {
		t.Fatal(err)
	}
	root, err := os.MkdirTemp(testutil.CanonicalTempRoot(t), "aok-recovery-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	store, err := accounting.Open(filepath.Join(root, "accounting.sqlite"), "recovery-test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if _, err = store.Ingest(context.Background(), "test-ingest", export, policy, at); err != nil {
		t.Fatal(err)
	}
	return store, policy, reserve.Demand, at
}

func reserveRecoveryFixture(t *testing.T, store *accounting.Store, policy usage.Policy, demand usage.Demand, at time.Time, attempt string) accounting.Token {
	t.Helper()
	state, err := store.ReadSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	result, err := store.Reserve(context.Background(), accounting.ReserveRequest{
		Key: "reserve-" + attempt, ID: "hold-" + attempt, Owner: "runtime-owner", AttemptID: attempt,
		ExpectedRevision: state.Export.LedgerRevision, LeaseUntil: at.Add(10 * time.Second).Format(time.RFC3339Nano), Demand: demand,
	}, policy, at)
	if err != nil {
		t.Fatal(err)
	}
	r := result.Reservation
	return accounting.Token{ID: r.ID, Owner: r.Owner, Epoch: r.AuthorityEpoch, Fence: r.Fence}
}

func TestRecoverReservationRetryUsesStableKeyAfterLostReply(t *testing.T) {
	ctx := context.Background()
	store, policy, demand, at := recoveryAccountingFixture(t)
	old := reserveRecoveryFixture(t, store, policy, demand, at, "attempt-lost-reply")
	firstAt := at.Add(11 * time.Second)
	first, err := store.RecoverReservation(ctx, accountingRecoveryKey(old), old, "recovery-test", policy, firstAt)
	if err != nil {
		t.Fatal(err)
	}
	if first.Reservation == nil || first.Reservation.State != "released" || first.Reservation.Fence != old.Fence+1 {
		t.Fatalf("unexpected first recovery result: %+v", first.Reservation)
	}
	// Simulate a lost reply: replay exactly the same old token and operation key.
	second, err := store.RecoverReservation(ctx, accountingRecoveryKey(old), old, "recovery-test", policy, firstAt.Add(time.Second))
	if err != nil {
		t.Fatalf("idempotent retry failed: %v", err)
	}
	if usage.Digest(first.Reservation) != usage.Digest(second.Reservation) {
		t.Fatalf("retry returned a different recovered reservation: first=%+v second=%+v", first.Reservation, second.Reservation)
	}
	state, err := store.ReadSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	hold, ok := findAttemptReservation(state.Export.Reservations, old.ID, "attempt-lost-reply", "recovery-test", demand, policy)
	if !ok || hold.State != "released" || hold.Fence != first.Reservation.Fence {
		t.Fatalf("lost-reply retry did not preserve the adopted hold: %+v, found=%v", hold, ok)
	}
}

func TestRecoveryRequiresExactAttemptAndDemandForHoldAdoption(t *testing.T) {
	store, policy, demand, at := recoveryAccountingFixture(t)
	old := reserveRecoveryFixture(t, store, policy, demand, at, "attempt-scoped")
	state, err := store.ReadSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := findAttemptReservation(state.Export.Reservations, old.ID, "different-attempt", "recovery-test", demand, policy); ok {
		t.Fatal("reservation for another attempt was adopted")
	}
	wrongDemand := demand
	wrongDemand.ProfileSHA256 = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	if _, ok := findAttemptReservation(state.Export.Reservations, old.ID, "attempt-scoped", "recovery-test", wrongDemand, policy); ok {
		t.Fatal("reservation with a different profile/demand was adopted")
	}
	if _, ok := findAttemptReservation(state.Export.Reservations, "hold-unrelated", "attempt-scoped", "recovery-test", demand, policy); ok {
		t.Fatal("unrelated reservation ID was adopted")
	}
}

func TestRecoveryFindsAlreadySettledMatchingHoldWithoutTakeover(t *testing.T) {
	ctx := context.Background()
	store, policy, demand, at := recoveryAccountingFixture(t)
	old := reserveRecoveryFixture(t, store, policy, demand, at, "attempt-settled")
	if _, err := store.ConfirmLaunch(ctx, accounting.Transition{Key: "confirm-attempt-settled", Token: old, ProfileSHA256: demand.ProfileSHA256}, policy, at.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	actual := append([]usage.Amount(nil), demand.Amounts...)
	if _, err := store.Reconcile(ctx, accounting.Transition{Key: "settle-attempt-settled", Token: old, Actual: actual}, policy, at.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	state, err := store.ReadSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	hold, ok := findAttemptReservation(state.Export.Reservations, old.ID, "attempt-settled", "recovery-test", demand, policy)
	if !ok || hold.State != "settled" {
		t.Fatalf("already-settled hold was not recognized: %+v, found=%v", hold, ok)
	}
	// Settled is terminal; a recovery takeover must not rewrite it or advance its fence.
	if _, err = store.RecoverReservation(ctx, accountingRecoveryKey(old), old, "recovery-test", policy, at.Add(20*time.Second)); err == nil {
		t.Fatal("settled hold was incorrectly taken over")
	}
	after, err := store.ReadSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if after.Export.Reservations[0].State != "settled" {
		t.Fatal("settled hold changed during failed takeover")
	}
}
