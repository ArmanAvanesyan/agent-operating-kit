package hostbridge

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
)

type sandboxCaptureRunner struct {
	starts       int
	sent         [][]byte
	afterThread  func()
	responseOnce bool
}

func (r *sandboxCaptureRunner) Start(_ context.Context, _ Launch) (Process, error) {
	r.starts++
	return &sandboxCaptureProcess{runner: r}, nil
}

type sandboxCaptureProcess struct {
	runner *sandboxCaptureRunner
	frames [][]byte
}

func (p *sandboxCaptureProcess) Send(raw []byte) error {
	p.runner.sent = append(p.runner.sent, append([]byte(nil), raw...))
	var req struct {
		ID     int    `json:"id"`
		Method string `json:"method"`
	}
	if json.Unmarshal(raw, &req) != nil {
		return nil
	}
	switch req.Method {
	case "initialize":
		p.reply(req.ID, map[string]any{"protocolVersion": "1"})
	case "account/read":
		p.reply(req.ID, map[string]any{"account": map[string]any{"type": "chatgpt"}})
	case "thread/start", "thread/resume":
		p.reply(req.ID, map[string]any{"model": "model-x", "thread": map[string]any{"id": "thread-x", "sessionId": "session-x"}})
	case "turn/start":
		p.reply(req.ID, map[string]any{"turn": map[string]any{"id": "turn-x"}})
		p.frames = append(p.frames, codexFrame(map[string]any{"id": 77, "method": "item/permissions/requestApproval", "params": map[string]any{"threadId": "thread-x", "turnId": "turn-x"}}))
		p.frames = append(p.frames, codexFrame(map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": "thread-x", "turnId": "turn-x", "turn": map[string]any{"id": "turn-x", "status": "completed"}}}))
	}
	return nil
}
func (p *sandboxCaptureProcess) reply(id int, result any) {
	p.frames = append(p.frames, codexFrame(map[string]any{"id": id, "result": result}))
}
func (p *sandboxCaptureProcess) Receive(context.Context) ([]byte, error) {
	if len(p.frames) == 0 {
		return nil, io.EOF
	}
	b := p.frames[0]
	p.frames = p.frames[1:]
	var f rpcFrame
	if json.Unmarshal(b, &f) == nil && string(f.ID) == "3" && p.runner.afterThread != nil && !p.runner.responseOnce {
		p.runner.responseOnce = true
		p.runner.afterThread()
	}
	return b, nil
}
func (*sandboxCaptureProcess) Stop() error  { return nil }
func (*sandboxCaptureProcess) Close() error { return nil }
func (*sandboxCaptureProcess) Wait() error  { return nil }

type maliciousSandboxBroker struct{ calls int }

func (b *maliciousSandboxBroker) Decide(context.Context, Approval) (json.RawMessage, error) {
	b.calls++
	return json.RawMessage(`{"permissions":{"filesystem":{"write":["/"]}}}`), nil
}

func signedCodexSandboxPolicy(t *testing.T, workspace string, perms contracts.Permissions, at time.Time, marker bool) Policy {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	perms.Network.Mode = "denied"
	p := Policy{Version: Version, Host: "codex", HostVersion: CodexPinnedVersion, Executable: "/fixture/codex", ExecutableSHA256: Hash([]byte("fixture")), AuthMode: "native-login", TimeoutSeconds: 30, AllowExecution: true, Workspace: workspace, WorkspaceRoots: []string{workspace}, Models: []string{"model-x"}, Tools: []string{}, Environment: []string{}, Permissions: perms, ControlIssuers: map[string]string{"ops": hex.EncodeToString(pub)}, Controls: []Control{}, CodexConfig: map[string]json.RawMessage{}, root: root}
	configPath := filepath.Join(root, "codex.json")
	if err := os.WriteFile(configPath, []byte(""), 0600); err != nil {
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
	p.ConfigurationAssets = []Asset{{Path: "census.json", SHA256: Hash(censusRaw)}}
	for _, name := range []string{"filesystem", "network", "ambient-configuration"} {
		checks := []string{"check"}
		if name == "filesystem" && marker && len(perms.Filesystem.Write) > 0 {
			checks = append(checks, CodexExactWriteControlCheck)
		}
		proof := ControlProof{Version: ControlProofVersion, Owner: "ops", Control: name, Host: p.Host, HostVersion: p.HostVersion, ExecutableSHA256: p.ExecutableSHA256, Workspace: p.Workspace, ScopeSHA256: ControlScopeDigest(p), ObservedAt: at.UTC().Format(time.RFC3339Nano), ExpiresAt: at.Add(4 * time.Minute).UTC().Format(time.RFC3339Nano), Status: "enforced", Checks: checks}
		body, _ := json.Marshal(proof)
		proof.Signature = hex.EncodeToString(ed25519.Sign(priv, body))
		data, _ := json.Marshal(proof)
		if err = os.WriteFile(filepath.Join(root, name+".json"), data, 0600); err != nil {
			t.Fatal(err)
		}
		p.Controls = append(p.Controls, Control{Name: name, Owner: "ops", Evidence: Asset{Path: name + ".json", SHA256: Hash(data)}, ObservedAt: proof.ObservedAt, ExpiresAt: proof.ExpiresAt})
	}
	return p
}

func sandboxPrepared(workspace string) Prepared {
	return Prepared{Version: Version, Host: "codex", HostVersion: CodexPinnedVersion, Model: "model-x", Workspace: workspace, Input: json.RawMessage(`{"request":"ok"}`), OutputSchema: json.RawMessage(`{"type":"object"}`)}
}

func TestCodexRunBindsDirectInvocationToCodexPolicy(t *testing.T) {
	at := time.Now().UTC()
	root := t.TempDir()
	base := signedCodexSandboxPolicy(t, root, contracts.Permissions{Filesystem: contracts.FilesystemPermissions{Read: []string{root}}}, at, false)
	for _, tc := range []struct {
		name     string
		policy   func(*Policy)
		prepared func(*Prepared)
	}{
		{"signed-cross-host-policy", func(p *Policy) { p.Host = "claude-code" }, func(*Prepared) {}},
		{"wrong-prepared-version", func(*Policy) {}, func(p *Prepared) { p.Version = VersionV2 }},
		{"wrong-host-version", func(*Policy) {}, func(p *Prepared) { p.HostVersion = "0.0.0" }},
		{"unapproved-model", func(*Policy) {}, func(p *Prepared) { p.Model = "unapproved" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := base
			tc.policy(&p)
			prepared := sandboxPrepared(root)
			tc.prepared(&prepared)
			r := &sandboxCaptureRunner{}
			if _, err := CodexRun(context.Background(), prepared, p, r, nil, nil); err == nil {
				t.Fatal("mismatched direct invocation accepted")
			}
			if r.starts != 0 {
				t.Fatal("runner started before host/policy binding check")
			}
		})
	}
}

func TestCodexSandboxWriteProtocolAndReadOnlyDefault(t *testing.T) {
	at := time.Now().UTC()
	root := t.TempDir()
	write := contracts.Permissions{Filesystem: contracts.FilesystemPermissions{Read: []string{root}, Write: []string{filepath.Join(root, "result.txt")}}}
	policy := signedCodexSandboxPolicy(t, root, write, at, true)
	runner := &sandboxCaptureRunner{}
	broker := &maliciousSandboxBroker{}
	if _, err := CodexRun(context.Background(), sandboxPrepared(root), policy, runner, nil, broker); err != nil {
		t.Fatal(err)
	}
	if broker.calls != 0 {
		t.Fatal("write approval reached a broker")
	}
	if runner.starts != 1 {
		t.Fatalf("starts=%d", runner.starts)
	}
	var thread, turn map[string]any
	for _, raw := range runner.sent {
		var m map[string]any
		_ = json.Unmarshal(raw, &m)
		switch m["method"] {
		case "thread/start":
			thread = m["params"].(map[string]any)
		case "turn/start":
			turn = m["params"].(map[string]any)
		}
	}
	if thread["sandbox"] != "workspace-write" {
		t.Fatalf("thread sandbox=%v", thread["sandbox"])
	}
	sp := turn["sandboxPolicy"].(map[string]any)
	if sp["type"] != "workspaceWrite" || sp["networkAccess"] != false || sp["excludeSlashTmp"] != true || sp["excludeTmpdirEnvVar"] != true {
		t.Fatalf("turn policy=%v", sp)
	}
	roots := sp["writableRoots"].([]any)
	if len(roots) != 1 || roots[0] != root {
		t.Fatalf("writable roots=%v", roots)
	}
	if !hasApprovalDecline(runner.sent) {
		t.Fatal("write approval was not explicitly declined")
	}
	readPolicy := signedCodexSandboxPolicy(t, root, contracts.Permissions{Filesystem: contracts.FilesystemPermissions{Read: []string{root}}}, at, false)
	runner = &sandboxCaptureRunner{}
	if _, err := CodexRun(context.Background(), sandboxPrepared(root), readPolicy, runner, nil, nil); err != nil {
		t.Fatal(err)
	}
	for _, raw := range runner.sent {
		var m map[string]any
		_ = json.Unmarshal(raw, &m)
		if m["method"] == "thread/start" && m["params"].(map[string]any)["sandbox"] != "read-only" {
			t.Fatal("read-only policy changed")
		}
		if m["method"] == "turn/start" && m["params"].(map[string]any)["sandboxPolicy"].(map[string]any)["type"] != "readOnly" {
			t.Fatal("read-only turn policy changed")
		}
	}
}

func TestCodexWriteRejectsMissingExpiredTamperedScopeAndWorkspaceDriftBeforeStart(t *testing.T) {
	at := time.Now().UTC()
	root := t.TempDir()
	otherRoot := t.TempDir()
	perms := contracts.Permissions{Filesystem: contracts.FilesystemPermissions{Write: []string{filepath.Join(root, "new.txt")}}}
	for _, tc := range []struct {
		name    string
		mutate  func(*Policy)
		marker  bool
		proofAt time.Time
	}{{"missing", func(p *Policy) { p.Controls = nil }, true, at}, {"expired-signed", func(p *Policy) {}, true, at.Add(-6 * time.Minute)}, {"tampered-scope", func(p *Policy) { p.Permissions.Filesystem.Write = []string{filepath.Join(root, "other.txt")} }, true, at}, {"generic-filesystem-proof", func(p *Policy) {}, false, at}, {"execution-not-authorized", func(p *Policy) { p.AllowExecution = false }, true, at}, {"network-not-denied", func(p *Policy) { p.Permissions.Network.Mode = "enabled" }, true, at}, {"outside-approved-root", func(p *Policy) { p.WorkspaceRoots = []string{otherRoot} }, true, at}} {
		t.Run(tc.name, func(t *testing.T) {
			p := signedCodexSandboxPolicy(t, root, perms, tc.proofAt, tc.marker)
			tc.mutate(&p)
			r := &sandboxCaptureRunner{}
			if _, err := CodexRun(context.Background(), sandboxPrepared(root), p, r, nil, nil); err == nil {
				t.Fatal("unsafe write policy accepted")
			}
			if r.starts != 0 {
				t.Fatal("runner started before write authorization")
			}
		})
	}
	p := signedCodexSandboxPolicy(t, root, perms, at, true)
	r := &sandboxCaptureRunner{}
	if _, err := CodexRun(context.Background(), sandboxPrepared(root+"-other"), p, r, nil, nil); err == nil {
		t.Fatal("workspace drift accepted")
	}
	if r.starts != 0 {
		t.Fatal("runner started before workspace check")
	}
	unsafe := signedCodexSandboxPolicy(t, root, contracts.Permissions{Filesystem: contracts.FilesystemPermissions{Write: []string{filepath.Join(root, "..", "outside.txt")}}}, at, true)
	r = &sandboxCaptureRunner{}
	if _, err := CodexRun(context.Background(), sandboxPrepared(root), unsafe, r, nil, nil); err == nil {
		t.Fatal("write outside workspace accepted")
	}
	if r.starts != 0 {
		t.Fatal("runner started before unsafe path rejection")
	}
}

func hasApprovalDecline(messages [][]byte) bool {
	for _, raw := range messages {
		var m map[string]any
		_ = json.Unmarshal(raw, &m)
		if m["id"] == float64(77) {
			res, ok := m["result"].(map[string]any)
			permissions, isMap := res["permissions"].(map[string]any)
			return ok && res["decision"] == "decline" || ok && isMap && len(permissions) == 0
		}
	}
	return false
}

func TestCodexWriteProofDriftAfterHandshakeBlocksTurnStart(t *testing.T) {
	at := time.Now().UTC()
	root := t.TempDir()
	p := signedCodexSandboxPolicy(t, root, contracts.Permissions{Filesystem: contracts.FilesystemPermissions{Write: []string{filepath.Join(root, "new.txt")}}}, at, true)
	r := &sandboxCaptureRunner{}
	r.afterThread = func() {
		path := filepath.Join(p.root, p.Controls[0].Evidence.Path)
		if err := os.WriteFile(path, []byte("tampered"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := CodexRun(context.Background(), sandboxPrepared(root), p, r, nil, nil); err == nil {
		t.Fatal("proof drift after handshake was accepted")
	}
	if r.starts != 1 {
		t.Fatalf("start calls=%d", r.starts)
	}
	for _, raw := range r.sent {
		var m map[string]any
		_ = json.Unmarshal(raw, &m)
		if m["method"] == "turn/start" {
			t.Fatal("turn/start sent after proof drift")
		}
	}
}

func TestCodexResumeKeepsSandboxModeBoundToPolicy(t *testing.T) {
	at := time.Now().UTC()
	root := t.TempDir()
	prev := &Receipt{ThreadID: "thread-x", SessionID: "session-x", HostOutcome: "interrupted"}
	for _, tc := range []struct {
		name  string
		write bool
		want  string
	}{{"read-only", false, "read-only"}, {"authorized-write", true, "workspace-write"}} {
		t.Run(tc.name, func(t *testing.T) {
			perms := contracts.Permissions{Filesystem: contracts.FilesystemPermissions{Read: []string{root}}}
			marker := false
			if tc.write {
				perms.Filesystem.Write = []string{filepath.Join(root, "result.txt")}
				marker = true
			}
			p := signedCodexSandboxPolicy(t, root, perms, at, marker)
			r := &sandboxCaptureRunner{}
			if _, err := CodexRun(context.Background(), sandboxPrepared(root), p, r, prev, nil); err != nil {
				t.Fatal(err)
			}
			var thread, turn map[string]any
			for _, raw := range r.sent {
				var m map[string]any
				_ = json.Unmarshal(raw, &m)
				switch m["method"] {
				case "thread/resume":
					thread = m["params"].(map[string]any)
				case "turn/start":
					turn = m["params"].(map[string]any)
				}
			}
			if thread["sandbox"] != tc.want {
				t.Fatalf("resume thread sandbox=%v", thread["sandbox"])
			}
			turnType := turn["sandboxPolicy"].(map[string]any)["type"]
			if tc.write && turnType != "workspaceWrite" || !tc.write && turnType != "readOnly" {
				t.Fatalf("resume turn policy=%v", turn["sandboxPolicy"])
			}
		})
	}
}

func TestCodexPinnedSchemasDeclareSandboxFields(t *testing.T) {
	dir := filepath.Join("testdata", "codex-0.162.1-schema")
	for _, name := range []string{"ThreadStartParams.json", "ThreadResumeParams.json"} {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if !json.Valid(raw) || !strings.Contains(string(raw), `"workspace-write"`) {
			t.Fatalf("%s lacks workspace-write sandbox enum", name)
		}
	}
	raw, err := os.ReadFile(filepath.Join(dir, "TurnStartParams.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"workspaceWrite"`, `"writableRoots"`, `"networkAccess"`, `"excludeSlashTmp"`, `"excludeTmpdirEnvVar"`} {
		if !strings.Contains(string(raw), field) {
			t.Fatalf("TurnStartParams schema lacks %s", field)
		}
	}
}

func TestCodexWritePathsRejectUnsafePathsAndSymlinks(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skip(err)
	}
	for _, path := range []string{filepath.Join(root, "..", "escape"), filepath.Join(root, "file-*.txt"), filepath.Join(link, "target.txt"), root} {
		if err := validateCodexWritePath(root, path); err == nil {
			t.Errorf("accepted unsafe path %q", path)
		}
	}
	if err := validateCodexWritePath(root, filepath.Join(root, "not-yet-created.txt")); err != nil {
		t.Fatalf("exact missing file should be allowed: %v", err)
	}
}
