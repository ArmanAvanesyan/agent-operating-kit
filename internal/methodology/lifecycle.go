// Package methodology contains offline methodology contracts and lifecycle guards.
package methodology

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

const LifecycleVersion = "aok-methodology-lifecycle/v1"

// Transition is an immutable, signed request to move a governed item between stages.
type Transition struct {
	Version          string     `json:"version"`
	ID               string     `json:"id"`
	Subject          string     `json:"subject"`
	Profile          string     `json:"profile"`
	From             string     `json:"from"`
	To               string     `json:"to"`
	ExpectedRevision uint64     `json:"expectedRevision"`
	Role             string     `json:"role"`
	Evidence         []Evidence `json:"evidence"`
	Signer           string     `json:"signer"`
	Signature        string     `json:"signature"`
}

type Evidence struct {
	Gate   string `json:"gate"`
	SHA256 string `json:"sha256"`
}
type Profile struct {
	ID             string
	Stages         []string
	Gates          map[string][]string
	Transitions    []Edge
	AllowedRoles   map[string][]string
	VerifyEvidence EvidenceVerifier
}

type Edge struct {
	From string
	To   string
}
type Snapshot struct {
	Subject, Profile, Stage string
	Revision                uint64
}
type KeyResolver func(signer string, role string) (ed25519.PublicKey, bool)
type EvidenceVerifier func(Evidence) error
type AppendFunc func(expectedRevision uint64, next Snapshot, transition Transition) error

// CanonicalTransitionBytes returns the exact bytes that must be signed. Signature
// is omitted and evidence order is canonicalized to make signing deterministic.
func CanonicalTransitionBytes(t Transition) ([]byte, error) {
	t.Signature = ""
	t.Evidence = append([]Evidence(nil), t.Evidence...)
	sort.Slice(t.Evidence, func(i, j int) bool { return t.Evidence[i].Gate < t.Evidence[j].Gate })
	return json.Marshal(t)
}

// CheckTransition validates stage order, role authorization, signature, and every
// gate required for the destination stage. It has no mutation authority.
func CheckTransition(p Profile, current Snapshot, t Transition, keys KeyResolver) error {
	if t.Version != LifecycleVersion || t.ID == "" || t.Subject == "" || t.Profile != p.ID {
		return errors.New("invalid methodology lifecycle transition identity")
	}
	if current.Subject != t.Subject || current.Profile != p.ID || current.Stage != t.From || current.Revision != t.ExpectedRevision {
		return errors.New("methodology lifecycle CAS precondition failed")
	}
	from, to := index(p.Stages, t.From), index(p.Stages, t.To)
	allowed := false
	if len(p.Transitions) > 0 {
		for _, edge := range p.Transitions {
			if edge.From == t.From && edge.To == t.To {
				allowed = true
				break
			}
		}
	} else {
		allowed = from >= 0 && to == from+1
	}
	if !allowed {
		return errors.New("methodology lifecycle transition is not an allowed next stage")
	}
	roles := p.AllowedRoles[t.From+"\x00"+t.To]
	if len(roles) == 0 {
		roles = []string{"methodology:" + p.ID + ":" + t.To}
	}
	roleAllowed := false
	for _, role := range roles {
		if t.Role == role {
			roleAllowed = true
		}
	}
	if !roleAllowed {
		return fmt.Errorf("transition role %q is not authorized for this gate", t.Role)
	}
	if keys == nil {
		return errors.New("role signature verifier is required")
	}
	key, ok := keys(t.Signer, t.Role)
	if !ok || len(key) != ed25519.PublicKeySize {
		return errors.New("authorized methodology role key is unavailable")
	}
	body, err := CanonicalTransitionBytes(t)
	if err != nil {
		return err
	}
	sig, err := hex.DecodeString(t.Signature)
	if err != nil || !ed25519.Verify(key, body, sig) {
		return errors.New("invalid methodology role signature")
	}
	want := append([]string(nil), p.Gates[t.From+"\x00"+t.To]...)
	if len(want) == 0 {
		want = append(want, p.Gates[t.To]...)
	}
	sort.Strings(want)
	seen := map[string]bool{}
	for _, e := range t.Evidence {
		if !validHash(e.SHA256) || e.Gate == "" || seen[e.Gate] {
			return errors.New("invalid or duplicate methodology gate evidence")
		}
		seen[e.Gate] = true
		if p.VerifyEvidence == nil {
			return errors.New("trusted methodology evidence verifier is required")
		}
		if err := p.VerifyEvidence(e); err != nil {
			return fmt.Errorf("methodology gate %q evidence rejected: %w", e.Gate, err)
		}
	}
	for _, g := range want {
		if !seen[g] {
			return fmt.Errorf("required methodology gate %q lacks evidence", g)
		}
	}
	return nil
}

// RecordTransition first verifies the signed transition and then delegates the
// single revision-checked append to the caller (for example, the P7 journal).
func RecordTransition(p Profile, current Snapshot, t Transition, keys KeyResolver, appendCAS AppendFunc) (Snapshot, error) {
	if err := CheckTransition(p, current, t, keys); err != nil {
		return Snapshot{}, err
	}
	if appendCAS == nil {
		return Snapshot{}, errors.New("transition journal callback is required")
	}
	next := Snapshot{Subject: current.Subject, Profile: current.Profile, Stage: t.To, Revision: current.Revision + 1}
	if err := appendCAS(current.Revision, next, t); err != nil {
		return Snapshot{}, err
	}
	return next, nil
}

func index(a []string, s string) int {
	for i, v := range a {
		if v == s {
			return i
		}
	}
	return -1
}
func validHash(s string) bool {
	if len(s) != 64 || strings.ToLower(s) != s {
		return false
	}
	_, e := hex.DecodeString(s)
	return e == nil
}
func Digest(data []byte) string { h := sha256.Sum256(data); return hex.EncodeToString(h[:]) }
