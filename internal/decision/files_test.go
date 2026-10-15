package decision

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestFileValidationAndEvidenceConfinement(t *testing.T) {
	root := t.TempDir()
	load := func(name string) map[string]any {
		b, e := os.ReadFile(filepath.Join("..", "contracts", "testdata", name))
		if e != nil {
			t.Fatal(e)
		}
		var v map[string]any
		if e = json.Unmarshal(b, &v); e != nil {
			t.Fatal(e)
		}
		return v
	}
	c := load("decision-contract.json")
	r := load("decision-result.json")
	c["inputSchema"] = "input.schema.json"
	r["value"] = nil
	r["confidence"] = nil
	write := func(name string, v any) {
		b, e := json.Marshal(v)
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(filepath.Join(root, name), b, 0600); e != nil {
			t.Fatal(e)
		}
	}
	write("contract.json", c)
	write("result.json", r)
	write("input.schema.json", map[string]any{"type": "object"})
	if report := ValidateFiles(filepath.Join(root, "contract.json"), filepath.Join(root, "result.json")); report.Status != "valid" {
		t.Fatal(report)
	}
	r["evidence"] = []any{"../outside.txt"}
	write("result.json", r)
	if report := ValidateFiles(filepath.Join(root, "contract.json"), filepath.Join(root, "result.json")); report.Status != "invalid" {
		t.Fatal("escaped evidence accepted")
	}
}
