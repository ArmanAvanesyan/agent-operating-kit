package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/agentresolve"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/hostbridge"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/modelcatalog"
)

func prepareHostCLIInput(t *testing.T) (string, string, string, hostbridge.Prepared) {
	t.Helper()
	base := t.TempDir()
	root := filepath.Join(base, "corpus")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	_, src, _, _ := runtime.Caller(0)
	src = filepath.Join(filepath.Dir(src), "..", "planning", "testdata", "corpus")
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		rel, e := filepath.Rel(src, path)
		if e != nil {
			return e
		}
		dst := filepath.Join(root, rel)
		if d.IsDir() {
			if rel == "." {
				return nil
			}
			return os.MkdirAll(dst, 0700)
		}
		b, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		if e = os.MkdirAll(filepath.Dir(dst), 0700); e != nil {
			return e
		}
		return os.WriteFile(dst, b, 0600)
	})
	if err != nil {
		t.Fatal(err)
	}
	profile := filepath.Join(root, "execution-profile.json")
	b, err := os.ReadFile(profile)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err = json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	doc["client"].(map[string]any)["adapter"] = "claude-code"
	model := doc["model"].(map[string]any)
	model["selector"] = "claude-test"
	model["reasoning"] = map[string]any{"mode": "provider-default"}
	delete(doc, "limits")
	b, _ = json.MarshalIndent(doc, "", "  ")
	if err = os.WriteFile(profile, b, 0600); err != nil {
		t.Fatal(err)
	}
	agent := filepath.Join(root, "agent.json")
	b, err = os.ReadFile(agent)
	if err != nil {
		t.Fatal(err)
	}
	var a map[string]any
	if err = json.Unmarshal(b, &a); err != nil {
		t.Fatal(err)
	}
	a["instructions"].(map[string]any)["parameters"] = map[string]any{}
	caps := a["capabilities"].(map[string]any)
	for _, key := range []string{"tools", "skills", "mcps", "plugins"} {
		items, _ := caps[key].([]any)
		for _, item := range items {
			if capability, ok := item.(map[string]any); ok {
				capability["required"] = false
			}
		}
	}
	b, _ = json.MarshalIndent(a, "", "  ")
	if err = os.WriteFile(agent, b, 0600); err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(base, "workspace")
	if err = os.MkdirAll(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	bin, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	ptr := func(s string) *string { return &s }
	assignment := filepath.Join(root, "assignment.json")
	policy := hostbridge.Policy{Version: hostbridge.Version, Host: "claude-code", Executable: exe, ExecutableSHA256: hex.EncodeToString(sha256Sum(bin)), HostVersion: "2.1.303", AuthMode: "native-login", WorkspaceRoots: []string{workspace}, Workspace: workspace, Models: []string{"claude-test"}, Tools: []string{"Read"}, Environment: []string{}, ConfigurationAssets: []hostbridge.Asset{}, Controls: []hostbridge.Control{}, TimeoutSeconds: 30, AllowExecution: true, CodexConfig: map[string]json.RawMessage{}, Configuration: modelcatalog.Configuration{Host: ptr("claude-code"), HostVersion: ptr("2.1.303"), ModelID: ptr("claude-test"), ReasoningMode: ptr("provider-default"), AuthMode: ptr("native-login")}}
	resolved := agentresolve.Resolve(assignment, root)
	if resolved.Snapshot == nil {
		t.Fatalf("fixture did not resolve: %+v", resolved.Diagnostics)
	}
	policy.Permissions = resolved.Snapshot.Permissions
	policyPath := filepath.Join(base, "policy.json")
	b, err = json.MarshalIndent(policy, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(policyPath, b, 0600); err != nil {
		t.Fatal(err)
	}
	preparedDir := filepath.Join(base, "prepared")
	var out, errout bytes.Buffer
	code, err := runHost([]string{"prepare", "--assignment", assignment, "--root", root, "--policy", policyPath, "--out", preparedDir}, &out, &errout)
	if err != nil || code != 0 {
		t.Fatalf("prepare CLI failed code=%d err=%v stderr=%s", code, err, errout.String())
	}
	p, err := hostbridge.LoadPrepared(preparedDir)
	if err != nil {
		t.Fatal(err)
	}
	return base, preparedDir, policyPath, p
}

func sha256Sum(b []byte) []byte { sum := sha256.Sum256(b); return sum[:] }

func TestHostReplayValidateOfflineAndImmutable(t *testing.T) {
	base, preparedDir, _, p := prepareHostCLIInput(t)
	for _, name := range []string{"PATH", "ANTHROPIC_API_KEY", "CLAUDE_CODE_OAUTH_TOKEN", "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY"} {
		old, ok := os.LookupEnv(name)
		if err := os.Unsetenv(name); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if ok {
				_ = os.Setenv(name, old)
			} else {
				_ = os.Unsetenv(name)
			}
		})
	}
	manifestDir := filepath.Join(base, "manifest")
	if err := os.MkdirAll(filepath.Join(manifestDir, "frames"), 0700); err != nil {
		t.Fatal(err)
	}
	// The fixture is copied from the repository into the test's temporary tree.
	_, source, _, _ := runtime.Caller(0)
	fixture := filepath.Join(filepath.Dir(source), "..", "hostbridge", "testdata", "claude", "success.jsonl")
	frame, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	var frameAssets []hostbridge.Asset
	for i, line := range bytes.Split(bytes.TrimSpace(frame), []byte("\n")) {
		path := filepath.Join(manifestDir, "frames", fmt.Sprintf("claude-%02d.json", i))
		if err = os.WriteFile(path, line, 0600); err != nil {
			t.Fatal(err)
		}
		frameAssets = append(frameAssets, hostbridge.Asset{Path: filepath.ToSlash(filepath.Join("frames", filepath.Base(path))), SHA256: hostbridge.Hash(line)})
	}
	for _, asset := range p.Sources {
		data, err := os.ReadFile(filepath.Join(preparedDir, filepath.FromSlash(asset.Path)))
		if err != nil {
			t.Fatal(err)
		}
		dest := filepath.Join(manifestDir, filepath.FromSlash(asset.Path))
		if err = os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(dest, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	manifest := hostbridge.ReplayManifest{Version: hostbridge.Version, Prepared: p, Frames: frameAssets}
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(manifestDir, "replay.json")
	if err = os.WriteFile(manifestPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	outDir := filepath.Join(base, "replayed")
	var stdout, stderr bytes.Buffer
	code, err := runHost([]string{"replay", "--manifest", manifestPath, "--out", outDir}, &stdout, &stderr)
	if err != nil || code != 0 {
		t.Fatalf("replay failed code=%d err=%v stderr=%s", code, err, stderr.String())
	}
	before := map[string][]byte{}
	if err = filepath.WalkDir(outDir, func(path string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			return nil
		}
		rel, e := filepath.Rel(outDir, path)
		if e != nil {
			return e
		}
		b, e := os.ReadFile(path)
		if e == nil {
			before[rel] = b
		}
		return e
	}); err != nil {
		t.Fatal(err)
	}
	var validOut, validErr bytes.Buffer
	code, err = runHost([]string{"validate", outDir}, &validOut, &validErr)
	if err != nil || code != 0 {
		t.Fatalf("validate failed code=%d err=%v stderr=%s", code, err, validErr.String())
	}
	for rel, want := range before {
		got, e := os.ReadFile(filepath.Join(outDir, rel))
		if e != nil || !bytes.Equal(got, want) {
			t.Fatalf("validate mutated %s: %v", rel, e)
		}
	}
	var againOut, againErr bytes.Buffer
	code, err = runHost([]string{"replay", "--manifest", manifestPath, "--out", outDir}, &againOut, &againErr)
	if err == nil && code == 0 {
		t.Fatal("replay overwrote immutable output")
	}
	for rel, want := range before {
		got, e := os.ReadFile(filepath.Join(outDir, rel))
		if e != nil || !bytes.Equal(got, want) {
			t.Fatalf("failed replay changed %s: %v", rel, e)
		}
	}
}
