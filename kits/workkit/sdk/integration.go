package sdk

import (
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	i "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/integrationv1"
	z "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/syncv2"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/engine"
	"path/filepath"
	"reflect"
	"strings"
	"time"
)

func IntegrationContractSchema(kind string) (map[string]any, error) {
	records := map[string]any{"configuration": i.Configuration{}, "policy": i.Policy{}, "enrollment": i.Enrollment{}, "event-envelope": i.EventEnvelope{}, "operational-read": i.OperationalReadRequest{}, "checkpoint": i.Checkpoint{}, "command": i.Command{}, "report": i.Report{}, "worker-recovery": i.WorkerRecovery{}, "operational-capture": i.OperationalReadObservation{}, "sync-configuration": z.Configuration{}, "sync-state": z.State{}, "sync-plan": z.Plan{}, "sync-resolution": z.Resolution{}, "sync-resolution-preview": z.ResolutionPreview{}, "sync-invocation": z.Invocation{}}
	v, ok := records[kind]
	if !ok {
		return nil, errors.New("unknown integration contract")
	}
	s := readSchemaType(reflect.TypeOf(v))
	s["$schema"] = "https://json-schema.org/draft/2020-12/schema"
	version := i.Version
	if strings.HasPrefix(kind, "sync-") {
		version = z.Version
	}
	s["$id"] = "https://workkit.example/spec/" + version + "/" + kind + ".schema.json"
	s["properties"].(map[string]any)["version"] = map[string]any{"const": version}
	restrictOperationFlags(s)
	return s, nil
}
func DefaultIntegrationLimits() i.Limits {
	return i.Limits{PollSeconds: 60, JitterSeconds: 10, ProcessSeconds: 900, CycleSeconds: 300, RequestSeconds: 30, Requests: 100, Items: 10000, ResponseBytes: 4 << 20, CaptureBytes: 64 << 20, MaxOperations: 100, MaxHints: 1000, InboxBytes: 64 << 20}
}
func ValidateIntegrationLimits(l i.Limits) error {
	d := DefaultIntegrationLimits()
	a := []int{l.PollSeconds, l.ProcessSeconds, l.CycleSeconds, l.RequestSeconds, l.Requests, l.Items, l.ResponseBytes, l.CaptureBytes, l.MaxOperations, l.MaxHints, l.InboxBytes}
	b := []int{d.PollSeconds, d.ProcessSeconds, d.CycleSeconds, d.RequestSeconds, d.Requests, d.Items, d.ResponseBytes, d.CaptureBytes, d.MaxOperations, d.MaxHints, d.InboxBytes}
	for x := range a {
		if a[x] < 1 || a[x] > b[x] {
			return errors.New("integration limit exceeds bounded policy")
		}
	}
	if l.PollSeconds < 10 || l.JitterSeconds < 0 || l.JitterSeconds > d.JitterSeconds {
		return errors.New("invalid polling cadence")
	}
	return nil
}
func validateIntegrationStore(s i.Store) error {
	if s.Version != i.JournalVersion || s.Kind != "IntegrationJournal" || !nativeDigest(s.ID) || s.Owner == "" || s.Namespace == "" || !filepath.IsAbs(s.Path) || filepath.Clean(s.Path) != s.Path {
		return errors.New("exact fresh journal/v2 identity required")
	}
	return nil
}
func ValidateIntegrationConfiguration(c i.Configuration) error {
	if c.Version != i.Version || c.Kind != "IntegrationConfiguration" || !nativeDigest(c.WorkspaceSHA256) || len(c.Routes) == 0 || len(c.Routes) > 16 || !filepath.IsAbs(c.Listen) || filepath.Clean(c.Listen) != c.Listen {
		return errors.New("invalid integration configuration")
	}
	if e := validateIntegrationStore(c.Store); e != nil {
		return e
	}
	if e := ValidateIntegrationLimits(c.Limits); e != nil {
		return e
	}
	for id, sha := range c.Routes {
		if id == "" || !nativeDigest(sha) {
			return errors.New("exact route/configuration references required")
		}
	}
	return nil
}
func IntegrationSigningBytes(en i.Enrollment) []byte {
	en.Signature = ""
	return append([]byte(i.Version+":"+en.Purpose+"\n"), engine.Encode(en)...)
}
func IngressSigningBytes(r i.IngressReceipt) []byte {
	r.Signature = ""
	return append([]byte(i.Version+":github-ingress\n"), engine.Encode(r)...)
}
func integrationSignature(issuer, signature string, body []byte, p i.Policy) error {
	seen := map[string]bool{}
	var key ed25519.PublicKey
	for _, v := range p.Issuers {
		raw, e := base64.StdEncoding.DecodeString(v.PublicKey)
		if e != nil || len(raw) != 32 || seen[v.ID] || v.ID == "" {
			return errors.New("invalid issuer set")
		}
		seen[v.ID] = true
		if v.ID == issuer {
			key = raw
		}
	}
	sig, e := base64.StdEncoding.DecodeString(signature)
	if e != nil || len(key) != 32 || !ed25519.Verify(key, body, sig) {
		return errors.New("invalid domain-separated integration signature")
	}
	return nil
}
func ValidateIntegrationAuthority(policy, enrollment []byte, subject, purpose string, now time.Time) (i.Policy, i.Enrollment, error) {
	var p i.Policy
	var e i.Enrollment
	if DecodeRead(policy, &p) != nil || DecodeRead(enrollment, &e) != nil || p.Version != i.Version || p.Kind != "IntegrationPolicy" || e.Version != i.Version || e.Kind != "IntegrationEnrollment" || e.ID == "" || e.Purpose != purpose || e.SubjectSHA256 != subject || !nativeDigest(subject) || e.PolicySHA256 != SHA256(policy) || e.ImplementationSHA256 != NativeImplementationSHA256() || e.Store != p.Store || validateIntegrationStore(e.Store) != nil {
		return p, e, errors.New("exact integration authority subject/store required")
	}
	if ValidateIntegrationLimits(p.Limits) != nil || len(p.Routes) == 0 || len(p.Routes) > 16 {
		return p, e, errors.New("bounded route policy required")
	}
	for id, digest := range p.Routes {
		if id == "" || !nativeDigest(digest) {
			return p, e, errors.New("invalid route policy digest")
		}
	}
	if e.RouteID != "" && !nativeDigest(p.Routes[e.RouteID]) {
		return p, e, errors.New("route outside protected policy")
	}
	if len(p.Issuers) == 0 || len(p.Issuers) > 32 || len(p.RevokedIDs) > 10000 {
		return p, e, errors.New("authority policy bounds")
	}
	allowed := false
	for _, v := range p.AllowedPurposes {
		allowed = allowed || v == purpose
	}
	for _, v := range p.RevokedIDs {
		if v == e.ID || v == e.Issuer {
			return p, e, errors.New("revoked integration authority")
		}
	}
	a, ae := nativeTime(p.NotBefore)
	b, be := nativeTime(p.ExpiresAt)
	if !allowed || ae != nil || be != nil || now.Before(a) || !now.Before(b) || nativeWindow(e.IssuedAt, e.ExpiresAt) != nil {
		return p, e, errors.New("integration policy expiry/purpose")
	}
	start, _ := nativeTime(e.IssuedAt)
	end, _ := nativeTime(e.ExpiresAt)
	if now.Before(start) || !now.Before(end) || end.After(b) {
		return p, e, errors.New("expired integration enrollment")
	}
	if err := integrationSignature(e.Issuer, e.Signature, IntegrationSigningBytes(e), p); err != nil {
		return p, e, err
	}
	return p, e, nil
}

