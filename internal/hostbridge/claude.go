package hostbridge

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/modelcatalog"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

var claudeUUID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-8][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)

const claudePinnedVersion = "2.1.303"

// ClaudeRun executes a single non-interactive Claude Code turn and records only
// observations from its stream. A successful host turn is never AOK acceptance.
func ClaudeRun(ctx context.Context, p Prepared, policy Policy, runner Runner, previous *Receipt) (Transcript, error) {
	var zero Transcript
	if ctx == nil || runner == nil || policy.Host != "claude-code" || policy.HostVersion != claudePinnedVersion || p.HostVersion != claudePinnedVersion || policy.AuthMode != "native-login" || policy.Executable == "" || p.Host != "claude-code" || p.Model == "" || p.Instructions == "" || !json.Valid(p.Input) || !json.Valid(p.OutputSchema) {
		return zero, errors.New("invalid Claude execution request")
	}
	if p.Configuration.Provider != nil && *p.Configuration.Provider != "anthropic" {
		return zero, errors.New("Claude provider route is unsupported")
	}
	if policy.AuthMode == "native-login" {
		for _, k := range policy.Environment {
			if isCredentialEnv(k) {
				return zero, errors.New("credential environment is not allowed for native login")
			}
		}
	}
	settings, err := claudeSettings(policy.Settings)
	if err != nil {
		return zero, err
	}
	configArgs, err := claudeConfigurationArgs(p.Configuration)
	if err != nil {
		return zero, err
	}
	args := []string{"-p", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose", "--model", p.Model}
	args = append(args, configArgs...)
	if previous != nil {
		if previous.SessionID == "" || !claudeUUID.MatchString(previous.SessionID) {
			return zero, errors.New("invalid previous Claude session")
		}
		args = append(args, "--resume", previous.SessionID)
	} else {
		id, e := newClaudeUUID()
		if e != nil {
			return zero, errors.New("could not create Claude session id")
		}
		args = append(args, "--session-id", id)
	}
	args = append(args, "--setting-sources", "", "--strict-mcp-config", "--mcp-config", `{"mcpServers":{}}`)
	tools := append([]string(nil), policy.Tools...)
	if len(p.NativeTasks) > 0 {
		if !policy.AllowNativeTasks {
			return zero, errors.New("Claude native tasks are not enabled by policy")
		}
		tools = append(tools, "TaskCreate", "TaskGet", "TaskList", "TaskUpdate")
	}
	tools = uniqueSorted(tools)
	args = append(args, "--tools", strings.Join(tools, ","), "--disallowedTools", "mcp__*", "--permission-mode", "default", "--settings", string(settings), "--json-schema", string(p.OutputSchema), "--system-prompt", claudeInstructions(p))

	env := make([]string, 0, len(policy.Environment))
	for _, k := range uniqueSorted(policy.Environment) {
		if !validEnvName(k) || policy.AuthMode == "native-login" && isCredentialEnv(k) {
			return zero, errors.New("invalid Claude environment policy")
		}
		if v, ok := os.LookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}
	proc, err := runner.Start(ctx, Launch{Executable: policy.Executable, Args: args, Dir: p.Workspace, Env: env, TimeoutSeconds: policy.TimeoutSeconds, MaxFrameBytes: MaxFrameBytes, MaxTotalBytes: MaxCaptureBytes})
	if err != nil {
		return zero, errors.New("Claude process could not start")
	}
	defer proc.Close()
	user := map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": claudeInput(p.Input)}}
	body, _ := json.Marshal(user)
	if err = proc.Send(body); err != nil {
		return zero, errors.New("Claude input could not be sent")
	}
	var frames [][]byte
	for {
		b, e := proc.Receive(ctx)
		if e != nil {
			if errors.Is(e, context.Canceled) || errors.Is(e, context.DeadlineExceeded) || ctx.Err() != nil {
				_ = proc.Stop()
				_ = proc.Wait()
				partial, parseErr := ParseClaudeFrames(p, previous, frames)
				if parseErr == nil && partial.SessionID != "" && partial.HostOutcome == "unknown" {
					partial.HostOutcome = "interrupted"
				}
				return partial, errors.New("Claude execution canceled")
			}
			if !errors.Is(e, io.EOF) {
				return zero, errors.New("Claude output could not be read")
			}
			break
		}
		frames = append(frames, b)
	}
	if e := proc.Wait(); e != nil {
		return zero, errors.New("Claude process failed")
	}
	t, err := ParseClaudeFrames(p, previous, frames)
	if err != nil {
		return zero, err
	}
	return t, nil
}

// ParseClaudeFrames validates newline-delimited Claude stream-json frames.
func ParseClaudeFrames(p Prepared, previous *Receipt, frames [][]byte) (Transcript, error) {
	t := Transcript{HostOutcome: "unknown"}
	seen := map[string]string{}
	var terminal, initialized bool
	for _, frame := range frames {
		if len(frame) == 0 || len(frame) > MaxFrameBytes || !json.Valid(frame) {
			return Transcript{}, errors.New("invalid Claude stream frame")
		}
		var m map[string]json.RawMessage
		if usage.Decode(frame, &m) != nil {
			return Transcript{}, errors.New("invalid Claude stream frame")
		}
		var typ string
		_ = json.Unmarshal(m["type"], &typ)
		raw, _ := json.Marshal(m)
		id := claudeString(m, "uuid")
		if id != "" {
			if !claudeUUID.MatchString(id) {
				return Transcript{}, errors.New("invalid Claude event identifier")
			}
			if old, ok := seen[id]; ok {
				if old == string(raw) {
					continue
				}
				return Transcript{}, errors.New("conflicting duplicate Claude event")
			}
			seen[id] = string(raw)
		}
		switch typ {
		case "system":
			if claudeString(m, "subtype") == "init" {
				if initialized {
					return Transcript{}, errors.New("Claude stream has multiple session initializations")
				}
				if claudeAPIKeySource(m) {
					return Transcript{}, errors.New("Claude initialization reports API-key authentication")
				}
				sid := claudeString(m, "session_id")
				model := claudeString(m, "model")
				if sid == "" || !claudeUUID.MatchString(sid) || model == "" || model != p.Model {
					return Transcript{}, errors.New("Claude session or model mismatch")
				}
				if previous != nil && sid != previous.SessionID {
					return Transcript{}, errors.New("Claude resume session mismatch")
				}
				t.SessionID, t.Model = sid, model
				initialized = true
				var tools []string
				_ = json.Unmarshal(m["tools"], &tools)
				t.ObservedTools = uniqueSorted(tools)
			}
		case "assistant", "user", "tool_use", "tool_result", "content_block_start", "content_block_delta", "content_block_stop":
			if typ == "assistant" || typ == "user" {
				if err := claudeObserveTaskMessage(m, &t, p.NativeTasks); err != nil {
					return Transcript{}, err
				}
			}
		case "result":
			if terminal {
				return Transcript{}, errors.New("multiple Claude terminal events")
			}
			terminal = true
			sid := claudeString(m, "session_id")
			if sid == "" || !claudeUUID.MatchString(sid) || sid != t.SessionID {
				return Transcript{}, errors.New("Claude result session mismatch")
			}
			sub := claudeString(m, "subtype")
			if strings.Contains(strings.ToLower(sub), "error") || claudeBool(m, "is_error") {
				t.HostOutcome = "error"
				break
			}
			if sub != "success" {
				return Transcript{}, errors.New("Claude result subtype is not recognized")
			}
			out := m["structured_output"]
			if len(out) == 0 || !json.Valid(out) || !jsonSchemaValid(out, p.OutputSchema) {
				return Transcript{}, errors.New("Claude structured output is missing or invalid")
			}
			t.Output = append(json.RawMessage(nil), out...)
			t.HostOutcome = "completed"
			if usage := m["usage"]; len(usage) > 0 && json.Valid(usage) {
				if safe := claudeUsage(usage); len(safe) > 0 {
					t.UsageFrames = append(t.UsageFrames, safe)
				}
			}
		default:
			// Unknown frame types are retained as bounded observations, never authority.
		}
		if id != "" {
			data := claudeSafeEvent(m, typ, id, t.SessionID, t.Model, t.ObservedTools)
			t.Events = append(t.Events, Event{Sequence: len(t.Events) + 1, NativeID: id, Type: typ, SessionID: t.SessionID, Data: data})
		}
	}
	if t.SessionID == "" {
		return Transcript{}, errors.New("Claude stream lacks an initialized session")
	}
	if terminal && t.HostOutcome == "error" {
		return t, nil
	}
	if !terminal {
		t.HostOutcome = "unknown"
	}
	return t, nil
}

func claudeAPIKeySource(m map[string]json.RawMessage) bool {
	var walk func(map[string]json.RawMessage) bool
	walk = func(obj map[string]json.RawMessage) bool {
		for k, v := range obj {
			n := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(k, "_", ""), "-", ""))
			if n == "apikeysource" || n == "authmethod" || n == "authenticationmethod" {
				var s string
				if json.Unmarshal(v, &s) == nil {
					s = strings.ToLower(s)
					for _, needle := range []string{"api_key", "apikey", "api-key", "api key", "api_key_helper", "api-key-helper"} {
						if strings.Contains(s, needle) {
							return true
						}
					}
				}
			}
			var nested map[string]json.RawMessage
			if json.Unmarshal(v, &nested) == nil && walk(nested) {
				return true
			}
		}
		return false
	}
	return walk(m)
}

