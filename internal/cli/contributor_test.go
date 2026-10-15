package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/task"
)

func TestCLIHooksAndImport(t *testing.T) {
	packet := filepath.Join("..", "hooks", "testdata", "complete.md")
	var stdout, stderr bytes.Buffer
	code := Run([]string{"hooks", "pre", packet}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("hooks pre exit %d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}

	histRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(histRoot, "x.md"), []byte("## Acceptance Criteria\nok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"import", "history", histRoot}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("import history exit %d: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Matched files: 1") {
		t.Fatalf("import output: %s", stdout.String())
	}
}

func TestCLITaskWithFakeGh(t *testing.T) {
	prev := task.GhRunner
	t.Cleanup(func() { task.GhRunner = prev })
	task.GhRunner = func(args []string) ([]byte, error) {
		return json.Marshal(map[string]any{
			"title": "T",
			"body":  "## Problem\n\nP\n",
			"url":   "https://github.com/acme/demo/issues/1",
			"state": "OPEN",
		})
	}
	var stdout, stderr bytes.Buffer
	code := Run([]string{"task", "sync", "https://github.com/acme/demo/issues/1"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("task sync exit %d: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"_kind": "issue"`) {
		t.Fatalf("sync out: %s", stdout.String())
	}

	out := filepath.Join(t.TempDir(), "p.md")
	stdout.Reset()
	code = Run([]string{"task", "packet", "https://github.com/acme/demo/issues/1", "-o", out}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("task packet exit %d: %s", code, stderr.String())
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "# Agent Task Packet") {
		t.Fatalf("packet: %s", data)
	}
}

func TestUsageListsContributorCommands(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"help"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	for _, needle := range []string{"task sync|packet", "hooks pre|after", "import history"} {
		if !strings.Contains(stdout.String(), needle) {
			t.Fatalf("usage missing %q:\n%s", needle, stdout.String())
		}
	}
}
