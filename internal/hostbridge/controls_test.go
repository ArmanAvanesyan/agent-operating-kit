package hostbridge

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func signedControls(t *testing.T, at time.Time) Policy {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	p := Policy{Host: "codex", HostVersion: "1.2.3", Executable: "/trusted/codex", ExecutableSHA256: Hash([]byte("binary")), Workspace: "/workspace", Tools: []string{"read"}, Environment: []string{}, CodexConfig: map[string]json.RawMessage{}, ConfigurationAssets: []Asset{}, ControlIssuers: map[string]string{"ops": hex.EncodeToString(pub)}, root: root}
	for _, name := range []string{"filesystem", "network", "ambient-configuration"} {
		proof := ControlProof{Version: ControlProofVersion, Owner: "ops", Control: name, Host: p.Host, HostVersion: p.HostVersion, ExecutableSHA256: p.ExecutableSHA256, Workspace: p.Workspace, ScopeSHA256: ControlScopeDigest(p), ObservedAt: at.UTC().Format(time.RFC3339Nano), ExpiresAt: at.Add(4 * time.Minute).UTC().Format(time.RFC3339Nano), Status: "enforced", Checks: []string{"check-" + name}}
		payload, _ := json.Marshal(proof)
		proof.Signature = hex.EncodeToString(ed25519.Sign(priv, payload))
		data, _ := json.Marshal(proof)
		path := filepath.Join(root, name+".json")
		if err = os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		p.Controls = append(p.Controls, Control{Name: name, Owner: "ops", Evidence: Asset{Path: name + ".json", SHA256: Hash(data)}, ObservedAt: proof.ObservedAt, ExpiresAt: proof.ExpiresAt, Workspace: p.Workspace, HostVersion: p.HostVersion})
	}
	return p
}

func TestVerifyControlsRequiresSignedScopedProofs(t *testing.T) {
	at := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	p := signedControls(t, at)
	if err := verifyControls(p, at.Add(time.Minute)); err != nil {
		t.Fatalf("valid signed controls rejected: %v", err)
	}
	p.Tools = append(p.Tools, "write")
	if err := verifyControls(p, at.Add(time.Minute)); err == nil {
		t.Fatal("proof remained valid after execution scope changed")
	}
}

func TestVerifyControlsRejectsUntrustedExpiredOrTamperedProof(t *testing.T) {
	at := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	p := signedControls(t, at)
	delete(p.ControlIssuers, "ops")
	if err := verifyControls(p, at); err == nil {
		t.Fatal("untrusted issuer accepted")
	}
	p = signedControls(t, at)
	if err := verifyControls(p, at.Add(5*time.Minute)); err == nil {
		t.Fatal("expired proof accepted")
	}
	p = signedControls(t, at)
	path := filepath.Join(p.root, p.Controls[0].Evidence.Path)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, []byte(" ")...)
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err = verifyControls(p, at); err == nil {
		t.Fatal("changed evidence accepted")
	}
}

func TestControlScopeBindsNativeTasksTimeoutAndRoutes(t *testing.T) {
	at := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	for name, change := range map[string]func(*Policy){"tasks": func(p *Policy) { p.AllowNativeTasks = true }, "timeout": func(p *Policy) { p.TimeoutSeconds++ }, "routes": func(p *Policy) { p.Models = []string{"other"} }, "workspace-roots": func(p *Policy) { p.WorkspaceRoots = []string{"/"} }} {
		t.Run(name, func(t *testing.T) {
			p := signedControls(t, at)
			change(&p)
			if verifyControls(p, at) == nil {
				t.Fatal("changed execution scope accepted")
			}
		})
	}
}
