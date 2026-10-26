package writeback

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestP7JournalVersionFencesAndNotifications(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(wkRoot(t), "v2.db")
	j, e := OpenWorkkitV2(ctx, path, "new-p7", "workkit", true)
	if e != nil {
		t.Fatal(e)
	}
	identity := j.Identity
	j.Close()
	if _, e = OpenWorkkit(ctx, path, "new-p7", "workkit", false); e == nil {
		t.Fatal("v2 relabeled v1")
	}
	j, e = OpenWorkkitV2Writable(ctx, identity)
	if e != nil {
		t.Fatal(e)
	}
	defer j.Close()
	now := time.Now().UTC()
	f, e := j.ClaimWorker(ctx, "route", "worker", now)
	if e != nil || f != 1 {
		t.Fatal(f, e)
	}
	if _, e = j.ClaimWorker(ctx, "route", "worker", now); e == nil {
		t.Fatal("duplicate same-ID first claim")
	}
	if _, e = j.ClaimWorker(ctx, "route", "other", now); e == nil {
		t.Fatal("two owners")
	}
	if _, e = j.ClaimWorker(ctx, "route", "worker", now.Add(31*time.Second), f); e == nil {
		t.Fatal("expiry takeover")
	}
	f, e = j.RecoverWorker(ctx, "route", "replacement", strings.Repeat("a", 64), f, now.Add(32*time.Second))
	if e != nil || f != 2 {
		t.Fatal(f, e)
	}
	if _, e = j.ClaimWorker(ctx, "route", "replacement", now.Add(33*time.Second), f); e != nil {
		t.Fatal("verified fence attach failed", e)
	}
	events, e := j.EventsForRoute(ctx, "operator", "route", 0, now.Add(34*time.Second))
	if e != nil || len(events) != 1 {
		t.Fatal(events, e)
	}
	raw, _ := json.Marshal(struct{ Consumer, Route string }{"operator", "route"})
	if e = j.AckEvents(ctx, DigestBytes(raw), events[0].Sequence+1, now.Add(35*time.Second)); e == nil {
		t.Fatal("ack unseen event")
	}
	if e = j.AckEvents(ctx, DigestBytes(raw), events[0].Sequence, now.Add(35*time.Second)); e != nil {
		t.Fatal(e)
	}
	if e = j.CheckWorker(ctx, "route", "worker", 1, now.Add(35*time.Second)); e == nil {
		t.Fatal("stale fence")
	}
}
func TestP7InboxCoverageReplayAndAtomicity(t *testing.T) {
	ctx := context.Background()
	j, e := OpenWorkkitV2(ctx, filepath.Join(wkRoot(t), "journal.db"), "new-p7", "workkit", true)
	if e != nil {
		t.Fatal(e)
	}
	defer j.Close()
	now := time.Now().UTC()
	payload := func(at time.Time) []byte {
		raw, _ := json.Marshal(map[string]any{"receivedAt": at.Format(time.RFC3339Nano)})
		return raw
	}
	body := strings.Repeat("a", 64)
	if status, e := j.IngestHint(ctx, "route", "connection", "delivery", body, payload(now), 2, 1024, now); e != nil || status != "queued" {
		t.Fatal(status, e)
	}
	if status, e := j.IngestHint(ctx, "route", "connection", "delivery", body, payload(now), 2, 1024, now); e != nil || status != "duplicate" {
		t.Fatal(status, e)
	}
	if status, e := j.IngestHint(ctx, "route", "connection", "duplicate-body", body, payload(now), 2, 1024, now); e != nil || status != "coalesced" {
		t.Fatal(status, e)
	}
	candidate := []byte(`{"candidate":"fixture"}`)
	cp := []byte(`{"completedWatermark":"","complete":true}`)
	if e = j.RecordIntegrationRead(ctx, "route", DigestBytes(candidate), candidate, now, map[string][]byte{"connection": cp}, now.Add(time.Second)); e != nil {
		t.Fatal(e)
	}
	if n, e := j.IntegrationHintCount(ctx, "route"); e != nil || n != 0 {
		t.Fatal("covered hints not drained", n, e)
	}
	if status, e := j.IngestHint(ctx, "route", "connection", "new", strings.Repeat("b", 64), payload(now.Add(2*time.Second)), 2, 1024, now.Add(2*time.Second)); e != nil || status != "queued" {
		t.Fatal("drained capacity not usable", status, e)
	}
	if status, e := j.IngestHint(ctx, "route", "connection", "delivery", strings.Repeat("c", 64), payload(now), 2, 1024, now.Add(3*time.Second)); e != nil || status != "quarantined" {
		t.Fatal(status, e)
	}
	events, e := j.EventsForRoute(ctx, "other", "foreign", 0, now.Add(4*time.Second))
	if e != nil || len(events) != 0 {
		t.Fatal("cross-route events", events, e)
	}
	if e = j.PutCheckpoint(ctx, "route", "connection", []byte(`{"completedWatermark":"invented"}`), false, now.Add(5*time.Second)); e == nil {
		t.Fatal("partial advanced completed coverage")
	}
}
func TestP7MultiprocessClaimChild(t *testing.T) {
	if os.Getenv("P7_CLAIM_CHILD") != "1" {
		return
	}
	j, e := OpenWorkkitV2Writable(context.Background(), WorkkitStoreIdentity{Version: WorkkitStoreVersionV2, Kind: "IntegrationJournal", Path: os.Getenv("P7_STORE_PATH"), ID: os.Getenv("P7_STORE_ID"), Namespace: "fixture", Owner: "workkit"})
	if e != nil {
		os.Exit(3)
	}
	defer j.Close()
	if _, e = j.ClaimWorker(context.Background(), "route", os.Getenv("P7_WORKER"), time.Now().UTC()); e != nil {
		os.Exit(4)
	}
}
func TestP7MultiprocessOneOwner(t *testing.T) {
	path := filepath.Join(wkRoot(t), "journal.db")
	j, e := OpenWorkkitV2(context.Background(), path, "fixture", "workkit", true)
	if e != nil {
		t.Fatal(e)
	}
	id := j.Identity.ID
	j.Close()
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, owner := range []string{"one", "two"} {
		wg.Add(1)
		go func(owner string) {
			defer wg.Done()
			cmd := exec.Command(os.Args[0], "-test.run=^TestP7MultiprocessClaimChild$")
			cmd.Env = append(os.Environ(), "P7_CLAIM_CHILD=1", "P7_STORE_PATH="+path, "P7_STORE_ID="+id, "P7_WORKER="+owner)
			results <- cmd.Run()
		}(owner)
	}
	wg.Wait()
	close(results)
	success := 0
	for e := range results {
		if e == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatal("multiple or zero admitted owners", success)
	}
}

