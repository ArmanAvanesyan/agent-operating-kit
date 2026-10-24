package operations

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// Child holds an uncommitted attempted release. Snapshotting must never turn
// that partial write into evidence that an uncertain obligation was refunded.
func TestSnapshotChild(t *testing.T) {
	path := os.Getenv("AOK_P9_SNAPSHOT_CHILD")
	if path == "" {
		return
	}
	db, e := sql.Open("sqlite", path)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	tx, e := db.Begin()
	if e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec("DELETE FROM obligations"); e != nil {
		t.Fatal(e)
	}
	fmt.Fprintln(os.Stdout, "ready")
	<-time.After(time.Minute)
}
func TestSnapshotDuringWriterCrashPreservesObligations(t *testing.T) {
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	source := filepath.Join(root, "live.sqlite")
	db, e := sql.Open("sqlite", source)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec("PRAGMA journal_mode=WAL"); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec("CREATE TABLE obligations (id TEXT); INSERT INTO obligations VALUES ('uncertain-send')"); e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, exe, "-test.run=^TestSnapshotChild$")
	child.Env = append(os.Environ(), "AOK_P9_SNAPSHOT_CHILD="+source)
	stdout, e := child.StdoutPipe()
	if e != nil {
		t.Fatal(e)
	}
	if e = child.Start(); e != nil {
		t.Fatal(e)
	}
	defer func() { _ = child.Process.Kill(); _ = child.Wait() }()
	if line, e := bufio.NewReader(stdout).ReadString('\n'); e != nil || line != "ready\n" {
		t.Fatalf("child readiness %q %v", line, e)
	}
	out := filepath.Join(root, "quarantine")
	if _, e = SnapshotSQLite(ctx, source, "outbox", out); e != nil {
		t.Fatal(e)
	}
	_ = child.Process.Kill()
	_ = child.Wait()
	if _, e = VerifySnapshot(out); e != nil {
		t.Fatal(e)
	}
	snap, e := sql.Open("sqlite", filepath.Join(out, "database.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	defer snap.Close()
	var id string
	if e = snap.QueryRow("SELECT id FROM obligations").Scan(&id); e != nil || id != "uncertain-send" {
		t.Fatalf("lost unresolved obligation: %q %v", id, e)
	}
}
