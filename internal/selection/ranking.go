package selection

import (
	"encoding/json"
	"math/big"
	"time"
)

func metrics(r Rank, priority string) []json.Number {
	switch priority {
	case "cost":
		return []json.Number{r.CostIndex, r.Quality, r.Scarcity, r.TimeIndex}
	case "capacity":
		return []json.Number{r.Scarcity, r.Quality, r.CostIndex, r.TimeIndex}
	case "deadline":
		return []json.Number{r.TimeIndex, r.Quality, r.Scarcity, r.CostIndex}
	default:
		return []json.Number{r.Quality, r.CostIndex, r.TimeIndex, r.Scarcity}
	}
}
func isQuality(priority string, i int) bool {
	return priority == "quality" && i == 0 || priority != "quality" && i == 1
}
func compare(a, b CandidateResult, p string) int {
	if a.Rank == nil || b.Rank == nil {
		return 0
	}
	x, y := metrics(*a.Rank, p), metrics(*b.Rank, p)
	for i := range x {
		c := rat(x[i]).Cmp(rat(y[i]))
		if isQuality(p, i) {
			c = -c
		}
		if c != 0 {
			return c
		}
	}
	if a.ConfigurationID < b.ConfigurationID {
		return -1
	}
	if a.ConfigurationID > b.ConfigurationID {
		return 1
	}
	return 0
}
func dominates(a, b Rank) bool {
	all := rat(a.Quality).Cmp(rat(b.Quality)) >= 0 && rat(a.CostIndex).Cmp(rat(b.CostIndex)) <= 0 && rat(a.TimeIndex).Cmp(rat(b.TimeIndex)) <= 0 && rat(a.Scarcity).Cmp(rat(b.Scarcity)) <= 0
	return all && (rat(a.Quality).Cmp(rat(b.Quality)) > 0 || rat(a.CostIndex).Cmp(rat(b.CostIndex)) < 0 || rat(a.TimeIndex).Cmp(rat(b.TimeIndex)) < 0 || rat(a.Scarcity).Cmp(rat(b.Scarcity)) < 0)
}
func improvement(a, b Rank, p string) bool { // a challenger, b incumbent
	x, y := metrics(a, p), metrics(b, p)
	for i := range x {
		v, w := rat(x[i]), rat(y[i])
		if v.Cmp(w) == 0 {
			continue
		}
		if isQuality(p, i) {
			return new(big.Rat).Sub(v, w).Cmp(big.NewRat(2, 100)) >= 0
		}
		if w.Sign() == 0 {
			return false
		}
		return new(big.Rat).Sub(w, v).Cmp(new(big.Rat).Mul(w, big.NewRat(1, 10))) >= 0
	}
	return false
}
func decide(d *SelectionDecision, t TaskProfile, p SelectionPolicy, prev *SelectionDecision, at time.Time) {
	best := -1
	for i := range d.Candidates {
		if !d.Candidates[i].Eligible || d.Candidates[i].Rank == nil {
			continue
		}
		for j := range d.Candidates {
			if i != j && d.Candidates[j].Eligible && d.Candidates[j].Rank != nil && dominates(*d.Candidates[j].Rank, *d.Candidates[i].Rank) {
				d.Candidates[i].Dominated = true
			}
		}
		if !d.Candidates[i].Dominated && (best < 0 || compare(d.Candidates[i], d.Candidates[best], p.Priority) < 0) {
			best = i
		}
	}
	if p.Pin != "" {
		pinned := -1
		for i, c := range d.Candidates {
			if c.ConfigurationID == p.Pin && c.Eligible {
				pinned = i
			}
		}
		if pinned >= 0 {
			best = pinned
			d.Explanation = append(d.Explanation, "eligible explicitly pinned route")
		} else {
			best = -1
			for _, edge := range p.Fallback {
				if edge.From == p.Pin {
					for i, c := range d.Candidates {
						if c.ConfigurationID == edge.To && c.Eligible {
							best = i
							d.Explanation = append(d.Explanation, "one explicit fallback edge; target clears every gate")
						}
					}
				}
			}
		}
	}
	if best >= 0 && prev != nil && !p.WaiveHysteresis && p.Pin == "" {
		for i, c := range d.Candidates {
			if c.ConfigurationID == prev.ConfigurationID && c.AccountScope == prev.AccountScope && c.Eligible && c.Rank != nil && prev.Disposition == "selected" && (prev.Inputs == nil || prev.Inputs["profile:"+c.ConfigurationID] == c.ProfileSHA256) {
				oldAt, e := time.Parse(time.RFC3339Nano, prev.AsOf)
				if e == nil && !at.Before(oldAt) && (at.Sub(oldAt) < 5*time.Minute || !improvement(*d.Candidates[best].Rank, *c.Rank, p.Priority)) {
					best = i
					d.Explanation = append(d.Explanation, "eligible incumbent retained by hysteresis")
				}
			}
		}
	}
	if best >= 0 {
		c := d.Candidates[best]
		d.Disposition = "selected"
		d.ConfigurationID = c.ConfigurationID
		d.AccountScope = c.AccountScope
		d.Explanation = append(d.Explanation, "offline advisory recommendation; ranking indices are not accepted-result guarantees")
		d.NextEvidence = append(d.NextEvidence, "runtime must revalidate, reserve and enforce through P6/P7")
		return
	}
	refresh, deferRoute, unknown, definitive := false, false, false, true
	var wait *time.Time
	deadline, _ := time.Parse(time.RFC3339Nano, t.Deadline)
	for _, c := range d.Candidates {
		if p.Pin != "" && c.ConfigurationID != p.Pin {
			allowed := false
			for _, edge := range p.Fallback {
				if edge.From == p.Pin && edge.To == c.ConfigurationID {
					allowed = true
				}
			}
			if !allowed {
				continue
			}
		}
		noncapacity := true
		hasfail := false
		for _, g := range c.Gates {
			if g.Status == "fail" {
				hasfail = true
			}
			if g.Name != "capacity" && g.Status != "pass" {
				noncapacity = false
			}
			if g.Status == "unknown" {
				unknown = true
				d.NextEvidence = append(d.NextEvidence, c.ConfigurationID+": "+g.Name+": "+g.Reason)
			}
		}
		if !hasfail {
			definitive = false
		}
		if noncapacity && c.Capacity != nil {
			if c.Capacity.Disposition == "refresh-needed" {
				refresh = true
				continue
			}
			if c.Capacity.Disposition == "insufficient" {
				latest := at
				known := true
				deficient := false
				for _, b := range c.Capacity.Balances {
					if b.Demand == nil || b.Headroom == nil {
						known = false
						continue
					}
					if rat(*b.Demand).Cmp(rat(*b.Headroom)) <= 0 {
						continue
					}
					deficient = true
					if b.ResetAt == nil {
						known = false
						continue
					}
					reset, e := time.Parse(time.RFC3339Nano, *b.ResetAt)
					if e != nil || !reset.After(at) {
						known = false
						continue
					}
					if reset.After(latest) {
						latest = reset
					}
				}
				if known && deficient && !latest.After(deadline) {
					if wait == nil || latest.Before(*wait) {
						w := latest
						wait = &w
					}
				} else {
					deferRoute = true
				}
			}
		}
	}
	if p.Pin != "" {
		found := false
		for _, c := range d.Candidates {
			if c.ConfigurationID == p.Pin {
				found = true
			}
		}
		if !found {
			definitive = true
		}
	}
	switch {
	case refresh:
		d.Disposition = "refresh-needed"
	case wait != nil:
		d.Disposition = "wait-until"
		s := wait.UTC().Format(time.RFC3339Nano)
		d.WaitUntil = &s
		d.Explanation = append(d.Explanation, "reset hint only; obtain new observations and rerun, future fit is unproven")
	case deferRoute || t.MidRun && !t.SafeCheckpoint:
		d.Disposition = "defer"
	case definitive:
		d.Disposition = "no-qualified-candidate"
	case unknown:
		d.Disposition = "abstain"
	default:
		d.Disposition = "abstain"
	}
	d.Explanation = append(d.Explanation, "no candidate passed every mandatory gate")
}
