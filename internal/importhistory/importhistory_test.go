package importhistory

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHistoryDigest(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "notes.md"), `# Sprint notes

## Agent Update

Status: done

## Final Summary

Shipped the installer path fix with evidence.
`)
	mustWrite(t, filepath.Join(root, "noise.txt"), "unrelated log line\n")
	mustWrite(t, filepath.Join(root, "dist", "skip.md"), "## Acceptance Criteria\nshould be ignored\n")

	report, err := Summarize(root)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(report, "# Agent History Import") {
		t.Fatalf("missing header: %s", report)
	}
	if !strings.Contains(report, "## notes.md") {
		t.Fatalf("missing matched file: %s", report)
	}
	if !strings.Contains(report, "Agent Update") {
		t.Fatalf("missing signal: %s", report)
	}
	if !strings.Contains(report, "Contains agent update block") {
		t.Fatalf("missing update marker: %s", report)
	}
	if !strings.Contains(report, "Final summary excerpt:") {
		t.Fatalf("missing excerpt: %s", report)
	}
	if !strings.Contains(report, "- Scanned files: 2") {
		t.Fatalf("expected notes.md + noise.txt scanned (dist ignored): %s", report)
	}
	if !strings.Contains(report, "- Matched files: 1") {
		t.Fatalf("expected one match: %s", report)
	}
	if strings.Contains(report, "skip.md") {
		t.Fatalf("dist file should be ignored: %s", report)
	}
}

func TestHistoryOutputFile(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "a.md"), "Verdict: PASS\n")
	out := filepath.Join(root, "out", "report.md")
	var stdout bytes.Buffer
	code, err := History(root, out, &stdout)
	if err != nil || code != 0 {
		t.Fatalf("code=%d err=%v", code, err)
	}
	if !strings.Contains(stdout.String(), "Wrote ") {
		t.Fatalf("stdout: %s", stdout.String())
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "Matched files: 1") {
		t.Fatalf("report: %s", data)
	}
}

func TestHistoryNotDir(t *testing.T) {
	_, err := Summarize(filepath.Join(t.TempDir(), "missing"))
	if err == nil {
		t.Fatal("expected error")
	}
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
