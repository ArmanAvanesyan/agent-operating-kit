package pilot

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"time"
)

func digest(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func parseFinite(s json.Number) (float64, error) {
	f, e := strconv.ParseFloat(string(s), 64)
	if e != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, errors.New("number must be finite")
	}
	return f, nil
}
func parseRational(n json.Number) (*big.Rat, error) {
	if _, err := parseFinite(n); err != nil {
		return nil, err
	}
	r, ok := new(big.Rat).SetString(string(n))
	if !ok {
		return nil, errors.New("invalid exact decimal")
	}
	return r, nil
}

func validateMetricSemantics(r MetricRule) error {
	want := struct{ unit, direction, denominator, analysis, method string }{}
	switch r.Name {
	case "quality":
		want = struct{ unit, direction, denominator, analysis, method string }{"proportion", "minimum", "complete-locked-audit-families", "independent-family", "wilson-one-sided-95"}
	case "consequential-miss":
		want = struct{ unit, direction, denominator, analysis, method string }{"proportion", "maximum", "complete-locked-audit-families", "independent-family", "wilson-one-sided-95"}
	case "accepted-result-cost":
		want = struct{ unit, direction, denominator, analysis, method string }{"currency-per-accepted", "maximum", "accepted-results-in-complete-locked-audit-families", "independent-family", "ratio-delta-normal-approx-95"}
	case "accepted-result-time":
		want = struct{ unit, direction, denominator, analysis, method string }{"seconds-per-accepted", "maximum", "accepted-results-in-complete-locked-audit-families", "independent-family", "ratio-delta-normal-approx-95"}
	case "capacity-deferral-rate":
		want = struct{ unit, direction, denominator, analysis, method string }{"proportion", "maximum", "eligible-before-capacity", "account-window", "paired-normal-approx-95"}
	case "provider-quota-denial-rate":
		want = struct{ unit, direction, denominator, analysis, method string }{"proportion", "maximum", "provider-attempts", "account-window", "paired-normal-approx-95"}
	case "quota-avoidance":
		want = struct{ unit, direction, denominator, analysis, method string }{"proportion", "minimum", "paired-provider-quota-denial-rate-improvement", "account-window", "paired-normal-approx-95"}
	case "observation-coverage":
		want = struct{ unit, direction, denominator, analysis, method string }{"proportion", "minimum", "all-locked-audit-repetitions", "independent-family", "wilson-one-sided-95"}
	case "prediction-coverage":
		want = struct{ unit, direction, denominator, analysis, method string }{"proportion", "minimum", "all-locked-audit-repetitions", "independent-family", "wilson-one-sided-95"}
	case "operational-safety":
		want = struct{ unit, direction, denominator, analysis, method string }{"count", "maximum", "six-hard-safety-counters", "independent-family", "deterministic-count"}
		if r.MinimumN != 1 {
			return errors.New("operational-safety has fixed deterministic counter sample size")
		}
		target, e := parseFinite(r.Target)
		if e != nil || target != 0 {
			return errors.New("operational-safety target must be exactly zero")
		}
	case "recovery-latency":
		want = struct{ unit, direction, denominator, analysis, method string }{"seconds", "maximum", "complete-recovery-drills", "independent-family", "normal-approx-95"}
	case "stability":
		want = struct{ unit, direction, denominator, analysis, method string }{"proportion", "maximum", "complete-locked-audit-families", "independent-family", "normal-approx-95"}
	default:
		return fmt.Errorf("unknown pilot metric %q has no validated estimator", r.Name)
	}
	if r.Unit != want.unit || r.Direction != want.direction || r.Denominator != want.denominator || r.AnalysisUnit != want.analysis || r.ConfidenceMethod != want.method {
		return fmt.Errorf("metric %q differs from its fixed v1 unit, direction, denominator, analysis unit, or confidence method", r.Name)
	}
	target, te := parseFinite(r.Target)
	stop, se := parseFinite(r.StopThreshold)
	if te != nil || se != nil || stop < 0 || (r.Direction == "minimum" && stop < target) || (r.Direction == "maximum" && stop > target) {
		return fmt.Errorf("metric %q stop threshold is inconsistent with its target and direction", r.Name)
	}
	if r.Unit == "proportion" && (target < 0 || target > 1 || stop > 1) {
		return fmt.Errorf("metric %q proportion target/stop threshold must be within [0,1]", r.Name)
	}
	if r.Name == "operational-safety" && stop != 0 {
		return errors.New("operational-safety stop threshold must be exactly zero")
	}
	return nil
}

