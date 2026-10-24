// Package store is a runtime-owned, single-local-authority reference accounting
// store. It does not execute agents or reserve allowance at the provider.
package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
	_ "modernc.org/sqlite"
)

var ErrConflict = errors.New("accounting conflict; retry the same request explicitly")
var ErrBlocked = errors.New("capacity admission blocked")
var identifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

type Store struct {
	db        *sql.DB
	authority string
	path      string
}

// AccountingStore belongs to the calling runtime; tokens grant no execution authority.
type AccountingStore interface {
	ReadSnapshot(context.Context) (State, error)
	Ingest(context.Context, string, usage.Export, usage.Policy, time.Time) (Result, error)
	Reserve(context.Context, ReserveRequest, usage.Policy, time.Time) (Result, error)
	Renew(context.Context, Transition, usage.Policy, time.Time) (Result, error)
	ConfirmLaunch(context.Context, Transition, usage.Policy, time.Time) (Result, error)
	ReleaseUnlaunched(context.Context, Transition, usage.Policy, time.Time) (Result, error)
	MarkUncertain(context.Context, Transition, usage.Policy, time.Time) (Result, error)
	Settle(context.Context, Transition, usage.Policy, time.Time) (Result, error)
	Reconcile(context.Context, Transition, usage.Policy, time.Time) (Result, error)
	Recover(context.Context, string, string, usage.Policy, time.Time) (Result, error)
	RecoverReservation(context.Context, string, Token, string, usage.Policy, time.Time) (Result, error)
	Close() error
}

var _ AccountingStore = (*Store)(nil)

type State struct {
	Epoch  string        `json:"epoch"`
	LastAt string        `json:"lastAt"`
	Export usage.Export  `json:"export"`
	Policy *usage.Policy `json:"policy"`
}
type ReserveRequest struct {
	Key              string       `json:"key"`
	ID               string       `json:"id"`
	Owner            string       `json:"owner"`
	AttemptID        string       `json:"attemptId"`
	ExpectedRevision int64        `json:"expectedRevision"`
	LeaseUntil       string       `json:"leaseUntil"`
	Demand           usage.Demand `json:"demand"`
}
type Token struct {
	ID    string `json:"id"`
	Owner string `json:"owner"`
	Epoch string `json:"epoch"`
	Fence int64  `json:"fence"`
}
type Transition struct {
	Key           string         `json:"key"`
	Token         Token          `json:"token"`
	LeaseUntil    string         `json:"leaseUntil,omitempty"`
	NewOwner      string         `json:"newOwner,omitempty"`
	Actual        []usage.Amount `json:"actual,omitempty"`
	ProfileSHA256 string         `json:"profileSha256,omitempty"`
}
type Result struct {
	Revision    int64              `json:"revision"`
	Reservation *usage.Reservation `json:"reservation"`
}

