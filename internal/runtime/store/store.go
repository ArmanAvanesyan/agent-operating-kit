// Package store implements the private, local durable runtime ledger.
package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/runtime/records"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/runtime/staging"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/verification"
	_ "modernc.org/sqlite"
)

var ErrConflict = errors.New("runtime ledger conflict")
var ErrBlocked = errors.New("runtime operation blocked")

type Store struct {
	db   *sql.DB
	root string
}

type diskState struct {
	State                  records.State                   `json:"state"`
	Policy                 records.Policy                  `json:"policy"`
	Fences                 map[string]int64                `json:"fences"`
	Acceptance             map[string]acceptanceProjection `json:"-"`
	TrustGeneration        int64                           `json:"-"`
	RevocationGeneration   int64                           `json:"-"`
	AcceptancePolicySHA256 string                          `json:"-"`
}

type acceptanceProjection struct {
	Candidate, Snapshot, Input, Attempt string
	ValidUntil                          time.Time
	Fence, Trust, Revocation            int64
	PolicySHA                           string
	Active                              bool
}

func digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func marshal(v any) []byte   { b, _ := json.Marshal(v); return b }
func snapshotDigest(xs []records.Asset) string {
	copy := append([]records.Asset{}, xs...)
	sort.Slice(copy, func(i, j int) bool { return copy[i].Path < copy[j].Path })
	type snapshotAsset struct {
		Path       string
		SHA        string
		Executable bool
	}
	projection := make([]snapshotAsset, len(copy))
	for i, asset := range copy {
		projection[i] = snapshotAsset{Path: asset.Path, SHA: asset.SHA256, Executable: asset.Executable}
	}
	return usage.Digest(projection)
}
func classify(e error) error {
	if e != nil && (strings.Contains(strings.ToLower(e.Error()), "locked") || strings.Contains(strings.ToLower(e.Error()), "busy")) {
		return fmt.Errorf("%w: %v", ErrConflict, e)
	}
	return e
}

// Create makes a fresh private root and initializes schema version 1.
func Create(root string, policy records.Policy, policySHA string) (*Store, error) {
	if root == "" || policy.Authority == "" || policySHA == "" || policy.MaxConcurrent < 1 {
		return nil, errors.New("invalid runtime root or policy")
	}
	abs, e := filepath.Abs(root)
	if e != nil {
		return nil, e
	}
	if e = realAncestors(abs); e != nil {
		return nil, e
	}
	if e = os.Mkdir(abs, 0700); e != nil {
		return nil, e
	}
	return initialize(abs, &policy, policySHA)
}

// Open attaches to an existing store without changing its policy or epoch.
func Open(root string) (*Store, error) {
	abs, e := filepath.Abs(root)
	if e != nil {
		return nil, e
	}
	if e = realAncestors(abs); e != nil {
		return nil, e
	}
	return initialize(abs, nil, "")
}

func realAncestors(path string) error {
	for p := path; ; p = filepath.Dir(p) {
		i, e := os.Lstat(p)
		if errors.Is(e, os.ErrNotExist) && p == path {
			p = filepath.Dir(p)
			i, e = os.Lstat(p)
		}
		if e != nil {
			return e
		}
		if i.Mode()&os.ModeSymlink != 0 {
			return errors.New("runtime path ancestors must not be symlinks")
		}
		if p == filepath.Dir(p) {
			return nil
		}
	}
}

