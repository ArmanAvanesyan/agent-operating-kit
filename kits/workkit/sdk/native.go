package sdk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	n "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/nativev1"
	api "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/v1alpha1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/engine"
	"net/url"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"
)

func NativeImplementationSHA256() string { return engine.ImplementationSHA256 }
func NativeContractSchema(kind string) (map[string]any, error) {
	records := map[string]any{"assignment": n.Assignment{}, "handle": n.Handle{}, "evidence": n.Evidence{}, "report": n.Report{}, "policy": n.ReadPolicy{}, "enrollment": n.Enrollment{}, "progress-link": n.ProgressLink{}, "operation-request": n.OperationRequest{}}
	v, ok := records[kind]
	if !ok {
		return nil, errors.New("unknown native contract")
	}
	s := readSchemaType(reflect.TypeOf(v))
	s["$schema"] = "https://json-schema.org/draft/2020-12/schema"
	s["$id"] = "https://workkit.example/spec/native/v1/" + kind + ".schema.json"
	s["properties"].(map[string]any)["version"] = map[string]any{"const": n.Version}
	kinds := map[string][]string{"assignment": {"Assignment"}, "handle": {"SessionHandle"}, "evidence": {"CapturedNativeEvidence"}, "report": {"NativeProjection", "NativeBindingCandidate", "NativeObservation", "NativeReconciliation"}, "policy": {"NativeReadPolicy"}, "enrollment": {"NativeReadEnrollment"}, "progress-link": {"NativeProgressLink"}, "operation-request": {"NativeOperationRequest"}}
	s["properties"].(map[string]any)["kind"] = map[string]any{"enum": kinds[kind]}
	restrictNativeSchema(s)
	restrictOperationFlags(s)
	return s, nil
}
func NativeHosts() []n.Host {
	hosts := []n.Host{}
	for _, h := range []string{"codex", "claude-code", "cursor", "opencode", "antigravity"} {
		source := "design-projection"
		op := "projection.export"
		category := "implementation"
		caps := []n.Capability{{Operation: op, Status: "implemented", Category: category, Source: source}}
		switch h {
		case "codex":
			caps = append(caps, n.Capability{Operation: "plans.read", Status: "import-only", Category: "published-interface", Source: "https://learn.chatgpt.com/docs/app-server"})
		case "claude-code":
			caps = append(caps, n.Capability{Operation: "events.observe", Status: "import-only", Category: "published-interface", Source: "https://code.claude.com/docs/en/agent-sdk/todo-tracking"})
		case "opencode":
			caps = append(caps, n.Capability{Operation: "todos.read", Status: "protected-read-port", Category: "published-interface", Source: "https://opencode.ai/docs/server/"})
		}
		for _, op := range []string{"plans.update", "tasks.list", "tasks.get", "tasks.create", "tasks.update", "tasks.dependencies", "tasks.assign", "teams.tasks", "continuation.bind"} {
			caps = append(caps, n.Capability{Operation: op, Status: "unsupported", Category: "not-established", Source: "direct-inference-free-interface-unqualified"})
		}
		hosts = append(hosts, n.Host{Host: h, Capabilities: caps, LiveQualification: "pending"})
	}
	return hosts
}
func nativeScope(s n.Scope) error {
	found := false
	for _, h := range NativeHosts() {
		found = found || s.Host == h.Host
	}
	if !found {
		return errors.New("unknown native host")
	}
	for _, v := range []string{s.HostVersion, s.Protocol, s.Deployment, s.Principal, s.Workspace, s.SessionID, s.Owner} {
		if v == "" || len(v) > 1024 || strings.ContainsAny(v, "\x00\r\n") {
			return errors.New("exact native scope required")
		}
	}
	if !filepath.IsAbs(s.Workspace) || filepath.Clean(s.Workspace) != s.Workspace {
		return errors.New("canonical workspace required")
	}
	u, e := url.Parse(s.Origin)
	if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || u.Opaque != "" || u.String() != s.Origin || u.Scheme != "https" && (u.Scheme != "http" || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost" && u.Hostname() != "::1")) {
		return errors.New("fixed canonical native origin required")
	}
	if strings.ContainsAny(s.SessionID, "/\\?#%") || s.SessionID == "." || s.SessionID == ".." {
		return errors.New("unsafe session identifier")
	}
	for _, h := range []string{s.AssignmentSHA256, s.ProfileSHA256, s.MappingSHA256} {
		if !nativeDigest(h) {
			return errors.New("exact input digest required")
		}
	}
	if s.Mode == "native-only" {
		if s.AttemptID != "" || s.Fence != "" {
			return errors.New("native-only cannot claim runtime authority")
		}
	} else if s.Mode != "managed" || s.AttemptID == "" || s.Fence == "" {
		return errors.New("managed attempt and fence required")
	}
	return nil
}
func nativeDigest(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func nativeTime(s string) (time.Time, error) { return time.Parse(time.RFC3339Nano, s) }
func nativeWindow(start, end string) error {
	a, e := nativeTime(start)
	b, f := nativeTime(end)
	if e != nil || f != nil || !b.After(a) || b.Sub(a) > 5*time.Minute {
		return errors.New("native evidence window must be bounded to five minutes")
	}
	return nil
}
func validateNativeHandle(h n.Handle) error {
	if h.Version != n.Version || h.Kind != "SessionHandle" || !nativeDigest(h.SchemaSHA256) {
		return errors.New("invalid native session handle")
	}
	if e := nativeScope(h.Scope); e != nil {
		return e
	}
	return nativeWindow(h.ObservedAt, h.ExpiresAt)
}
func nativeDiagnostic(r *n.Report, code, message string) {
	r.Diagnostics = append(r.Diagnostics, api.Diagnostic{Code: code, Field: "native", Message: message, Severity: "warning"})
}

// NativeRequest accepts explicit safe files only; neither task text nor a record
// installs a transport, authority callback, session owner or executable plugin.
type NativeRequest struct{ Assignment, Profile, Mapping, Handle, Evidence, Operation, AsOf, Out string }

func nativeInputs(ctx context.Context, q NativeRequest) (map[string][]byte, error) {
	files := map[string][]byte{}
	for key, p := range map[string]string{"assignment": q.Assignment, "profile": q.Profile, "mapping": q.Mapping, "handle": q.Handle, "evidence": q.Evidence, "operation-request": q.Operation} {
		if (key == "evidence" || key == "operation-request") && p == "" {
			continue
		}
		if p == "" {
			return nil, errors.New("missing native input")
		}
		raw, e := ReadLocal(ctx, p)
		if e != nil {
			return nil, e
		}
		files["assets/"+key+".json"] = raw
	}
	return files, nil
}
func nativeBuild(files map[string][]byte, kind, asof string) (n.Report, error) {
	r := n.Report{AdapterVersion: n.AdapterVersion, ImplementationSHA256: NativeImplementationSHA256(), Version: n.Version, Kind: kind, AsOf: asof, Status: "ok", CurrentAuthority: "not-established", LiveQualification: "not-established", Inputs: map[string]string{}, Steps: []n.Step{}, Operations: []n.OperationDraft{}, Entries: []n.Entry{}, Diagnostics: []api.Diagnostic{}, Assets: []api.Asset{}}
	if kind != "NativeProjection" && kind != "NativeBindingCandidate" && kind != "NativeObservation" && kind != "NativeReconciliation" {
		return r, errors.New("unknown native report kind")
	}
	if _, has := files["assets/evidence.json"]; has && kind != "NativeObservation" && kind != "NativeReconciliation" {
		return r, errors.New("evidence is not accepted by projection/binding commands")
	}
	now, e := nativeTime(asof)
	if e != nil {
		return r, e
	}
	var a n.Assignment
	var p api.NativeTaskProfile
	var m api.Mapping
	var h n.Handle
	for name, target := range map[string]any{"assignment": &a, "handle": &h} {
		if e := DecodeRead(files["assets/"+name+".json"], target); e != nil {
			return r, e
		}
	}
	if a.Version != n.Version || a.Kind != "Assignment" || len(a.Steps) > 1000 {
		return r, errors.New("invalid native assignment")
	}
	if e := setupDocument(files["assets/profile.json"], "NativeTaskProfile", &p); e != nil {
		return r, e
	}
	if e := setupDocument(files["assets/mapping.json"], "Mapping", &m); e != nil {
		return r, e
	}
	if e := validateNativeHandle(h); e != nil {
		return r, e
	}
	r.Scope = h.Scope
	r.ObservedAt = h.ObservedAt
	for _, name := range []string{"assignment", "profile", "mapping", "handle"} {
		r.Inputs[name] = SHA256(files["assets/"+name+".json"])
	}
	if r.Inputs["assignment"] != h.Scope.AssignmentSHA256 || r.Inputs["profile"] != h.Scope.ProfileSHA256 || r.Inputs["mapping"] != h.Scope.MappingSHA256 || p.Spec.Host != h.Scope.Host || p.Spec.HostVersion != h.Scope.HostVersion || p.Spec.Protocol != h.Scope.Protocol || string(p.Spec.MappingRef.Sha256) != h.Scope.MappingSHA256 || p.Spec.MappingRef.Id != m.Metadata.Id || p.Spec.MappingRef.Revision != m.Metadata.Revision {
		return r, errors.New("native configuration/profile/mapping identity mismatch")
	}
	if h.Scope.Mode == "managed" && p.Spec.Mode != "managed" || h.Scope.Mode == "native-only" && p.Spec.Mode == "managed" {
		return r, errors.New("profile/authority mode mismatch")
	}
	at, _ := nativeTime(h.ObservedAt)
	end, _ := nativeTime(h.ExpiresAt)
	if now.Before(at) || !now.Before(end) {
		r.Status = "conflict"
		nativeDiagnostic(&r, "native.stale", "session evidence is stale or future-dated")
	}
	seen := map[string]bool{}
	for _, step := range a.Steps {
		if step.Key == "" || step.Description == "" || seen[step.Key] {
			return r, errors.New("duplicate or empty plan step")
		}
		seen[step.Key] = true
	}
	for _, step := range a.Steps {
		deps := map[string]bool{}
		for _, dep := range step.DependsOn {
			if !seen[dep] || dep == step.Key || deps[dep] {
				return r, errors.New("unresolved or duplicate dependency")
			}
			deps[dep] = true
		}
	}
	visiting, done := map[string]bool{}, map[string]bool{}
	byKey := map[string]n.Step{}
	for _, s := range a.Steps {
		byKey[s.Key] = s
	}
	var visit func(string) bool
	visit = func(k string) bool {
		if visiting[k] {
			return false
		}
		if done[k] {
			return true
		}
		visiting[k] = true
		for _, d := range byKey[k].DependsOn {
			if !visit(d) {
				return false
			}
		}
		visiting[k] = false
		done[k] = true
		return true
	}
	for _, s := range a.Steps {
		if !visit(s.Key) {
			return r, errors.New("cyclic plan dependency")
		}
	}
	r.Steps = append(r.Steps, a.Steps...)
	r.Complete = true
	if raw, ok := files["assets/operation-request.json"]; ok {
		if kind != "NativeProjection" {
			return r, errors.New("native operation requests require plan")
		}
		var request n.OperationRequest
		if DecodeRead(raw, &request) != nil || request.Version != n.Version || request.Kind != "NativeOperationRequest" || !seen[request.StepKey] {
			return r, errors.New("invalid native operation request")
		}
		switch request.Operation {
		case "tasks.create":
			if request.TargetNativeID != "" {
				return r, errors.New("creation cannot invent a native ID")
			}
		case "tasks.update", "tasks.dependencies", "tasks.assign", "plans.update":
			if request.TargetNativeID == "" {
				return r, errors.New("exact proposed target identity required")
			}
		default:
			return r, errors.New("unsupported native operation request")
		}
		r.Inputs["operation-request"] = SHA256(raw)
		key := struct {
			Scope   n.Scope
			Request n.OperationRequest
		}{h.Scope, request}
		r.Operations = append(r.Operations, n.OperationDraft{LogicalID: "native-intent-" + SHA256(engine.Encode(key)), Request: request, Status: "blocked", Reason: "no-qualified-native-mutation-api-or-admission"})
		r.Status = "blocked"
		nativeDiagnostic(&r, "native.mutation-unavailable", "operation intent is non-executing; no writer or native admission is installed")
	}
	if kind == "NativeProjection" {
		nativeDiagnostic(&r, "native.projection-only", "projection does not create native records")
	}
	if kind == "NativeObservation" || kind == "NativeReconciliation" {
		if err := nativeObservationProfile(p, h); err != nil {
			r.Status = "blocked"
			r.Complete = false
			nativeDiagnostic(&r, "native.profile-unavailable", "profile does not permit the scoped observation operation")
			if raw, ok := files["assets/evidence.json"]; ok {
				r.Inputs["evidence"] = SHA256(raw)
			}
			return r, nil
		}
	}
	if raw, ok := files["assets/evidence.json"]; ok {
		r.Inputs["evidence"] = SHA256(raw)
		var ev n.Evidence
		if e := DecodeRead(raw, &ev); e != nil {
			return r, e
		}
		if e := nativeObserve(&r, ev, now); e != nil {
			return r, e
		}
	} else if kind == "NativeObservation" || kind == "NativeReconciliation" {
		return r, errors.New("observation evidence required")
	}
	if !r.Complete && r.Status == "ok" {
		r.Status = "partial"
	}
	return r, nil
}
func NativeEvaluate(ctx context.Context, kind string, q NativeRequest) (n.Report, error) {
	if kind != "NativeProjection" && kind != "NativeBindingCandidate" && kind != "NativeObservation" && kind != "NativeReconciliation" {
		return n.Report{}, failure("native.kind", "unsupported native report", 2, nil)
	}
	f, e := nativeInputs(ctx, q)
	if e != nil {
		return n.Report{}, failure("native.input", "native inputs could not be safely read", 2, e)
	}
	r, e := nativeBuild(f, kind, q.AsOf)
	if e != nil {
		return r, failure("native.invalid", "native identities or evidence failed validation", 2, e)
	}
	if q.Out != "" {
		e = writeNative(ctx, &r, f, q.Out)
	}
	return r, e
}
func nativeBundleFiles(r *n.Report, files map[string][]byte) map[string][]byte {
	names := []string{}
	for k := range files {
		names = append(names, k)
	}
	sort.Strings(names)
	r.Assets = []api.Asset{}
	for _, k := range names {
		r.Assets = append(r.Assets, api.Asset{Path: k, Sha256: api.Sha(SHA256(files[k]))})
	}
	out := cloneBytes(files)
	out["bundle.json"] = engine.Encode(r)
	return out
}
func writeNative(ctx context.Context, r *n.Report, files map[string][]byte, out string) error {
	w, e := engine.ReserveBundle(ctx, out)
	if e != nil {
		return failure("native.output", "fresh native output directory required", 2, e)
	}
	if e = w.Write(ctx, nativeBundleFiles(r, files)); e != nil {
		return e
	}
	return w.Sync(ctx)
}
func InspectNative(ctx context.Context, root, asof string) (n.Report, error) {
	raw, e := ReadLocal(ctx, filepath.Join(root, "bundle.json"))
	if e != nil {
		return n.Report{}, e
	}
	var r n.Report
	if e = DecodeRead(raw, &r); e != nil {
		return r, e
	}
	if r.Version != n.Version || r.AuthorizationGranted || r.ExecutionReady || r.MigrationActivated || r.ProductionActivated || r.CurrentAuthority != "not-established" || r.LiveQualification != "not-established" {
		return r, errors.New("invalid native report authority")
	}
	files := map[string][]byte{}
	total := len(raw)
	for _, a := range r.Assets {
		if !strings.HasPrefix(a.Path, "assets/") || files[a.Path] != nil {
			return r, errors.New("unsafe/duplicate native asset")
		}
		p, e := ConfinedLocal(root, a.Path)
		if e != nil {
			return r, e
		}
		b, e := ReadLocal(ctx, p)
		if e != nil || SHA256(b) != string(a.Sha256) {
			return r, errors.New("native asset hash mismatch")
		}
		total += len(b)
		if total > engine.MaxTotalBytes {
			return r, errors.New("native evidence bound")
		}
		files[a.Path] = b
	}
	expected, e := nativeBuild(files, r.Kind, r.AsOf)
	if e != nil {
		return r, e
	}
	expected.Assets = r.Assets
	if !bytes.Equal(engine.Encode(expected), engine.Encode(r)) {
		return r, errors.New("native report does not reproduce from assets")
	}
	return nativeBuild(files, r.Kind, asof)
}
func nativeObserve(r *n.Report, ev n.Evidence, now time.Time) error {
	if ev.Version != n.Version || ev.Kind != "CapturedNativeEvidence" || !reflect.DeepEqual(ev.Scope, r.Scope) || len(ev.Events) > 10000 {
		return errors.New("native evidence scope/version mismatch")
	}
	observed, e := nativeTime(ev.ObservedAt)
	handleAt, _ := nativeTime(r.ObservedAt)
	if e != nil || observed.Before(handleAt) || observed.After(now) {
		return errors.New("invalid original observation date")
	}
	r.ObservedAt = ev.ObservedAt
	r.Complete = ev.Complete && len(ev.Events) > 0
	switch ev.Disposition {
	case "", "observed", "incomplete", "authentication", "permission", "timeout", "cancelled", "rate-limit", "identity-conflict", "schema", "unverified":
	default:
		return errors.New("invalid native capture disposition")
	}
	if ev.Disposition != "" && ev.Disposition != "observed" {
		r.Complete = false
		nativeDiagnostic(r, "native."+ev.Disposition, "native read did not establish complete current observations")
	}
	if !ev.Complete {
		nativeDiagnostic(r, "native.incomplete", "source coverage is incomplete; no effect or missing task is inferred")
	}
	if now.Sub(observed) >= 5*time.Minute {
		r.Status = "conflict"
		nativeDiagnostic(r, "native.stale", "native observation expired")
	}
	events := append([]n.Event(nil), ev.Events...)
	sort.Slice(events, func(i, j int) bool { return events[i].Sequence < events[j].Sequence })
	ids := map[string][]byte{}
	seqs := map[int64]string{}
	calls := map[string]n.Event{}
	results := map[string]bool{}
	for _, ev := range events {
		at, e := nativeTime(ev.ObservedAt)
		if e != nil || at.Before(handleAt) || at.After(observed) || !reflect.DeepEqual(ev.Scope, r.Scope) || ev.ID == "" || ev.Sequence < 0 {
			return errors.New("event identity/date mismatch")
		}
		raw := engine.Encode(ev)
		if prior, ok := ids[ev.ID]; ok {
			if !bytes.Equal(prior, raw) {
				return errors.New("conflicting duplicate native event")
			}
			continue
		}
		if id, ok := seqs[ev.Sequence]; ok && id != ev.ID {
			return errors.New("ambiguous native event order")
		}
		ids[ev.ID] = raw
		seqs[ev.Sequence] = ev.ID
		if r.Scope.Host == "claude-code" && (ev.Type == "tool_use" || ev.Type == "tool_result") {
			if ev.CallID == "" {
				return errors.New("native tool correlation required")
			}
			if ev.Type == "tool_use" {
				if _, ok := calls[ev.CallID]; ok {
					return errors.New("duplicate tool correlation")
				}
				calls[ev.CallID] = ev
				continue
			}
			call, ok := calls[ev.CallID]
			if !ok || results[ev.CallID] {
				nativeDiagnostic(r, "native.unmatched-result", "tool result has no unique preceding call")
				r.Complete = false
				continue
			}
			results[ev.CallID] = true
			if e := nativeClaudeResult(r, call, ev); e != nil {
				return e
			}
			continue
		}
		switch {
		case r.Scope.Host == "codex" && ev.Type == "turn/plan/updated":
			var p struct {
				ThreadID    string  `json:"threadId"`
				TurnID      string  `json:"turnId"`
				Explanation *string `json:"explanation,omitempty"`
				Plan        []struct {
					Step   string `json:"step"`
					Status string `json:"status"`
				} `json:"plan"`
			}
			if e := DecodeRead(ev.Data, &p); e != nil || p.ThreadID != r.Scope.SessionID || p.TurnID == "" {
				return errors.New("invalid Codex plan scope")
			}
			for _, s := range p.Plan {
				r.Entries = append(r.Entries, n.Entry{SourceEventID: ev.ID, Category: "plan", State: s.Status, Value: engine.Encode(s)})
			}
		case r.Scope.Host == "opencode" && ev.Type == "session.identity.snapshot":
			var identity map[string]json.RawMessage
			if e := DecodeRead(ev.Data, &identity); e != nil {
				return e
			}
			var id, directory string
			if json.Unmarshal(identity["id"], &id) != nil || json.Unmarshal(identity["directory"], &directory) != nil || id != r.Scope.SessionID || directory != r.Scope.Workspace {
				return errors.New("session identity mismatch")
			}
		case r.Scope.Host == "opencode" && ev.Type == "session.todo.snapshot":
			var todos []NativeTodo
			if e := DecodeRead(ev.Data, &todos); e != nil {
				return e
			}
			for _, todo := range todos {
				if todo.Content == "" {
					return errors.New("invalid todo content")
				}
				r.Entries = append(r.Entries, n.Entry{SourceEventID: ev.ID, Category: "todo", State: todo.Status, Value: engine.Encode(todo)})
			}
		default:
			r.Complete = false
			nativeDiagnostic(r, "native.unsupported-event", "event is preserved without upgrading its capability")
		}
	}
	for call := range calls {
		if !results[call] {
			r.Complete = false
			nativeDiagnostic(r, "native.unresolved-call", "tool call has no confirmed result")
		}
	}
	return nil
}

// NativeTodo has no portable identity. Any optional provider ID stays in Value.
type NativeTodo struct {
	Content  string `json:"content"`
	Status   string `json:"status"`
	Priority string `json:"priority"`
	ID       string `json:"id,omitempty"`
}

func nativeClaudeResult(r *n.Report, call, result n.Event) error {
	var c struct {
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	}
	if e := DecodeRead(call.Data, &c); e != nil {
		return e
	}
	var out struct {
		IsError bool            `json:"is_error"`
		Result  json.RawMessage `json:"result"`
	}
	if e := DecodeRead(result.Data, &out); e != nil {
		return e
	}
	if out.IsError {
		r.Complete = false
		nativeDiagnostic(r, "native.tool-failed", "tool failure cannot establish task state")
		return nil
	}
	var inputAliases map[string]json.RawMessage
	if e := DecodeRead(c.Input, &inputAliases); e != nil {
		return e
	}
	for _, pair := range [][2]string{{"taskId", "id"}, {"taskId", "task_id"}, {"id", "task_id"}, {"activeForm", "active_form"}} {
		if a, ok := inputAliases[pair[0]]; ok {
			if b, ok := inputAliases[pair[1]]; ok && !bytes.Equal(a, b) {
				return errors.New("conflicting tool aliases")
			}
		}
	}
	var values map[string]json.RawMessage
	if e := DecodeRead(out.Result, &values); e != nil {
		return e
	}
	switch c.Name {
	case "TaskCreate":
		var task struct {
			ID      string `json:"id"`
			Subject string `json:"subject"`
		}
		if e := DecodeRead(values["task"], &task); e != nil || task.ID == "" {
			r.Complete = false
			nativeDiagnostic(r, "native.missing-id", "successful create lacks exact returned task identity")
			return nil
		}
		r.Entries = append(r.Entries, n.Entry{SourceEventID: result.ID, Category: "task", NativeID: task.ID, State: "pending", Value: out.Result})
	case "TaskUpdate", "TaskGet", "TaskList":
		// Keep observation conservative: a proposal is never an executed patch. Raw
		// result is retained but only an explicit success/result identity is adopted.
		var input map[string]json.RawMessage
		if e := DecodeRead(c.Input, &input); e != nil {
			return e
		}
		var id string
		for _, key := range []string{"taskId", "id", "task_id"} {
			if b, ok := input[key]; ok {
				var next string
				if json.Unmarshal(b, &next) != nil || id != "" && id != next {
					return errors.New("conflicting tool identity aliases")
				}
				id = next
			}
		}
		if c.Name == "TaskUpdate" {
			var success bool
			var returned string
			if json.Unmarshal(values["success"], &success) != nil || !success || json.Unmarshal(values["taskId"], &returned) != nil || returned == "" || returned != id {
				r.Complete = false
				nativeDiagnostic(r, "native.unconfirmed-update", "update requires exact successful returned identity")
				return nil
			}
			r.Entries = append(r.Entries, n.Entry{SourceEventID: result.ID, Category: "task", NativeID: returned, State: "unknown", Value: out.Result})
		} else {
			r.Entries = append(r.Entries, n.Entry{SourceEventID: result.ID, Category: "task-read", State: "unknown", Value: out.Result})
		}
	default:
		r.Complete = false
		nativeDiagnostic(r, "native.background-or-unsupported", "tool/background completion is not task tracking or acceptance")
	}
	return nil
}

// NativeMutationUnavailable performs no bootstrap, credential lookup or I/O.
func NativeMutationUnavailable() error {
	return failure("native.mutation-unavailable", "direct native mutation API and protected admission remain unqualified", 3, nil)
}
func NativeReportExit(r n.Report) int {
	switch r.Status {
	case "conflict":
		return 4
	case "partial":
		return 6
	case "blocked":
		return 3
	}
	return 0
}
func nativeError(err error) error {
	return failure("native.invalid", "native evidence failed safe validation", 2, err)
}

func nativeObservationProfile(p api.NativeTaskProfile, h n.Handle) error {
	if p.Spec.Mode == "export-only" {
		return errors.New("export-only cannot observe")
	}
	required := ""
	switch h.Scope.Host {
	case "codex":
		required = "plans.read"
	case "claude-code":
		required = "events.observe"
	case "opencode":
		required = "todos.read"
	default:
		return errors.New("projection-only host")
	}
	found := false
	for _, op := range p.Spec.Operations {
		if op == required {
			found = true
		} else if op != "projection.export" {
			return errors.New("unsupported profile operation")
		}
	}
	if !found {
		return errors.New("explicit observation operation required")
	}
	return nil
}

func restrictNativeSchema(s map[string]any) {
	if p, ok := s["properties"].(map[string]any); ok {
		for name, raw := range p {
			m, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			switch name {
			case "host":
				m["enum"] = []string{"codex", "claude-code", "cursor", "opencode", "antigravity"}
			case "mode":
				m["enum"] = []string{"native-only", "managed"}
			case "currentAuthority", "liveQualification":
				m["const"] = "not-established"
			}
			if strings.HasSuffix(name, "Sha256") {
				m["pattern"] = "^[a-f0-9]{64}$"
			}
		}
	}
	for _, raw := range s {
		switch v := raw.(type) {
		case map[string]any:
			restrictNativeSchema(v)
		case []any:
			for _, r := range v {
				if m, ok := r.(map[string]any); ok {
					restrictNativeSchema(m)
				}
			}
		}
	}
}
