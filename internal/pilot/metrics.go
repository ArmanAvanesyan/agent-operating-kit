package pilot

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"time"
)

func normalResult(rule MetricRule, values []float64, missing, censored, unknown int) (MetricResult, bool) {
	r := MetricResult{Name: rule.Name, N: len(values), Missing: missing, Censored: censored, Unknown: unknown, Supported: true}
	if len(values) < rule.MinimumN || missing > 0 || censored > 0 || unknown > 0 {
		return r, false
	}
	mean := 0.0
	for _, x := range values {
		mean += x
	}
	mean /= float64(len(values))
	variance := 0.0
	for _, x := range values {
		d := x - mean
		variance += d * d
	}
	if len(values) > 1 {
		variance /= float64(len(values) - 1)
	}
	z := 1.96
	if rule.ConfidenceMethod == "paired-normal-approx-95" {
		z = 1.96
	}
	lo, hi := mean-z*math.Sqrt(variance/float64(len(values))), mean+z*math.Sqrt(variance/float64(len(values)))
	target, _ := parseFinite(rule.Target)
	r.Estimate = jsonNumber(mean)
	r.Lower = jsonNumber(lo)
	r.Upper = jsonNumber(hi)
	if rule.Direction == "minimum" {
		r.Pass = lo >= target
	} else {
		r.Pass = hi <= target
	}
	return r, r.Pass
}
func jsonNumber(f float64) json.Number { return json.Number(strconv.FormatFloat(f, 'f', -1, 64)) }
func conservativeWilsonNumber(v float64, lower bool) json.Number {
	const scale = 1e12
	if lower {
		v = math.Floor(v*scale) / scale
	} else {
		v = math.Ceil(v*scale) / scale
	}
	return jsonNumber(v)
}