func claudeInstructions(p Prepared) string {
	s := p.Instructions
	if len(p.NativeTasks) > 0 {
		var tasks strings.Builder
		tasks.WriteString("\n\nAOK task observation request: use Claude native task tools to represent these steps. This is observation only; never claim AOK state changed. Return resulting native task IDs in structured output when possible.\n")
		for _, x := range p.NativeTasks {
			fmt.Fprintf(&tasks, "- %s: %s", x.ID, x.Subject)
			if x.Description != "" {
				fmt.Fprintf(&tasks, " — %s", x.Description)
			}
			if len(x.Dependencies) > 0 {
				fmt.Fprintf(&tasks, " (depends on: %s)", strings.Join(x.Dependencies, ", "))
			}
			tasks.WriteByte('\n')
		}
		s += tasks.String()
	}
	return s
}
func claudeInput(b json.RawMessage) string {
	var s string
	if json.Unmarshal(b, &s) == nil {
		return s
	}
	return string(b)
}
func claudeString(m map[string]json.RawMessage, k string) string {
	var s string
	_ = json.Unmarshal(m[k], &s)
	return s
}
func claudeBool(m map[string]json.RawMessage, k string) bool {
	var b bool
	_ = json.Unmarshal(m[k], &b)
	return b
}
func uniqueSorted(in []string) []string {
	set := map[string]bool{}
	for _, s := range in {
		if s != "" {
			set[s] = true
		}
	}
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
func validEnvName(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if !(r == '_' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || i > 0 && r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}
func isCredentialEnv(s string) bool {
	u := strings.ToUpper(s)
	return strings.Contains(u, "KEY") || strings.Contains(u, "TOKEN") || strings.Contains(u, "SECRET") || strings.Contains(u, "PASSWORD") || strings.Contains(u, "CREDENTIAL")
}
func newClaudeUUID() (string, error) {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		return "", e
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b)
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:], nil
}
func claudeSettings(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "null" {
		return json.RawMessage(`{}`), nil
	}
	var m map[string]json.RawMessage
	if usage.Decode(raw, &m) != nil {
		return nil, errors.New("invalid Claude settings")
	}
	for k := range m {
		if k != "disableAllHooks" && k != "sandbox" {
			return nil, errors.New("unsupported Claude settings override")
		}
	}
	if v, ok := m["disableAllHooks"]; ok && string(v) != "true" {
		return nil, errors.New("Claude hooks must be disabled")
	}
	if v, ok := m["sandbox"]; ok {
		var sandbox map[string]json.RawMessage
		if json.Unmarshal(v, &sandbox) != nil || len(sandbox) != 2 || string(sandbox["enabled"]) != "true" || string(sandbox["allowUnsandboxedCommands"]) != "false" {
			return nil, errors.New("Claude sandbox settings do not prove the required restriction")
		}
	}
	b, e := json.Marshal(m)
	if e != nil {
		return nil, errors.New("invalid Claude settings")
	}
	return b, nil
}