// EvaluateStopRules applies preregistered confidence-bound stop thresholds.
// Missing or unsupported results do not silently evaluate as zero; ordinary
// assessment completeness separately blocks promotion in those cases.
func EvaluateStopRules(rules []MetricRule, results []MetricResult) (bool, []string) {
	byName := make(map[string]MetricResult, len(results))
	for _, result := range results {
		byName[result.Name] = result
	}
	var reasons []string
	for _, rule := range rules {
		result, ok := byName[rule.Name]
		if !ok || !result.Supported || result.N == 0 || result.Missing > 0 || result.Censored > 0 || result.Unknown > 0 {
			continue
		}
		if rule.Name == "operational-safety" {
			v, err := parseFinite(result.Estimate)
			if err == nil && v > 0 {
				reasons = append(reasons, "stop threshold crossed: operational-safety deterministic counter is nonzero")
			}
			continue
		}
		threshold, err := parseFinite(rule.StopThreshold)
		if err != nil {
			continue
		}
		var bound json.Number
		if rule.Direction == "maximum" {
			bound = result.Upper
		} else if rule.Direction == "minimum" {
			bound = result.Lower
		} else {
			continue
		}
		value, err := parseFinite(bound)
		if err != nil {
			continue
		}
		crossed := rule.Direction == "maximum" && value > threshold || rule.Direction == "minimum" && value < threshold
		if crossed {
			reasons = append(reasons, fmt.Sprintf("stop threshold crossed: %s confidence bound %s %s %s", rule.Name, bound, rule.Direction, rule.StopThreshold))
		}
	}
	return len(reasons) > 0, reasons
}