func assessEpisodeMetric(rule MetricRule, m Manifest, b Bundle, asOf time.Time) (MetricResult, bool, error) {
	routeHash := digest(m.Routes[0])
	byFamily := map[string]map[string]EpisodeObservation{}
	ids := map[string]bool{}
	receiptOwner := map[string]string{}
	unknown, censored := 0, 0
	currency := ""
	for _, e := range b.Episodes {
		part, ok := m.FamilyPartitions[e.FamilyID]
		if !ok || part != "locked-audit" {
			return MetricResult{}, false, errors.New("episode is outside locked audit families")
		}
		reps, ok := m.RepetitionsByFamily[e.FamilyID]
		if !ok || !contains(reps, e.RepetitionID) {
			return MetricResult{}, false, errors.New("episode repetition not preregistered")
		}
		if e.ID == "" || ids[e.ID] || e.RouteSHA256 != routeHash || !validSHA256(e.ReceiptSHA256) || !validSHA256(e.VerifierSHA256) {
			return MetricResult{}, false, errors.New("episode requires unique id and independent receipt/verifier hashes")
		}
		ids[e.ID] = true
		owner := e.FamilyID + "\x00" + e.RepetitionID
		if prior, ok := receiptOwner[e.ReceiptSHA256]; ok && prior != owner {
			return MetricResult{}, false, errors.New("episode receipt replay across family/repetition")
		}
		receiptOwner[e.ReceiptSHA256] = owner
		at, err := time.Parse(time.RFC3339, e.ObservedAt)
		if err != nil || at.After(asOf) {
			return MetricResult{}, false, errors.New("episode timestamp invalid or future")
		}
		if e.Outcome != "accepted" && e.Outcome != "failed" && e.Outcome != "interrupted" {
			return MetricResult{}, false, errors.New("episode outcome invalid")
		}
		if e.Accepted != (e.Outcome == "accepted") {
			return MetricResult{}, false, errors.New("episode acceptance contradicts outcome")
		}
		if byFamily[e.FamilyID] == nil {
			byFamily[e.FamilyID] = map[string]EpisodeObservation{}
		}
		if _, ok := byFamily[e.FamilyID][e.RepetitionID]; ok {
			return MetricResult{}, false, errors.New("duplicate episode for family repetition")
		}
		byFamily[e.FamilyID][e.RepetitionID] = e
		if e.Censored {
			censored++
		}
		if currency == "" {
			currency = e.Currency
		} else if currency != e.Currency {
			return MetricResult{}, false, errors.New("mixed cost currencies")
		}
	}
	familyTotals := []float64{}
	familyAccepts := []float64{}
	missing := 0
	for family, partition := range m.FamilyPartitions {
		if partition != "locked-audit" {
			continue
		}
		expected := m.RepetitionsByFamily[family]
		episodes := byFamily[family]
		if len(episodes) != len(expected) {
			missing++
			continue
		}
		costSum := new(big.Rat)
		timeSum := 0.0
		accepted := 0
		complete := true
		for _, rep := range expected {
			e, ok := episodes[rep]
			if !ok || e.Censored {
				complete = false
				continue
			}
			cost, ce := parseRational(e.Cost)
			sc, se := parseRational(e.SetupCost)
			rc, re := parseRational(e.ReviewCost)
			xc, xe := parseRational(e.RecoveryCost)
			elapsed, ee := parseFinite(e.ElapsedSeconds)
			setup, es := parseFinite(e.SetupSeconds)
			review, er := parseFinite(e.ReviewSeconds)
			recovery, ex := parseFinite(e.RecoverySeconds)
			if ce != nil || se != nil || re != nil || xe != nil || ee != nil || es != nil || er != nil || ex != nil || cost.Sign() < 0 || sc.Sign() < 0 || rc.Sign() < 0 || xc.Sign() < 0 || elapsed < 0 || setup < 0 || review < 0 || recovery < 0 {
				unknown++
				complete = false
				continue
			}
			components := new(big.Rat).Add(sc, rc)
			components.Add(components, xc)
			if cost.Cmp(components) < 0 || elapsed+1e-9 < setup+review+recovery {
				return MetricResult{}, false, errors.New("episode total omits setup/review/recovery cost or time")
			}
			costSum.Add(costSum, cost)
			timeSum += elapsed
			if e.Accepted {
				accepted++
			}
		}
		if !complete {
			missing++
			continue
		}
		var x float64
		if rule.Name == "accepted-result-cost" {
			x, _ = costSum.Float64()
		} else {
			x = timeSum
		}
		familyTotals = append(familyTotals, x)
		familyAccepts = append(familyAccepts, float64(accepted))
	}
	if rule.Name == "accepted-result-cost" && currency == "" {
		unknown++
	}
	if missing > 0 || censored > 0 || unknown > 0 || len(familyTotals) < rule.MinimumN {
		return MetricResult{Name: rule.Name, N: len(familyTotals), Missing: missing, Censored: censored, Unknown: unknown, Supported: true}, false, nil
	}
	sumTotal, sumAccepts := 0.0, 0.0
	for i := range familyTotals {
		sumTotal += familyTotals[i]
		sumAccepts += familyAccepts[i]
	}
	if sumAccepts <= 0 || math.IsInf(sumTotal, 0) || math.IsNaN(sumTotal) {
		return MetricResult{Name: rule.Name, N: len(familyTotals), Unknown: 1, Supported: true}, false, nil
	}
	ratio := sumTotal / sumAccepts
	residualSquares := 0.0
	for i := range familyTotals {
		residual := familyTotals[i] - ratio*familyAccepts[i]
		residualSquares += residual * residual
	}
	variance := residualSquares / float64(len(familyTotals)-1)
	se := math.Sqrt(float64(len(familyTotals)) * variance / (sumAccepts * sumAccepts))
	lo, hi := ratio-1.96*se, ratio+1.96*se
	r := MetricResult{Name: rule.Name, N: len(familyTotals), Supported: true, Estimate: jsonNumber(ratio), Lower: jsonNumber(lo), Upper: jsonNumber(hi)}
	target, _ := parseFinite(rule.Target)
	if rule.Direction == "minimum" {
		r.Pass = lo >= target
	} else {
		r.Pass = hi <= target
	}
	return r, r.Pass, nil
}

