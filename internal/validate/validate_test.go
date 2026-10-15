package validate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestKitPassesOnRepo(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, ".codex-plugin", "plugin.json")); err != nil {
		t.Skip("not running inside kit checkout")
	}
	errs := Kit(root)
	if len(errs) > 0 {
		t.Fatalf("validate errors: %v", errs[:min(5, len(errs))])
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
