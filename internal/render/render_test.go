package render

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
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

func TestResourcePackBundleTargets(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	tmp := t.TempDir()
	for _, target := range []string{"codex", "claude-code", "cursor", "opencode", "gemini", "openclaw", "hermes-agent"} {
		bundle, err := RenderTargetBundle(root, "repository-delivery", target, tmp)
		if err != nil {
			t.Fatalf("%s: %v", target, err)
		}
		if _, err := os.Stat(filepath.Join(bundle, "sources", "packs", "repository-delivery", "skills")); err != nil {
			t.Fatalf("%s canonical skills: %v", target, err)
		}
		var contract map[string]any
		data, err := os.ReadFile(filepath.Join(bundle, "aok-bundle.json"))
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, &contract); err != nil {
			t.Fatal(err)
		}
		if contract["resourcePaths"] == nil {
			t.Fatalf("%s missing resource path metadata", target)
		}
		if target == "codex" {
			plugin, err := loadJSON(filepath.Join(bundle, ".codex-plugin", "plugin.json"))
			if err != nil {
				t.Fatal(err)
			}
			if plugin["skills"] == nil {
				t.Fatal("Codex plugin missing skills entry")
			}
		}
		if target == "claude-code" {
			if _, err := os.Stat(filepath.Join(bundle, ".claude-plugin", "plugin.json")); err != nil {
				t.Fatal(err)
			}
		}
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

func TestResourceBundlesResolveCanonicalSkillGuides(t *testing.T) {
	root, _ := filepath.Abs("../..")
	out := t.TempDir()
	for _, target := range []string{"codex", "claude-code", "cursor", "opencode", "gemini", "openclaw", "hermes-agent"} {
		bundle, err := RenderTargetBundle(root, "repository-delivery", target, out)
		if err != nil {
			t.Fatalf("%s: %v", target, err)
		}
		if _, err := os.Stat(filepath.Join(bundle, "sources", "packs", "repository-delivery", "skills")); err != nil {
			t.Fatalf("%s canonical skills: %v", target, err)
		}
		data, err := os.ReadFile(filepath.Join(bundle, "aok-bundle.json"))
		if err != nil {
			t.Fatal(err)
		}
		var contract map[string]any
		if err := json.Unmarshal(data, &contract); err != nil {
			t.Fatal(err)
		}
		if contract["resourcePaths"] == nil {
			t.Fatalf("%s missing resource paths", target)
		}
		if target == "codex" {
			plugin, err := loadJSON(filepath.Join(bundle, ".codex-plugin", "plugin.json"))
			if err != nil {
				t.Fatal(err)
			}
			skills, ok := plugin["skills"].(string)
			if !ok || skills != "./sources/packs/repository-delivery/skills/" {
				t.Fatalf("unexpected Codex skills path: %#v", plugin["skills"])
			}
		}
		if target == "claude-code" {
			assertGuideResolves(t, bundle, filepath.Join("skills", "repo-delivery-setup", "SKILL.md"))
		}
		if target == "cursor" {
			assertGuideResolves(t, bundle, filepath.Join(".cursor", "skills", "repo-delivery-setup", "SKILL.md"))
		}
	}
}

func assertGuideResolves(t *testing.T, bundle, wrapper string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(bundle, wrapper))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`\]\(([^)]+)\)`).FindSubmatch(data)
	if len(m) != 2 {
		t.Fatalf("missing canonical guide link in %s", wrapper)
	}
	guide := filepath.Clean(filepath.Join(filepath.Dir(filepath.Join(bundle, wrapper)), string(m[1])))
	if _, err := os.Stat(guide); err != nil {
		t.Fatalf("wrapper %s resolves to missing guide %s: %v", wrapper, guide, err)
	}
}

func TestSupportTreeFiltersArtifactsAndRejectsSymlinks(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "support")
	dest := filepath.Join(root, "copy")
	for _, rel := range []string{"guide.md", "build/output.txt", ".venv/lib/fake", "pkg.egg-info/meta", "__pycache__/a.pyc", ".coverage.123", "keep.py"} {
		path := filepath.Join(src, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := copyFilteredPackTree(src, dest); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"guide.md", "keep.py"} {
		if _, err := os.Stat(filepath.Join(dest, rel)); err != nil {
			t.Fatal(err)
		}
	}
	for _, rel := range []string{"build", ".venv", "pkg.egg-info", "__pycache__", ".coverage.123"} {
		if _, err := os.Stat(filepath.Join(dest, rel)); !os.IsNotExist(err) {
			t.Fatalf("artifact copied: %s", rel)
		}
	}
	if err := os.Symlink(filepath.Join(root, "outside"), filepath.Join(src, "escape")); err != nil {
		t.Fatal(err)
	}
	if err := copyFilteredPackTree(src, filepath.Join(root, "copy2")); err == nil {
		t.Fatal("expected symlink rejection")
	}
}

func TestBundleSourceContainmentSiblingAndSymlink(t *testing.T) {
	root := t.TempDir()
	ship := filepath.Join(root, "packs", "ship-pipeline")
	support := filepath.Join(root, "packs", "repository-delivery")
	if err := os.MkdirAll(ship, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"README.md", ".venv/cache", "build/out", "thing.pyc"} {
		path := filepath.Join(support, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("ok"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	bundle := filepath.Join(root, "bundle")
	var copied []string
	if err := copyBundleSource(root, ship, "../repository-delivery", bundle, &copied); err != nil {
		t.Fatal(err)
	}
	if len(copied) != 1 || copied[0] != "sources/packs/repository-delivery" {
		t.Fatalf("bad copied metadata: %#v", copied)
	}
	if _, err := os.Stat(filepath.Join(bundle, filepath.FromSlash(copied[0]), "README.md")); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{".venv", "build", "thing.pyc"} {
		if _, err := os.Stat(filepath.Join(bundle, "sources", "packs", "repository-delivery", rel)); !os.IsNotExist(err) {
			t.Fatalf("artifact copied: %s", rel)
		}
	}
	outside := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(support, "escape")); err != nil {
		t.Fatal(err)
	}
	if err := copyBundleSource(root, ship, "../repository-delivery", filepath.Join(root, "bundle2"), &copied); err == nil {
		t.Fatal("expected nested symlink rejection")
	}
	if err := copyBundleSource(root, ship, "../../outside", filepath.Join(root, "bundle3"), &copied); err == nil {
		t.Fatal("expected traversal rejection")
	}
}

func TestSkillRootSafety(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"skills", "../escape", ".", "C:/skills", "skills\\link"} {
		if validSkillRoot(root, rel) != (rel == "skills") {
			t.Fatalf("unexpected validity for %q", rel)
		}
	}
	if err := os.Symlink(filepath.Join(root, "skills"), filepath.Join(root, "alias")); err != nil {
		t.Fatal(err)
	}
	if validSkillRoot(root, "alias") {
		t.Fatal("accepted symlink skillRoot")
	}
}

func TestCanonicalGuideReferencesStayInsideDeclaredTree(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "skills", "fixture", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("fixture"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, guide := range []string{"../outside", "skills/../outside", "/absolute", "C:/outside", "other/fixture.md", "skills/missing/SKILL.md"} {
		if err := validateSkillGuides(root, "skills", []map[string]any{{"id": "fixture", "sourcePath": guide}}); err == nil {
			t.Fatalf("unsafe guide accepted: %s", guide)
		}
	}
	if err := validateSkillGuides(root, "skills", []map[string]any{{"id": "fixture", "sourcePath": "skills/fixture/SKILL.md"}}); err != nil {
		t.Fatal(err)
	}
}
