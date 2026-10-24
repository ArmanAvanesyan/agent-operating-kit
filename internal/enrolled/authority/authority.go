// Package authority implements a deployable external witness and budget service.
// Its database belongs to an independently administered authority domain, never
// the worker's backup. Implementation is not evidence of that deployment fact.
package authority

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/operations"
	runtimep7 "github.com/ArmanAvanesyan/agent-operating-kit/internal/runtime"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
	_ "modernc.org/sqlite"
)

const Version = "aok-enrolled-authority/v1"
const maxBody = 2 << 20

var ErrBlocked = errors.New("protected authority denied request; qualification, scope, budget or current state unavailable")

type Proof struct {
	Version           string `json:"version"`
	Gate              string `json:"gate"`
	ScopeSHA256       string `json:"scopeSha256"`
	MeasurementSHA256 string `json:"measurementSha256"`
	Origin            string `json:"origin"`
	ObservedAt        string `json:"observedAt"`
	ValidUntil        string `json:"validUntil"`
	Issuer            string `json:"issuer"`
	Signature         string `json:"signature"`
}
type Issuer struct {
	ID        string `json:"id"`
	Role      string `json:"role"`
	PublicKey string `json:"publicKey"`
}
type Operation struct {
	ID          string `json:"id"`
	Boundary    string `json:"boundary"`
	InputSHA256 string `json:"inputSha256"`
}

// Enrollment is approved through an externally held operator signing key. Proof
// issuers independently vouch for their measured gates; no caller boolean passes.
type Enrollment struct {
	Version            string               `json:"version"`
	ID                 string               `json:"id"`
	ParentID           string               `json:"parentId"`
	Principal          string               `json:"principal"`
	Scope              runtimep7.TrialScope `json:"scope"`
	Activation         operations.Epoch     `json:"activation"`
	Operations         []Operation          `json:"operations"`
	Bindings           map[string]string    `json:"bindings"`
	Proofs             []Proof              `json:"proofs"`
	MaximumTrials      int                  `json:"maximumTrials"`
	MaximumConsumption json.Number          `json:"maximumConsumption"`
	MaximumSpend       json.Number          `json:"maximumSpend"`
	IssuedAt           string               `json:"issuedAt"`
	ExpiresAt          string               `json:"expiresAt"`
	Signature          string               `json:"signature"`
}
type Config struct {
	// Anchor is separately administered and retained. Empty never permits serving.
	Anchor                  Anchor
	InitialAnchorSHA256     string
	AllowUnanchoredForTests bool
	Database                string
	ServerID                string
	OperatorKey             ed25519.PublicKey
	ResponseKey             ed25519.PrivateKey
	Issuers                 []Issuer
	Enrollments             []Enrollment
	// Tokens are held by the authority process, outside task data and replies.
	Tokens map[string]string
}
type Store struct {
	db       *sql.DB
	cfg      Config
	mu       sync.Mutex
	clock    func() time.Time
	starting bool
}
type Request struct {
	Version      string            `json:"version"`
	Nonce        string            `json:"nonce"`
	EnrollmentID string            `json:"enrollmentId"`
	Action       string            `json:"action"`
	ScopeSHA256  string            `json:"scopeSha256"`
	LogicalID    string            `json:"logicalId"`
	Boundary     string            `json:"boundary"`
	InputSHA256  string            `json:"inputSha256"`
	Generation   uint64            `json:"generation"`
	Bindings     map[string]string `json:"bindings"`
}
type Response struct {
	Version       string           `json:"version"`
	ServerID      string           `json:"serverId"`
	Nonce         string           `json:"nonce"`
	RequestSHA256 string           `json:"requestSha256"`
	ObservedAt    string           `json:"observedAt"`
	ValidUntil    string           `json:"validUntil"`
	Epoch         operations.Epoch `json:"epoch"`
	Ordinal       int64            `json:"ordinal"`
	LeaseID       string           `json:"leaseId"`
	Signature     string           `json:"signature"`
}

