package writeback

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const workkitApplicationID = 1464553301
const WorkkitStoreVersion = "workkit-operations/v1"

const workkitSchemaSQL = `CREATE TABLE workkit_identity(singleton INTEGER PRIMARY KEY CHECK(singleton=1),id TEXT NOT NULL,path TEXT NOT NULL,namespace TEXT NOT NULL,owner TEXT NOT NULL); CREATE TABLE workkit_plans(id TEXT PRIMARY KEY,digest TEXT NOT NULL,payload BLOB NOT NULL,kind TEXT NOT NULL); CREATE TABLE workkit_routes(id TEXT PRIMARY KEY,revision INTEGER NOT NULL,payload BLOB NOT NULL); CREATE TABLE workkit_field_owners(target TEXT PRIMARY KEY,route TEXT NOT NULL); CREATE TABLE workkit_evidence(operation_id TEXT NOT NULL,digest TEXT NOT NULL,payload BLOB NOT NULL,PRIMARY KEY(operation_id,digest)); CREATE TABLE workkit_local_intents(id TEXT PRIMARY KEY,digest TEXT NOT NULL,state TEXT NOT NULL);`

type WorkkitStoreIdentity struct {
	Version   string `json:"version"`
	Kind      string `json:"kind"`
	Path      string `json:"path"`
	ID        string `json:"id"`
	Namespace string `json:"namespace"`
	Owner     string `json:"owner"`
}
type WorkkitJournal struct {
	integrationCommitGate func(*sql.Tx) error
	Box                   *Outbox
	Identity              WorkkitStoreIdentity
}

// OpenWorkkit uses the existing outbox/CAS/nonce/clock implementation with a
// distinct application identity. It never upgrades or opens a legacy AOK store.
func OpenWorkkit(ctx context.Context, path, namespace, owner string, initialize bool) (*WorkkitJournal, error) {
	return openWorkkitFormat(ctx, path, namespace, owner, initialize, workkitApplicationID, WorkkitStoreVersion, "")
}
func openWorkkitFormat(ctx context.Context, path, namespace, owner string, initialize bool, app int, version, extra string) (*WorkkitJournal, error) {
	abs, e := filepath.Abs(path)
	if e != nil || path != abs || namespace == "" || owner == "" {
		return nil, errors.New("canonical journal path/namespace/owner required")
	}
	for p := filepath.Dir(abs); ; p = filepath.Dir(p) {
		st, e := os.Lstat(p)
		if e != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("unsafe journal ancestor")
		}
		if p == filepath.Dir(p) {
			break
		}
	}
	if initialize {
		f, e := os.OpenFile(abs, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
		if e != nil {
			return nil, e
		}
		f.Close()
	}
	var box *Outbox
	if initialize {
		box, e = openProfile(abs, app)
	} else {
		box, e = openReadProfile(abs, app)
	}
	if e != nil {
		return nil, e
	}
	fail := func(e error) (*WorkkitJournal, error) { box.Close(); return nil, e }
	if initialize {
		_, e = box.db.ExecContext(ctx, workkitSchemaSQL+extra)
		if e != nil {
			return fail(e)
		}
		var id [32]byte
		if _, e = rand.Read(id[:]); e != nil {
			return fail(e)
		}
		_, e = box.db.ExecContext(ctx, `INSERT INTO workkit_identity VALUES(1,?,?,?,?)`, hex.EncodeToString(id[:]), abs, namespace, owner)
		if e != nil {
			return fail(e)
		}
	}
	if e = validateWorkkitSchemaExtra(box.db, extra); e != nil {
		return fail(e)
	}
	identity := WorkkitStoreIdentity{Version: version, Kind: "IntegrationJournal"}
	e = box.db.QueryRowContext(ctx, `SELECT id,path,namespace,owner FROM workkit_identity WHERE singleton=1`).Scan(&identity.ID, &identity.Path, &identity.Namespace, &identity.Owner)
	if e != nil || identity.Path != abs || identity.Namespace != namespace || identity.Owner != owner {
		return fail(errors.New("journal binding mismatch; copies and ownership transfer require explicit migration"))
	}
	return &WorkkitJournal{Box: box, Identity: identity}, nil
}

