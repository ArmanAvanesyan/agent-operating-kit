package provision

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

// Grant is provisioning-specific; item SyncOperation grants cannot authorize it.
type Grant struct {
	Version    string `json:"version"`
	ID         string `json:"id"`
	Issuer     string `json:"issuer"`
	Nonce      string `json:"nonce"`
	Scope      Scope  `json:"scope"`
	PlanSHA256 string `json:"planSha256"`
	ChangeID   string `json:"changeId"`
	IssuedAt   string `json:"issuedAt"`
	ExpiresAt  string `json:"expiresAt"`
	Signature  string `json:"signature"`
}

// Trust is supplied from the protected operator authority, never a plan or source.
// Current must enforce revocations and qualified operation-specific writer scope.
type Trust interface {
	Key(context.Context, string) (ed25519.PublicKey, error)
	Current(context.Context, Grant, time.Time) error
}
type GrantAuthority struct {
	Grant Grant
	Trust Trust
}

func GrantBytes(g Grant) []byte { g.Signature = ""; b, _ := json.Marshal(g); return b }
func (a GrantAuthority) Verify(ctx context.Context, p Plan, c Change, now time.Time) error {
	g := a.Grant
	if a.Trust == nil || g.Version != Version || g.ID == "" || g.Nonce == "" || g.Scope != p.Scope || g.PlanSHA256 != p.Digest || g.ChangeID != c.ID {
		return fmt.Errorf("exact provisioning grant required")
	}
	start, e := time.Parse(time.RFC3339Nano, g.IssuedAt)
	end, e2 := time.Parse(time.RFC3339Nano, g.ExpiresAt)
	if e != nil || e2 != nil || now.Before(start) || !now.Before(end) || end.Sub(start) > 5*time.Minute {
		return fmt.Errorf("stale provisioning grant")
	}
	key, e := a.Trust.Key(ctx, g.Issuer)
	sig, e2 := hex.DecodeString(g.Signature)
	if e != nil || e2 != nil || len(key) != ed25519.PublicKeySize || !ed25519.Verify(key, GrantBytes(g), sig) {
		return fmt.Errorf("untrusted provisioning signature")
	}
	return a.Trust.Current(ctx, g, now)
}
