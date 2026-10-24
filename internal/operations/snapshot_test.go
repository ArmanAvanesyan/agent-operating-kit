package operations

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestSnapshotCommittedWALAndTamper(t *testing.T) {
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	source := filepath.Join(root, "live.sqlite")
	db, e := sql.Open("sqlite", source)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	if _, e = db.Exec("PRAGMA journal_mode=WAL"); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec("CREATE TABLE obligations (id TEXT); INSERT INTO obligations VALUES ('uncertain-hold')"); e != nil {
		t.Fatal(e)
	}
	out := filepath.Join(root, "backup")
	r, e := SnapshotSQLite(context.Background(), source, "runtime", out)
	if e != nil {
		t.Fatal(e)
	}
	if !r.InspectionOnly || r.ExecutionReady {
		t.Fatal(r)
	}
	if _, e = VerifySnapshot(out); e != nil {
		t.Fatal(e)
	}
	copyDB, e := sql.Open("sqlite", filepath.Join(out, "database.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	var id string
	e = copyDB.QueryRow("SELECT id FROM obligations").Scan(&id)
	copyDB.Close()
	if e != nil || id != "uncertain-hold" {
		t.Fatalf("%s %v", id, e)
	}
	if _, e = SnapshotSQLite(context.Background(), source, "runtime", out); e == nil {
		t.Fatal("overwrote output")
	}
	if e = os.WriteFile(filepath.Join(out, "database.sqlite"), []byte("tampered"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = VerifySnapshot(out); e == nil {
		t.Fatal("tamper accepted")
	}
}
func TestSnapshotRejectsSymlinkAndCancellation(t *testing.T) {
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	source := filepath.Join(root, "source")
	os.WriteFile(source, []byte("db"), 0600)
	link := filepath.Join(root, "link")
	if os.Symlink(source, link) == nil {
		if _, e = SnapshotSQLite(context.Background(), link, "runtime", filepath.Join(root, "out")); e == nil {
			t.Fatal("symlink accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e = SnapshotSQLite(ctx, source, "runtime", filepath.Join(root, "out2")); e == nil {
		t.Fatal("cancel ignored")
	}
}
