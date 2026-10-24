package selection

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/evaluation"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/modelcatalog"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

var digestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

func read(path string, target any) ([]byte, error) {
	if e := safePath(path); e != nil {
		return nil, e
	}
	b, e := modelcatalog.ReadFile(path)
	if e != nil {
		return nil, e
	}
	if e = usage.Decode(b, target); e != nil {
		return nil, e
	}
	return b, nil
}
func asset(root string, a Asset) ([]byte, error) {
	if e := safePath(root); e != nil {
		return nil, e
	}
	if !digestPattern.MatchString(a.SHA256) {
		return nil, fmt.Errorf("invalid asset digest")
	}
	b, e := modelcatalog.ReadAsset(root, a.Path)
	if e == nil && usage.Hash(b) != a.SHA256 {
		e = fmt.Errorf("asset digest mismatch: %s", a.Path)
	}
	return b, e
}
func contextTrusted(o ContextObservation, c Candidate, t TaskProfile, p evaluation.EvidencePolicy, bindingSHA string, at time.Time) bool {
	if o.Dimension == "account" && !o.CoverageComplete {
		return false
	}
	if o.BindingSHA256 != bindingSHA {
		return false
	}
	if o.AssignmentSHA256 != t.Assignment.SHA256 || o.TaskSHA256 != t.Packet.SHA256 || o.InputRevisionSHA256 != t.InputRevision.SHA256 {
		return false
	}
	if o.ConfigurationID != c.ConfigurationID || o.ProfileSHA256 != c.Profile.SHA256 || o.AccountScope != c.AccountScope {
		return false
	}
	observed, e := time.Parse(time.RFC3339Nano, o.ObservedAt)
	until, e2 := time.Parse(time.RFC3339Nano, o.ExpiresAt)
	if e != nil || e2 != nil || observed.After(at) || !at.Before(until) || at.Sub(observed) > 5*time.Minute {
		return false
	}
	sig, e := base64.StdEncoding.DecodeString(o.Signature)
	if e != nil {
		return false
	}
	o.Signature = ""
	msg, _ := json.Marshal(o)
	var canonical map[string]any
	if usage.Decode(msg, &canonical) != nil {
		return false
	}
	msg, _ = json.Marshal(canonical)
	for _, i := range p.Issuers {
		if i.ID != o.Issuer {
			continue
		}
		allowed := false
		for _, r := range i.Roles {
			if r == "context" {
				allowed = true
			}
		}
		key, e := base64.StdEncoding.DecodeString(i.PublicKey)
		if allowed && e == nil && len(key) == ed25519.PublicKeySize && ed25519.Verify(key, msg, sig) {
			return true
		}
	}
	return false
}
func resolveShards(root string, paths []string) ([]string, error) {
	out := []string{}
	for _, p := range paths {
		if filepath.IsAbs(p) || !filepath.IsLocal(p) {
			return nil, fmt.Errorf("unsafe shard path")
		}
		out = append(out, filepath.Join(root, p))
	}
	return out, nil
}

func safePath(path string) error {
	abs, e := filepath.Abs(path)
	if e != nil {
		return e
	}
	for p := abs; ; p = filepath.Dir(p) {
		st, e := os.Lstat(p)
		if e != nil {
			return e
		}
		if st.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink ancestor forbidden")
		}
		if p == filepath.Dir(p) {
			return nil
		}
	}
}
