package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// TestFoundationsCLI exercises the shipped executable boundary. It deliberately
// gives the binary an empty PATH and an allow-listed environment: these commands
// must operate on local files and must not depend on Python, host tools, or secrets.
func TestFoundationsCLI(t *testing.T) {
	repo := repositoryRoot(t)
	bin := filepath.Join(t.TempDir(), "aok")
	build := exec.Command("go", "build", "-o", bin, "./cmd/aok")
	build.Dir = repo
	// Compilation uses the developer toolchain; only the resulting binary is
	// executed without PATH, host tools, or ambient credentials.
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build aok: %v\n%s", err, out)
	}
	home := t.TempDir()
	work, err := os.MkdirTemp(repo, ".aok-foundations-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(work) })
	root := func(parts ...string) string { return filepath.Join(append([]string{repo}, parts...)...) }
	run := func(wd string, want int, args ...string) map[string]any {
		t.Helper()
		cmd := exec.Command(bin, args...)
		cmd.Dir = wd
		cmd.Env = testEnvironment(t, home, repo)
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		got := 0
		if err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				got = ee.ExitCode()
			} else {
				t.Fatalf("run %v: %v", args, err)
			}
		}
		if got != want {
			t.Fatalf("%v exit=%d want=%d\nstdout: %s\nstderr: %s", args, got, want, stdout.String(), stderr.String())
		}
		var result map[string]any
		if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
			t.Fatalf("%v did not emit JSON: %v\n%s", args, err, stdout.String())
		}
		return result
	}

	// Canonical validation succeeds; resolution emits a useful static snapshot
	// but remains blocked because runtime enforcement has not been verified.
	agentCorpus := root("internal", "agentresolve", "testdata", "corpus")
	if r := run(repo, 0, "agent", "validate", agentCorpus, "--json"); r["status"] != "valid" {
		t.Fatalf("agent validate report=%v", r)
	}
	resolveOut := filepath.Join(work, "resolution")
	r := run(repo, 1, "agent", "resolve", filepath.Join(agentCorpus, "assignment.json"), "--root", agentCorpus, "--out", resolveOut, "--json")
	if r["status"] != "blocked" {
		t.Fatalf("resolve report=%v", r)
	}
	assertJSONFile(t, filepath.Join(resolveOut, "resolution.json"), "blocked")

	// Planning readiness is evaluated against explicit, fixed timestamps. The
	// same corpus is fresh at noon on the 16th and expired on the 18th.
	planSource := root("internal", "planning", "testdata", "corpus")
	planCorpus := filepath.Join(work, "planning-corpus")
	copyTreeCLI(t, planSource, planCorpus)
	if r := run(repo, 0, "planning", "validate", planCorpus, "--json"); r["status"] != "valid" {
		t.Fatalf("planning validate report=%v", r)
	}
	plan := filepath.Join(planCorpus, "plan.json")
	evidence := filepath.Join(planCorpus, "evidence.json")
	readyFresh := run(repo, 0, "planning", "ready", plan, "--evidence", evidence, "--root", planCorpus, "--as-of", "2026-10-16T12:00:00Z", "--json")
	if readyFresh["status"] != "valid" {
		t.Fatalf("fresh readiness report=%v", readyFresh)
	}
	readyStale := run(repo, 1, "planning", "ready", plan, "--evidence", evidence, "--root", planCorpus, "--as-of", "2026-10-18T00:00:00Z", "--json")
	if readyStale["status"] != "blocked" {
		t.Fatalf("stale readiness report=%v", readyStale)
	}

	testNativeCLIAllTargets(t, repo, work, root, run)
}

type cliRun func(wd string, want int, args ...string) map[string]any