// OpenWorkkitWritable reopens an already bound WorkKit store without initializing
// schema or adopting a copied/legacy store. The caller still needs current trust.
func OpenWorkkitWritable(ctx context.Context, expected WorkkitStoreIdentity) (*WorkkitJournal, error) {
	if expected.Version != WorkkitStoreVersion {
		return nil, errors.New("exact v1 store required")
	}
	return openWorkkitWritableFormat(ctx, expected, workkitApplicationID, "")
}
func openWorkkitWritableFormat(ctx context.Context, expected WorkkitStoreIdentity, app int, extra string) (*WorkkitJournal, error) {
	ro, e := openWorkkitFormat(ctx, expected.Path, expected.Namespace, expected.Owner, false, app, expected.Version, extra)
	if e != nil {
		return nil, e
	}
	if ro.Identity != expected {
		ro.Box.Close()
		return nil, errors.New("exact store identity required")
	}
	ro.Box.Close()
	uri := url.URL{Scheme: "file", Path: filepath.ToSlash(expected.Path)}
	db, e := sql.Open("sqlite", uri.String())
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	box := &Outbox{db: db}
	if e = validateStoreIdentityProfile(db, false, app); e == nil {
		e = validateWorkkitSchemaExtra(db, extra)
	}
	if e != nil {
		db.Close()
		return nil, e
	}
	if e != nil {
		return nil, e
	}
	if _, e = db.ExecContext(ctx, `PRAGMA journal_mode=WAL; PRAGMA synchronous=FULL; PRAGMA busy_timeout=5000`); e != nil {
		db.Close()
		return nil, e
	}
	j := &WorkkitJournal{Box: box, Identity: expected}
	var id, path, ns, owner string
	e = box.db.QueryRowContext(ctx, `SELECT id,path,namespace,owner FROM workkit_identity WHERE singleton=1`).Scan(&id, &path, &ns, &owner)
	if e != nil || id != expected.ID || path != expected.Path || ns != expected.Namespace || owner != expected.Owner {
		box.Close()
		return nil, errors.New("store changed while opening")
	}
	return j, nil
}
func (j *WorkkitJournal) Close() error { return j.Box.Close() }
func (j *WorkkitJournal) PutPlan(ctx context.Context, id, digest, kind string, payload []byte) error {
	if id == "" || digest != DigestBytes(payload) || len(payload) > 4<<20 {
		return errors.New("invalid pinned plan")
	}
	var old []byte
	var hash string
	e := j.Box.db.QueryRowContext(ctx, `SELECT digest,payload FROM workkit_plans WHERE id=?`, id).Scan(&hash, &old)
	if e == nil {
		if hash != digest || !bytes.Equal(old, payload) {
			return errors.New("same logical ID with changed bytes")
		}
		return nil
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return e
	}
	_, e = j.Box.db.ExecContext(ctx, `INSERT INTO workkit_plans VALUES(?,?,?,?)`, id, digest, payload, kind)
	return e
}
func (j *WorkkitJournal) Plan(ctx context.Context, id string) ([]byte, error) {
	var raw []byte
	e := j.Box.db.QueryRowContext(ctx, `SELECT payload FROM workkit_plans WHERE id=? AND length(payload)<=4194304`, id).Scan(&raw)
	if len(raw) > 4<<20 {
		return nil, errors.New("plan bound")
	}
	return raw, e
}
func (j *WorkkitJournal) Cancel(ctx context.Context, id string) error {
	res, e := j.Box.db.ExecContext(ctx, `UPDATE outbox SET state='cancelled',fence=fence+1 WHERE operation_id=? AND attempts=0 AND state IN ('prepared','authorized')`, id)
	if e != nil {
		return e
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return errors.New("operation may have escaped; reconcile without resend")
	}
	return nil
}
func (j *WorkkitJournal) Route(ctx context.Context, id string) (int64, []byte, error) {
	var revision int64
	var raw []byte
	e := j.Box.db.QueryRowContext(ctx, `SELECT revision,payload FROM workkit_routes WHERE id=? AND length(payload)<=4194304`, id).Scan(&revision, &raw)
	if len(raw) > 4<<20 {
		return 0, nil, errors.New("route state bound")
	}
	return revision, raw, e
}

