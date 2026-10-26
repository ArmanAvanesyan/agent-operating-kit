package writeback

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

const workkitApplicationIDV2 = 1464553302
const WorkkitStoreVersionV2 = "workkit-journal/v2"
const integrationSchemaSQL = `CREATE TABLE integration_workers(route TEXT PRIMARY KEY,owner TEXT NOT NULL,fence INTEGER NOT NULL,until_ns INTEGER NOT NULL); CREATE TABLE integration_inbox(route TEXT NOT NULL,connection TEXT NOT NULL,delivery TEXT NOT NULL,body TEXT NOT NULL,payload BLOB NOT NULL,state TEXT NOT NULL,received_ns INTEGER NOT NULL,PRIMARY KEY(route,connection,delivery)); CREATE TABLE integration_events(sequence INTEGER PRIMARY KEY AUTOINCREMENT,route TEXT NOT NULL,kind TEXT NOT NULL,subject TEXT NOT NULL,observed TEXT NOT NULL); CREATE TABLE integration_consumers(id TEXT PRIMARY KEY,route TEXT NOT NULL,delivered INTEGER NOT NULL,acknowledged INTEGER NOT NULL); CREATE TABLE integration_candidates(route TEXT PRIMARY KEY,digest TEXT NOT NULL,payload BLOB NOT NULL); CREATE TABLE integration_checkpoints(route TEXT NOT NULL,connection TEXT NOT NULL,payload BLOB NOT NULL,PRIMARY KEY(route,connection));`

func OpenWorkkitV2(ctx context.Context, path, namespace, owner string, initialize bool) (*WorkkitJournal, error) {
	return openWorkkitFormat(ctx, path, namespace, owner, initialize, workkitApplicationIDV2, WorkkitStoreVersionV2, integrationSchemaSQL)
}
func OpenWorkkitV2Writable(ctx context.Context, s WorkkitStoreIdentity) (*WorkkitJournal, error) {
	if s.Version != WorkkitStoreVersionV2 {
		return nil, errors.New("exact v2 store required")
	}
	return openWorkkitWritableFormat(ctx, s, workkitApplicationIDV2, integrationSchemaSQL)
}
func integrationNotify(tx *sql.Tx, route, kind, subject string, now time.Time) error {
	var count int
	if tx.QueryRow(`SELECT count(*) FROM integration_events`).Scan(&count) != nil || count >= 10000 {
		return errors.New("notification backpressure; preserve obligations")
	}
	_, e := tx.Exec(`INSERT INTO integration_events(route,kind,subject,observed) VALUES(?,?,?,?)`, route, kind, subject, now.UTC().Format(time.RFC3339Nano))
	return e
}
func (j *WorkkitJournal) integration(ctx context.Context, fn func(*sql.Tx) error, now time.Time) error {
	if j.Identity.Version != WorkkitStoreVersionV2 {
		return errors.New("v2 journal required")
	}
	tx, e := j.Box.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = checkAndAdvanceTrustedClock(tx, now); e != nil {
		return e
	}
	if e = fn(tx); e != nil {
		return e
	}
	return tx.Commit()
}

