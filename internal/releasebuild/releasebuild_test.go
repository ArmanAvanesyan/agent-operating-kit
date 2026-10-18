package releasebuild

import (
	"debug/macho"
	"encoding/binary"
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
	for _, arch := range []string{"amd64", "arm64"} {
		binaryPath := filepath.Join(dist, "aok-0.0.0-test-darwin-"+arch)
		image, err := macho.Open(binaryPath)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, load := range image.Loads {
			raw := load.Raw()
			if len(raw) >= 4 && binary.LittleEndian.Uint32(raw[:4]) == 0x1b {
				found = true
			}
		}
		image.Close()
		if !found {
			t.Fatalf("Darwin %s release lacks LC_UUID", arch)
		}
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

func TestExplorationSkillsIncludedInCodexPlugin(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	files, err := iterFiles(root, CodexPluginPaths)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, file := range files {
		rel, err := filepath.Rel(root, file)
		if err != nil {
			t.Fatal(err)
		}
		found[filepath.ToSlash(rel)] = true
	}
	for _, path := range []string{
		"skills/project-exploration/SKILL.md",
		"skills/project-exploration/scripts/inventory.py",
		"skills/project-exploration/scripts/structure.py",
		"skills/project-exploration/references/structural-analysis.md",
		"skills/environment-readiness/SKILL.md",
		"skills/environment-readiness/scripts/assess.py",
	} {
		if !found[path] {
			t.Errorf("Codex plugin missing skill resource: %s", path)
		}
	}
}

func TestReleaseRefusesSymlinkPayloadAndFiltersCaches(t *testing.T) {
	root := t.TempDir()
	pack := filepath.Join(root, "packs", "repository-delivery")
	if err := os.MkdirAll(pack, 0755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "private")
	if err := os.WriteFile(outside, []byte("must not package"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(pack, "leak")
	if err := os.Symlink(outside, link); err != nil {
		t.Skip(err)
	}
	if _, err := iterFiles(root, []string{"packs"}); err == nil {
		t.Fatal("symlink file accepted")
	}
	if _, err := iterFiles(root, []string{"packs/repository-delivery/leak"}); err == nil {
		t.Fatal("direct symlink accepted")
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(pack, ".venv"), 0755); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{".coverage", ".venv/cache", "cache.pyc", "safe.py"} {
		if err := os.WriteFile(filepath.Join(pack, rel), []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	files, err := iterFiles(root, []string{"packs"})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || filepath.Base(files[0]) != "safe.py" {
		t.Fatalf("cache leakage: %v", files)
	}
}
