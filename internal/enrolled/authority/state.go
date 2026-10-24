package authority

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
)

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}
type stateCommit struct{ Prior, Request, Observed, State string }

// stateDigest commits the logical ledger AND its SQL schema. Exclude only the
// two commitment tables to avoid a recursive hash. Every row is ordered by its
// complete key; SQLite journal/page layout is deliberately not an identity.
func stateDigest(ctx context.Context, q queryer) (string, error) {
	queries := []string{
		`SELECT type,name,tbl_name,sql FROM sqlite_master WHERE name NOT LIKE 'sqlite_%' AND name NOT IN ('anchor_tip','anchor_commit') ORDER BY type,name`,
		`SELECT * FROM identity ORDER BY id`, `SELECT * FROM clock ORDER BY id`,
		`SELECT * FROM budget ORDER BY parent`, `SELECT * FROM consumed ORDER BY parent,logical,boundary`,
		`SELECT * FROM epochs ORDER BY authority`, `SELECT * FROM captures ORDER BY parent,source`,
		`SELECT * FROM capture_results ORDER BY parent,source,ordinal`, `SELECT * FROM stops ORDER BY parent`,
		`SELECT * FROM nonces ORDER BY enrollment,nonce`,
	}
	all := make([][][]any, 0, len(queries))
	for _, stmt := range queries {
		rows, e := q.QueryContext(ctx, stmt)
		if e != nil {
			return "", e
		}
		cols, e := rows.Columns()
		if e != nil {
			rows.Close()
			return "", e
		}
		group := [][]any{}
		for rows.Next() {
			v := make([]any, len(cols))
			ptr := make([]any, len(cols))
			for i := range v {
				ptr[i] = &v[i]
			}
			if e = rows.Scan(ptr...); e != nil {
				rows.Close()
				return "", e
			}
			group = append(group, v)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return "", e
		}
		all = append(all, group)
	}
	return usage.Digest(all), nil
}
func verifyState(ctx context.Context, q queryer, tip string) error {
	var raw string
	if e := q.QueryRowContext(ctx, `SELECT value FROM anchor_commit WHERE id=1`).Scan(&raw); e != nil {
		return ErrBlocked
	}
	var c stateCommit
	if usage.Decode([]byte(raw), &c) != nil || usage.Digest(c) != tip {
		return ErrBlocked
	}
	h, e := stateDigest(ctx, q)
	if e != nil || h != c.State {
		return ErrBlocked
	}
	return nil
}
func commitState(ctx context.Context, tx *sql.Tx, prior, request, observed string) (string, error) {
	state, e := stateDigest(ctx, tx)
	if e != nil {
		return "", e
	}
	c := stateCommit{Prior: prior, Request: request, Observed: observed, State: state}
	raw, e := json.Marshal(c)
	if e != nil {
		return "", e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO anchor_commit VALUES(1,?) ON CONFLICT(id) DO UPDATE SET value=excluded.value`, string(raw)); e != nil {
		return "", e
	}
	return usage.Digest(c), nil
}
