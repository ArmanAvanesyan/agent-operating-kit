package enrollment

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/enrolled/bootstrap"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/evidence"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
)

const checkPurpose = "codex-github-first-live-run"
const maxCheckFile = 4 << 20
const maxCheckTotal = 64 << 20

// Check performs bounded offline inspection only. It never resolves references,
// executes a host, or treats evidence presence as qualification.
func Check(root string, at time.Time) (Report, error) {
	r := Report{Version: Version, AsOf: at.UTC(), Status: "blocked", OfflineOnly: true}
	if at.IsZero() {
		return r, errors.New("as-of time is required")
	}
	stageByID := make(map[string]string)
	for _, n := range Requirements() {
		stageByID[n.ID] = n.Stage
	}
	for _, id := range []string{"controls", "identity", "capability", "capacity", "verification", "runtime", "authorization", "platform", "provider-read", "provider-operation", "witness", "budget", "stop-monitor"} {
		stageByID["live:"+id] = "calibration"
	}
	stageByID["live:quality"] = "ordinary-pilot"
	stageByID["production:activation"] = "production"
	add := func(id, path, status, detail string) {
		stage := stageByID[id]
		if stage == "" {
			stage = "qualification"
		}
		r.Findings = append(r.Findings, Finding{ID: id, Path: path, Stage: stage, Status: status, Detail: detail})
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return r, err
	}
	var total int64
	read := func(rel string) ([]byte, error) { return safeRead(root, rel, &total) }
	b, err := read("manifest.json")
	if err != nil {
		add("manifest", "manifest.json", "missing", "required manifest unavailable or unsafe")
	} else {
		var m Manifest
		if usage.Decode(b, &m) != nil {
			add("manifest", "manifest.json", "invalid", "manifest JSON rejected")
		} else if m.Version != Version || m.Purpose != checkPurpose || !reflect.DeepEqual(m.Required, Requirements()) {
			add("manifest", "manifest.json", "invalid", "manifest version, purpose, or compiled requirements mismatch")
		} else {
			generated, genErr := Files()
			var canonical Manifest
			if genErr != nil || usage.Decode(generated["manifest.json"], &canonical) != nil || !reflect.DeepEqual(m.Drafts, canonical.Drafts) {
				add("manifest", "manifest.json", "invalid", "manifest immutable draft inventory differs from compiled generator")
				add("draft-inventory", "", "invalid", "draft list, duplicate detection, or immutable hash inventory differs from compiled generator")
			} else {
				add("manifest", "manifest.json", "valid", "closed manifest and compiled requirements match")
				checkDrafts(m.Drafts, read, add)
			}
		}
	}
	checkInventory(read, add)
	rootKey, rootErr := read("inputs/bootstrap-root.ed25519")
	if rootErr != nil || len(rootKey) != ed25519.PublicKeySize {
		add("public-root", "inputs/bootstrap-root.ed25519", "missing", "required public root key unavailable or invalid")
	}
	for _, n := range Requirements() {
		if n.Check == "public-key" || n.Check == "bootstrap" {
			continue
		}
		if _, e := read(n.Path); e != nil {
			add(n.ID, n.Path, "missing", "required evidence unavailable or unsafe; independent live qualification remains pending")
		} else {
			add(n.ID, n.Path, "present-unverified", "evidence is present; independent live qualification remains pending")
		}
	}
	for _, n := range Requirements() {
		if n.Check == "bootstrap" {
			checkBootstrap(n, rootKey, rootErr, at, read, add)
		}
	}
	for _, id := range []string{"controls", "identity", "capability", "capacity", "verification", "runtime", "authorization", "platform", "provider-read", "provider-operation", "witness", "budget", "stop-monitor"} {
		add("live:"+id, "", "pending-external-qualification", "independent live gate evidence and review remain pending")
	}
	add("live:quality", "", "pending-external-qualification", "independent ordinary-pilot quality acceptance remains pending")
	add("production:activation", "", "pending-external-qualification", "production activation is not established by offline inspection")
	sort.Slice(r.Findings, func(i, j int) bool {
		if r.Findings[i].ID != r.Findings[j].ID {
			return r.Findings[i].ID < r.Findings[j].ID
		}
		return r.Findings[i].Path < r.Findings[j].Path
	})
	return r, nil
}

