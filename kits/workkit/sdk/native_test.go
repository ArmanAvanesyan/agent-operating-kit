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
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func nativeFixture(t *testing.T, host string) (NativeRequest, n.Handle) {
	t.Helper()
	root := readTestDir(t)
	write := func(name string, v any) string {
		p := filepath.Join(root, name+".json")
		if e := os.WriteFile(p, engine.Encode(v), 0600); e != nil {
			t.Fatal(e)
		}
		return p
	}
	mapping := api.Mapping{APIVersion: api.Version, Kind: "Mapping", Metadata: api.Metadata{Id: "native-map", Revision: 1}, Spec: api.MappingSpec{Fields: []api.MappingSpecFieldsItem{}, Lifecycle: []api.MappingSpecLifecycleItem{}, PersonMapping: []api.MappingSpecPersonMappingItem{}}}
	m := write("mapping", mapping)
	assignment := n.Assignment{Version: n.Version, Kind: "Assignment", Steps: []n.Step{{Key: "step-1", Description: "synthetic only", DependsOn: []string{}}}}
	a := write("assignment", assignment)
	op := map[string]string{"codex": "plans.read", "claude-code": "events.observe", "opencode": "todos.read", "cursor": "projection.export", "antigravity": "projection.export"}[host]
	profile := api.NativeTaskProfile{APIVersion: api.Version, Kind: "NativeTaskProfile", Metadata: api.Metadata{Id: "profile", Revision: 1}, Spec: api.NativeTaskProfileSpec{Host: host, HostVersion: "fixture-1", Protocol: "opencode-http/v1", Mode: "observe", Operations: []string{op}, MappingRef: api.Ref{Kind: "Mapping", Id: "native-map", Revision: 1, Sha256: api.Sha(SHA256(engine.Encode(mapping)))}}}
	p := write("profile", profile)
	h := n.Handle{Version: n.Version, Kind: "SessionHandle", Scope: n.Scope{Host: host, HostVersion: "fixture-1", Protocol: profile.Spec.Protocol, Deployment: "disposable", Origin: "http://127.0.0.1:4096", Principal: "synthetic-principal", Workspace: filepath.Clean(root), SessionID: "session-fixture", Owner: "session-owner", AssignmentSHA256: SHA256(engine.Encode(assignment)), ProfileSHA256: SHA256(engine.Encode(profile)), MappingSHA256: SHA256(engine.Encode(mapping)), Mode: "native-only"}, ObservedAt: "2026-10-26T12:00:00Z", ExpiresAt: "2026-10-26T12:05:00Z", SchemaSHA256: NativeOpenCodeSchemaSHA256()}
	q := NativeRequest{Assignment: a, Profile: p, Mapping: m, Handle: write("handle", h), AsOf: "2026-10-26T12:00:02Z", Out: filepath.Join(root, "output")}
	return q, h
}
func nativeEvidence(t *testing.T, q *NativeRequest, h n.Handle, events []n.Event, complete bool) {
	t.Helper()
	for i := range events {
		events[i].Scope = h.Scope
		if events[i].ObservedAt == "" {
			events[i].ObservedAt = "2026-10-26T12:00:01Z"
		}
	}
	ev := n.Evidence{Version: n.Version, Kind: "CapturedNativeEvidence", Scope: h.Scope, ObservedAt: "2026-10-26T12:00:01Z", Complete: complete, Events: events}
	q.Evidence = filepath.Join(filepath.Dir(q.Handle), "evidence.json")
	if e := os.WriteFile(q.Evidence, engine.Encode(ev), 0600); e != nil {
		t.Fatal(e)
	}
}
func TestNativeProjectionBindingsAllHosts(t *testing.T) {
	for _, host := range []string{"codex", "claude-code", "cursor", "opencode", "antigravity"} {
		t.Run(host, func(t *testing.T) {
			q, _ := nativeFixture(t, host)
			r, e := NativeEvaluate(context.Background(), "NativeProjection", q)
			if e != nil {
				t.Fatal(e)
			}
			if r.AuthorizationGranted || r.ExecutionReady || r.ProductionActivated || r.MigrationActivated || r.LiveQualification != "not-established" || len(r.Steps) != 1 {
				t.Fatalf("unsafe report %+v", r)
			}
			again, e := InspectNative(context.Background(), q.Out, q.AsOf)
			if e != nil || len(again.Steps) != 1 {
				t.Fatal(e)
			}
			if _, e = NativeEvaluate(context.Background(), "NativeProjection", q); e == nil {
				t.Fatal("overwrite allowed")
			}
			stale, e := InspectNative(context.Background(), q.Out, "2026-10-26T12:05:00Z")
			if e != nil || stale.Status != "conflict" {
				t.Fatal("stale not reported", e)
			}
			tamper := filepath.Join(q.Out, "assets/assignment.json")
			os.WriteFile(tamper, []byte("{}"), 0600)
			if _, e = InspectNative(context.Background(), q.Out, q.AsOf); e == nil {
				t.Fatal("tamper accepted")
			}
		})
	}
}
func TestNativeClaudeCorrelationAndUnknowns(t *testing.T) {
	q, h := nativeFixture(t, "claude-code")
	events := []n.Event{{ID: "c", Sequence: 1, Type: "tool_use", CallID: "call", Data: json.RawMessage(`{"name":"TaskCreate","input":{"subject":"synthetic"}}`)}, {ID: "r", Sequence: 2, Type: "tool_result", CallID: "call", Data: json.RawMessage(`{"is_error":false,"result":{"task":{"id":"native-1","subject":"synthetic"}}}`)}}
	nativeEvidence(t, &q, h, []n.Event{events[1], events[0], events[0]}, true)
	r, e := NativeEvaluate(context.Background(), "NativeObservation", q)
	if e != nil || !r.Complete || len(r.Entries) != 1 || r.Entries[0].NativeID != "native-1" {
		t.Fatal(r, e)
	}
	for _, test := range []struct {
		name      string
		events    []n.Event
		wantError bool
		count     int
	}{{"unmatched", events[1:], false, 0}, {"unresolved", events[:1], false, 0}, {"failed", []n.Event{events[0], {ID: "r", Sequence: 2, Type: "tool_result", CallID: "call", Data: json.RawMessage(`{"is_error":true,"result":{}}`)}}, false, 0}, {"missing-id", []n.Event{events[0], {ID: "r", Sequence: 2, Type: "tool_result", CallID: "call", Data: json.RawMessage(`{"is_error":false,"result":{}}`)}}, false, 0}, {"conflicting-alias", []n.Event{{ID: "u", Sequence: 1, Type: "tool_use", CallID: "update", Data: json.RawMessage(`{"name":"TaskUpdate","input":{"taskId":"one","id":"two"}}`)}, {ID: "r", Sequence: 2, Type: "tool_result", CallID: "update", Data: json.RawMessage(`{"is_error":false,"result":{"success":true,"taskId":"one"}}`)}}, true, 0}} {
		t.Run(test.name, func(t *testing.T) {
			q, h := nativeFixture(t, "claude-code")
			nativeEvidence(t, &q, h, test.events, true)
			r, e := NativeEvaluate(context.Background(), "NativeObservation", q)
			if (e != nil) != test.wantError || e == nil && (r.Complete || len(r.Entries) != test.count) {
				t.Fatal(r, e)
			}
		})
	}
}
func TestNativeCodexTodoCategoryAndProfiles(t *testing.T) {
	for _, host := range []string{"codex", "opencode"} {
		t.Run(host, func(t *testing.T) {
			q, h := nativeFixture(t, host)
			event := n.Event{ID: "e", Sequence: 0, Type: "session.todo.snapshot", Data: json.RawMessage(`[{"content":"synthetic","status":"completed","priority":"high"}]`)}
			category := "todo"
			if host == "codex" {
				event.Type = "turn/plan/updated"
				event.Data = json.RawMessage(`{"threadId":"session-fixture","turnId":"turn-1","plan":[{"step":"synthetic","status":"completed"}]}`)
				category = "plan"
			}
			nativeEvidence(t, &q, h, []n.Event{event}, true)
			r, e := NativeEvaluate(context.Background(), "NativeObservation", q)
			if e != nil || len(r.Entries) != 1 || r.Entries[0].Category != category || r.Entries[0].NativeID != "" {
				t.Fatal(r, e)
			}
			// Export-only may project, but cannot acquire observation support.
			var p api.NativeTaskProfile
			raw, _ := os.ReadFile(q.Profile)
			DecodeRead(raw, &p)
			p.Spec.Mode = "export-only"
			raw = engine.Encode(p)
			os.WriteFile(q.Profile, raw, 0600)
			h.Scope.ProfileSHA256 = SHA256(raw)
			os.WriteFile(q.Handle, engine.Encode(h), 0600)
			q.Out = filepath.Join(filepath.Dir(q.Handle), "blocked")
			nativeEvidence(t, &q, h, []n.Event{event}, true)
			r, e = NativeEvaluate(context.Background(), "NativeObservation", q)
			if e != nil || r.Status != "blocked" || r.Complete || len(r.Entries) != 0 {
				t.Fatal(r, e)
			}
		})
	}
}
func TestNativeUnsafeInputsAndScope(t *testing.T) {
	for _, name := range []string{"session", "principal", "owner", "workspace", "deployment", "attempt", "digest", "schema", "version", "duplicate-json", "cycle", "symlink"} {
		t.Run(name, func(t *testing.T) {
			q, h := nativeFixture(t, "opencode")
			switch name {
			case "session":
				h.Scope.SessionID = "../foreign"
			case "principal":
				h.Scope.Principal = ""
			case "owner":
				h.Scope.Owner = ""
			case "workspace":
				h.Scope.Workspace = "relative"
			case "deployment":
				h.Scope.Deployment = ""
			case "attempt":
				h.Scope.AttemptID = "forbidden"
			case "digest":
				h.Scope.AssignmentSHA256 = strings.Repeat("0", 64)
			case "schema":
				h.SchemaSHA256 = "bad"
			case "version":
				h.Version = "workkit-native/v999"
			case "duplicate-json":
				os.WriteFile(q.Assignment, []byte(`{"version":"workkit-native/v1","version":"workkit-native/v1","kind":"Assignment","steps":[]}`), 0600)
			case "cycle":
				a := n.Assignment{Version: n.Version, Kind: "Assignment", Steps: []n.Step{{Key: "a", Description: "a", DependsOn: []string{"b"}}, {Key: "b", Description: "b", DependsOn: []string{"a"}}}}
				raw := engine.Encode(a)
				os.WriteFile(q.Assignment, raw, 0600)
				h.Scope.AssignmentSHA256 = SHA256(raw)
			case "symlink":
				target := q.Assignment + ".real"
				os.Rename(q.Assignment, target)
				os.Symlink(target, q.Assignment)
			}
			os.WriteFile(q.Handle, engine.Encode(h), 0600)
			if _, e := NativeEvaluate(context.Background(), "NativeProjection", q); e == nil {
				t.Fatal("unsafe input accepted")
			}
		})
	}
}
func TestNativeEventScopeAndForgedReports(t *testing.T) {
	q, h := nativeFixture(t, "opencode")
	nativeEvidence(t, &q, h, []n.Event{{ID: "e", Type: "session.todo.snapshot", Sequence: 0, Data: json.RawMessage(`[]`)}}, true)
	r, e := NativeEvaluate(context.Background(), "NativeObservation", q)
	if e != nil {
		t.Fatal(e)
	}
	r.Entries = append(r.Entries, n.Entry{SourceEventID: "forged", Category: "task", NativeID: "fake", State: "completed", Value: json.RawMessage(`{}`)})
	os.WriteFile(filepath.Join(q.Out, "bundle.json"), engine.Encode(r), 0600)
	if _, e = InspectNative(context.Background(), q.Out, q.AsOf); e == nil {
		t.Fatal("forged entry accepted")
	}
	var ev n.Evidence
	raw, _ := os.ReadFile(q.Evidence)
	DecodeRead(raw, &ev)
	ev.Events[0].Scope.SessionID = "another"
	os.WriteFile(q.Evidence, engine.Encode(ev), 0600)
	q.Out = ""
	if _, e = NativeEvaluate(context.Background(), "NativeObservation", q); e == nil {
		t.Fatal("cross-session event accepted")
	}
}