func initialize(root string, policy *records.Policy, policySHA string) (*Store, error) {
	fi, e := os.Lstat(root)
	if e != nil {
		return nil, e
	}
	if !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("runtime root must be a real directory")
	}
	if e = secureDirectory(root); e != nil {
		return nil, e
	}
	path := filepath.Join(root, "runtime.sqlite")
	if fi, e = os.Lstat(path); e == nil {
		if !fi.Mode().IsRegular() || fi.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("runtime database must be a regular file")
		}
		if e = secureFile(path); e != nil {
			return nil, e
		}
	} else if !errors.Is(e, os.ErrNotExist) {
		return nil, e
	}
	if e = checkSidecars(path); e != nil {
		return nil, e
	}
	dbfile, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if e == nil {
		e = dbfile.Close()
	} else if !errors.Is(e, os.ErrExist) {
		return nil, e
	} else {
		e = nil
	}
	if e != nil {
		return nil, e
	}
	dsn := (&url.URL{Scheme: "file", Path: filepath.ToSlash(path)}).String()
	db, e := sql.Open("sqlite", dsn)
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db, root: root}
	ok := false
	defer func() {
		if !ok {
			db.Close()
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, e = db.ExecContext(ctx, `PRAGMA busy_timeout=5000; PRAGMA foreign_keys=ON; PRAGMA journal_mode=WAL; PRAGMA synchronous=FULL;`); e != nil {
		return nil, e
	}
	var version int
	if e = db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); e != nil {
		return nil, e
	}
	if version != 0 && version != 1 && version != 2 {
		return nil, errors.New("unsupported runtime database version")
	}
	_, e = db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS state(id INTEGER PRIMARY KEY CHECK(id=1),payload BLOB NOT NULL,digest TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS operations(key TEXT PRIMARY KEY,request TEXT NOT NULL,result BLOB NOT NULL);
CREATE TABLE IF NOT EXISTS audit(sequence INTEGER PRIMARY KEY AUTOINCREMENT,payload BLOB NOT NULL,digest TEXT NOT NULL,previous TEXT NOT NULL);`)
	if e != nil {
		return nil, e
	}
	if version == 0 {
		if _, e = db.ExecContext(ctx, "PRAGMA user_version=1"); e != nil {
			return nil, e
		}
	}
	if policy != nil {
		var n int
		e = db.QueryRowContext(ctx, "SELECT count(*) FROM state").Scan(&n)
		if e != nil {
			return nil, e
		}
		if n != 0 {
			return nil, errors.New("runtime root already initialized")
		}
		random := make([]byte, 16)
		if _, e = rand.Read(random); e != nil {
			return nil, e
		}
		d := diskState{State: records.State{Version: records.Version, LifecycleVersion: records.LifecycleVersion, Authority: policy.Authority, Epoch: hex.EncodeToString(random), PolicySHA256: policySHA, Revision: 0, Runs: []records.Run{}, Events: []records.Event{}}, Policy: *policy, Fences: map[string]int64{}}
		b := marshal(d)
		if _, e = db.ExecContext(ctx, "INSERT INTO state VALUES(1,?,?)", b, digest(b)); e != nil {
			return nil, e
		}
	}
	if _, e = s.Snapshot(ctx); e != nil {
		return nil, e
	}
	if e = checkSidecars(path); e != nil {
		return nil, e
	}
	ok = true
	return s, nil
}

// EnableAcceptanceProjection explicitly migrates the runtime database to schema
// version 2. Opening a store never enables or migrates acceptance implicitly.
func (s *Store) EnableAcceptanceProjection(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return classify(err)
	}
	defer tx.Rollback()
	var version int
	if err = tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version == 2 {
		return tx.Commit()
	}
	if version != 1 {
		return errors.New("runtime acceptance migration requires schema version 1")
	}
	_, err = tx.ExecContext(ctx, `CREATE TABLE acceptance_projection(run_id TEXT NOT NULL, step_id TEXT NOT NULL, receipt_sha256 TEXT NOT NULL, policy_sha256 TEXT NOT NULL, candidate_sha256 TEXT NOT NULL, snapshot_sha256 TEXT NOT NULL, input_sha256 TEXT NOT NULL, attempt_id TEXT NOT NULL, fence INTEGER NOT NULL, valid_until TEXT NOT NULL, trust_generation INTEGER NOT NULL, revocation_generation INTEGER NOT NULL, active INTEGER NOT NULL CHECK(active IN (0,1)), PRIMARY KEY(run_id,step_id));
CREATE TABLE acceptance_generation(id INTEGER PRIMARY KEY CHECK(id=1),trust_generation INTEGER NOT NULL,revocation_generation INTEGER NOT NULL,policy_sha256 TEXT NOT NULL);
CREATE TABLE acceptance_revocations(receipt_sha256 TEXT PRIMARY KEY,revoked_at TEXT NOT NULL);
CREATE TABLE acceptance_audit(sequence INTEGER PRIMARY KEY AUTOINCREMENT, run_id TEXT NOT NULL, step_id TEXT NOT NULL, receipt_sha256 TEXT NOT NULL, trust_generation INTEGER NOT NULL, revocation_generation INTEGER NOT NULL, active INTEGER NOT NULL, at TEXT NOT NULL);`)
	if err != nil {
		return classify(err)
	}
	if _, err = tx.ExecContext(ctx, "PRAGMA user_version=2"); err != nil {
		return classify(err)
	}
	return classify(tx.Commit())
}

// SetAcceptanceAuthorityGenerations pins the exact externally verified policy
// digest alongside monotonically advancing protected generations.
func (s *Store) SetAcceptanceAuthorityGenerations(ctx context.Context, trustGeneration, revocationGeneration int64, policySHA string) error {
	if trustGeneration < 1 || revocationGeneration < 1 || policySHA == "" {
		return errors.New("acceptance generations must be positive")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return classify(err)
	}
	defer tx.Rollback()
	var version int
	if err = tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version != 2 {
		return errors.New("acceptance projection is not enabled")
	}
	var oldTrust, oldRev int64
	var oldPolicy string
	err = tx.QueryRowContext(ctx, "SELECT trust_generation,revocation_generation,policy_sha256 FROM acceptance_generation WHERE id=1").Scan(&oldTrust, &oldRev, &oldPolicy)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return classify(err)
	}
	if errors.Is(err, sql.ErrNoRows) {
		_, err = tx.ExecContext(ctx, "INSERT INTO acceptance_generation(id,trust_generation,revocation_generation,policy_sha256) VALUES(1,?,?,?)", trustGeneration, revocationGeneration, policySHA)
	} else {
		if trustGeneration < oldTrust || revocationGeneration < oldRev {
			return ErrConflict
		}
		if trustGeneration == oldTrust && revocationGeneration == oldRev && oldPolicy != policySHA {
			return ErrConflict
		}
		if trustGeneration == oldTrust && revocationGeneration == oldRev {
			return tx.Commit()
		}
		_, err = tx.ExecContext(ctx, "UPDATE acceptance_generation SET trust_generation=?,revocation_generation=?,policy_sha256=? WHERE id=1", trustGeneration, revocationGeneration, policySHA)
		if err == nil {
			_, err = tx.ExecContext(ctx, "INSERT INTO acceptance_audit(run_id,step_id,receipt_sha256,trust_generation,revocation_generation,active,at) SELECT run_id,step_id,receipt_sha256,?,?,0,? FROM acceptance_projection WHERE active=1", trustGeneration, revocationGeneration, time.Now().UTC().Format(time.RFC3339Nano))
		}
		if err == nil {
			_, err = tx.ExecContext(ctx, "UPDATE acceptance_projection SET active=0,trust_generation=?,revocation_generation=?", trustGeneration, revocationGeneration)
		}
	}
	if err != nil {
		return classify(err)
	}
	return classify(tx.Commit())
}

// AttachAcceptance atomically binds a signed, immutable P8 receipt to the
// exact current stopped runtime attempt. The supplied policy is the caller's
// protected trust view; it is never persisted as runtime authority.
func (s *Store) AttachAcceptance(ctx context.Context, receipt verification.AcceptanceReceipt, candidate verification.CandidateManifest, policy verification.VerificationPolicy, trustGeneration, revocationGeneration int64, asOf time.Time) error {
	if trustGeneration < 1 || revocationGeneration < 1 || asOf.IsZero() {
		return errors.New("acceptance trust generations and trusted time are required")
	}
	if err := verification.VerifyAcceptance(receipt, policy.Issuers, asOf, time.Duration(policy.MaxAgeSeconds)*time.Second); err != nil {
		return err
	}
	if err := verification.ValidatePolicy(policy); err != nil {
		return err
	}
	candidateSHA, candidateErr := verification.Digest(candidate)
	if candidateErr != nil || candidate.Origin != "runtime" || verification.ValidateAcceptanceManifestBinding(receipt, candidate, candidateSHA, receipt.SnapshotSHA256, policy) != nil {
		return errors.New("acceptance candidate binding mismatch or non-runtime origin")
	}
	issuedAt, _ := time.Parse(time.RFC3339, receipt.IssuedAt)
	validUntil := issuedAt.Add(time.Duration(policy.MaxAgeSeconds) * time.Second)
	if receipt.ValidUntil != "" {
		if expiry, parseErr := time.Parse(time.RFC3339, receipt.ValidUntil); parseErr != nil {
			return errors.New("invalid acceptance expiry")
		} else if expiry.Before(validUntil) {
			validUntil = expiry
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return classify(err)
	}
	defer tx.Rollback()
	var version int
	if err = tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version != 2 {
		return errors.New("acceptance projection is not enabled")
	}
	receiptSHA := digest(marshal(receipt))
	var currentTrust, currentRev int64
	var currentPolicy string
	if err = tx.QueryRowContext(ctx, "SELECT trust_generation,revocation_generation,policy_sha256 FROM acceptance_generation WHERE id=1").Scan(&currentTrust, &currentRev, &currentPolicy); err != nil || currentTrust != trustGeneration || currentRev != revocationGeneration || currentPolicy != receipt.PolicySHA256 {
		return ErrBlocked
	}
	var revoked int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM acceptance_revocations WHERE receipt_sha256=?", receiptSHA).Scan(&revoked); err != nil {
		return classify(err)
	}
	if revoked != 0 {
		return ErrBlocked
	}
	d, err := loadFrom(ctx, tx)
	if err != nil {
		return classify(err)
	}
	step, _, ok := findStep(&d, receipt.RunID, receipt.StepID)
	if !ok {
		return ErrBlocked
	}
	var run *records.Run
	for i := range d.State.Runs {
		if d.State.Runs[i].Manifest.ID == receipt.RunID {
			run = &d.State.Runs[i]
			break
		}
	}
	if run == nil {
		return ErrBlocked
	}
	if run.Cancelled || step.Checkpoint == nil || !step.Checkpoint.Quiescent || !step.StopObserved || step.Token.AttemptID != receipt.AttemptID || uint64(step.Token.Fence) != receipt.Fence || run.ManifestSHA256 != receipt.InputSHA256 || candidate.Repository != run.Manifest.Repository || candidate.SourceRevision != step.Checkpoint.BaseRevision || candidate.BaseRevision != run.Manifest.InputRevision || step.Checkpoint.BaseRevision != run.Manifest.InputRevision || candidate.HostReceiptSHA256 != step.HostReceiptSHA256 || candidate.PreparedReceiptSHA256 != step.PreparedSHA256 || candidate.CheckpointSHA256 != step.Checkpoint.Digest || snapshotDigest(step.Checkpoint.Assets) != receipt.SnapshotSHA256 || run.Revision != int64(receipt.CandidateRevision) {
		return fmt.Errorf("%w: acceptance lineage mismatch (revision %d/%d, checkpoint %t, quiescent %t, stopped %t, attempt %t, fence %t, manifest input %t, repository %t, source revision %t, base revision %t, host %t, prepared %t, checkpoint digest %t, snapshot %t)", ErrBlocked, run.Revision, receipt.CandidateRevision, step.Checkpoint != nil, step.Checkpoint != nil && step.Checkpoint.Quiescent, step.StopObserved, step.Token.AttemptID == receipt.AttemptID, uint64(step.Token.Fence) == receipt.Fence, run.ManifestSHA256 == receipt.InputSHA256, candidate.Repository == run.Manifest.Repository, step.Checkpoint != nil && candidate.SourceRevision == step.Checkpoint.BaseRevision, candidate.BaseRevision == run.Manifest.InputRevision, candidate.HostReceiptSHA256 == step.HostReceiptSHA256, candidate.PreparedReceiptSHA256 == step.PreparedSHA256, step.Checkpoint != nil && candidate.CheckpointSHA256 == step.Checkpoint.Digest, step.Checkpoint != nil && snapshotDigest(step.Checkpoint.Assets) == receipt.SnapshotSHA256)
	}
	var oldTrust, oldRev int64
	var oldReceipt string
	err = tx.QueryRowContext(ctx, "SELECT trust_generation,revocation_generation,receipt_sha256 FROM acceptance_projection WHERE run_id=? AND step_id=?", receipt.RunID, receipt.StepID).Scan(&oldTrust, &oldRev, &oldReceipt)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return classify(err)
	}
	if !errors.Is(err, sql.ErrNoRows) && (trustGeneration < oldTrust || revocationGeneration < oldRev) {
		return ErrConflict
	}
	if !errors.Is(err, sql.ErrNoRows) && oldReceipt != receiptSHA && trustGeneration == oldTrust && revocationGeneration == oldRev {
		return ErrConflict
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO acceptance_projection(run_id,step_id,receipt_sha256,policy_sha256,candidate_sha256,snapshot_sha256,input_sha256,attempt_id,fence,valid_until,trust_generation,revocation_generation,active) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,1) ON CONFLICT(run_id,step_id) DO UPDATE SET receipt_sha256=excluded.receipt_sha256,policy_sha256=excluded.policy_sha256,candidate_sha256=excluded.candidate_sha256,snapshot_sha256=excluded.snapshot_sha256,input_sha256=excluded.input_sha256,attempt_id=excluded.attempt_id,fence=excluded.fence,valid_until=excluded.valid_until,trust_generation=excluded.trust_generation,revocation_generation=excluded.revocation_generation,active=1`, receipt.RunID, receipt.StepID, receiptSHA, receipt.PolicySHA256, receipt.CandidateSHA256, receipt.SnapshotSHA256, receipt.InputSHA256, receipt.AttemptID, receipt.Fence, validUntil.UTC().Format(time.RFC3339Nano), trustGeneration, revocationGeneration); err != nil {
		return classify(err)
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO acceptance_audit(run_id,step_id,receipt_sha256,trust_generation,revocation_generation,active,at) VALUES(?,?,?,?,?,1,?)", receipt.RunID, receipt.StepID, receiptSHA, trustGeneration, revocationGeneration, asOf.UTC().Format(time.RFC3339Nano)); err != nil {
		return classify(err)
	}
	return classify(tx.Commit())
}

