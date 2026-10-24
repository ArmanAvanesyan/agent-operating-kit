package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestVerificationOfflineCommands(t *testing.T) {
	root, e := filepath.Abs("../..")
	if e != nil {
		t.Fatal(e)
	}
	base, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	fixture := filepath.Join(root, "internal", "verification", "testdata", "offline")
	bundle := filepath.Join(base, "candidate")
	invoke := func(want int, args ...string) {
		t.Helper()
		var out, err bytes.Buffer
		if code := Run(args, &out, &err); code != want {
			t.Fatalf("%v code=%d want=%d stdout=%s stderr=%s", args, code, want, out.String(), err.String())
		}
	}
	invoke(0, "verify", "import", "--manifest", filepath.Join(fixture, "manifest.json"), "--policy", filepath.Join(fixture, "policy.json"), "--out", bundle, "--json")
	invoke(0, "verify", "check", bundle, "--policy", filepath.Join(fixture, "policy.json"), "--as-of", "2026-10-24T12:00:00Z", "--json")
	invoke(1, "verify", "import", "--manifest", filepath.Join(fixture, "manifest.json"), "--policy", filepath.Join(fixture, "policy.json"), "--out", bundle)
	invoke(2, "verify", "check", bundle, "--policy", filepath.Join(fixture, "policy.json"), "--as-of", "bad")
	invoke(1, "verify", "run", bundle, "--policy", filepath.Join(fixture, "policy.json"), "--out", filepath.Join(base, "blocked-run"))
	invoke(0, "writeback", "replay", "--scenario", filepath.Join(root, "internal", "writeback", "testdata", "replay.json"), "--as-of", "2026-10-24T12:00:00Z", "--out", filepath.Join(base, "replay"), "--json")
	invoke(1, "tracker", "dispatch", "--outbox", filepath.Join(base, "never-created.sqlite"), "--operation", "one", "--once", "--json")
	invoke(2, "tracker", "dispatch", "--outbox", filepath.Join(base, "never-created.sqlite"), "--operation", "one")
	invoke(2, "runtime", "dispatch", filepath.Join(base, "missing-runtime"), "--run", "run", "--once", "--verification-policy", filepath.Join(fixture, "policy.json"))
	invoke(0, "delivery", "handoff", "--help")
	invoke(1, "tracker", "outbox", filepath.Join(base, "never-created.sqlite"))
	if _, err := os.Lstat(filepath.Join(base, "never-created.sqlite")); !os.IsNotExist(err) {
		t.Fatal("inspection/blocked dispatch created state", err)
	}
}
