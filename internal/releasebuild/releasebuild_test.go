package releasebuild

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadVersion(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	v, err := LoadVersion(root)
	if err != nil {
		t.Fatal(err)
	}
	if v == "" {
		t.Fatal("empty version")
	}
}

func TestIterFilesAndWriteZip(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	files, err := iterFiles(root, []string{"LICENSE", "CHANGELOG.md"})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("files: %v", files)
	}
	out := filepath.Join(t.TempDir(), "t.zip")
	if err := writeZip(root, out, "base", files); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(out)
	if err != nil || info.Size() == 0 {
		t.Fatalf("zip: %v %v", info, err)
	}
}

func TestBuildSmokeWithFakeBundles(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping full release build in short mode")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	prev := RenderBundlesCmd
	t.Cleanup(func() { RenderBundlesCmd = prev })
	RenderBundlesCmd = func(_, outDir string) error {
		if err := os.MkdirAll(filepath.Join(outDir, "demo"), 0o755); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(outDir, "demo", "README.md"), []byte("# bundle\n"), 0o644)
	}

	dist := filepath.Join(t.TempDir(), "release")
	artifacts, err := Build(root, dist, "0.0.0-test")
	if err != nil {
		t.Fatal(err)
	}
	if len(artifacts) < 10 {
		t.Fatalf("too few artifacts: %d", len(artifacts))
	}
	sums := filepath.Join(dist, "SHA256SUMS")
	data, err := os.ReadFile(sums)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"aok-0.0.0-test-darwin-arm64",
		"aok-0.0.0-test-windows-amd64.exe",
		"agent-operating-kit-0.0.0-test-kit.zip",
		"agent-operating-kit-0.0.0-test-bundles.zip",
	} {
		if !strings.Contains(string(data), name) {
			t.Fatalf("SHA256SUMS missing %s:\n%s", name, data)
		}
		if _, err := os.Stat(filepath.Join(dist, name)); err != nil {
			t.Fatalf("missing artifact %s: %v", name, err)
		}
	}
	raw, err := os.ReadFile(filepath.Join(dist, "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	var idx Index
	if err := json.Unmarshal(raw, &idx); err != nil {
		t.Fatal(err)
	}
	if idx.Product != Product || idx.Version != "0.0.0-test" {
		t.Fatalf("index: %+v", idx)
	}
}