// RevokeAcceptance advances the local generation and closes the active
// projection. A later claim must observe a newly verified attachment.
func (s *Store) RevokeAcceptance(ctx context.Context, runID, stepID string, trustGeneration, revocationGeneration int64, at time.Time) error {
	if runID == "" || stepID == "" || trustGeneration < 1 || revocationGeneration < 1 || at.IsZero() {
		return errors.New("revocation identity, generations and time required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return classify(err)
	}
	defer tx.Rollback()
	var oldTrust, oldRev int64
	if err = tx.QueryRowContext(ctx, "SELECT trust_generation,revocation_generation FROM acceptance_projection WHERE run_id=? AND step_id=?", runID, stepID).Scan(&oldTrust, &oldRev); err != nil {
		return classify(err)
	}
	if trustGeneration < oldTrust || revocationGeneration <= oldRev {
		return ErrConflict
	}
	var currentTrust, currentRev int64
	var currentPolicy string
	if err = tx.QueryRowContext(ctx, "SELECT trust_generation,revocation_generation,policy_sha256 FROM acceptance_generation WHERE id=1").Scan(&currentTrust, &currentRev, &currentPolicy); err != nil {
		return classify(err)
	}
	if trustGeneration < currentTrust || revocationGeneration < currentRev {
		return ErrConflict
	}
	var receiptSHA string
	if err = tx.QueryRowContext(ctx, "SELECT receipt_sha256 FROM acceptance_projection WHERE run_id=? AND step_id=?", runID, stepID).Scan(&receiptSHA); err != nil {
		return classify(err)
	}
	if _, err = tx.ExecContext(ctx, "INSERT OR IGNORE INTO acceptance_revocations(receipt_sha256,revoked_at) VALUES(?,?)", receiptSHA, at.UTC().Format(time.RFC3339Nano)); err != nil {
		return classify(err)
	}
	if _, err = tx.ExecContext(ctx, "UPDATE acceptance_generation SET trust_generation=?,revocation_generation=? WHERE id=1", trustGeneration, revocationGeneration); err != nil {
		return classify(err)
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO acceptance_audit(run_id,step_id,receipt_sha256,trust_generation,revocation_generation,active,at) SELECT run_id,step_id,receipt_sha256,?,?,0,? FROM acceptance_projection WHERE active=1", trustGeneration, revocationGeneration, at.UTC().Format(time.RFC3339Nano)); err != nil {
		return classify(err)
	}
	if _, err = tx.ExecContext(ctx, "UPDATE acceptance_projection SET trust_generation=?,revocation_generation=?,active=0", trustGeneration, revocationGeneration); err != nil {
		return classify(err)
	}
	return classify(tx.Commit())
}

// AcceptanceActive is read-only and fails closed when the opt-in migration or
// matching current trust generations are absent.
func (s *Store) AcceptanceActive(ctx context.Context, runID, stepID string, trustGeneration, revocationGeneration int64) bool {
	var active int
	err := s.db.QueryRowContext(ctx, `SELECT p.active FROM acceptance_projection p JOIN acceptance_generation g ON g.id=1 WHERE p.run_id=? AND p.step_id=? AND p.trust_generation=? AND p.revocation_generation=? AND p.policy_sha256=g.policy_sha256 AND g.trust_generation=? AND g.revocation_generation=? AND NOT EXISTS (SELECT 1 FROM acceptance_revocations r WHERE r.receipt_sha256=p.receipt_sha256)`, runID, stepID, trustGeneration, revocationGeneration, trustGeneration, revocationGeneration).Scan(&active)
	return err == nil && active == 1
}

func getRun(d *diskState, runID string) (*records.Run, bool) {
	for i := range d.State.Runs {
		if d.State.Runs[i].Manifest.ID == runID {
			return &d.State.Runs[i], true
		}
	}
	return nil, false
}

func checkSidecars(path string) error {
	for _, p := range []string{path, path + "-wal", path + "-shm"} {
		i, e := os.Lstat(p)
		if errors.Is(e, os.ErrNotExist) {
			continue
		}
		if e != nil {
			return e
		}
		if !i.Mode().IsRegular() || i.Mode()&os.ModeSymlink != 0 {
			return errors.New("runtime database files must be regular files")
		}
		if e = secureFile(p); e != nil {
			return e
		}
	}
	return nil
}
func (s *Store) Close() error { return s.db.Close() }

// SQL transactions are serialized by SQLite; load the projection and chain before mutation.
func (s *Store) load(ctx context.Context) (diskState, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return diskState{}, classify(err)
	}
	defer tx.Rollback()
	d, err := loadFrom(ctx, tx)
	if err != nil {
		return d, err
	}
	if err = tx.Commit(); err != nil {
		return diskState{}, classify(err)
	}
	return d, nil
}