// Open requires a dedicated real local directory. Authority is an opaque runtime
// identity; opening an existing store never takes it over implicitly.
func Open(path, authority string) (*Store, error) {
	if !identifier.MatchString(authority) {
		return nil, errors.New("invalid authority")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	for p := filepath.Dir(abs); ; p = filepath.Dir(p) {
		info, e := os.Lstat(p)
		if e != nil {
			return nil, e
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("store ancestors must be real directories")
		}
		if filepath.Dir(p) == p {
			break
		}
	}
	if err = secureDirectory(filepath.Dir(abs)); err != nil {
		return nil, err
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		info, e := os.Lstat(abs + suffix)
		if errors.Is(e, os.ErrNotExist) {
			continue
		}
		if e != nil {
			return nil, e
		}
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("store and sidecars must be regular files")
		}
		if err = secureFile(abs + suffix); err != nil {
			return nil, err
		}
	}
	f, e := os.OpenFile(abs, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if e == nil {
		if err = f.Close(); err != nil {
			return nil, err
		}
	} else if !errors.Is(e, os.ErrExist) {
		return nil, e
	}
	if err = secureFile(abs); err != nil {
		return nil, err
	}
	dsn := (&url.URL{Scheme: "file", Path: filepath.ToSlash(abs)}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db, authority: authority, path: abs}
	ok := false
	defer func() {
		if !ok {
			db.Close()
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err = db.ExecContext(ctx, `PRAGMA busy_timeout=5000; PRAGMA foreign_keys=ON; PRAGMA journal_mode=WAL; PRAGMA synchronous=FULL;`); err != nil {
		return nil, classify(err)
	}
	var schema int
	if err = db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&schema); err != nil {
		return nil, err
	}
	if schema != 0 && schema != 1 {
		return nil, errors.New("unsupported accounting database version")
	}
	_, err = db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS state(id INTEGER PRIMARY KEY CHECK(id=1),authority TEXT NOT NULL,payload BLOB NOT NULL,digest TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS operations(key TEXT PRIMARY KEY,request_digest TEXT NOT NULL,result BLOB NOT NULL);
CREATE TABLE IF NOT EXISTS audit(sequence INTEGER PRIMARY KEY AUTOINCREMENT,operation TEXT UNIQUE NOT NULL,state_digest TEXT NOT NULL,previous_digest TEXT NOT NULL,payload BLOB NOT NULL,request_digest TEXT NOT NULL,result_digest TEXT NOT NULL);
PRAGMA user_version=1;`)
	if err != nil {
		return nil, classify(err)
	}
	random := make([]byte, 16)
	if _, err = rand.Read(random); err != nil {
		return nil, err
	}
	state := State{Epoch: hex.EncodeToString(random), Export: usage.EmptyExport()}
	b, _ := json.Marshal(state)
	if _, err = db.ExecContext(ctx, "INSERT OR IGNORE INTO state VALUES(1,?,?,?)", authority, b, usage.Hash(b)); err != nil {
		return nil, classify(err)
	}
	if _, err = s.ReadSnapshot(ctx); err != nil {
		return nil, err
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if _, e := os.Lstat(abs + suffix); e == nil {
			if err = secureFile(abs + suffix); err != nil {
				return nil, err
			}
		}
	}
	ok = true
	return s, nil
}
func (s *Store) Close() error { return s.db.Close() }
func classify(err error) error {
	if err != nil && (strings.Contains(err.Error(), "locked") || strings.Contains(err.Error(), "busy") || errors.Is(err, context.DeadlineExceeded)) {
		return fmt.Errorf("%w: contention deadline", ErrConflict)
	}
	return err
}
func (s *Store) ReadSnapshot(ctx context.Context) (State, error) {
	var state State
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return state, classify(err)
	}
	defer tx.Rollback()
	var authority, digest string
	var b []byte
	if err := tx.QueryRowContext(ctx, "SELECT authority,payload,digest FROM state WHERE id=1").Scan(&authority, &b, &digest); err != nil {
		return state, err
	}
	if authority != s.authority || usage.Hash(b) != digest {
		return state, errors.New("store authority or integrity mismatch")
	}
	if err := usage.Decode(b, &state); err != nil {
		return state, err
	}
	if state.Export.Version != usage.Version || state.Export.LedgerRevision < 0 || state.Epoch == "" {
		return state, errors.New("invalid accounting state")
	}
	if state.Policy != nil {
		if err = usage.ValidateExport(state.Export, *state.Policy); err != nil {
			return state, err
		}
	}
	rows, err := tx.QueryContext(ctx, "SELECT state_digest,previous_digest,payload FROM audit ORDER BY sequence")
	if err != nil {
		return state, err
	}
	defer rows.Close()
	previous := ""
	count := int64(0)
	for rows.Next() {
		var current, prior string
		var payload []byte
		if err = rows.Scan(&current, &prior, &payload); err != nil {
			return state, err
		}
		if usage.Hash(payload) != current || previous != "" && prior != previous {
			return state, errors.New("accounting audit chain integrity mismatch")
		}
		previous = current
		count++
	}
	if err = rows.Err(); err != nil {
		return state, err
	}
	if count != state.Export.LedgerRevision || count > 0 && previous != digest {
		return state, errors.New("accounting projection does not match audit log")
	}
	if err = rows.Close(); err != nil {
		return state, err
	}
	if err = verifyAudit(ctx, tx, state, digest); err != nil {
		return state, err
	}
	return state, tx.Commit()
}

func (s *Store) mutate(ctx context.Context, key string, request any, at time.Time, fn func(*State) (Result, error)) (result Result, err error) {
	if !identifier.MatchString(key) || at.IsZero() {
		return result, errors.New("operation requires safe idempotency key and time")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return result, classify(err)
	}
	defer conn.Close()
	if _, err = conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return result, classify(err)
	}
	defer func() {
		if err != nil {
			_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
		}
	}()
	keyHash := usage.Hash([]byte(key))
	requestHash := usage.Digest(request)
	var bootRaw []byte
	var bootAuthority, bootDigest string
	if err = conn.QueryRowContext(ctx, "SELECT authority,payload,digest FROM state WHERE id=1").Scan(&bootAuthority, &bootRaw, &bootDigest); err != nil {
		return result, err
	}
	if bootAuthority != s.authority || usage.Hash(bootRaw) != bootDigest {
		return result, errors.New("store integrity or authority mismatch")
	}
	var bootState State
	if err = usage.Decode(bootRaw, &bootState); err != nil {
		return result, err
	}
	if bootState.Policy != nil {
		if err = usage.ValidateExport(bootState.Export, *bootState.Policy); err != nil {
			return result, err
		}
	}
	if err = verifyAudit(ctx, conn, bootState, bootDigest); err != nil {
		return result, err
	}
	var savedHash string
	var saved []byte
	e := conn.QueryRowContext(ctx, "SELECT request_digest,result FROM operations WHERE key=?", keyHash).Scan(&savedHash, &saved)
	if e == nil {
		if requestHash != savedHash {
			return result, fmt.Errorf("%w: idempotency payload changed", ErrConflict)
		}
		if err = json.Unmarshal(saved, &result); err != nil {
			return result, err
		}
		_, err = conn.ExecContext(ctx, "COMMIT")
		return result, classify(err)
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return result, e
	}
	var authority, digest string
	var raw []byte
	if err = conn.QueryRowContext(ctx, "SELECT authority,payload,digest FROM state WHERE id=1").Scan(&authority, &raw, &digest); err != nil {
		return result, err
	}
	if authority != s.authority || usage.Hash(raw) != digest {
		return result, errors.New("store integrity or authority mismatch")
	}
	var state State
	if err = usage.Decode(raw, &state); err != nil {
		return result, err
	}
	if state.LastAt != "" {
		last, _ := time.Parse(time.RFC3339Nano, state.LastAt)
		if at.Before(last) {
			return result, errors.New("clock moved backwards")
		}
	}
	result, err = fn(&state)
	if err != nil {
		return result, err
	}
	state.LastAt = at.UTC().Format(time.RFC3339Nano)
	state.Export.LedgerRevision++
	result.Revision = state.Export.LedgerRevision
	b, err := json.Marshal(state)
	if err != nil {
		return result, err
	}
	if len(b) > 4<<20 {
		return result, errors.New("accounting snapshot exceeds bounded store size")
	}
	if _, err = conn.ExecContext(ctx, "UPDATE state SET payload=?,digest=? WHERE id=1", b, usage.Hash(b)); err != nil {
		return result, classify(err)
	}
	resultBytes, _ := json.Marshal(result)
	if _, err = conn.ExecContext(ctx, "INSERT INTO operations VALUES(?,?,?)", keyHash, requestHash, resultBytes); err != nil {
		return result, classify(err)
	}
	if _, err = conn.ExecContext(ctx, "INSERT INTO audit(operation,state_digest,previous_digest,payload,request_digest,result_digest) VALUES(?,?,?,?,?,?)", keyHash, usage.Hash(b), digest, b, requestHash, usage.Hash(resultBytes)); err != nil {
		return result, classify(err)
	}
	_, err = conn.ExecContext(ctx, "COMMIT")
	return result, classify(err)
}

func (s *Store) Ingest(ctx context.Context, key string, e usage.Export, p usage.Policy, at time.Time) (Result, error) {
	if err := usage.ValidatePolicy(p); err != nil {
		return Result{}, err
	}
	if e.PolicySHA256 != usage.PolicyHash(p) {
		return Result{}, errors.New("ingestion requires matching trusted policy digest")
	}
	return s.mutate(ctx, key, struct {
		Export usage.Export
		Policy usage.Policy
	}{e, p}, at, func(state *State) (Result, error) {
		if len(e.Reservations) > 0 || len(e.Charges) > 0 {
			return Result{}, errors.New("ingestion cannot forge runtime charges or reservations")
		}
		merged := state.Export
		if len(merged.Bindings) == 0 {
			merged.Bindings = append([]usage.Binding{}, e.Bindings...)
		} else if usage.Digest(merged.Bindings) != usage.Digest(e.Bindings) {
			return Result{}, fmt.Errorf("%w: binding changes require a new authority/store", ErrConflict)
		}
		if merged.PolicySHA256 != "" && merged.PolicySHA256 != e.PolicySHA256 {
			return Result{}, fmt.Errorf("%w: policy changed; existing commitments require explicit migration", ErrConflict)
		}
		merged.PolicySHA256 = e.PolicySHA256
		merged.Events = append(append([]usage.Event{}, merged.Events...), e.Events...)
		var ds []usage.Diagnostic
		merged.Events, ds = usage.NormalizeEvents(merged.Events)
		if len(ds) > 0 {
			return Result{}, fmt.Errorf("%w: usage event ambiguity", ErrConflict)
		}
		quotas := map[string]usage.Quota{}
		for _, q := range append(append([]usage.Quota{}, merged.Quotas...), e.Quotas...) {
			if old, ok := quotas[q.ID]; ok && usage.Digest(old) != usage.Digest(q) {
				return Result{}, fmt.Errorf("%w: quota identity changed", ErrConflict)
			}
			quotas[q.ID] = q
		}
		merged.Quotas = []usage.Quota{}
		for _, q := range quotas {
			merged.Quotas = append(merged.Quotas, q)
		}
		sortExport(&merged)
		if err := usage.ValidateExport(merged, p); err != nil {
			return Result{}, err
		}
		state.Export = merged
		state.Policy = &p
		return Result{}, nil
	})
}

func (s *Store) Reserve(ctx context.Context, r ReserveRequest, p usage.Policy, at time.Time) (Result, error) {
	if !identifier.MatchString(r.ID) || !identifier.MatchString(r.Owner) || !identifier.MatchString(r.AttemptID) {
		return Result{}, errors.New("invalid reservation identity")
	}
	if err := usage.ValidatePolicy(p); err != nil {
		return Result{}, err
	}
	if err := usage.ValidateDemand(r.Demand); err != nil {
		return Result{}, err
	}
	until, err := time.Parse(time.RFC3339Nano, r.LeaseUntil)
	if err != nil || !until.After(at) {
		return Result{}, errors.New("lease must expire in the future")
	}
	demandUntil, _ := time.Parse(time.RFC3339Nano, r.Demand.ValidUntil)
	if until.After(demandUntil) {
		return Result{}, errors.New("lease cannot outlive demand validity")
	}
	return s.mutate(ctx, r.Key, struct {
		Request ReserveRequest
		Policy  usage.Policy
	}{r, p}, at, func(state *State) (Result, error) {
		if state.Export.LedgerRevision != r.ExpectedRevision {
			return Result{}, ErrConflict
		}
		if state.Export.PolicySHA256 != usage.PolicyHash(p) {
			return Result{}, fmt.Errorf("%w: policy migration required", ErrConflict)
		}
		for _, old := range state.Export.Reservations {
			if old.ID == r.ID {
				return Result{}, ErrConflict
			}
		}
		cap := usage.Capacity(state.Export, p, r.Demand.ConfigurationID, &r.Demand, at)
		if cap.Disposition != "observed-fit" || cap.Status != "valid" {
			return Result{}, fmt.Errorf("%w: %s", ErrBlocked, cap.Disposition)
		}
		hold := usage.Reservation{ID: r.ID, Authority: s.authority, AuthorityEpoch: state.Epoch, Owner: r.Owner, AttemptID: r.AttemptID, ConfigurationID: r.Demand.ConfigurationID, PolicySHA256: usage.PolicyHash(p), SnapshotSHA256: cap.Digest, ProfileSHA256: r.Demand.ProfileSHA256, DemandSHA256: usage.Digest(r.Demand), DemandValidUntil: r.Demand.ValidUntil, Fence: 1, LeaseUntil: r.LeaseUntil, State: "held", Amounts: append([]usage.Amount{}, r.Demand.Amounts...)}
		state.Export.Reservations = append(state.Export.Reservations, hold)
		return Result{Reservation: &hold}, nil
	})
}

func (s *Store) transition(ctx context.Context, op string, r Transition, p usage.Policy, at time.Time) (Result, error) {
	if !identifier.MatchString(r.Token.ID) || !identifier.MatchString(r.Token.Owner) || r.Token.Fence < 1 {
		return Result{}, errors.New("invalid reservation token")
	}
	if err := usage.ValidatePolicy(p); err != nil {
		return Result{}, err
	}
	return s.mutate(ctx, r.Key, struct {
		Operation string
		Request   Transition
		Policy    usage.Policy
	}{op, r, p}, at, func(state *State) (Result, error) {
		var hold *usage.Reservation
		for i := range state.Export.Reservations {
			if state.Export.Reservations[i].ID == r.Token.ID {
				hold = &state.Export.Reservations[i]
				break
			}
		}
		if hold == nil || state.Epoch != r.Token.Epoch || hold.AuthorityEpoch != r.Token.Epoch || hold.Owner != r.Token.Owner || hold.Fence != r.Token.Fence {
			return Result{}, ErrConflict
		}
		if hold.PolicySHA256 != usage.PolicyHash(p) || state.Export.PolicySHA256 != usage.PolicyHash(p) {
			return Result{}, fmt.Errorf("%w: policy migration required", ErrConflict)
		}
		until, _ := time.Parse(time.RFC3339Nano, hold.LeaseUntil)
		alive := at.Before(until)
		switch op {
		case "renew":
			if !alive || hold.State != "held" && hold.State != "in-flight" {
				return Result{}, ErrConflict
			}
			next, e := time.Parse(time.RFC3339Nano, r.LeaseUntil)
			if e != nil || !next.After(until) {
				return Result{}, errors.New("renewal must extend lease")
			}
			validUntil, _ := time.Parse(time.RFC3339Nano, hold.DemandValidUntil)
			if next.After(validUntil) {
				return Result{}, errors.New("lease cannot outlive original demand")
			}
			hold.LeaseUntil = r.LeaseUntil
		case "launch":
			if r.ProfileSHA256 != hold.ProfileSHA256 {
				return Result{}, errors.New("launch must bind exact reserved profile digest")
			}
			if !alive || hold.State != "held" || hold.PolicySHA256 != usage.PolicyHash(p) {
				return Result{}, ErrConflict
			}
			copy := state.Export
			copy.Reservations = append([]usage.Reservation{}, state.Export.Reservations...)
			for i := range copy.Reservations {
				if copy.Reservations[i].ID == hold.ID {
					copy.Reservations[i].State = "released"
				}
			}
			demand := usage.Demand{Version: usage.Version, ConfigurationID: hold.ConfigurationID, Source: "reservation", ValidUntil: hold.DemandValidUntil, ProfileSHA256: hold.ProfileSHA256, Amounts: hold.Amounts}
			cap := usage.Capacity(copy, p, hold.ConfigurationID, &demand, at)
			if cap.Disposition != "observed-fit" || cap.Status != "valid" {
				return Result{}, fmt.Errorf("%w: %s", ErrBlocked, cap.Disposition)
			}
			hold.State = "in-flight"
		case "release":
			if hold.State != "held" {
				return Result{}, errors.New("only an unlaunched hold can be released")
			}
			hold.State = "released"
		case "uncertain":
			if hold.State != "in-flight" {
				return Result{}, ErrConflict
			}
			hold.State = "uncertain"
		case "settle", "reconcile":
			// A run spanning a reset may consume the new window too. Until the
			// runtime can supply explicit cross-window allocation evidence, an old
			// generation settlement must not silently free its uncertain hold.
			for _, amount := range hold.Amounts {
				latest := map[string]int64{}
				for _, q := range state.Export.Quotas {
					if q.Constraint.ID == amount.ConstraintID {
						key := usage.Digest(struct{ Source, Host, Version, Auth string }{q.Origin.SourceID, q.Origin.Host, q.Origin.HostVersion, q.Origin.AuthMode})
						if old, ok := latest[key]; !ok || q.Revision > old {
							latest[key] = q.Revision
						}
					}
				}
				for _, q := range state.Export.Quotas {
					key := usage.Digest(struct{ Source, Host, Version, Auth string }{q.Origin.SourceID, q.Origin.Host, q.Origin.HostVersion, q.Origin.AuthMode})
					if q.Constraint.ID == amount.ConstraintID && q.Revision == latest[key] && q.Generation != amount.Generation {
						return Result{}, fmt.Errorf("%w: cross-window allocation evidence is required; hold retained", ErrConflict)
					}
				}
			}
			if op == "settle" && !alive {
				return Result{}, ErrConflict
			}
			if hold.State != "in-flight" && hold.State != "uncertain" {
				return Result{}, ErrConflict
			}
			if len(r.Actual) != len(hold.Amounts) {
				return Result{}, errors.New("actual amounts must cover every reserved constraint")
			}
			seen := map[string]bool{}
			for _, actual := range r.Actual {
				var matched bool
				for _, amount := range hold.Amounts {
					if actual.ConstraintID == amount.ConstraintID && actual.Generation == amount.Generation && actual.Unit == amount.Unit {
						matched = true
					}
				}
				if !matched || seen[actual.ConstraintID] {
					return Result{}, errors.New("actual amount scope mismatch")
				}
				seen[actual.ConstraintID] = true
				v, e := usage.NumberRat(actual.Amount)
				if e != nil || v.Sign() < 0 {
					return Result{}, errors.New("actual amount must be nonnegative")
				}
				id := usage.Digest(struct{ Reservation, Constraint, Generation string }{hold.ID, actual.ConstraintID, actual.Generation})
				rid := hold.ID
				state.Export.Charges = append(state.Export.Charges, usage.Charge{ID: id, ConstraintID: actual.ConstraintID, Generation: actual.Generation, Unit: actual.Unit, Amount: actual.Amount, ReservationID: &rid})
			}
			hold.State = "settled"
		default:
			return Result{}, errors.New("unknown reservation operation")
		}
		value := *hold
		return Result{Reservation: &value}, nil
	})
}
func (s *Store) Renew(ctx context.Context, r Transition, p usage.Policy, at time.Time) (Result, error) {
	return s.transition(ctx, "renew", r, p, at)
}
func (s *Store) ConfirmLaunch(ctx context.Context, r Transition, p usage.Policy, at time.Time) (Result, error) {
	return s.transition(ctx, "launch", r, p, at)
}
func (s *Store) ReleaseUnlaunched(ctx context.Context, r Transition, p usage.Policy, at time.Time) (Result, error) {
	return s.transition(ctx, "release", r, p, at)
}
func (s *Store) MarkUncertain(ctx context.Context, r Transition, p usage.Policy, at time.Time) (Result, error) {
	return s.transition(ctx, "uncertain", r, p, at)
}
func (s *Store) Settle(ctx context.Context, r Transition, p usage.Policy, at time.Time) (Result, error) {
	return s.transition(ctx, "settle", r, p, at)
}
func (s *Store) Reconcile(ctx context.Context, r Transition, p usage.Policy, at time.Time) (Result, error) {
	return s.transition(ctx, "reconcile", r, p, at)
}

// Recover explicitly takes over expired reservations, fencing only their old
// owners. A running/uncertain hold is retained; it is never freed by lease expiry.
func (s *Store) Recover(ctx context.Context, key, newOwner string, p usage.Policy, at time.Time) (Result, error) {
	if !identifier.MatchString(newOwner) {
		return Result{}, errors.New("invalid recovery owner")
	}
	return s.mutate(ctx, key, struct {
		Operation, Owner string
		Policy           usage.Policy
	}{"recover", newOwner, p}, at, func(state *State) (Result, error) {
		for i := range state.Export.Reservations {
			r := &state.Export.Reservations[i]
			until, _ := time.Parse(time.RFC3339Nano, r.LeaseUntil)
			if !at.Before(until) && (r.State == "held" || r.State == "in-flight" || r.State == "uncertain") {
				if r.Fence == 1<<63-1 {
					return Result{}, errors.New("reservation fence exhausted")
				}
				r.Fence++
				r.Owner = newOwner
				if r.State == "held" {
					r.State = "released"
				} else {
					r.State = "uncertain"
				}
			}
		}
		if err := usage.ValidateExport(state.Export, p); err != nil {
			return Result{}, err
		}
		return Result{}, nil
	})
}

// RecoverReservation takes over exactly one expired reservation. A held hold is
// released; an in-flight or uncertain hold stays uncertain because expiry does
// not prove the host stopped or that no usage occurred.
func (s *Store) RecoverReservation(ctx context.Context, key string, old Token, newOwner string, p usage.Policy, at time.Time) (Result, error) {
	if !identifier.MatchString(old.ID) || !identifier.MatchString(old.Owner) || !identifier.MatchString(newOwner) || old.Fence < 1 {
		return Result{}, errors.New("invalid reservation recovery token")
	}
	if err := usage.ValidatePolicy(p); err != nil {
		return Result{}, err
	}
	return s.mutate(ctx, key, struct {
		Operation string
		Token     Token
		NewOwner  string
		Policy    usage.Policy
	}{"recover-reservation", old, newOwner, p}, at, func(state *State) (Result, error) {
		if state.Policy == nil || state.Export.PolicySHA256 != usage.PolicyHash(p) || usage.Digest(*state.Policy) != usage.Digest(p) {
			return Result{}, fmt.Errorf("%w: policy migration required", ErrConflict)
		}
		for i := range state.Export.Reservations {
			hold := &state.Export.Reservations[i]
			if hold.ID != old.ID {
				continue
			}
			if state.Epoch != old.Epoch || hold.AuthorityEpoch != old.Epoch || hold.Owner != old.Owner || hold.Fence != old.Fence {
				return Result{}, ErrConflict
			}
			until, err := time.Parse(time.RFC3339Nano, hold.LeaseUntil)
			if err != nil {
				return Result{}, errors.New("invalid reservation lease")
			}
			if at.Before(until) {
				return Result{}, ErrConflict
			}
			if hold.State != "held" && hold.State != "in-flight" && hold.State != "uncertain" {
				return Result{}, ErrConflict
			}
			if hold.Fence == 1<<63-1 {
				return Result{}, errors.New("reservation fence exhausted")
			}
			hold.Fence++
			hold.Owner = newOwner
			if hold.State == "held" {
				hold.State = "released"
			} else {
				hold.State = "uncertain"
			}
			copy := *hold
			return Result{Reservation: &copy}, nil
		}
		return Result{}, ErrConflict
	})
}
