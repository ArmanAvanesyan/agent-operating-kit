package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/runtime/records"
)

func runRuntimeAcceptance(t *testing.T, want int, args ...string) (string, string) {
	t.Helper()
	var out, stderr bytes.Buffer
	got := Run(append([]string{"runtime"}, args...), &out, &stderr)
	if got != want {
		t.Fatalf("runtime %v exit=%d want=%d\nstdout=%s\nstderr=%s", args, got, want, out.String(), stderr.String())
	}
	return out.String(), stderr.String()
}

func runtimeCLIFixture(t *testing.T) (string, string, string) {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(base, "repository")
	staging := filepath.Join(base, "staging")
	if err = os.Mkdir(repo, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(staging, 0700); err != nil {
		t.Fatal(err)
	}
	usagePolicySource := filepath.Join("..", "usage", "testdata", "policy.json")
	usagePolicyBytes, err := os.ReadFile(usagePolicySource)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(base, "usage-policy.json"), usagePolicyBytes, 0600); err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256(usagePolicyBytes)
	policy := records.Policy{Version: records.Version, Authority: "runtime-cli-test", RepositoryRoots: []string{repo}, StagingRoot: staging, AccountingPath: filepath.Join(base, "accounting.sqlite"), UsagePolicy: records.Asset{Path: "usage-policy.json", SHA256: hex.EncodeToString(h[:])}, Hosts: []records.HostRoute{}, AllowDispatch: false, MaxConcurrent: 2, LeaseSeconds: 10, HeartbeatSeconds: 1, MaxAttempts: 3, ReconciliationIssuers: map[string]string{}}
	policyBytes, err := json.MarshalIndent(policy, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	policyPath := filepath.Join(base, "runtime-policy.json")
	if err = os.WriteFile(policyPath, append(policyBytes, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	return base, policyPath, filepath.Join(base, "runtime-state")
}

func TestRuntimeInitStatusValidateAndBlockedDispatch(t *testing.T) {
	_, policy, state := runtimeCLIFixture(t)
	if out, _ := runRuntimeAcceptance(t, 0, "init", "--policy", policy, "--state", state, "--json"); !strings.Contains(out, `"status": "valid"`) {
		t.Fatalf("init report unexpected: %s", out)
	}
	if out, _ := runRuntimeAcceptance(t, 0, "validate", state, "--json"); !strings.Contains(out, `"accepted": false`) {
		t.Fatalf("validate report unexpected: %s", out)
	}
	if out, _ := runRuntimeAcceptance(t, 0, "status", state, "--json"); !strings.Contains(out, "offline-core") {
		t.Fatalf("status report unexpected: %s", out)
	}
	out, _ := runRuntimeAcceptance(t, 1, "dispatch", state, "--run", "fixture-run", "--once", "--json")
	var report records.Report
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatal(err)
	}
	if report.Status != "blocked" || report.Accepted || report.CodexLiveGate != "blocked-p6-control-proofs-and-live-conformance" {
		t.Fatalf("dispatch bypassed live gate: %+v", report)
	}
}

func TestRuntimeReplayNeedsNoPathOrCredentialAndOutputIsImmutable(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate runtime fixture")
	}
	scenario := filepath.Join(filepath.Dir(source), "..", "runtime", "testdata", "offline", "replay.json")
	base := t.TempDir()
	outDir := filepath.Join(base, "replay-output")
	t.Setenv("PATH", "")
	for _, name := range []string{"OPENAI_API_KEY", "ANTHROPIC_API_KEY", "CODEX_API_KEY", "CLAUDE_CODE_OAUTH_TOKEN", "AZURE_OPENAI_API_KEY"} {
		t.Setenv(name, "")
	}
	out, _ := runRuntimeAcceptance(t, 0, "replay", "--scenario", scenario, "--as-of", "2026-10-23T12:00:19Z", "--out", outDir, "--json")
	var report records.Report
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatal(err)
	}
	if report.Accepted || report.RuntimeGate != "synthetic-replay-only" {
		t.Fatalf("replay report crossed readiness boundary: %+v", report)
	}
	before := map[string][]byte{}
	for _, name := range []string{"report.json", "eventtrace.json"} {
		b, err := os.ReadFile(filepath.Join(outDir, name))
		if err != nil {
			t.Fatal(err)
		}
		before[name] = b
	}
	runRuntimeAcceptance(t, 1, "replay", "--scenario", scenario, "--as-of", "2026-10-23T12:00:19Z", "--out", outDir, "--json")
	for name, want := range before {
		got, err := os.ReadFile(filepath.Join(outDir, name))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("immutable replay output changed: %s", name)
		}
	}
}

func TestRuntimeFlagErrorsAndUnsafeReplayInputs(t *testing.T) {
	_, policy, state := runtimeCLIFixture(t)
	runRuntimeAcceptance(t, 2, "dispatch", state, "--run", "fixture-run")
	runRuntimeAcceptance(t, 2, "status", state, "--unknown")
	runRuntimeAcceptance(t, 2, "dispatch", state, "--run", "fixture-run", "--once", "--once")
	runRuntimeAcceptance(t, 0, "init", "--policy", policy, "--state", state, "--json")

	_, source, _, _ := runtime.Caller(0)
	scenario := filepath.Join(filepath.Dir(source), "..", "runtime", "testdata", "offline", "replay.json")
	fixture, err := os.ReadFile(scenario)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err = json.Unmarshal(fixture, &document); err != nil {
		t.Fatal(err)
	}
	unsupported := filepath.Join(t.TempDir(), "unsupported.json")
	document["version"] = "aok-runtime/v999"
	unsupportedBytes, _ := json.MarshalIndent(document, "", "  ")
	if err = os.WriteFile(unsupported, unsupportedBytes, 0600); err != nil {
		t.Fatal(err)
	}
	runRuntimeAcceptance(t, 1, "replay", "--scenario", unsupported, "--as-of", "2026-10-23T12:00:19Z", "--out", filepath.Join(t.TempDir(), "unsupported-out"), "--json")
	runRuntimeAcceptance(t, 1, "replay", "--scenario", scenario, "--as-of", "2026-10-23T12:00:19Z", "--out", filepath.Dir(scenario), "--json")
}