// Keep observations of native task identifiers tied to actual tool calls and results.
func claudeObserveTaskMessage(m map[string]json.RawMessage, t *Transcript, steps []TaskStep) error {
	var msg map[string]json.RawMessage
	if json.Unmarshal(m["message"], &msg) != nil {
		return nil
	}
	var blocks []map[string]json.RawMessage
	if json.Unmarshal(msg["content"], &blocks) != nil {
		return nil
	}
	for _, block := range blocks {
		kind, name := claudeString(block, "type"), claudeString(block, "name")
		if kind == "tool_use" && isClaudeTaskTool(name) {
			if !stringIn(t.ObservedTools, name) {
				return errors.New("Claude native task tool was not available in the observed tool list")
			}
			input := safeTaskInput(name, block["input"])
			stepID := taskStepForCall(name, input, steps, t.Events)
			callID := claudeString(block, "id")
			if len(input) > 0 && callID != "" {
				data := map[string]json.RawMessage{"input": input}
				if stepID != "" {
					b, _ := json.Marshal(stepID)
					data["stepId"] = b
				}
				safe, _ := json.Marshal(data)
				t.Events = append(t.Events, Event{Sequence: len(t.Events) + 1, NativeID: callID, Type: "native_task_call:" + name, SessionID: t.SessionID, Data: safe})
			}
		}
		if kind == "tool_result" {
			id := claudeString(block, "tool_use_id")
			content := block["content"]
			var s string
			if json.Unmarshal(content, &s) == nil {
				content = []byte(s)
			}
			if len(content) > 0 && json.Valid(content) && id != "" && !claudeBool(block, "is_error") {
				// Bind only to an observed task tool call; output text is not an ID.
				for _, e := range t.Events {
					if e.NativeID == id && strings.HasPrefix(e.Type, "native_task_call:") {
						ids := taskResultIDs(content)
						if len(ids) > 0 {
							data := map[string]any{"taskIds": ids}
							if bindings := taskResultBindings(t.Events, id, ids, steps); len(bindings) > 0 {
								data["taskBindings"] = bindings
							}
							b, _ := json.Marshal(data)
							t.Events = append(t.Events, Event{Sequence: len(t.Events) + 1, NativeID: id, Type: "native_task_result", SessionID: t.SessionID, Data: b})
						}
						break
					}
				}
			}
		}
	}
	return nil
}

