package usage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUsageFixturesAndStrictNestedJSON(t *testing.T) {
	root := "testdata"
	p, _, err := ReadPolicy(filepath.Join(root, "policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	e, _, err := Load(filepath.Join(root, "export.json"), filepath.Join(root, "policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = ValidateExport(e, p); err != nil {
		t.Fatal(err)
	}
	if len(e.Quotas) != 1 {
		t.Fatalf("Load did not retain fixture quota: got %d", len(e.Quotas))
	}
	b, err := os.ReadFile(filepath.Join(root, "binding.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = ValidateJSON(b, "binding.json"); err != nil {
		t.Fatal(err)
	}
	var binding Binding
	if err = Decode(b, &binding); err != nil {
		t.Fatal(err)
	}
	if err = ValidateBinding(binding); err != nil {
		t.Fatal(err)
	}
	d, err := os.ReadFile(filepath.Join(root, "demand.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = ValidateJSON(d, "demand.json"); err != nil {
		t.Fatal(err)
	}
	var demand Demand
	if err = Decode(d, &demand); err != nil {
		t.Fatal(err)
	}
	if err = ValidateDemand(demand); err != nil {
		t.Fatal(err)
	}
	bad := strings.Replace(string(b), `"id": "fixture-binding"`, `"id": "fixture-binding", "unexpected": true`, 1)
	if err = ValidateJSON([]byte(bad), "binding.json"); err == nil {
		t.Fatal("accepted unknown nested property")
	}
}
func TestUsageEvidenceHashIsChecked(t *testing.T) {
	root := t.TempDir()
	raw, _ := os.ReadFile("testdata/policy.json")
	if err := os.WriteFile(filepath.Join(root, "policy.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	e := EmptyExport()
	e.PolicySHA256 = Hash(raw)
	e.Evidence = []Asset{{Path: "evidence.json", SHA256: strings.Repeat("0", 64)}}
	if err := os.WriteFile(filepath.Join(root, "usage.json"), mustJSON(t, e), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "evidence.json"), []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(root, filepath.Join(root, "policy.json")); err == nil || !strings.Contains(err.Error(), "hash mismatch") {
		t.Fatalf("expected evidence hash failure, got %v", err)
	}
}
func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	return b
}

func TestValidateExportScopesEventIDsAndRejectsCrossScopeCorrections(t *testing.T) {
	p, _, err := ReadPolicy("testdata/policy.json")
	if err != nil {
		t.Fatal(err)
	}
	e, _, err := Load("testdata/export.json", "testdata/policy.json")
	if err != nil {
		t.Fatal(err)
	}
	base := Event{ID: "same-id", Origin: e.Quotas[0].Origin, BindingID: e.Bindings[0].ID, Metric: "requests", Unit: "requests", Value: json.Number("1"), Semantics: "incremental", Billable: true, DedupeCoverage: "strong"}
	a := base
	a.SessionID = "session-a"
	b := base
	b.SessionID = "session-b"
	e.Events = []Event{a, b}
	if err = ValidateExport(e, p); err != nil {
		t.Fatalf("same event ID in distinct source scopes should be retained: %v", err)
	}
	correction := a
	correction.ID = "correction"
	correction.Origin.SourceID = "other-source"
	correction.CorrectionOf = &a.ID
	e.Events = []Event{a, correction}
	if err = ValidateExport(e, p); err == nil || !strings.Contains(err.Error(), "origin does not match binding") {
		t.Fatalf("expected cross-source correction to be rejected, got %v", err)
	}
	correction = a
	correction.ID = "correction"
	correction.CorrectionOf = &correction.ID
	e.Events = []Event{a, correction}
	if err = ValidateExport(e, p); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("expected correction cycle rejection, got %v", err)
	}
}
func TestValidateExportRejectsUnsafeQuotaOrigin(t *testing.T) {
	p, _, err := ReadPolicy("testdata/policy.json")
	if err != nil {
		t.Fatal(err)
	}
	e, _, err := Load("testdata/export.json", "testdata/policy.json")
	if err != nil {
		t.Fatal(err)
	}
	e.Quotas[0].Origin.Host = "codex\x1b[31m"
	if err = ValidateExport(e, p); err == nil {
		t.Fatal("accepted terminal control in quota origin")
	}
}

func TestQuotaOverLimitAndUnknownFreshnessRemainValidEvidence(t *testing.T) {
	p, _, err := ReadPolicy("testdata/policy.json")
	if err != nil {
		t.Fatal(err)
	}
	e, _, err := Load("testdata/export.json", "testdata/policy.json")
	if err != nil {
		t.Fatal(err)
	}
	neg := json.Number("-5.25")
	used := json.Number("105.25")
	q := &e.Quotas[0]
	q.Remaining = &neg
	q.Used = &used
	q.Origin.FreshnessKnown = false
	q.Origin.ObservedAt = q.Origin.ReceivedAt
	if err = ValidateExport(e, p); err != nil {
		t.Fatalf("over-limit observation or unknown freshness rejected: %v", err)
	}
	files, err := Files(e)
	if err != nil {
		t.Fatal(err)
	}
	if err = ValidateJSON(files["usage.json"], "export.json"); err != nil {
		t.Fatalf("closed export schema rejected signed remaining: %v", err)
	}
	invalid := json.Number("-1")
	q.Used = &invalid
	files, err = Files(e)
	if err == nil {
		t.Fatal("writer accepted an export with negative used value")
	}
}

func TestExactNumberValidationRejectsNonJSONAndUnboundedLexemes(t *testing.T) {
	for _, lexeme := range []string{"1/3", "0x10", "NaN", "1e1001", "1e-1001", "1e1000000000"} {
		if _, err := NumberRat(json.Number(lexeme)); err == nil {
			t.Errorf("NumberRat accepted malformed or unbounded lexeme %q", lexeme)
		}
	}
	if _, err := NumberRat(json.Number("1.25e3")); err != nil {
		t.Fatalf("valid bounded decimal rejected: %v", err)
	}
	p, _, err := ReadPolicy("testdata/policy.json")
	if err != nil {
		t.Fatal(err)
	}
	e, _, err := Load("testdata/export.json", "testdata/policy.json")
	if err != nil {
		t.Fatal(err)
	}
	bad := json.Number("1/3")
	e.Quotas[0].Remaining = &bad
	if err = ValidateExport(e, p); err == nil {
		t.Fatal("ValidateExport accepted non-JSON rational")
	}
	d := Demand{Version: Version, ConfigurationID: "opaque-config", ProfileSHA256: strings.Repeat("a", 64), Source: "test", ValidUntil: "2026-10-23T12:01:00Z", Amounts: []Amount{{ConstraintID: e.Bindings[0].ConstraintIDs[0], Generation: "g", Unit: "requests", Amount: json.Number("1e1000000000")}}}
	if err = ValidateDemand(d); err == nil {
		t.Fatal("ValidateDemand accepted unbounded exponent")
	}
}

func TestQuotaCaptureStreamIdentityMustBeUnambiguous(t *testing.T) {
	e, p, err := Load("testdata/export.json", "testdata/policy.json")
	if err != nil {
		t.Fatal(err)
	}
	b := e.Bindings[0]
	b.ID = "other-binding"
	b.StreamID = "other-stream"
	e.Bindings = append(e.Bindings, b)
	if err = ValidateExport(e, p); err == nil {
		t.Fatal("source-local quota ordinal could name two independent streams")
	}
}
