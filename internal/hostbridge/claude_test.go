package hostbridge

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/modelcatalog"
)

const testClaudeSession = "550e8400-e29b-41d4-a716-446655440000"

func claudeFixture(t *testing.T) [][]byte {
	t.Helper()
	f, e := os.Open("testdata/claude/success.jsonl")
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	var out [][]byte
	s := bufio.NewScanner(f)
	for s.Scan() {
		out = append(out, append([]byte(nil), s.Bytes()...))
	}
	if e = s.Err(); e != nil {
		t.Fatal(e)
	}
	return out
}
func claudePrepared() Prepared {
	ptr := func(s string) *string { return &s }
	return Prepared{Host: "claude-code", HostVersion: claudePinnedVersion, Model: "claude-test", Configuration: modelcatalog.Configuration{Host: ptr("claude-code"), HostVersion: ptr(claudePinnedVersion), ModelID: ptr("claude-test"), ReasoningMode: ptr("provider-default"), AuthMode: ptr("native-login")}, Workspace: "/tmp", Instructions: "Do the requested work", Input: json.RawMessage(`"hello"`), OutputSchema: json.RawMessage(`{"type":"object","properties":{"ok":{"type":"boolean"}},"required":["ok"],"additionalProperties":false}`), NativeTasks: []TaskStep{{ID: "step-1", Subject: "Do work"}}}
}

type fakeClaudeRunner struct {
	frames  [][]byte
	launch  Launch
	starts  int
	waitErr error
}

func (r *fakeClaudeRunner) Start(_ context.Context, l Launch) (Process, error) {
	r.starts++
	r.launch = l
	return &fakeClaudeProcess{frames: r.frames, waitErr: r.waitErr}, nil
}

type fakeClaudeProcess struct {
	frames  [][]byte
	i       int
	sent    []byte
	waitErr error
}

func (p *fakeClaudeProcess) Send(b []byte) error { p.sent = append([]byte(nil), b...); return nil }
func (p *fakeClaudeProcess) Receive(context.Context) ([]byte, error) {
	if p.i >= len(p.frames) {
		return nil, io.EOF
	}
	b := p.frames[p.i]
	p.i++
	return b, nil
}
func (*fakeClaudeProcess) Stop() error   { return nil }
func (*fakeClaudeProcess) Close() error  { return nil }
func (p *fakeClaudeProcess) Wait() error { return p.waitErr }

