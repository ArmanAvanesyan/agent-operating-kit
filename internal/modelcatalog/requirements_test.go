package modelcatalog

import (
	"encoding/json"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"testing"
	"time"
)

func TestRequirementTrustScopeFreshnessConflict(t *testing.T) {
	at := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	scope := Configuration{Host: String("codex"), HostVersion: String("1.2.3"), Provider: String("acme"), ModelID: String("x"), ReasoningMode: String("effort"), Effort: String("medium")}
	profile := contracts.ExecutionProfile{Client: contracts.ClientSettings{Adapter: "codex", VersionConstraint: scope.HostVersion}, Model: contracts.ModelSettings{Selector: "x", Provider: scope.Provider, Reasoning: contracts.ReasoningSettings{Mode: "effort", Effort: scope.Effort}}}
	assertion := Assertion{ID: "a", SourceID: "s", EntryID: "e", Dimension: "toolUse", Value: json.RawMessage(`true`), Category: Published, Scope: scope, ObservedAt: at.Add(-time.Minute).Format(time.RFC3339), ExpiresAt: at.Add(time.Hour).Format(time.RFC3339), Polarity: "supports"}
	makeCapture := func(a []Assertion) *Capture {
		return trustedCapture(t, Catalog{Sources: []Source{{ID: "s", Artifact: Asset{Path: "artifact.json", SHA256: Hash([]byte("raw"))}}}, Assertions: a})
	}
	req := []Requirement{{"toolUse", json.RawMessage(`true`)}}
	c := makeCapture([]Assertion{assertion})
	if got := CheckRequirements(c, profile, "", req, at)[0].Status; got != "supported" {
		t.Fatal(got)
	}
	neg := assertion
	neg.ID = "negative"
	neg.Polarity = "contradicts"
	c = makeCapture([]Assertion{assertion, neg})
	if got := CheckRequirements(c, profile, "", req, at)[0].Status; got != "unknown" {
		t.Fatal("conflict", got)
	}
	c = makeCapture([]Assertion{neg})
	if got := CheckRequirements(c, profile, "", req, at)[0].Status; got != "unsupported" {
		t.Fatal(got)
	}
	assertion.ExpiresAt = at.Format(time.RFC3339)
	c = makeCapture([]Assertion{assertion})
	if got := CheckRequirements(c, profile, "", req, at)[0].Status; got != "unknown" {
		t.Fatal("expired", got)
	}
	assertion.ExpiresAt = at.Add(time.Hour).Format(time.RFC3339)
	assertion.Category = Derived
	c = makeCapture([]Assertion{assertion})
	if got := CheckRequirements(c, profile, "", req, at)[0].Status; got != "unknown" {
		t.Fatal("guidance promoted", got)
	}
	if got := CheckRequirements(c, profile, "", []Requirement{{"namedTools", json.RawMessage(`["shell"]`)}}, at)[0].Status; got != "unknown" {
		t.Fatal("unscoped namedtools promoted", got)
	}
}
