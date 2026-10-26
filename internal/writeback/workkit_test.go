package writeback_test

import (
	"context"
	"database/sql"
	wb "github.com/ArmanAvanesyan/agent-operating-kit/internal/writeback"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
	"testing"
)

func privateRoot(t *testing.T) string {
	t.Helper()
	p, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func TestReviewDamagedStoreCannotRepairObligations(t *testing.T) {
	ctx := context.Background()
	p := privateRoot(t) + "/journal.db"
	j, e := wb.OpenWorkkit(ctx, p, "fixture-namespace", "fixture-owner", true)
	if e != nil {
		t.Fatal(e)
	}
	identity := j.Identity
	j.Close()
	db, e := sql.Open("sqlite", p)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec("DROP TABLE authorization_uses"); e != nil {
		t.Fatal(e)
	}
	db.Close()
	if opened, e := wb.OpenWorkkitWritable(ctx, identity); e == nil {
		opened.Close()
		t.Fatal("missing consumption table was accepted or repaired")
	}
	db, e = sql.Open("sqlite", p)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	var n int
	if e = db.QueryRow("SELECT count(*) FROM sqlite_master WHERE name='authorization_uses'").Scan(&n); e != nil || n != 0 {
		t.Fatal("reopen modified damaged journal", n, e)
	}
}
func TestReviewLegacyAndCopiedStoresStaySeparate(t *testing.T) {
	ctx := context.Background()
	root := privateRoot(t)
	p := root + "/workkit.db"
	j, e := wb.OpenWorkkit(ctx, p, "fixture-ns", "fixture-owner", true)
	if e != nil {
		t.Fatal(e)
	}
	j.Close()
	if box, e := wb.Open(p); e == nil {
		box.Close()
		t.Fatal("legacy AOK opener accepted WorkKit store")
	}
	raw, e := os.ReadFile(p)
	if e != nil {
		t.Fatal(e)
	}
	copy := root + "/copy.db"
	if e = os.WriteFile(copy, raw, 0600); e != nil {
		t.Fatal(e)
	}
	if opened, e := wb.OpenWorkkit(ctx, copy, "fixture-ns", "fixture-owner", false); e == nil {
		opened.Close()
		t.Fatal("copied store adopted without migration")
	}
	legacy := root + "/legacy.db"
	box, e := wb.Open(legacy)
	if e != nil {
		t.Fatal(e)
	}
	box.Close()
	if opened, e := wb.OpenWorkkit(ctx, legacy, "fixture-ns", "fixture-owner", false); e == nil {
		opened.Close()
		t.Fatal("WorkKit accepted legacy store")
	}
}
