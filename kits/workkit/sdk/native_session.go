package sdk

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	n "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/nativev1"
	api "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/v1alpha1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/engine"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"time"
)

type NativeReadCapture struct{ Session, Todos []byte }
type nativeReadFailure struct{ disposition string }

func (e *nativeReadFailure) Error() string { return "native read failed closed" }
func nativeReadFail(s string) error        { return &nativeReadFailure{s} }

// NativeReadBackend is protected embedding plumbing, not a task-selectable plugin.
// Current must verify independent signature custody, revocation, exact enrollment,
// endpoint/account/session qualification and (managed only) current runtime fence.
type NativeReadBackend interface {
	Current(context.Context, n.Handle, []byte, []byte) error
	CurrentAttempt(context.Context, n.Scope) error
	Read(context.Context, n.Handle, func(context.Context) error) (NativeReadCapture, error)
}

func (s *Service) WithNativeReadBackend(b NativeReadBackend) *Service {
	v := *s
	v.native = b
	return &v
}

// NativeOpenCodeSchemaSHA256 pins the exact supported response adapter shape.
// It is not a claim that any installed server conforms to this schema.
func NativeOpenCodeSchemaSHA256() string {
	return SHA256([]byte("opencode-session-todo/v1;session:{id,directory};todo:{content,status,priority,id?};no-task-identity"))
}
func validateNativeRead(h n.Handle, handle, policy, enrollment []byte, now time.Time) error {
	if e := validateNativeHandle(h); e != nil {
		return e
	}
	if h.Scope.Host != "opencode" || h.Scope.Protocol != "opencode-http/v1" || h.SchemaSHA256 != NativeOpenCodeSchemaSHA256() {
		return errors.New("unsupported pinned OpenCode schema/protocol")
	}
	var p n.ReadPolicy
	var en n.Enrollment
	if DecodeRead(policy, &p) != nil || DecodeRead(enrollment, &en) != nil {
		return errors.New("invalid native read enrollment")
	}
	if p.Version != n.Version || p.Kind != "NativeReadPolicy" || !reflect.DeepEqual(p.Scope, h.Scope) || p.SchemaSHA256 != h.SchemaSHA256 || en.Version != n.Version || en.Kind != "NativeReadEnrollment" || en.Purpose != "native-observe" || en.HandleSHA256 != SHA256(handle) || en.PolicySHA256 != SHA256(policy) || en.ImplementationSHA256 != engine.ImplementationSHA256 || en.Issuer == "" || en.Signature == "" || en.SecretReference == "" {
		return errors.New("native read scope/purpose mismatch")
	}
	for _, w := range [][2]string{{h.ObservedAt, h.ExpiresAt}, {p.NotBefore, p.ExpiresAt}, {en.IssuedAt, en.ExpiresAt}} {
		if e := nativeWindow(w[0], w[1]); e != nil {
			return e
		}
		a, _ := nativeTime(w[0])
		b, _ := nativeTime(w[1])
		if now.Before(a) || !now.Before(b) {
			return errors.New("expired or future native read evidence")
		}
	}
	return nil
}

