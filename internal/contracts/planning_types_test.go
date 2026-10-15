package contracts

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestPlanningFixturesParseAndTypedRoundTrip(t *testing.T) {
	fixtures := []string{"goal-contract.json", "plan-definition.json", "plan-revision.json", "decision-contract.json", "decision-result.json", "optimization-experiment.json", "sdlc-traceability.json", "planning-evidence.json"}
	for _, name := range fixtures {
		t.Run(name, func(t *testing.T) {
			input, err := os.ReadFile(filepath.Join("testdata", name))
			if err != nil {
				t.Fatal(err)
			}
			parsed, diagnostics, err := Parse(input)
			if err != nil {
				t.Fatalf("Parse: %v (%+v)", err, diagnostics)
			}
			resource := parsed.(Resource)
			value, err := DecodeTyped(resource)
			if err != nil {
				t.Fatalf("DecodeTyped: %v", err)
			}
			encoded, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			roundtrip, diagnostics, err := Parse(encoded)
			if err != nil {
				t.Fatalf("round-trip Parse: %v (%+v)\n%s", err, diagnostics, encoded)
			}
			again, err := DecodeTyped(roundtrip.(Resource))
			if err != nil {
				t.Fatalf("round-trip DecodeTyped: %v", err)
			}
			firstJSON, _ := json.Marshal(value)
			againJSON, _ := json.Marshal(again)
			if !reflect.DeepEqual(jsonValue(t, firstJSON), jsonValue(t, againJSON)) {
				t.Fatalf("typed round trip lost fields\nfirst: %s\nagain: %s", firstJSON, againJSON)
			}
		})
	}
}

func jsonValue(t *testing.T, data []byte) any {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestDecisionResultKeepsRawJSONAndNumberPrecision(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "decision-result.json"))
	if err != nil {
		t.Fatal(err)
	}
	parsed, _, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeTyped(parsed.(Resource))
	if err != nil {
		t.Fatal(err)
	}
	result := got.(DecisionResult)
	if string(result.Value) != `["overview", "deep", 1.0000000000000000001]` {
		t.Fatalf("value precision lost: %s", result.Value)
	}
	if result.Confidence == nil || result.Confidence.String() != "0.123456789012345678901" {
		t.Fatalf("confidence precision lost: %#v", result.Confidence)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(encoded, []byte("1.0000000000000000001")) || !bytes.Contains(encoded, []byte("0.123456789012345678901")) {
		t.Fatalf("exact numeric lexemes missing from encoding: %s", encoded)
	}
}

func TestPlanningSchemaRejectsUnknownAndMissingPinnedFields(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "plan-definition.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	doc["unexpected"] = true
	bad, _ := json.Marshal(doc)
	if _, _, err := Parse(bad); err == nil {
		t.Fatal("unknown top-level field accepted")
	}
	delete(doc, "unexpected")
	steps := doc["steps"].([]any)
	delete(steps[0].(map[string]any), "verifierRef")
	bad, _ = json.Marshal(doc)
	if _, _, err := Parse(bad); err == nil {
		t.Fatal("step without pinned verifier accepted")
	}
}

func TestPlanningEvidenceRequiresPinnedHashAndExplicitNullPredecessor(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "planning-evidence.json"))
	if err != nil {
		t.Fatal(err)
	}
	parsed, _, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if got := parsed.(Resource).Envelope.Kind; got != "PlanningEvidence" {
		t.Fatalf("kind=%s", got)
	}
	revData, err := os.ReadFile(filepath.Join("testdata", "plan-revision.json"))
	if err != nil {
		t.Fatal(err)
	}
	revAny, _, err := Parse(revData)
	if err != nil {
		t.Fatal(err)
	}
	rev, err := DecodeTyped(revAny.(Resource))
	if err != nil {
		t.Fatal(err)
	}
	if rev.(PlanRevision).Predecessor != nil {
		t.Fatal("first revision predecessor should remain explicitly null")
	}
}

func TestPlanningCountersBoundedWithoutFloatCoercion(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "planning-evidence.json"))
	if err != nil {
		t.Fatal(err)
	}
	value, err := DecodeJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	doc := value.(map[string]any)
	doc["attemptsUsed"] = json.Number("1e0")
	b, _ := json.Marshal(doc)
	if _, _, err = Parse(b); err != nil {
		t.Fatal(err)
	}
	doc["attemptsUsed"] = json.Number("2147483648")
	b, _ = json.Marshal(doc)
	if _, _, err = Parse(b); err == nil {
		t.Fatal("out-of-range counter accepted")
	}
}
