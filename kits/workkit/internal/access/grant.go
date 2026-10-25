// Package access validates short-lived local-action grants. These are not AOK
// runtime grants and never authorize tracker mutation, migration or production.
package access

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	a "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/accessv1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/auth"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/engine"
	"path/filepath"
	"time"
)

func SigningBytes(g a.Grant) []byte {
	g.Signature = ""
	b, _ := json.Marshal(g)
	return append([]byte("workkit-access/v1:grant\n"), b...)
}
func Contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
func Validate(g a.Grant, p a.Policy, policyRaw, request []byte, action, out, impl string, now time.Time) error {
	if g.Version != a.Version || g.Kind != "AccessGrant" || p.Version != a.Version || p.Kind != "AccessPolicy" || !auth.ID.MatchString(g.OperationID) || g.Action != action || !Contains(p.AllowedActions, action) || Contains(p.RevokedOperations, g.OperationID) {
		return errors.New("invalid or revoked access grant")
	}
	if !filepath.IsAbs(out) || filepath.Clean(out) != out || g.Output != out || g.RequestSHA256 != engine.Hash(request) || g.PolicySHA256 != engine.Hash(policyRaw) || g.ImplementationSHA256 != impl {
		return errors.New("grant binding mismatch")
	}
	issued, e := time.Parse(time.RFC3339Nano, g.IssuedAt)
	if e != nil {
		return errors.New("grant time")
	}
	expiry, e := time.Parse(time.RFC3339Nano, g.ExpiresAt)
	if e != nil || issued.After(now) || !expiry.After(now) || expiry.Sub(issued) > 5*time.Minute || !expiry.After(issued) {
		return errors.New("grant expired or invalid")
	}
	start, e := time.Parse(time.RFC3339Nano, p.NotBefore)
	if e != nil || now.Before(start) {
		return errors.New("policy not current")
	}
	end, e := time.Parse(time.RFC3339Nano, p.ExpiresAt)
	if e != nil || !end.After(now) || expiry.After(end) {
		return errors.New("policy expired")
	}
	seen := map[string]bool{}
	var key ed25519.PublicKey
	for _, issuer := range p.Issuers {
		b, e := base64.StdEncoding.DecodeString(issuer.PublicKey)
		if e != nil || len(b) != ed25519.PublicKeySize || !auth.ID.MatchString(issuer.ID) || seen[issuer.ID] {
			return errors.New("invalid issuer")
		}
		seen[issuer.ID] = true
		if issuer.ID == g.Issuer {
			key = b
		}
	}
	sig, e := base64.StdEncoding.DecodeString(g.Signature)
	if e != nil || len(key) != ed25519.PublicKeySize || !ed25519.Verify(key, SigningBytes(g), sig) {
		return errors.New("invalid signature")
	}
	return nil
}
