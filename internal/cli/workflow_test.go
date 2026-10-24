package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func copyWorkflowCorpus(t *testing.T) string {
	t.Helper()
	dst := t.TempDir()
	src := "../workflowcheck/testdata/corpus"
	err := filepath.WalkDir(src, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0700)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0600)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

func workflowTreeHashes(t *testing.T, root string) map[string][32]byte {
	t.Helper()
	result := map[string][32]byte{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		result[rel] = sha256.Sum256(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func mutateWorkflowKind(t *testing.T, root, kind string, mutate func(map[string]any)) {
	t.Helper()
	found := false
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".json") || found {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var doc map[string]any
		if json.Unmarshal(data, &doc) != nil || doc["kind"] != kind {
			return nil
		}
		mutate(doc)
		data, err = json.MarshalIndent(doc, "", "  ")
		if err != nil {
			return err
		}
		found = true
		return os.WriteFile(path, data, 0600)
	})
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatalf("fixture has no %s", kind)
	}
}

func TestWorkflowValidationOfflineAndUnchanged(t *testing.T) {
	root := copyWorkflowCorpus(t)
	before := workflowTreeHashes(t, root)
	var first bytes.Buffer
	for i := 0; i < 2; i++ {
		var out, stderr bytes.Buffer
		if code := Run([]string{"workflow", "validate", root, "--json"}, &out, &stderr); code != 0 {
			t.Fatalf("code %d: %s %s", code, &out, &stderr)
		}
		var report workflowReport
		if err := json.Unmarshal(out.Bytes(), &report); err != nil {
			t.Fatal(err)
		}
		if report.Status != "valid" || report.AuthorizationGranted || report.ExecutionReady || report.ResourceCounts["HostTaskBinding"] < 2 {
			t.Fatalf("unexpected report: %s", out.String())
		}
		if i == 0 {
			first.Write(out.Bytes())
		} else if !bytes.Equal(first.Bytes(), out.Bytes()) {
			t.Fatal("non-deterministic JSON report")
		}
	}
	var out, stderr bytes.Buffer
	if code := Run([]string{"workflow", "validate", root}, &out, &stderr); code != 0 || !strings.Contains(out.String(), "Workflow validation: valid") || !strings.Contains(out.String(), "Execution ready: false") {
		t.Fatalf("text: %d %s %s", code, &out, &stderr)
	}
	if !reflect.DeepEqual(before, workflowTreeHashes(t, root)) {
		t.Fatal("validator changed corpus")
	}
}

func TestWorkflowSemanticErrorsReachEveryEntrypoint(t *testing.T) {
	for _, tc := range []struct {
		name, kind string
		mutate     func(map[string]any)
	}{
		{"packet-hash", "WorkItemBinding", func(d map[string]any) { d["packet"].(map[string]any)["sha256"] = strings.Repeat("0", 64) }},
		{"attempt", "RunRecord", func(d map[string]any) { d["attemptId"] = "foreign-attempt" }},
		{"receipt", "SyncReceipt", func(d map[string]any) { d["operationDigest"] = strings.Repeat("0", 64) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := copyWorkflowCorpus(t)
			mutateWorkflowKind(t, root, tc.kind, tc.mutate)
			for _, command := range []string{"agent", "planning", "workflow"} {
				var out, stderr bytes.Buffer
				code := Run([]string{command, "validate", root, "--json"}, &out, &stderr)
				if code != 1 || !strings.Contains(out.String(), `"status": "invalid"`) || !strings.Contains(out.String(), "workflow.") {
					t.Fatalf("%s bypassed semantics: %d %s %s", command, code, &out, &stderr)
				}
			}
		})
	}
}

func TestWorkflowMisuseAndEmptyDomain(t *testing.T) {
	for _, args := range [][]string{
		{"workflow"}, {"workflow", "apply"}, {"workflow", "validate"},
		{"workflow", "validate", "one", "two"}, {"workflow", "validate", "one", "--out", "two"},
		{"workflow", "validate", "one", "--json", "--json"},
	} {
		var out, stderr bytes.Buffer
		if code := Run(args, &out, &stderr); code != 2 {
			t.Fatalf("%v: %d", args, code)
		}
	}
	for _, args := range [][]string{{"workflow", "--help"}, {"workflow", "validate", "--help"}} {
		var out, stderr bytes.Buffer
		if code := Run(args, &out, &stderr); code != 0 {
			t.Fatalf("%v: %d", args, code)
		}
	}
	for _, root := range []string{t.TempDir(), filepath.Join(t.TempDir(), "absent"), "../planning/testdata/corpus"} {
		var out, stderr bytes.Buffer
		if code := Run([]string{"workflow", "validate", root, "--json"}, &out, &stderr); code != 1 || !strings.Contains(out.String(), "workflow.corpus.empty") {
			t.Fatalf("%s: %d %s %s", root, code, &out, &stderr)
		}
	}
}

func TestWorkflowUnsafeAssetCannotEscape(t *testing.T) {
	for _, assetPath := range []string{"../escape", "/tmp/escape", `C:\\escape`, "assets/./packet.md"} {
		t.Run(assetPath, func(t *testing.T) {
			root := copyWorkflowCorpus(t)
			mutateWorkflowKind(t, root, "WorkItemBinding", func(d map[string]any) { d["packet"].(map[string]any)["path"] = assetPath })
			var out, stderr bytes.Buffer
			if code := Run([]string{"workflow", "validate", root, "--json"}, &out, &stderr); code != 1 {
				t.Fatalf("unsafe asset %q: %d %s %s", assetPath, code, &out, &stderr)
			}
		})
	}
}

func TestWorkflowRejectsSymlinkAsset(t *testing.T) {
	root := copyWorkflowCorpus(t)
	outside := filepath.Join(t.TempDir(), "packet.md")
	if err := os.WriteFile(outside, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "assets", "outside-link.md")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	mutateWorkflowKind(t, root, "WorkItemBinding", func(d map[string]any) {
		d["packet"].(map[string]any)["path"] = "assets/outside-link.md"
	})
	var out, stderr bytes.Buffer
	if code := Run([]string{"workflow", "validate", root, "--json"}, &out, &stderr); code != 1 || !strings.Contains(out.String(), "symlink") {
		t.Fatalf("symlink accepted: %d %s %s", code, &out, &stderr)
	}
}
