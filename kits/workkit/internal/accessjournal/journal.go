// Package accessjournal is a Windows durability backend for local auth/install
// actions only. It never owns tracker sends, runtime attempts or secret values.
package accessjournal

import (
	"context"
	"database/sql"
	"errors"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/engine"
	_ "modernc.org/sqlite"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

func open(ctx context.Context, root string) (*sql.DB, error) {
	if _, e := engine.SafePath(root); e != nil {
		return nil, e
	}
	p := filepath.Join(root, "workkit-access.sqlite")
	if _, e := os.Lstat(p); e == nil {
		if _, e = engine.SafePath(p); e != nil {
			return nil, e
		}
	} else if !os.IsNotExist(e) {
		return nil, e
	}
	uriPath := filepath.ToSlash(p)
	if filepath.VolumeName(p) != "" {
		uriPath = "/" + uriPath
	}
	uri := url.URL{Scheme: "file", Path: uriPath}
	db, e := sql.Open("sqlite", uri.String())
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	var version int
	if e = db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); e != nil || version != 0 && version != 1 {
		db.Close()
		return nil, errors.New("unsupported access journal version")
	}
	if version == 0 {
		var tables int
		if e = db.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table'").Scan(&tables); e != nil || tables != 0 {
			db.Close()
			return nil, errors.New("unversioned nonempty journal rejected")
		}
	}
	if _, e = db.ExecContext(ctx, `PRAGMA busy_timeout=5000; PRAGMA journal_mode=WAL; PRAGMA synchronous=FULL;
 CREATE TABLE IF NOT EXISTS consumption (id TEXT PRIMARY KEY, receipt BLOB);
 CREATE TABLE IF NOT EXISTS holds (id TEXT PRIMARY KEY);
 CREATE TABLE IF NOT EXISTS states (id TEXT PRIMARY KEY, payload BLOB NOT NULL); PRAGMA user_version=1;`); e != nil {
		db.Close()
		return nil, e
	}
	return db, nil
}
func Consume(ctx context.Context, root, id string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	db, e := open(ctx, root)
	if e != nil {
		return e
	}
	defer db.Close()
	_, e = db.ExecContext(ctx, "INSERT INTO consumption(id) VALUES(?)", id)
	if e != nil {
		return errors.New("operation consumed or journal unavailable")
	}
	return nil
}
func Hold(ctx context.Context, root, id string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	db, e := open(ctx, root)
	if e != nil {
		return e
	}
	defer db.Close()
	_, e = db.ExecContext(ctx, "INSERT INTO holds(id) VALUES(?)", id)
	if e != nil {
		return errors.New("scope held or journal unavailable")
	}
	return nil
}
func Complete(ctx context.Context, root, operation, hold, stateKey string, receipt, state []byte) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	db, e := open(ctx, root)
	if e != nil {
		return e
	}
	defer db.Close()
	tx, e := db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if operation != "" {
		result, e := tx.ExecContext(ctx, "UPDATE consumption SET receipt=? WHERE id=? AND receipt IS NULL", receipt, operation)
		if e != nil {
			return e
		}
		n, e := result.RowsAffected()
		if e != nil || n != 1 {
			return errors.New("operation finalization conflict")
		}
	}
	if stateKey != "" {
		if _, e = tx.ExecContext(ctx, "INSERT INTO states(id,payload) VALUES(?,?) ON CONFLICT(id) DO UPDATE SET payload=excluded.payload", stateKey, state); e != nil {
			return e
		}
	}
	if hold != "" {
		if _, e = tx.ExecContext(ctx, "DELETE FROM holds WHERE id=?", hold); e != nil {
			return e
		}
	}
	return tx.Commit()
}
func State(ctx context.Context, root, key string) ([]byte, error) {
	db, e := open(ctx, root)
	if e != nil {
		return nil, e
	}
	defer db.Close()
	var b []byte
	e = db.QueryRowContext(ctx, "SELECT payload FROM states WHERE id=?", key).Scan(&b)
	return b, e
}
