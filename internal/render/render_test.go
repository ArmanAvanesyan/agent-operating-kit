package render

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestRenderPackCodex(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	var stdout bytes.Buffer
	if err := Pack(root, "engineering-guardrails", "codex", filepath.Join(out, "eg-codex.json"), &stdout); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out, "eg-codex.json")); err != nil {
		t.Fatal(err)
	}
}

func TestRenderBundle(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	var stdout bytes.Buffer
	if err := Bundle(root, "engineering-guardrails", "cursor", out, &stdout); err != nil {
		t.Fatal(err)
	}
	contract := filepath.Join(out, "cursor", "engineering-guardrails", "aok-bundle.json")
	if _, err := os.Stat(contract); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out, "index.json")); err != nil {
		t.Fatal(err)
	}
}

func TestRenderCursorNativeTree(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	var stdout bytes.Buffer
	if err := Bundle(root, "engineering-guardrails", "cursor", out, &stdout); err != nil {
		t.Fatal(err)
	}
	bundle := filepath.Join(out, "cursor", "engineering-guardrails")
	// Generic bundle remains.
	if _, err := os.Stat(filepath.Join(bundle, "aok-bundle.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(bundle, "import.json")); err != nil {
		t.Fatal(err)
	}
	// Native Cursor project tree (additive).
	rule := filepath.Join(bundle, ".cursor", "rules", "quality-guard.mdc")
	if _, err := os.Stat(rule); err != nil {
		t.Fatalf("expected native rule at %s: %v", rule, err)
	}
	data, err := os.ReadFile(rule)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !bytes.Contains(data, []byte("alwaysApply: false")) {
		t.Fatalf("rule missing frontmatter: %s", text)
	}
	if !bytes.Contains(data, []byte("Quality Guard")) {
		t.Fatalf("rule missing role name: %s", text)
	}
	checklist := filepath.Join(bundle, ".cursor", "hooks-checklist.md")
	if _, err := os.Stat(checklist); err != nil {
		t.Fatalf("expected hooks checklist: %v", err)
	}
	// Skills pack smoke: ship-pipeline emits SKILL.md
	out2 := t.TempDir()
	if err := Bundle(root, "ship-pipeline", "cursor", out2, &stdout); err != nil {
		t.Fatal(err)
	}
	skill := filepath.Join(out2, "cursor", "ship-pipeline", ".cursor", "skills", "ship-pr", "SKILL.md")
	if _, err := os.Stat(skill); err != nil {
		t.Fatalf("expected skill SKILL.md: %v", err)
	}
}
