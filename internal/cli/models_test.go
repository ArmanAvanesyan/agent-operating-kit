package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const modelManifestFixture = "../../docs/research/model-catalog-manifest.json"
const modelPolicyFixture = "../../docs/research/model-catalog-policy.json"

func runModelsTest(t *testing.T, expected int, args ...string) []byte {
	t.Helper()
	var out, stderr bytes.Buffer
	if got := Run(args, &out, &stderr); got != expected {
		t.Fatalf("%v exit=%d want=%d\n%s\n%s", args, got, expected, out.String(), stderr.String())
	}
	return append([]byte(nil), out.Bytes()...)
}

func importModelsCLI(t *testing.T) string {
	t.Helper()
	out := filepath.Join(modelTempDir(t), "catalog")
	runModelsTest(t, 0, "models", "import", "--manifest", modelManifestFixture, "--policy", modelPolicyFixture, "--out", out, "--json")
	return out
}

func modelTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestModelsCLIImportValidateInspectDiffRefresh(t *testing.T) {
	out := importModelsCLI(t)
	before := workflowTreeHashes(t, out)
	runModelsTest(t, 0, "models", "validate", out, "--policy", modelPolicyFixture, "--json")
	first := runModelsTest(t, 0, "models", "inspect", out, "--as-of", "2026-10-15T12:00:00Z", "--json")
	second := runModelsTest(t, 0, "models", "inspect", out, "--as-of", "2026-10-15T12:00:00Z", "--json")
	if !bytes.Equal(first, second) {
		t.Fatal("inspection is not deterministic")
	}
	var inspected map[string]any
	if err := json.Unmarshal(first, &inspected); err != nil {
		t.Fatal(err)
	}
	if inspected["authorizationGranted"] != false || inspected["executionReady"] != false || inspected["probeGate"] != "pending-p6" {
		t.Fatalf("unsafe inspection claim: %s", first)
	}
	diff := runModelsTest(t, 0, "models", "diff", out, out, "--as-of", "2026-10-15T12:00:00Z", "--json")
	var compared struct{ Changes []any }
	if err := json.Unmarshal(diff, &compared); err != nil || len(compared.Changes) != 0 {
		t.Fatalf("self diff %s: %v", diff, err)
	}
	refreshed := filepath.Join(modelTempDir(t), "refreshed")
	runModelsTest(t, 0, "models", "refresh", out, "--manifest", modelManifestFixture, "--policy", modelPolicyFixture, "--out", refreshed, "--json")
	runModelsTest(t, 0, "models", "validate", refreshed, "--policy", modelPolicyFixture, "--json")
	if _, err := os.Stat(filepath.Join(refreshed, "refresh-report.json")); err != nil {
		t.Fatal(err)
	}
	if got := workflowTreeHashes(t, out); !hashMapsEqual(before, got) {
		t.Fatal("read/refresh changed original bundle")
	}
	// A second import cannot overwrite a completed bundle.
	runModelsTest(t, 1, "models", "import", "--manifest", modelManifestFixture, "--policy", modelPolicyFixture, "--out", out, "--json")
	if !hashMapsEqual(before, workflowTreeHashes(t, out)) {
		t.Fatal("overwrite refusal changed bundle")
	}
}

func hashMapsEqual(a, b map[string][32]byte) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func TestModelsCheckAndCatalogAwareResolution(t *testing.T) {
	bundle := importModelsCLI(t)
	profile := "../agentresolve/testdata/corpus/execution-profile.json"
	// Locate the exact fixture name without depending on its display identifier.
	entries, err := os.ReadDir("../agentresolve/testdata/corpus")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		p := filepath.Join("../agentresolve/testdata/corpus", entry.Name())
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		var d map[string]any
		if json.Unmarshal(b, &d) == nil && d["kind"] == "ExecutionProfile" {
			profile = p
			break
		}
	}
	raw := runModelsTest(t, 0, "models", "check", "--catalog", bundle, "--policy", modelPolicyFixture, "--profile", profile, "--as-of", "2026-10-15T12:00:00Z", "--json")
	var check map[string]any
	if err := json.Unmarshal(raw, &check); err != nil {
		t.Fatal(err)
	}
	if check["compatibility"] != "unknown" {
		t.Fatalf("historical research promoted into runtime compatibility: %s", raw)
	}
	out := filepath.Join(modelTempDir(t), "resolution")
	resolved := runModelsTest(t, 1, "agent", "resolve", "../agentresolve/testdata/corpus/assignment.json", "--out", out, "--catalog", bundle, "--catalog-policy", modelPolicyFixture, "--as-of", "2026-10-15T12:00:00Z", "--json")
	var report map[string]any
	if err := json.Unmarshal(resolved, &report); err != nil {
		t.Fatal(err)
	}
	if report["status"] != "blocked" {
		t.Fatalf("catalog granted readiness: %s", resolved)
	}
	snapshot := report["snapshot"].(map[string]any)
	if snapshot["catalogEvidence"] == nil {
		t.Fatal("catalog evidence not bound into snapshot")
	}
}

func TestModelsMisuseAndGroupedResolverOptions(t *testing.T) {
	for _, args := range [][]string{
		{"models"}, {"models", "select"}, {"models", "import"},
		{"models", "inspect", "x"}, {"models", "inspect", "x", "--as-of", "bad"},
		{"models", "inspect", "x", "--as-of", "2026-10-15T12:00:00Z", "--as-of", "2026-10-15T12:00:00Z"},
		{"models", "validate", "x", "--policy", "p", "--out", "y"},
		{"models", "diff", "x", "--as-of", "2026-10-15T12:00:00Z"},
		{"agent", "resolve", "a", "--out", "b", "--catalog", "c"},
		{"agent", "resolve", "a", "--out", "b", "--catalog-policy", "c"},
		{"agent", "resolve", "a", "--out", "b", "--as-of", "2026-10-15T12:00:00Z"},
	} {
		runModelsTest(t, 2, args...)
	}
	for _, args := range [][]string{{"models", "--help"}, {"models", "import", "--help"}} {
		runModelsTest(t, 0, args...)
	}
}

func TestModelsRejectTamperedProjection(t *testing.T) {
	bundle := importModelsCLI(t)
	p := filepath.Join(bundle, "catalog.json")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var c map[string]any
	if err := json.Unmarshal(b, &c); err != nil {
		t.Fatal(err)
	}
	entry := c["entries"].([]any)[0].(map[string]any)
	entry["name"] = "forged model"
	b, err = json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(p, b, 0600); err != nil {
		t.Fatal(err)
	}
	output := runModelsTest(t, 1, "models", "validate", bundle, "--policy", modelPolicyFixture, "--json")
	if !strings.Contains(string(output), "projection") {
		t.Fatalf("no projection diagnostic: %s", output)
	}
}