func claudeConfigurationArgs(c modelcatalog.Configuration) ([]string, error) {
	if c.ReasoningBudgetTokens != nil || c.ServiceTier != nil || c.Deployment != nil || c.ModelRevision != nil || c.Generation != nil {
		return nil, errors.New("Claude configuration contains an unsupported selector or generation override")
	}
	if c.ReasoningMode != nil && *c.ReasoningMode == "provider-default" && c.Effort == nil {
		return nil, nil
	}
	if c.ReasoningMode == nil || *c.ReasoningMode != "effort" || c.Effort == nil || *c.Effort == "" {
		return nil, errors.New("Claude reasoning configuration is unsupported")
	}
	return []string{"--effort", *c.Effort}, nil
}

func taskStepForCall(tool string, input json.RawMessage, steps []TaskStep, events []Event) string {
	var m map[string]json.RawMessage
	_ = json.Unmarshal(input, &m)
	if tool == "TaskCreate" {
		subject := claudeString(m, "subject")
		found := ""
		for _, s := range steps {
			if subject == s.Subject {
				if found != "" {
					return ""
				}
				found = s.ID
			}
		}
		return found
	}
	if tool == "TaskGet" || tool == "TaskUpdate" {
		id := claudeString(m, "taskId")
		return knownTaskStep(id, steps, events)
	}
	return ""
}

type taskBinding struct {
	TaskID string `json:"taskId"`
	StepID string `json:"stepId"`
}

func taskResultBindings(events []Event, callID string, ids []string, steps []TaskStep) []taskBinding {
	var call *Event
	for i := range events {
		if strings.HasPrefix(events[i].Type, "native_task_call:") && events[i].NativeID == callID {
			call = &events[i]
			break
		}
	}
	if call == nil {
		return nil
	}
	var data struct {
		Input  json.RawMessage `json:"input"`
		StepID string          `json:"stepId"`
	}
	_ = json.Unmarshal(call.Data, &data)
	tool := strings.TrimPrefix(call.Type, "native_task_call:")
	var input map[string]json.RawMessage
	_ = json.Unmarshal(data.Input, &input)
	out := []taskBinding{}
	for _, nativeID := range ids {
		stepID := ""
		switch tool {
		case "TaskCreate":
			if data.StepID != "" && len(ids) == 1 {
				stepID = data.StepID
			}
		case "TaskGet", "TaskUpdate":
			if claudeString(input, "taskId") == nativeID {
				stepID = data.StepID
			}
		case "TaskList":
			stepID = knownTaskStep(nativeID, steps, events)
		}
		if stepID != "" {
			out = append(out, taskBinding{TaskID: nativeID, StepID: stepID})
		}
	}
	return out
}
func knownTaskStep(nativeID string, steps []TaskStep, events []Event) string {
	known := ""
	ambiguous := false
	for _, s := range steps {
		if nativeID != "" && s.NativeID == nativeID {
			if known != "" && known != s.ID {
				ambiguous = true
			}
			known = s.ID
		}
	}
	for _, e := range events {
		if e.Type == "native_task_result" {
			var prev struct {
				Bindings []taskBinding `json:"taskBindings"`
			}
			_ = json.Unmarshal(e.Data, &prev)
			for _, b := range prev.Bindings {
				if b.TaskID == nativeID {
					if known != "" && known != b.StepID {
						ambiguous = true
					}
					known = b.StepID
				}
			}
		}
	}
	if ambiguous {
		return ""
	}
	return known
}

