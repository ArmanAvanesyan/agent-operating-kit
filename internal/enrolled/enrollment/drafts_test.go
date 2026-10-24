package enrollment

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFilesAreDeterministicClosedAndHashAllImmutableDrafts(t *testing.T) {
	a, err := Files()
	if err != nil {
		t.Fatal(err)
	}
	b, err := Files()
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != len(b) {
		t.Fatalf("file counts differ: %d != %d", len(a), len(b))
	}
	for p, data := range a {
		if string(data) != string(b[p]) {
			t.Fatalf("%s is not reproducible", p)
		}
	}
	for p, data := range a {
		if strings.HasPrefix(p, "drafts/") {
			var w map[string]any
			if err := json.Unmarshal(data, &w); err != nil {
				t.Fatalf("%s invalid JSON: %v", p, err)
			}
			if w["executable"] != false || w["version"] != Version || w["kind"] == "" || w["instructions"] == "" {
				t.Fatalf("%s missing explicit closed wrapper: %#v", p, w)
			}
		}
	}
	var manifest Manifest
	if err := json.Unmarshal(a["manifest.json"], &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Purpose != checkPurpose {
		t.Fatalf("manifest purpose %q does not match compiled enrollment purpose", manifest.Purpose)
	}
	if len(manifest.Required) != len(Requirements()) {
		t.Fatal("compiled requirements not preserved")
	}
	if len(manifest.Drafts) == 0 {
		t.Fatal("manifest has no immutable drafts")
	}
	for _, asset := range manifest.Drafts {
		data, ok := a[asset.Path]
		if !ok {
			t.Fatalf("manifest references missing %s", asset.Path)
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != asset.SHA256 {
			t.Fatalf("hash mismatch for %s", asset.Path)
		}
	}
	if _, ok := a["operator-inputs.json"]; !ok {
		t.Fatal("operator inventory missing")
	}
	for _, n := range Requirements() {
		if _, ok := a[n.Path]; ok {
			t.Fatalf("required live input unexpectedly present: %s", n.Path)
		}
	}
	for p, data := range a {
		if strings.HasPrefix(p, "drafts/") {
			var v any
			if err := json.Unmarshal(data, &v); err != nil {
				t.Fatal(err)
			}
			if hasNonemptySignature(v) {
				t.Fatalf("%s contains a nonempty signature", p)
			}
		}
	}
}

func hasNonemptySignature(v any) bool {
	switch x := v.(type) {
	case map[string]any:
		for k, child := range x {
			if strings.EqualFold(k, "signature") {
				if s, ok := child.(string); ok && s != "" {
					return true
				}
			}
			if hasNonemptySignature(child) {
				return true
			}
		}
	case []any:
		for _, child := range x {
			if hasNonemptySignature(child) {
				return true
			}
		}
	}
	return false
}

func TestPrepareUsesCreateOnlyDirectoryAndRejectsSymlinkParent(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(root, "out")
	if err := Prepare(out); err != nil {
		t.Fatal(err)
	}
	report, err := Check(out, time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if report.Findings[0].ID == "" {
		t.Fatal("checker returned no findings")
	}
	validManifest, validDrafts := false, 0
	for _, f := range report.Findings {
		if f.ID == "manifest" && f.Status == "valid" {
			validManifest = true
		}
		if f.ID == "draft" && f.Status == "valid" {
			validDrafts++
		}
	}
	var preparedManifest Manifest
	manifestBytes, err := os.ReadFile(filepath.Join(out, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(manifestBytes, &preparedManifest); err != nil {
		t.Fatal(err)
	}
	if !validManifest || validDrafts != len(preparedManifest.Drafts) {
		t.Fatalf("checker did not validate all immutable drafts: manifest=%v drafts=%d/%d", validManifest, validDrafts, len(preparedManifest.Drafts))
	}
	if err := Prepare(out); err == nil {
		t.Fatal("Prepare overwrote an existing output")
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	if err := Prepare(filepath.Join(link, "linked-out")); err == nil {
		t.Fatal("Prepare accepted a symlink parent")
	}
}