type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func loadFrom(ctx context.Context, q queryer) (diskState, error) {
	var d diskState
	var b []byte
	var h string
	if e := q.QueryRowContext(ctx, "SELECT payload,digest FROM state WHERE id=1").Scan(&b, &h); e != nil {
		return d, e
	}
	if digest(b) != h {
		return d, errors.New("runtime state integrity mismatch")
	}
	if e := json.Unmarshal(b, &d); e != nil {
		return d, e
	}
	if d.State.Version != records.Version || d.State.LifecycleVersion != records.LifecycleVersion || d.State.Epoch == "" || d.Fences == nil {
		return d, errors.New("invalid runtime state")
	}
	var dbVersion int
	if err := q.QueryRowContext(ctx, "PRAGMA user_version").Scan(&dbVersion); err != nil {
		return d, err
	}
	if dbVersion == 2 {
		d.Acceptance = map[string]acceptanceProjection{}
		_ = q.QueryRowContext(ctx, "SELECT trust_generation,revocation_generation,policy_sha256 FROM acceptance_generation WHERE id=1").Scan(&d.TrustGeneration, &d.RevocationGeneration, &d.AcceptancePolicySHA256)
		rows, err := q.QueryContext(ctx, "SELECT run_id,step_id,candidate_sha256,snapshot_sha256,input_sha256,attempt_id,fence,valid_until,trust_generation,revocation_generation,policy_sha256,active FROM acceptance_projection")
		if err != nil {
			return d, err
		}
		for rows.Next() {
			var run, step string
			var p acceptanceProjection
			var active int
			var validUntil string
			if err = rows.Scan(&run, &step, &p.Candidate, &p.Snapshot, &p.Input, &p.Attempt, &p.Fence, &validUntil, &p.Trust, &p.Revocation, &p.PolicySHA, &active); err != nil {
				rows.Close()
				return d, err
			}
			p.ValidUntil, _ = time.Parse(time.RFC3339Nano, validUntil)
			p.Active = active == 1
			d.Acceptance[run+"/"+step] = p
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			return d, err
		}
		if err = rows.Close(); err != nil {
			return d, err
		}
	}
	d.State.Events = nil
	rows, e := q.QueryContext(ctx, "SELECT sequence,payload,digest,previous FROM audit ORDER BY sequence")
	if e != nil {
		return d, e
	}
	defer rows.Close()
	prev := ""
	i := int64(0)
	for rows.Next() {
		var dbSequence int64
		var raw []byte
		var h, p string
		if e = rows.Scan(&dbSequence, &raw, &h, &p); e != nil {
			return d, e
		}
		var ev records.Event
		if e = json.Unmarshal(raw, &ev); e != nil {
			return d, e
		}
		unsigned := ev
		unsigned.SHA256 = ""
		base := marshal(unsigned)
		expected := digest(append(append([]byte(p), 0), base...))
		if p != prev || expected != h {
			return d, errors.New("runtime audit chain integrity mismatch")
		}
		i++
		if dbSequence != i || ev.Sequence != i || ev.PreviousSHA256 != p || ev.SHA256 != h {
			return d, fmt.Errorf("runtime audit event mismatch (sequence %d/%d, hash %t, previous %t)", ev.Sequence, i, ev.SHA256 == h, ev.PreviousSHA256 == p)
		}
		d.State.Events = append(d.State.Events, ev)
		prev = h
	}
	if e = rows.Err(); e != nil {
		return d, e
	}
	if e = rows.Close(); e != nil {
		return d, e
	}
	var operationCount int
	if e = q.QueryRowContext(ctx, "SELECT count(*) FROM operations").Scan(&operationCount); e != nil {
		return d, e
	}
	if int64(operationCount) != int64(len(d.State.Events)) {
		return d, errors.New("runtime operation/audit count mismatch")
	}
	for _, ev := range d.State.Events {
		var operationRequest string
		var operationResult []byte
		if e = q.QueryRowContext(ctx, "SELECT request,result FROM operations WHERE key=?", ev.OperationKey).Scan(&operationRequest, &operationResult); e != nil {
			return d, errors.New("runtime idempotency record missing")
		}
		if operationRequest != ev.RequestSHA256 || digest(operationResult) != ev.PayloadSHA256 {
			return d, errors.New("runtime idempotency integrity mismatch")
		}
	}
	if d.State.Revision != i {
		return d, fmt.Errorf("runtime audit projection mismatch (revision %d, events %d)", d.State.Revision, i)
	}
	return d, nil
}

func (s *Store) Snapshot(ctx context.Context) (records.State, error) {
	d, e := s.load(ctx)
	return d.State, e
}
func (s *Store) Policy() (records.Policy, error) {
	d, e := s.load(context.Background())
	return d.Policy, e
}

