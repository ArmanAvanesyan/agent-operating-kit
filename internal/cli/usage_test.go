package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/modelcatalog"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage/collectors"
)

const usageFixtureRoot = "../usage/testdata"

func runUsageAcceptance(t *testing.T, want int, args ...string) []byte {
	t.Helper()
	var out, stderr bytes.Buffer
	got := Run(append([]string{"usage"}, args...), &out, &stderr)
	if got != want {
		t.Fatalf("usage %v exit=%d want=%d\nstdout=%s\nstderr=%s", args, got, want, out.String(), stderr.String())
	}
	return append([]byte(nil), out.Bytes()...)
}
func usageTempDir(t *testing.T) string {
	t.Helper()
	p, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func usageHash(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func writeUsageJSON(t *testing.T, path string, v any) []byte {
	t.Helper()
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		t.Fatal(e)
	}
	b = append(b, '\n')
	if e = os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(path, b, 0600); e != nil {
		t.Fatal(e)
	}
	return b
}

func TestUsageValidateSummaryAndCollectorSupport(t *testing.T) {
	export := filepath.Join(usageFixtureRoot, "export.json")
	policy := filepath.Join(usageFixtureRoot, "policy.json")
	raw := runUsageAcceptance(t, 0, "validate", export, "--policy", policy, "--json")
	var report usage.Report
	if e := json.Unmarshal(raw, &report); e != nil {
		t.Fatal(e)
	}
	if report.Status != "valid" || report.AuthorizationGranted || report.ExecutionReady {
		t.Fatalf("unsafe validation report: %s", raw)
	}
	args := []string{"summary", export, "--policy", policy, "--as-of", "2026-10-23T12:00:30Z", "--group-by", "host,model,pool", "--json"}
	first := runUsageAcceptance(t, 0, args...)
	second := runUsageAcceptance(t, 0, args...)
	if !bytes.Equal(first, second) {
		t.Fatal("summary is not deterministic for fixed input and as-of")
	}
	var summary usage.Summary
	if e := json.Unmarshal(first, &summary); e != nil {
		t.Fatal(e)
	}
	if summary.Status != "valid" || summary.AuthorizationGranted || summary.ExecutionReady || summary.AsOf != "2026-10-23T12:00:30Z" {
		t.Fatalf("unexpected summary: %s", first)
	}
	raw = runUsageAcceptance(t, 0, "collectors", "--json")
	var result struct {
		Report     usage.Report                `json:"report"`
		Collectors []usage.CollectorDescriptor `json:"collectors"`
	}
	if e := json.Unmarshal(raw, &result); e != nil {
		t.Fatal(e)
	}
	if len(result.Collectors) != 8 || result.Report.AuthorizationGranted || result.Report.ExecutionReady {
		t.Fatalf("collector inventory claims execution/readiness: %s", raw)
	}
	for _, d := range result.Collectors {
		if d.LiveGate != "pending" {
			t.Fatalf("%s live gate=%s", d.Host, d.LiveGate)
		}
	}
}

func TestUsageMisuseAndInvalidInputExitCodes(t *testing.T) {
	for _, args := range [][]string{
		{"validate"}, {"validate", "x", "--policy", "p", "--unknown"}, {"summary", "x", "--policy", "p"},
		{"summary", "x", "--policy", "p", "--as-of", "bad"}, {"summary", filepath.Join(usageFixtureRoot, "export.json"), "--policy", filepath.Join(usageFixtureRoot, "policy.json"), "--as-of", "2026-10-23T12:00:00Z", "--group-by", "host,host"},
		{"import", "--source", "codex"}, {"collectors", "--host", "not-a-host"},
	} {
		runUsageAcceptance(t, 2, args...)
	}
	runUsageAcceptance(t, 1, "validate", filepath.Join(usageFixtureRoot, "export.json"), "--policy", filepath.Join(usageFixtureRoot, "missing-policy.json"), "--json")
}

func TestUsageReplayVerifiesManifestAndWritesDeterministicImmutableBundle(t *testing.T) {
	base := usageTempDir(t)
	inputRoot := filepath.Join(base, "inputs")
	output1 := filepath.Join(base, "replay-one")
	output2 := filepath.Join(base, "replay-two")
	policyPath := filepath.Join(usageFixtureRoot, "policy.json")
	exportBytes, e := os.ReadFile(filepath.Join(usageFixtureRoot, "export.json"))
	if e != nil {
		t.Fatal(e)
	}
	inputPath := filepath.Join(inputRoot, "bundle", "usage.json")
	if e = os.MkdirAll(filepath.Dir(inputPath), 0700); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(inputPath, exportBytes, 0600); e != nil {
		t.Fatal(e)
	}
	manifestPath := filepath.Join(inputRoot, "manifest.json")
	manifest := struct {
		Version string        `json:"version"`
		Inputs  []usage.Asset `json:"inputs"`
	}{usage.Version, []usage.Asset{{Path: "bundle/usage.json", SHA256: usageHash(exportBytes)}}}
	writeUsageJSON(t, manifestPath, manifest)
	asof := "2026-10-23T12:00:30Z"
	baseArgs := []string{"replay", "--manifest", manifestPath, "--policy", policyPath, "--as-of", asof, "--json"}
	firstArgs := append(append([]string{}, baseArgs...), "--out", output1)
	first := runUsageAcceptance(t, 0, firstArgs...)
	secondArgs := append(append([]string{}, baseArgs...), "--out", output2)
	second := runUsageAcceptance(t, 0, secondArgs...)
	if !bytes.Equal(first, second) {
		t.Fatal("replay report changed across identical inputs")
	}
	for _, name := range []string{"usage.json", "replay-report.json"} {
		a, e := os.ReadFile(filepath.Join(output1, name))
		if e != nil {
			t.Fatal(e)
		}
		b, e := os.ReadFile(filepath.Join(output2, name))
		if e != nil {
			t.Fatal(e)
		}
		if !bytes.Equal(a, b) {
			t.Fatalf("replay output %s is not deterministic", name)
		}
	}
	before, e := os.ReadFile(filepath.Join(output1, "usage.json"))
	if e != nil {
		t.Fatal(e)
	}
	runUsageAcceptance(t, 1, firstArgs...)
	after, e := os.ReadFile(filepath.Join(output1, "usage.json"))
	if e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("failed overwrite changed prior replay output")
	}
	manifest.Inputs[0].SHA256 = strings.Repeat("0", 64)
	badManifest := filepath.Join(inputRoot, "bad-manifest.json")
	writeUsageJSON(t, badManifest, manifest)
	badOut := filepath.Join(base, "bad-replay")
	runUsageAcceptance(t, 1, "replay", "--manifest", badManifest, "--policy", policyPath, "--as-of", asof, "--out", badOut, "--json")
	if _, e = os.Stat(badOut); !os.IsNotExist(e) {
		t.Fatalf("bad hash created replay directory: %v", e)
	}
}

