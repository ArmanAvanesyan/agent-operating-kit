package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/methodology"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
)

// EnableGovernance adds opt-in projections to the P7 journal. It neither alters
// old signed records nor activates a methodology or provisioning authority.
func (s *Store) EnableGovernance(ctx context.Context) error {
	_, e := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS governance_subject(subject TEXT PRIMARY KEY, revision INTEGER NOT NULL, payload BLOB NOT NULL);
 CREATE TABLE IF NOT EXISTS governance_event(sequence INTEGER PRIMARY KEY AUTOINCREMENT, kind TEXT NOT NULL, logical_id TEXT NOT NULL UNIQUE, subject TEXT NOT NULL, revision INTEGER NOT NULL, payload BLOB NOT NULL, digest TEXT NOT NULL, at TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS provisioning_intent(logical_id TEXT PRIMARY KEY, digest TEXT NOT NULL, disposition TEXT NOT NULL);`)
	return e
}

type governanceQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func readMethodology(ctx context.Context, q governanceQuerier, subject string) (methodology.Snapshot, error) {
	var snap methodology.Snapshot
	var raw []byte
	var revision uint64
	if e := q.QueryRowContext(ctx, "SELECT payload,revision FROM governance_subject WHERE subject=?", subject).Scan(&raw, &revision); e != nil {
		return snap, e
	}
	if e := usage.Decode(raw, &snap); e != nil {
		return snap, e
	}
	if snap.Subject != subject || snap.Revision != revision {
		return snap, fmt.Errorf("methodology projection revision mismatch")
	}
	var event []byte
	var digest string
	var er uint64
	if e := q.QueryRowContext(ctx, "SELECT payload,digest,revision FROM governance_event WHERE subject=? AND kind='methodology' ORDER BY sequence DESC LIMIT 1", subject).Scan(&event, &digest, &er); e != nil {
		return snap, e
	}
	var t methodology.Transition
	if usage.Hash(event) != digest || usage.Decode(event, &t) != nil || er != revision || t.ExpectedRevision+1 != revision || t.Subject != snap.Subject || t.Profile != snap.Profile || t.To != snap.Stage {
		return snap, fmt.Errorf("methodology journal/projection integrity mismatch")
	}
	return snap, nil
}
func (s *Store) MethodologySnapshot(ctx context.Context, subject string) (methodology.Snapshot, error) {
	return readMethodology(ctx, s.db, subject)
}

// RecordMethodology verifies protected role/evidence inside the same transaction
// as the revision CAS and replay-consumption journal append.
func (s *Store) RecordMethodology(ctx context.Context, p methodology.Profile, t methodology.Transition, keys methodology.KeyResolver, at time.Time) (methodology.Snapshot, error) {
	var current methodology.Snapshot
	if at.IsZero() || len(p.Stages) == 0 {
		return current, fmt.Errorf("protected profile and current instant required")
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return current, e
	}
	defer tx.Rollback()
	current, e = readMethodology(ctx, tx, t.Subject)
	if e == sql.ErrNoRows {
		current = methodology.Snapshot{Subject: t.Subject, Profile: p.ID, Stage: p.Stages[0]}
	} else if e != nil {
		return current, e
	}
	next, e := methodology.RecordTransition(p, current, t, keys, func(expected uint64, n methodology.Snapshot, transition methodology.Transition) error {
		raw, e := json.Marshal(transition)
		if e != nil {
			return e
		}
		payload, e := json.Marshal(n)
		if e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, "INSERT INTO governance_event(kind,logical_id,subject,revision,payload,digest,at) VALUES(?,?,?,?,?,?,?)", "methodology", transition.ID, transition.Subject, n.Revision, raw, usage.Hash(raw), at.UTC().Format(time.RFC3339Nano)); e != nil {
			return e
		}
		if expected == 0 {
			_, e = tx.ExecContext(ctx, "INSERT INTO governance_subject(subject,revision,payload) VALUES(?,?,?)", transition.Subject, n.Revision, payload)
			return e
		}
		result, e := tx.ExecContext(ctx, "UPDATE governance_subject SET revision=?,payload=? WHERE subject=? AND revision=?", n.Revision, payload, transition.Subject, expected)
		if e != nil {
			return e
		}
		count, e := result.RowsAffected()
		if e != nil {
			return e
		}
		if count != 1 {
			return fmt.Errorf("methodology revision conflict")
		}
		return nil
	})
	if e != nil {
		return current, e
	}
	if e = tx.Commit(); e != nil {
		return current, e
	}
	return next, nil
}

// Consume implements provisioning's durable one-send boundary in the same
// runtime database. A crash after this commit never permits an automatic resend.
func (s *Store) Consume(ctx context.Context, id, digest string) error {
	if id == "" || len(digest) != 64 {
		return fmt.Errorf("invalid provisioning intent")
	}
	_, e := s.db.ExecContext(ctx, "INSERT INTO provisioning_intent(logical_id,digest,disposition) VALUES(?,?,?)", id, digest, "unknown-awaiting-reconciliation")
	return e
}
func (s *Store) Record(ctx context.Context, id, disposition string) error {
	if disposition != "unknown-awaiting-reconciliation" && disposition != "readback-observed" {
		return fmt.Errorf("invalid provisioning outcome")
	}
	r, e := s.db.ExecContext(ctx, "UPDATE provisioning_intent SET disposition=? WHERE logical_id=? AND (disposition!='readback-observed' OR ?='readback-observed')", disposition, id, disposition)
	if e != nil {
		return e
	}
	n, e := r.RowsAffected()
	if e != nil {
		return e
	}
	if n != 1 {
		return fmt.Errorf("unconsumed provisioning operation")
	}
	return nil
}

// VerifiedMethodologySnapshot replays the signed approval chain through current
// protected role/evidence services. Plain MethodologySnapshot is an integrity
// projection only and must never serve as an approval by itself.
func (s *Store) VerifiedMethodologySnapshot(ctx context.Context, subject string, p methodology.Profile, keys methodology.KeyResolver) (methodology.Snapshot, error) {
	projected, e := s.MethodologySnapshot(ctx, subject)
	if e != nil {
		return projected, e
	}
	if len(p.Stages) == 0 {
		return projected, fmt.Errorf("protected profile required")
	}
	rows, e := s.db.QueryContext(ctx, "SELECT logical_id,payload,digest,revision FROM governance_event WHERE subject=? AND kind='methodology' ORDER BY sequence", subject)
	if e != nil {
		return projected, e
	}
	defer rows.Close()
	snap := methodology.Snapshot{Subject: subject, Profile: p.ID, Stage: p.Stages[0]}
	count := 0
	for rows.Next() {
		count++
		if count > 10000 {
			return projected, fmt.Errorf("methodology history exceeds verification bound")
		}
		var id, digest string
		var b []byte
		var revision uint64
		if e := rows.Scan(&id, &b, &digest, &revision); e != nil {
			return projected, e
		}
		var tr methodology.Transition
		if usage.Hash(b) != digest || usage.Decode(b, &tr) != nil || tr.ID != id || revision != snap.Revision+1 {
			return projected, fmt.Errorf("methodology journal tampered")
		}
		if e := methodology.CheckTransition(p, snap, tr, keys); e != nil {
			return projected, e
		}
		snap = methodology.Snapshot{Subject: subject, Profile: p.ID, Stage: tr.To, Revision: revision}
	}
	if e := rows.Err(); e != nil {
		return projected, e
	}
	if count == 0 || snap != projected {
		return projected, fmt.Errorf("methodology projection differs from verified history")
	}
	return snap, nil
}
