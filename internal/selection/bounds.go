package selection

import (
	"fmt"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/evaluation"
	"time"
)

func applyUpperBounds(e *evaluation.EstimateArtifact, p SelectionPolicy, root string, at time.Time) error {
	if p.UpperBounds == nil {
		return nil
	}
	seen := false
	for _, b := range *p.UpperBounds {
		if b.Cohort != e.Cohort {
			continue
		}
		if seen {
			return fmt.Errorf("conflicting explicit bounds for cohort")
		}
		seen = true
		if _, err := asset(root, b.Source); err != nil {
			return err
		}
		obs, err := time.Parse(time.RFC3339Nano, b.ObservedAt)
		until, err2 := time.Parse(time.RFC3339Nano, b.ValidUntil)
		if err != nil || err2 != nil || obs.After(at) || !at.Before(until) || at.Sub(obs) >= 7*24*time.Hour {
			return fmt.Errorf("explicit upper bound expired or invalid")
		}
		until = minTime(until, obs.Add(7*24*time.Hour))
		original, _ := time.Parse(time.RFC3339Nano, e.DemandValidUntil)
		if at.Before(original) {
			until = minTime(until, original)
		} else { // expired measured demand must be discarded before applying current approved bounds
			e.CostUpper = nil
			e.SecondsUpper = nil
			e.Bounds = nil
		}
		if b.CostUpper != nil && rat(*b.CostUpper).Sign() < 0 || b.SecondsUpper != nil && rat(*b.SecondsUpper).Sign() < 0 {
			return fmt.Errorf("negative explicit bound")
		}
		if e.CostUpper == nil && b.CostUpper != nil {
			e.CostUpper = b.CostUpper
			e.Currency = b.Currency
		}
		if e.SecondsUpper == nil && b.SecondsUpper != nil {
			e.SecondsUpper = b.SecondsUpper
		}
		for _, bound := range b.Bounds {
			if bound.Upper == nil || rat(*bound.Upper).Sign() < 0 {
				return fmt.Errorf("invalid explicit resource bound")
			}
			exists := false
			for i, measured := range e.Bounds {
				if measured.PoolID == bound.PoolID && measured.WindowFamily == bound.WindowFamily && measured.Metric == bound.Metric && measured.Unit == bound.Unit {
					exists = true
					if measured.Upper == nil {
						bound.Method = "trusted-policy-upper-bound; unmeasured"
						e.Bounds[i] = bound
					}
				}
			}
			if !exists {
				bound.Method = "trusted-policy-upper-bound; unmeasured"
				e.Bounds = append(e.Bounds, bound)
			}
		}
		e.DemandValidUntil = until.UTC().Format(time.RFC3339Nano)
		e.Provenance = append(e.Provenance, "unmeasured-policy-bound:"+b.Source.SHA256)
		e.Method += "; explicit unmeasured consumption bounds from external policy"
	}
	return nil
}
