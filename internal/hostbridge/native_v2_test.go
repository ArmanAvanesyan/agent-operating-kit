package hostbridge

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/hostdriver"
)

func TestNativeV2RunUsesBoundProtocolFixtureWithoutInputSend(t *testing.T) {
	cases := []struct{ host, frame, wantArg string }{
		{"opencode", `{"type":"step_finish","status":"success","result":"{\"ok\":true}","sessionID":"s1","model":"m"}`, "--format"},
		{"cursor", `{"type":"result","result":"{\"ok\":true}","session_id":"s1","model":"m"}`, "stream-json"},
		{"gemini", `{"type":"result","response":"{\"ok\":true}","session_id":"s1","model":"m"}`, "--prompt"},
		{"openclaw", `{"ok":true,"status":"ok","final":"{\"ok\":true}","session_id":"s1","model":"m"}`, "--isolated"},
	}
	for _, tc := range cases {
		t.Run(tc.host, func(t *testing.T) {
			d, ok := hostdriver.Lookup(tc.host)
			if !ok {
				t.Fatal("driver missing")
			}
			p := Prepared{Version: VersionV2, Host: tc.host, HostVersion: "1.2.3", HostDriverSHA256: d.Digest, HostProtocol: d.Protocol, AssignmentSHA256: strings.Repeat("a", 64), Model: "model-x", Workspace: "/tmp", Instructions: "only task", Input: json.RawMessage(`{"task":"bounded"}`), OutputSchema: json.RawMessage(`{"type":"object"}`)}
			policy := Policy{Version: VersionV2, Host: tc.host, HostVersion: p.HostVersion, HostDriverSHA256: d.Digest, HostProtocol: d.Protocol, Executable: "/fixture/host", Workspace: p.Workspace, Models: []string{p.Model}, AuthMode: "native-login", TimeoutSeconds: 5}
			p.PolicySHA256 = PolicyHash(policy)
			r := &fakeClaudeRunner{frames: [][]byte{[]byte(tc.frame)}}
			got, err := NativeV2Run(context.Background(), p, policy, r, "attempt-42")
			if err != nil {
				t.Fatal(err)
			}
			if got.HostOutcome != "completed" || string(got.Output) != `{"ok":true}` {
				t.Fatalf("bad observation: outcome=%s output=%s", got.HostOutcome, got.Output)
			}
			if r.starts != 1 || len(r.launch.Args) == 0 {
				t.Fatalf("expected one controlled launch: %+v", r)
			}
			found := false
			for _, arg := range r.launch.Args {
				if arg == tc.wantArg {
					found = true
				}
			}
			if !found {
				t.Fatalf("%s launch missing protocol arg %q: %#v", tc.host, tc.wantArg, r.launch.Args)
			}
			if len(r.launch.Env) != 0 {
				t.Fatalf("native credential env leaked: %#v", r.launch.Env)
			}
		})
	}
}

func TestNativeV2RejectsResumeAndMissingAttemptIdentity(t *testing.T) {
	d, _ := hostdriver.Lookup("cursor")
	p := Prepared{Version: VersionV2, Host: "cursor", HostVersion: "1", HostDriverSHA256: d.Digest, HostProtocol: d.Protocol, PolicySHA256: "x", Model: "m", Workspace: "/tmp", Input: json.RawMessage(`{}`), OutputSchema: json.RawMessage(`{}`)}
	policy := Policy{Version: VersionV2, Host: "cursor", HostVersion: "1", HostDriverSHA256: d.Digest, HostProtocol: d.Protocol, Workspace: "/tmp", Executable: "cursor", TimeoutSeconds: 1}
	if _, err := NativeV2Run(context.Background(), p, policy, &fakeClaudeRunner{}, ""); err == nil {
		t.Fatal("missing attempt identity accepted")
	}
}

func TestV2LaunchRejectsControlsItCannotApply(t *testing.T) {
	d, _ := hostdriver.Lookup("cursor")
	p := Prepared{Version: VersionV2, Host: "cursor", HostVersion: "1", HostDriverSHA256: d.Digest, HostProtocol: d.Protocol, PolicySHA256: "x", Model: "m", Workspace: "/tmp", Input: json.RawMessage(`{}`), OutputSchema: json.RawMessage(`{}`)}
	effort := "high"
	for name, mutate := range map[string]func(*Policy){
		"effort":   func(p *Policy) { p.Configuration.Effort = &effort },
		"tools":    func(p *Policy) { p.Tools = []string{"shell"} },
		"settings": func(p *Policy) { p.Settings = json.RawMessage(`{"sandbox":true}`) },
	} {
		t.Run(name, func(t *testing.T) {
			policy := Policy{Version: VersionV2, Host: "cursor", HostVersion: "1", HostDriverSHA256: d.Digest, HostProtocol: d.Protocol, Workspace: "/tmp", Executable: "/fixture/cursor", TimeoutSeconds: 1}
			mutate(&policy)
			p.PolicySHA256 = PolicyHash(policy)
			if _, err := V2Launch(p, policy); err == nil {
				t.Fatal("unapplied control accepted")
			}
		})
	}
}
func TestV2LaunchPreservesExactInputNumbersAndCompiledOpenClawArguments(t *testing.T) {
	d, _ := hostdriver.Lookup("openclaw")
	policy := Policy{Version: VersionV2, Host: "openclaw", HostVersion: "1", HostDriverSHA256: d.Digest, HostProtocol: d.Protocol, Executable: "/fixture/host", Workspace: "/workspace", TimeoutSeconds: 10}
	p := Prepared{HostDriverSHA256: d.Digest, HostProtocol: d.Protocol, Version: VersionV2, Host: policy.Host, HostVersion: policy.HostVersion, PolicySHA256: PolicyHash(policy), Workspace: policy.Workspace, Model: "explicit/model", Input: json.RawMessage(`{"id":123456789012345678901234567890,"decimal":0.000000000000000001}`), OutputSchema: json.RawMessage(`{}`)}
	launch, e := V2Launch(p, policy)
	if e != nil {
		t.Fatal(e)
	}
	prompt := launch.Args[len(launch.Args)-1]
	if !strings.Contains(prompt, "123456789012345678901234567890") || !strings.Contains(prompt, "0.000000000000000001") {
		t.Fatal("rounded input", prompt)
	}
	for _, arg := range launch.Args {
		if arg == "--message" {
			t.Fatal("agent exec requires positional prompt")
		}
	}
}
