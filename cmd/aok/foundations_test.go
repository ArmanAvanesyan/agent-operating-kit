package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
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
	binaryBytes, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("built AOK SHA256=%x; runtime PATH empty", sha256.Sum256(binaryBytes))
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
	// Workflow records use the same shipped offline boundary and never grant
	// authority or execution readiness, even when every declared check passes.
	workflowSource := root("internal", "workflowcheck", "testdata", "corpus")
	workflowCorpus := filepath.Join(work, "workflow-corpus")
	copyTreeCLI(t, workflowSource, workflowCorpus)
	wr := run(repo, 0, "workflow", "validate", workflowCorpus, "--json")
	if wr["status"] != "valid" || wr["authorizationGranted"] != false || wr["executionReady"] != false {
		t.Fatalf("workflow validation report=%v", wr)
	}
	// A workflow-domain semantic defect must reach all commands that accept
	// these kinds, rather than passing as merely schema-valid JSON.
	entries, err := os.ReadDir(workflowCorpus)
	if err != nil {
		t.Fatal(err)
	}
	corrupted := false
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		p := filepath.Join(workflowCorpus, entry.Name())
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		var doc map[string]any
		if json.Unmarshal(b, &doc) != nil || doc["kind"] != "RunRecord" {
			continue
		}
		doc["attemptId"] = "foreign-attempt"
		b, err = json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(p, b, 0600); err != nil {
			t.Fatal(err)
		}
		corrupted = true
		break
	}
	if !corrupted {
		t.Fatal("workflow fixture missing run record")
	}
	for _, domain := range []string{"workflow", "agent", "planning"} {
		if r := run(repo, 1, domain, "validate", workflowCorpus, "--json"); r["status"] != "invalid" {
			t.Fatalf("%s skipped workflow semantics: %v", domain, r)
		}
	}
	// Every catalog command is offline at the shipped executable boundary.
	modelManifest := root("docs", "research", "model-catalog-manifest.json")
	modelPolicy := root("docs", "research", "model-catalog-policy.json")
	modelBundle := filepath.Join(work, "model-catalog")
	if r := run(repo, 0, "models", "import", "--manifest", modelManifest, "--policy", modelPolicy, "--out", modelBundle, "--json"); r["status"] != "valid" {
		t.Fatalf("catalog import report=%v", r)
	}
	run(repo, 0, "models", "validate", modelBundle, "--policy", modelPolicy, "--json")
	if r := run(repo, 0, "models", "inspect", modelBundle, "--as-of", "2026-10-23T12:00:00Z", "--json"); r["authorizationGranted"] != false || r["executionReady"] != false || r["probeGate"] != "pending-p6" {
		t.Fatalf("catalog promoted research evidence: %v", r)
	}
	if r := run(repo, 0, "models", "diff", modelBundle, modelBundle, "--as-of", "2026-10-23T12:00:00Z", "--json"); len(r["changes"].([]any)) != 0 {
		t.Fatalf("catalog self diff: %v", r)
	}
	refreshedBundle := filepath.Join(work, "model-catalog-refreshed")
	run(repo, 0, "models", "refresh", modelBundle, "--manifest", modelManifest, "--policy", modelPolicy, "--out", refreshedBundle, "--json")
	run(repo, 0, "models", "validate", refreshedBundle, "--policy", modelPolicy, "--json")
	run(repo, 0, "models", "check", "--catalog", modelBundle, "--policy", modelPolicy, "--profile", root("internal", "planning", "testdata", "corpus", "execution-profile.json"), "--as-of", "2026-10-23T12:00:00Z", "--json")
	usageExport := root("internal", "usage", "testdata", "export.json")
	usagePolicy := root("internal", "usage", "testdata", "policy.json")
	if r := run(repo, 0, "usage", "validate", usageExport, "--policy", usagePolicy, "--json"); r["authorizationGranted"] != false || r["executionReady"] != false || r["runtimeGate"] != "pending-p6-p7" {
		t.Fatalf("usage promoted runtime readiness: %v", r)
	}
	run(repo, 0, "usage", "summary", usageExport, "--policy", usagePolicy, "--as-of", "2026-10-23T12:00:30Z", "--json")
	if r := run(repo, 0, "usage", "collectors", "--json"); len(r["collectors"].([]any)) != 8 {
		t.Fatalf("collector support inventory: %v", r)
	}
	run(repo, 0, "usage", "capacity", usageExport, "--policy", usagePolicy, "--catalog", modelBundle, "--catalog-policy", modelPolicy, "--profile", root("internal", "planning", "testdata", "corpus", "execution-profile.json"), "--as-of", "2026-10-23T12:00:30Z", "--json")
	agentCorpus := root("internal", "agentresolve", "testdata", "corpus")
	if r := run(repo, 0, "agent", "validate", agentCorpus, "--json"); r["status"] != "valid" {
		t.Fatalf("agent validate report=%v", r)
	}
	resolveOut := filepath.Join(work, "resolution")
	r := run(repo, 1, "agent", "resolve", filepath.Join(agentCorpus, "assignment.json"), "--root", agentCorpus, "--out", resolveOut, "--json")
	if r["status"] != "blocked" {
		t.Fatalf("resolve report=%v", r)
	}
	catalogResolved := run(repo, 1, "agent", "resolve", filepath.Join(agentCorpus, "assignment.json"), "--root", agentCorpus, "--out", filepath.Join(work, "catalog-resolution"), "--catalog", modelBundle, "--catalog-policy", modelPolicy, "--as-of", "2026-10-23T12:00:00Z", "--json")
	if catalogResolved["status"] != "blocked" || catalogResolved["snapshot"].(map[string]any)["catalogEvidence"] == nil {
		t.Fatalf("catalog resolution missing blocked evidence: %v", catalogResolved)
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
		{"codex", "project-registered", map[string][]byte{
			".codex/config.toml":           []byte("[agents.aok-probe]\ndescription = \"Synthetic read-only discovery fixture\"\nconfig_file = \"agents/aok-probe.toml\"\n"),
			".codex/agents/aok-probe.toml": read("codex/testdata/probe.toml"),
		}},
		{"claude-code", "plugin", map[string][]byte{"agents/reviewer.md": []byte("---\nname: aok-reviewer\ndescription: Synthetic plugin projection\nmodel: sonnet\n---\nReview only synthetic content.\n")}},
		{"cursor", "project", map[string][]byte{".cursor/agents/reviewer.md": read("cursor/testdata/reviewer.md")}},
		{"gemini", "project", map[string][]byte{".gemini/agents/security-auditor.md": read("gemini/testdata/security-auditor.md")}},
		{"hermes-agent", "profile", map[string][]byte{"config.yaml": read("hermes/testdata/config.yaml"), "SOUL.md": read("hermes/testdata/SOUL.md"), "profile.yaml": read("hermes/testdata/profile.yaml")}},
		{"openclaw", "isolated-home", map[string][]byte{".openclaw/openclaw.json": []byte(`{"agents":{"entries":{"main":{"identity":{"name":"Main"},"model":"openai/gpt-5.4-mini"}}}}`), ".openclaw/workspace/AGENTS.md": []byte("Synthetic isolated-home instruction content.\n")}},
		{"opencode", "project", map[string][]byte{".opencode/agents/aok-probe.md": read("opencode/testdata/probe.md")}},
	}
	for _, tc := range cases {
		t.Run(tc.target+"-"+tc.scope, func(t *testing.T) {
			key := tc.target + "-" + tc.scope
			wantHost := "verified"
			if tc.target == "codex" && tc.scope == "project" || tc.target == "claude-code" && tc.scope == "plugin" {
				wantHost = "not-run"
			}
			check := func(operation string, report map[string]any) {
				t.Helper()
				wantStatus := "blocked"
				switch operation {
				case "inspect", "compare":
					wantStatus = "valid"
				case "source-drift-export", "source-drift-import":
					wantStatus = "invalid"
				}
				if report["status"] != wantStatus {
					t.Fatalf("%s status: %v", operation, report)
				}
				host, ok := report["hostLoading"].(map[string]any)
				if !ok || host["status"] != wantHost || report["runtimeEnforcement"] != "unverified" {
					t.Fatalf("%s scope/runtime boundary: %v", operation, report)
				}
				t.Logf("%s: status=%v host=%v runtime=%v mapping=%v", operation, report["status"], host["status"], report["runtimeEnforcement"], report["mappingStatus"])
			}
			input := filepath.Join(work, "native-"+key)
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
			paths := make([]string, 0, len(tc.files))
			for p := range tc.files {
				paths = append(paths, p)
			}
			sort.Strings(paths)
			for _, p := range paths {
				b := tc.files[p]
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

			valid := run(repo, 0, "agent", "native", "inspect", "--target", tc.target, "--scope", tc.scope, "--input", input, "--json")
			check("inspect", valid)
			inspect := run(repo, 1, "agent", "native", "inspect", "--target", tc.target, "--scope", tc.scope, "--input", input, "--require", "agent.permissions.network.mode", "--json")
			check("required-unsupported", inspect)
			requireCLIDiagnostic(t, inspect, "native.control.required-unsupported")
			if inspect["status"] != "blocked" {
				t.Fatalf("required network control should block: %v", inspect)
			}
			importOut := filepath.Join(work, "import-"+key)
			imported := run(repo, 1, "agent", "native", "import", "--target", tc.target, "--scope", tc.scope, "--input", input, "--out", importOut, "--json")
			check("import", imported)
			if imported["status"] != "blocked" {
				t.Fatalf("import should remain draft-incomplete: %v", imported)
			}
			assertJSONFile(t, filepath.Join(importOut, "aok-native.json"), "")
			assertJSONFile(t, filepath.Join(importOut, "report.json"), "blocked")

			exportOut := filepath.Join(work, "export-"+key)
			exported := run(repo, 1, "agent", "native", "export", "--target", tc.target, "--scope", tc.scope, "--input", filepath.Join(importOut, "aok-native.json"), "--source", input, "--out", exportOut, "--json")
			check("export", exported)
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
			check("compare", compared)
			if compared["status"] != "valid" || compared["mappingStatus"] != "compatible" {
				t.Fatalf("round-trip compare should be valid: %v", compared)
			}

			// With a sidecar, re-import binds to rendered sources; losing the
			// sidecar must yield an explicitly incomplete fresh draft.
			sidecar := filepath.Join(exportOut, "aok-native.json")
			restored := run(repo, 1, "agent", "native", "import", "--target", tc.target, "--scope", tc.scope,
				"--input", exportOut, "--sidecar", sidecar, "--out", filepath.Join(work, "restore-"+key), "--json")
			check("sidecar-reimport", restored)
			reference := assertJSONFile(t, sidecar, "")
			recovered := assertJSONFile(t, filepath.Join(work, "restore-"+key, "aok-native.json"), "")
			digest, ok := reference["digest"].(string)
			if !ok || digest == "" || recovered["digest"] != digest {
				t.Fatalf("sidecar re-import lost lineage: reference=%v restored=%v", reference["digest"], recovered["digest"])
			}

			lost := run(repo, 1, "agent", "native", "import", "--target", tc.target, "--scope", tc.scope,
				"--input", exportOut, "--out", filepath.Join(work, "without-sidecar-"+key), "--json")
			check("sidecar-loss", lost)
			requireCLIDiagnostic(t, lost, "native.provenance.incomplete")
			fresh := assertJSONFile(t, filepath.Join(work, "without-sidecar-"+key, "aok-native.json"), "")
			if fresh["completeness"] != "draft-incomplete" || fresh["digest"] == digest {
				t.Fatalf("missing sidecar must produce a new incomplete lineage: %v", fresh)
			}

			// One changed source byte must block both re-import and export.
			source := filepath.Join(input, filepath.FromSlash(paths[0]))
			if err := os.WriteFile(source, append(append([]byte(nil), tc.files[paths[0]]...), '\n'), 0600); err != nil {
				t.Fatal(err)
			}
			conflict := run(repo, 1, "agent", "native", "export", "--target", tc.target, "--scope", tc.scope,
				"--input", filepath.Join(importOut, "aok-native.json"), "--source", input, "--out", filepath.Join(work, "drift-export-"+key), "--json")
			check("source-drift-export", conflict)
			requireCLIDiagnostic(t, conflict, "native.bundle.conflict")
			conflict = run(repo, 1, "agent", "native", "import", "--target", tc.target, "--scope", tc.scope,
				"--input", input, "--sidecar", filepath.Join(importOut, "aok-native.json"), "--out", filepath.Join(work, "drift-import-"+key), "--json")
			check("source-drift-import", conflict)
			requireCLIDiagnostic(t, conflict, "native.bundle.conflict")
		})
	}
}

func assertJSONFile(t *testing.T, path, status string) map[string]any {
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
	return v
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

func requireCLIDiagnostic(t *testing.T, report map[string]any, code string) {
	t.Helper()
	diagnostics, _ := report["diagnostics"].([]any)
	for _, value := range diagnostics {
		if diagnostic, ok := value.(map[string]any); ok && diagnostic["code"] == code {
			return
		}
	}
	t.Fatalf("missing diagnostic %s: %v", code, report)
}