func TestP7CheckpointScopeCannotDrainOtherConnection(t *testing.T) {
	ctx := context.Background()
	j, e := OpenWorkkitV2(ctx, filepath.Join(wkRoot(t), "journal.db"), "new-p7", "workkit", true)
	if e != nil {
		t.Fatal(e)
	}
	defer j.Close()
	now := time.Now().UTC()
	cp := []byte(`{"routeId":"route","configurationSha256":"config","connectionId":"left","accountSha256":"account","querySha256":"query","apiVersion":"7.1","completedWatermark":""}`)
	raw := []byte(`{"candidate":"fixture"}`)
	for _, conn := range []string{"left", "right"} {
		if _, e = j.IngestHint(ctx, "route", conn, "id", DigestBytes([]byte(conn)), []byte(`{"receivedAt":"`+now.Format(time.RFC3339Nano)+`"}`), 10, 4096, now); e != nil {
			t.Fatal(e)
		}
	}
	if e = j.RecordIntegrationRead(ctx, "route", DigestBytes(raw), raw, now, map[string][]byte{"left": cp}, now.Add(time.Second)); e != nil {
		t.Fatal(e)
	}
	if n, e := j.IntegrationHintCount(ctx, "route"); e != nil || n != 1 {
		t.Fatal("unrelated hint drained", n, e)
	}
	for _, dimension := range []string{"routeId", "configurationSha256", "connectionId", "accountSha256", "querySha256", "apiVersion"} {
		var changed map[string]any
		json.Unmarshal(cp, &changed)
		changed[dimension] = "substituted"
		b, _ := json.Marshal(changed)
		if e = j.RecordIntegrationRead(ctx, "route", DigestBytes(raw), raw, now, map[string][]byte{"left": b}, now.Add(2*time.Second)); e == nil {
			t.Fatal("checkpoint drift accepted", dimension)
		}
	}
}

