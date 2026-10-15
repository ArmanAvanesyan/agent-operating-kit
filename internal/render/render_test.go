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
