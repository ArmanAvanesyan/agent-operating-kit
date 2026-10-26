package writeback

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func wkRoot(t *testing.T) string {
	p, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func TestWorkkitRouteCASAdmissionAndPreservation(t *testing.T) {
	ctx := context.Background()
	j, e := OpenWorkkit(ctx, wkRoot(t)+"/journal.db", "new-disposable", "workkit", true)
	if e != nil {
		t.Fatal(e)
	}
	defer j.Close()
	now := time.Now().UTC()
	state := map[string]any{"initialized": true, "paused": false, "unresolved": []string{"op"}, "pendingPlanSha256": "plan"}
	raw, _ := json.Marshal(state)
	if e = j.CommitRoute(ctx, "route", 0, raw, "init", DigestBytes(raw), []string{"field"}, nil, now); e != nil {
		t.Fatal(e)
	}
	if e = j.Box.BindWorkkitAdmission("route", 1, "plan", "op"); e != nil {
		t.Fatal(e)
	}
	tx, e := j.Box.db.Begin()
	if e != nil {
		t.Fatal(e)
	}
	if e = j.Box.workkitGate(tx); e != nil {
		t.Fatal(e)
	}
	tx.Rollback()
	state["paused"] = true
	raw, _ = json.Marshal(state)
	if e = j.CommitRoute(ctx, "route", 1, raw, "pause", DigestBytes(raw), nil, nil, now.Add(time.Second)); e != nil {
		t.Fatal(e)
	}
	tx, _ = j.Box.db.Begin()
	if e = j.Box.workkitGate(tx); e == nil {
		t.Fatal("paused route admitted")
	}
	tx.Rollback()
	// Resume does not clear the pending plan or obligations; stale fences remain blocked.
	if revision, payload, e := j.Route(ctx, "route"); e != nil || revision != 2 || string(payload) != string(raw) {
		t.Fatal(revision, e)
	}
	if e = j.CommitRoute(ctx, "other", 0, raw, "conflicting-owner", DigestBytes(raw), []string{"field"}, nil, now.Add(time.Second)); e == nil {
		t.Fatal("duplicate field owner")
	}
	if e = j.CommitRoute(ctx, "route", 2, raw, "unconfirmed-baseline", DigestBytes(raw), nil, []string{"unconfirmed"}, now.Add(time.Second)); e == nil {
		t.Fatal("unconfirmed baseline")
	}
}
func TestWorkkitConcurrentRouteAdmissionAndNoOverwrite(t *testing.T) {
	ctx := context.Background()
	j, e := OpenWorkkit(ctx, wkRoot(t)+"/journal.db", "new", "workkit", true)
	if e != nil {
		t.Fatal(e)
	}
	defer j.Close()
	identity := j.Identity
	raw := []byte(`{"revision":1}`)
	var wg sync.WaitGroup
	var lock sync.Mutex
	success := 0
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w, e := OpenWorkkitWritable(ctx, identity)
			if e != nil {
				return
			}
			defer w.Close()
			if e = w.CommitRoute(ctx, "route", 0, raw, "same-use", DigestBytes(raw), nil, nil, time.Now().UTC()); e == nil {
				lock.Lock()
				success++
				lock.Unlock()
			}
		}()
	}
	wg.Wait()
	if success != 1 {
		t.Fatal("duplicate route admission", success)
	}
	if _, e = OpenWorkkit(ctx, identity.Path, identity.Namespace, identity.Owner, true); e == nil {
		t.Fatal("overwrote journal")
	}
	if e = j.PutPlan(ctx, "op", DigestBytes([]byte("one")), "comment", []byte("one")); e != nil {
		t.Fatal(e)
	}
	if e = j.PutPlan(ctx, "op", DigestBytes([]byte("two")), "comment", []byte("two")); e == nil {
		t.Fatal("changed operation accepted")
	}
}

func TestWorkkitCrashChild(t *testing.T) {
	if os.Getenv("AOK_TEST_WORKKIT_CRASH") != "1" {
		return
	}
	ctx := context.Background()
	path := os.Getenv("AOK_TEST_WORKKIT_DB")
	j, e := OpenWorkkit(ctx, path, "synthetic-crash", "workkit", true)
	if e != nil {
		os.Exit(72)
	}
	raw := []byte(`{"initialized":true,"paused":false,"revision":1,"pendingPlanSha256":"synthetic-plan","unresolved":["pending-op"]}`)
	if e = j.CommitRoute(ctx, "route", 0, raw, "admit", DigestBytes(raw), nil, nil, time.Now().UTC()); e != nil {
		os.Exit(73)
	}
	os.Exit(71)
}
func TestWorkkitCrashAfterAdmissionPreservesPending(t *testing.T) {
	ctx := context.Background()
	path := wkRoot(t) + "/journal.db"
	cmd := exec.Command(os.Args[0], "-test.run=^TestWorkkitCrashChild$")
	cmd.Env = append(os.Environ(), "AOK_TEST_WORKKIT_CRASH=1", "AOK_TEST_WORKKIT_DB="+path)
	e := cmd.Run()
	exit, ok := e.(*exec.ExitError)
	if !ok || exit.ExitCode() != 71 {
		t.Fatal("crash fixture failed", e)
	}
	j, e := OpenWorkkit(ctx, path, "synthetic-crash", "workkit", false)
	if e != nil {
		t.Fatal(e)
	}
	defer j.Close()
	revision, raw, e := j.Route(ctx, "route")
	if e != nil || revision != 1 || !bytes.Contains(raw, []byte("pending-op")) {
		t.Fatal("lost obligations", revision, e)
	}
}