func validHash(s string) bool {
	b, e := hex.DecodeString(s)
	return e == nil && len(b) == 32 && s == strings.ToLower(s)
}
func validID(s string) bool {
	return s != "" && len(s) <= 256 && !strings.ContainsAny(s, "\x00\r\n/\\")
}
func unsigned(v any) []byte { b, _ := json.Marshal(v); return b }
func enrollmentPayload(e Enrollment) []byte {
	e.Signature = ""
	return append([]byte("aok-enrollment:authority:v1\n"), unsigned(e)...)
}
func proofPayload(p Proof) []byte {
	p.Signature = ""
	return append([]byte("aok-enrollment:proof:v1\n"), unsigned(p)...)
}
func responsePayload(r Response) []byte {
	r.Signature = ""
	return append([]byte("aok-authority:response:v1\n"), unsigned(r)...)
}
func SignEnrollment(e Enrollment, key ed25519.PrivateKey) Enrollment {
	e.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(key, enrollmentPayload(e)))
	return e
}
func SignProof(p Proof, key ed25519.PrivateKey) Proof {
	p.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(key, proofPayload(p)))
	return p
}

func requiredGates(e Enrollment) []string {
	gates := []string{"controls", "identity", "capability", "capacity", "verification", "runtime", "authorization", "platform", "provider-read", "provider-operation", "witness", "budget", "stop-monitor"}
	if e.Scope.Purpose == "controlled-evidence-collection" {
		gates = append(gates, "quality")
	}
	return gates
}

// ValidateEnrollment checks signatures and freshness without opening a database or resolving secrets.
func ValidateEnrollment(e Enrollment, operator ed25519.PublicKey, issuers []Issuer, at time.Time) error {
	return verify(e, Config{OperatorKey: operator, Issuers: issuers}, at)
}

func verify(e Enrollment, c Config, at time.Time) error {
	if b, err := json.Marshal(e); err != nil || len(b) > maxBody || len(e.Proofs) > 100 {
		return ErrBlocked
	}
	if e.Version != Version || !validID(e.ID) || !validID(e.ParentID) || !validID(e.Principal) || !validHash(e.Activation.ScopeSHA256) || e.Activation.Authority == "" || e.Activation.Holder == "" || e.Activation.Generation < 1 || len(e.Operations) < 1 || len(e.Operations) > 1000 || len(e.Bindings) == 0 || len(e.Bindings) > 100 || e.MaximumTrials < 1 || e.MaximumTrials > 10000 {
		return ErrBlocked
	}
	if e.Scope.Purpose != "calibration-experiment" && e.Scope.Purpose != "controlled-evidence-collection" {
		return ErrBlocked
	}
	for _, h := range []string{e.Scope.EnrollmentSHA256, e.Scope.GrantSHA256, e.Scope.RouteSHA256, e.Scope.InputSHA256} {
		if !validHash(h) {
			return ErrBlocked
		}
	}
	if !validID(e.Scope.TrialID) || !validID(e.Scope.RunID) || !validRepository(e.Scope.Repository) || e.Scope.Account == "" || e.Scope.MaximumSeconds < 1 || e.Scope.MaximumSeconds > 60 || e.Scope.ConsumptionUnit == "" || e.Scope.Currency == "" {
		return ErrBlocked
	}
	for _, n := range []json.Number{e.MaximumConsumption, e.MaximumSpend, e.Scope.MaximumConsumption, e.Scope.MaximumSpend} {
		r, err := usage.NumberRat(n)
		if err != nil || r.Sign() < 0 {
			return ErrBlocked
		}
	}
	ec, _ := usage.NumberRat(e.MaximumConsumption)
	sc, _ := usage.NumberRat(e.Scope.MaximumConsumption)
	es, _ := usage.NumberRat(e.MaximumSpend)
	ss, _ := usage.NumberRat(e.Scope.MaximumSpend)
	if ec.Sign() <= 0 || sc.Sign() <= 0 || sc.Cmp(ec) > 0 || ss.Cmp(es) > 0 {
		return ErrBlocked
	}
	start, se := time.Parse(time.RFC3339Nano, e.IssuedAt)
	until, ue := time.Parse(time.RFC3339Nano, e.ExpiresAt)
	if se != nil || ue != nil || start.After(at) || !at.Before(until) || until.Sub(start) > 24*time.Hour {
		return ErrBlocked
	}
	sig, err := base64.StdEncoding.DecodeString(e.Signature)
	if err != nil || len(c.OperatorKey) != ed25519.PublicKeySize || !ed25519.Verify(c.OperatorKey, enrollmentPayload(e), sig) {
		return ErrBlocked
	}
	issuers := map[string]Issuer{}
	for _, i := range c.Issuers {
		if issuers[i.ID].ID != "" {
			return ErrBlocked
		}
		issuers[i.ID] = i
	}
	seen := map[string]bool{}
	for _, p := range e.Proofs {
		i, ok := issuers[p.Issuer]
		key, ke := base64.StdEncoding.DecodeString(i.PublicKey)
		signature, de := base64.StdEncoding.DecodeString(p.Signature)
		observed, oe := time.Parse(time.RFC3339Nano, p.ObservedAt)
		expiry, ee := time.Parse(time.RFC3339Nano, p.ValidUntil)
		if !ok || ke != nil || de != nil || len(key) != ed25519.PublicKeySize || p.Version != Version || i.Role != p.Gate+"-issuer" || p.Issuer == e.Principal || p.Origin != "live" || p.ScopeSHA256 != usage.Digest(e.Scope) || !validHash(p.MeasurementSHA256) || oe != nil || ee != nil || observed.After(at) || !at.Before(expiry) || expiry.Sub(observed) > 5*time.Minute || seen[p.Gate] || !ed25519.Verify(ed25519.PublicKey(key), proofPayload(p), signature) {
			return ErrBlocked
		}
		seen[p.Gate] = true
	}
	for _, g := range requiredGates(e) {
		if !seen[g] {
			return ErrBlocked
		}
	}
	ids := map[string]bool{}
	for _, o := range e.Operations {
		key := usage.Digest(struct{ ID, Boundary string }{o.ID, o.Boundary})
		if !validID(o.ID) || !validHash(o.InputSHA256) || (o.Boundary != "host-launch" && o.Boundary != "provider-send") || ids[key] {
			return ErrBlocked
		}
		ids[key] = true
	}
	for name, h := range e.Bindings {
		if !validID(name) || !validHash(h) {
			return ErrBlocked
		}
	}
	return nil
}