func TestClaudeRunFixtureAndNativeTaskEvidence(t *testing.T) {
	r := &fakeClaudeRunner{frames: claudeFixture(t)}
	p := claudePrepared()
	policy := Policy{Host: "claude-code", HostVersion: claudePinnedVersion, Executable: "claude", AuthMode: "native-login", TimeoutSeconds: 5, AllowNativeTasks: true, Tools: []string{"Read"}, Settings: json.RawMessage(`{"disableAllHooks":true}`)}
	got, e := ClaudeRun(context.Background(), p, policy, r, nil)
	if e != nil {
		t.Fatal(e)
	}
	if got.HostOutcome != "completed" || got.SessionID != testClaudeSession || string(got.Output) != `{"ok":true}` || got.Model != p.Model {
		t.Fatalf("unexpected transcript: %#v", got)
	}
	if len(got.UsageFrames) != 1 || !strings.Contains(string(got.UsageFrames[0]), "0.1234567890123456789") {
		t.Fatalf("exact outer cumulative cost was not retained: %+v", got.UsageFrames)
	}
	var calls, results int
	for _, ev := range got.Events {
		if ev.Type == "native_task_call:TaskCreate" {
			calls++
		}
		if ev.Type == "native_task_result" {
			results++
			if !strings.Contains(string(ev.Data), "native-task-7") {
				t.Fatalf("missing native ID: %s", ev.Data)
			}
			if !strings.Contains(string(ev.Data), `"stepId":"step-1"`) || !strings.Contains(string(ev.Data), `"taskId":"native-task-7"`) {
				t.Fatalf("create result not tied to supported step: %s", ev.Data)
			}
		}
	}
	if calls != 1 || results != 1 {
		t.Fatalf("task evidence call/result counts %d/%d", calls, results)
	}
	for _, ev := range got.Events {
		if strings.Contains(string(ev.Data), "SECRET_CANARY") || strings.Contains(string(ev.Data), "PRIVATE_CWD") || strings.Contains(string(ev.Data), "PRIVATE_ACCOUNT") || strings.Contains(string(ev.Data), "UNRELATED_SESSION_HISTORY") {
			t.Fatalf("private/unrelated stream material persisted in event: %s", ev.Data)
		}
	}
	joined := strings.Join(r.launch.Args, " ")
	for _, want := range []string{"--resume", "--tools", "TaskCreate", "--setting-sources", "--strict-mcp-config", "--disallowedTools", "--permission-mode"} {
		if !strings.Contains(joined, want) && want != "--resume" {
			t.Errorf("missing arg %s in %s", want, joined)
		}
	}
	if strings.Contains(joined, "TaskCreate,TaskGet,TaskList,TaskUpdate,Read") {
		t.Fatalf("unexpected tool ordering: %s", joined)
	}
}
func TestClaudeResumeAndRefusesCredentials(t *testing.T) {
	r := &fakeClaudeRunner{frames: claudeFixture(t)}
	p := claudePrepared()
	pol := Policy{Host: "claude-code", HostVersion: claudePinnedVersion, Executable: "claude", AuthMode: "native-login", TimeoutSeconds: 2, AllowNativeTasks: true, Tools: []string{"Read"}}
	prev := &Receipt{SessionID: testClaudeSession}
	if _, e := ClaudeRun(context.Background(), p, pol, r, prev); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(strings.Join(r.launch.Args, " "), "--resume "+testClaudeSession) {
		t.Fatalf("resume not used: %v", r.launch.Args)
	}
	pol.Environment = []string{"ANTHROPIC_API_KEY"}
	if _, e := ClaudeRun(context.Background(), p, pol, r, nil); e == nil {
		t.Fatal("native login accepted API credential")
	}
}
func TestParseClaudeRejectsAuthModelMismatchAndDuplicateConflicts(t *testing.T) {
	p := claudePrepared()
	frames := claudeFixture(t)
	auth := append([][]byte(nil), frames[:1]...)
	auth = append(auth, []byte(`{"type":"result","subtype":"error_during_execution","session_id":"550e8400-e29b-41d4-a716-446655440000","result":"authentication failed"}`))
	out, e := ParseClaudeFrames(p, nil, auth)
	if e != nil || out.HostOutcome != "error" {
		t.Fatalf("auth error: %#v %v", out, e)
	}
	bad := append([][]byte(nil), frames...)
	bad[0] = []byte(strings.Replace(string(bad[0]), "claude-test", "other-model", 1))
	if _, e = ParseClaudeFrames(p, nil, bad); e == nil {
		t.Fatal("model mismatch accepted")
	}
	dup := append([][]byte(nil), frames...)
	dup = append(dup, []byte(strings.Replace(string(frames[1]), "Do work", "different", 1)))
	if _, e = ParseClaudeFrames(p, nil, dup); e == nil {
		t.Fatal("conflicting duplicate event accepted")
	}
	apiKey := append([][]byte(nil), frames...)
	apiKey[0] = []byte(strings.Replace(string(apiKey[0]), `"tools":[`, `"apiKeySource":"ANTHROPIC_API_KEY","tools":[`, 1))
	if _, e = ParseClaudeFrames(p, nil, apiKey); e == nil {
		t.Fatal("API-key auth source accepted under native-login bridge")
	}
	dupType := append([][]byte(nil), frames...)
	dupType[1] = []byte(strings.Replace(string(dupType[1]), `"type":"assistant"`, `"type":"assistant","type":"user"`, 1))
	if _, e = ParseClaudeFrames(p, nil, dupType); e == nil {
		t.Fatal("duplicate top-level event type accepted")
	}
	dupNested := append([][]byte(nil), frames...)
	dupNested[len(dupNested)-1] = []byte(strings.Replace(string(dupNested[len(dupNested)-1]), `"input_tokens":12`, `"input_tokens":12,"input_tokens":900`, 1))
	if _, e = ParseClaudeFrames(p, nil, dupNested); e == nil {
		t.Fatal("duplicate nested usage counter accepted")
	}
}
func TestParseClaudeEOFIsUnknownAndRequiresValidOutput(t *testing.T) {
	p := claudePrepared()
	f := claudeFixture(t)
	got, e := ParseClaudeFrames(p, nil, f[:1])
	if e != nil || got.HostOutcome != "unknown" {
		t.Fatalf("EOF outcome %#v %v", got, e)
	}
	bad := append([][]byte(nil), f...)
	bad[len(bad)-1] = []byte(`{"type":"result","subtype":"success","structured_output":{"ok":"wrong"}}`)
	if _, e = ParseClaudeFrames(p, nil, bad); e == nil {
		t.Fatal("schema-invalid result accepted")
	}
}

type cancelClaudeRunner struct{}

func (cancelClaudeRunner) Start(context.Context, Launch) (Process, error) {
	return cancelClaudeProcess{}, nil
}

