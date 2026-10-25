package toolchain

import (
	"archive/zip"
	"bytes"
	"context"
	a "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/accessv1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/engine"
	"os"
	"path/filepath"
	"testing"
)

func TestRuntimeTreeIncludesDirectoriesAndDependencies(t *testing.T) {
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	os.WriteFile(filepath.Join(root, "gh"), []byte("binary"), 0700)
	first, e := HashTree(context.Background(), root)
	if e != nil {
		t.Fatal(e)
	}
	os.Mkdir(filepath.Join(root, "empty"), 0700)
	second, _ := HashTree(context.Background(), root)
	if first == second {
		t.Fatal("empty directory not pinned")
	}
	pin := a.ToolPin{ID: "gh", Path: filepath.Join(root, "gh"), SHA256: engine.Hash([]byte("binary")), VersionOutput: "fixture", Platform: Platform(), Runtime: &a.LocalPin{Path: root, SHA256: second}}
	if Verify(context.Background(), pin) != nil {
		t.Fatal("initial tree rejected")
	}
	os.WriteFile(filepath.Join(root, "dependency.py"), []byte("changed"), 0600)
	if Verify(context.Background(), pin) == nil {
		t.Fatal("dependency substitution accepted")
	}
}
func TestArchiveFullLayoutAndExplicitDiscoveryNoExecution(t *testing.T) {
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	for name, body := range map[string]string{"bin/gh": "binary", "lib/dependency": "library"} {
		f, _ := z.Create(name)
		f.Write([]byte(body))
	}
	z.Close()
	raw := buf.Bytes()
	dir, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	plan := a.InstallPlan{Action: "install", Destination: filepath.Join(dir, "installed"), Artifact: a.Artifact{ToolID: "gh", ToolVersion: "1", Platform: Platform(), Format: "zip", Layout: "tree-v1", Executable: "bin/gh", MaxBytes: 1 << 20, SHA256: engine.Hash(raw), ExecutableSHA256: engine.Hash([]byte("binary")), VersionOutput: "fixture"}}
	pin, e := installLayout(context.Background(), plan, raw, func() error { return nil })
	if e != nil || pin.Runtime == nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(filepath.Join(plan.Destination, "lib/dependency")); e != nil {
		t.Fatal("layout discarded dependency")
	}
	found, e := Discover(context.Background(), a.DiscoveryRequest{Version: a.Version, Kind: "ToolDiscovery", Roots: []string{filepath.Join(plan.Destination, "bin")}, Tools: []string{"gh"}})
	if e != nil || len(found) != 1 || found[0].Status != "discovered-unverified" {
		t.Fatal("discovery executed or fabricated version", e)
	}
}
func TestAzureExtensionArchiveUsesDeclaredParentLayout(t *testing.T) {
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	file, _ := z.Create("azext_devops/__init__.py")
	file.Write([]byte("fixture-extension"))
	z.Close()
	raw := buf.Bytes()
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	plan := a.InstallPlan{Action: "install", Destination: filepath.Join(root, "extensions"), Artifact: a.Artifact{ToolID: "az", ToolVersion: "1", Platform: Platform(), Format: "zip", Layout: "azure-extension", Executable: "azext_devops/__init__.py", MaxBytes: 1 << 20, SHA256: engine.Hash(raw), ExecutableSHA256: engine.Hash([]byte("fixture-extension")), VersionOutput: "fixture"}}
	pin, e := installLayout(context.Background(), plan, raw, func() error { return nil })
	if e != nil || pin.Runtime == nil || pin.Runtime.Path != plan.Destination {
		t.Fatal("extension root not emitted", e)
	}
}
