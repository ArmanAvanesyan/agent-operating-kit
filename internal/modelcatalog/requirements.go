package modelcatalog

import (
	"encoding/json"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"math/big"
	"sort"
	"time"
)

// Requirement is an explicit task requirement, never inferred from prose.
type Requirement struct {
	Dimension string          `json:"dimension"`
	Value     json.RawMessage `json:"value"`
}

// CheckRequirements shares catalog trust, scope and freshness checks. Unsupported
// dimensions stay unknown, and listing/editorial evidence cannot prove behavior.
func CheckRequirements(c *Capture, profile contracts.ExecutionProfile, profileSHA string, requirements []Requirement, at time.Time) []Finding {
	out := []Finding{}
	valid := c != nil && c.PolicyExternal && c.validationSeal != "" && c.validationSeal == captureSeal(c)
	for _, req := range requirements {
		f := Finding{Field: req.Dimension, Status: "unknown", Message: "no fresh trusted scoped requirement evidence", AssertionIDs: []string{}}
		allowed := map[string]bool{"contextTokens": true, "maxOutputTokens": true, "modalities": true, "structuredOutput": true, "toolUse": true}
		positive, negative := false, false
		if valid && allowed[req.Dimension] {
			for _, a := range c.Catalog.Assertions {
				if a.Dimension != req.Dimension || !assertionTrusted(c, a) || !fresh(c, a, at) || !scopeMatchesField(a.Scope, profile, req.Dimension) || (a.Category != Published && a.Category != Invoked && a.Category != Evaluated) {
					continue
				}
				matches := requirementContains(req.Dimension, a.Value, req.Value)
				// A published scoped supported limit/list is an upper declaration. A
				// smaller smoke-tested value does not establish an upper limit.
				if !matches && a.Category == Published && a.Polarity == "supports" && validRequirementValue(req.Dimension, a.Value) && validRequirementValue(req.Dimension, req.Value) {
					negative = true
					f.AssertionIDs = append(f.AssertionIDs, a.ID)
					f.Category = a.Category
				}
				if matches {
					if a.Polarity == "supports" {
						positive = true
					}
					if a.Polarity == "contradicts" {
						negative = true
					}
					f.AssertionIDs = append(f.AssertionIDs, a.ID)
					f.Category = a.Category
				}
			}
			for _, p := range c.Catalog.Probes {
				if !probeTrusted(c, p) || p.Outcome != "success" || p.ProfileSHA256 != profileSHA || !probeMatchesProfile(p.Configuration, profile) || probeFreshness(c, p, at) != "fresh" {
					continue
				}
				for _, o := range p.Observations {
					sameDimension := o.Dimension == req.Dimension
					// An exact-profile observation of an accepted explicit generation budget
					// establishes that setting only, not a generated output length guarantee.
					testedBudget := req.Dimension == "maxOutputTokens" && o.Dimension == "generation.maxOutputTokens" && profile.Model.Generation != nil && profile.Model.Generation.MaxOutputTokens != nil && assertionMatches("generation.maxOutputTokens", o.Value, string(*profile.Model.Generation.MaxOutputTokens))
					if (sameDimension || testedBudget) && requirementContains(req.Dimension, o.Value, req.Value) {
						positive = true
						f.AssertionIDs = append(f.AssertionIDs, p.ID)
						f.Category = Invoked
					}
				}
			}
		}
		if positive && negative {
			f.Message = "conflicting scoped requirement evidence"
		} else if negative {
			f.Status = "unsupported"
			f.Message = "trusted scoped evidence rejects requirement"
		} else if positive {
			f.Status = "supported"
			f.Message = "trusted scoped evidence satisfies requirement"
		}
		sort.Strings(f.AssertionIDs)
		out = append(out, f)
	}
	return out
}
func requirementContains(d string, got, want []byte) bool {
	if d == "contextTokens" || d == "maxOutputTokens" {
		var a, b json.Number
		if Decode(got, &a) != nil || Decode(want, &b) != nil {
			return false
		}
		x, ok := new(big.Rat).SetString(string(a))
		y, ok2 := new(big.Rat).SetString(string(b))
		return ok && ok2 && x.Sign() >= 0 && y.Sign() >= 0 && x.Cmp(y) >= 0
	}
	if d == "modalities" {
		var a, b []string
		if Decode(got, &a) != nil || Decode(want, &b) != nil {
			return false
		}
		for _, v := range b {
			found := false
			for _, w := range a {
				if w == v {
					found = true
				}
			}
			if !found {
				return false
			}
		}
		return true
	}
	return jsonEqual(json.RawMessage(got), json.RawMessage(want))
}

// LookupConfiguration includes exact identities carried by imported probe evidence;
// presence establishes identity only, not any capability or runtime readiness.
func LookupConfiguration(c *Capture, id string) (Configuration, bool) {
	if c == nil || c.validationSeal == "" || c.validationSeal != captureSeal(c) {
		return Configuration{}, false
	}
	for _, r := range c.Catalog.Configurations {
		if r.ID == id && ConfigurationID(r.Identity) == id {
			return r.Identity, true
		}
	}
	for _, p := range c.Catalog.Probes {
		if ConfigurationID(p.Configuration) == id && probeTrusted(c, p) {
			return p.Configuration, true
		}
	}
	return Configuration{}, false
}

func validRequirementValue(d string, v []byte) bool {
	switch d {
	case "contextTokens", "maxOutputTokens":
		var n json.Number
		if Decode(v, &n) != nil {
			return false
		}
		r, ok := new(big.Rat).SetString(string(n))
		return ok && r.Sign() >= 0
	case "toolUse", "structuredOutput":
		var b bool
		return Decode(v, &b) == nil
	case "modalities":
		var a []string
		return Decode(v, &a) == nil && len(a) > 0
	}
	return false
}
