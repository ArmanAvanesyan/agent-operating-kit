package releasebuild

import (
	"archive/zip"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/validate"
)

func TestArchivesIncludeMethodologyAndWorkItemCatalogs(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	for name, inputs := range map[string][]string{
		"kit": KitPaths, "codex-plugin": CodexPluginPaths, "packs": PackPaths,
	} {
		t.Run(name, func(t *testing.T) {
			files, err := iterFiles(root, inputs)
			if err != nil {
				t.Fatal(err)
			}
			seen := map[string]bool{}
			templates := 0
			for _, file := range files {
				rel, err := filepath.Rel(root, file)
				if err != nil {
					t.Fatal(err)
				}
				rel = filepath.ToSlash(rel)
				seen[rel] = true
				if strings.HasPrefix(rel, "work-items/templates/") && strings.HasSuffix(rel, ".md") {
					templates++
				}
			}
			for _, required := range []string{
				"work-items/catalog.json",
				"methodologies/fdd/feature-contract.json",
				"methodologies/dsdm/requirement-contract.json",
				"methodologies/aup/iteration-contract.json",
			} {
				if !seen[required] {
					t.Errorf("missing %s", required)
				}
			}
			if templates != 19 {
				t.Errorf("work-item templates = %d, want 19", templates)
			}
		})
	}
}

func TestPackagedKitValidationAndReleaseBuild(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	files, err := iterFiles(root, KitPaths)
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	archivePath := filepath.Join(directory, "kit.zip")
	if err := writeZip(root, archivePath, "kit", files); err != nil {
		t.Fatal(err)
	}
	archive, err := zip.OpenReader(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	for _, item := range archive.File {
		destination := filepath.Join(directory, item.Name)
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			t.Fatal(err)
		}
		reader, err := item.Open()
		if err != nil {
			t.Fatal(err)
		}
		content, err := io.ReadAll(reader)
		reader.Close()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(destination, content, item.Mode().Perm()); err != nil {
			t.Fatal(err)
		}
	}
	kitRoot := filepath.Join(directory, "kit")
	t.Run("GoValidator", func(t *testing.T) {
		if errors := validate.Kit(kitRoot); len(errors) != 0 {
			t.Fatalf("extracted kit validation: %v", errors)
		}
	})
	t.Run("PythonValidator", func(t *testing.T) {
		python, err := exec.LookPath("python3")
		if err != nil {
			t.Skip("contributor Python validator requires python3")
		}
		command := exec.Command(python, filepath.Join(kitRoot, "scripts", "aok"), "validate")
		command.Dir = kitRoot
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("extracted kit Python validation: %v\n%s", err, output)
		}
	})
	t.Run("GoWorkkitBuildAndOfflineRead", func(t *testing.T) {
		binary := filepath.Join(directory, "workkit")
		command := exec.Command("go", "build", "-o", binary, "./kits/workkit/cmd/workkit")
		command.Dir = kitRoot
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("extracted WorkKit build: %v\n%s", err, output)
		}
		command = exec.Command(binary, "workspace", "validate", "--workspace", filepath.Join(kitRoot, "kits/workkit/testdata/corpora/native-only/workspace.json"), "--json")
		command.Env = []string{"PATH=" + filepath.Join(directory, "empty-path")}
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("extracted WorkKit offline validation: %v\n%s", err, output)
		}
	})
	t.Run("GoReleaseBuilder", func(t *testing.T) {
		command := exec.Command("go", "build", "-o", filepath.Join(directory, "aok-release"), "./cmd/aok-release")
		command.Dir = kitRoot
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("extracted kit release builder: %v\n%s", err, output)
		}
	})
}