func testNativeCLIAllTargets(t *testing.T, repo, work string, root func(...string) string, run cliRun) {
	t.Helper()
	type targetCase struct {
		target, scope string
		files         map[string][]byte
	}
	read := func(path string) []byte {
		t.Helper()
		b, err := os.ReadFile(root("internal", "nativeadapters", path))
		if err != nil {
			t.Fatalf("read fixture %s: %v", path, err)
		}
		return b
	}
	cases := []targetCase{
		{"agy", "project", map[string][]byte{".agents/agents/aok-probe/agent.md": read("agy/testdata/probe.md")}},
		{"claude-code", "project", map[string][]byte{".claude/agents/reviewer.md": []byte("---\nname: aok-reviewer\ndescription: Synthetic review agent\nmodel: sonnet\ncustomField: retained\n---\nReview only the supplied synthetic content.\n")}},
		{"codex", "project", map[string][]byte{".codex/agents/aok-probe.toml": read("codex/testdata/probe.toml")}},
		{"cursor", "project", map[string][]byte{".cursor/agents/reviewer.md": read("cursor/testdata/reviewer.md")}},
		{"gemini", "project", map[string][]byte{".gemini/agents/security-auditor.md": read("gemini/testdata/security-auditor.md")}},
		{"hermes-agent", "profile", map[string][]byte{"config.yaml": read("hermes/testdata/config.yaml"), "SOUL.md": read("hermes/testdata/SOUL.md"), "profile.yaml": read("hermes/testdata/profile.yaml")}},
		{"openclaw", "isolated-home", map[string][]byte{".openclaw/openclaw.json": []byte(`{"agents":{"entries":{"main":{"identity":{"name":"Main"},"model":"openai/gpt-5.4-mini"}}}}`), ".openclaw/workspace/AGENTS.md": []byte("Synthetic isolated-home instruction content.\n")}},
		{"opencode", "project", map[string][]byte{".opencode/agents/aok-probe.md": read("opencode/testdata/probe.md")}},
	}
	for _, tc := range cases {
		t.Run(tc.target, func(t *testing.T) {
			input := filepath.Join(work, "native-"+tc.target)
			if err := os.MkdirAll(input, 0o700); err != nil {
				t.Fatal(err)
			}
			manifest := struct {
				Version string `json:"version"`
				Files   []struct {
					Path   string `json:"path"`
					Format string `json:"format"`
				} `json:"files"`
			}{Version: "aok-native-input/v1"}
			for p, b := range tc.files {
				full := filepath.Join(input, filepath.FromSlash(p))
				if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(full, b, 0o600); err != nil {
					t.Fatal(err)
				}
				format := "markdown"
				switch filepath.Ext(p) {
				case ".toml":
					format = "toml"
				case ".yaml", ".yml":
					format = "yaml"
				case ".json":
					format = "json"
				}
				manifest.Files = append(manifest.Files, struct {
					Path   string `json:"path"`
					Format string `json:"format"`
				}{Path: p, Format: format})
			}
			mb, err := json.MarshalIndent(manifest, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(input, "aok-native-input.json"), append(mb, '\n'), 0o600); err != nil {
				t.Fatal(err)
			}

			inspect := run(repo, 1, "agent", "native", "inspect", "--target", tc.target, "--scope", tc.scope, "--input", input, "--require", "agent.permissions.network.mode", "--json")
			if inspect["status"] != "blocked" {
				t.Fatalf("required network control should block: %v", inspect)
			}
			importOut := filepath.Join(work, "import-"+tc.target)
			imported := run(repo, 1, "agent", "native", "import", "--target", tc.target, "--scope", tc.scope, "--input", input, "--out", importOut, "--json")
			if imported["status"] != "blocked" {
				t.Fatalf("import should remain draft-incomplete: %v", imported)
			}
			assertJSONFile(t, filepath.Join(importOut, "aok-native.json"), "")
			assertJSONFile(t, filepath.Join(importOut, "report.json"), "blocked")

			exportOut := filepath.Join(work, "export-"+tc.target)
			exported := run(repo, 1, "agent", "native", "export", "--target", tc.target, "--scope", tc.scope, "--input", filepath.Join(importOut, "aok-native.json"), "--source", input, "--out", exportOut, "--json")
			if exported["status"] != "blocked" {
				t.Fatalf("export should remain blocked: %v", exported)
			}
			assertJSONFile(t, filepath.Join(exportOut, "report.json"), "blocked")
			assertJSONFile(t, filepath.Join(exportOut, "aok-native.json"), "")
			if len(tc.files) > 0 {
				for p := range tc.files {
					if _, err := os.Stat(filepath.Join(exportOut, filepath.FromSlash(p))); err != nil {
						t.Fatalf("exported native file %s missing: %v", p, err)
					}
				}
			}
			compared := run(repo, 0, "agent", "native", "compare", "--target", tc.target, "--scope", tc.scope, "--input", exportOut, "--against", filepath.Join(exportOut, "aok-native.json"), "--json")
			if compared["status"] != "valid" || compared["mappingStatus"] != "compatible" {
				t.Fatalf("round-trip compare should be valid: %v", compared)
			}
		})
	}
}

func assertJSONFile(t *testing.T, path, status string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected output file %s: %v", path, err)
	}
	var v map[string]any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("output %s is not JSON: %v", path, err)
	}
	if status != "" && v["status"] != status {
		t.Fatalf("output %s status=%v want=%s", path, v["status"], status)
	}
}

func testEnvironment(t *testing.T, home, repo string) []string {
	t.Helper()
	env := []string{"PATH=", "HOME=" + home, "TMPDIR=" + t.TempDir(), "TMP=" + t.TempDir(), "TEMP=" + t.TempDir(), "AOK_ROOT=" + repo}
	if runtime.GOOS == "windows" {
		for _, key := range []string{"SystemRoot", "WINDIR", "PATHEXT", "ComSpec"} {
			if value := os.Getenv(key); value != "" {
				env = append(env, key+"="+value)
			}
		}
	}
	return env
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test source")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("repository root: %v", err)
	}
	return root
}

func copyTreeCLI(t *testing.T, src, dst string) {
	t.Helper()
	if err := filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		to := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(to, 0o700)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(to, b, 0o600)
	}); err != nil {
		t.Fatal(fmt.Errorf("copy planning fixture: %w", err))
	}
}