type nativeRoundTrip func(*http.Request) (*http.Response, error)

func (f nativeRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func nativeResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}
}
func TestNativeReadAdmissionAndPrivacy(t *testing.T) {
	for _, test := range []string{"success", "credential-revocation", "wrong-session", "redirect", "auth", "permission", "rate-limit", "schema", "secret-echo", "basic-echo", "empty-content", "oversize", "cancel", "timeout"} {
		t.Run(test, func(t *testing.T) {
			q, h := nativeFixture(t, "opencode")
			now := time.Now().UTC()
			h.ObservedAt = now.Add(-time.Second).Format(time.RFC3339Nano)
			h.ExpiresAt = now.Add(4 * time.Minute).Format(time.RFC3339Nano)
			os.WriteFile(q.Handle, engine.Encode(h), 0600)
			q.AsOf = ""
			p := n.ReadPolicy{Version: n.Version, Kind: "NativeReadPolicy", Scope: h.Scope, SchemaSHA256: h.SchemaSHA256, NotBefore: h.ObservedAt, ExpiresAt: h.ExpiresAt}
			policy := filepath.Join(filepath.Dir(q.Handle), "policy.json")
			os.WriteFile(policy, engine.Encode(p), 0600)
			en := n.Enrollment{Version: n.Version, Kind: "NativeReadEnrollment", Purpose: "native-observe", HandleSHA256: SHA256(engine.Encode(h)), PolicySHA256: SHA256(engine.Encode(p)), ImplementationSHA256: NativeImplementationSHA256(), IssuedAt: h.ObservedAt, ExpiresAt: h.ExpiresAt, SecretReference: "protected-opaque-reference", Issuer: "test-only", Signature: "fixture-only"}
			enrollment := filepath.Join(filepath.Dir(q.Handle), "enrollment.json")
			os.WriteFile(enrollment, engine.Encode(en), 0600)
			calls, checks := 0, 0
			revoked := false
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			auth := "Bearer SYNTHETIC-SECRET-CANARY"
			if test == "basic-echo" {
				auth = "Basic " + base64.StdEncoding.EncodeToString([]byte("testuser:CANARY-PASSWORD"))
			}
			backend := &OpenCodeReadTransport{Trust: func(_ context.Context, got n.Handle, _, _ []byte) error {
				checks++
				if !reflect.DeepEqual(got, h) || revoked {
					return errors.New("unverified authority")
				}
				return nil
			}, Credentials: func(context.Context, n.Handle) (string, error) {
				if test == "credential-revocation" {
					revoked = true
				}
				return auth, nil
			}}
			backend.Transport = nativeRoundTrip(func(req *http.Request) (*http.Response, error) {
				calls++
				if req.Method != "GET" || req.URL.Query().Get("directory") != h.Scope.Workspace || req.Header.Get("Authorization") != auth {
					t.Fatal("wrong read request")
				}
				if test == "redirect" {
					res := nativeResponse(302, "")
					res.Header.Set("Location", "http://foreign.invalid/")
					return res, nil
				}
				if test == "auth" {
					return nativeResponse(401, "private"), nil
				}
				if test == "permission" {
					return nativeResponse(403, "private"), nil
				}
				if test == "rate-limit" {
					return nativeResponse(429, "private"), nil
				}
				if test == "cancel" {
					cancel()
					return nil, context.Canceled
				}
				if test == "timeout" {
					return nil, context.DeadlineExceeded
				}
				if strings.HasSuffix(req.URL.Path, "/todo") {
					body := `[{"content":"synthetic","status":"pending","priority":"high"}]`
					switch test {
					case "schema":
						body = `{"not":"todos"}`
					case "empty-content":
						body = `[{"content":"","status":"pending","priority":"high"}]`
					case "oversize":
						body = strings.Repeat("x", engine.MaxDocumentBytes+1)
					case "secret-echo":
						body = `[{"content":"SYNTHETIC-SECRET-CANARY","status":"pending","priority":"high"}]`
					case "basic-echo":
						body = `[{"content":"CANARY-PASSWORD","status":"pending","priority":"high"}]`
					}
					return nativeResponse(200, body), nil
				}
				id := h.Scope.SessionID
				if test == "wrong-session" {
					id = "other"
				}
				return nativeResponse(200, `{"id":"`+id+`","directory":`+string(engine.Encode(h.Scope.Workspace))+`,"title":"synthetic"}`), nil
			})
			svc := New(nil).WithNativeReadBackend(backend)
			r, e := svc.ObserveNative(ctx, q, policy, enrollment)
			if test == "success" {
				if e != nil || !r.Complete || calls != 2 || checks < 4 || len(r.Entries) != 1 {
					t.Fatal(r, e, calls, checks)
				}
				if _, e = InspectNative(context.Background(), q.Out, time.Now().UTC().Format(time.RFC3339Nano)); e != nil {
					t.Fatal("capture cannot reproduce", e)
				}
			} else {
				if e == nil && r.Complete {
					t.Fatal("unsafe success", test)
				}
				if test == "credential-revocation" && calls != 0 {
					t.Fatal("send after revocation")
				}
			}
			if test != "success" && test != "credential-revocation" {
				if e != nil {
					t.Fatal("admitted read failure lost immutable evidence", test, e)
				}
				if _, err := InspectNative(context.Background(), q.Out, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
					t.Fatal("partial capture failed reproduction", test, err)
				}
				var evidence n.Evidence
				raw, _ := os.ReadFile(filepath.Join(q.Out, "assets/evidence.json"))
				if DecodeRead(raw, &evidence) != nil || evidence.Disposition == "observed" || evidence.Complete {
					t.Fatal("failure disposition missing", evidence)
				}
				if test == "timeout" && evidence.Disposition != "timeout" || test == "cancel" && evidence.Disposition != "cancelled" || test == "empty-content" && evidence.Disposition != "schema" {
					t.Fatal(test, evidence.Disposition)
				}
			}
			if raw, e := os.ReadFile(filepath.Join(q.Out, "assets/evidence.json")); e == nil && (bytes.Contains(raw, []byte("SYNTHETIC-SECRET-CANARY")) || bytes.Contains(raw, []byte("CANARY-PASSWORD"))) {
				t.Fatal("secret leaked")
			}
		})
	}
}
func TestNativeDefaultAndReadOnlyBoundaries(t *testing.T) {
	q, _ := nativeFixture(t, "opencode")
	q.AsOf = ""
	if _, e := New(nil).ObserveNative(context.Background(), q, "missing", "missing"); e == nil {
		t.Fatal("default binary read enrollment")
	}
	if e := NativeMutationUnavailable(); e == nil {
		t.Fatal("native mutation enabled")
	}
	for _, host := range NativeHosts() {
		for _, cap := range host.Capabilities {
			if strings.HasPrefix(cap.Operation, "tasks.") && cap.Status != "unsupported" {
				t.Fatal("task API invented")
			}
		}
	}
}