// VerifyGitHubHint verifies bytes and receipt only. It grants no read or mutation
// authority, and does not treat unsigned GitHub headers as provider attestations.
func VerifyGitHubHint(envelope i.EventEnvelope, p i.Policy, secret []byte, now time.Time) (i.Hint, error) {
	var hint i.Hint
	r := envelope.Receipt
	if envelope.Version != i.Version || envelope.Kind != "GitHubEventEnvelope" || envelope.Provider != "github" || envelope.RouteID == "" || r.RouteID != envelope.RouteID || r.ID == "" || r.Ingress == "" || r.ConnectionID == "" || r.RepositoryID == "" || r.Origin != "https://api.github.com" || len(envelope.Body) > 4<<20 || len(secret) < 16 || r.BodySHA256 != SHA256(envelope.Body) || r.Headers.DeliveryID == "" || len(r.Headers.DeliveryID) > 256 || nativeWindow(r.ReceivedAt, r.ExpiresAt) != nil {
		return hint, errors.New("invalid scoped GitHub ingress envelope")
	}
	received, _ := nativeTime(r.ReceivedAt)
	until, _ := nativeTime(r.ExpiresAt)
	if now.Before(received) || !now.Before(until) {
		return hint, errors.New("ingress receipt expired; provider age remains unknown")
	}
	for _, id := range p.RevokedIDs {
		if id == r.ID || id == r.Issuer || id == r.Ingress {
			return hint, errors.New("revoked ingress identity")
		}
	}
	if e := integrationSignature(r.Issuer, r.Signature, IngressSigningBytes(r), p); e != nil {
		return hint, e
	}
	expected := hmac.New(sha256.New, secret)
	expected.Write(envelope.Body)
	supplied, e := hex.DecodeString(strings.TrimPrefix(r.Headers.Signature256, "sha256="))
	if e != nil || !strings.HasPrefix(r.Headers.Signature256, "sha256=") || !hmac.Equal(expected.Sum(nil), supplied) {
		return hint, errors.New("GitHub body signature failed")
	}
	var body struct {
		Action     string `json:"action"`
		Repository struct {
			ID json.Number `json:"id"`
		} `json:"repository"`
		Issue struct {
			ID          json.Number     `json:"id"`
			PullRequest json.RawMessage `json:"pull_request,omitempty"`
		} `json:"issue"`
	}
	// Provider payload has extensible fields; strict JSON/duplicate validation is
	// retained, while unsupported payload fields remain in the original asset.
	value, e := engine.JSONValue(envelope.Body)
	if e != nil {
		return hint, e
	}
	raw, _ := json.Marshal(value)
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.UseNumber()
	if d.Decode(&body) != nil || body.Repository.ID.String() != r.RepositoryID || body.Issue.ID == "" || len(body.Issue.PullRequest) > 0 {
		return hint, errors.New("issue repository scope or PR semantics mismatch")
	}
	if r.Headers.Event != "issues" && r.Headers.Event != "issue_comment" {
		return hint, errors.New("unsupported webhook event")
	}
	if body.Action == "" {
		return hint, errors.New("missing event action")
	}
	hint = i.Hint{ID: r.ID, RouteID: envelope.RouteID, ConnectionID: r.ConnectionID, RepositoryID: r.RepositoryID, DeliveryID: r.Headers.DeliveryID, BodySHA256: r.BodySHA256, ReceivedAt: r.ReceivedAt, Disposition: "refresh-hint"}
	return hint, nil
}

// CheckIntegrationWindow rechecks an already signature-verified immutable session
// before local bookkeeping. Dynamic custody/revocation is still checked by Trust.
func CheckIntegrationWindow(p i.Policy, e i.Enrollment, now time.Time) error {
	if nativeWindow(e.IssuedAt, e.ExpiresAt) != nil {
		return errors.New("invalid authority window")
	}
	start, se := nativeTime(e.IssuedAt)
	end, ee := nativeTime(e.ExpiresAt)
	a, ae := nativeTime(p.NotBefore)
	b, be := nativeTime(p.ExpiresAt)
	if se != nil || ee != nil || ae != nil || be != nil || now.Before(start) || !now.Before(end) || now.Before(a) || !now.Before(b) {
		return errors.New("authority expired before bookkeeping")
	}
	for _, id := range p.RevokedIDs {
		if id == e.ID || id == e.Issuer {
			return errors.New("revoked authority")
		}
	}
	return nil
}
