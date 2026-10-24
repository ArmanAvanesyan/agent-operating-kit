package hostbridge

import (
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/agentresolve"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/modelcatalog"
)

func copyCoreCorpus(t *testing.T, dst string) string {
	t.Helper()
	_, source, _, _ := runtime.Caller(0)
	src := filepath.Join(filepath.Dir(source), "..", "planning", "testdata", "corpus")
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		out := filepath.Join(dst, rel)
		if d.IsDir() {
			if rel == "." {
				return nil
			}
			return os.MkdirAll(out, 0700)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err = os.MkdirAll(filepath.Dir(out), 0700); err != nil {
			return err
		}
		return os.WriteFile(out, b, 0600)
	})
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dst, "assignment.json")
}

func corePointer(s string) *string { return &s }
func makeCorePolicy(t *testing.T, root, workspace string) Policy {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	bin, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	policy := Policy{Version: Version, Host: "claude-code", Executable: exe, ExecutableSHA256: Hash(bin), HostVersion: claudePinnedVersion, AuthMode: "native-login", WorkspaceRoots: []string{workspace}, Workspace: workspace, Models: []string{"claude-test"}, Tools: []string{"Read"}, Environment: []string{}, ConfigurationAssets: []Asset{}, Controls: []Control{}, TimeoutSeconds: 30, CodexConfig: map[string]json.RawMessage{}, Configuration: modelcatalog.Configuration{Host: corePointer("claude-code"), HostVersion: corePointer(claudePinnedVersion), ModelID: corePointer("claude-test"), ReasoningMode: corePointer("provider-default"), AuthMode: corePointer("native-login")}}
	policy.root = root
	return policy
}
func prepareCoreFixture(t *testing.T) (string, string, string, Policy, Bundle) {
	t.Helper()
	base := t.TempDir()
	root := filepath.Join(base, "corpus")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	assignment := copyCoreCorpus(t, root)
	workspace := filepath.Join(base, "workspace")
	if err := os.MkdirAll(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	profilePath := filepath.Join(root, "execution-profile.json")
	raw, err := os.ReadFile(profilePath)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err = json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	client := doc["client"].(map[string]any)
	client["adapter"] = "claude-code"
	model := doc["model"].(map[string]any)
	model["selector"] = "claude-test"
	model["reasoning"] = map[string]any{"mode": "provider-default"}
	delete(doc, "limits")
	updated, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(profilePath, updated, 0600); err != nil {
		t.Fatal(err)
	}
	agentPath := filepath.Join(root, "agent.json")
	agentBytes, err := os.ReadFile(agentPath)
	if err != nil {
		t.Fatal(err)
	}
	var agent map[string]any
	if err = json.Unmarshal(agentBytes, &agent); err != nil {
		t.Fatal(err)
	}
	instructions := agent["instructions"].(map[string]any)
	instructions["parameters"] = map[string]any{}
	caps := agent["capabilities"].(map[string]any)
	for _, key := range []string{"tools", "skills", "mcps", "plugins"} {
		items, _ := caps[key].([]any)
		for _, item := range items {
			if capability, ok := item.(map[string]any); ok {
				capability["required"] = false
			}
		}
	}
	agentBytes, err = json.MarshalIndent(agent, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(agentPath, agentBytes, 0600); err != nil {
		t.Fatal(err)
	}
	policy := makeCorePolicy(t, root, workspace)
	resolved := agentresolve.Resolve(assignment, root)
	if resolved.Snapshot == nil {
		t.Fatalf("fixture did not resolve: %+v", resolved.Diagnostics)
	}
	policy.Permissions = resolved.Snapshot.Permissions
	policy.AllowExecution = true
	bundle, err := Prepare(assignment, root, policy)
	if err != nil {
		t.Fatalf("Prepare fixture: %v", err)
	}
	return base, root, assignment, policy, bundle
}

type countingCoreRunner struct {
	starts         int
	launches       []Launch
	version, model string
}

func (r *countingCoreRunner) Start(_ context.Context, l Launch) (Process, error) {
	r.starts++
	r.launches = append(r.launches, l)
	args := strings.Join(l.Args, " ")
	if len(l.Args) > 0 && l.Args[0] == "--version" {
		return &coreProcess{frames: [][]byte{[]byte("claude " + r.version + "\n")}}, nil
	}
	if len(l.Args) > 1 && l.Args[0] == "auth" && l.Args[1] == "status" {
		return &coreProcess{frames: [][]byte{[]byte(`{"loggedIn":true,"authMethod":"oauth_token"}`)}}, nil
	}
	if strings.Contains(args, "--input-format stream-json") {
		init := []byte(`{"type":"system","subtype":"init","uuid":"11111111-1111-4111-8111-111111111111","session_id":"550e8400-e29b-41d4-a716-446655440000","model":"` + r.model + `","tools":[]}`)
		result := []byte(`{"type":"result","subtype":"success","session_id":"550e8400-e29b-41d4-a716-446655440000","structured_output":{"ok":true}}`)
		return &coreProcess{frames: [][]byte{init, result}}, nil
	}
	return &coreProcess{}, nil
}

type coreProcess struct {
	sent   bool
	frames [][]byte
	i      int
}

func (p *coreProcess) Send(b []byte) error { p.sent = true; return nil }
func (p *coreProcess) Receive(context.Context) ([]byte, error) {
	if p.i < len(p.frames) {
		b := p.frames[p.i]
		p.i++
		return b, nil
	}
	return nil, io.EOF
}
func (*coreProcess) Stop() error  { return nil }
func (*coreProcess) Close() error { return nil }
func (*coreProcess) Wait() error  { return nil }

func TestPrepareClaudeFixtureAndRunRefusesBeforeRunner(t *testing.T) {
	_, _, _, policy, bundle := prepareCoreFixture(t)
	p := *bundle.Prepared
	if p.Host != "claude-code" || p.Model != "claude-test" || p.ConfigurationID == "" || !json.Valid(p.OutputSchema) || p.Digest != preparedDigest(p) {
		t.Fatalf("unexpected prepared identity: %+v", p)
	}
	runner := &countingCoreRunner{}
	result, err := Run(context.Background(), p, policy, runner, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if runner.starts != 0 || result.Receipt == nil || result.Receipt.Report.Status != "blocked" || len(result.Receipt.Report.Diagnostics) == 0 || result.Receipt.Report.Diagnostics[0].Code != "enforcement-unverified" {
		t.Fatalf("missing controls did not block before host: starts=%d result=%+v", runner.starts, result.Receipt)
	}
	mutations := []struct {
		name     string
		prepared Prepared
		policy   Policy
	}{{"input", p, policy}, {"policy", p, policy}, {"model", p, policy}}
	mutations[0].prepared.Input = json.RawMessage(`{"changed":true}`)
	mutations[1].policy.Models = []string{"another-model"}
	mutations[2].prepared.Model = "another-model"
	for _, tc := range mutations {
		t.Run(tc.name, func(t *testing.T) {
			r := &countingCoreRunner{}
			got, e := Run(context.Background(), tc.prepared, tc.policy, r, nil, nil)
			if e != nil || r.starts != 0 || got.Receipt == nil || got.Receipt.Report.Status != "blocked" {
				t.Fatalf("drift did not fail closed: %+v %v starts=%d", got.Receipt, e, r.starts)
			}
		})
	}
}

func TestFakeNativeRunnerHandlesVersionAndStream(t *testing.T) {
	_, _, _, policy, bundle := prepareCoreFixture(t)
	p := *bundle.Prepared
	r := &countingCoreRunner{version: claudePinnedVersion, model: p.Model}
	capabilities, err := Preflight(context.Background(), policy, r)
	if err != nil || capabilities.HostVersion != claudePinnedVersion || r.starts != 2 {
		t.Fatalf("fake version preflight failed: %+v %v starts=%d", capabilities, err, r.starts)
	}
	transcript, err := ClaudeRun(context.Background(), p, policy, r, nil)
	if err != nil || transcript.HostOutcome != "completed" || transcript.Model != p.Model || r.starts != 3 {
		t.Fatalf("fake stream run failed: %+v %v starts=%d", transcript, err, r.starts)
	}
}

func TestPolicyMalformedDuplicateVersionSymlinkAndCapturedAssetTamper(t *testing.T) {
	base, root, _, policy, bundle := prepareCoreFixture(t)
	duplicate := filepath.Join(base, "duplicate-policy.json")
	if err := os.WriteFile(duplicate, []byte(`{"version":"aok-host-bridge/v1","version":"aok-host-bridge/v1"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadPolicy(duplicate); err == nil {
		t.Fatal("duplicate policy key accepted")
	}
	policy.Version = "aok-host-bridge/v0"
	versionPath := filepath.Join(base, "wrong-version.json")
	if err := os.WriteFile(versionPath, encoded(policy), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadPolicy(versionPath); err == nil {
		t.Fatal("unsupported host bridge version accepted")
	}
	link := filepath.Join(root, "assets", "symlink-task.json")
	if err := os.Symlink(filepath.Join(root, "assets", "task.json"), link); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadAsset(root, "assets/symlink-task.json"); err == nil {
		t.Fatal("symlinked corpus asset accepted")
	}
	out := filepath.Join(base, "bundle")
	if err := os.MkdirAll(out, 0700); err != nil {
		t.Fatal(err)
	}
	for name, data := range bundle.Files {
		path := filepath.Join(out, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	p, err := LoadPrepared(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Sources) == 0 {
		t.Fatal("prepared bundle did not capture sources")
	}
	asset := filepath.Join(out, filepath.FromSlash(p.Sources[0].Path))
	if err = os.WriteFile(asset, []byte("tampered"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = LoadPrepared(out); err == nil {
		t.Fatal("tampered captured source accepted")
	}
}

func TestPreparedRecomputedDigestCannotRebindCapturedInputs(t *testing.T) {
	base, _, _, _, bundle := prepareCoreFixture(t)
	out := filepath.Join(base, "prepared-rebinding")
	for name, data := range bundle.Files {
		path := filepath.Join(out, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	original := *bundle.Prepared
	for name, change := range map[string]func(*Prepared){
		"work-item": func(p *Prepared) {
			p.WorkItemRef = &contracts.Reference{Kind: "WorkItemBinding", ID: "injected", Version: "1.0.0"}
		},
		"profile":     func(p *Prepared) { p.ProfileSHA256 = Hash([]byte("different")) },
		"assignment":  func(p *Prepared) { p.AssignmentSHA256 = Hash([]byte("different")) },
		"schema":      func(p *Prepared) { p.OutputSchema = json.RawMessage(`{"type":"string"}`) },
		"native-plan": func(p *Prepared) { p.NativeTasks = []TaskStep{{ID: "injected", Subject: "Injected"}} },
	} {
		t.Run(name, func(t *testing.T) {
			p := original
			change(&p)
			p.Digest = preparedDigest(p)
			if err := os.WriteFile(filepath.Join(out, "prepared.json"), encoded(p), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadPrepared(out); err == nil {
				t.Fatal("rebound prepared inputs accepted")
			}
		})
	}
}