type cancelClaudeProcess struct{}

func (cancelClaudeProcess) Send([]byte) error { return nil }
func (cancelClaudeProcess) Receive(ctx context.Context) ([]byte, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}
func (cancelClaudeProcess) Stop() error  { return nil }
func (cancelClaudeProcess) Close() error { return nil }
func (cancelClaudeProcess) Wait() error  { return nil }

type partialCancelClaudeRunner struct{ frames [][]byte }

func (r partialCancelClaudeRunner) Start(context.Context, Launch) (Process, error) {
	return &partialCancelClaudeProcess{frames: r.frames}, nil
}

type partialCancelClaudeProcess struct {
	frames [][]byte
	index  int
}

func (*partialCancelClaudeProcess) Send([]byte) error { return nil }
func (p *partialCancelClaudeProcess) Receive(ctx context.Context) ([]byte, error) {
	if p.index < len(p.frames) {
		frame := p.frames[p.index]
		p.index++
		return frame, nil
	}
	<-ctx.Done()
	return nil, ctx.Err()
}
func (*partialCancelClaudeProcess) Stop() error  { return nil }
func (*partialCancelClaudeProcess) Close() error { return nil }
func (*partialCancelClaudeProcess) Wait() error  { return nil }

func TestClaudeRunCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := ClaudeRun(ctx, claudePrepared(), Policy{Host: "claude-code", HostVersion: claudePinnedVersion, Executable: "claude", AuthMode: "native-login", TimeoutSeconds: 2, AllowNativeTasks: true}, cancelClaudeRunner{}, nil)
	if err == nil || !strings.Contains(err.Error(), "canceled") {
		t.Fatalf("expected cancellation, got %v", err)
	}
}

func TestClaudeRunCancellationPreservesSanitizedPartialSession(t *testing.T) {
	frames := claudeFixture(t)[:2]
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	transcript, err := ClaudeRun(ctx, claudePrepared(), Policy{Host: "claude-code", HostVersion: claudePinnedVersion, Executable: "claude", AuthMode: "native-login", TimeoutSeconds: 2, AllowNativeTasks: true}, partialCancelClaudeRunner{frames: frames}, nil)
	if err == nil || !strings.Contains(err.Error(), "canceled") {
		t.Fatalf("expected cancellation, got %v", err)
	}
	if transcript.HostOutcome != "interrupted" || transcript.SessionID != testClaudeSession || transcript.Model != "claude-test" || len(transcript.Events) < 2 || len(transcript.Output) != 0 {
		t.Fatalf("partial interruption identity was not retained safely: %#v", transcript)
	}
	for _, event := range transcript.Events {
		if strings.Contains(string(event.Data), "SECRET_CANARY") || strings.Contains(string(event.Data), "UNRELATED_SESSION_HISTORY") {
			t.Fatalf("unsafe partial data persisted: %s", event.Data)
		}
	}
}

func TestClaudeRunChecksSuccessfulProcessExit(t *testing.T) {
	r := &fakeClaudeRunner{frames: claudeFixture(t), waitErr: errors.New("private process error")}
	p := claudePrepared()
	pol := Policy{Host: "claude-code", HostVersion: claudePinnedVersion, Executable: "claude", AuthMode: "native-login", TimeoutSeconds: 5, AllowNativeTasks: true}
	if got, err := ClaudeRun(context.Background(), p, pol, r, nil); err == nil || got.HostOutcome != "" {
		t.Fatalf("nonzero process exit returned output: %#v %v", got, err)
	}
}

func TestClaudeNativeTaskDeletionResultIsOnlyObservedID(t *testing.T) {
	p := claudePrepared()
	p.NativeTasks = nil
	frames := [][]byte{
		[]byte(`{"type":"system","subtype":"init","uuid":"11111111-1111-4111-8111-111111111111","session_id":"550e8400-e29b-41d4-a716-446655440000","model":"claude-test","tools":["TaskUpdate"]}`),
		[]byte(`{"type":"assistant","uuid":"22222222-2222-4222-8222-222222222222","message":{"role":"assistant","content":[{"type":"tool_use","id":"call-delete","name":"TaskUpdate","input":{"taskId":"native-9","status":"deleted","description":"SHOULD_NOT_PERSIST"}}]}}`),
		[]byte(`{"type":"user","uuid":"33333333-3333-4333-8333-333333333333","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"call-delete","content":"{\"taskId\":\"native-9\",\"status\":\"deleted\",\"accepted\":true}"}]}}`),
		[]byte(`{"type":"result","subtype":"success","session_id":"550e8400-e29b-41d4-a716-446655440000","structured_output":{"ok":true}}`),
	}
	got, err := ParseClaudeFrames(p, nil, frames)
	if err != nil {
		t.Fatal(err)
	}
	var result json.RawMessage
	for _, ev := range got.Events {
		if ev.Type == "native_task_result" {
			result = ev.Data
		}
		if strings.Contains(string(ev.Data), "SHOULD_NOT_PERSIST") || strings.Contains(string(ev.Data), "accepted") {
			t.Fatalf("unexpected task authority/data: %s", ev.Data)
		}
	}
	if string(result) != `{"taskIds":["native-9"]}` {
		t.Fatalf("expected exact observed ID only, got %s", result)
	}
}

