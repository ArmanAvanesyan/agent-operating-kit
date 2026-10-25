package sdk

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	a "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/accessv1"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func accessJSON(v any) []byte { b, _ := json.Marshal(v); return b }
func accessConfig(t *testing.T) []byte {
	t.Helper()
	raw, e := os.ReadFile("../testdata/read/connections/github.json")
	if e != nil {
		t.Fatal(e)
	}
	var old struct {
		Profile json.RawMessage `json:"profile"`
	}
	if e = json.Unmarshal(raw, &old); e != nil {
		t.Fatal(e)
	}
	var p map[string]any
	json.Unmarshal(old.Profile, &p)
	meta := p["metadata"].(map[string]any)
	spec := p["spec"].(map[string]any)
	scope := spec["scope"].(map[string]any)
	ids := []string{}
	for _, id := range scope["containerIds"].([]any) {
		ids = append(ids, id.(string))
	}
	c := a.Configuration{Version: a.Version, Kind: "AuthConfiguration", ConnectionID: meta["id"].(string), Profile: old.Profile, Provider: "github", Origin: spec["origin"].(string), NativePrincipal: "7", TenantID: scope["tenantId"].(string), Containers: ids, Mode: "token", SecretReferences: map[string]string{"token": "WK_P3_TOKEN"}}
	b := accessJSON(c)
	if _, e = ValidateAuthConfiguration(b); e != nil {
		t.Fatal(e)
	}
	return b
}

type authFixture struct {
	calls   atomic.Int32
	bad     string
	entered chan bool
	release chan bool
}

func (f *authFixture) Authenticate(ctx context.Context, action string, c a.Configuration, check func() error) (a.Binding, error) {
	f.calls.Add(1)
	if e := check(); e != nil {
		return a.Binding{}, e
	}
	if f.entered != nil {
		f.entered <- true
		select {
		case <-f.release:
		case <-ctx.Done():
			return a.Binding{}, ctx.Err()
		}
	}
	if f.bad == "error" {
		return a.Binding{}, errors.New("PRIVATE_CANARY_MUST_NOT_LEAK")
	}
	status := "authenticated"
	if action == "auth logout" {
		status = "disconnected"
	}
	b := a.Binding{Version: a.Version, Kind: "AuthBindingObservation", ConnectionID: c.ConnectionID, Principal: c.NativePrincipal, TenantID: c.TenantID, Mode: c.Mode, Generation: "generation-1", Status: status, ObservedAt: time.Now().UTC().Format(time.RFC3339Nano), StorageMode: "external-reference", LiveQualification: "pending", ProviderInteraction: "identity-observation"}
	if f.bad == "future" {
		b.ObservedAt = time.Now().Add(time.Hour).Format(time.RFC3339Nano)
	}
	if f.bad == "wrong-principal" {
		b.Principal = "wrong"
	}
	if f.bad == "status" {
		b.Status = "ready"
	}
	return b, nil
}

type accessCase struct {
	policy  []byte
	grant   a.Grant
	private ed25519.PrivateKey
	out     string
}