// CommitRoute is local, fenced bookkeeping. Provider intent state remains only
// in the inherited outbox. External consumption must precede this transaction.
func (j *WorkkitJournal) CommitRoute(ctx context.Context, id string, expected int64, payload []byte, logicalID, digest string, targets []string, confirmed []string, now time.Time) error {
	if len(payload) > 4<<20 || len(targets) > 10000 || len(confirmed) > 10000 || logicalID == "" || digest != DigestBytes(payload) {
		return errors.New("route state bounds/digest")
	}
	tx, e := j.Box.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = checkAndAdvanceTrustedClock(tx, now); e != nil {
		return e
	}
	if j.integrationCommitGate != nil {
		if err := j.integrationCommitGate(tx); err != nil {
			return err
		}
	}
	for _, op := range confirmed {
		var state string
		if e = tx.QueryRow(`SELECT state FROM outbox WHERE operation_id=?`, op).Scan(&state); e != nil || state != "readback-verified" {
			return errors.New("baseline requires authoritative confirmed readback")
		}
	}
	if _, e = tx.Exec(`INSERT INTO workkit_local_intents VALUES(?,?,'consumed')`, logicalID, digest); e != nil {
		return errors.New("local operation ID already consumed")
	}
	for _, target := range targets {
		if _, e = tx.Exec(`INSERT INTO workkit_field_owners(target,route) VALUES(?,?) ON CONFLICT(target) DO NOTHING`, target, id); e != nil {
			return e
		}
		var route string
		if e = tx.QueryRow(`SELECT route FROM workkit_field_owners WHERE target=?`, target).Scan(&route); e != nil || route != id {
			return errors.New("target field already owned by another route")
		}
	}
	if expected == 0 {
		_, e = tx.Exec(`INSERT INTO workkit_routes VALUES(?,1,?)`, id, payload)
	} else {
		var res interface{ RowsAffected() (int64, error) }
		res, e = tx.Exec(`UPDATE workkit_routes SET revision=revision+1,payload=? WHERE id=? AND revision=?`, payload, id, expected)
		if e == nil {
			n, _ := res.RowsAffected()
			if n != 1 {
				return errors.New("stale route revision")
			}
		}
	}
	if e != nil {
		return e
	}
	if j.Identity.Version == WorkkitStoreVersionV2 {
		if e = integrationNotify(tx, id, "route-committed", digest, now); e != nil {
			return e
		}
	}
	return tx.Commit()
}
func (j *WorkkitJournal) LocalConsumed(ctx context.Context, id string) bool {
	var n int
	e := j.Box.db.QueryRowContext(ctx, `SELECT count(*) FROM workkit_local_intents WHERE id=?`, id).Scan(&n)
	return e == nil && n > 0
}
func (j *WorkkitJournal) ListPlans(ctx context.Context) ([]string, error) {
	rows, e := j.Box.db.QueryContext(ctx, `SELECT id FROM workkit_plans ORDER BY id LIMIT 10001`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			return nil, e
		}
		ids = append(ids, id)
		if len(ids) > 10000 {
			return nil, errors.New("operation inspection bound")
		}
	}
	return ids, rows.Err()
}

func validateWorkkitSchema(db *sql.DB) error { return validateWorkkitSchemaExtra(db, "") }
func validateWorkkitSchemaExtra(db *sql.DB, extra string) error {
	normalize := func(v string) string {
		return strings.ToLower(strings.Join(strings.Fields(strings.ReplaceAll(v, "IF NOT EXISTS ", "")), " "))
	}
	expected := map[string]string{}
	pattern := regexp.MustCompile(`(?is)^CREATE TABLE(?: IF NOT EXISTS)? ([a-z_]+)\(`)
	for _, stmt := range strings.Split(outboxSchemaSQL+workkitSchemaSQL+extra, ";") {
		stmt = strings.TrimSpace(stmt)
		m := pattern.FindStringSubmatch(stmt)
		if len(m) > 1 {
			expected[m[1]] = normalize(stmt)
		}
	}
	rows, e := db.Query(`SELECT type,name,sql FROM sqlite_master WHERE type IN ('table','trigger') AND name NOT LIKE 'sqlite_%'`)
	if e != nil {
		return e
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var kind, name, source string
		if e = rows.Scan(&kind, &name, &source); e != nil {
			return e
		}
		if kind != "table" || expected[name] == "" || normalize(source) != expected[name] {
			return errors.New("damaged or unknown WorkKit journal schema; no repair or migration")
		}
		count++
	}
	if count != len(expected) {
		return errors.New("missing journal tables; obligations cannot be recreated")
	}
	return rows.Err()
}