func TestNativeProgressFullIdentityAndReproduction(t *testing.T) {
	ctx := context.Background()
	q, base := operationFixture(t, "github")
	nq, h := nativeFixture(t, "opencode")
	// Match original fixture dates, without renewing them.
	at, _ := nativeTime(q.AsOf)
	h.ObservedAt = at.Add(-time.Second).Format(time.RFC3339Nano)
	h.ExpiresAt = at.Add(4 * time.Minute).Format(time.RFC3339Nano)
	os.WriteFile(nq.Handle, engine.Encode(h), 0600)
	nq.AsOf = q.AsOf
	ev := n.Evidence{Version: n.Version, Kind: "CapturedNativeEvidence", Scope: h.Scope, ObservedAt: h.ObservedAt, Complete: true, Events: []n.Event{{ID: "todo", Sequence: 0, ObservedAt: h.ObservedAt, Scope: h.Scope, Type: "session.todo.snapshot", Data: json.RawMessage(`[{"content":"synthetic","status":"in_progress","priority":"high"}]`)}}}
	nq.Evidence = filepath.Join(filepath.Dir(nq.Handle), "evidence.json")
	os.WriteFile(nq.Evidence, engine.Encode(ev), 0600)
	report, e := NativeEvaluate(ctx, "NativeObservation", nq)
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := os.ReadFile(filepath.Join(nq.Out, "bundle.json"))
	binding, _ := os.ReadFile(q.Binding)
	mapping, _ := os.ReadFile(q.Mapping)
	desiredRaw, _ := os.ReadFile(q.Desired)
	var desired struct {
		Fields map[string]json.RawMessage `json:"fields"`
	}
	json.Unmarshal(desiredRaw, &desired)
	target := api.Identity{Provider: base.Target.Provider, Origin: base.Target.Origin, BasePath: base.Target.BasePath, TenantId: base.Target.TenantID, ContainerId: base.Target.ContainerID, ItemKind: base.Target.ItemKind, NativeId: base.Target.NativeID}
	link := n.ProgressLink{Version: n.Version, Kind: "NativeProgressLink", ObservationSHA256: SHA256(raw), ScopeSHA256: SHA256(engine.Encode(report.Scope)), BindingSHA256: SHA256(binding), MappingSHA256: SHA256(mapping), Target: target, Rules: []n.ProgressRule{{EntryIndex: 0, ExpectedState: "in_progress", Field: "body", Value: desired.Fields["body"]}}}
	path := filepath.Join(filepath.Dir(nq.Handle), "link.json")
	os.WriteFile(path, engine.Encode(link), 0600)
	q.Out = filepath.Join(filepath.Dir(q.Desired), "native-progress")
	draft, e := PlanNativeProgress(ctx, nq.Out, path, q)
	if e != nil || draft.Status != "draft" {
		t.Fatal(e, draft.Diagnostics)
	}
	if _, e = PrepareOperation(ctx, q.Out); e != nil {
		t.Fatal(e)
	}
	if dest := os.Getenv("AOK_TEST_WORKKIT_NATIVE_PROGRESS_OUT"); dest != "" {
		exportNativeProgressFixture(t, dest, nq.Out, path, q)
	}
	for _, part := range []string{"provider", "origin", "basePath", "itemKind"} {
		t.Run(part, func(t *testing.T) {
			bad := link
			switch part {
			case "provider":
				bad.Target.Provider = "linear"
			case "origin":
				bad.Target.Origin = "https://foreign.invalid"
			case "basePath":
				bad.Target.BasePath = "/foreign"
			case "itemKind":
				bad.Target.ItemKind = "task"
			}
			os.WriteFile(path, engine.Encode(bad), 0600)
			q.Out = filepath.Join(filepath.Dir(q.Desired), "bad-"+part)
			if _, e := PlanNativeProgress(ctx, nq.Out, path, q); e == nil {
				t.Fatal("foreign progress correlation accepted")
			}
		})
	}
}
func TestNativeManagedAuthorityNotImplied(t *testing.T) {
	backend := &OpenCodeReadTransport{}
	q, h := nativeFixture(t, "opencode")
	h.Scope.Mode = "managed"
	h.Scope.AttemptID = "runtime-attempt"
	h.Scope.Fence = "fence-1"
	if e := backend.CurrentAttempt(context.Background(), h.Scope); e == nil {
		t.Fatal("missing callback accepted")
	}
	q.Out = ""
	raw, _ := os.ReadFile(q.Profile)
	var p api.NativeTaskProfile
	DecodeRead(raw, &p)
	p.Spec.Mode = "managed"
	raw = engine.Encode(p)
	os.WriteFile(q.Profile, raw, 0600)
	h.Scope.ProfileSHA256 = SHA256(raw)
	os.WriteFile(q.Handle, engine.Encode(h), 0600)
	r, e := NativeEvaluate(context.Background(), "NativeBindingCandidate", q)
	if e != nil || r.CurrentAuthority != "not-established" {
		t.Fatal(r, e)
	}
	backend.Attempt = func(_ context.Context, s n.Scope) error {
		if s.Fence != "fresh" {
			return errors.New("stale fence")
		}
		return nil
	}
	if e := backend.CurrentAttempt(context.Background(), h.Scope); e == nil {
		t.Fatal("stale fence accepted")
	}
}

