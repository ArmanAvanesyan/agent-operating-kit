// Package decision validates declared outcomes without granting authorization.
package decision

import (
	"encoding/json"
	"math/big"
	"reflect"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
)

func object(v any) map[string]any { m, _ := v.(map[string]any); return m }
func list(v any) []any            { a, _ := v.([]any); return a }
func text(v any) string           { s, _ := v.(string); return s }
func diagnostic(code, field, message string) contracts.Diagnostic {
	return contracts.Diagnostic{Code: code, Field: field, Severity: "error", Message: message}
}
func number(v any) (*big.Rat, bool) {
	n, ok := v.(json.Number)
	if !ok {
		return nil, false
	}
	r, ok := new(big.Rat).SetString(string(n))
	return r, ok
}

// CheckContract adds semantic constraints beyond the closed shape schema.
func CheckContract(c map[string]any) []contracts.Diagnostic {
	ds := []contracts.Diagnostic{}
	kind := text(c["decisionType"])
	choices := list(c["choices"])
	if kind == "choice" || kind == "ranking" {
		if len(choices) == 0 {
			ds = append(ds, diagnostic("decision.choices", "choices", "choice and ranking decisions require alternatives"))
		}
	} else if len(choices) > 0 {
		ds = append(ds, diagnostic("decision.choices", "choices", "boolean and score decisions cannot declare alternatives"))
	}
	score := object(c["scoreRange"])
	if kind == "score" {
		min, a := number(score["min"])
		max, b := number(score["max"])
		if !a || !b || min.Cmp(max) > 0 {
			ds = append(ds, diagnostic("decision.score-range", "scoreRange", "score range requires numeric min <= max"))
		}
	} else if score != nil {
		ds = append(ds, diagnostic("decision.score-range", "scoreRange", "score range is only meaningful for score decisions"))
	}
	return ds
}

// Check assumes both documents passed operational shape validation. Evidence
// locators are checked by the caller; this function validates their necessity.
func Check(c, r map[string]any) []contracts.Diagnostic {
	ds := CheckContract(c)
	add := func(code, field, message string) { ds = append(ds, diagnostic(code, field, message)) }
	meta := object(c["metadata"])
	expected := map[string]any{"kind": "DecisionContract", "id": meta["id"], "version": meta["version"]}
	if !reflect.DeepEqual(object(r["decisionRef"]), expected) {
		add("decision.reference", "decisionRef", "result must pin this exact decision contract")
	}
	if r["authorizationGranted"] != false {
		add("decision.authorization", "authorizationGranted", "decision results never grant authorization")
	}
	status := text(r["status"])
	uncertainty := object(c["uncertainty"])
	if status != "decided" {
		if r["value"] != nil {
			add("decision.value", "value", "abstained and failed outcomes require null value")
		}
		if r["confidence"] != nil || r["confidenceMeaning"] != "unavailable" {
			add("decision.confidence", "confidence", "non-decisions cannot report decision confidence")
		}
		if status == "abstained" && uncertainty["allowAbstention"] != true {
			add("decision.abstention", "status", "contract forbids abstention")
		}
		if status != "abstained" && status != "failed" {
			add("decision.status", "status", "unknown result status")
		}
		return ds
	}
	if c["evidenceRequired"] == true && len(list(r["evidence"])) == 0 {
		add("decision.evidence", "evidence", "decided outcome requires supporting evidence; missing evidence requires abstention")
	}
	switch text(c["decisionType"]) {
	case "boolean":
		if _, ok := r["value"].(bool); !ok {
			add("decision.value", "value", "boolean decision requires true or false")
		}
	case "choice":
		found := false
		for _, choice := range list(c["choices"]) {
			if reflect.DeepEqual(choice, r["value"]) {
				found = true
			}
		}
		if !found {
			add("decision.value", "value", "choice is not a declared alternative")
		}
	case "score":
		n, ok := number(r["value"])
		bounds := object(c["scoreRange"])
		min, a := number(bounds["min"])
		max, b := number(bounds["max"])
		if !ok || !a || !b || n.Cmp(min) < 0 || n.Cmp(max) > 0 {
			add("decision.value", "value", "score is outside the declared numeric range")
		}
	case "ranking":
		values, ok := r["value"].([]any)
		allowed := map[string]bool{}
		for _, v := range list(c["choices"]) {
			allowed[text(v)] = true
		}
		valid := ok && len(values) == len(allowed)
		seen := map[string]bool{}
		for _, v := range values {
			s, ok := v.(string)
			if !ok || !allowed[s] || seen[s] {
				valid = false
			}
			seen[s] = true
		}
		if !valid {
			add("decision.value", "value", "ranking must contain each declared alternative exactly once")
		}
	default:
		add("decision.type", "decisionType", "unknown decision type")
	}
	confidence := r["confidence"]
	meaning := text(r["confidenceMeaning"])
	if confidence == nil {
		if meaning != "unavailable" {
			add("decision.confidence", "confidenceMeaning", "null confidence requires unavailable meaning")
		}
	} else {
		n, ok := number(confidence)
		if !ok || n.Sign() < 0 || n.Cmp(big.NewRat(1, 1)) > 0 {
			add("decision.confidence", "confidence", "confidence must be between zero and one")
		}
		switch meaning {
		case "provider-reported-uncalibrated":
			if text(r["observedProvider"]) == "" {
				add("decision.confidence", "observedProvider", "provider-reported confidence requires observed provider")
			}
		case "calibrated":
			if text(uncertainty["calibrationRef"]) == "" {
				add("decision.confidence", "confidenceMeaning", "calibrated confidence requires a calibration artifact reference")
			}
		default:
			add("decision.confidence", "confidenceMeaning", "numeric confidence requires an explicit confidence interpretation")
		}
	}
	return ds
}