func assessWindowMetric(rule MetricRule, m Manifest, b Bundle, asOf time.Time) (MetricResult, bool, error) {
	route := m.Routes[0]
	routeHash := digest(route)
	type pair struct{ base, treat *AccountWindowObservation }
	pairs := map[string]*pair{}
	ids := map[string]bool{}
	receipts := map[string]bool{}
	unknown, censored := 0, 0
	for i := range b.AccountWindows {
		w := b.AccountWindows[i]
		if w.ID == "" || w.BlockID == "" || ids[w.ID] || w.RouteSHA256 != routeHash || w.AccountScopeSHA256 != route.AccountScopeSHA256 || !validSHA256(w.AccountScopeSHA256) || !validSHA256(w.ReceiptSHA256) || !validSHA256(w.VerifierSHA256) || receipts[w.ReceiptSHA256] {
			return MetricResult{}, false, errors.New("account window identity/receipt invalid or replayed")
		}
		ids[w.ID] = true
		receipts[w.ReceiptSHA256] = true
		at, e := time.Parse(time.RFC3339, w.ObservedAt)
		if e != nil || at.After(asOf) {
			return MetricResult{}, false, errors.New("account window timestamp invalid or future")
		}
		if w.Unknown {
			unknown++
		}
		if w.Censored {
			censored++
		}
		if w.Arm != "baseline" && w.Arm != "treatment" {
			return MetricResult{}, false, errors.New("account window arm invalid")
		}
		if w.EligibleBeforeCapacity < 0 || w.CapacityDeferrals < 0 || w.CapacityDeferrals > w.EligibleBeforeCapacity || w.ProviderAttempts < 0 || w.QuotaDenials < 0 || w.QuotaDenials > w.ProviderAttempts || w.AdmittedStarts < 0 {
			return MetricResult{}, false, errors.New("account window counts invalid")
		}
		p := pairs[w.BlockID]
		if p == nil {
			p = &pair{}
			pairs[w.BlockID] = p
		}
		if w.Arm == "baseline" {
			if p.base != nil {
				return MetricResult{}, false, errors.New("duplicate baseline arm")
			}
			p.base = &w
		} else {
			if p.treat != nil {
				return MetricResult{}, false, errors.New("duplicate treatment arm")
			}
			p.treat = &w
		}
	}
	values := []float64{}
	missing := 0
	orders := map[string]bool{}
	for _, p := range pairs {
		if p.base == nil || p.treat == nil || p.base.AccountScopeSHA256 != p.treat.AccountScopeSHA256 || p.base.Sequence != p.treat.Sequence || (p.base.Sequence != "baseline-first" && p.base.Sequence != "treatment-first") {
			missing++
			continue
		}
		if p.base.Unknown || p.treat.Unknown || p.base.Censored || p.treat.Censored {
			missing++
			continue
		}
		orders[p.base.Sequence] = true
		var v float64
		switch rule.Name {
		case "capacity-deferral-rate":
			if p.treat.EligibleBeforeCapacity == 0 {
				missing++
				continue
			}
			v = float64(p.treat.CapacityDeferrals) / float64(p.treat.EligibleBeforeCapacity)
		case "provider-quota-denial-rate":
			if p.treat.ProviderAttempts == 0 {
				missing++
				continue
			}
			v = float64(p.treat.QuotaDenials) / float64(p.treat.ProviderAttempts)
		case "quota-avoidance":
			if p.base.ProviderAttempts == 0 || p.treat.ProviderAttempts == 0 {
				missing++
				continue
			}
			v = float64(p.base.QuotaDenials)/float64(p.base.ProviderAttempts) - float64(p.treat.QuotaDenials)/float64(p.treat.ProviderAttempts)
		default:
			return MetricResult{}, false, fmt.Errorf("no typed account-window estimator for %q", rule.Name)
		}
		values = append(values, v)
	}
	if len(orders) < 2 {
		missing++
	}
	r, pass := normalResult(rule, values, missing, censored, unknown)
	return r, pass, nil
}