// transact validates the full audit and state before each mutation, then commits projection, event and idempotency result together.
func (s *Store) transact(ctx context.Context, key string, request any, at time.Time, runID, stepID, attemptID, phase string, fn func(*diskState) (any, error)) (any, error) {
	if key == "" || at.IsZero() {
		return nil, errors.New("operation key and timestamp are required")
	}
	req := digest(marshal(request))
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return nil, classify(e)
	}
	defer tx.Rollback()
	var prior string
	var saved []byte
	e = tx.QueryRowContext(ctx, "SELECT request,result FROM operations WHERE key=?", key).Scan(&prior, &saved)
	if e == nil {
		if prior != req {
			return nil, ErrConflict
		}
		var v any
		_ = json.Unmarshal(saved, &v)
		return saved, nil
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return nil, classify(e)
	}
	d, e := loadFrom(ctx, tx)
	if e != nil {
		return nil, classify(e)
	}
	if last := d.State.LastAt; last != "" {
		t, er := time.Parse(time.RFC3339Nano, last)
		if er != nil || at.Before(t) {
			return nil, errors.New("runtime clock moved backwards")
		}
	}
	result, e := fn(&d)
	if e != nil {
		return nil, e
	}
	d.State.Revision++
	d.State.LastAt = at.UTC().Format(time.RFC3339Nano)
	prev := ""
	if len(d.State.Events) > 0 {
		prev = d.State.Events[len(d.State.Events)-1].SHA256
	}
	ev := records.Event{Sequence: int64(len(d.State.Events) + 1), ID: fmt.Sprintf("%s-%d", d.State.Epoch, len(d.State.Events)+1), OperationKey: key, RequestSHA256: req, RunID: runID, StepID: stepID, AttemptID: attemptID, Phase: phase, At: d.State.LastAt, PreviousSHA256: prev}
	ev.PayloadSHA256 = digest(marshal(result))
	ev.SHA256 = ""
	base := marshal(ev)
	ev.SHA256 = digest(append(append([]byte(prev), 0), base...))
	raw := marshal(ev)
	if _, e = tx.ExecContext(ctx, "INSERT INTO audit(payload,digest,previous) VALUES(?,?,?)", raw, ev.SHA256, prev); e != nil {
		return nil, classify(e)
	}
	d.State.Events = append(d.State.Events, ev)
	b := marshal(d)
	if _, e = tx.ExecContext(ctx, "UPDATE state SET payload=?,digest=? WHERE id=1", b, digest(b)); e != nil {
		return nil, classify(e)
	}
	rb := marshal(result)
	if _, e = tx.ExecContext(ctx, "INSERT INTO operations VALUES(?,?,?)", key, req, rb); e != nil {
		return nil, classify(e)
	}
	if e = tx.Commit(); e != nil {
		return nil, classify(e)
	}
	return result, nil
}

func (s *Store) Submit(ctx context.Context, key string, m records.Manifest, manifestSHA string, at time.Time) (records.Run, error) {
	req := struct {
		M   records.Manifest
		SHA string
	}{m, manifestSHA}
	v, e := s.transact(ctx, key, req, at, m.ID, "", "", "pending", func(d *diskState) (any, error) {
		for _, r := range d.State.Runs {
			if r.Manifest.ID == m.ID {
				return nil, ErrConflict
			}
		}
		if m.ID == "" || len(m.Steps) == 0 {
			return nil, errors.New("manifest requires id and steps")
		}
		seen := map[string]bool{}
		for _, spec := range m.Steps {
			if spec.ID == "" || seen[spec.ID] {
				return nil, errors.New("manifest step ids must be nonempty and unique")
			}
			seen[spec.ID] = true
		}
		r := records.Run{Manifest: m, ManifestSHA256: manifestSHA, Steps: make([]records.Step, len(m.Steps)), Revision: 1}
		for i, x := range m.Steps {
			r.Steps[i] = records.Step{ID: x.ID, Phase: "pending"}
		}
		d.State.Runs = append(d.State.Runs, r)
		return r, nil
	})
	if e != nil {
		return records.Run{}, e
	}
	var r records.Run
	return r, decodeResult(v, &r)
}

func (s *Store) Claim(ctx context.Context, key, runID, stepID, owner, attemptID string, at time.Time) (records.Token, error) {
	return s.ClaimWithAcceptance(ctx, key, runID, stepID, owner, attemptID, at, AcceptanceGuard{})
}

type AcceptanceGuard struct {
	Dependencies         []string
	TrustGeneration      int64
	RevocationGeneration int64
	AuthorityValidUntil  time.Time
	PolicySHA256         string
}

func (s *Store) ClaimWithAcceptance(ctx context.Context, key, runID, stepID, owner, attemptID string, at time.Time, guard AcceptanceGuard) (records.Token, error) {
	req := []string{runID, stepID, owner, attemptID}
	req = append(req, fmt.Sprint(guard.TrustGeneration, "/", guard.RevocationGeneration), strings.Join(guard.Dependencies, ","))
	v, e := s.transact(ctx, key, req, at, runID, stepID, attemptID, "claimed", func(d *diskState) (any, error) {
		active := 0
		for _, r := range d.State.Runs {
			for _, st := range r.Steps {
				if activePhase(st.Phase) {
					active++
				}
			}
		}
		if active >= d.Policy.MaxConcurrent {
			return nil, ErrBlocked
		}
		for ri := range d.State.Runs {
			r := &d.State.Runs[ri]
			if r.Manifest.ID != runID {
				continue
			}
			if r.Cancelled {
				return nil, ErrBlocked
			}
			if !acceptanceDependencies(d, runID, guard, at) {
				return nil, ErrBlocked
			}
			for si := range r.Steps {
				st := &r.Steps[si]
				if st.ID != stepID || st.Phase != "pending" {
					continue
				}
				if d.Policy.MaxAttempts > 0 && st.Attempts >= d.Policy.MaxAttempts {
					return nil, ErrBlocked
				}
				fk := runID + "/" + stepID
				d.Fences[fk]++
				checkpoint := st.Checkpoint
				attempts := st.Attempts + 1
				*st = records.Step{ID: st.ID, Phase: "pending", Checkpoint: checkpoint, Attempts: attempts}
				st.Token = records.Token{RunID: runID, StepID: stepID, AttemptID: attemptID, Owner: owner, Epoch: d.State.Epoch, Fence: d.Fences[fk]}
				st.Phase = "claimed"
				st.LeaseUntil = at.Add(time.Duration(d.Policy.LeaseSeconds) * time.Second).UTC().Format(time.RFC3339Nano)
				r.Revision++
				return st.Token, nil
			}
		}
		return nil, ErrBlocked
	})
	if e != nil {
		return records.Token{}, e
	}
	var t records.Token
	return t, decodeResult(v, &t)
}

func acceptanceDependencies(d *diskState, runID string, guard AcceptanceGuard, at time.Time) bool {
	if len(guard.Dependencies) == 0 {
		return true
	}
	if guard.TrustGeneration < 1 || guard.RevocationGeneration < 1 || guard.PolicySHA256 == "" || guard.AuthorityValidUntil.IsZero() || !at.Before(guard.AuthorityValidUntil) || d.TrustGeneration != guard.TrustGeneration || d.RevocationGeneration != guard.RevocationGeneration || d.AcceptancePolicySHA256 != guard.PolicySHA256 {
		return false
	}
	for _, dependency := range guard.Dependencies {
		step, _, ok := findStep(d, runID, dependency)
		if !ok {
			return false
		}
		p, exists := d.Acceptance[runID+"/"+dependency]
		run, runOK := getRun(d, runID)
		if !exists || !runOK || !p.Active || !at.Before(p.ValidUntil) || p.Trust != d.TrustGeneration || p.Revocation != d.RevocationGeneration || p.PolicySHA != d.AcceptancePolicySHA256 || p.PolicySHA != guard.PolicySHA256 || step.Checkpoint == nil || !step.StopObserved || !step.Checkpoint.Quiescent || step.Token.AttemptID != p.Attempt || step.Token.Fence != p.Fence || p.Candidate == "" || run.ManifestSHA256 != p.Input || snapshotDigest(step.Checkpoint.Assets) != p.Snapshot {
			return false
		}
	}
	return true
}