func TestP7LatestConflictBlocksPriorBatchAtSharedIntent(t *testing.T) {
	ctx := context.Background()
	j, e := OpenWorkkitV2(ctx, filepath.Join(wkRoot(t), "journal.db"), "new-p7", "workkit", true)
	if e != nil {
		t.Fatal(e)
	}
	defer j.Close()
	now := time.Now().UTC()
	ready := []byte(`{"status":"ready","conflicts":[]}`)
	plan := DigestBytes(ready)
	if e = j.RecordIntegrationPlan(ctx, "route", plan, ready, now); e != nil {
		t.Fatal(e)
	}
	if e = j.BindIntegrationCandidate("route", plan, "", ready); e != nil {
		t.Fatal(e)
	}
	state := []byte(`{"initialized":true,"paused":false,"pendingPlanSha256":"` + plan + `","unresolved":["op"]}`)
	if e = j.CommitRoute(ctx, "route", 0, state, "begin", DigestBytes(state), nil, nil, now); e != nil {
		t.Fatal(e)
	}
	fence, e := j.ClaimWorker(ctx, "route", "worker", now)
	if e != nil {
		t.Fatal(e)
	}
	if e = j.Box.BindWorkkitAdmissionV2("route", 1, plan, "op", "worker", fence); e != nil {
		t.Fatal(e)
	}
	check := func() error {
		tx, e := j.Box.db.BeginTx(ctx, nil)
		if e != nil {
			return e
		}
		defer tx.Rollback()
		return j.Box.workkitGate(tx)
	}
	if e = check(); e != nil {
		t.Fatal("valid shared intent gate", e)
	}
	conflict := []byte(`{"status":"conflict","conflicts":[{"id":"changed"}]}`)
	if e = j.RecordIntegrationPlan(ctx, "route", DigestBytes(conflict), conflict, now.Add(time.Millisecond)); e != nil {
		t.Fatal(e)
	}
	if e = check(); e == nil {
		t.Fatal("old clean proposal bypassed latest conflict")
	}
	if e = j.BindIntegrationCandidate("route", plan, "", ready); e != nil {
		t.Fatal(e)
	}
	if e = j.CommitRoute(ctx, "route", 1, state, "stale-begin", DigestBytes(state), nil, nil, now.Add(time.Second)); e == nil {
		t.Fatal("stale candidate committed")
	}
	// An exact reviewed resolution may replace its original conflict, not a newer one.
	if e = j.BindIntegrationCandidate("route", plan, DigestBytes(conflict), ready); e != nil {
		t.Fatal(e)
	}
	if e = j.CommitRoute(ctx, "route", 1, state, "reviewed-resolution", DigestBytes(state), nil, nil, now.Add(time.Second)); e != nil {
		t.Fatal(e)
	}
	if e = j.Box.BindWorkkitAdmissionV2("route", 2, plan, "op", "worker", fence); e != nil {
		t.Fatal(e)
	}
	if e = check(); e != nil {
		t.Fatal(e)
	}
}