// Open verifies signed enrollments and opens durable authority-owned state. It
// never creates issuer keys, qualifies a deployment, or imports a worker backup.
func Open(c Config) (*Store, error) {
	if (!c.AllowUnanchoredForTests && (c.Anchor == nil || !validHash(c.InitialAnchorSHA256))) || !validID(c.ServerID) || len(c.OperatorKey) != ed25519.PublicKeySize || len(c.ResponseKey) != ed25519.PrivateKeySize || len(c.Enrollments) < 1 || len(c.Enrollments) > 1000 {
		return nil, ErrBlocked
	}
	raw, err := json.Marshal(c.Enrollments)
	if err != nil {
		return nil, err
	}
	var captured []Enrollment
	if usage.Decode(raw, &captured) != nil {
		return nil, ErrBlocked
	}
	c.Enrollments = captured
	c.OperatorKey = append(ed25519.PublicKey(nil), c.OperatorKey...)
	c.ResponseKey = append(ed25519.PrivateKey(nil), c.ResponseKey...)
	c.Issuers = append([]Issuer(nil), c.Issuers...)
	tokens := map[string]string{}
	seenTokens := map[[32]byte]bool{}
	for p, t := range c.Tokens {
		h := sha256.Sum256([]byte(t))
		if seenTokens[h] {
			return nil, ErrBlocked
		}
		seenTokens[h] = true
		if !validID(p) || len(t) < 24 || len(t) > 4096 || strings.ContainsAny(t, "\r\n") {
			return nil, ErrBlocked
		}
		tokens[p] = t
	}
	c.Tokens = tokens
	for _, e := range c.Enrollments {
		if err = verify(e, c, time.Now().UTC()); err != nil {
			return nil, err
		}
		if c.Tokens[e.Principal] == "" {
			return nil, ErrBlocked
		}
	}
	path, err := filepath.Abs(c.Database)
	if err != nil {
		return nil, err
	}
	for p := filepath.Dir(path); ; p = filepath.Dir(p) {
		st, e := os.Lstat(p)
		if e != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
			return nil, ErrBlocked
		}
		if filepath.Dir(p) == p {
			break
		}
	}
	st, err := os.Lstat(filepath.Dir(path))
	if err != nil || st.Mode().Perm()&0077 != 0 {
		return nil, errors.New("authority database requires a private existing directory")
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if st, e := os.Lstat(path + suffix); e == nil && (!st.Mode().IsRegular() || st.Mode()&os.ModeSymlink != 0 || st.Mode().Perm()&0077 != 0) {
			return nil, ErrBlocked
		} else if e != nil && !os.IsNotExist(e) {
			return nil, e
		}
	}
	created := false
	if f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600); e == nil {
		created = true
		f.Close()
	} else if !os.IsExist(e) {
		return nil, e
	}
	db, err := sql.Open("sqlite", (&url.URL{Scheme: "file", Path: path, RawQuery: "_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(FULL)"}).String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	for _, q := range []string{
		`CREATE TABLE IF NOT EXISTS nonces (enrollment TEXT NOT NULL, nonce TEXT NOT NULL, PRIMARY KEY(enrollment,nonce))`,
		`CREATE TABLE IF NOT EXISTS anchor_commit (id INTEGER PRIMARY KEY CHECK(id=1), value TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS anchor_tip (id INTEGER PRIMARY KEY CHECK(id=1), tip TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS identity (id INTEGER PRIMARY KEY CHECK(id=1), server TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS clock (id INTEGER PRIMARY KEY CHECK(id=1), lastat TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS budget (parent TEXT PRIMARY KEY, trials INTEGER NOT NULL, consumption TEXT NOT NULL, spend TEXT NOT NULL, countcap INTEGER NOT NULL, consumptioncap TEXT NOT NULL, spendcap TEXT NOT NULL, unit TEXT NOT NULL, currency TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS consumed (parent TEXT NOT NULL, logical TEXT NOT NULL, boundary TEXT NOT NULL, input TEXT NOT NULL, PRIMARY KEY(parent,logical,boundary))`,
		`CREATE TABLE IF NOT EXISTS epochs (authority TEXT PRIMARY KEY, generation INTEGER NOT NULL, scope TEXT NOT NULL, holder TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS stops (parent TEXT PRIMARY KEY, reason TEXT NOT NULL, observed TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS captures (parent TEXT NOT NULL, source TEXT NOT NULL, ordinal INTEGER NOT NULL, lease TEXT NOT NULL, untiltime TEXT NOT NULL, PRIMARY KEY(parent,source))`,
		`CREATE TABLE IF NOT EXISTS capture_results (parent TEXT NOT NULL, source TEXT NOT NULL, ordinal INTEGER NOT NULL, lease TEXT NOT NULL, result_sha256 TEXT NOT NULL, completed TEXT NOT NULL, PRIMARY KEY(parent,source,ordinal))`,
	} {
		if _, err = db.Exec(q); err != nil {
			db.Close()
			return nil, err
		}
	}
	_ = os.Chmod(path, 0600)
	if _, err = db.Exec(`INSERT OR IGNORE INTO identity VALUES(1,?)`, c.ServerID); err != nil {
		db.Close()
		return nil, err
	}
	var id string
	if db.QueryRow(`SELECT server FROM identity WHERE id=1`).Scan(&id) != nil || id != c.ServerID {
		db.Close()
		return nil, ErrBlocked
	}
	if c.Anchor != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err = db.Exec(`INSERT OR IGNORE INTO anchor_tip VALUES(1,?)`, c.InitialAnchorSHA256)
		var tip string
		if err != nil || db.QueryRow(`SELECT tip FROM anchor_tip WHERE id=1`).Scan(&tip) != nil {
			db.Close()
			return nil, ErrBlocked
		}
		current, ae := c.Anchor.Current(ctx, c.ServerID)
		if ae != nil || current != tip || (!created && verifyState(ctx, db, tip) != nil) {
			db.Close()
			return nil, ErrBlocked
		}
	}
	s := &Store{db: db, cfg: c, clock: time.Now, starting: true}
	if err = s.install(); err != nil {
		db.Close()
		return nil, err
	}
	if c.Anchor != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		first := c.Enrollments[0]
		n := make([]byte, 16)
		if _, err = rand.Read(n); err != nil {
			db.Close()
			return nil, err
		}
		_, err = s.process(ctx, Request{Version: Version, Nonce: hex.EncodeToString(n), EnrollmentID: first.ID, Action: "current", ScopeSHA256: usage.Digest(first.Scope), InputSHA256: usage.Digest(c.Enrollments)}, first.Principal)
		if err != nil {
			db.Close()
			return nil, ErrBlocked
		}
	}
	s.starting = false
	return s, nil
}
func (s *Store) Close() error { return s.db.Close() }
func (s *Store) install() error {
	tx, e := s.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	seen := map[string]bool{}
	for _, en := range s.cfg.Enrollments {
		if seen[en.ID] {
			return ErrBlocked
		}
		seen[en.ID] = true
		_, e = tx.Exec(`INSERT OR IGNORE INTO budget VALUES(?,0,'0','0',?,?,?,?,?)`, en.ParentID, en.MaximumTrials, string(en.MaximumConsumption), string(en.MaximumSpend), en.Scope.ConsumptionUnit, en.Scope.Currency)
		if e != nil {
			return e
		}
		var n int
		var cap, spend, unit, currency string
		if tx.QueryRow(`SELECT countcap,consumptioncap,spendcap,unit,currency FROM budget WHERE parent=?`, en.ParentID).Scan(&n, &cap, &spend, &unit, &currency) != nil || n != en.MaximumTrials || cap != string(en.MaximumConsumption) || spend != string(en.MaximumSpend) || unit != en.Scope.ConsumptionUnit || currency != en.Scope.Currency {
			return ErrBlocked
		}
		var generation uint64
		var scope, holder string
		e = tx.QueryRow(`SELECT generation,scope,holder FROM epochs WHERE authority=?`, en.Activation.Authority).Scan(&generation, &scope, &holder)
		if e == sql.ErrNoRows {
			_, e = tx.Exec(`INSERT INTO epochs VALUES(?,?,?,?)`, en.Activation.Authority, en.Activation.Generation, en.Activation.ScopeSHA256, en.Activation.Holder)
		} else if e == nil {
			if en.Activation.Generation < generation || en.Activation.Generation == generation && (en.Activation.ScopeSHA256 != scope || en.Activation.Holder != holder) {
				return ErrBlocked
			}
			if en.Activation.Generation > generation {
				_, e = tx.Exec(`UPDATE epochs SET generation=?,scope=?,holder=? WHERE authority=?`, en.Activation.Generation, en.Activation.ScopeSHA256, en.Activation.Holder, en.Activation.Authority)
			}
		}
		if e != nil {
			return e
		}
	}
	return tx.Commit()
}
func (s *Store) enrollment(id string) (Enrollment, bool) {
	for _, e := range s.cfg.Enrollments {
		if e.ID == id {
			return e, true
		}
	}
	return Enrollment{}, false
}

