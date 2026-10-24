package cli

import (
	"bytes"
	"encoding/json"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/toolisolation"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHostIsolationCheckPreservesNativeGateAndImmutableOutput(t *testing.T) {
	root := t.TempDir()
	policy := filepath.Join(root, "policy.json")
	dest := filepath.Join(root, "report")
	p := toolisolation.Policy{Version: toolisolation.Version, Workspace: filepath.Join(root, "workspace"), Target: "pilot-output.txt", WorkerExecutable: filepath.Join(root, "worker"), WorkerSHA256: strings.Repeat("a", 64), TimeoutSeconds: 30}
	b, _ := json.Marshal(p)
	if err := os.WriteFile(policy, b, 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"isolation-check", "--policy", policy, "--as-of", "2026-10-24T12:00:00Z", "--out", dest, "--json"}
	var out, errout bytes.Buffer
	code, err := runHost(args, &out, &errout)
	if err != nil || code != 1 {
		t.Fatalf("%d %v %s", code, err, errout.String())
	}
	var r toolisolation.Inspection
	if err = json.Unmarshal(out.Bytes(), &r); err != nil {
		t.Fatal(err)
	}
	if !r.PolicyValid || r.AuthorizationGranted || r.ExecutionReady || r.ProductionEligible || r.NativeCodexTransport != "unsupported-pinned-0.162.1-tool-delegation" {
		t.Fatal("inspection laundered execution readiness")
	}
	before, _ := os.ReadFile(filepath.Join(dest, "tool-isolation-report.json"))
	out.Reset()
	errout.Reset()
	code, _ = runHost(args, &out, &errout)
	if code != 1 {
		t.Fatal("overwrite allowed")
	}
	after, _ := os.ReadFile(filepath.Join(dest, "tool-isolation-report.json"))
	if !bytes.Equal(before, after) {
		t.Fatal("output overwritten")
	}
	code, _ = runHost([]string{"isolation-check", "--policy", policy}, &out, &errout)
	if code != 2 {
		t.Fatal("missing instant accepted")
	}
	if _, err = os.Stat(p.Workspace); !os.IsNotExist(err) {
		t.Fatal("offline inspection materialized workspace")
	}
}