func TestNativeOperationDraftAndSDKEvidenceBoundary(t *testing.T) {
	q, h := nativeFixture(t, "opencode")
	nativeEvidence(t, &q, h, []n.Event{}, true)
	q.Out = ""
	for _, kind := range []string{"NativeProjection", "NativeBindingCandidate"} {
		if _, e := NativeEvaluate(context.Background(), kind, q); e == nil {
			t.Fatal("SDK projection accepted evidence", kind)
		}
	}
	q.Evidence = ""
	request := n.OperationRequest{Version: n.Version, Kind: "NativeOperationRequest", Operation: "tasks.create", StepKey: "step-1"}
	q.Operation = filepath.Join(filepath.Dir(q.Handle), "request.json")
	os.WriteFile(q.Operation, engine.Encode(request), 0600)
	r, e := NativeEvaluate(context.Background(), "NativeProjection", q)
	if e != nil || r.Status != "blocked" || len(r.Operations) != 1 || r.Operations[0].Request.TargetNativeID != "" {
		t.Fatal(r, e)
	}
	request.TargetNativeID = "invented"
	os.WriteFile(q.Operation, engine.Encode(request), 0600)
	if _, e = NativeEvaluate(context.Background(), "NativeProjection", q); e == nil {
		t.Fatal("creation invented native identity")
	}
}
func TestNativeReadEnrollmentExactFreshness(t *testing.T) {
	q, h := nativeFixture(t, "opencode")
	now, _ := nativeTime(q.AsOf)
	raw, _ := os.ReadFile(q.Handle)
	p := n.ReadPolicy{Version: n.Version, Kind: "NativeReadPolicy", Scope: h.Scope, SchemaSHA256: h.SchemaSHA256, NotBefore: h.ObservedAt, ExpiresAt: h.ExpiresAt}
	policy := engine.Encode(p)
	en := n.Enrollment{Version: n.Version, Kind: "NativeReadEnrollment", Purpose: "native-observe", HandleSHA256: SHA256(raw), PolicySHA256: SHA256(policy), ImplementationSHA256: NativeImplementationSHA256(), IssuedAt: h.ObservedAt, ExpiresAt: h.ExpiresAt, SecretReference: "opaque", Issuer: "fixture", Signature: "fixture"}
	if e := validateNativeRead(h, raw, policy, engine.Encode(en), now); e != nil {
		t.Fatal(e)
	}
	for _, change := range []string{"expired", "future", "purpose", "implementation", "payload", "unbounded"} {
		t.Run(change, func(t *testing.T) {
			bad := en
			switch change {
			case "expired":
				bad.ExpiresAt = now.Format(time.RFC3339Nano)
			case "future":
				bad.IssuedAt = now.Add(time.Second).Format(time.RFC3339Nano)
			case "purpose":
				bad.Purpose = "provider-send"
			case "implementation":
				bad.ImplementationSHA256 = strings.Repeat("0", 64)
			case "payload":
				bad.HandleSHA256 = strings.Repeat("0", 64)
			case "unbounded":
				bad.ExpiresAt = now.Add(time.Hour).Format(time.RFC3339Nano)
			}
			if e := validateNativeRead(h, raw, policy, engine.Encode(bad), now); e == nil {
				t.Fatal("invalid authority accepted")
			}
		})
	}
}

