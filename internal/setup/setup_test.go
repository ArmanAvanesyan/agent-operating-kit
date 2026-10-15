package setup

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestClaudeSetupMarketplaceKey(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	var stdout bytes.Buffer
	err = ClaudeSetup(ClaudeSetupOptions{
		Root:   root,
		Home:   home,
		Enable: []string{"all"},
	}, &stdout)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}
	markets := data["extraKnownMarketplaces"].(map[string]any)
	if _, ok := markets["aok-claude-code"]; !ok {
		t.Fatalf("missing aok-claude-code key: %v", markets)
	}
	enabled := data["enabledPlugins"].(map[string]any)
	if enabled["agent-operating-kit@aok-claude-code"] != true {
		t.Fatalf("root plugin not enabled: %v", enabled)
	}
}

func TestProjectInitAndValidate(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	var stdout bytes.Buffer
	if err := ProjectInit(root, target, true, &stdout); err != nil {
		t.Fatal(err)
	}
	missing := ValidateProject(target)
	if len(missing) > 0 {
		t.Fatalf("project missing: %v", missing)
	}
}
