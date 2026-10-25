package accessjournal

import (
	"context"
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

func TestMain(m *testing.M) {
	if len(os.Args) == 3 && os.Args[1] == "crash-held" {
		ctx := context.Background()
		if Consume(ctx, os.Args[2], "crashed-op") != nil || Hold(ctx, os.Args[2], "crashed-scope") != nil {
			os.Exit(8)
		}
		os.Exit(23)
	}
	os.Exit(m.Run())
}
func TestJournalConcurrencyCrashReplayAndTerminalState(t *testing.T) {
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	os.Chmod(root, 0700)
	ctx := context.Background()
	if e = Consume(ctx, root, "seed"); e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	var winners atomic.Int32
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if Hold(ctx, root, "shared-scope") == nil {
				winners.Add(1)
			}
		}()
	}
	wg.Wait()
	if winners.Load() != 1 {
		t.Fatal("concurrent hold count", winners.Load())
	}
	if e = Complete(ctx, root, "seed", "shared-scope", "state", []byte("receipt-metadata"), []byte("state-metadata")); e != nil {
		t.Fatal(e)
	}
	if e = Consume(ctx, root, "seed"); e == nil {
		t.Fatal("completed operation reused")
	}
	state, e := State(ctx, root, "state")
	if e != nil || string(state) != "state-metadata" {
		t.Fatal(e)
	}
	cmd := exec.Command(os.Args[0], "crash-held", root)
	if e = cmd.Run(); e == nil {
		t.Fatal("crash fixture unexpectedly clean")
	}
	if Consume(ctx, root, "crashed-op") == nil || Hold(ctx, root, "crashed-scope") == nil {
		t.Fatal("crash lost pending protection")
	}
	db, e := sql.Open("sqlite", filepath.Join(root, "workkit-access.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	_, e = db.Exec("PRAGMA user_version=999")
	db.Close()
	if e != nil {
		t.Fatal(e)
	}
	if Consume(ctx, root, "unknown-version") == nil {
		t.Fatal("unknown journal schema accepted")
	}
}
