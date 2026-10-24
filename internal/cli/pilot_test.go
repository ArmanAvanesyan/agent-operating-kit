package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPhase9OfflineCLIAndNoAuthority(t *testing.T) {
	root, e := filepath.Abs("../..")
	if e != nil {
		t.Fatal(e)
	}
	base, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	invoke := func(want int, args ...string) string {
		t.Helper()
		var out, err bytes.Buffer
		if code := Run(args, &out, &err); code != want {
			t.Fatalf("%v code=%d want=%d stdout=%s stderr=%s", args, code, want, &out, &err)
		}
		return out.String()
	}
	r := filepath.Join(root, "internal", "readiness", "testdata")
	p := filepath.Join(root, "internal", "pilot", "testdata")
	args := []string{"readiness", "report", "--manifest", filepath.Join(r, "route.json"), "--policy", filepath.Join(r, "policy.json"), "--as-of", "2026-10-24T12:00:00Z", "--out", filepath.Join(base, "ready"), "--json"}
	s := invoke(1, args...)
	if !strings.Contains(s, "policy-trust") || !strings.Contains(s, `"executionReady": false`) {
		t.Fatal(s)
	}
	invoke(1, args...)
	invoke(2, "readiness", "report", "--manifest", filepath.Join(r, "route.json"), "--policy", filepath.Join(r, "policy.json"), "--as-of", "invalid", "--out", filepath.Join(base, "bad"))
	invoke(0, "pilot", "validate", "--manifest", filepath.Join(p, "manifest.json"), "--policy", filepath.Join(p, "policy.json"), "--as-of", "2026-10-24T12:00:00Z", "--json")
	invoke(0, "pilot", "replay", "--scenario", filepath.Join(p, "scenario.json"), "--as-of", "2026-10-24T12:00:00Z", "--out", filepath.Join(base, "replay"), "--json")
	s = invoke(1, "pilot", "assess", "--campaign", p, "--policy", filepath.Join(p, "policy.json"), "--as-of", "2026-10-24T12:00:00Z", "--out", filepath.Join(base, "assess"), "--json")
	if !strings.Contains(s, `"productionEligible": false`) || !strings.Contains(s, `"metrics"`) {
		t.Fatal("assessment failed before generating its report", s)
	}
	no := filepath.Join(base, "no-launch")
	s = invoke(1, "pilot", "execute", "--campaign", p, "--authority", "missing", "--once", "--out", no, "--json")
	if !strings.Contains(s, `"executionReady": false`) {
		t.Fatal(s)
	}
	if _, e = os.Lstat(no); !os.IsNotExist(e) {
		t.Fatal("blocked execution wrote state", e)
	}
	invoke(2, "pilot", "execute", "--campaign", p, "--authority", "missing", "--once", "--as-of", "2026-10-24T12:00:00Z", "--out", no)
	invoke(2, "pilot", "execute", "--campaign", p, "--authority", "missing", "--out", no)
	for _, cmd := range []string{"readiness", "pilot", "operations"} {
		invoke(0, cmd, "--help")
	}
}