// ObserveNative reads only through injected protected enrollment. Standard binaries
// cannot enable it by passing enrollment JSON, environment variables or CLI flags.
func (s *Service) ObserveNative(ctx context.Context, q NativeRequest, policyPath, enrollmentPath string) (n.Report, error) {
	if q.AsOf != "" || q.Evidence != "" || q.Out == "" {
		return n.Report{}, nativeError(errors.New("connected reads use current time and fresh output"))
	}
	if s.native == nil {
		return n.Report{}, failure("native.read-unenrolled", "protected native reader is not installed", 3, nil)
	}
	files, e := nativeInputs(ctx, q)
	if e != nil {
		return n.Report{}, nativeError(e)
	}
	policy, e := ReadLocal(ctx, policyPath)
	if e != nil {
		return n.Report{}, nativeError(e)
	}
	enrollment, e := ReadLocal(ctx, enrollmentPath)
	if e != nil {
		return n.Report{}, nativeError(e)
	}
	var h n.Handle
	if e := DecodeRead(files["assets/handle.json"], &h); e != nil {
		return n.Report{}, nativeError(e)
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	current := func(c context.Context) error {
		if e := c.Err(); e != nil {
			return e
		}
		if e := validateNativeRead(h, files["assets/handle.json"], policy, enrollment, time.Now().UTC()); e != nil {
			return e
		}
		if h.Scope.Mode == "managed" {
			if e := s.native.CurrentAttempt(c, cloneRead(h.Scope)); e != nil {
				return e
			}
		}
		return s.native.Current(c, cloneRead(h), append([]byte(nil), policy...), append([]byte(nil), enrollment...))
	}
	if e := current(ctx); e != nil {
		return n.Report{}, failure("native.read-blocked", "current native session authority is unverified", 3, e)
	}
	// Validate every assignment/profile/mapping binding before opening the transport.
	if _, e = nativeBuild(files, "NativeBindingCandidate", time.Now().UTC().Format(time.RFC3339Nano)); e != nil {
		return n.Report{}, nativeError(e)
	}
	var profile api.NativeTaskProfile
	if setupDocument(files["assets/profile.json"], "NativeTaskProfile", &profile) != nil || nativeObservationProfile(profile, h) != nil {
		return n.Report{}, failure("native.profile-unavailable", "profile does not permit scoped observation", 3, nil)
	}
	reservation, e := engine.ReserveBundle(ctx, q.Out)
	if e != nil {
		return n.Report{}, failure("native.output", "fresh native output directory required", 2, e)
	}
	capture, e := s.native.Read(ctx, cloneRead(h), current)
	raw := capture.Todos
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	evidence := n.Evidence{Version: n.Version, Kind: "CapturedNativeEvidence", Scope: h.Scope, ObservedAt: stamp, Disposition: "observed", Complete: e == nil, Events: []n.Event{}}
	if e == nil {
		var todos []NativeTodo
		if len(raw) > engine.MaxDocumentBytes || DecodeRead(raw, &todos) != nil || len(todos) > 10000 {
			e = nativeReadFail("schema")
			evidence.Complete = false
		}
		for _, todo := range todos {
			if todo.Content == "" || todo.Status == "" || todo.Priority == "" {
				e = nativeReadFail("schema")
				evidence.Complete = false
			}
		}
		var identity map[string]json.RawMessage
		var id, directory string
		if len(capture.Session) > engine.MaxDocumentBytes || DecodeRead(capture.Session, &identity) != nil || json.Unmarshal(identity["id"], &id) != nil || json.Unmarshal(identity["directory"], &directory) != nil || id != h.Scope.SessionID || directory != h.Scope.Workspace {
			e = nativeReadFail("identity-conflict")
			evidence.Complete = false
		}
		if e == nil {
			evidence.Events = append(evidence.Events, n.Event{ID: "identity-" + SHA256(capture.Session), Sequence: 0, ObservedAt: stamp, Scope: h.Scope, Type: "session.identity.snapshot", Data: append(json.RawMessage(nil), capture.Session...)})
			evidence.Events = append(evidence.Events, n.Event{ID: "snapshot-" + SHA256(raw), Sequence: 1, ObservedAt: stamp, Scope: h.Scope, Type: "session.todo.snapshot", Data: append(json.RawMessage(nil), raw...)})
		}
	}
	if e != nil {
		evidence.Disposition = "incomplete"
		var fail *nativeReadFailure
		if errors.As(e, &fail) {
			evidence.Disposition = fail.disposition
		}
		if errors.Is(ctx.Err(), context.Canceled) {
			evidence.Disposition = "cancelled"
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			evidence.Disposition = "timeout"
		}
	}
	files["assets/evidence.json"] = engine.Encode(evidence)
	files["assets/read-policy.json"] = policy
	files["assets/read-enrollment.json"] = enrollment
	r, buildErr := nativeBuild(files, "NativeObservation", stamp)
	if buildErr != nil {
		return r, nativeError(buildErr)
	}
	// Transport failure is a reproducible partial capture. No raw error text or
	// unvalidated response bytes enter evidence. The original observation is kept.
	preserve, cleanup := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cleanup()
	if e = reservation.Write(preserve, nativeBundleFiles(&r, files)); e != nil {
		return r, e
	}
	if e = reservation.Sync(preserve); e != nil {
		return r, e
	}
	return r, nil
}

// OpenCodeReadTransport permits exactly two GETs for an existing session.
// Trust and Credentials must come from a protected compiled embedding. Credentials
// returns a fully formed authorization value; it is never serialized or logged.
type OpenCodeReadTransport struct {
	Transport   http.RoundTripper
	Attempt     func(context.Context, n.Scope) error
	Trust       func(context.Context, n.Handle, []byte, []byte) error
	Credentials func(context.Context, n.Handle) (string, error)
}

func (b *OpenCodeReadTransport) CurrentAttempt(ctx context.Context, scope n.Scope) error {
	if b == nil || b.Attempt == nil {
		return errors.New("protected current attempt/fence authority missing")
	}
	return b.Attempt(ctx, scope)
}
func (b *OpenCodeReadTransport) Current(ctx context.Context, h n.Handle, p, e []byte) error {
	if b == nil || b.Transport == nil || b.Trust == nil || b.Credentials == nil {
		return errors.New("protected native ports required")
	}
	return b.Trust(ctx, h, p, e)
}
func (b *OpenCodeReadTransport) Read(ctx context.Context, h n.Handle, guard func(context.Context) error) (NativeReadCapture, error) {
	if b == nil || b.Transport == nil || b.Credentials == nil || guard == nil || nativeScope(h.Scope) != nil || h.Scope.Host != "opencode" {
		return NativeReadCapture{}, errors.New("native transport unavailable")
	}
	if e := guard(ctx); e != nil {
		return NativeReadCapture{}, e
	}
	auth, e := b.Credentials(ctx, h)
	if e != nil || auth == "" {
		return NativeReadCapture{}, errors.New("native credential unavailable")
	}
	client := &http.Client{Transport: b.Transport, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("native redirects rejected") }}
	get := func(suffix string) ([]byte, error) {
		reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		// Recheck after credentials and before each request; no cached admission.
		if e := guard(reqCtx); e != nil {
			return nil, e
		}
		u := h.Scope.Origin + "/session/" + url.PathEscape(h.Scope.SessionID) + suffix + "?directory=" + url.QueryEscape(h.Scope.Workspace)
		req, e := http.NewRequestWithContext(reqCtx, http.MethodGet, u, nil)
		if e != nil {
			return nil, errors.New("invalid scoped native request")
		}
		req.Header.Set("Authorization", auth)
		res, e := client.Do(req)
		if e != nil {
			if errors.Is(e, context.DeadlineExceeded) || errors.Is(reqCtx.Err(), context.DeadlineExceeded) {
				return nil, nativeReadFail("timeout")
			}
			if errors.Is(e, context.Canceled) {
				return nil, nativeReadFail("cancelled")
			}
			return nil, errors.New("native request failed")
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			status := "incomplete"
			switch res.StatusCode {
			case 401:
				status = "authentication"
			case 403:
				status = "permission"
			case 429:
				status = "rate-limit"
			}
			return nil, nativeReadFail(status)
		}
		raw, e := io.ReadAll(io.LimitReader(res.Body, engine.MaxDocumentBytes+1))
		if errors.Is(e, context.DeadlineExceeded) || errors.Is(reqCtx.Err(), context.DeadlineExceeded) {
			return nil, nativeReadFail("timeout")
		}
		if e != nil || len(raw) > engine.MaxDocumentBytes || nativeCredentialEcho(raw, auth) {
			return nil, errors.New("native read bound or privacy violation")
		}
		return raw, nil
	}
	raw, e := get("")
	if e != nil {
		return NativeReadCapture{}, e
	}
	var identity map[string]json.RawMessage
	if DecodeRead(raw, &identity) != nil {
		return NativeReadCapture{}, errors.New("invalid session response")
	}
	var id, directory string
	if json.Unmarshal(identity["id"], &id) != nil || json.Unmarshal(identity["directory"], &directory) != nil || id != h.Scope.SessionID || directory != h.Scope.Workspace {
		return NativeReadCapture{}, nativeReadFail("identity-conflict")
	}
	todos, e := get("/todo")
	return NativeReadCapture{Session: raw, Todos: todos}, e
}

func nativeCredentialEcho(raw []byte, auth string) bool {
	if bytes.Contains(raw, []byte(auth)) {
		return true
	}
	parts := strings.Fields(auth)
	if len(parts) != 2 {
		return true
	}
	if bytes.Contains(raw, []byte(parts[1])) {
		return true
	}
	if strings.EqualFold(parts[0], "Basic") {
		decoded, e := base64.StdEncoding.DecodeString(parts[1])
		if e != nil {
			return true
		}
		for _, part := range strings.SplitN(string(decoded), ":", 2) {
			if len(part) > 0 && bytes.Contains(raw, []byte(part)) {
				return true
			}
		}
	}
	return false
}