// VerifyHistoricalCreateReadback verifies a fresh independently signed observation
// against immutable historical intent only. It creates no send permission and
// intentionally does not require the expired or revoked original send grant.
func VerifyHistoricalCreateReadback(r CreateReadbackReport, p TrustPolicy, i CreateIntent, now time.Time) (VerifiedCreateReadback, error) {
	validated, err := ParseCreateIntent(i)
	if err != nil {
		return VerifiedCreateReadback{}, err
	}
	digest, err := CreateIntentDigest(validated.intent)
	if err != nil {
		return VerifiedCreateReadback{}, err
	}
	return VerifyCreateReadback(r, p, VerifiedCreateIntent{intent: validated.intent, intentDigest: digest}, now)
}

// BindWorkkitAdmission serializes sync admission against pause/revision changes
// in the very transaction that consumes the existing outbox send intent.
// Calls already admitted before a pause retain their uncertainty obligations.
func (o *Outbox) BindWorkkitAdmission(route string, revision int64, plan, id string) error {
	if route == "" || revision < 1 {
		return errors.New("initialized exact route required")
	}
	if err := validateStoreIdentityProfile(o.db, false, workkitApplicationID); err != nil {
		return err
	}
	o.workkitGate = func(tx *sql.Tx) error {
		var rev int64
		var raw []byte
		if err := tx.QueryRow(`SELECT revision,payload FROM workkit_routes WHERE id=? AND length(payload)<=4194304`, route).Scan(&rev, &raw); err != nil {
			return err
		}
		var state struct {
			Initialized bool     `json:"initialized"`
			Paused      bool     `json:"paused"`
			Unresolved  []string `json:"unresolved"`
			Pending     string   `json:"pendingPlanSha256"`
		}
		included := false
		if json.Unmarshal(raw, &state) != nil {
			return errors.New("invalid route state")
		}
		included = false
		for _, v := range state.Unresolved {
			if v == id {
				included = true
			}
		}
		if rev != revision || !state.Initialized || state.Paused || state.Pending != plan || !included {
			return errors.New("sync route paused, changed or unresolved before admission")
		}
		return nil
	}
	return nil
}

// PutEvidence retains bounded signed summaries. Credential values never belong here.
func (j *WorkkitJournal) PutEvidence(ctx context.Context, id string, raw []byte) error {
	if id == "" || len(raw) > 4<<20 {
		return errors.New("evidence bounds")
	}
	var total int
	if err := j.Box.db.QueryRowContext(ctx, `SELECT coalesce(sum(length(payload)),0) FROM workkit_evidence WHERE operation_id=?`, id).Scan(&total); err != nil {
		return err
	}
	if total+len(raw) > 64<<20 {
		return errors.New("evidence aggregate bound")
	}
	_, err := j.Box.db.ExecContext(ctx, `INSERT INTO workkit_evidence VALUES(?,?,?) ON CONFLICT(operation_id,digest) DO NOTHING`, id, DigestBytes(raw), raw)
	return err
}
func (j *WorkkitJournal) Evidence(ctx context.Context, id string) (map[string][]byte, error) {
	rows, err := j.Box.db.QueryContext(ctx, `SELECT digest,payload FROM workkit_evidence WHERE operation_id=? AND length(payload)<=4194304 ORDER BY digest LIMIT 129`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	files := map[string][]byte{}
	total := 0
	for rows.Next() {
		var digest string
		var raw []byte
		if err = rows.Scan(&digest, &raw); err != nil {
			return nil, err
		}
		total += len(raw)
		if len(files) >= 128 || total > 64<<20 || DigestBytes(raw) != digest {
			return nil, errors.New("evidence identity/bounds")
		}
		files["assets/native/"+digest+".json"] = raw
	}
	return files, rows.Err()
}