func TestClaudeSettingsAndConfigurationFailClosed(t *testing.T) {
	for _, raw := range []string{`{"sandbox":{"enabled":true}}`, `{"sandbox":{"enabled":true,"allowUnsandboxedCommands":true}}`, `{"sandbox":{"enabled":true,"allowUnsandboxedCommands":false,"extra":true}}`, `{"sandbox":false}`, `{"apiKeyHelper":"x"}`} {
		if _, err := claudeSettings(json.RawMessage(raw)); err == nil {
			t.Errorf("weak or unsupported setting accepted: %s", raw)
		}
	}
	good, err := claudeSettings(json.RawMessage(`{"disableAllHooks":true,"sandbox":{"enabled":true,"allowUnsandboxedCommands":false}}`))
	if err != nil || !json.Valid(good) {
		t.Fatalf("strict sandbox rejected: %s %v", good, err)
	}
	effort, mode := "high", "effort"
	args, err := claudeConfigurationArgs(modelcatalog.Configuration{ReasoningMode: &mode, Effort: &effort})
	if err != nil || len(args) != 2 || args[0] != "--effort" || args[1] != "high" {
		t.Fatalf("effort args %v %v", args, err)
	}
	if _, err = claudeConfigurationArgs(modelcatalog.Configuration{ReasoningMode: &mode}); err == nil {
		t.Fatal("effort reasoning mode without effort accepted")
	}
	tier := "priority"
	if _, err = claudeConfigurationArgs(modelcatalog.Configuration{ServiceTier: &tier}); err == nil {
		t.Fatal("unsupported service tier accepted")
	}
	r := &fakeClaudeRunner{frames: claudeFixture(t)}
	p := claudePrepared()
	p.Configuration = modelcatalog.Configuration{ReasoningMode: &mode, Effort: &effort}
	policy := Policy{Host: "claude-code", HostVersion: claudePinnedVersion, Executable: "claude", AuthMode: "native-login", TimeoutSeconds: 5, AllowNativeTasks: true}
	if _, err = ClaudeRun(context.Background(), p, policy, r, nil); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(r.launch.Args, " ")
	if !strings.Contains(joined, "--effort high") {
		t.Fatalf("prepared effort not passed exactly: %s", joined)
	}
	unsupported := "bedrock"
	p.Configuration = modelcatalog.Configuration{Provider: &unsupported}
	r = &fakeClaudeRunner{frames: claudeFixture(t)}
	if _, err = ClaudeRun(context.Background(), p, policy, r, nil); err == nil || r.starts != 0 {
		t.Fatal("unsupported provider route was launched")
	}
}

func TestClaudeTaskReadbackMustMatchExactNativeID(t *testing.T) {
	p := claudePrepared()
	p.NativeTasks = []TaskStep{{ID: "step-known", Subject: "Known", NativeID: "expected-native"}}
	frames := [][]byte{
		[]byte(`{"type":"system","subtype":"init","uuid":"11111111-1111-4111-8111-111111111111","session_id":"550e8400-e29b-41d4-a716-446655440000","model":"claude-test","tools":["TaskGet"]}`),
		[]byte(`{"type":"assistant","uuid":"22222222-2222-4222-8222-222222222222","message":{"role":"assistant","content":[{"type":"tool_use","id":"call-get","name":"TaskGet","input":{"taskId":"expected-native"}}]}}`),
		[]byte(`{"type":"user","uuid":"33333333-3333-4333-8333-333333333333","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"call-get","content":"{\"taskId\":\"unrelated-native\"}"}]}}`),
		[]byte(`{"type":"result","subtype":"success","session_id":"550e8400-e29b-41d4-a716-446655440000","structured_output":{"ok":true}}`),
	}
	got, err := ParseClaudeFrames(p, nil, frames)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range got.Events {
		if ev.Type == "native_task_result" && strings.Contains(string(ev.Data), "taskBindings") {
			t.Fatalf("unrelated readback bound to AOK step: %s", ev.Data)
		}
	}
}
