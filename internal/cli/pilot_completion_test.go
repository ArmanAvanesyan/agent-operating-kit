package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/operations"
)

func TestCompletionCLIKeepsQualificationAndEffectsBlocked(t *testing.T) {
	base, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	invoke := func(want int, args ...string) string {
		t.Helper()
		var out, errout bytes.Buffer
		if got := Run(args, &out, &errout); got != want {
			t.Fatalf("%v: code %d want %d out=%s err=%s", args, got, want, &out, &errout)
		}
		return out.String()
	}
	output := filepath.Join(base, "gates")
	raw := invoke(1, "pilot", "gates", "--as-of", "2026-10-24T12:00:00Z", "--out", output, "--json")
	ledger, e := operations.ReadCompletionLedger([]byte(raw))
	if e != nil {
		t.Fatal(e)
	}
	if !ledger.InspectionOnly || ledger.AuthorizationGranted || ledger.ExecutionReady {
		t.Fatal("ledger granted authority")
	}
	before, e := os.ReadFile(filepath.Join(output, "completion-gates.json"))
	if e != nil {
		t.Fatal(e)
	}
	invoke(1, "pilot", "gates", "--as-of", "2026-10-24T12:00:00Z", "--out", output, "--json")
	after, e := os.ReadFile(filepath.Join(output, "completion-gates.json"))
	if e != nil || !bytes.Equal(before, after) {
		t.Fatal("immutable ledger overwritten", e)
	}
	invoke(2, "pilot", "gates", "--out", filepath.Join(base, "bad"))
	invoke(2, "pilot", "gates", "--as-of", "invalid", "--out", filepath.Join(base, "bad"))
	fixture := filepath.Join("..", "pilot", "experiment", "testdata", "bundle.json")
	s := invoke(0, "pilot", "experiment", "validate", "--bundle", fixture, "--as-of", "2026-10-24T12:00:00Z", "--json")
	var r map[string]any
	if e = json.Unmarshal([]byte(s), &r); e != nil {
		t.Fatal(e)
	}
	if r["structuralOnly"] != true || r["executionReady"] != false || r["productionEligible"] != false {
		t.Fatal("structural validation promoted trust")
	}
	for _, mode := range []string{"experiment", "calibration"} {
		out := filepath.Join(base, "no-"+mode)
		args := []string{"pilot", mode, "execute", "--once", "--out", out, "--json"}
		if mode == "experiment" {
			args = append(args, "--bundle", "absent")
		} else {
			args = append(args, "--campaign", "absent", "--authority", "absent")
		}
		invoke(1, args...)
		if _, e = os.Lstat(out); !os.IsNotExist(e) {
			t.Fatal("unqualified execution created output or intent", e)
		}
		invoke(2, append(args, "--as-of", "2026-10-24T12:00:00Z")...)
	}
	invoke(2, "pilot", "experiment", "execute", "--bundle", "absent", "--out", filepath.Join(base, "missing-once"))
}