func TestUsageOfflineImportLoadSummaryDropsRawCanaries(t *testing.T) {
	base := usageTempDir(t)
	policyPath := filepath.Join(base, "policy.json")
	bindingPath := filepath.Join(base, "binding.json")
	inputPath := filepath.Join(base, "input.jsonl")
	out := filepath.Join(base, "imported")
	policy := usage.Policy{Version: usage.Version, Sources: []usage.SourceRule{{ID: "app-server-fixture", Host: "codex", HostVersion: "protocol-v2-fixture", AdapterVersion: collectors.AdapterVersion, AuthModes: []string{"chatgpt"}, Metrics: []string{"input-tokens", "cached-input-tokens", "output-tokens", "reasoning-tokens", "percentage"}, Authority: "openai/codex-app", MaxAgeSeconds: 60, CollectionAllowed: true}}, SafetyMargins: map[string]json.Number{}}
	policyBytes := writeUsageJSON(t, policyPath, policy)
	_ = policyBytes
	binding := usage.Binding{ID: "cli-import", PoolID: "explicit-account-pool", Provider: "openai", Service: "codex-app", Host: "codex", HostVersion: "protocol-v2-fixture", AuthMode: "chatgpt", SourceID: "app-server-fixture", StreamID: "fixture-stream", ConfigurationIDs: []string{"opaque-config"}, ConstraintIDs: []string{}, ConstraintsComplete: false}
	writeUsageJSON(t, bindingPath, binding)
	input, e := os.ReadFile("../usage/collectors/testdata/codex-app-server-v2.jsonl")
	if e != nil {
		t.Fatal(e)
	}
	input = bytes.Replace(input, []byte(`{"method":"thread/tokenUsage/updated"`), []byte(`{"prompt":"CANARY_PROMPT_7231","cwd":"/private/CANARY_PATH_2841","credential":"CANARY_SECRET_9652","method":"thread/tokenUsage/updated"`), 1)
	if e = os.WriteFile(inputPath, input, 0600); e != nil {
		t.Fatal(e)
	}
	received := "2026-10-23T12:00:00Z"
	runUsageAcceptance(t, 0, "import", "--source", "codex", "--input", inputPath, "--binding", bindingPath, "--policy", policyPath, "--received-at", received, "--out", out, "--json")
	imported, loadedPolicy, e := usage.Load(out, policyPath)
	if e != nil {
		t.Fatal(e)
	}
	if len(imported.Events) == 0 || len(imported.Quotas) == 0 {
		t.Fatalf("normalized import omitted observations: events=%d quotas=%d", len(imported.Events), len(imported.Quotas))
	}
	if imported.PolicySHA256 != usage.Hash(mustReadUsage(t, policyPath)) {
		t.Fatal("import did not pin exact supplied policy bytes")
	}
	summary := runUsageAcceptance(t, 0, "summary", filepath.Join(out, "usage.json"), "--policy", policyPath, "--as-of", "2026-10-23T12:00:30Z", "--json")
	var s usage.Summary
	if e = json.Unmarshal(summary, &s); e != nil {
		t.Fatal(e)
	}
	if s.Status != "valid" || s.AuthorizationGranted || s.ExecutionReady {
		t.Fatalf("unexpected imported summary: %s", summary)
	}
	raw, e := os.ReadFile(filepath.Join(out, "usage.json"))
	if e != nil {
		t.Fatal(e)
	}
	report, e := os.ReadFile(filepath.Join(out, "collection-report.json"))
	if e != nil {
		t.Fatal(e)
	}
	for _, secret := range []string{"CANARY_PROMPT_7231", "CANARY_PATH_2841", "CANARY_SECRET_9652"} {
		if bytes.Contains(raw, []byte(secret)) || bytes.Contains(report, []byte(secret)) {
			t.Fatalf("raw canary persisted: %s", secret)
		}
	}
	if e = usage.ValidateExport(imported, loadedPolicy); e != nil {
		t.Fatal(e)
	}
}
func mustReadUsage(t *testing.T, path string) []byte {
	t.Helper()
	b, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	return b
}