func ValidateManifest(m Manifest) error {
	if m.Version != Version || strings.TrimSpace(m.ID) == "" || (m.Purpose != Calibration && m.Purpose != Production) {
		return errors.New("invalid pilot version, id, or purpose")
	}
	if m.Risk != "standard" && m.Risk != "high" {
		return errors.New("pilot risk must be standard or high")
	}
	if _, e := time.Parse(time.RFC3339, m.FrozenAt); e != nil {
		return errors.New("frozenAt must be RFC3339")
	}
	for _, h := range []string{m.ConfigurationSHA256, m.DatasetSHA256, m.ProtocolSHA256} {
		if len(h) != 64 {
			return errors.New("manifest requires three SHA-256 bindings")
		}
		if _, e := hex.DecodeString(h); e != nil {
			return errors.New("invalid manifest hash")
		}
	}
	if m.MinimumShadowOpportunities < 100 {
		return errors.New("minimum shadow opportunities must be at least 100")
	}
	if len(m.Routes) != 1 || len(m.AllowedRepositories) == 0 || len(m.AllowedAccounts) == 0 || len(m.Operators) == 0 || m.MaximumAttempts <= 0 || m.MaximumSeconds <= 0 || m.RetentionDays <= 0 || m.ConsumptionUnit == "" {
		return errors.New("aok-pilot/v1 requires exactly one frozen route, scope, operators, finite budgets, and retention")
	}
	if n, e := parseFinite(m.MaximumConsumption); e != nil || n <= 0 {
		return errors.New("manifest requires a positive finite consumption cap")
	}
	for _, r := range m.Routes {
		if r.Host == "" || r.HostVersion == "" || r.Provider == "" || r.Platform == "" || r.Transport == "" || r.NativeTenant == "" || r.API == "" || r.Origin == "" || r.Account == "" || r.Operation == "" || r.AuthMode == "" {
			return errors.New("route must bind exact host, native, transport, account, operation, and auth dimensions")
		}
		for _, h := range []string{r.HostSHA256, r.ControlPolicySHA256, r.ConfigurationSHA256, r.ProfileSHA256, r.RouteManifestSHA256, r.VerifierSHA256, r.RubricSHA256, r.EnvironmentSHA256, r.AccountScopeSHA256} {
			if !validSHA256(h) {
				return errors.New("route requires hashes for host, control policy, configuration, profile, route manifest, verifier, rubric, environment, and account scope")
			}
		}
		if r.ConfigurationSHA256 != m.ConfigurationSHA256 {
			return errors.New("route configuration hash differs from frozen campaign configuration")
		}
	}
	if len(m.Trials) == 0 {
		return errors.New("manifest needs frozen trial identities")
	}
	trialIDs := map[string]bool{}
	routeIDs := map[string]bool{}
	for _, r := range m.Routes {
		routeIDs[digest(r)] = true
	}
	globalCap, _ := parseRational(m.MaximumConsumption)
	for _, tr := range m.Trials {
		if tr.ID == "" || trialIDs[tr.ID] || !contains(m.AllowedRepositories, tr.Repository) || !contains(m.AllowedAccounts, tr.Account) || !validSHA256(tr.InputSHA256) || !routeIDs[tr.RouteSHA256] || tr.MaximumAttempts < 1 || tr.MaximumAttempts > m.MaximumAttempts || tr.MaximumSeconds < 1 || tr.MaximumSeconds > m.MaximumSeconds || tr.ConsumptionUnit != m.ConsumptionUnit {
			return errors.New("invalid frozen trial identity or scope")
		}
		trialIDs[tr.ID] = true
		cap, e := parseRational(tr.MaximumConsumption)
		if e != nil || cap.Sign() <= 0 || cap.Cmp(globalCap) > 0 {
			return errors.New("trial consumption cap exceeds campaign or is invalid")
		}
	}
	if len(m.Metrics) == 0 {
		return errors.New("at least one preregistered metric is required")
	}
	seen := map[string]bool{}
	for family, p := range m.FamilyPartitions {
		if strings.TrimSpace(family) == "" || (p != "development" && p != "validation" && p != "locked-audit") {
			return errors.New("invalid family partition")
		}
	}
	for family, reps := range m.RepetitionsByFamily {
		if _, ok := m.FamilyPartitions[family]; !ok || len(reps) < 3 || !sort.StringsAreSorted(reps) {
			return errors.New("each family requires at least three sorted preregistered repetitions")
		}
		for i, r := range reps {
			if r == "" || (i > 0 && r == reps[i-1]) {
				return errors.New("invalid preregistered repetitions")
			}
		}
	}
	if len(m.RepetitionsByFamily) != len(m.FamilyPartitions) {
		return errors.New("every partitioned family needs frozen repetitions")
	}
	requiredMetrics := []string{"quality", "consequential-miss", "accepted-result-cost", "accepted-result-time", "capacity-deferral-rate", "provider-quota-denial-rate", "quota-avoidance", "observation-coverage", "prediction-coverage", "operational-safety", "recovery-latency", "stability"}
	knownMetrics := map[string]bool{}
	for _, name := range requiredMetrics {
		knownMetrics[name] = true
	}
	for _, r := range m.Metrics {
		if !knownMetrics[r.Name] {
			return fmt.Errorf("unknown pilot metric %q has no validated estimator", r.Name)
		}
		seen[r.Name] = true
	}
	for _, name := range requiredMetrics {
		if !seen[name] {
			return fmt.Errorf("mandatory pilot metric %q is missing", name)
		}
	}
	clear(seen)
	for _, r := range m.Metrics {
		if r.Name == "" || seen[r.Name] {
			return errors.New("metric names must be nonempty and unique")
		}
		seen[r.Name] = true
		if r.Unit == "" || r.Denominator == "" || (r.Direction != "minimum" && r.Direction != "maximum") || r.MinimumN <= 0 || r.AnalysisUnit != "independent-family" && r.AnalysisUnit != "account-window" {
			return fmt.Errorf("invalid metric rule %q", r.Name)
		}
		if r.ConfidenceMethod != "normal-approx-95" && r.ConfidenceMethod != "ratio-delta-normal-approx-95" && r.ConfidenceMethod != "wilson-one-sided-95" && r.ConfidenceMethod != "paired-normal-approx-95" && r.ConfidenceMethod != "deterministic-count" {
			return fmt.Errorf("unsupported confidence method for %q", r.Name)
		}
		if (r.ConfidenceMethod == "normal-approx-95" || r.ConfidenceMethod == "ratio-delta-normal-approx-95" || r.ConfidenceMethod == "paired-normal-approx-95") && r.MinimumN < 2 {
			return fmt.Errorf("metric %q needs at least two independent units for a variance estimate", r.Name)
		}
		if r.Name == "quality" {
			floor := 0.80
			minimum := 30
			if m.Risk == "high" {
				floor = 0.95
				minimum = 52
			}
			target, e := parseFinite(r.Target)
			if e != nil || r.Unit != "proportion" || r.Denominator != "complete-locked-audit-families" || r.Direction != "minimum" || r.ConfidenceMethod != "wilson-one-sided-95" || r.AnalysisUnit != "independent-family" || r.MinimumN < minimum || target < floor || target > 1 {
				return errors.New("quality rule violates the P4 family count or Wilson floor")
			}
		}
		windowMetric := r.Name == "capacity-deferral-rate" || r.Name == "provider-quota-denial-rate" || r.Name == "quota-avoidance"
		if windowMetric && (r.AnalysisUnit != "account-window" || r.ConfidenceMethod != "paired-normal-approx-95" || r.Unit != "proportion") {
			return fmt.Errorf("metric %q requires supported paired account-window proportion analysis", r.Name)
		}
		if !windowMetric && r.AnalysisUnit != "independent-family" {
			return fmt.Errorf("metric %q requires independent-family analysis", r.Name)
		}
		if err := validateMetricSemantics(r); err != nil {
			return err
		}
		cl, e := parseFinite(r.ConfidenceLevel)
		if e != nil || cl != 0.95 {
			return fmt.Errorf("metric %q requires explicit 0.95 confidence", r.Name)
		}
		if r.Baseline == "" || r.MissingRule != "exclude-and-report" || r.CensoringRule != "retain-and-report-no-imputation" {
			return fmt.Errorf("metric %q lacks fixed baseline/missing/censoring rules", r.Name)
		}
		if _, e = parseFinite(r.Target); e != nil {
			return fmt.Errorf("invalid target for %q", r.Name)
		}
		if _, e = parseFinite(r.StopThreshold); e != nil {
			return fmt.Errorf("invalid stop threshold for %q", r.Name)
		}
	}
	return nil
}