// Claim never takes over an expired worker. Recovery requires independently proven
// quiescence; expiry alone cannot establish that an escaped request stopped.
func (j *WorkkitJournal) ClaimWorker(ctx context.Context, route, owner string, now time.Time, expected ...int64) (int64, error) {
	var fence int64
	err := j.integration(ctx, func(tx *sql.Tx) error {
		if route == "" || owner == "" {
			return errors.New("exact worker identity required")
		}
		var who string
		var end int64
		e := tx.QueryRow(`SELECT owner,fence,until_ns FROM integration_workers WHERE route=?`, route).Scan(&who, &fence, &end)
		if errors.Is(e, sql.ErrNoRows) {
			fence = 1
			_, e = tx.Exec(`INSERT INTO integration_workers VALUES(?,?,?,?)`, route, owner, fence, now.Add(30*time.Second).UnixNano())
			return e
		}
		if e != nil {
			return e
		}
		want := int64(0)
		if len(expected) == 1 {
			want = expected[0]
		}
		if who != owner || end <= now.UnixNano() || want != fence {
			return errors.New("worker ownership or stop proof required; no expiry takeover")
		}
		_, e = tx.Exec(`UPDATE integration_workers SET until_ns=? WHERE route=? AND owner=? AND fence=?`, now.Add(30*time.Second).UnixNano(), route, owner, fence)
		return e
	}, now)
	return fence, err
}
func (j *WorkkitJournal) CheckWorker(ctx context.Context, route, owner string, fence int64, now time.Time) error {
	var who string
	var f, end int64
	e := j.Box.db.QueryRowContext(ctx, `SELECT owner,fence,until_ns FROM integration_workers WHERE route=?`, route).Scan(&who, &f, &end)
	if e != nil || who != owner || f != fence || now.UnixNano() >= end {
		return errors.New("worker fence expired or changed")
	}
	return nil
}
func (j *WorkkitJournal) RecoverWorker(ctx context.Context, route, owner, proofSHA string, prior int64, now time.Time) (int64, error) {
	if len(proofSHA) != 64 {
		return 0, errors.New("protected quiescence receipt required")
	}
	var next int64
	e := j.integration(ctx, func(tx *sql.Tx) error {
		var old int64
		if e := tx.QueryRow(`SELECT fence FROM integration_workers WHERE route=?`, route).Scan(&old); e != nil || old != prior {
			return errors.New("stale recovery fence")
		}
		next = old + 1
		_, e := tx.Exec(`UPDATE integration_workers SET owner=?,fence=?,until_ns=? WHERE route=?`, owner, next, now.Add(30*time.Second).UnixNano(), route)
		if e != nil {
			return e
		}
		return integrationNotify(tx, route, "worker-recovered", proofSHA, now)
	}, now)
	return next, e
}
func (j *WorkkitJournal) IngestHint(ctx context.Context, route, connection, delivery, body string, payload []byte, maxHints, maxBytes int, now time.Time) (string, error) {
	status := "queued"
	var observation struct {
		ReceivedAt string `json:"receivedAt"`
	}
	if json.Unmarshal(payload, &observation) != nil {
		return "", errors.New("hint observation required")
	}
	received, parseErr := time.Parse(time.RFC3339Nano, observation.ReceivedAt)
	if parseErr != nil || received.After(now) {
		return "", errors.New("original hint observation required")
	}
	if len(payload) > 4<<20 || route == "" || connection == "" || delivery == "" || len(body) != 64 || maxHints < 1 || maxHints > 1000 || maxBytes < 1 || maxBytes > 64<<20 {
		return "", errors.New("hint bounds")
	}
	e := j.integration(ctx, func(tx *sql.Tx) error {
		var prior string
		e := tx.QueryRow(`SELECT body FROM integration_inbox WHERE route=? AND connection=? AND delivery=?`, route, connection, delivery).Scan(&prior)
		if e == nil {
			status = "duplicate"
			if prior != body {
				status = "quarantined"
				if _, e = tx.Exec(`UPDATE integration_inbox SET state='quarantined' WHERE route=? AND connection=? AND delivery=?`, route, connection, delivery); e != nil {
					return e
				}
			}
			return integrationNotify(tx, route, status, body, now)
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		var n, size int
		if e = tx.QueryRow(`SELECT coalesce(sum(CASE WHEN state IN ('pending','quarantined') THEN 1 ELSE 0 END),0),coalesce(sum(length(payload)+length(route)+length(connection)+length(delivery)+length(body)),0) FROM integration_inbox`).Scan(&n, &size); e != nil {
			return e
		}
		if n >= maxHints || size+len(payload) > maxBytes {
			return errors.New("inbox backpressure")
		}
		var identical int
		if err := tx.QueryRow(`SELECT count(*) FROM integration_inbox WHERE route=? AND connection=? AND body=? AND state='pending'`, route, connection, body).Scan(&identical); err != nil {
			return err
		}
		state := "pending"
		if identical > 0 {
			state = "coalesced"
			status = "coalesced"
		}
		_, e = tx.Exec(`INSERT INTO integration_inbox VALUES(?,?,?,?,?,?,?)`, route, connection, delivery, body, payload, state, received.UnixNano())
		if e != nil {
			return e
		}
		return integrationNotify(tx, route, "hint-received", body, now)
	}, now)
	return status, e
}

type IntegrationEvent struct {
	Sequence      int64  `json:"sequence"`
	RouteID       string `json:"routeId"`
	Kind          string `json:"kind"`
	SubjectSHA256 string `json:"subjectSha256"`
	ObservedAt    string `json:"observedAt"`
}

func (j *WorkkitJournal) Events(ctx context.Context, consumer string, after int64, now time.Time) ([]IntegrationEvent, error) {
	return j.eventsScoped(ctx, consumer, "", after, now)
}
func (j *WorkkitJournal) EventsForRoute(ctx context.Context, consumer, route string, after int64, now time.Time) ([]IntegrationEvent, error) {
	raw, _ := json.Marshal(struct{ Consumer, Route string }{consumer, route})
	return j.eventsScoped(ctx, DigestBytes(raw), route, after, now)
}
func (j *WorkkitJournal) eventsScoped(ctx context.Context, consumer, route string, after int64, now time.Time) ([]IntegrationEvent, error) {
	result := []IntegrationEvent{}
	if consumer == "" || after < 0 {
		return nil, errors.New("exact consumer/cursor required")
	}
	e := j.integration(ctx, func(tx *sql.Tx) error {
		rows, e := tx.Query(`SELECT sequence,route,kind,subject,observed FROM integration_events WHERE sequence>? AND (?='' OR route=?) ORDER BY sequence LIMIT 1000`, after, route, route)
		if e != nil {
			return e
		}
		for rows.Next() {
			var event IntegrationEvent
			if e = rows.Scan(&event.Sequence, &event.RouteID, &event.Kind, &event.SubjectSHA256, &event.ObservedAt); e != nil {
				rows.Close()
				return e
			}
			result = append(result, event)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		last := after
		if len(result) > 0 {
			last = result[len(result)-1].Sequence
		}
		var maximum int64
		if e = tx.QueryRow(`SELECT coalesce(max(sequence),0) FROM integration_events`).Scan(&maximum); e != nil || last > maximum {
			return errors.New("event cursor beyond journal")
		}
		_, e = tx.Exec(`INSERT INTO integration_consumers VALUES(?,?,?,0) ON CONFLICT(id) DO UPDATE SET delivered=max(delivered,excluded.delivered)`, consumer, route, last)
		return e
	}, now)
	return result, e
}
func (j *WorkkitJournal) AckEvents(ctx context.Context, consumer string, sequence int64, now time.Time) error {
	return j.integration(ctx, func(tx *sql.Tx) error {
		var delivered, ack int64
		if e := tx.QueryRow(`SELECT delivered,acknowledged FROM integration_consumers WHERE id=?`, consumer).Scan(&delivered, &ack); e != nil || sequence < ack || sequence > delivered {
			return errors.New("ack exceeds delivered notifications or regresses")
		}
		_, e := tx.Exec(`UPDATE integration_consumers SET acknowledged=? WHERE id=?`, sequence, consumer)
		return e
	}, now)
}
func (j *WorkkitJournal) PutCheckpoint(ctx context.Context, route, connection string, raw []byte, complete bool, now time.Time) error {
	if len(raw) > 4<<20 || !json.Valid(raw) {
		return errors.New("checkpoint bound")
	}
	return j.integration(ctx, func(tx *sql.Tx) error {
		if !complete {
			var previous []byte
			err := tx.QueryRow(`SELECT payload FROM integration_checkpoints WHERE route=? AND connection=?`, route, connection).Scan(&previous)
			if err == nil {
				var old, next map[string]json.RawMessage
				json.Unmarshal(previous, &old)
				json.Unmarshal(raw, &next)
				if string(old["completedWatermark"]) != string(next["completedWatermark"]) {
					return errors.New("partial capture cannot advance completed watermark")
				}
			}
		}
		_, e := tx.Exec(`INSERT INTO integration_checkpoints VALUES(?,?,?) ON CONFLICT(route,connection) DO UPDATE SET payload=excluded.payload`, route, connection, raw)
		if e != nil {
			return e
		}
		return integrationNotify(tx, route, "coverage-observed", DigestBytes(raw), now)
	}, now)
}

// The existing send-intent transaction also checks the v2 worker and route.
func (o *Outbox) BindWorkkitAdmissionV2(route string, revision int64, plan, id, worker string, fence int64) error {
	if revision < 1 || fence < 1 || worker == "" || route == "" || validateStoreIdentityProfile(o.db, false, workkitApplicationIDV2) != nil {
		return errors.New("exact v2 worker admission required")
	}
	o.workkitGate = func(tx *sql.Tx) error {
		var rev int64
		var raw []byte
		if e := tx.QueryRow(`SELECT revision,payload FROM workkit_routes WHERE id=?`, route).Scan(&rev, &raw); e != nil {
			return e
		}
		var state struct {
			Initialized bool     `json:"initialized"`
			Paused      bool     `json:"paused"`
			Pending     string   `json:"pendingPlanSha256"`
			Unresolved  []string `json:"unresolved"`
		}
		if json.Unmarshal(raw, &state) != nil || rev != revision || !state.Initialized || state.Paused || state.Pending != plan {
			return errors.New("v2 route paused or changed")
		}
		var candidateDigest string
		var candidateRaw []byte
		if tx.QueryRow(`SELECT digest,payload FROM integration_candidates WHERE route=?`, route).Scan(&candidateDigest, &candidateRaw) != nil || candidateDigest != plan {
			return errors.New("current route candidate changed")
		}
		var candidate struct {
			Status    string            `json:"status"`
			Conflicts []json.RawMessage `json:"conflicts"`
		}
		if json.Unmarshal(candidateRaw, &candidate) != nil || candidate.Status != "ready" || len(candidate.Conflicts) > 0 {
			return errors.New("unresolved route conflict blocks sends")
		}
		var quarantined int
		if tx.QueryRow(`SELECT count(*) FROM integration_inbox WHERE route=? AND state='quarantined'`, route).Scan(&quarantined) != nil || quarantined > 0 {
			return errors.New("quarantined delivery conflict blocks route sends")
		}
		included := false
		for _, value := range state.Unresolved {
			included = included || value == id
		}
		var owner string
		var f, until int64
		if tx.QueryRow(`SELECT owner,fence,until_ns FROM integration_workers WHERE route=?`, route).Scan(&owner, &f, &until) != nil || !included || owner != worker || f != fence || time.Now().UnixNano() >= until {
			return errors.New("v2 worker fence or pending operation changed")
		}
		return nil
	}
	return nil
}

func integrationRecordPlan(tx *sql.Tx, route, digest string, raw []byte, now time.Time) error {
	if len(raw) > 4<<20 || DigestBytes(raw) != digest {
		return errors.New("candidate digest/bound")
	}
	if _, err := tx.Exec(`INSERT INTO integration_candidates VALUES(?,?,?) ON CONFLICT(route) DO UPDATE SET digest=excluded.digest,payload=excluded.payload`, route, digest, raw); err != nil {
		return err
	}
	var previous string
	err := tx.QueryRow(`SELECT digest FROM workkit_plans WHERE id=?`, "candidate:"+route+":"+digest).Scan(&previous)
	if err == nil {
		if previous != digest {
			return errors.New("candidate ID conflict")
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if _, err = tx.Exec(`INSERT INTO workkit_plans VALUES(?,?,?,'directional-candidate')`, "candidate:"+route+":"+digest, digest, raw); err != nil {
		return err
	}
	return integrationNotify(tx, route, "candidate-prepared", digest, now)
}
func (j *WorkkitJournal) RecordIntegrationPlan(ctx context.Context, route, digest string, raw []byte, now time.Time) error {
	return j.integration(ctx, func(tx *sql.Tx) error { return integrationRecordPlan(tx, route, digest, raw, now) }, now)
}
func (j *WorkkitJournal) RecordIntegrationRead(ctx context.Context, route, digest string, raw []byte, through time.Time, checkpoints map[string][]byte, now time.Time) error {
	if through.After(now) || through.IsZero() {
		return errors.New("original complete read coverage required")
	}
	return j.integration(ctx, func(tx *sql.Tx) error {
		if err := integrationRecordPlan(tx, route, digest, raw, now); err != nil {
			return err
		}
		for connection, raw := range checkpoints {
			if len(raw) > 4<<20 || !json.Valid(raw) {
				return errors.New("checkpoint bound")
			}
			var previous []byte
			err := tx.QueryRow(`SELECT payload FROM integration_checkpoints WHERE route=? AND connection=?`, route, connection).Scan(&previous)
			if err == nil {
				var a, b map[string]json.RawMessage
				if json.Unmarshal(previous, &a) != nil || json.Unmarshal(raw, &b) != nil {
					return errors.New("invalid checkpoint")
				}
				for _, key := range []string{"routeId", "configurationSha256", "connectionId", "accountSha256", "querySha256", "apiVersion"} {
					if !bytes.Equal(a[key], b[key]) {
						return errors.New("checkpoint identity drift; preserve prior obligations")
					}
				}
				b["completedWatermark"] = a["completedWatermark"]
				raw, _ = json.Marshal(b)
			} else if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			if _, err = tx.Exec(`INSERT INTO integration_checkpoints VALUES(?,?,?) ON CONFLICT(route,connection) DO UPDATE SET payload=excluded.payload`, route, connection, raw); err != nil {
				return err
			}
		}
		for connection := range checkpoints {
			if _, err := tx.Exec(`UPDATE integration_inbox SET state='read-observed',payload='{}' WHERE route=? AND connection=? AND state IN ('pending','coalesced') AND received_ns<=?`, route, connection, through.UnixNano()); err != nil {
				return err
			}
		}
		return integrationNotify(tx, route, "read-coverage-advanced", digest, now)
	}, now)
}
func (j *WorkkitJournal) IntegrationHintCount(ctx context.Context, route string) (int, error) {
	var count int
	err := j.Box.db.QueryRowContext(ctx, `SELECT count(*) FROM integration_inbox WHERE route=? AND state IN ('pending','quarantined')`, route).Scan(&count)
	return count, err
}

// BindIntegrationCandidate checks the latest observed proposal atomically with
// the route-state commit. Only exact reviewed resolution lineage may replace it.
func (j *WorkkitJournal) BindIntegrationCandidate(route, digest, base string, raw []byte) error {
	if j.Identity.Version != WorkkitStoreVersionV2 || DigestBytes(raw) != digest {
		return errors.New("exact v2 candidate required")
	}
	j.integrationCommitGate = func(tx *sql.Tx) error {
		var latest string
		if tx.QueryRow(`SELECT digest FROM integration_candidates WHERE route=?`, route).Scan(&latest) != nil || latest != digest && (base == "" || latest != base) {
			return errors.New("latest observed route proposal changed; fresh review required")
		}
		_, err := tx.Exec(`UPDATE integration_candidates SET digest=?,payload=? WHERE route=?`, digest, raw, route)
		return err
	}
	return nil
}
func (j *WorkkitJournal) IntegrationCandidate(ctx context.Context, route string) (string, []byte, error) {
	var digest string
	var raw []byte
	err := j.Box.db.QueryRowContext(ctx, `SELECT digest,payload FROM integration_candidates WHERE route=?`, route).Scan(&digest, &raw)
	return digest, raw, err
}
func (j *WorkkitJournal) IntegrationNotificationStatus(ctx context.Context, route string) (count int, delivered, ack int64, err error) {
	err = j.Box.db.QueryRowContext(ctx, `SELECT count(*) FROM integration_events WHERE route=?`, route).Scan(&count)
	if err != nil {
		return
	}
	err = j.Box.db.QueryRowContext(ctx, `SELECT coalesce(max(delivered),0),coalesce(min(acknowledged),0) FROM integration_consumers WHERE route=?`, route).Scan(&delivered, &ack)
	return
}
