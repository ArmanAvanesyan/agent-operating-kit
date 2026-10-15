package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestAgentResolveMisuse(t *testing.T) {
	for _, args := range [][]string{{"agent", "resolve"}, {"agent", "resolve", "file"}, {"agent", "resolve", "file", "--out"}, {"agent", "resolve", "file", "--out", "a", "--out", "b"}, {"agent", "resolve", "file", "extra", "--out", "a"}} {
		var out, stderr bytes.Buffer
		if code := Run(args, &out, &stderr); code != 2 {
			t.Fatalf("%v: %d %s %s", args, code, &out, &stderr)
		}
	}
}
func TestAgentResolveWritesInvalidEvidenceWithoutOverwrite(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	corpus := filepath.Join(root, "corpus")
	if err = os.Mkdir(corpus, 0700); err != nil {
		t.Fatal(err)
	}
	assignment := filepath.Join(corpus, "assignment.json")
	if err = os.WriteFile(assignment, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(root, "report")
	var out, stderr bytes.Buffer
	args := []string{"agent", "resolve", assignment, "--out", destination, "--json"}
	if code := Run(args, &out, &stderr); code != 1 {
		t.Fatalf("%d %s %s", code, &out, &stderr)
	}
	before, err := os.ReadFile(filepath.Join(destination, "resolution.json"))
	if err != nil {
		t.Fatal(err)
	}
	var report struct{ Status string }
	if err = json.Unmarshal(before, &report); err != nil {
		t.Fatal(err)
	}
	if report.Status != "invalid" {
		t.Fatal(report.Status)
	}
	out.Reset()
	stderr.Reset()
	if code := Run(args, &out, &stderr); code != 1 || !bytes.Contains(out.Bytes(), []byte("resolution.output")) {
		t.Fatalf("overwrite was not rejected: %d %s", code, &out)
	}
	after, _ := os.ReadFile(filepath.Join(destination, "resolution.json"))
	if !bytes.Equal(before, after) {
		t.Fatal("report changed")
	}
}

func TestAgentResolveBlockedWritesSnapshot(t *testing.T) {
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(parent, "evidence")
	assignment := filepath.Join("..", "agentresolve", "testdata", "corpus", "assignment.json")
	var out, stderr bytes.Buffer
	if code := Run([]string{"agent", "resolve", assignment, "--out", destination, "--json"}, &out, &stderr); code != 1 {
		t.Fatalf("%d %s %s", code, &out, &stderr)
	}
	var report struct {
		Status   string
		Snapshot *struct{ Digest string }
	}
	if err = json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Status != "blocked" || report.Snapshot == nil || len(report.Snapshot.Digest) != 64 {
		t.Fatalf("missing blocked snapshot: %s", &out)
	}
	disk, err := os.ReadFile(filepath.Join(destination, "resolution.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(disk, out.Bytes()) {
		t.Fatal("disk and machine report differ")
	}
}