func safeRead(root, rel string, total *int64) ([]byte, error) {
	if rel == "" || strings.Contains(rel, "\\") || evidence.CheckPath(rel) != nil || path.Clean(rel) != rel {
		return nil, errors.New("unsafe relative path")
	}
	p := filepath.Join(root, filepath.FromSlash(rel))
	if !within(root, p) {
		return nil, errors.New("path escape")
	}
	for cur := p; ; cur = filepath.Dir(cur) {
		st, e := os.Lstat(cur)
		if e != nil {
			return nil, e
		}
		if st.Mode()&fs.ModeSymlink != 0 {
			return nil, errors.New("symlink path")
		}
		if cur == filepath.Dir(cur) {
			break
		}
		if !within(root, cur) {
			// Ancestors above the bundle root are checked for symlinks too.
			if within(cur, root) {
				continue
			}
			return nil, errors.New("path escape")
		}
	}
	st, e := os.Lstat(p)
	if e != nil {
		return nil, e
	}
	if !st.Mode().IsRegular() || st.Size() > maxCheckFile || *total+st.Size() > maxCheckTotal {
		return nil, errors.New("file bound or type rejected")
	}
	f, e := os.Open(p)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	opened, e := f.Stat()
	if e != nil || !opened.Mode().IsRegular() || !os.SameFile(st, opened) {
		return nil, errors.New("file changed during safe read")
	}
	b, e := io.ReadAll(io.LimitReader(f, maxCheckFile+1))
	if e != nil {
		return nil, e
	}
	if len(b) > maxCheckFile || *total+int64(len(b)) > maxCheckTotal {
		return nil, errors.New("file bound exceeded")
	}
	*total += int64(len(b))
	return b, nil
}
func within(root, p string) bool {
	rel, e := filepath.Rel(root, p)
	return e == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
func checkDrafts(ds []Asset, read func(string) ([]byte, error), add func(string, string, string, string)) {
	seen := map[string]bool{}
	folded := map[string]bool{}
	for _, d := range ds {
		key := d.Path
		fold := strings.ToLower(key)
		if key == "" || seen[key] || folded[fold] || strings.Contains(key, "\\") || evidence.CheckPath(key) != nil || path.Clean(key) != key {
			add("draft", "", "invalid", "duplicate, colliding, or unsafe draft path")
			continue
		}
		seen[key] = true
		folded[fold] = true
		b, e := read(d.Path)
		if e != nil {
			add("draft", d.Path, "missing", "draft unavailable or unsafe")
			continue
		}
		h := sha256.Sum256(b)
		if !validSHA(d.SHA256) || hex.EncodeToString(h[:]) != strings.ToLower(d.SHA256) {
			add("draft", d.Path, "invalid", "draft hash mismatch")
		} else {
			add("draft", d.Path, "valid", "immutable draft hash matches")
		}
	}
}
func validSHA(s string) bool {
	if len(s) != 64 {
		return false
	}
	_, e := hex.DecodeString(s)
	return e == nil
}
func checkInventory(read func(string) ([]byte, error), add func(string, string, string, string)) {
	p := "operator-inputs.json"
	b, e := read(p)
	if e != nil {
		add("operator-inputs", p, "missing", "required inventory unavailable or unsafe")
		return
	}
	var v Inventory
	if usage.Decode(b, &v) != nil || v.Version != Version {
		add("operator-inputs", p, "invalid", "inventory encoding or version rejected")
		return
	}
	fields := []struct {
		id, val string
		hash    bool
	}{{"repository", v.Repository, false}, {"repositoryId", v.RepositoryID, false}, {"tenantId", v.TenantID, false}, {"immutableIssueId", v.ImmutableIssueID, false}, {"issueNumber", v.IssueNumber, false}, {"platform", v.Platform, false}, {"codexExecutableSha256", v.CodexExecutableSHA256, true}, {"ghExecutableSha256", v.GHExecutableSHA256, true}, {"ghVersion", v.GHVersion, false}, {"modelSelector", v.ModelSelector, false}, {"configurationId", v.ConfigurationID, false}, {"profileSha256", v.ProfileSHA256, true}, {"accountRef", v.AccountRef, false}, {"poolId", v.PoolID, false}, {"operator", v.Operator, false}, {"recoveryOwner", v.RecoveryOwner, false}}
	for _, f := range fields {
		if strings.TrimSpace(f.val) == "" {
			add("input:"+f.id, p, "missing-value", "required operator input is empty")
		} else if f.hash && !validSHA(f.val) {
			add("input:"+f.id, p, "invalid", "operator input hash is malformed")
		} else if f.id == "issueNumber" && !digits(f.val) {
			add("input:"+f.id, p, "invalid", "issue number is malformed")
		} else {
			add("input:"+f.id, p, "present-unverified", "operator input recorded; value withheld")
		}
	}
	allowed := make(map[string]bool)
	for _, id := range SecretReferenceIDs() {
		allowed[id] = true
	}
	for name := range v.SecretReferences {
		if !allowed[name] {
			add("secret-references", p, "invalid", "unknown secret reference identifier")
		}
	}
	for _, name := range SecretReferenceIDs() {
		ref, ok := v.SecretReferences[name]
		if !ok || strings.TrimSpace(ref) == "" {
			add("secret-reference:"+name, p, "missing-value", "required opaque reference is missing")
		} else {
			add("secret-reference:"+name, p, "present-unverified", "opaque reference recorded; credentials not resolved")
		}
	}
}
func digits(s string) bool {
	if s == "" || s == "0" || (len(s) > 1 && s[0] == '0') {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
func checkBootstrap(n Need, key []byte, keyErr error, at time.Time, read func(string) ([]byte, error), add func(string, string, string, string)) {
	b, e := read(n.Path)
	if e != nil {
		add(n.ID, n.Path, "missing", "signed bootstrap unavailable or unsafe")
		return
	}
	if keyErr != nil || len(key) != ed25519.PublicKeySize {
		add(n.ID, n.Path, "invalid", "trusted public root unavailable")
		return
	}
	var env bootstrap.Envelope
	if usage.Decode(b, &env) != nil || env.Version != bootstrap.EnvelopeVersion || len(env.Payload) == 0 {
		add(n.ID, n.Path, "invalid", "bootstrap envelope rejected")
		return
	}
	issued, ie := time.Parse(time.RFC3339Nano, env.IssuedAt)
	expires, ee := time.Parse(time.RFC3339Nano, env.ExpiresAt)
	sig, se := base64.StdEncoding.DecodeString(env.Signature)
	if ie != nil || ee != nil || issued.After(at) || !at.Before(expires) || !expires.After(issued) || expires.Sub(issued) > 24*time.Hour || se != nil || !ed25519.Verify(ed25519.PublicKey(key), bootstrap.SigningBytes(env), sig) {
		add(n.ID, n.Path, "invalid", "signature or enrollment time invalid")
		return
	}
	var c bootstrap.Config
	if usage.Decode(env.Payload, &c) != nil || c.Version != bootstrap.ConfigVersion || c.Service != expectedService(n.Contract) || !expectedAction(c, n.Contract) {
		add(n.ID, n.Path, "invalid", "signed config fields do not match required service/action")
		return
	}
	add(n.ID, n.Path, "signature-valid-only", "signature verifies under supplied package key; no external root pin, qualification, or authorization is established")
}
func expectedService(contract string) string {
	if strings.HasPrefix(contract, "worker:") {
		return "worker"
	}
	if strings.HasPrefix(contract, "qualification:") {
		return "qualification"
	}
	if contract == "anchor" {
		return "anchor"
	}
	if contract == "verifier" {
		return "verifier"
	}
	if contract == "authority" {
		return "authority"
	}
	return ""
}
func expectedAction(c bootstrap.Config, contract string) bool {
	if strings.HasPrefix(contract, "worker:") {
		mode := strings.TrimPrefix(contract, "worker:")
		if c.Worker == nil || c.Worker.Mode != mode {
			return false
		}
		switch mode {
		case "calibration":
			return c.Worker.Calibration != nil
		case "experiment":
			return c.Worker.Experiment != nil
		case "verify-candidate", "accept-candidate", "handoff-candidate":
			return c.Worker.PostRun != nil && c.Worker.PostRun.Stage == mode
		case "github-dispatch", "github-reconcile":
			return c.Worker.GitHubWrite != nil
		}
		return false
	}
	if strings.HasPrefix(contract, "qualification:") {
		if c.Qualification == nil || c.Qualification.Action != strings.TrimPrefix(contract, "qualification:") {
			return false
		}
		switch c.Qualification.Action {
		case "github-read":
			return c.Qualification.GitHubRead != nil
		case "codex-quota":
			return c.Qualification.CodexQuota != nil
		case "host-conformance":
			return c.Qualification.Host != nil
		case "verifier-conformance":
			return c.Qualification.Verifier != nil
		case "github-comment":
			return c.Qualification.GitHubWrite != nil
		}
		return false
	}
	if contract == "anchor" {
		return c.Anchor != nil
	}
	if contract == "verifier" {
		return c.Verifier != nil
	}
	if contract == "authority" {
		return c.Authority != nil
	}
	return false
}
