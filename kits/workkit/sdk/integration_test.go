package sdk

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"

	"errors"
	i "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/integrationv1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/engine"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/readsource"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func p7authority(t *testing.T, subject, purpose string) (i.Policy, i.Enrollment, ed25519.PrivateKey, []byte) {
	t.Helper()
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	now := time.Now().UTC()
	store := i.Store{Version: i.JournalVersion, Kind: "IntegrationJournal", Path: filepath.Join(readTestDir(t), "store.db"), ID: strings.Repeat("a", 64), Namespace: "new-fixture-only", Owner: "workkit"}
	p := i.Policy{Version: i.Version, Kind: "IntegrationPolicy", Store: store, Limits: DefaultIntegrationLimits(), Routes: map[string]string{"route": strings.Repeat("b", 64)}, NotBefore: now.Add(-time.Minute).Format(time.RFC3339Nano), ExpiresAt: now.Add(time.Hour).Format(time.RFC3339Nano), Issuers: []i.Issuer{{ID: "fixture", PublicKey: base64.StdEncoding.EncodeToString(pub)}}, RevokedIDs: []string{}, AllowedPurposes: []string{purpose}}
	raw := engine.Encode(p)
	e := i.Enrollment{Version: i.Version, Kind: "IntegrationEnrollment", ID: "one-use-fixture", Purpose: purpose, Store: store, RouteID: "route", WorkerID: "worker", SubjectSHA256: subject, PolicySHA256: SHA256(raw), ImplementationSHA256: NativeImplementationSHA256(), IssuedAt: now.Add(-time.Second).Format(time.RFC3339Nano), ExpiresAt: now.Add(4 * time.Minute).Format(time.RFC3339Nano), Issuer: "fixture", Fence: 1}
	e.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(key, IntegrationSigningBytes(e)))
	return p, e, key, raw
}
func TestP7DomainSeparatedAuthority(t *testing.T) {
	subject := SHA256([]byte("synthetic"))
	p, e, key, raw := p7authority(t, subject, "local-inspect")
	if _, _, err := ValidateIntegrationAuthority(raw, engine.Encode(e), subject, e.Purpose, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	for _, test := range []string{"wrong-root", "expired", "revoked", "purpose", "store", "payload", "version"} {
		t.Run(test, func(t *testing.T) {
			bad := e
			policy := p
			switch test {
			case "wrong-root":
				bad.Issuer = "other"
			case "expired":
				bad.ExpiresAt = time.Now().Add(-time.Second).UTC().Format(time.RFC3339Nano)
			case "revoked":
				policy.RevokedIDs = []string{bad.ID}
			case "purpose":
				bad.Purpose = "provider-send"
			case "store":
				bad.Store.Version = "workkit-operations/v1"
			case "payload":
				bad.SubjectSHA256 = SHA256([]byte("other"))
			case "version":
				bad.Version = "workkit-integration/v999"
			}
			pol := engine.Encode(policy)
			bad.PolicySHA256 = SHA256(pol)
			bad.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(key, IntegrationSigningBytes(bad)))
			if _, _, err := ValidateIntegrationAuthority(pol, engine.Encode(bad), subject, e.Purpose, time.Now().UTC()); err == nil {
				t.Fatal("tampered scope accepted")
			}
		})
	}
}
func p7webhook(t *testing.T) (i.EventEnvelope, i.Policy, ed25519.PrivateKey, []byte) {
	t.Helper()
	p, _, key, _ := p7authority(t, SHA256([]byte("hint")), "ingest-hint")
	body := []byte("{\n  \"action\": \"edited\", \"repository\": {\"id\": 123}, \"issue\": {\"id\": 456}\n}\n")
	secret := []byte("synthetic-github-webhook-secret")
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	now := time.Now().UTC()
	r := i.IngressReceipt{ID: "nonce-1", RouteID: "route", Ingress: "fixture-ingress", ConnectionID: "github", RepositoryID: "123", Origin: "https://api.github.com", BodySHA256: SHA256(body), Headers: i.Headers{DeliveryID: "delivery-1", Event: "issues", Signature256: "sha256=" + hex.EncodeToString(mac.Sum(nil))}, ReceivedAt: now.Add(-time.Second).Format(time.RFC3339Nano), ExpiresAt: now.Add(time.Minute).Format(time.RFC3339Nano), Issuer: "fixture"}
	r.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(key, IngressSigningBytes(r)))
	return i.EventEnvelope{Version: i.Version, Kind: "GitHubEventEnvelope", Provider: "github", RouteID: "route", Body: body, Receipt: r}, p, key, secret
}
func TestP7WebhookExactBytesSchemaAndScope(t *testing.T) {
	env, p, key, secret := p7webhook(t)
	raw := engine.Encode(env)
	var again i.EventEnvelope
	if e := DecodeRead(raw, &again); e != nil || !bytes.Equal(env.Body, again.Body) {
		t.Fatal("raw body changed", e)
	}
	if _, e := VerifyGitHubHint(again, p, secret, time.Now().UTC()); e != nil {
		t.Fatal(e)
	}
	schema, e := IntegrationContractSchema("event-envelope")
	if e != nil {
		t.Fatal(e)
	}
	compiler := jsonschema.NewCompiler()
	if e = compiler.AddResource("https://workkit.example/native-test", jsonSchemaValue(schema)); e != nil {
		t.Fatal(e)
	}
	compiled, e := compiler.Compile("https://workkit.example/native-test")
	if e != nil {
		t.Fatal(e)
	}
	value, e := engine.JSONValue(raw)
	if e != nil || compiled.Validate(value) != nil {
		t.Fatal("byte schema does not match wire encoding", e, compiled.Validate(value))
	}
	for _, test := range []string{"body", "headers", "nonce", "foreign-repo", "route", "expired", "unknown-event", "pull-request"} {
		t.Run(test, func(t *testing.T) {
			bad := cloneRead(env)
			switch test {
			case "body":
				bad.Body = append(bad.Body, ' ')
			case "headers":
				bad.Receipt.Headers.DeliveryID = "different"
			case "nonce":
				bad.Receipt.ID = "other"
			case "foreign-repo":
				bad.Receipt.RepositoryID = "999"
			case "route":
				bad.RouteID = "other"
			case "expired":
				bad.Receipt.ExpiresAt = time.Now().Add(-time.Second).UTC().Format(time.RFC3339Nano)
			case "unknown-event":
				bad.Receipt.Headers.Event = "push"
			case "pull-request":
				bad.Body = []byte(`{"action":"edited","repository":{"id":123},"issue":{"id":456,"pull_request":{}}}`)
				bad.Receipt.BodySHA256 = SHA256(bad.Body)
				mac := hmac.New(sha256.New, secret)
				mac.Write(bad.Body)
				bad.Receipt.Headers.Signature256 = "sha256=" + hex.EncodeToString(mac.Sum(nil))
			}
			if test == "foreign-repo" || test == "unknown-event" || test == "expired" || test == "pull-request" {
				bad.Receipt.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(key, IngressSigningBytes(bad.Receipt)))
			}
			if _, e := VerifyGitHubHint(bad, p, secret, time.Now().UTC()); e == nil {
				t.Fatal("untrusted hint accepted")
			}
		})
	}
}
func TestP7IPCPreservesPolicyBytesAndCommandSubjects(t *testing.T) {
	p, e, _, raw := p7authority(t, SHA256([]byte("x")), "events-ack")
	_ = p
	command := i.Command{Version: i.Version, Kind: "IntegrationCommand", Action: "ack", RouteID: e.RouteID, Consumer: "operator", AfterSequence: 2, Policy: raw, Enrollment: engine.Encode(e)}
	command.SubjectSHA256 = IntegrationCommandSubject(command)
	var decoded i.Command
	if err := DecodeRead(engine.Encode(command), &decoded); err != nil || !bytes.Equal(decoded.Policy, raw) || !bytes.Equal(decoded.Enrollment, command.Enrollment) {
		t.Fatal("signature-bearing bytes compacted", err)
	}
	changed := command
	changed.AfterSequence++
	if IntegrationCommandSubject(changed) == command.SubjectSHA256 {
		t.Fatal("ack substitution")
	}
	if _, err := New(nil).IntegrationCommand(context.Background(), command); err == nil {
		t.Fatal("unenrolled standard service accepted command")
	}
}
func TestP7DefaultLimitsAndCurrentExpiry(t *testing.T) {
	limits := DefaultIntegrationLimits()
	if ValidateIntegrationLimits(limits) != nil {
		t.Fatal("defaults")
	}
	limits.MaxOperations = 101
	if ValidateIntegrationLimits(limits) == nil {
		t.Fatal("unbounded batch")
	}
	p, en, _, _ := p7authority(t, SHA256([]byte("x")), "local-inspect")
	if CheckIntegrationWindow(p, en, time.Now().Add(time.Hour)) == nil {
		t.Fatal("expired bookkeeping allowed")
	}
	backend := guardedOperationalRead{request: i.OperationalReadRequest{Limits: DefaultIntegrationLimits()}, guard: func(context.Context) error { return errors.New("revoked") }}
	if _, err := backend.Do(context.Background(), readsource.Source{}, readsource.Request{}); err == nil {
		t.Fatal("revocation bypass")
	}
}

func jsonSchemaValue(value any) any {
	raw := engine.Encode(value)
	out, e := engine.JSONValue(raw)
	if e != nil {
		panic(e)
	}
	return out
}
