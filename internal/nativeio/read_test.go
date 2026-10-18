package nativeio

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExplicitFileAndManifest(t *testing.T) {
	root := t.TempDir()
	name := filepath.Join(root, "agent.md")
	if err := os.WriteFile(name, []byte("---\nname: agent\n---\nbody"), 0600); err != nil {
		t.Fatal(err)
	}
	files, err := Load(name)
	if err != nil || len(files) != 1 || files[0].Format != "markdown" {
		t.Fatalf("%v %v", files, err)
	}
	manifest := []byte(`{"version":"aok-native-input/v1","files":[{"path":"agent.md"}]}`)
	if err := os.WriteFile(filepath.Join(root, ManifestName), manifest, 0600); err != nil {
		t.Fatal(err)
	}
	files, err = Load(root)
	if err != nil || len(files) != 1 || files[0].Path != "agent.md" {
		t.Fatalf("%v %v", files, err)
	}
}
func TestRejectUnsafeManifestAndInput(t *testing.T) {
	for _, data := range []string{
		`{"version":"aok-native-input/v1","version":"aok-native-input/v1","files":[]}`,
		`{"version":"aok-native-input/v1","files":[{"path":"../outside"}]}`,
		`{"version":"aok-native-input/v1","files":[{"path":"link"}]}`,
		`{"version":"aok-native-input/v1","files":[{"path":"source"},{"path":"SOURCE"}]}`,
		`{"version":"wrong","files":[{"path":"source"}]}`,
	} {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "source"), []byte("secret data"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(root, "source"), filepath.Join(root, "link")); err != nil {
			t.Skip(err)
		}
		if err := os.WriteFile(filepath.Join(root, ManifestName), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(root); err == nil {
			t.Fatalf("accepted %s", data)
		}
	}
}
func TestDirectoryNeverDiscoversAmbientFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "credentials.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root); err == nil {
		t.Fatal("read directory without explicit inventory")
	}
}

func TestMarkdownExtensionInferenceForExplicitAndManifestInputs(t *testing.T) {
	for _, name := range []string{"reviewer.md", "reviewer.MDC", "reviewer.markdown"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, name), []byte("---\nname: reviewer\n---\nReview only."), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, ManifestName), []byte(`{"version":"aok-native-input/v1","files":[{"path":"`+name+`"}]}`), 0600); err != nil {
				t.Fatal(err)
			}
			for _, input := range []string{filepath.Join(root, name), root} {
				files, err := Load(input)
				if err != nil || len(files) != 1 || files[0].Format != "markdown" {
					t.Fatalf("extension inference for %s: files=%v err=%v", input, files, err)
				}
			}
		})
	}
}