func (s *Store) DependenciesActive(ctx context.Context, runID string, guard AcceptanceGuard) bool {
	return s.DependenciesActiveAt(ctx, runID, guard, time.Now().UTC())
}

func (s *Store) DependenciesActiveAt(ctx context.Context, runID string, guard AcceptanceGuard, at time.Time) bool {
	if at.IsZero() {
		return false
	}
	d, err := s.load(ctx)
	return err == nil && acceptanceDependencies(&d, runID, guard, at.UTC())
}

// AcceptedSteps returns IDs whose attached receipts still bind the current
// quiescent attempt, checkpoint, input and protected generations.
func (s *Store) AcceptedSteps(ctx context.Context, runID string, guard AcceptanceGuard) []string {
	return s.AcceptedStepsAt(ctx, runID, guard, time.Now().UTC())
}

func (s *Store) AcceptedStepsAt(ctx context.Context, runID string, guard AcceptanceGuard, at time.Time) []string {
	if at.IsZero() {
		return nil
	}
	d, err := s.load(ctx)
	if err != nil || len(d.Acceptance) == 0 {
		return nil
	}
	run, ok := getRun(&d, runID)
	if !ok {
		return nil
	}
	out := []string{}
	for _, step := range run.Steps {
		if acceptanceDependencies(&d, runID, AcceptanceGuard{Dependencies: []string{step.ID}, TrustGeneration: guard.TrustGeneration, RevocationGeneration: guard.RevocationGeneration, AuthorityValidUntil: guard.AuthorityValidUntil, PolicySHA256: guard.PolicySHA256}, at.UTC()) {
			out = append(out, step.ID)
		}
	}
	return out
}

func (s *Store) Check(ctx context.Context, t records.Token, at time.Time) error {
	d, e := s.load(ctx)
	if e != nil {
		return e
	}
	if at.IsZero() || d.State.LastAt != "" && at.Before(parseTime(d.State.LastAt)) {
		return errors.New("runtime clock moved backwards")
	}
	st, ri, ok := findStep(&d, t.RunID, t.StepID)
	if !ok || d.State.Runs[ri].Cancelled || st.Token != t || st.Phase == "blocked" || st.Phase == "cancelled" || st.Phase == "failed" || st.Phase == "exhausted" {
		return ErrBlocked
	}
	until := parseTime(st.LeaseUntil)
	if until.IsZero() || !at.Before(until) {
		return ErrBlocked
	}
	return nil
}

// CheckWithAcceptance repeats the exact dependency projection and runtime fence
// guard from one SQLite snapshot immediately before launch/publication.
func (s *Store) CheckWithAcceptance(ctx context.Context, t records.Token, at time.Time, guard AcceptanceGuard) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return classify(err)
	}
	defer tx.Rollback()
	d, err := loadFrom(ctx, tx)
	if err != nil {
		return err
	}
	if at.IsZero() || d.State.LastAt != "" && at.Before(parseTime(d.State.LastAt)) {
		return errors.New("runtime clock moved backwards")
	}
	st, ri, ok := findStep(&d, t.RunID, t.StepID)
	if !ok || d.State.Runs[ri].Cancelled || st.Token != t || st.Phase == "blocked" || st.Phase == "cancelled" || st.Phase == "failed" || st.Phase == "exhausted" || !parseTime(st.LeaseUntil).After(at) {
		return ErrBlocked
	}
	if !acceptanceDependencies(&d, t.RunID, guard, at) {
		return ErrBlocked
	}
	return tx.Commit()
}

func (s *Store) Update(ctx context.Context, key string, t records.Token, phase string, step records.Step, at time.Time, acceptance ...AcceptanceGuard) (records.Step, error) {
	req := any(struct {
		T records.Token
		P string
		S records.Step
	}{t, phase, step})
	guard := AcceptanceGuard{}
	if len(acceptance) > 0 {
		guard = acceptance[0]
		req = struct {
			T records.Token
			P string
			S records.Step
			G AcceptanceGuard
		}{t, phase, step, guard}
	}
	v, e := s.transact(ctx, key, req, at, t.RunID, t.StepID, t.AttemptID, phase, func(d *diskState) (any, error) {
		cur, ri, ok := findStep(d, t.RunID, t.StepID)
		if !ok || cur.Token != t || cur.Phase == "blocked" || cur.Phase == "cancelled" && !d.State.Runs[ri].Cancelled {
			return nil, ErrBlocked
		}
		if parseTime(cur.LeaseUntil).Before(at) && !(d.State.Runs[ri].Cancelled && phase == "blocked" && step.StopObserved) {
			return nil, ErrBlocked
		}
		if step.ID != cur.ID || step.Token != cur.Token || step.Attempts != cur.Attempts {
			return nil, errors.New("immutable step identity changed")
		}
		if !acceptanceDependencies(d, t.RunID, guard, at) {
			return nil, ErrBlocked
		}
		if step.Accepted || phase == "accepted" || phase == "completed" {
			return nil, ErrBlocked
		}
		run := &d.State.Runs[ri]
		if run.Cancelled {
			// Cancellation closes admission and publication. A stopped candidate may
			// still be recorded as blocked for later manual reconciliation.
			if phase != "blocked" || !step.StopObserved || (!activePhase(cur.Phase) && cur.Phase != "cancelled") {
				return nil, ErrBlocked
			}
		} else if !transition(cur.Phase, phase) {
			return nil, ErrBlocked
		}
		if !validProgressEvidence(cur, &step, phase, run.Cancelled) {
			return nil, ErrBlocked
		}
		if evidenceFrozen(cur.Phase) && !sameBoundEvidence(cur, &step) {
			if cur.Phase != "running" || phase != "awaiting-verification" || !sameEvidenceExceptCheckpoint(cur, &step) || !validAttemptCheckpoint(step.Checkpoint, t, run.Manifest.InputRevision, cur.Workspace) || staging.Verify(*step.Checkpoint) != nil {
				return nil, ErrBlocked
			}
		}
		step.Phase = phase
		step.LeaseUntil = cur.LeaseUntil
		d.State.Runs[ri].Steps[indexOf(&d.State.Runs[ri], t.StepID)] = step
		d.State.Runs[ri].Revision++
		return step, nil
	})
	if e != nil {
		return records.Step{}, e
	}
	var out records.Step
	return out, decodeResult(v, &out)
}

