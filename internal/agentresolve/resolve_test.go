package agentresolve

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func fixtureRoot() string { return filepath.Join("testdata", "corpus") }

func TestResolveDeterministicAndHashSensitive(t *testing.T) {
	root := fixtureRoot()
	first := Resolve(filepath.Join(root, "assignment.json"), root)
	if first.Status != "blocked" || first.Snapshot == nil {
		t.Fatalf("expected blocked static snapshot, got %+v", first)
	}
	if len(first.Snapshot.Sources) == 0 || first.Snapshot.Digest == "" {
		t.Fatalf("missing source manifest/digest: %+v", first.Snapshot)
	}
	implicitRoot := Resolve(filepath.Join(root, "assignment.json"), "")
	if implicitRoot.Snapshot == nil || implicitRoot.Snapshot.Digest != first.Snapshot.Digest {
		t.Fatalf("default corpus root differs from assignment parent: %+v", implicitRoot)
	}
	if _, ok := first.Snapshot.Inputs["depth"]; !ok {
		t.Fatalf("assignment input missing from merged inputs: %+v", first.Snapshot.Inputs)
	}
	if _, ok := first.Snapshot.OutputParameters["includeEvidence"]; !ok {
		t.Fatalf("output parameter missing: %+v", first.Snapshot.OutputParameters)
	}
	if !hasDiagnostic(first, "resolve.readiness.unverified") || !hasDiagnostic(first, "resolve.compatibility.unverified") {
		t.Fatalf("expected explicit readiness and compatibility blockers: %+v", first.Diagnostics)
	}
	if first.Snapshot.Permissions.Network.Mode != "denied" {
		t.Fatalf("environment network ceiling not applied: %#v", first.Snapshot.Permissions.Network)
	}
	if len(first.Snapshot.ReadinessEvidence) < 5 || len(first.Snapshot.CompatibilityEvidence) < 4 {
		t.Fatalf("missing per-control blocked evidence: %+v / %+v", first.Snapshot.ReadinessEvidence, first.Snapshot.CompatibilityEvidence)
	}

	other := t.TempDir()
	copyTree(t, root, other)
	second := Resolve(filepath.Join(other, "assignment.json"), other)
	if second.Snapshot == nil || second.Snapshot.Digest != first.Snapshot.Digest {
		t.Fatalf("root location changed snapshot: %+v", second)
	}
	if err := os.WriteFile(filepath.Join(other, "assets", "skills", "project.md"), []byte("modified skill"), 0644); err != nil {
		t.Fatal(err)
	}
	third := Resolve(filepath.Join(other, "assignment.json"), other)
	if third.Snapshot == nil || third.Snapshot.Digest == first.Snapshot.Digest {
		t.Fatal("asset mutation did not change digest")
	}
}

func TestResolveRejectsMissingReferencesAndInvalidInputs(t *testing.T) {
	for _, tc := range []struct{ name, old, replacement string }{
		{"missing agent", `"id": "repository-explorer"`, `"id": "missing-agent"`},
		{"invalid input", `"depth": "deep"`, `"depth": "unknown"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			copyTree(t, fixtureRoot(), root)
			path := filepath.Join(root, "assignment.json")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			data = bytes.Replace(data, []byte(tc.old), []byte(tc.replacement), 1)
			if err = os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			report := Resolve(path, root)
			if report.Status != "invalid" || report.Snapshot != nil {
				t.Fatalf("expected invalid report: %+v", report)
			}
		})
	}
}

func TestResolveRejectsMissingAndSymlinkAssignment(t *testing.T) {
	root := fixtureRoot()
	missing := Resolve(filepath.Join(root, "missing.json"), root)
	if missing.Status != "invalid" || missing.Snapshot != nil {
		t.Fatalf("missing assignment: %+v", missing)
	}
	link := filepath.Join(t.TempDir(), "linked.json")
	if err := os.Symlink(filepath.Join(root, "assignment.json"), link); err != nil {
		t.Fatal(err)
	}
	unsafe := Resolve(link, root)
	if unsafe.Status != "invalid" || unsafe.Snapshot != nil {
		t.Fatalf("symlink assignment: %+v", unsafe)
	}
}

func TestResolveRejectsCorpusSymlink(t *testing.T) {
	root := t.TempDir()
	copyTree(t, fixtureRoot(), root)
	if err := os.Symlink(filepath.Join(root, "agent.json"), filepath.Join(root, "agent-link.json")); err != nil {
		t.Fatal(err)
	}
	report := Resolve(filepath.Join(root, "assignment.json"), root)
	if report.Status != "invalid" || report.Snapshot != nil {
		t.Fatalf("expected symlink rejection: %+v", report)
	}
}

func TestResolveRejectsFileSizeBound(t *testing.T) {
	root := t.TempDir()
	copyTree(t, fixtureRoot(), root)
	if err := os.WriteFile(filepath.Join(root, "assets", "too-large.bin"), make([]byte, maxFile+1), 0600); err != nil {
		t.Fatal(err)
	}
	report := Resolve(filepath.Join(root, "assignment.json"), root)
	if report.Status != "invalid" || report.Snapshot != nil {
		t.Fatalf("expected file size rejection: %+v", report)
	}
}

func TestResolveRejectsFileCountBound(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "assignment.json"), []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < maxFiles; i++ {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("support-%03d.txt", i)), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	report := Resolve(filepath.Join(root, "assignment.json"), root)
	if report.Status != "invalid" || report.Snapshot != nil {
		t.Fatalf("expected file count rejection: %+v", report)
	}
}

func hasDiagnostic(report Report, code string) bool {
	for _, diagnostic := range report.Diagnostics {
		if diagnostic.Code == code {
			return true
		}
	}
	return false
}

func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		to := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(to, 0755)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err = os.MkdirAll(filepath.Dir(to), 0755); err != nil {
			return err
		}
		return os.WriteFile(to, b, 0644)
	})
	if err != nil {
		t.Fatal(err)
	}
}
