package kit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func writeManifest(t *testing.T, root, version string) {
	t.Helper()
	dir := filepath.Join(root, ".codex-plugin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(map[string]string{
		"name":    PluginName,
		"version": version,
	})
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestIsKitRootAndVersion(t *testing.T) {
	root := t.TempDir()
	if IsKitRoot(root) {
		t.Fatal("empty dir should not be kit root")
	}
	writeManifest(t, root, "1.2.3")
	if !IsKitRoot(root) {
		t.Fatal("expected kit root")
	}
	v, err := Version(root)
	if err != nil {
		t.Fatal(err)
	}
	if v != "1.2.3" {
		t.Fatalf("version=%q", v)
	}
}

func TestFindRootFromAOKRoot(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, "0.4.0")
	t.Setenv(EnvRoot, root)
	got, err := FindRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got != root {
		t.Fatalf("got %q want %q", got, root)
	}
}

func TestFindRootWalkAncestors(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, "0.4.0")
	nested := filepath.Join(root, "packs", "demo")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvRoot, "")
	got, err := FindRoot(nested)
	if err != nil {
		t.Fatal(err)
	}
	if got != root {
		t.Fatalf("got %q want %q", got, root)
	}
}

func TestMarketplaceAndShimPaths(t *testing.T) {
	home := t.TempDir()
	market, plugin := MarketplacePaths(home)
	if filepath.Base(market) != "marketplace.json" {
		t.Fatalf("marketplace=%q", market)
	}
	if filepath.Base(plugin) != PluginName {
		t.Fatalf("plugin=%q", plugin)
	}
	shim := CLIShimPath(home)
	if filepath.Base(shim) != "aok" && filepath.Base(shim) != "aok.cmd" {
		t.Fatalf("shim=%q", shim)
	}
}
