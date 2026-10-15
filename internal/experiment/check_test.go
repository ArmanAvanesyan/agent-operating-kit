package experiment

import (
	"strings"
	"testing"
)

func fixture() map[string]any {
	return map[string]any{"mutableFields": []any{"instructions.sources"}, "protectedFields": []any{}, "evaluator": map[string]any{"immutable": true}, "promotion": map[string]any{"automatic": false}, "datasets": map[string]any{"train": []any{"a@" + strings.Repeat("a", 64)}, "validation": []any{"b@" + strings.Repeat("b", 64)}, "test": []any{"c@" + strings.Repeat("c", 64)}}}
}
func TestBoundaries(t *testing.T) {
	if ds := Check(fixture()); len(ds) > 0 {
		t.Fatal(ds)
	}
	for _, field := range []string{"permissions", "permissions.filesystem", "identity", "evaluator.ref", "datasets", "instructions.*"} {
		t.Run(field, func(t *testing.T) {
			d := fixture()
			d["mutableFields"] = []any{field}
			if len(Check(d)) == 0 {
				t.Fatal("unsafe field accepted")
			}
		})
	}
	for _, split := range []string{"validation", "test"} {
		t.Run(split, func(t *testing.T) {
			d := fixture()
			obj(d["datasets"])[split] = []any{"a@" + strings.Repeat("f", 64)}
			if len(Check(d)) == 0 {
				t.Fatal("dataset identity overlap accepted")
			}
			obj(d["datasets"])[split] = []any{"different@" + strings.Repeat("a", 64)}
			if len(Check(d)) == 0 {
				t.Fatal("dataset content overlap accepted")
			}
		})
	}
}
