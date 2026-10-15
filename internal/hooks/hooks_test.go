package hooks

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrePass(t *testing.T) {
	var stdout bytes.Buffer
	code, err := Pre(filepath.Join("testdata", "complete.md"), &stdout)
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stdout.String())
	}
	if !strings.Contains(stdout.String(), "Pre-hooks passed.") {
		t.Fatalf("unexpected output: %s", stdout.String())
	}
}

func TestPreFail(t *testing.T) {
	var stdout bytes.Buffer
	code, err := Pre(filepath.Join("testdata", "incomplete.md"), &stdout)
	if err != nil {
		t.Fatal(err)
	}
	if code != 1 {
		t.Fatalf("want exit 1, got %d", code)
	}
	out := stdout.String()
	if !strings.Contains(out, "Pre-hooks failed") {
		t.Fatalf("missing fail header: %s", out)
	}
	for _, name := range []string{"Problem", "Acceptance Criteria", "Verification Plan", "Target Repo"} {
		if !strings.Contains(out, "- "+name) {
			t.Fatalf("missing failed check %s in: %s", name, out)
		}
	}
}

func TestPreMissingFile(t *testing.T) {
	var stdout bytes.Buffer
	code, err := Pre(filepath.Join("testdata", "missing.md"), &stdout)
	if code != 1 || err == nil {
		t.Fatalf("want error exit, got code=%d err=%v", code, err)
	}
}

func TestAfterWarning(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code, err := After(filepath.Join("testdata", "complete.md"), &stdout, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(stdout.String(), "## Agent Update") {
		t.Fatalf("missing stub: %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "After-hooks warning") {
		t.Fatalf("expected empty summary warning: %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "QA verdict is still pending") {
		t.Fatalf("expected QA warning: %s", stderr.String())
	}
}

func TestExtractSection(t *testing.T) {
	body := "## Problem\n\nHello\n\n## Scope\n\nWorld\n"
	if got := ExtractSection(body, "Problem"); got != "Hello" {
		t.Fatalf("got %q", got)
	}
	if got := ExtractSection(body, "Missing"); got != "" {
		t.Fatalf("got %q", got)
	}
}
