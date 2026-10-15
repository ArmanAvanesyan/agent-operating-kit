package cli

import (
	"bytes"
	"testing"
)

func TestPlanningCommandMisuse(t *testing.T) {
	for _, args := range [][]string{{"planning"}, {"planning", "validate"}, {"planning", "ready", "plan"}, {"planning", "validate", "a", "b"}, {"planning", "ready", "plan", "--evidence", "e", "--as-of"}, {"decision"}, {"decision", "validate-result", "one"}} {
		var out, err bytes.Buffer
		if code := Run(args, &out, &err); code != 2 {
			t.Fatalf("%v: %d %s %s", args, code, &out, &err)
		}
	}
}
func TestPlanningAndDecisionInvalidInput(t *testing.T) {
	for _, args := range [][]string{{"planning", "validate", t.TempDir(), "--json"}, {"decision", "validate-result", "missing-contract", "missing-result", "--json"}} {
		var out, err bytes.Buffer
		if code := Run(args, &out, &err); code != 1 || !bytes.Contains(out.Bytes(), []byte(`"status": "invalid"`)) {
			t.Fatalf("%v: %d %s %s", args, code, &out, &err)
		}
	}
}

func TestPlanningReadySyntheticCorpus(t *testing.T) {
	for _, tc := range []struct {
		at   string
		code int
		word string
	}{{"2026-10-16T12:00:00Z", 0, `"inspect"`}, {"2026-10-17T00:00:00Z", 1, `"blocked"`}} {
		var out, err bytes.Buffer
		args := []string{"planning", "ready", "../planning/testdata/corpus/plan.json", "--evidence", "../planning/testdata/corpus/evidence.json", "--as-of", tc.at, "--json"}
		if code := Run(args, &out, &err); code != tc.code || !bytes.Contains(out.Bytes(), []byte(tc.word)) {
			t.Fatalf("%v: %d %s %s", args, code, &out, &err)
		}
	}
}