// Explicit opt-in exports synthetic test inputs only; production commands never
// inspect this variable. Normal tests create no persistent fixture directories.
func exportNativeProgressFixture(t *testing.T, out, native, link string, q PlanItemRequest) {
	t.Helper()
	if err := os.Mkdir(out, 0700); err != nil {
		t.Fatal(err)
	}
	paths := map[string]string{"progress-from": native, "link": link, "snapshot": q.Snapshot, "observation": q.Observation, "desired": q.Desired, "mapping": q.Mapping, "binding": q.Binding, "capability": q.Capability, "policy": q.Policy}
	flags := map[string]string{"as-of": q.AsOf}
	for key, source := range paths {
		dest := filepath.Join(out, key)
		info, e := os.Stat(source)
		if e != nil {
			t.Fatal(e)
		}
		if info.IsDir() {
			e = filepath.WalkDir(source, func(path string, entry os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				rel, _ := filepath.Rel(source, path)
				target := filepath.Join(dest, rel)
				if entry.IsDir() {
					return os.MkdirAll(target, 0700)
				}
				if entry.Type()&os.ModeSymlink != 0 {
					return errors.New("unsafe fixture asset")
				}
				raw, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				return os.WriteFile(target, raw, 0600)
			})
		} else {
			raw, err := os.ReadFile(source)
			if err != nil {
				t.Fatal(err)
			}
			e = os.WriteFile(dest, raw, 0600)
		}
		if e != nil {
			t.Fatal(e)
		}
		flags[key] = dest
	}
	// The P5 planner must propagate a blocked capability with domain exit 3.
	raw, e := os.ReadFile(flags["capability"])
	if e != nil {
		t.Fatal(e)
	}
	var fields map[string]json.RawMessage
	if DecodeRead(raw, &fields) != nil {
		t.Fatal("capability fixture")
	}
	fields["supported"] = json.RawMessage(`false`)
	os.WriteFile(flags["capability"], engine.Encode(fields), 0600)
	if e := os.WriteFile(filepath.Join(out, "arguments.json"), engine.Encode(flags), 0600); e != nil {
		t.Fatal(e)
	}
}