func (s *Store) Renew(ctx context.Context, key string, t records.Token, at time.Time) error {
	_, e := s.transact(ctx, key, t, at, t.RunID, t.StepID, t.AttemptID, "renewed", func(d *diskState) (any, error) {
		st, ri, ok := findStep(d, t.RunID, t.StepID)
		if !ok || d.State.Runs[ri].Cancelled || st.Token != t || st.Phase == "blocked" || !parseTime(st.LeaseUntil).After(at) {
			return nil, ErrBlocked
		}
		st.LeaseUntil = at.Add(time.Duration(d.Policy.LeaseSeconds) * time.Second).UTC().Format(time.RFC3339Nano)
		d.State.Runs[ri].Revision++
		return st, nil
	})
	return e
}
func (s *Store) Cancel(ctx context.Context, key, runID, reason string, at time.Time) error {
	_, e := s.transact(ctx, key, []string{runID, reason}, at, runID, "", "", "cancelled", func(d *diskState) (any, error) {
		for ri := range d.State.Runs {
			r := &d.State.Runs[ri]
			if r.Manifest.ID == runID {
				r.Cancelled = true
				r.CancelReason = reason
				for si := range r.Steps {
					if activePhase(r.Steps[si].Phase) {
						r.Steps[si].Phase = "cancelled"
					}
				}
				r.Revision++
				return r, nil
			}
		}
		return nil, ErrBlocked
	})
	return e
}
func (s *Store) Recover(ctx context.Context, key, runID string, at time.Time) error {
	_, e := s.transact(ctx, key, runID, at, runID, "", "", "recovered", func(d *diskState) (any, error) {
		for ri := range d.State.Runs {
			r := &d.State.Runs[ri]
			if r.Manifest.ID != runID {
				continue
			}
			for si := range r.Steps {
				st := &r.Steps[si]
				if activePhase(st.Phase) && !parseTime(st.LeaseUntil).After(at) {
					fk := runID + "/" + st.ID
					d.Fences[fk]++
					st.Phase = "blocked"
					if st.LaunchPossible {
						st.Reason = "launch may have occurred; reconciliation required"
					} else {
						st.Reason = "expired unlaunched claim blocked pending reconciliation"
					}
					st.Token.Fence = d.Fences[fk]
				}
			}
			r.Revision++
			return r, nil
		}
		return nil, ErrBlocked
	})
	return e
}

// Reconcile records externally verified recovery observations for an exact
// fenced attempt. It leaves the step blocked and never grants acceptance.
func (s *Store) Reconcile(ctx context.Context, key string, token records.Token, step records.Step, at time.Time) (records.Step, error) {
	request := struct {
		Token records.Token
		Step  records.Step
	}{token, step}
	v, err := s.transact(ctx, key, request, at, token.RunID, token.StepID, token.AttemptID, "reconciled", func(d *diskState) (any, error) {
		cur, ri, ok := findStep(d, token.RunID, token.StepID)
		if !ok || cur.Token != token {
			return nil, ErrBlocked
		}
		run := &d.State.Runs[ri]
		if cur.Phase != "blocked" && cur.Phase != "cancelled" && cur.Phase != "awaiting-verification" && !(run.Cancelled && activePhase(cur.Phase)) {
			return nil, ErrBlocked
		}
		if step.ID != cur.ID || step.Token != cur.Token || step.Attempts != cur.Attempts || step.Accepted || !step.StopObserved || step.LaunchPossible {
			return nil, ErrBlocked
		}
		if step.Workspace != cur.Workspace || step.DecisionSHA256 != cur.DecisionSHA256 || step.PreparedSHA256 != cur.PreparedSHA256 || step.HostReceiptSHA256 != cur.HostReceiptSHA256 || !equal(step.Demand, cur.Demand) {
			return nil, ErrBlocked
		}
		cp := step.Checkpoint
		if cp == nil || !validAttemptCheckpoint(cp, token, run.Manifest.InputRevision, cur.Workspace) || staging.Verify(*cp) != nil || step.HostReceiptSHA256 == "" {
			return nil, ErrBlocked
		}
		if err := validateRecoveredAccounting(cur, &step); err != nil {
			return nil, err
		}
		step.Phase = "blocked"
		step.LeaseUntil = cur.LeaseUntil
		run.Steps[indexOf(run, token.StepID)] = step
		run.Revision++
		return step, nil
	})
	if err != nil {
		return records.Step{}, err
	}
	var out records.Step
	return out, decodeResult(v, &out)
}

func validateRecoveredAccounting(old, next *records.Step) error {
	if old.Reservation == nil || old.AccountingToken == nil {
		if old.Reservation != nil || old.AccountingToken != nil || next.Reservation != nil || next.AccountingToken != nil {
			return ErrBlocked
		}
		return nil
	}
	if next.Reservation == nil || next.AccountingToken == nil {
		return ErrBlocked
	}
	a, b := old.Reservation, next.Reservation
	baseline := *a
	baseline.Owner, baseline.Fence, baseline.State = b.Owner, b.Fence, b.State
	if !equal(baseline, *b) || b.Fence <= a.Fence || b.Owner == a.Owner {
		return ErrBlocked
	}
	if b.State != "uncertain" && b.State != "released" && b.State != "settled" {
		return ErrBlocked
	}
	if old.AccountingToken.ID != next.AccountingToken.ID || old.AccountingToken.ID != b.ID || old.AccountingToken.Epoch != next.AccountingToken.Epoch || old.AccountingToken.Epoch != b.AuthorityEpoch || old.AccountingToken.Fence >= next.AccountingToken.Fence || next.AccountingToken.Fence != b.Fence || next.AccountingToken.Owner != b.Owner {
		return ErrBlocked
	}
	return nil
}