func Assess(m Manifest, p Policy, b Bundle, asOf time.Time) (Assessment, error) {
	return AssessWithVerifier(m, p, b, asOf, nil)
}

func AssessWithVerifier(m Manifest, p Policy, b Bundle, asOf time.Time, verifier QualificationVerifier) (Assessment, error) {
	if err := ValidateManifest(m); err != nil {
		return Assessment{}, err
	}
	if frozen, _ := time.Parse(time.RFC3339, m.FrozenAt); !asOf.After(frozen) {
		return Assessment{}, errors.New("assessment time must follow manifest freeze")
	}
	if err := ValidatePolicy(p); err != nil {
		return Assessment{}, err
	}
	if p.ManifestSHA256 != digest(m) {
		return Assessment{}, errors.New("external policy manifest binding invalid")
	}
	for _, ref := range p.Evidence {
		if ref.Kind == "" || !validSHA256(ref.SHA256) || !validSHA256(ref.ScopeSHA256) {
			return Assessment{}, errors.New("policy evidence references require kind and scoped hashes")
		}
	}
	if b.Version != Version || b.ManifestSHA256 != digest(m) {
		return Assessment{}, errors.New("bundle version or manifest binding invalid")
	}
	for _, n := range []int{b.ConsequentialMisses, b.UnauthorizedRequests, b.HardGateBypasses, b.FalseAcceptances, b.OverAdmissions, b.AmbiguousResends} {
		if n < 0 {
			return Assessment{}, errors.New("operational safety counters cannot be negative")
		}
	}
	if _, e := time.Parse(time.RFC3339, p.ExpiresAt); e != nil {
		return Assessment{}, errors.New("policy expiry must be RFC3339")
	}
	a := Assessment{Version: Version, ManifestSHA256: digest(m), BundleSHA256: digest(b), Status: "eligible-for-review", ShadowRequired: m.MinimumShadowOpportunities}
	seen := map[string]bool{}
	inputs := map[string]bool{}
	for _, s := range b.Shadow {
		if s.ID == "" || !validSHA256(s.InputSHA256) || seen[s.ID] {
			return Assessment{}, errors.New("shadow opportunities require unique IDs and input hashes")
		}
		seen[s.ID] = true
		if inputs[s.InputSHA256] {
			return Assessment{}, errors.New("duplicate shadow input does not count as distinct opportunity")
		}
		inputs[s.InputSHA256] = true
		observed, e := time.Parse(time.RFC3339, s.ObservedAt)
		if e != nil || observed.After(asOf) {
			return Assessment{}, errors.New("shadow timestamp must be RFC3339")
		}
		if s.Eligible {
			a.ShadowEligible++
		}
		if s.Abstained {
			a.ShadowAbstentions++
		}
		if s.Dispatched {
			return Assessment{}, errors.New("shadow observation must never dispatch")
		}
	}
	if a.ShadowEligible < m.MinimumShadowOpportunities {
		a.Reasons = append(a.Reasons, "fewer than required distinct eligible shadow opportunities")
	}
	results, pass, err := assessMetrics(m, b, asOf)
	if err != nil {
		return Assessment{}, err
	}
	a.Metrics = results
	a.StopTriggered, a.StopReasons = EvaluateStopRules(m.Metrics, results)
	if a.StopTriggered {
		a.Reasons = append(a.Reasons, a.StopReasons...)
	}
	if !pass {
		a.Reasons = append(a.Reasons, "one or more preregistered metric bounds fail or lack sample")
	}
	if b.ConsequentialMisses > 0 || b.UnauthorizedRequests > 0 || b.HardGateBypasses > 0 || b.FalseAcceptances > 0 || b.OverAdmissions > 0 || b.AmbiguousResends > 0 {
		a.Reasons = append(a.Reasons, "deterministic operational safety count must be zero")
	}
	if verifier == nil {
		a.Reasons = append(a.Reasons, "no injected trust verifier for policy, freeze chronology, P4, native, or operational evidence")
	} else {
		q, e := verifier.Verify(m, p, b, asOf)
		if e != nil {
			return Assessment{}, e
		}
		if !q.Authorization || !q.Native || !q.P4 || !q.P3 || !q.Route {
			a.Reasons = append(a.Reasons, "verified experiment authorization, native, P4, P3, or route evidence is incomplete")
		}
	}
	if !asOf.Before(mustTime(p.ExpiresAt)) {
		a.Reasons = append(a.Reasons, "external policy expired")
	}
	if len(a.Reasons) > 0 {
		a.Status = "eligible-for-review"
		if a.StopTriggered {
			a.Status = "blocked"
		} else if !pass || a.ShadowEligible < m.MinimumShadowOpportunities {
			a.Status = "insufficient-evidence"
		} else {
			a.Status = "blocked"
		}
		return sealAssessment(a), nil
	}
	if m.Purpose == Calibration {
		a.Status = "blocked"
		a.Reasons = append(a.Reasons, "calibration evidence cannot qualify production")
		return sealAssessment(a), nil
	}
	// Assessment is evidence for human review only. Activation and execution
	// require a separately implemented, protected promotion boundary.
	a.Status = "eligible-for-review"
	return sealAssessment(a), nil
}

