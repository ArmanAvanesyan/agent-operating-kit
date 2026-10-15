package task

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseGitHubRef(t *testing.T) {
	issue := ParseGitHubRef("https://github.com/acme/demo/issues/42")
	if issue == nil || issue.Kind != "issue" || issue.Owner != "acme" || issue.Repo != "demo" || issue.Num != "42" {
		t.Fatalf("issue: %+v", issue)
	}
	pr := ParseGitHubRef("https://github.com/acme/demo/pull/7")
	if pr == nil || pr.Kind != "pr" || pr.Num != "7" {
		t.Fatalf("pr: %+v", pr)
	}
	if ParseGitHubRef("https://example.com/x") != nil {
		t.Fatal("expected nil")
	}
}

func TestSyncAndPacketWithFakeGh(t *testing.T) {
	prev := GhRunner
	t.Cleanup(func() { GhRunner = prev })
	GhRunner = func(args []string) ([]byte, error) {
		if len(args) < 3 || args[0] != "issue" || args[1] != "view" {
			t.Fatalf("unexpected args: %v", args)
		}
		payload := map[string]any{
			"title": "Install path fix",
			"body":  "## Problem\n\nBroken PATH\n\n## Scope\n\nLinux only\n\n## Acceptance Criteria\n\n- [x] works\n\n## Verification\n\n- test\n",
			"url":   "https://github.com/acme/demo/issues/42",
			"state": "OPEN",
		}
		return json.Marshal(payload)
	}

	var syncOut bytes.Buffer
	code, err := Sync("https://github.com/acme/demo/issues/42", &syncOut)
	if err != nil || code != 0 {
		t.Fatalf("sync code=%d err=%v", code, err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(syncOut.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["_kind"] != "issue" || decoded["_repo"] != "acme/demo" || decoded["_number"] != "42" {
		t.Fatalf("enriched fields: %v", decoded)
	}

	var packetOut bytes.Buffer
	code, err = Packet("https://github.com/acme/demo/issues/42", "", &packetOut)
	if err != nil || code != 0 {
		t.Fatalf("packet code=%d err=%v", code, err)
	}
	text := packetOut.String()
	if !strings.Contains(text, "# Agent Task Packet") {
		t.Fatalf("missing header: %s", text)
	}
	if !strings.Contains(text, "| Issue | https://github.com/acme/demo/issues/42 |") {
		t.Fatalf("missing issue url: %s", text)
	}
	if !strings.Contains(text, "| Target Repo | acme/demo |") {
		t.Fatalf("missing target repo: %s", text)
	}
	if !strings.Contains(text, "Broken PATH") {
		t.Fatalf("missing problem: %s", text)
	}
	if !strings.Contains(text, "Verdict: `PENDING`") {
		t.Fatalf("missing QA pending: %s", text)
	}

	outPath := filepath.Join(t.TempDir(), "packet.md")
	var wrote bytes.Buffer
	code, err = Packet("https://github.com/acme/demo/issues/42", outPath, &wrote)
	if err != nil || code != 0 {
		t.Fatalf("write code=%d err=%v", code, err)
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "Broken PATH") {
		t.Fatalf("file content: %s", data)
	}
}

func TestViewRejectsNonGitHub(t *testing.T) {
	_, err := View("not-a-url")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestPacketMarkdownDefaults(t *testing.T) {
	md := PacketMarkdown(map[string]any{
		"title":  "T",
		"body":   "",
		"url":    "https://github.com/acme/demo/pull/1",
		"_kind":  "pr",
		"_repo":  "acme/demo",
		"_number": "1",
	})
	if !strings.Contains(md, "| Pull Request | https://github.com/acme/demo/pull/1 |") {
		t.Fatalf("pr field: %s", md)
	}
	if !strings.Contains(md, "## Problem\n\nT\n") {
		t.Fatalf("title fallback: %s", md)
	}
	if !strings.Contains(md, "In scope:") {
		t.Fatalf("default scope: %s", md)
	}
}