func TestUsageCapacityRemainsUnknownWithoutDemandAndBindsExactProfile(t *testing.T) {
	bundle := importModelsCLI(t)
	_, report := modelcatalog.Load(bundle, modelPolicyFixture)
	if report.Status != "valid" {
		t.Fatalf("catalog fixture failed: %+v", report)
	}
	profile := filepath.Join("..", "agentresolve", "testdata", "corpus", "execution-profile.json")
	export := filepath.Join(usageFixtureRoot, "export.json")
	policy := filepath.Join(usageFixtureRoot, "policy.json")
	asof := "2026-10-23T12:00:30Z"
	base := []string{"capacity", export, "--catalog", bundle, "--catalog-policy", modelPolicyFixture, "--profile", profile, "--policy", policy, "--as-of", asof, "--json"}
	raw := runUsageAcceptance(t, 0, base...)
	var capacity map[string]any
	if e := json.Unmarshal(raw, &capacity); e != nil {
		t.Fatal(e)
	}
	if capacity["disposition"] != "unknown" || capacity["executionReady"] != false || capacity["authorizationGranted"] != false {
		t.Fatalf("missing demand became a positive capacity decision: %s", raw)
	}
	runUsageAcceptance(t, 1, append(append([]string{}, base...), "--require-fit")...)
	configuration := "not-in-catalog"
	profileBytes, e := os.ReadFile(profile)
	if e != nil {
		t.Fatal(e)
	}
	demand := usage.Demand{Version: usage.Version, ConfigurationID: configuration, ProfileSHA256: strings.Repeat("0", 64), Source: "explicit-bound", ValidUntil: "2026-10-23T12:01:00Z", Amounts: []usage.Amount{{ConstraintID: strings.Repeat("a", 64), Generation: "generation", Unit: "requests", Amount: json.Number("1")}}}
	demandPath := filepath.Join(usageTempDir(t), "demand.json")
	writeUsageJSON(t, demandPath, demand)
	badProfile := append(append([]string{}, base...), "--demand", demandPath)
	runUsageAcceptance(t, 1, badProfile...)
	demand.ProfileSHA256 = usageHash(profileBytes)
	writeUsageJSON(t, demandPath, demand)
	badConfiguration := append(append([]string{}, base...), "--demand", demandPath)
	raw = runUsageAcceptance(t, 1, badConfiguration...)
	if !bytes.Contains(raw, []byte("configuration")) {
		t.Fatalf("unknown catalog configuration was not rejected clearly: %s", raw)
	}
}

func TestUsageCommandDoesNotClaimLiveCollection(t *testing.T) {
	base := usageTempDir(t)
	bindingPath := filepath.Join(base, "binding.json")
	writeUsageJSON(t, bindingPath, usage.Binding{ID: "b", PoolID: "p", Provider: "provider", Service: "service", Host: "codex", HostVersion: "v1", AuthMode: "test", SourceID: "source", StreamID: "stream", ConfigurationIDs: []string{}, ConstraintIDs: []string{}, ConstraintsComplete: false})
	out := filepath.Join(base, "collected")
	runUsageAcceptance(t, 1, "collect", "--host", "codex", "--binding", bindingPath, "--policy", filepath.Join(usageFixtureRoot, "policy.json"), "--out", out, "--json")
	if _, e := os.Stat(out); !os.IsNotExist(e) {
		t.Fatalf("pending collector transport wrote output: %v", e)
	}
	raw := runUsageAcceptance(t, 2, "collectors", "--host", "unknown", "--json")
	if len(raw) != 0 {
		t.Fatalf("unknown host should be CLI misuse without a report: %s", raw)
	}
}
