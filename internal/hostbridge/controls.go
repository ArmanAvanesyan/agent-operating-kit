package hostbridge

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
)

const ControlProofVersion = "aok-control-proof/v1"

// ControlProof is an externally signed, short-lived assertion about a host
// boundary. Its signing key is trusted only through operator policy.
type ControlProof struct {
	Version          string   `json:"version"`
	Owner            string   `json:"owner"`
	Control          string   `json:"control"`
	Host             string   `json:"host"`
	HostVersion      string   `json:"hostVersion"`
	ExecutableSHA256 string   `json:"executableSha256"`
	Workspace        string   `json:"workspace"`
	ScopeSHA256      string   `json:"scopeSha256"`
	ObservedAt       string   `json:"observedAt"`
	ExpiresAt        string   `json:"expiresAt"`
	Status           string   `json:"status"`
	Checks           []string `json:"checks"`
	Signature        string   `json:"signature"`
}

// ControlScopeDigest binds a proof to the exact execution-related policy.
func ControlScopeDigest(p Policy) string {
	// Proof assets/signatures are excluded to avoid self-reference. Every other
	// exported policy field, including native-task permission and trust roots,
	// is part of the signed execution scope.
	p.Controls = nil
	return Digest(p)
}

func cloneRawMap(in map[string]json.RawMessage) map[string]json.RawMessage {
	if in == nil {
		return nil
	}
	out := make(map[string]json.RawMessage, len(in))
	for k, v := range in {
		out[k] = append(json.RawMessage(nil), v...)
	}
	return out
}

func verifyControls(p Policy, at time.Time) error {
	if at.IsZero() {
		return fmt.Errorf("control verification time required")
	}
	required := []string{"filesystem", "network", "ambient-configuration"}
	byName := map[string]Control{}
	for _, c := range p.Controls {
		if c.Name != "filesystem" && c.Name != "network" && c.Name != "ambient-configuration" {
			return fmt.Errorf("unsupported required control")
		}
		if _, ok := byName[c.Name]; ok {
			return fmt.Errorf("duplicate control proof")
		}
		byName[c.Name] = c
	}
	for _, name := range required {
		c, ok := byName[name]
		if !ok {
			return fmt.Errorf("required control %s has no signed proof", name)
		}
		data, err := assetBytes(p.root, c.Evidence)
		if err != nil {
			return fmt.Errorf("control evidence unavailable")
		}
		var proof ControlProof
		if err = usage.Decode(data, &proof); err != nil {
			return fmt.Errorf("control proof is invalid")
		}
		if proof.Version != ControlProofVersion || proof.Owner != c.Owner || proof.Control != c.Name || proof.Host != p.Host || proof.HostVersion != p.HostVersion || proof.ExecutableSHA256 != p.ExecutableSHA256 || proof.Workspace != p.Workspace || proof.ScopeSHA256 != ControlScopeDigest(p) || proof.ObservedAt != c.ObservedAt || proof.ExpiresAt != c.ExpiresAt || proof.Status != "enforced" || len(proof.Checks) == 0 {
			return fmt.Errorf("control proof scope or status mismatch")
		}
		seen := map[string]bool{}
		for _, check := range proof.Checks {
			check = strings.TrimSpace(check)
			if check == "" || seen[check] {
				return fmt.Errorf("control proof checks are incomplete")
			}
			seen[check] = true
		}
		start, e1 := time.Parse(time.RFC3339Nano, proof.ObservedAt)
		end, e2 := time.Parse(time.RFC3339Nano, proof.ExpiresAt)
		if e1 != nil || e2 != nil || !end.After(start) || end.Sub(start) > 5*time.Minute || at.Before(start) || !at.Before(end) {
			return fmt.Errorf("control proof expired or outside validity window")
		}
		keyHex, ok := p.ControlIssuers[proof.Owner]
		if !ok {
			return fmt.Errorf("control proof owner is not operator-trusted")
		}
		pub, e := hex.DecodeString(keyHex)
		if e != nil || len(pub) != ed25519.PublicKeySize {
			return fmt.Errorf("control proof issuer key is invalid")
		}
		sig, e := hex.DecodeString(proof.Signature)
		if e != nil || len(sig) != ed25519.SignatureSize {
			return fmt.Errorf("control proof signature is invalid")
		}
		proof.Signature = ""
		message, e := json.Marshal(proof)
		if e != nil || !ed25519.Verify(ed25519.PublicKey(pub), message, sig) {
			return fmt.Errorf("control proof signature verification failed")
		}
	}
	return nil
}
