package nativebridge

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/nativeadapter"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/nativeadapters/codex"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/nativeadapters/opencode"
)

func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	src := filepath.Join("..", "agentresolve", "testdata", "corpus")
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		out := filepath.Join(root, rel)
		if d.IsDir() {
			return os.MkdirAll(out, 0700)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(out, b, 0600)
	})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "agent.json")
	b, _ := os.ReadFile(path)
	var agent map[string]any
	if err := json.Unmarshal(b, &agent); err != nil {
		t.Fatal(err)
	}
	delete(agent["instructions"].(map[string]any), "parameters")
	b, _ = json.Marshal(agent)
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	return root
}
func TestCanonicalExportRetainsSourceSelectionAndBlocksUnsupportedControls(t *testing.T) {
	root := fixture(t)
	registry := nativeadapter.NewRegistry()
	if err := registry.Register(opencode.New()); err != nil {
		t.Fatal(err)
	}
	files, b, r := Export(nativeadapter.NewEngine(registry), "opencode", "project", filepath.Join(root, "assignment.json"), root, nil)
	if r.Status != "blocked" || len(files) != 1 || b.Canonical == nil || b.Canonical.SnapshotDigest == "" || len(b.Canonical.Sources) == 0 {
		t.Fatalf("failed candidate: %+v", r)
	}
	if len(b.Document.Assets["instructions.md"]) == 0 {
		t.Fatal("instruction bytes lost")
	}
	encoded, err := nativeadapter.EncodeBundle(b)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := nativeadapter.DecodeBundle(encoded); err != nil {
		t.Fatal(err)
	}
	reimported, report := nativeadapter.NewEngine(registry).Import("opencode", "project", files, &b, nil)
	if reimported.Canonical == nil || reimported.Canonical.SnapshotDigest != b.Canonical.SnapshotDigest {
		t.Fatalf("canonical provenance lost: %+v", report)
	}
	b.Canonical.SnapshotDigest = "changed"
	if _, err := nativeadapter.EncodeBundle(b); err == nil {
		t.Fatal("tampered source evidence accepted")
	}
}
func TestCanonicalTargetMismatchHasNoOutput(t *testing.T) {
	root := fixture(t)
	files, _, r := Export(nativeadapter.NewEngine(nil), "claude-code", "project", filepath.Join(root, "assignment.json"), root, nil)
	if len(files) != 0 || r.Status != "blocked" {
		t.Fatalf("target mismatch accepted: %+v", r)
	}
}

func TestCanonicalCodexEffortProjection(t *testing.T) {
	root := fixture(t)
	path := filepath.Join(root, "execution-profile.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var profile map[string]any
	if err := json.Unmarshal(data, &profile); err != nil {
		t.Fatal(err)
	}
	profile["client"].(map[string]any)["adapter"] = "codex"
	profile["model"].(map[string]any)["reasoning"] = map[string]any{"mode": "effort", "effort": "medium"}
	data, _ = json.Marshal(profile)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	registry := nativeadapter.NewRegistry()
	if err := registry.Register(codex.New()); err != nil {
		t.Fatal(err)
	}
	files, b, r := Export(nativeadapter.NewEngine(registry), "codex", "project", filepath.Join(root, "assignment.json"), root, nil)
	if len(files) != 1 || r.Status != "blocked" || string(b.Document.Fields["executionProfile.model.reasoning.effort"]) != `"medium"` {
		t.Fatalf("effort mapping lost: %+v", r)
	}
}
