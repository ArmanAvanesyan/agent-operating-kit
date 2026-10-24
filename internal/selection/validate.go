package selection

import (
	"encoding/json"
	"fmt"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
	"math/big"
	"time"
)

func validateInputs(t TaskProfile, m CandidateManifest, p SelectionPolicy) error {
	if t.Version != Version || m.Version != Version || p.Version != Version {
		return fmt.Errorf("unsupported selection version")
	}
	if t.TaskClass == "" || t.Language == "" || (t.Risk != "standard" && t.Risk != "high") || t.Currency == "" || !digestPattern.MatchString(t.ProtocolSHA256) {
		return fmt.Errorf("explicit task class/language/risk/currency/protocol required")
	}
	for _, n := range []string{string(t.ContextTokens), string(t.OutputTokens), string(t.SpendLimit), string(p.StandardFloor), string(p.HighRiskFloor)} {
		if n == "" {
			return fmt.Errorf("explicit numeric budgets/floors required")
		}
	}
	for _, n := range []struct{ v string }{{string(t.ContextTokens)}, {string(t.OutputTokens)}, {string(t.SpendLimit)}} {
		r, ok := newRat(n.v)
		if !ok || r.Sign() < 0 {
			return fmt.Errorf("invalid task budget")
		}
	}
	if _, e := time.Parse(time.RFC3339Nano, t.Deadline); e != nil {
		return fmt.Errorf("explicit deadline required")
	}
	if p.DefaultsVersion != "aok-selection-defaults/v1" {
		return fmt.Errorf("unsupported policy defaults version")
	}
	if p.Priority != "quality" && p.Priority != "cost" && p.Priority != "capacity" && p.Priority != "deadline" {
		return fmt.Errorf("unknown priority")
	}
	sf, ok := newRat(string(p.StandardFloor))
	hf, ok2 := newRat(string(p.HighRiskFloor))
	if !ok || !ok2 || sf.Cmp(ratNumber("0.80")) < 0 || hf.Cmp(ratNumber("0.95")) < 0 || sf.Cmp(ratNumber("1")) > 0 || hf.Cmp(ratNumber("1")) > 0 {
		return fmt.Errorf("policy cannot lower versioned quality floors")
	}
	if len(m.Candidates) == 0 || len(m.Candidates) > 100 || len(m.OutcomeShards) > 100 {
		return fmt.Errorf("candidate/shard bounds invalid")
	}
	seen := map[string]bool{}
	for _, c := range m.Candidates {
		if !digestPattern.MatchString(c.ConfigurationID) || seen[c.ConfigurationID] || c.AccountScope == "" || !digestPattern.MatchString(c.CatalogSHA256) {
			return fmt.Errorf("invalid or duplicate candidate identity")
		}
		seen[c.ConfigurationID] = true
	}
	edges := map[string]bool{}
	for _, e := range p.Fallback {
		if e.From == e.To || edges[e.From] || !seen[e.To] || !seen[e.From] {
			return fmt.Errorf("fallback must be one explicit edge per source to a declared candidate")
		}
		edges[e.From] = true
	}
	return nil
}

func newRat(s string) (*big.Rat, bool) { v, e := usage.NumberRat(json.Number(s)); return v, e == nil }
func ratNumber(s string) *big.Rat      { return rat(json.Number(s)) }