func claudeSafeEvent(m map[string]json.RawMessage, typ, id, sid, model string, tools []string) json.RawMessage {
	safe := map[string]any{"type": typ, "uuid": id}
	if typ == "system" && claudeString(m, "subtype") == "init" {
		safe["subtype"] = "init"
		safe["session_id"] = sid
		safe["model"] = model
		safe["tools"] = tools
	}
	if typ == "result" {
		safe["subtype"] = claudeString(m, "subtype")
		safe["session_id"] = sid
		safe["is_error"] = claudeBool(m, "is_error")
		if u := claudeUsage(m["usage"]); len(u) > 0 {
			var x any
			_ = json.Unmarshal(u, &x)
			safe["usage"] = x
		}
	}
	b, _ := json.Marshal(safe)
	return b
}
func stringIn(hay []string, needle string) bool {
	for _, s := range hay {
		if s == needle {
			return true
		}
	}
	return false
}
func safeTaskInput(tool string, raw json.RawMessage) json.RawMessage {
	var in map[string]json.RawMessage
	if json.Unmarshal(raw, &in) != nil {
		return nil
	}
	out := map[string]json.RawMessage{}
	keys := []string{}
	switch tool {
	case "TaskCreate":
		keys = []string{"subject"}
	case "TaskGet":
		keys = []string{"taskId"}
	case "TaskList":
		keys = []string{}
	case "TaskUpdate":
		keys = []string{"taskId", "status"}
	}
	for _, k := range keys {
		if v := in[k]; len(v) > 0 && json.Valid(v) {
			out[k] = v
		}
	}
	b, _ := json.Marshal(out)
	return b
}
func taskResultIDs(raw json.RawMessage) []string {
	var x any
	if usage.Decode(raw, &x) != nil {
		return nil
	}
	set := map[string]bool{}
	var addObject func(any)
	addObject = func(v any) {
		m, ok := v.(map[string]any)
		if !ok {
			return
		}
		add := func(id any) {
			if isJSONScalarString(id) {
				s := fmt.Sprint(id)
				if s != "" {
					set[s] = true
				}
			}
		}
		add(m["taskId"])
		if m["taskId"] == nil {
			add(m["id"])
		}
		if task, ok := m["task"]; ok {
			addObject(task)
		}
		if tasks, ok := m["tasks"].([]any); ok {
			for _, task := range tasks {
				addObject(task)
			}
		}
	}
	if list, ok := x.([]any); ok {
		for _, v := range list {
			addObject(v)
		}
	} else {
		addObject(x)
	}
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
func isJSONScalarString(v any) bool {
	switch v.(type) {
	case string, json.Number:
		return true
	}
	return false
}
func isClaudeTaskTool(s string) bool {
	return s == "TaskCreate" || s == "TaskGet" || s == "TaskList" || s == "TaskUpdate"
}
func claudeUsage(raw json.RawMessage) json.RawMessage {
	var src map[string]json.RawMessage
	if json.Unmarshal(raw, &src) != nil {
		return nil
	}
	dst := map[string]json.RawMessage{}
	for _, k := range []string{"input_tokens", "output_tokens", "cache_read_input_tokens", "cache_creation_input_tokens"} {
		v := src[k]
		if len(v) == 0 {
			continue
		}
		var n json.Number
		d := json.NewDecoder(strings.NewReader(string(v)))
		d.UseNumber()
		if d.Decode(&n) == nil {
			if validUsageInteger(n) {
				dst[k] = append(json.RawMessage(nil), v...)
			}
		}
	}
	if v := src["total_cost_usd"]; len(v) > 0 {
		var n json.Number
		d := json.NewDecoder(strings.NewReader(string(v)))
		d.UseNumber()
		if d.Decode(&n) == nil && validUsageDecimal(n) {
			dst["total_cost_usd"] = append(json.RawMessage(nil), v...)
		}
	}
	b, _ := json.Marshal(dst)
	if len(dst) == 0 {
		return nil
	}
	return b
}
func jsonSchemaValid(value, schema json.RawMessage) bool {
	if !json.Valid(value) || !json.Valid(schema) {
		return false
	}
	var sd, instance any
	if usage.Decode(schema, &sd) != nil || usage.Decode(value, &instance) != nil {
		return false
	}
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft2020)
	c.UseLoader(claudeSchemaLoader{})
	if c.AddResource("https://aok.invalid/claude/output.json", sd) != nil {
		return false
	}
	compiled, e := c.Compile("https://aok.invalid/claude/output.json")
	return e == nil && compiled.Validate(instance) == nil
}

type claudeSchemaLoader struct{}

func (claudeSchemaLoader) Load(url string) (any, error) {
	return nil, fmt.Errorf("external schema reference is forbidden: %s", url)
}