func accessInputs(t *testing.T, request []byte, action string) accessCase {
	t.Helper()
	dir := readTestDir(t)
	ledger := filepath.Join(dir, "ledger")
	if e := os.Mkdir(ledger, 0700); e != nil {
		t.Fatal(e)
	}
	pub, priv, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	now := time.Now().UTC()
	var c a.Configuration
	_ = json.Unmarshal(request, &c)
	p := a.Policy{Version: a.Version, Kind: "AccessPolicy", LedgerRoot: ledger, Issuers: []a.Issuer{{ID: "fixture", PublicKey: base64.StdEncoding.EncodeToString(pub)}}, Artifacts: []a.Artifact{}, AllowedConnections: []string{c.ConnectionID}, AllowedActions: []string{action}, RevokedOperations: []string{}, NotBefore: now.Add(-time.Minute).Format(time.RFC3339Nano), ExpiresAt: now.Add(4 * time.Minute).Format(time.RFC3339Nano)}
	raw := accessJSON(p)
	out := filepath.Join(dir, "attempt")
	g := a.Grant{Version: a.Version, Kind: "AccessGrant", OperationID: "operation-1", Action: action, RequestSHA256: SHA256(request), PolicySHA256: SHA256(raw), ImplementationSHA256: AccessImplementationSHA256(), Output: out, Issuer: "fixture", IssuedAt: now.Add(-time.Second).Format(time.RFC3339Nano), ExpiresAt: now.Add(time.Minute).Format(time.RFC3339Nano)}
	return accessCase{raw, g, priv, out}
}
func (f accessCase) signed() []byte {
	g := f.grant
	g.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(f.private, AccessSigningBytes(g)))
	return accessJSON(g)
}
func TestAccessGrantBindingsAndNoEarlyIO(t *testing.T) {
	raw := accessConfig(t)
	for _, bad := range []string{"payload", "signature", "expiry", "version", "policy", "operation", "implementation", "output", "revoked", "unknown-field", "duplicate-key", "null"} {
		t.Run(bad, func(t *testing.T) {
			f := accessInputs(t, raw, "auth login")
			g := f.signed()
			p := f.policy
			r := raw
			switch bad {
			case "payload":
				r = append(raw, ' ')
			case "signature":
				var grant a.Grant
				json.Unmarshal(g, &grant)
				grant.Signature = base64.StdEncoding.EncodeToString(make([]byte, 64))
				g = accessJSON(grant)
			case "expiry":
				f.grant.ExpiresAt = time.Now().Add(-time.Second).Format(time.RFC3339Nano)
				g = f.signed()
			case "version":
				f.grant.Version = "workkit-access/v9"
				g = f.signed()
			case "policy":
				p = append(p, ' ')
			case "operation":
				f.grant.OperationID = "../escape"
				g = f.signed()
			case "implementation":
				f.grant.ImplementationSHA256 = strings.Repeat("0", 64)
				g = f.signed()
			case "output":
				f.grant.Output += "-changed"
				g = f.signed()
			case "revoked":
				var policy a.Policy
				json.Unmarshal(p, &policy)
				policy.RevokedOperations = []string{f.grant.OperationID}
				p = accessJSON(policy)
				f.grant.PolicySHA256 = SHA256(p)
				g = f.signed()
			case "unknown-field":
				r = append([]byte(`{"unexpected":false,`), raw[1:]...)
			case "duplicate-key":
				r = append([]byte(`{"version":"workkit-access/v1",`), raw[1:]...)
			case "null":
				r = []byte(`null`)
			}
			b := &authFixture{}
			if _, e := New(nil).WithAccessBackend(b).RunAccess(context.Background(), "auth login", r, p, g, f.out); e == nil {
				t.Fatal("invalid authority accepted")
			}
			if b.calls.Load() != 0 {
				t.Fatal("IO before validation")
			}
			if _, e := os.Stat(f.out); !os.IsNotExist(e) {
				t.Fatal("invalid request reserved output")
			}
		})
	}
}
func TestAccessImmutableReplayAndOfflineUntrustedInspection(t *testing.T) {
	raw := accessConfig(t)
	f := accessInputs(t, raw, "auth login")
	b := &authFixture{}
	s := New(nil).WithAccessBackend(b)
	r, e := s.RunAccess(context.Background(), "auth login", raw, f.policy, f.signed(), f.out)
	if e != nil {
		t.Fatal(e)
	}
	if r.Status != "ok" || r.Binding.EvidenceTrust != "current-process-observation" || r.Binding.ScopedReadVerified || r.ProductionActivation != "not-authorized" || r.LiveQualification != "pending" || r.MigrationActivation != "blocked" {
		t.Fatal("authority laundering")
	}
	if _, e = s.RunAccess(context.Background(), "auth login", raw, f.policy, f.signed(), f.out); e == nil {
		t.Fatal("output reused")
	}
	f.out += "-new"
	f.grant.Output = f.out
	if _, e = s.RunAccess(context.Background(), "auth login", raw, f.policy, f.signed(), f.out); e == nil || b.calls.Load() != 1 {
		t.Fatal("new grant reused operation")
	}
	inspected, e := InspectAuth(raw, accessJSON(r), time.Now())
	if e != nil || inspected.Status != "unknown" || inspected.EvidenceTrust != "unverified-local-receipt" {
		t.Fatalf("unsigned receipt trusted: %+v %v", inspected, e)
	}
	inspected, e = InspectAuth(raw, accessJSON(r), time.Now().Add(time.Hour))
	if e != nil || inspected.Status != "expired" {
		t.Fatal("freshness renewed")
	}
}
func TestAccessBadBackendAndCanaryEvidence(t *testing.T) {
	for _, bad := range []string{"future", "wrong-principal", "status", "error"} {
		t.Run(bad, func(t *testing.T) {
			raw := accessConfig(t)
			f := accessInputs(t, raw, "auth login")
			b := &authFixture{bad: bad}
			r, e := New(nil).WithAccessBackend(b).RunAccess(context.Background(), "auth login", raw, f.policy, f.signed(), f.out)
			if e == nil || r.Status != "unknown" {
				t.Fatal("bad observation accepted")
			}
			if strings.Contains(e.Error(), "PRIVATE_CANARY") {
				t.Fatal("secret in error")
			}
			filepath.Walk(f.out, func(path string, info os.FileInfo, err error) error {
				if err == nil && !info.IsDir() {
					raw, _ := os.ReadFile(path)
					if strings.Contains(string(raw), "PRIVATE_CANARY") {
						t.Fatal("secret in evidence")
					}
				}
				return nil
			})
			if _, e = New(nil).WithAccessBackend(b).RunAccess(context.Background(), "auth login", raw, f.policy, f.signed(), f.out); e == nil {
				t.Fatal("uncertain operation retried")
			}
		})
	}
}
func TestAccessConcurrentOneSend(t *testing.T) {
	raw := accessConfig(t)
	f := accessInputs(t, raw, "auth login")
	b := &authFixture{}
	s := New(nil).WithAccessBackend(b)
	var wg sync.WaitGroup
	var success atomic.Int32
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, e := s.RunAccess(context.Background(), "auth login", raw, f.policy, f.signed(), f.out); e == nil {
				success.Add(1)
			}
		}()
	}
	wg.Wait()
	if success.Load() != 1 || b.calls.Load() != 1 {
		t.Fatalf("not one send: %d %d", success.Load(), b.calls.Load())
	}
}
func TestAccessUnsafeInputsAndNativeStoreBlocked(t *testing.T) {
	raw := accessConfig(t)
	var c a.Configuration
	json.Unmarshal(raw, &c)
	c.Mode = "gh-native"
	c.Tool = &a.ToolPin{}
	raw = accessJSON(c)
	f := accessInputs(t, raw, "auth login")
	b := &authFixture{}
	if _, e := New(nil).WithAccessBackend(b).RunAccess(context.Background(), "auth login", raw, f.policy, f.signed(), f.out); e == nil || b.calls.Load() != 0 {
		t.Fatal("native credential store enabled")
	}
	for _, origin := range []string{"http://github.com", "https://user:secret@github.com", "https://github.com/escape", "https://github.com?secret=1"} {
		c.Mode = "token"
		c.Origin = origin
		if _, e := ValidateAuthConfiguration(accessJSON(c)); e == nil {
			t.Fatal("unsafe origin")
		}
	}
}