func sealAssessment(a Assessment) Assessment {
	b, _ := json.Marshal(a)
	a.seal = sha256.Sum256(b)
	return a
}
func validAssessment(a Assessment) bool { b, _ := json.Marshal(a); return a.seal == sha256.Sum256(b) }
func AssessmentSHA256(a Assessment) string {
	if !validAssessment(a) {
		return ""
	}
	return digest(a)
}
func validSHA256(s string) bool {
	if len(s) != 64 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}
func mustTime(s string) time.Time { t, _ := time.Parse(time.RFC3339, s); return t }

func assessMetrics(m Manifest, b Bundle, asOf time.Time) ([]MetricResult, bool, error) {
	var out []MetricResult
	all := true
	routeHash := digest(m.Routes[0])
	globalIDs := map[string]bool{}
	globalReceiptOwners := map[string]string{}
	for _, rule := range m.Metrics {
		if rule.Name == "operational-safety" {
			for _, o := range b.Observations {
				if o.Metric == rule.Name {
					return nil, false, errors.New("operational-safety is derived only from deterministic bundle counters")
				}
			}
			r := safetyCounterMetric(rule, b)
			if !r.Pass {
				all = false
			}
			out = append(out, r)
			continue
		}
		if rule.AnalysisUnit == "account-window" {
			r, pass, e := assessWindowMetric(rule, m, b, asOf)
			if e != nil {
				return nil, false, e
			}
			if !pass {
				all = false
			}
			out = append(out, r)
			continue
		}
		if rule.Name == "accepted-result-cost" || rule.Name == "accepted-result-time" {
			r, pass, e := assessEpisodeMetric(rule, m, b, asOf)
			if e != nil {
				return nil, false, e
			}
			if !pass {
				all = false
			}
			out = append(out, r)
			continue
		}
		groups := map[string][]float64{}
		familySuccess := map[string]bool{}
		familyAcceptedCount := map[string]int{}
		seenRows := map[string]bool{}
		missing, censored, unknown := 0, 0, 0
		for _, o := range b.Observations {
			if o.Metric != rule.Name {
				continue
			}
			if o.FamilyID == "" {
				return nil, false, errors.New("metric observation needs family id")
			}
			if o.ID == "" || globalIDs[o.ID] || !validSHA256(o.ReceiptSHA256) || !validSHA256(o.VerifierSHA256) {
				return nil, false, errors.New("metric observations must reference independent receipt and verifier hashes")
			}
			if o.RouteSHA256 != routeHash {
				return nil, false, errors.New("metric observation route differs from sole frozen campaign route")
			}
			globalIDs[o.ID] = true
			observed, e := time.Parse(time.RFC3339, o.ObservedAt)
			if e != nil || observed.After(asOf) {
				return nil, false, errors.New("metric observation time invalid or in the future")
			}
			part, ok := m.FamilyPartitions[o.FamilyID]
			if !ok || part != "locked-audit" {
				return nil, false, errors.New("metric observation is not in locked audit partition")
			}
			expected, ok := m.RepetitionsByFamily[o.FamilyID]
			if !ok || o.RepetitionID == "" {
				return nil, false, errors.New("observation repetition is not preregistered")
			}
			allowed := false
			for _, rep := range expected {
				if rep == o.RepetitionID {
					allowed = true
				}
			}
			if !allowed {
				return nil, false, errors.New("unregistered repetition")
			}
			rowKey := o.FamilyID + "\x00" + o.RepetitionID + "\x00" + o.Metric
			if o.ID == "" || seenRows[rowKey] {
				return nil, false, errors.New("duplicate family repetition metric observation")
			}
			seenRows[rowKey] = true
			owner := o.FamilyID + "\x00" + o.RepetitionID
			if prior, ok := globalReceiptOwners[o.ReceiptSHA256]; ok && prior != owner {
				return nil, false, errors.New("receipt replay across families or repetitions")
			}
			globalReceiptOwners[o.ReceiptSHA256] = owner
			if o.Unknown || o.Value == nil {
				unknown++
				continue
			}
			if o.Censored {
				censored++
				continue
			}
			if o.Unit != rule.Unit {
				return nil, false, errors.New("metric unit mismatch")
			}
			f, e := parseFinite(*o.Value)
			if e != nil {
				return nil, false, e
			}
			groups[o.FamilyID] = append(groups[o.FamilyID], f)
			familyAcceptedCount[o.FamilyID]++
			if _, ok := familySuccess[o.FamilyID]; !ok {
				familySuccess[o.FamilyID] = true
			}
			if !o.Accepted {
				familySuccess[o.FamilyID] = false
			}
		}
		vals := make([]float64, 0, len(groups))
		completeSuccess := map[string]bool{}
		for family, g := range groups {
			if len(g) != len(m.RepetitionsByFamily[family]) {
				continue
			}
			completeSuccess[family] = familyAcceptedCount[family] == len(m.RepetitionsByFamily[family]) && familySuccess[family]
			sum := 0.0
			for _, v := range g {
				sum += v
			}
			vals = append(vals, sum/float64(len(g)))
		}
		for family, partition := range m.FamilyPartitions {
			if partition == "locked-audit" && len(groups[family]) < len(m.RepetitionsByFamily[family]) {
				missing++
			}
		}
		sort.Float64s(vals)
		r := MetricResult{Name: rule.Name, N: len(vals), Missing: missing, Censored: censored, Unknown: unknown, Supported: true}
		if len(vals) < rule.MinimumN {
			all = false
			out = append(out, r)
			continue
		}
		mean := 0.0
		for _, v := range vals {
			mean += v
		}
		mean /= float64(len(vals))
		variance := 0.0
		for _, v := range vals {
			d := v - mean
			variance += d * d
		}
		if len(vals) > 1 {
			variance /= float64(len(vals) - 1)
		}
		lo, hi := mean-1.96*math.Sqrt(variance/float64(len(vals))), mean+1.96*math.Sqrt(variance/float64(len(vals)))
		if rule.ConfidenceMethod == "wilson-one-sided-95" {
			success := 0
			for _, ok := range completeSuccess {
				if rule.Name == "consequential-miss" && !ok || rule.Name != "consequential-miss" && ok {
					success++
				}
			}
			mean = float64(success) / float64(len(vals))
			lo, hi = wilson(success, len(vals))
		}
		target, _ := parseFinite(rule.Target)
		r.Estimate = json.Number(strconv.FormatFloat(mean, 'f', -1, 64))
		r.Lower = conservativeWilsonNumber(lo, true)
		r.Upper = conservativeWilsonNumber(hi, false)
		if rule.Direction == "minimum" {
			r.Pass = lo >= target
		} else {
			r.Pass = hi <= target
		}
		if !r.Pass {
			all = false
		}
		if r.Missing > 0 || r.Censored > 0 || r.Unknown > 0 {
			r.Pass = false
			all = false
		}
		out = append(out, r)
	}
	return out, all, nil
}

