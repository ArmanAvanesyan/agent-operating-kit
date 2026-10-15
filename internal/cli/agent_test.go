package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestAgentValidationUsage(t *testing.T) {
	for _, args := range [][]string{{"agent"}, {"agent", "native"}, {"agent", "validate"}, {"agent", "validate", "--bad"}, {"agent", "validate", "a", "b"}} {
		var out, err bytes.Buffer
		if code := Run(args, &out, &err); code != 2 {
			t.Fatalf("%v: code %d, want misuse 2", args, code)
		}
	}
	var out, err bytes.Buffer
	if code := Run([]string{"agent", "--help"}, &out, &err); code != 0 {
		t.Fatal(code)
	}
}

func TestAgentValidationInvalidReport(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bad.json"), []byte(`{"apiVersion":"aok/v1alpha1","apiVersion":"aok/v1alpha1"}`), 0600); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	if code := Run([]string{"agent", "validate", dir, "--json"}, &out, &stderr); code != 1 {
		t.Fatalf("code %d: %s %s", code, &out, &stderr)
	}
	var report struct {
		Status      string
		Diagnostics []struct{ Code, Source, Field, Severity, Message string }
	}
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Status != "invalid" || len(report.Diagnostics) == 0 || report.Diagnostics[0].Code == "" || report.Diagnostics[0].Source == "" {
		t.Fatalf("incomplete diagnostic: %s", &out)
	}
}

func TestAgentValidationMissingDirectory(t *testing.T) {
	var out, stderr bytes.Buffer
	if code := Run([]string{"agent", "validate", filepath.Join(t.TempDir(), "absent"), "--json"}, &out, &stderr); code != 1 {
		t.Fatalf("code %d: %s %s", code, &out, &stderr)
	}
}

func TestAgentValidationValidStaticCorpus(t *testing.T) {
	data, err := os.ReadFile("../contracts/testdata/context-policy.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err = json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	doc["requiredDocuments"] = []string{}
	data, err = json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err = os.WriteFile(filepath.Join(dir, "context.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	if code := Run([]string{"agent", "validate", "--json", dir}, &out, &stderr); code != 0 {
		t.Fatalf("code %d: %s %s", code, &out, &stderr)
	}
	var report struct{ Status string }
	if err = json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Status != "valid" {
		t.Fatalf("unexpected report: %s", &out)
	}
}