// ReconcileUnlaunched records a trusted prelaunch recovery after external
// evidence proved the exact attempt stopped and its P3 hold was released.
// It may adopt the release receipt when the reserve reply was lost before the
// runtime projection could persist its reservation token.
func (s *Store) ReconcileUnlaunched(ctx context.Context, key string, token records.Token, step records.Step, at time.Time) (records.Step, error) {
	request := struct {
		Token records.Token
		Step  records.Step
	}{token, step}
	v, err := s.transact(ctx, key, request, at, token.RunID, token.StepID, token.AttemptID, "reconciled-unlaunched", func(d *diskState) (any, error) {
		cur, ri, ok := findStep(d, token.RunID, token.StepID)
		if !ok || cur.Token != token || cur.Phase != "blocked" {
			return nil, ErrBlocked
		}
		run := &d.State.Runs[ri]
		if step.ID != cur.ID || step.Token != cur.Token || step.Attempts != cur.Attempts || step.Accepted || !step.StopObserved || step.LaunchPossible || step.HostReceiptSHA256 != "" {
			return nil, ErrBlocked
		}
		if step.Workspace == "" || step.Workspace != cur.Workspace || step.DecisionSHA256 == "" || step.DecisionSHA256 != cur.DecisionSHA256 || step.PreparedSHA256 != cur.PreparedSHA256 || !equal(step.Demand, cur.Demand) {
			return nil, ErrBlocked
		}
		cp := step.Checkpoint
		if cp == nil || !validAttemptCheckpoint(cp, token, run.Manifest.InputRevision, cur.Workspace) || staging.Verify(*cp) != nil {
			return nil, ErrBlocked
		}
		if step.Reservation == nil || step.AccountingToken == nil || step.Demand == nil {
			return nil, ErrBlocked
		}
		r := step.Reservation
		accountToken := step.AccountingToken
		if r.ID != "hold-"+token.AttemptID || r.AttemptID != token.AttemptID || r.State != "released" || r.ProfileSHA256 != step.Demand.ProfileSHA256 || r.ConfigurationID != step.Demand.ConfigurationID || r.DemandSHA256 != usage.Digest(*step.Demand) || r.DemandValidUntil != step.Demand.ValidUntil || !equal(r.Amounts, step.Demand.Amounts) {
			return nil, ErrBlocked
		}
		if accountToken.ID != r.ID || accountToken.Epoch != r.AuthorityEpoch || accountToken.Owner != r.Owner || accountToken.Fence != r.Fence || accountToken.Fence < 1 {
			return nil, ErrBlocked
		}
		if cur.Reservation != nil || cur.AccountingToken != nil {
			if cur.Reservation == nil || cur.AccountingToken == nil {
				return nil, ErrBlocked
			}
			old := cur.Reservation
			oldToken := cur.AccountingToken
			baseline := *old
			baseline.Owner, baseline.Fence, baseline.State = r.Owner, r.Fence, r.State
			sameToken := oldToken.ID == accountToken.ID && oldToken.Epoch == accountToken.Epoch && oldToken.Owner == accountToken.Owner && oldToken.Fence == accountToken.Fence && old.Owner == r.Owner && old.Fence == r.Fence
			recoveredToken := oldToken.ID == accountToken.ID && oldToken.Epoch == accountToken.Epoch && oldToken.Fence < accountToken.Fence && oldToken.Owner != accountToken.Owner && old.Owner == r.Owner && old.Fence < r.Fence
			if !equal(baseline, *r) || (!sameToken && !recoveredToken) {
				return nil, ErrBlocked
			}
		}
		step.Phase = "blocked"
		step.LeaseUntil = cur.LeaseUntil
		run.Steps[indexOf(run, token.StepID)] = step
		run.Revision++
		return step, nil
	})
	if err != nil {
		return records.Step{}, err
	}
	var out records.Step
	return out, decodeResult(v, &out)
}

// Resume returns a reconciled quiescent step to pending. The new claim preserves
// its checkpoint while resetting attempt-local evidence and allocating a new fence.
func (s *Store) Resume(ctx context.Context, key, runID, stepID, checkpointID string, at time.Time) error {
	request := []string{runID, stepID, checkpointID}
	_, err := s.transact(ctx, key, request, at, runID, stepID, "", "pending", func(d *diskState) (any, error) {
		step, ri, ok := findStep(d, runID, stepID)
		if !ok || step.Phase != "blocked" || d.State.Runs[ri].Cancelled || step.Accepted || !step.StopObserved || step.Checkpoint == nil || step.Checkpoint.ID != checkpointID || !validAttemptCheckpoint(step.Checkpoint, step.Token, d.State.Runs[ri].Manifest.InputRevision, step.Checkpoint.Workspace) || staging.Verify(*step.Checkpoint) != nil {
			return nil, ErrBlocked
		}
		if step.Reservation == nil || (step.Reservation.State != "settled" && step.Reservation.State != "released") {
			return nil, ErrBlocked
		}
		if d.Policy.MaxAttempts > 0 && step.Attempts >= d.Policy.MaxAttempts {
			return nil, ErrBlocked
		}
		step.Phase = "pending"
		step.LeaseUntil = ""
		d.State.Runs[ri].Revision++
		return step, nil
	})
	return err
}

func decodeResult(v any, out any) error {
	b, ok := v.([]byte)
	if !ok {
		b = marshal(v)
	}
	return json.Unmarshal(b, out)
}
func findStep(d *diskState, run, step string) (*records.Step, int, bool) {
	for ri := range d.State.Runs {
		r := &d.State.Runs[ri]
		if r.Manifest.ID == run {
			for si := range r.Steps {
				if r.Steps[si].ID == step {
					return &r.Steps[si], ri, true
				}
			}
		}
	}
	return nil, 0, false
}
func indexOf(r *records.Run, id string) int {
	for i, s := range r.Steps {
		if s.ID == id {
			return i
		}
	}
	return -1
}
func transition(a, b string) bool {
	switch a {
	case "claimed":
		return b == "staged" || b == "reserved" || b == "launch-intent" || b == "blocked" || b == "failed"
	case "staged":
		return b == "reservation-intent" || b == "blocked" || b == "failed"
	case "reservation-intent":
		return b == "reserved" || b == "blocked" || b == "failed"
	case "reserved":
		return b == "launch-intent" || b == "blocked" || b == "failed"
	case "launch-intent":
		return b == "running" || b == "blocked" || b == "failed"
	case "running":
		return b == "awaiting-verification" || b == "blocked" || b == "failed"
	case "awaiting-verification":
		return b == "blocked" || b == "failed"
	}
	return false
}

func activePhase(p string) bool {
	switch p {
	case "claimed", "staged", "reservation-intent", "reserved", "launch-intent", "running", "awaiting-verification":
		return true
	}
	return false
}
func evidenceFrozen(p string) bool {
	switch p {
	case "reserved", "launch-intent", "running", "awaiting-verification", "blocked", "cancelled":
		return true
	}
	return false
}
func sameBoundEvidence(a, b *records.Step) bool {
	return a.Workspace == b.Workspace && equal(a.Checkpoint, b.Checkpoint) && equal(a.Reservation, b.Reservation) && equal(a.AccountingToken, b.AccountingToken) && a.DecisionSHA256 == b.DecisionSHA256 && equal(a.Demand, b.Demand) && a.PreparedSHA256 == b.PreparedSHA256
}
func sameEvidenceExceptCheckpoint(a, b *records.Step) bool {
	x, y := *a, *b
	x.Checkpoint = nil
	y.Checkpoint = nil
	return sameBoundEvidence(&x, &y)
}
func validProgressEvidence(cur, next *records.Step, phase string, cancelled bool) bool {
	if cur.StopObserved && !next.StopObserved {
		return false
	}
	if cur.LaunchPossible != next.LaunchPossible && !(!cur.LaunchPossible && next.LaunchPossible && phase == "launch-intent") {
		return false
	}
	if cur.HostReceiptSHA256 != next.HostReceiptSHA256 && !(cur.HostReceiptSHA256 == "" && next.HostReceiptSHA256 != "" && (phase == "awaiting-verification" || cancelled && phase == "blocked")) {
		return false
	}
	return true
}
func validAttemptCheckpoint(cp *records.Checkpoint, t records.Token, inputRevision, workspace string) bool {
	return cp != nil && cp.Version == records.Version && cp.ID != "" && cp.Quiescent && cp.AttemptID == t.AttemptID && cp.InputRevision == inputRevision && cp.Workspace == workspace && cp.Digest != ""
}
func equal(a, b any) bool          { return digest(marshal(a)) == digest(marshal(b)) }
func parseTime(v string) time.Time { t, _ := time.Parse(time.RFC3339Nano, v); return t }