func safetyCounterMetric(rule MetricRule, b Bundle) MetricResult {
	total := b.ConsequentialMisses + b.UnauthorizedRequests + b.HardGateBypasses + b.FalseAcceptances + b.OverAdmissions + b.AmbiguousResends
	v := json.Number(strconv.Itoa(total))
	return MetricResult{Name: rule.Name, N: 6, Estimate: v, Lower: v, Upper: v, Pass: total == 0, Supported: true}
}
func wilson(s, n int) (float64, float64) {
	z := 1.644853626951
	p := float64(s) / float64(n)
	d := 1 + z*z/float64(n)
	c := (p + z*z/(2*float64(n))) / d
	h := z * math.Sqrt(p*(1-p)/float64(n)+z*z/(4*float64(n*n))) / d
	return math.Max(0, c-h), math.Min(1, c+h)
}

func Replay(s ReplayScenario) (ReplayResult, error) {
	if s.Version != Version || s.CapacityBlocks < 0 || s.QuotaDenials < 0 || s.AdmittedStarts < 0 || s.Accepted < 0 || s.Failed < 0 || s.Accepted+s.Failed > s.AdmittedStarts {
		return ReplayResult{}, errors.New("invalid replay scenario")
	}
	r := ReplayResult{Version: Version, Opportunities: len(s.Opportunities), CapacityBlocks: s.CapacityBlocks, QuotaDenials: s.QuotaDenials, AdmittedStarts: s.AdmittedStarts, Accepted: s.Accepted, Failed: s.Failed}
	seen := map[string]bool{}
	for _, o := range s.Opportunities {
		if o.ID == "" || seen[o.ID] {
			return ReplayResult{}, errors.New("replay opportunity ids must be unique")
		}
		seen[o.ID] = true
		if o.Eligible {
			r.Eligible++
		}
		if o.Abstained {
			r.Abstentions++
		}
		if o.Dispatched {
			return ReplayResult{}, errors.New("replay may not dispatch")
		}
	}
	return r, nil
}
