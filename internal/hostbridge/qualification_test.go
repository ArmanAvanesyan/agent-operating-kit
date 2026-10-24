package hostbridge

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/modelcatalog"
)

const fakeClaudeSession = "00000000-0000-4000-8000-000000000001"

type qualificationFakeRunner struct {
	workspace string
	model     string
	starts    int
}

type codexQualificationRunner struct{ model string }

func (r *codexQualificationRunner) Start(_ context.Context, l Launch) (Process, error) {
	if len(l.Args) == 1 && l.Args[0] == "--version" {
		return &codexQualificationProcess{frames: [][]byte{[]byte("Codex " + CodexPinnedVersion + "\n")}}, nil
	}
	return &codexQualificationProcess{model: r.model}, nil
}

type codexQualificationProcess struct {
	model  string
	frames [][]byte
	at     int
	wait   bool
}

func (p *codexQualificationProcess) Send(raw []byte) error {
	var req struct {
		ID     int             `json:"id"`
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	if json.Unmarshal(raw, &req) != nil {
		return nil
	}
	var result any
	switch req.ID {
	case 1:
		result = map[string]any{"protocolVersion": "1"}
	case 2:
		result = map[string]any{"account": map[string]any{"type": "chatgpt"}}
	case 3:
		result = map[string]any{"model": p.model, "thread": map[string]any{"id": "codex-thread-1", "sessionId": "codex-session-1"}}
	case 4:
		var params struct {
			Input []struct {
				Text string `json:"text"`
			} `json:"input"`
		}
		_ = json.Unmarshal(req.Params, &params)
		input := ""
		if len(params.Input) > 0 {
			input = params.Input[0].Text
		}
		if strings.Contains(input, "Wait for more input") {
			p.wait = true
			return nil
		}
		p.frames = append(p.frames, codexFrame(map[string]any{"id": 4, "result": map[string]any{"turn": map[string]any{"id": "codex-turn-1"}}}))
		p.frames = append(p.frames, codexFrame(map[string]any{"method": "turn/started", "params": map[string]any{"threadId": "codex-thread-1", "turnId": "codex-turn-1"}}))
		if path := codexPath(input, ".aok-qualification-"); path != "" {
			content, _ := os.ReadFile(path)
			p.command("read-item", "cat "+path, 0, string(content), "completed")
		}
		if path := codexPath(input, ".aok-qualification-canary-"); path != "" {
			p.command("write-item", "echo changed > "+path, 1, "outside workspace denied", "failed")
		}
		out := `{"ok":true}`
		if strings.Contains(input, "Read this file") {
			if path := codexPath(input, ".aok-qualification-"); path != "" {
				b, _ := os.ReadFile(path)
				encoded, _ := json.Marshal(map[string]any{"ok": true, "nonce": string(b)})
				out = string(encoded)
			}
		}
		p.frames = append(p.frames, codexFrame(map[string]any{"method": "item/completed", "params": map[string]any{"threadId": "codex-thread-1", "turnId": "codex-turn-1", "item": map[string]any{"id": "agent-item", "type": "agentMessage", "text": out}}}))
		p.frames = append(p.frames, codexFrame(map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": "codex-thread-1", "turnId": "codex-turn-1", "turn": map[string]any{"id": "codex-turn-1", "status": "completed"}}}))
		return nil
	}
	p.frames = append(p.frames, codexFrame(map[string]any{"id": req.ID, "result": result}))
	return nil
}
func (p *codexQualificationProcess) command(id, command string, code int, output, status string) {
	p.frames = append(p.frames, codexFrame(map[string]any{"method": "item/started", "params": map[string]any{"threadId": "codex-thread-1", "turnId": "codex-turn-1", "item": map[string]any{"id": id, "type": "commandExecution", "command": command}}}))
	p.frames = append(p.frames, codexFrame(map[string]any{"method": "item/completed", "params": map[string]any{"threadId": "codex-thread-1", "turnId": "codex-turn-1", "item": map[string]any{"id": id, "type": "commandExecution", "command": command, "exitCode": code, "aggregatedOutput": output, "status": status}}}))
}
func codexFrame(v any) []byte { b, _ := json.Marshal(v); return b }
func codexPath(s, prefix string) string {
	for _, w := range strings.Fields(s) {
		w = strings.Trim(w, "\"'`.,:)")
		if strings.Contains(w, prefix) {
			return w
		}
	}
	return ""
}
func (p *codexQualificationProcess) Receive(ctx context.Context) ([]byte, error) {
	if p.wait {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if p.at >= len(p.frames) {
		return nil, io.EOF
	}
	b := p.frames[p.at]
	p.at++
	return b, nil
}
func (p *codexQualificationProcess) Stop() error  { return nil }
func (p *codexQualificationProcess) Close() error { return nil }
func (p *codexQualificationProcess) Wait() error  { return nil }

func (f *qualificationFakeRunner) Start(_ context.Context, l Launch) (Process, error) {
	f.starts++
	if len(l.Args) == 1 && l.Args[0] == "--version" {
		return &qualificationFakeProcess{frames: [][]byte{[]byte("Claude Code v2.1.303\n")}}, nil
	}
	if len(l.Args) == 3 && l.Args[0] == "auth" && l.Args[1] == "status" && l.Args[2] == "--json" {
		return &qualificationFakeProcess{frames: [][]byte{[]byte(`{"loggedIn":true,"authMethod":"oauth_token"}`)}}, nil
	}
	return &qualificationFakeProcess{launch: l, workspace: f.workspace, model: f.model}, nil
}

type qualificationFakeProcess struct {
	launch           Launch
	workspace, model string
	frames           [][]byte
	sent             []byte
	at               int
	stopped          bool
	wait             bool
}

func (p *qualificationFakeProcess) Send(b []byte) error {
	p.sent = append([]byte(nil), b...)
	p.prepare()
	return nil
}
func (p *qualificationFakeProcess) prepare() {
	var envelope map[string]json.RawMessage
	_ = json.Unmarshal(p.sent, &envelope)
	var msg map[string]json.RawMessage
	_ = json.Unmarshal(envelope["message"], &msg)
	var text string
	_ = json.Unmarshal(msg["content"], &text)
	if strings.Contains(text, "Wait for more input") {
		p.wait = true
		return
	}
	toolArg := ""
	for i, a := range p.launch.Args {
		if a == "--tools" && i+1 < len(p.launch.Args) {
			toolArg = p.launch.Args[i+1]
		}
	}
	tools := []string{}
	if toolArg != "" {
		tools = strings.Split(toolArg, ",")
	}
	frames := []any{map[string]any{"type": "system", "subtype": "init", "uuid": "00000000-0000-4000-8000-000000000010", "session_id": fakeClaudeSession, "model": p.model, "tools": tools}}
	var content []any
	if strings.Contains(toolArg, "Read") {
		entries, _ := os.ReadDir(p.workspace)
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".aok-qualification-") && strings.HasSuffix(e.Name(), ".txt") {
				path := filepath.Join(p.workspace, e.Name())
				data, _ := os.ReadFile(path)
				content = append(content, map[string]any{"type": "tool_use", "id": "read-call-1", "name": "Read", "input": map[string]any{"file_path": path}})
				frames = append(frames, map[string]any{"type": "assistant", "uuid": "00000000-0000-4000-8000-000000000011", "message": map[string]any{"content": content}})
				frames = append(frames, map[string]any{"type": "user", "uuid": "00000000-0000-4000-8000-000000000012", "message": map[string]any{"content": []any{map[string]any{"type": "tool_result", "tool_use_id": "read-call-1", "content": string(data)}}}})
				content = nil
				break
			}
		}
	}
	if strings.Contains(toolArg, "Write") {
		path := ""
		for _, word := range strings.Fields(text) {
			if strings.Contains(word, ".aok-qualification-canary-") {
				path = strings.Trim(word, "\"'`.,")
			}
		}
		content = append(content, map[string]any{"type": "tool_use", "id": "write-call-1", "name": "Write", "input": map[string]any{"file_path": path, "content": "changed"}})
		frames = append(frames, map[string]any{"type": "assistant", "uuid": "00000000-0000-4000-8000-000000000013", "message": map[string]any{"content": content}})
		frames = append(frames, map[string]any{"type": "user", "uuid": "00000000-0000-4000-8000-000000000014", "message": map[string]any{"content": []any{map[string]any{"type": "tool_result", "tool_use_id": "write-call-1", "is_error": true, "content": "outside workspace denied"}}}})
		content = nil
	}
	if strings.Contains(toolArg, "TaskCreate") {
		calls := []struct {
			name, id string
			input    map[string]any
			result   any
		}{{"TaskCreate", "task-call-1", map[string]any{"subject": "AOK qualification task", "description": "Synthetic temporary native task"}, map[string]any{"id": "native-task-1"}}, {"TaskGet", "task-call-2", map[string]any{"taskId": "native-task-1"}, map[string]any{"id": "native-task-1"}}, {"TaskList", "task-call-3", map[string]any{}, map[string]any{"tasks": []any{map[string]any{"id": "native-task-1"}}}}, {"TaskUpdate", "task-call-4", map[string]any{"taskId": "native-task-1", "status": "completed"}, map[string]any{"id": "native-task-1"}}}
		for i, c := range calls {
			frames = append(frames, map[string]any{"type": "assistant", "uuid": fmt.Sprintf("00000000-0000-4000-8000-%012d", 20+i*2), "message": map[string]any{"content": []any{map[string]any{"type": "tool_use", "id": c.id, "name": c.name, "input": c.input}}}})
			res, _ := json.Marshal(c.result)
			frames = append(frames, map[string]any{"type": "user", "uuid": fmt.Sprintf("00000000-0000-4000-8000-%012d", 21+i*2), "message": map[string]any{"content": []any{map[string]any{"type": "tool_result", "tool_use_id": c.id, "content": string(res)}}}})
		}
	}
	out := map[string]any{"ok": true}
	if strings.Contains(text, "Read this file") {
		for _, e := range mustReadDir(p.workspace) {
			if strings.HasPrefix(e, ".aok-qualification-") && strings.HasSuffix(e, ".txt") {
				v, _ := os.ReadFile(filepath.Join(p.workspace, e))
				out["nonce"] = string(v)
				break
			}
		}
	}
	frames = append(frames, map[string]any{"type": "result", "uuid": "00000000-0000-4000-8000-000000000030", "subtype": "success", "session_id": fakeClaudeSession, "structured_output": out})
	for _, frame := range frames {
		b, _ := json.Marshal(frame)
		p.frames = append(p.frames, b)
	}
}
func mustReadDir(path string) []string {
	es, _ := os.ReadDir(path)
	out := make([]string, 0, len(es))
	for _, e := range es {
		out = append(out, e.Name())
	}
	return out
}
func (p *qualificationFakeProcess) Receive(ctx context.Context) ([]byte, error) {
	if p.wait {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if p.at >= len(p.frames) {
		return nil, io.EOF
	}
	b := p.frames[p.at]
	p.at++
	return b, nil
}
func (p *qualificationFakeProcess) Stop() error  { p.stopped = true; return nil }
func (p *qualificationFakeProcess) Close() error { return nil }
func (p *qualificationFakeProcess) Wait() error  { return nil }

func signedQualificationPolicy(t *testing.T) (Policy, *qualificationFakeRunner) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(root, "workspace")
	if err := os.Mkdir(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	eh, err := executableHash(exe)
	if err != nil {
		t.Fatal(err)
	}
	model := "claude-opus-4-7"
	reasonMode, effort := "effort", "low"
	p := Policy{Version: Version, Host: "claude-code", Executable: exe, ExecutableSHA256: eh, HostVersion: claudePinnedVersion, AuthMode: "native-login", AccountScope: "synthetic-test-account", WorkspaceRoots: []string{root}, Workspace: workspace, Models: []string{model}, Tools: []string{"Read", "Write"}, Environment: []string{}, TimeoutSeconds: 30, AllowExecution: true, AllowNativeTasks: true, Settings: json.RawMessage(`{}`), CodexConfig: map[string]json.RawMessage{}, Configuration: modelcatalog.Configuration{ModelID: &model, ReasoningMode: &reasonMode, Effort: &effort}}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	p.ControlIssuers = map[string]string{"test-operator": hex.EncodeToString(pub)}
	p.root = root
	at := time.Now().UTC().Truncate(time.Second)
	for _, name := range []string{"filesystem", "network", "ambient-configuration"} {
		proof := ControlProof{Version: ControlProofVersion, Owner: "test-operator", Control: name, Host: p.Host, HostVersion: p.HostVersion, ExecutableSHA256: p.ExecutableSHA256, Workspace: p.Workspace, ScopeSHA256: ControlScopeDigest(p), ObservedAt: at.Format(time.RFC3339Nano), ExpiresAt: at.Add(4 * time.Minute).Format(time.RFC3339Nano), Status: "enforced", Checks: []string{"fixture-check"}}
		payload, _ := json.Marshal(proof)
		proof.Signature = hex.EncodeToString(ed25519.Sign(priv, payload))
		data, _ := json.Marshal(proof)
		file := name + ".json"
		if err = os.WriteFile(filepath.Join(root, file), data, 0600); err != nil {
			t.Fatal(err)
		}
		p.Controls = append(p.Controls, Control{Name: name, Owner: proof.Owner, Evidence: Asset{Path: file, SHA256: Hash(data)}, ObservedAt: proof.ObservedAt, ExpiresAt: proof.ExpiresAt, Workspace: p.Workspace, HostVersion: p.HostVersion})
	}
	return p, &qualificationFakeRunner{workspace: workspace, model: model}
}

func TestQualifyCompletesBoundedSyntheticMatrix(t *testing.T) {
	p, runner := signedQualificationPolicy(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	b, err := Qualify(ctx, p, runner)
	if err != nil {
		t.Fatal(err)
	}
	profileDigest, profileErr := QualificationProfileDigest(p)
	if profileErr != nil || profileDigest != Hash(b.Files["synthetic-profile.json"]) {
		t.Fatalf("qualification demand profile differs from invoked profile: %s %v", profileDigest, profileErr)
	}
	if b.Receipt == nil || b.Receipt.Report.Status != "valid" || b.Receipt.Report.Accepted || b.Receipt.Report.AuthorizationGranted || b.Receipt.Report.ExecutionReady || b.Receipt.Report.LiveGate != "fixture-qualified" {
		t.Fatalf("qualification report overstated outcome: %+v; cases=%s", b.Receipt, b.Files["qualification.json"])
	}
	var report struct {
		Cases  []QualificationCase `json:"cases"`
		Report Report              `json:"report"`
	}
	if err = json.Unmarshal(b.Files["qualification.json"], &report); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"invocation", "resume", "permitted-workspace-read", "active-cancellation", "forbidden-outside-write", "native-tasks"} {
		found := false
		for _, c := range report.Cases {
			if c.Name == want && c.Status == "passed" {
				found = true
			}
		}
		if !found {
			t.Fatalf("required case %s did not pass: %+v", want, report.Cases)
		}
	}
}

func TestQualifyDoesNotAcceptUnverifiedEnforcement(t *testing.T) {
	p, runner := signedQualificationPolicy(t)
	p.ControlIssuers = nil
	b, err := Qualify(context.Background(), p, runner)
	if err != nil {
		t.Fatal(err)
	}
	if b.Receipt == nil || b.Receipt.Report.Status == "valid" || b.Receipt.Report.Accepted || runner.starts != 0 {
		t.Fatalf("unverified controls reported qualified: %+v", b.Receipt)
	}
}

func TestQualifyCodexSyntheticMatrixAndInvocationProbe(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(root, "workspace")
	if err = os.Mkdir(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	eh, err := executableHash(exe)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "codex.toml")
	if err = os.WriteFile(configPath, []byte(""), 0600); err != nil {
		t.Fatal(err)
	}
	census, err := CodexCensus(configPath)
	if err != nil {
		t.Fatal(err)
	}
	censusRaw, _ := json.Marshal(census)
	if err = os.WriteFile(filepath.Join(root, "census.json"), censusRaw, 0600); err != nil {
		t.Fatal(err)
	}
	model := "gpt-6.1-sol"
	host, version, auth := "codex", CodexPinnedVersion, "native-login"
	p := Policy{Version: Version, Host: host, Executable: exe, ExecutableSHA256: eh, HostVersion: version, AuthMode: auth, AccountScope: "codex-synthetic-test-account", WorkspaceRoots: []string{root}, Workspace: workspace, Models: []string{model}, Tools: []string{}, Environment: []string{}, ConfigurationAssets: []Asset{{Path: "census.json", SHA256: Hash(censusRaw)}}, Controls: []Control{}, ControlIssuers: map[string]string{}, TimeoutSeconds: 30, AllowExecution: true, AllowNativeTasks: true, Settings: json.RawMessage(`{}`), CodexConfig: map[string]json.RawMessage{}, Configuration: modelcatalog.Configuration{Host: &host, HostVersion: &version, ModelID: &model, AuthMode: &auth}}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	p.ControlIssuers = map[string]string{"test-operator": hex.EncodeToString(pub)}
	p.root = root
	at := time.Now().UTC().Truncate(time.Second)
	for _, name := range []string{"filesystem", "network", "ambient-configuration"} {
		proof := ControlProof{Version: ControlProofVersion, Owner: "test-operator", Control: name, Host: p.Host, HostVersion: p.HostVersion, ExecutableSHA256: p.ExecutableSHA256, Workspace: p.Workspace, ScopeSHA256: ControlScopeDigest(p), ObservedAt: at.Format(time.RFC3339Nano), ExpiresAt: at.Add(4 * time.Minute).Format(time.RFC3339Nano), Status: "enforced", Checks: []string{"fixture-check"}}
		payload, _ := json.Marshal(proof)
		proof.Signature = hex.EncodeToString(ed25519.Sign(priv, payload))
		data, _ := json.Marshal(proof)
		file := name + ".json"
		if err = os.WriteFile(filepath.Join(root, file), data, 0600); err != nil {
			t.Fatal(err)
		}
		p.Controls = append(p.Controls, Control{Name: name, Owner: proof.Owner, Evidence: Asset{Path: file, SHA256: Hash(data)}, ObservedAt: proof.ObservedAt, ExpiresAt: proof.ExpiresAt, Workspace: p.Workspace, HostVersion: p.HostVersion})
	}
	runner := &codexQualificationRunner{model: model}
	b, err := Qualify(context.Background(), p, runner)
	if err != nil {
		t.Fatal(err)
	}
	if b.Receipt == nil || b.Receipt.Report.Status != "valid" || b.Receipt.Report.LiveGate != "fixture-qualified" || b.Receipt.Report.Accepted || b.Receipt.Report.AuthorizationGranted || b.Receipt.Report.ExecutionReady {
		t.Fatalf("Codex synthetic gate overstated: %+v read=%s", b.Receipt, b.Files["read-transcript.json"])
	}
	var probe modelcatalog.ProbeEvidence
	if err = json.Unmarshal(b.Files["invocation-probe.json"], &probe); err != nil {
		t.Fatal(err)
	}
	if probe.Outcome != "success" || probe.ProfileSHA256 == "" || probe.AccountScope != p.AccountScope || probe.EvidenceSHA256 == "" {
		t.Fatalf("synthetic invocation probe incomplete: %+v", probe)
	}
	for _, name := range []string{"smoke-transcript.json", "resume-transcript.json", "read-transcript.json", "denial-transcript.json", "transcript.json"} {
		var persisted Transcript
		if err = json.Unmarshal(b.Files[name], &persisted); err != nil {
			t.Fatalf("%s is not a transcript: %v", name, err)
		}
		if semanticJSON(persisted.Output) != `{"redacted":true}` {
			t.Errorf("%s persisted an unredacted output: %s", name, persisted.Output)
		}
	}
	if probe.EvidenceSHA256 != Hash(b.Files["smoke-transcript.json"]) {
		t.Fatal("invocation probe hash does not bind the persisted redacted smoke transcript")
	}
	var report struct {
		Cases []QualificationCase `json:"cases"`
	}
	if err = json.Unmarshal(b.Files["qualification.json"], &report); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"invocation", "resume", "permitted-workspace-read", "active-cancellation", "forbidden-outside-write"} {
		found := false
		for _, c := range report.Cases {
			if c.Name == want && c.Status == "passed" {
				found = true
			}
		}
		if !found {
			t.Fatalf("Codex required case %s missing: %+v", want, report.Cases)
		}
	}
}

func TestRedactQualificationTranscriptRemovesPrivacyCanaries(t *testing.T) {
	const canaryOutput = "qualification-output-private-canary"
	const canaryPath = "/outside/private/canary-file"
	const canaryText = "qualification-event-private-canary"
	transcript := Transcript{
		Output: json.RawMessage(`{"value":"` + canaryOutput + `"}`),
		Events: []Event{{Data: json.RawMessage(`{"command":"` + canaryPath + `","aggregatedOutput":"` + canaryText + `","text":"` + canaryText + `","safe":"retained"}`)}},
	}
	redacted := redactQualificationTranscript(transcript)
	raw, err := json.Marshal(redacted)
	if err != nil {
		t.Fatal(err)
	}
	for _, canary := range []string{canaryOutput, canaryPath, canaryText} {
		if strings.Contains(string(raw), canary) {
			t.Errorf("persisted transcript contains privacy canary %q: %s", canary, raw)
		}
	}
	if string(redacted.Output) != `{"redacted":true}` {
		t.Fatalf("output was not replaced with redaction marker: %s", redacted.Output)
	}
	if !strings.Contains(string(raw), "retained") {
		t.Fatalf("safe event metadata was discarded: %s", raw)
	}
}
