package decision

import (
	"encoding/json"
	"testing"
)

func contract(kind string) map[string]any {
	c := map[string]any{"metadata": map[string]any{"id": "decision", "version": "1.0.0"}, "decisionType": kind, "choices": []any{}, "evidenceRequired": true, "uncertainty": map[string]any{"allowAbstention": true, "calibrationRef": nil}}
	if kind == "choice" || kind == "ranking" {
		c["choices"] = []any{"a", "b"}
	}
	if kind == "score" {
		c["scoreRange"] = map[string]any{"min": json.Number("0"), "max": json.Number("10")}
	}
	return c
}
func result(value any) map[string]any {
	return map[string]any{"decisionRef": map[string]any{"kind": "DecisionContract", "id": "decision", "version": "1.0.0"}, "authorizationGranted": false, "status": "decided", "value": value, "evidence": []any{"evidence.txt"}, "confidence": nil, "confidenceMeaning": "unavailable"}
}
func TestTypedOutcomes(t *testing.T) {
	for _, tc := range []struct {
		kind      string
		good, bad any
	}{{"boolean", false, "false"}, {"choice", "a", "other"}, {"score", json.Number("10"), json.Number("10.01")}, {"ranking", []any{"b", "a"}, []any{"a", "a"}}} {
		t.Run(tc.kind, func(t *testing.T) {
			if ds := Check(contract(tc.kind), result(tc.good)); len(ds) > 0 {
				t.Fatal(ds)
			}
			if ds := Check(contract(tc.kind), result(tc.bad)); len(ds) == 0 {
				t.Fatal("invalid value accepted")
			}
		})
	}
}
func TestEvidenceAbstentionConfidence(t *testing.T) {
	c := contract("boolean")
	r := result(true)
	r["evidence"] = []any{}
	if len(Check(c, r)) == 0 {
		t.Fatal("missing evidence accepted")
	}
	r["status"] = "abstained"
	r["value"] = nil
	if ds := Check(c, r); len(ds) > 0 {
		t.Fatal(ds)
	}
	object(c["uncertainty"])["allowAbstention"] = false
	if len(Check(c, r)) == 0 {
		t.Fatal("forbidden abstention accepted")
	}
	c = contract("boolean")
	r = result(true)
	r["confidence"] = json.Number("0.8")
	r["confidenceMeaning"] = "calibrated"
	if len(Check(c, r)) == 0 {
		t.Fatal("uncalibrated claim accepted")
	}
	object(c["uncertainty"])["calibrationRef"] = "calibration.json"
	if ds := Check(c, r); len(ds) > 0 {
		t.Fatal(ds)
	}
	r["authorizationGranted"] = true
	if len(Check(c, r)) == 0 {
		t.Fatal("authorization accepted")
	}
}
func TestExactPinAndScoreBounds(t *testing.T) {
	c := contract("score")
	r := result(json.Number("1"))
	object(r["decisionRef"])["version"] = "2.0.0"
	if len(Check(c, r)) == 0 {
		t.Fatal("wrong version accepted")
	}
	c["scoreRange"] = map[string]any{"min": json.Number("9"), "max": json.Number("1")}
	if len(CheckContract(c)) == 0 {
		t.Fatal("reversed bounds accepted")
	}
}
