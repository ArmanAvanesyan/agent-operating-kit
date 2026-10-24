package operations

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/evidence"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/modelcatalog"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
	_ "modernc.org/sqlite"
)

// SnapshotReceipt is a consistent single-store SQLite snapshot. It says nothing
// about cross-store atomicity or safe replacement-authority activation.
type SnapshotReceipt struct {
	Version              string `json:"version"`
	StoreID              string `json:"storeId"`
	DatabaseSHA256       string `json:"databaseSha256"`
	InspectionOnly       bool   `json:"inspectionOnly"`
	AuthorizationGranted bool   `json:"authorizationGranted"`
	ExecutionReady       bool   `json:"executionReady"`
}

func realFile(path string) (string, error) {
	abs, e := filepath.Abs(path)
	if e != nil {
		return "", e
	}
	for p := abs; ; p = filepath.Dir(p) {
		st, e := os.Lstat(p)
		if e != nil {
			return "", e
		}
		if st.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("symlink path rejected")
		}
		if p == abs && !st.Mode().IsRegular() {
			return "", errors.New("regular database file required")
		}
		if filepath.Dir(p) == p {
			break
		}
	}
	return abs, nil
}

// SnapshotSQLite captures committed pages including a WAL using SQLite itself.
// New output is immutable; no raw DB/WAL file copying or migration is performed.
// Operators must separately quiesce and coordinate all authority stores.
func SnapshotSQLite(ctx context.Context, source, storeID, out string) (SnapshotReceipt, error) {
	var r SnapshotReceipt
	if ctx.Err() != nil {
		return r, ctx.Err()
	}
	if storeID == "" {
		return r, errors.New("store ID required")
	}
	source, e := realFile(source)
	if e != nil {
		return r, e
	}
	var total int64
	for _, path := range []string{source, source + "-wal", source + "-shm", source + "-journal"} {
		st, err := os.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil || !st.Mode().IsRegular() || st.Mode()&os.ModeSymlink != 0 {
			return r, errors.New("unsafe database sidecar")
		}
		total += st.Size()
		if total > 256<<20 {
			return r, errors.New("store exceeds snapshot byte budget")
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	u := url.URL{Scheme: "file", Path: source}
	q := u.Query()
	q.Set("mode", "ro")
	q.Add("_pragma", "trusted_schema=OFF")
	u.RawQuery = q.Encode()
	db, e := sql.Open("sqlite", u.String())
	if e != nil {
		return r, e
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	var integrity string
	if e = db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity); e != nil || integrity != "ok" {
		return r, fmt.Errorf("source integrity check failed")
	}
	temp, e := os.MkdirTemp("", "aok-snapshot-")
	if e != nil {
		return r, e
	}
	defer os.RemoveAll(temp)
	temp, e = filepath.EvalSymlinks(temp)
	if e != nil {
		return r, e
	}
	dest := filepath.Join(temp, "database.sqlite")
	if _, e = db.ExecContext(ctx, "VACUUM INTO ?", dest); e != nil {
		return r, e
	}
	st, statErr := os.Stat(dest)
	if statErr != nil || st.Size() > 256<<20 {
		return r, errors.New("snapshot exceeds bounded store size")
	}
	data, e := os.ReadFile(dest)
	if e != nil {
		return r, e
	}
	if len(data) > 256<<20 {
		return r, errors.New("snapshot exceeds bounded store size")
	}
	r = SnapshotReceipt{Version: Version, StoreID: storeID, DatabaseSHA256: usage.Hash(data), InspectionOnly: true}
	raw, e := json.MarshalIndent(r, "", "  ")
	if e != nil {
		return r, e
	}
	e = evidence.WriteFiles(out, map[string][]byte{"database.sqlite": data, "snapshot.json": append(raw, '\n')}, source)
	return r, e
}

// VerifySnapshot checks an offline quarantine capture without opening authority.
func VerifySnapshot(root string) (SnapshotReceipt, error) {
	var r SnapshotReceipt
	raw, e := modelcatalog.ReadFile(filepath.Join(root, "snapshot.json"))
	if e != nil {
		return r, e
	}
	if e = usage.Decode(raw, &r); e != nil {
		return r, e
	}
	if r.Version != Version || !r.InspectionOnly || r.AuthorizationGranted || r.ExecutionReady || r.StoreID == "" || !hash.MatchString(r.DatabaseSHA256) {
		return r, errors.New("invalid quarantine snapshot")
	}
	path, e := realFile(filepath.Join(root, "database.sqlite"))
	if e != nil {
		return r, e
	}
	st, e := os.Stat(path)
	if e != nil || st.Size() > 256<<20 {
		return r, errors.New("snapshot size invalid")
	}
	data, e := os.ReadFile(path)
	if e != nil {
		return r, e
	}
	if usage.Hash(data) != r.DatabaseSHA256 {
		return r, errors.New("snapshot hash changed")
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if _, err := os.Lstat(path + suffix); !os.IsNotExist(err) {
			return r, errors.New("quarantine snapshot has mutable sidecars")
		}
	}
	return r, nil
}