func (s *Store) process(ctx context.Context, r Request, principal string) (Response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.clock().UTC()
	var out Response
	en, ok := s.enrollment(r.EnrollmentID)
	if !ok || en.Principal != principal || r.Version != Version || !validID(r.Nonce) || r.ScopeSHA256 != usage.Digest(en.Scope) || verify(en, s.cfg, now) != nil {
		return out, ErrBlocked
	}
	expires, _ := time.Parse(time.RFC3339Nano, en.ExpiresAt)
	until := now.Add(time.Minute)
	if expires.Before(until) {
		until = expires
	}
	for _, p := range en.Proofs {
		t, _ := time.Parse(time.RFC3339Nano, p.ValidUntil)
		if t.Before(until) {
			until = t
		}
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	var anchorPrior string
	if s.cfg.Anchor != nil {
		if tx.QueryRowContext(ctx, `SELECT tip FROM anchor_tip WHERE id=1`).Scan(&anchorPrior) != nil {
			return out, ErrBlocked
		}
		current, err := s.cfg.Anchor.Current(ctx, s.cfg.ServerID)
		if err != nil || current != anchorPrior || (!s.starting && verifyState(ctx, tx, anchorPrior) != nil) {
			return out, ErrBlocked
		}
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO nonces VALUES(?,?)`, en.ID, r.Nonce); e != nil {
		return out, ErrBlocked
	}
	var prior string
	ce := tx.QueryRowContext(ctx, `SELECT lastat FROM clock WHERE id=1`).Scan(&prior)
	if ce != nil && ce != sql.ErrNoRows {
		return out, ce
	}
	if prior != "" {
		last, le := time.Parse(time.RFC3339Nano, prior)
		if le != nil || now.Before(last) {
			return out, ErrBlocked
		}
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO clock VALUES(1,?) ON CONFLICT(id) DO UPDATE SET lastat=excluded.lastat`, now.Format(time.RFC3339Nano)); e != nil {
		return out, e
	}
	var gen uint64
	var scope, holder string
	if tx.QueryRowContext(ctx, `SELECT generation,scope,holder FROM epochs WHERE authority=?`, en.Activation.Authority).Scan(&gen, &scope, &holder) != nil || gen != en.Activation.Generation || scope != en.Activation.ScopeSHA256 || holder != en.Activation.Holder {
		return out, ErrBlocked
	}
	out = Response{Version: Version, ServerID: s.cfg.ServerID, Nonce: r.Nonce, RequestSHA256: usage.Digest(r), ObservedAt: now.Format(time.RFC3339Nano), ValidUntil: until.Format(time.RFC3339Nano), Epoch: operations.Epoch{Authority: en.Activation.Authority, Holder: holder, ScopeSHA256: scope, Generation: gen, ObservedAt: now, ValidUntil: until}}
	var stopped int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM stops WHERE parent=?`, en.ParentID).Scan(&stopped); err != nil || (stopped != 0 && r.Action != "stop") {
		return out, ErrBlocked
	}
	switch r.Action {
	case "stop":
		if !validHash(r.InputSHA256) {
			return out, ErrBlocked
		}
		_, e = tx.ExecContext(ctx, `INSERT OR IGNORE INTO stops VALUES(?,?,?)`, en.ParentID, r.InputSHA256, now.Format(time.RFC3339Nano))
	case "check":
		if len(r.Bindings) != len(en.Bindings) {
			return out, ErrBlocked
		}
		for name, hash := range r.Bindings {
			if !validID(name) || !validHash(hash) || en.Bindings[name] != hash {
				return out, ErrBlocked
			}
		}
	case "budget":
		if r.LogicalID != en.Scope.TrialID {
			return out, ErrBlocked
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO consumed VALUES(?,?,?,?)`, en.ParentID, r.LogicalID, "budget", en.Scope.InputSHA256); e != nil {
			return out, ErrBlocked
		}
		var count, max int
		var used, spent, cap, spendcap string
		if tx.QueryRowContext(ctx, `SELECT trials,consumption,spend,countcap,consumptioncap,spendcap FROM budget WHERE parent=?`, en.ParentID).Scan(&count, &used, &spent, &max, &cap, &spendcap) != nil {
			return out, ErrBlocked
		}
		u, _ := usage.NumberRat(json.Number(used))
		v, _ := usage.NumberRat(en.Scope.MaximumConsumption)
		u.Add(u, v)
		c, _ := usage.NumberRat(json.Number(cap))
		sp, _ := usage.NumberRat(json.Number(spent))
		v, _ = usage.NumberRat(en.Scope.MaximumSpend)
		sp.Add(sp, v)
		sc, _ := usage.NumberRat(json.Number(spendcap))
		if count >= max || u.Cmp(c) > 0 || sp.Cmp(sc) > 0 {
			return out, ErrBlocked
		}
		_, e = tx.ExecContext(ctx, `UPDATE budget SET trials=?,consumption=?,spend=? WHERE parent=?`, count+1, usage.Decimal(u), usage.Decimal(sp), en.ParentID)
	case "trial":
		if r.LogicalID != en.Scope.TrialID || !validHash(r.InputSHA256) || r.InputSHA256 != en.Bindings["trial-scope"] {
			return out, ErrBlocked
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO consumed VALUES(?,?,?,?)`, en.ParentID, r.LogicalID, "trial", r.InputSHA256)
	case "current":
	case "consume":
		if r.Generation != gen {
			return out, ErrBlocked
		}
		allowed := false
		for _, o := range en.Operations {
			if o.ID == r.LogicalID && o.Boundary == r.Boundary && o.InputSHA256 == r.InputSHA256 {
				allowed = true
			}
		}
		if !allowed {
			return out, ErrBlocked
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO consumed VALUES(?,?,?,?)`, en.ParentID, r.LogicalID, r.Boundary, r.InputSHA256)
	case "capture":
		source := r.Bindings["source"]
		if !validHash(source) || en.Bindings["source"] != source {
			return out, ErrBlocked
		}
		captureAccount := usage.Digest(struct{ Account, Source string }{en.Scope.Account, source})
		var ordinal int64
		var lease, oldUntil string
		err := tx.QueryRowContext(ctx, `SELECT ordinal,lease,untiltime FROM captures WHERE parent=? AND source=?`, captureAccount, source).Scan(&ordinal, &lease, &oldUntil)
		if err != nil && err != sql.ErrNoRows {
			return out, err
		}
		expiry, _ := time.Parse(time.RFC3339Nano, oldUntil)
		if lease != "" && now.Before(expiry) {
			return out, ErrBlocked
		}
		lease = r.Nonce
		ordinal++
		out.Ordinal = ordinal
		out.LeaseID = lease
		_, e = tx.ExecContext(ctx, `INSERT INTO captures VALUES(?,?,?,?,?) ON CONFLICT(parent,source) DO UPDATE SET ordinal=excluded.ordinal,lease=excluded.lease,untiltime=excluded.untiltime`, captureAccount, source, ordinal, lease, now.Add(5*time.Second).Format(time.RFC3339Nano))
	case "capture-check":
		source := r.Bindings["source"]
		if !validHash(source) || en.Bindings["source"] != source || r.Generation < 1 || r.LogicalID == "" {
			return out, ErrBlocked
		}
		captureAccount := usage.Digest(struct{ Account, Source string }{en.Scope.Account, source})
		var ordinal int64
		var lease, leaseUntil string
		if tx.QueryRowContext(ctx, `SELECT ordinal,lease,untiltime FROM captures WHERE parent=? AND source=?`, captureAccount, source).Scan(&ordinal, &lease, &leaseUntil) != nil || ordinal != int64(r.Generation) || lease != r.LogicalID {
			return out, ErrBlocked
		}
		expires, pe := time.Parse(time.RFC3339Nano, leaseUntil)
		if pe != nil || !now.Before(expires) {
			return out, ErrBlocked
		}
	case "capture-complete":
		source := r.Bindings["source"]
		if en.Bindings["source"] != source || r.LogicalID == "" || r.Generation < 1 || !validHash(r.InputSHA256) {
			return out, ErrBlocked
		}
		captureAccount := usage.Digest(struct{ Account, Source string }{en.Scope.Account, source})
		var ordinal int64
		var lease string
		if tx.QueryRowContext(ctx, `SELECT ordinal,lease FROM captures WHERE parent=? AND source=?`, captureAccount, source).Scan(&ordinal, &lease) != nil || ordinal != int64(r.Generation) || lease != r.LogicalID {
			return out, ErrBlocked
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO capture_results(parent,source,ordinal,lease,result_sha256,completed) VALUES(?,?,?,?,?,?)`, captureAccount, source, ordinal, lease, r.InputSHA256, now.Format(time.RFC3339Nano)); e != nil {
			return out, ErrBlocked
		}
		result, er := tx.ExecContext(ctx, `UPDATE captures SET lease='' WHERE parent=? AND source=? AND lease=?`, captureAccount, source, r.LogicalID)
		if er != nil {
			return out, er
		}
		n, _ := result.RowsAffected()
		if n != 1 {
			return out, ErrBlocked
		}
	default:
		return out, ErrBlocked
	}
	if e != nil {
		return out, ErrBlocked
	}
	if s.cfg.Anchor != nil {
		next, ce := commitState(ctx, tx, anchorPrior, usage.Digest(r), now.Format(time.RFC3339Nano))
		if ce != nil {
			return out, ErrBlocked
		}
		if e = s.cfg.Anchor.Advance(ctx, s.cfg.ServerID, anchorPrior, next); e != nil {
			return out, ErrBlocked
		}
		if _, e = tx.ExecContext(ctx, `UPDATE anchor_tip SET tip=? WHERE id=1`, next); e != nil {
			return out, ErrBlocked
		}
	}
	if e = tx.Commit(); e != nil {
		return out, e
	}
	out.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(s.cfg.ResponseKey, responsePayload(out)))
	return out, nil
}

func (s *Store) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil || r.Method != "POST" || r.URL.Path != "/v1/authority" || r.URL.RawQuery != "" {
			http.Error(w, "request denied", http.StatusBadRequest)
			return
		}
		principal := ""
		got := sha256.Sum256([]byte(r.Header.Get("Authorization")))
		for p, t := range s.cfg.Tokens {
			want := sha256.Sum256([]byte("Bearer " + t))
			if subtle.ConstantTimeCompare(got[:], want[:]) == 1 {
				principal = p
			}
		}
		if principal == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		raw, e := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
		var req Request
		if e != nil || len(raw) > maxBody || usage.Decode(raw, &req) != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		out, e := s.process(ctx, req, principal)
		if e != nil {
			http.Error(w, "authority unavailable or request blocked", http.StatusConflict)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	})
}

type Client struct {
	Endpoint, ServerID, EnrollmentID string
	Scope                            runtimep7.TrialScope
	PublicKey                        ed25519.PublicKey
	Token                            func(context.Context) (string, error)
	Transport                        http.RoundTripper
}

func (c Client) Call(ctx context.Context, r Request) (Response, error) {
	var out Response
	u, e := url.Parse(c.Endpoint)
	if ctx == nil || e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Path != "/v1/authority" || u.RawQuery != "" || u.Fragment != "" || c.Token == nil || len(c.PublicKey) != ed25519.PublicKeySize {
		return out, ErrBlocked
	}
	nonce := make([]byte, 16)
	if _, e = rand.Read(nonce); e != nil {
		return out, e
	}
	r.Version = Version
	r.Nonce = hex.EncodeToString(nonce)
	r.EnrollmentID = c.EnrollmentID
	r.ScopeSHA256 = usage.Digest(c.Scope)
	raw, e := json.Marshal(r)
	if e != nil || len(raw) > maxBody {
		return out, ErrBlocked
	}
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	token, e := c.Token(bounded)
	if e != nil || len(token) < 24 || strings.ContainsAny(token, "\r\n") {
		return out, ErrBlocked
	}
	request, e := http.NewRequestWithContext(bounded, "POST", c.Endpoint, strings.NewReader(string(raw)))
	if e != nil {
		return out, e
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	client := http.Client{Transport: c.Transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, e := client.Do(request)
	if e != nil {
		return out, ErrBlocked
	}
	defer resp.Body.Close()
	body, e := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if e != nil || resp.StatusCode != 200 || len(body) > maxBody || strings.Contains(string(body), token) || usage.Decode(body, &out) != nil {
		return out, ErrBlocked
	}
	observed, oe := time.Parse(time.RFC3339Nano, out.ObservedAt)
	expires, ee := time.Parse(time.RFC3339Nano, out.ValidUntil)
	now := time.Now().UTC()
	sig, se := base64.StdEncoding.DecodeString(out.Signature)
	if out.Version != Version || out.ServerID != c.ServerID || out.Nonce != r.Nonce || out.RequestSHA256 != usage.Digest(r) || oe != nil || ee != nil || observed.After(now.Add(time.Second)) || now.Sub(observed) > 5*time.Second || !now.Before(expires) || expires.Sub(observed) > time.Minute || se != nil || !ed25519.Verify(c.PublicKey, responsePayload(out), sig) {
		return Response{}, ErrBlocked
	}
	return out, nil
}

// Status never asserts deployment qualification or production eligibility.
func Status() map[string]any {
	return map[string]any{"version": Version, "implementation": "implemented", "credentialGate": "external-enrollment-required", "liveGate": "unqualified", "productionActivation": "disabled", "authorizationGranted": false, "executionReady": false}
}

var repoPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,99}/[A-Za-z0-9][A-Za-z0-9_.-]{0,99}$`)

func validRepository(s string) bool {
	if !repoPattern.MatchString(s) {
		return false
	}
	for _, p := range strings.Split(s, "/") {
		if p == "." || p == ".." {
			return false
		}
	}
	return true
}
