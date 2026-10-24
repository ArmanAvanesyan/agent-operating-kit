package store

import (
	"context"
	"database/sql"
	"errors"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
)

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func verifyAudit(ctx context.Context, q queryer, state State, digest string) error {
	root := State{Epoch: state.Epoch, Export: usage.EmptyExport()}
	previous := usage.Digest(root)
	rows, err := q.QueryContext(ctx, "SELECT a.state_digest,a.previous_digest,a.payload,a.request_digest,a.result_digest,o.request_digest,o.result FROM audit a LEFT JOIN operations o ON o.key=a.operation ORDER BY a.sequence")
	if err != nil {
		return err
	}
	defer rows.Close()
	count := int64(0)
	for rows.Next() {
		var current, prior string
		var b []byte
		var requestHash, resultHash string
		var savedRequest sql.NullString
		var savedResult []byte
		if err = rows.Scan(&current, &prior, &b, &requestHash, &resultHash, &savedRequest, &savedResult); err != nil {
			return err
		}
		if !savedRequest.Valid || savedRequest.String != requestHash || usage.Hash(savedResult) != resultHash {
			return errors.New("accounting idempotency integrity mismatch")
		}
		if usage.Hash(b) != current || prior != previous {
			return errors.New("accounting audit integrity mismatch")
		}
		previous = current
		count++
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if err = rows.Close(); err != nil {
		return err
	}
	var operations int64
	if err = q.QueryRowContext(ctx, "SELECT COUNT(*) FROM operations").Scan(&operations); err != nil {
		return err
	}
	if operations != count {
		return errors.New("accounting idempotency/audit count mismatch")
	}
	if count != state.Export.LedgerRevision || previous != digest {
		return errors.New("accounting projection/audit mismatch")
	}
	return nil
}
