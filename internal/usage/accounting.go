package usage

import (
	"encoding/json"
	"math/big"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// NumberRat parses a JSON decimal without passing through binary floating point.
func NumberRat(n json.Number) (*big.Rat, error) {
	text := string(n)
	if len(text) > 1024 || !decimalLexeme.MatchString(text) {
		return nil, strconv.ErrSyntax
	}
	if i := strings.IndexAny(text, "eE"); i >= 0 {
		exponent, err := strconv.Atoi(text[i+1:])
		if err != nil || exponent < -1000 || exponent > 1000 {
			return nil, strconv.ErrRange
		}
	}
	r, ok := new(big.Rat).SetString(string(n))
	if !ok {
		return nil, strconv.ErrSyntax
	}
	return r, nil
}

var decimalLexeme = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]{1,4})?$`)

// Decimal emits the shortest exact finite decimal when possible, and otherwise
// emits an exact rational spelling. Usage values normally have finite decimals.
func Decimal(r *big.Rat) json.Number {
	if r == nil {
		return json.Number("0")
	}
	s := r.FloatString(decimalPlaces(r.Denom()))
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	if s == "-0" || s == "" {
		s = "0"
	}
	return json.Number(s)
}

func decimalPlaces(d *big.Int) int {
	d = new(big.Int).Set(d)
	two, five := big.NewInt(2), big.NewInt(5)
	count := 0
	for new(big.Int).Mod(d, two).Sign() == 0 {
		d.Quo(d, two)
		count++
	}
	for new(big.Int).Mod(d, five).Sign() == 0 {
		d.Quo(d, five)
		count++
	}
	if d.Cmp(big.NewInt(1)) != 0 {
		return 18
	}
	return count
}

// NormalizeEvents performs deterministic deduplication and revision handling.
// Conflicting duplicate IDs are quarantined; no arrival-order winner is chosen.
func NormalizeEvents(events []Event) ([]Event, []Diagnostic) {
	byID := map[string]map[string]Event{}
	var weakEvents []Event
	var ds []Diagnostic
	for _, e := range events {
		key := eventKey(e)
		if key == "" {
			weakEvents = append(weakEvents, e)
			continue
		}
		if byID[key] == nil {
			byID[key] = map[string]Event{}
		}
		canonical := canonicalEvent(e)
		if old, ok := byID[key][canonical]; !ok || Digest(e) < Digest(old) {
			byID[key][canonical] = e
		}
	}
	out := make([]Event, len(weakEvents), len(weakEvents)+len(byID))
	copy(out, weakEvents)
	for k, variants := range byID {
		if len(variants) > 1 {
			ds = append(ds, Diagnostic{Code: "event-conflict", Field: "events", Message: "conflicting payloads for event key " + k})
		}
		for _, e := range variants {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Origin.ObservedAt != b.Origin.ObservedAt {
			return a.Origin.ObservedAt < b.Origin.ObservedAt
		}
		if a.Sequence != b.Sequence {
			return a.Sequence < b.Sequence
		}
		if eventKey(a) != eventKey(b) {
			return eventKey(a) < eventKey(b)
		}
		return canonicalEvent(a) < canonicalEvent(b)
	})
	sort.Slice(ds, func(i, j int) bool { return ds[i].Message < ds[j].Message })
	return out, append(ds, correctionDiagnostics(out)...)
}

func eventScope(e Event) string {
	return strings.Join([]string{e.Origin.SourceID, e.BindingID, e.SessionID}, "\x00")
}

// Corrections are scoped to a source stream/session. Bad branches and cycles
// remain preserved as evidence but are never treated as canonical replacements.
func correctionDiagnostics(events []Event) []Diagnostic {
	byScopedID := map[string][]Event{}
	for _, e := range events {
		byScopedID[eventScope(e)+"\x00"+e.ID] = append(byScopedID[eventScope(e)+"\x00"+e.ID], e)
	}
	children := map[string][]string{}
	parents := map[string]string{}
	var ds []Diagnostic
	for _, e := range events {
		if e.CorrectionOf == nil {
			continue
		}
		target := eventScope(e) + "\x00" + *e.CorrectionOf
		source := eventScope(e) + "\x00" + e.ID
		if len(byScopedID[target]) == 0 {
			ds = append(ds, Diagnostic{Code: "correction-target-missing", Field: "events.correctionOf", Message: "correction target is missing in its source/session scope: " + *e.CorrectionOf})
			continue
		}
		if len(byScopedID[source]) > 1 || len(byScopedID[target]) > 1 {
			ds = append(ds, Diagnostic{Code: "correction-ambiguous", Field: "events.correctionOf", Message: "correction target or correction ID has conflicting branches"})
			continue
		}
		children[target] = append(children[target], source)
		parents[source] = target
	}
	for target, cs := range children {
		if len(cs) > 1 {
			ds = append(ds, Diagnostic{Code: "correction-branch", Field: "events.correctionOf", Message: "multiple corrections target " + target})
		}
	}
	for start := range parents {
		seen := map[string]bool{}
		cur := start
		for cur != "" {
			if seen[cur] {
				ds = append(ds, Diagnostic{Code: "correction-cycle", Field: "events.correctionOf", Message: "correction chain contains a cycle"})
				break
			}
			seen[cur] = true
			cur = parents[cur]
		}
	}
	sort.Slice(ds, func(i, j int) bool {
		if ds[i].Code != ds[j].Code {
			return ds[i].Code < ds[j].Code
		}
		return ds[i].Message < ds[j].Message
	})
	return ds
}

func validCorrection(e Event, all []Event) bool {
	if e.CorrectionOf == nil {
		return true
	}
	byScope := eventScope(e)
	target := byScope + "\x00" + *e.CorrectionOf
	source := byScope + "\x00" + e.ID
	ids := map[string]int{}
	children := map[string]int{}
	records := map[string]Event{}
	for _, v := range all {
		k := eventScope(v) + "\x00" + v.ID
		ids[k]++
		records[k] = v
		if v.CorrectionOf != nil {
			children[eventScope(v)+"\x00"+*v.CorrectionOf]++
		}
	}
	if ids[source] != 1 || ids[target] != 1 || children[target] != 1 {
		return false
	}
	seen := map[string]bool{}
	cur := e
	for cur.CorrectionOf != nil {
		k := eventScope(cur) + "\x00" + cur.ID
		if seen[k] {
			return false
		}
		seen[k] = true
		parent := eventScope(cur) + "\x00" + *cur.CorrectionOf
		next, ok := records[parent]
		if !ok {
			return false
		}
		cur = next
	}
	return true
}

func eventKey(e Event) string {
	if e.ID != "" {
		return e.Origin.SourceID + "\x00" + e.BindingID + "\x00" + e.SessionID + "\x00" + e.ID
	}
	if e.Sequence != 0 && e.CallID != "" {
		return strings.Join([]string{e.Origin.SourceID, e.BindingID, e.SessionID, e.CallID, strconv.FormatInt(e.Sequence, 10), e.Metric}, "\x00")
	}
	return ""
}
func canonicalEvent(e Event) string {
	// Adapter and receipt metadata describe the observation path, not the
	// underlying event payload. Excluding them makes replay stable across adapter
	// upgrades and re-imports while preserving source event identity and value.
	e.Origin.AdapterVersion = ""
	e.Origin.ReceivedAt = ""
	return Digest(e)
}

func SummaryFor(e Export, p Policy, asOf time.Time, groupBy []string) Summary {
	s := Summary{Report: BaseReport(), AsOf: asOf.UTC().Format(time.RFC3339Nano), Totals: []Total{}}
	normal, ds := NormalizeEvents(e.Events)
	s.Diagnostics = append(s.Diagnostics, ds...)
	if len(ds) > 0 {
		s.Status = "partial"
	}
	bindings := map[string]Binding{}
	for _, b := range e.Bindings {
		bindings[b.ID] = b
	}
	conflicts := map[string]bool{}
	payloads := map[string]map[string]bool{}
	for _, v := range e.Events {
		k := eventKey(v)
		if payloads[k] == nil {
			payloads[k] = map[string]bool{}
		}
		payloads[k][canonicalEvent(v)] = true
	}
	for k, vs := range payloads {
		if k != "" && len(vs) > 1 {
			conflicts[k] = true
		}
	}
	superseded := map[string]bool{}
	for _, v := range normal {
		if v.CorrectionOf != nil && validCorrection(v, normal) {
			superseded[eventScope(v)+"\x00"+*v.CorrectionOf] = true
		}
	}
	groups := map[string]*big.Rat{}
	coverage := map[string]string{}
	estimates := map[string]*big.Rat{}
	estimateCoverage := map[string]string{}
	// Latest cumulative sample replaces prior samples in its logical series.
	type sample struct {
		e  Event
		at time.Time
	}
	series := map[string][]sample{}
	var candidates []Event
	for _, v := range normal {
		if conflicts[eventKey(v)] {
			continue
		}
		if v.CorrectionOf != nil && !validCorrection(v, normal) {
			continue
		}
		if superseded[eventScope(v)+"\x00"+v.ID] {
			continue
		}
		at, err := time.Parse(time.RFC3339Nano, v.Origin.ObservedAt)
		if err != nil || at.After(asOf) {
			continue
		}
		if v.Semantics == "cumulative" {
			key := strings.Join([]string{v.BindingID, v.SessionID, v.TurnID, v.CallID, v.Metric, v.Unit, v.CounterEpoch}, "\x00")
			series[key] = append(series[key], sample{v, at})
			continue
		}
		candidates = append(candidates, v)
	}
	for _, ss := range series {
		sort.Slice(ss, func(i, j int) bool {
			if ss[i].e.Sequence > 0 && ss[j].e.Sequence > 0 && ss[i].e.Sequence != ss[j].e.Sequence {
				return ss[i].e.Sequence < ss[j].e.Sequence
			}
			return ss[i].at.Before(ss[j].at)
		})
		if len(ss) > 0 {
			decrease := false
			for i := 1; i < len(ss); i++ {
				if mustRat(ss[i].e.Value).Cmp(mustRat(ss[i-1].e.Value)) < 0 {
					decrease = true
				}
			}
			if decrease {
				s.Diagnostics = append(s.Diagnostics, Diagnostic{Code: "counter-discontinuity", Field: "events.value", Message: "cumulative counter decreased without a new counter epoch"})
				s.Status = "partial"
				continue
			}
			latest := ss[len(ss)-1].e
			candidates = append(candidates, latest)
		}
	}
	candidates = excludeAmbiguousParentRollups(candidates, bindings, p, &s)
	candidates = dedupeProviderRequests(candidates, bindings, p, &s)
	for _, v := range candidates {
		addSummaryEvent(v, groupBy, groups, coverage, estimates, estimateCoverage, &s, bindings, eventTrustedByPolicy(v, p))
	}
	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		parts := strings.Split(k, "\x00")
		s.Totals = append(s.Totals, Total{Group: parts[0], Metric: parts[1], Unit: parts[2], Value: Decimal(groups[k]), Coverage: coverage[k]})
	}
	estimateKeys := make([]string, 0, len(estimates))
	for k := range estimates {
		estimateKeys = append(estimateKeys, k)
	}
	sort.Strings(estimateKeys)
	s.Estimates = []Total{}
	for _, k := range estimateKeys {
		parts := strings.Split(k, "\x00")
		s.Estimates = append(s.Estimates, Total{Group: parts[0], Metric: parts[1], Unit: parts[2], Value: Decimal(estimates[k]), Coverage: estimateCoverage[k]})
	}
	sort.Slice(s.Diagnostics, func(i, j int) bool {
		if s.Diagnostics[i].Code != s.Diagnostics[j].Code {
			return s.Diagnostics[i].Code < s.Diagnostics[j].Code
		}
		if s.Diagnostics[i].Field != s.Diagnostics[j].Field {
			return s.Diagnostics[i].Field < s.Diagnostics[j].Field
		}
		return s.Diagnostics[i].Message < s.Diagnostics[j].Message
	})
	return s
}

func eventPool(e Event, bindings map[string]Binding) string {
	if b, ok := bindings[e.BindingID]; ok {
		return b.PoolID
	}
	return ""
}

func excludeAmbiguousParentRollups(events []Event, bindings map[string]Binding, p Policy, s *Summary) []Event {
	children := map[string][]Event{}
	for _, child := range events {
		if child.ParentCallID == nil || !child.Billable || child.SubsetOf != nil || !eventTrustedByPolicy(child, p) {
			continue
		}
		pool := eventPool(child, bindings)
		if pool == "" {
			continue
		}
		key := strings.Join([]string{pool, child.Origin.SourceID, *child.ParentCallID, child.Metric, child.Unit}, "\x00")
		children[key] = append(children[key], child)
	}
	blocked := map[string]bool{}
	for _, parent := range events {
		if !parent.Billable || parent.SubsetOf != nil || parent.CallID == "" || !eventTrustedByPolicy(parent, p) {
			continue
		}
		pool := eventPool(parent, bindings)
		if pool == "" {
			continue
		}
		key := strings.Join([]string{pool, parent.Origin.SourceID, parent.CallID, parent.Metric, parent.Unit}, "\x00")
		cs := children[key]
		if len(cs) == 0 {
			continue
		}
		ambiguous := parent.RequestID == ""
		for _, child := range cs {
			if child.RequestID == "" || child.RequestID == parent.RequestID {
				ambiguous = true
			}
		}
		if ambiguous {
			blocked[eventKey(parent)+"\x00"+canonicalEvent(parent)] = true
			s.Status = "partial"
			s.Diagnostics = append(s.Diagnostics, Diagnostic{Code: "parent-rollup-ambiguous", Field: "events.parentCallId", Message: "inclusive parent usage overlaps child calls without distinct provider request IDs; parent rollup was excluded"})
		}
	}
	out := make([]Event, 0, len(events))
	for _, e := range events {
		if !blocked[eventKey(e)+"\x00"+canonicalEvent(e)] {
			out = append(out, e)
		} else {
			s.Excluded++
		}
	}
	return out
}

func dedupeProviderRequests(events []Event, bindings map[string]Binding, p Policy, s *Summary) []Event {
	groups := map[string][]Event{}
	unscoped := make([]Event, 0, len(events))
	for _, e := range events {
		pool := eventPool(e, bindings)
		isEstimate := !e.Billable && e.Metric == "currency" && e.Semantics == "cumulative"
		if (!e.Billable && !isEstimate) || e.RequestID == "" || pool == "" || e.Attribution.Provider == nil || *e.Attribution.Provider == "" || !eventTrustedByPolicy(e, p) {
			unscoped = append(unscoped, e)
			continue
		}
		billing := "billable"
		if isEstimate {
			billing = "estimate"
		}
		key := strings.Join([]string{pool, *e.Attribution.Provider, e.RequestID, e.Metric, e.Unit, billing}, "\x00")
		groups[key] = append(groups[key], e)
	}
	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := append([]Event{}, unscoped...)
	for _, k := range keys {
		items := groups[k]
		observers := map[string][]Event{}
		for _, e := range items {
			observer := e.Origin.SourceID + "\x00" + e.BindingID
			observers[observer] = append(observers[observer], e)
		}
		observerIDs := make([]string, 0, len(observers))
		for id := range observers {
			observerIDs = append(observerIDs, id)
		}
		sort.Strings(observerIDs)
		if len(observerIDs) == 1 {
			out = append(out, observers[observerIDs[0]]...)
			continue
		}
		totals := map[string]*big.Rat{}
		models := map[string]string{}
		conflict := false
		for _, observer := range observerIDs {
			total := new(big.Rat)
			model := ""
			for _, e := range observers[observer] {
				value, err := NumberRat(e.Value)
				if err != nil {
					conflict = true
					break
				}
				total.Add(total, value)
				current := attributionModel(e)
				if current == "" || (model != "" && model != current) {
					conflict = true
					break
				}
				model = current
			}
			totals[observer] = total
			models[observer] = model
		}
		baseline := observerIDs[0]
		for _, observer := range observerIDs[1:] {
			if totals[observer].Cmp(totals[baseline]) != 0 || models[observer] != models[baseline] {
				conflict = true
			}
		}
		if conflict {
			s.Status = "partial"
			s.Excluded += len(items)
			s.Diagnostics = append(s.Diagnostics, Diagnostic{Code: "provider-request-conflict", Field: "events.requestId", Message: "same provider request has conflicting cross-CLI totals or actual models within a shared pool; observations were excluded"})
			continue
		}
		// Keep every fragment from one deterministic observer after the complete
		// observation totals agree across CLIs.
		out = append(out, observers[baseline]...)
	}
	sort.Slice(out, func(i, j int) bool {
		if eventKey(out[i]) != eventKey(out[j]) {
			return eventKey(out[i]) < eventKey(out[j])
		}
		return canonicalEvent(out[i]) < canonicalEvent(out[j])
	})
	return out
}
func attributionModel(e Event) string {
	if e.Attribution.Model == nil {
		return ""
	}
	return *e.Attribution.Model
}
func sameExactValue(a, b json.Number) bool {
	ar, ae := NumberRat(a)
	br, be := NumberRat(b)
	return ae == nil && be == nil && ar.Cmp(br) == 0
}

func addSummaryEvent(v Event, by []string, sums map[string]*big.Rat, cov map[string]string, estimates map[string]*big.Rat, estimateCoverage map[string]string, s *Summary, bindings map[string]Binding, trusted bool) {
	isEstimate := !v.Billable && v.Metric == "currency" && v.Semantics == "cumulative"
	if (!v.Billable && !isEstimate) || v.SubsetOf != nil {
		s.Excluded++
		return
	}
	key := groupValue(v, by, bindings) + "\x00" + v.Metric + "\x00" + v.Unit
	r, err := NumberRat(v.Value)
	if err != nil {
		s.Diagnostics = append(s.Diagnostics, Diagnostic{Code: "invalid-number", Field: "events.value", Message: "event value is not an exact decimal"})
		return
	}
	if isEstimate {
		if estimates[key] == nil {
			estimates[key] = new(big.Rat)
		}
		estimates[key].Add(estimates[key], r)
	} else {
		if sums[key] == nil {
			sums[key] = new(big.Rat)
		}
		sums[key].Add(sums[key], r)
	}
	targetCoverage := cov
	if isEstimate {
		targetCoverage = estimateCoverage
	}
	if !trusted {
		setCoverage(targetCoverage, key, "untrusted")
		s.Status = "partial"
		s.Diagnostics = append(s.Diagnostics, Diagnostic{Code: "untrusted-event-source", Field: "events.origin", Message: "event source is not authorized by the supplied policy; usage is retained with untrusted coverage"})
	} else if v.DedupeCoverage == "weak" || v.DedupeCoverage == "none" {
		setCoverage(targetCoverage, key, "partial")
	} else if targetCoverage[key] == "" {
		targetCoverage[key] = "complete"
	}
	if v.Attribution.ConfigurationID == nil && v.Attribution.Model == nil && v.Attribution.Provider == nil {
		s.Unattributed++
	}
}

func eventTrustedByPolicy(v Event, p Policy) bool {
	for _, r := range p.Sources {
		if r.ID == v.Origin.SourceID && r.Host == v.Origin.Host && r.HostVersion == v.Origin.HostVersion && r.AdapterVersion == v.Origin.AdapterVersion && contains(r.AuthModes, v.Origin.AuthMode) && contains(r.Metrics, v.Metric) && r.Authority != "" {
			return true
		}
	}
	return false
}
func setCoverage(cov map[string]string, key, value string) {
	rank := map[string]int{"complete": 0, "partial": 1, "untrusted": 2}
	if rank[value] > rank[cov[key]] {
		cov[key] = value
	}
}
func groupValue(v Event, by []string, bindings map[string]Binding) string {
	var vals []string
	for _, k := range by {
		x := "unknown"
		switch k {
		case "host":
			x = v.Origin.Host
		case "provider":
			if v.Attribution.Provider != nil {
				x = *v.Attribution.Provider
			}
		case "model":
			if v.Attribution.Model != nil {
				x = *v.Attribution.Model
			}
		case "configuration", "configurationId":
			if v.Attribution.ConfigurationID != nil {
				x = *v.Attribution.ConfigurationID
			}
		case "pool":
			if b, ok := bindings[v.BindingID]; ok {
				x = b.PoolID
			} else {
				x = "unknown"
			}
		case "project":
			if v.Attribution.ProjectID != nil {
				x = *v.Attribution.ProjectID
			}
		case "work-item":
			if v.Attribution.WorkItemRef != nil {
				x = v.Attribution.WorkItemRef.ID
			}
		case "run":
			if v.Attribution.RunRef != nil {
				x = v.Attribution.RunRef.ID
			}
		case "attempt":
			if v.Attribution.AttemptID != nil {
				x = *v.Attribution.AttemptID
			}
		case "parent":
			if v.ParentCallID != nil {
				x = *v.ParentCallID
			}
		case "child":
			x = v.CallID
		}
		vals = append(vals, k+"="+x)
	}
	if len(vals) == 0 {
		return "all"
	}
	return strings.Join(vals, "|")
}

func Capacity(e Export, p Policy, configurationID string, demand *Demand, asOf time.Time) CapacitySnapshot {
	c := CapacitySnapshot{Report: BaseReport(), Version: Version, AsOf: asOf.UTC().Format(time.RFC3339Nano), LedgerRevision: e.LedgerRevision, ExportSHA256: Digest(e), PolicySHA256: PolicyHash(p), ConfigurationID: configurationID, Balances: []Balance{}, Disposition: "unknown"}
	if demand != nil {
		c.DemandSHA256 = Digest(demand)
	}
	if err := ValidateExport(e, p); err != nil {
		c.Status = "invalid"
		c.Diagnostics = append(c.Diagnostics, Diagnostic{Code: "invalid-export", Field: "export", Message: err.Error()})
		return finishCapacity(c)
	}
	if e.PolicySHA256 != PolicyHash(p) {
		c.Status = "invalid"
		c.Diagnostics = append(c.Diagnostics, Diagnostic{Code: "policy-digest-mismatch", Field: "export.policySha256", Message: "export is pinned to a different accounting policy"})
		return finishCapacity(c)
	}
	if demand != nil {
		if err := ValidateDemand(*demand); err != nil {
			c.Status = "invalid"
			c.Diagnostics = append(c.Diagnostics, Diagnostic{Code: "invalid-demand", Field: "demand", Message: err.Error()})
			return finishCapacity(c)
		}
	}
	if configurationID == "" {
		c.Diagnostics = append(c.Diagnostics, Diagnostic{Code: "unknown-configuration", Field: "configurationId", Message: "configuration ID is required"})
		return finishCapacity(c)
	}
	var relevant []Binding
	constraintSet := map[string]bool{}
	bindingsComplete := true
	for _, b := range e.Bindings {
		for _, id := range b.ConfigurationIDs {
			if id == configurationID {
				relevant = append(relevant, b)
				if !b.ConstraintsComplete {
					bindingsComplete = false
				}
				for _, cid := range b.ConstraintIDs {
					constraintSet[cid] = true
				}
			}
		}
	}
	if len(relevant) == 0 {
		c.Diagnostics = append(c.Diagnostics, Diagnostic{Code: "unknown-binding", Field: "bindings", Message: "no binding covers configuration"})
		return finishCapacity(c)
	}
	if !bindingsComplete {
		c.Diagnostics = append(c.Diagnostics, Diagnostic{Code: "incomplete-binding", Field: "bindings", Message: "binding does not establish complete constraint coverage"})
	}
	if demand != nil {
		if demand.ConfigurationID != configurationID {
			c.Diagnostics = append(c.Diagnostics, Diagnostic{Code: "demand-configuration-mismatch", Field: "demand.configurationId", Message: "demand is bound to a different configuration"})
			demand = nil
		}
	}
	if demand != nil {
		valid, err := time.Parse(time.RFC3339Nano, demand.ValidUntil)
		if err != nil || !asOf.Before(valid) {
			c.Diagnostics = append(c.Diagnostics, Diagnostic{Code: "expired-demand", Field: "demand.validUntil", Message: "demand is expired or has invalid validity"})
			demand = nil
		}
	}
	if demand == nil || demand.ConfigurationID != configurationID || demand.ValidUntil == "" {
		c.Diagnostics = append(c.Diagnostics, Diagnostic{Code: "unknown-demand", Field: "demand", Message: "a matching valid demand is required to determine fit"})
	}
	quotas := map[string][]Quota{}
	for _, q := range e.Quotas {
		if constraintSet[q.Constraint.ID] {
			quotas[q.Constraint.ID] = append(quotas[q.Constraint.ID], q)
		}
	}
	allComplete := true
	allFit := true
	demandComplete := demand != nil
	unknownEvidence := !bindingsComplete
	if !bindingsComplete {
		allComplete = false
		allFit = false
	}
	if len(constraintSet) == 0 {
		c.Diagnostics = append(c.Diagnostics, Diagnostic{Code: "unknown-constraints", Field: "bindings.constraintIds", Message: "configuration has no declared capacity constraints"})
		return finishCapacity(c)
	}
	demandSeen := map[string]bool{}
	for cid := range constraintSet {
		qs := quotas[cid]
		if len(qs) == 0 {
			allComplete = false
			allFit = false
			c.Diagnostics = append(c.Diagnostics, Diagnostic{Code: "missing-quota", Field: "quotas", Message: "missing quota for constraint " + cid})
			continue
		}
		// Select only exact policy-authorized source observations; conflicting peers stay unknown.
		var eligible []Quota
		for _, q := range qs {
			if policyAllows(p, q) && quotaBindingAllowed(e.Bindings, q, cid) {
				eligible = append(eligible, q)
			}
		}
		if len(eligible) == 0 {
			allComplete = false
			allFit = false
			c.Diagnostics = append(c.Diagnostics, Diagnostic{Code: "untrusted-quota-source", Field: "quotas", Message: "no quota observation matches the trusted source policy and binding for constraint " + cid})
			continue
		}
		sourceIDs := map[string]bool{}
		for _, q := range eligible {
			sourceIDs[q.Origin.SourceID] = true
		}
		if len(sourceIDs) > 1 {
			allComplete = false
			allFit = false
			unknownEvidence = true
			c.Diagnostics = append(c.Diagnostics, Diagnostic{Code: "quota-source-conflict", Field: "quotas", Message: "multiple policy-authorized sources make incomparable claims for constraint " + cid})
			continue
		}
		sort.Slice(eligible, func(i, j int) bool { return eligible[i].Revision > eligible[j].Revision })
		q := eligible[0]
		if len(eligible) > 1 && eligible[0].Revision == eligible[1].Revision && Digest(eligible[0]) != Digest(eligible[1]) {
			allComplete = false
			allFit = false
			unknownEvidence = true
			continue
		}
		b := Balance{Constraint: q.Constraint, Generation: q.Generation, QuotaID: q.ID, Status: "unknown", PendingCharges: json.Number("0"), Held: json.Number("0"), SafetyMargin: json.Number("0"), Overdrawn: json.Number("0"), Conservative: false}
		constraintComplete := true
		obs, oe := time.Parse(time.RFC3339Nano, q.Origin.ObservedAt)
		exp, xe := time.Parse(time.RFC3339Nano, q.ExpiresAt)
		if !q.Origin.FreshnessKnown {
			b.Status = "freshness-unknown"
			allComplete = false
			allFit = false
			constraintComplete = false
			unknownEvidence = true
		} else if oe != nil || xe != nil || asOf.Before(obs) || !asOf.Before(exp) || asOf.Sub(obs) > time.Duration(sourceAge(p, q))*time.Second {
			b.Status = "stale"
			allComplete = false
			allFit = false
			constraintComplete = false
		}
		if q.Completeness != "full" {
			b.Status = "incomplete"
			allComplete = false
			allFit = false
			constraintComplete = false
		}
		if q.ResetAt != nil {
			reset, re := time.Parse(time.RFC3339Nano, *q.ResetAt)
			if re == nil && !asOf.Before(reset) {
				b.Status = "reset-unconfirmed"
				allComplete = false
				allFit = false
				constraintComplete = false
			}
		}
		if q.Remaining != nil {
			b.ReportedRemaining = q.Remaining
		} else if q.Constraint.Metric == "percentage" && q.Used != nil {
			percentage := new(big.Rat).Sub(big.NewRat(100, 1), mustRat(*q.Used))
			n := Decimal(percentage)
			b.ReportedRemaining = &n
		} else {
			allComplete = false
			allFit = false
			constraintComplete = false
		}
		rem := new(big.Rat)
		if q.Remaining != nil {
			if rr, err := NumberRat(*q.Remaining); err == nil {
				rem.Set(rr)
			} else {
				allComplete = false
				allFit = false
				constraintComplete = false
			}
		} else if q.Constraint.Metric == "percentage" && q.Used != nil {
			rem.Sub(big.NewRat(100, 1), mustRat(*q.Used))
		}
		pending := new(big.Rat)
		held := new(big.Rat)
		reservationCharges := map[string]*big.Rat{}
		coveredReservationCharges := map[string]*big.Rat{}
		covered := map[string]bool{}
		for _, id := range q.CoveredChargeIDs {
			covered[id] = true
		}
		activeReservations := map[string]bool{}
		reservationMatches := map[string]bool{}
		for _, r := range e.Reservations {
			if r.State == "held" || r.State == "in-flight" || r.State == "uncertain" {
				if r.State != "held" {
					activeReservations[r.ID] = true
				} else if until, er := time.Parse(time.RFC3339Nano, r.LeaseUntil); er != nil || asOf.Before(until) {
					activeReservations[r.ID] = true
				}
				if activeReservations[r.ID] {
					for _, a := range r.Amounts {
						reservationMatches[r.ID+"\x00"+a.ConstraintID+"\x00"+a.Generation] = true
					}
				}
			}
		}
		for _, ch := range e.Charges {
			if ch.ConstraintID == cid && ch.Generation == q.Generation {
				if covered[ch.ID] {
					if ch.ReservationID != nil && activeReservations[*ch.ReservationID] {
						if coveredReservationCharges[*ch.ReservationID] == nil {
							coveredReservationCharges[*ch.ReservationID] = new(big.Rat)
						}
						if x, err := NumberRat(ch.Amount); err == nil {
							coveredReservationCharges[*ch.ReservationID].Add(coveredReservationCharges[*ch.ReservationID], x)
						}
					}
					continue
				}
				if x, err := NumberRat(ch.Amount); err == nil {
					if ch.ReservationID != nil && activeReservations[*ch.ReservationID] && reservationMatches[*ch.ReservationID+"\x00"+cid+"\x00"+q.Generation] {
						if reservationCharges[*ch.ReservationID] == nil {
							reservationCharges[*ch.ReservationID] = new(big.Rat)
						}
						reservationCharges[*ch.ReservationID].Add(reservationCharges[*ch.ReservationID], x)
					} else {
						pending.Add(pending, x)
					}
				} else {
					allComplete = false
					allFit = false
					constraintComplete = false
				}
			}
		}
		for _, r := range e.Reservations {
			if r.State == "held" || r.State == "in-flight" || r.State == "uncertain" {
				if r.State == "held" {
					until, er := time.Parse(time.RFC3339Nano, r.LeaseUntil)
					if er == nil && !asOf.Before(until) {
						continue
					}
				}
				for _, a := range r.Amounts {
					if a.ConstraintID == cid && a.Generation != q.Generation {
						allComplete = false
						allFit = false
						constraintComplete = false
						unknownEvidence = true
						b.Status = "cross-generation-reservation"
						continue
					}
					if a.ConstraintID == cid && a.Generation == q.Generation {
						if r.PolicySHA256 != PolicyHash(p) || r.ConfigurationID == "" {
							allComplete = false
							allFit = false
							constraintComplete = false
							unknownEvidence = true
							b.Status = "reservation-evidence-unknown"
						}
						if x, er := NumberRat(a.Amount); er == nil {
							actual := reservationCharges[r.ID]
							if actual == nil {
								actual = new(big.Rat)
							}
							used := coveredReservationCharges[r.ID]
							if used == nil {
								used = new(big.Rat)
							}
							unspent := new(big.Rat).Sub(x, used)
							if unspent.Sign() < 0 {
								unspent.SetInt64(0)
							}
							if actual.Cmp(unspent) > 0 {
								held.Add(held, actual)
							} else {
								held.Add(held, unspent)
							}
						} else {
							allComplete = false
							allFit = false
						}
					}
				}
			}
		}
		b.PendingCharges = Decimal(pending)
		b.Held = Decimal(held)
		if margin, ok := p.SafetyMargins[cid]; ok {
			b.SafetyMargin = margin
		}
		if b.Status == "unknown" {
			b.Status = "fresh"
		}
		if constraintComplete && b.Status == "fresh" {
			net := new(big.Rat).Sub(rem, pending)
			net.Sub(net, held)
			if m, ok := NumberRat(b.SafetyMargin); ok == nil {
				net.Sub(net, m)
			}
			if net.Sign() < 0 {
				b.Overdrawn = Decimal(new(big.Rat).Neg(net))
				net.SetInt64(0)
			}
			b.Headroom = ptrNum(Decimal(net))
		}
		if demand != nil {
			matched := false
			for _, a := range demand.Amounts {
				if a.ConstraintID == cid && a.Generation == q.Generation && a.Unit == q.Constraint.Unit {
					matched = true
					demandSeen[cid] = true
					b.Demand = &a.Amount
					if b.Headroom == nil || mustRat(*b.Headroom).Cmp(mustRat(a.Amount)) < 0 {
						allFit = false
					}
				}
			}
			if !matched {
				demandComplete = false
				allFit = false
				c.Diagnostics = append(c.Diagnostics, Diagnostic{Code: "unbounded-constraint-demand", Field: "demand.amounts", Message: "demand does not cover constraint " + cid + " with matching unit and generation"})
			}
		}
		c.Balances = append(c.Balances, b)
	}
	if demand != nil {
		for cid := range constraintSet {
			if !demandSeen[cid] {
				demandComplete = false
				allFit = false
			}
		}
		for _, a := range demand.Amounts {
			if !constraintSet[a.ConstraintID] {
				demandComplete = false
				allFit = false
				c.Diagnostics = append(c.Diagnostics, Diagnostic{Code: "extra-demand-constraint", Field: "demand.amounts", Message: "demand references a constraint outside configuration coverage"})
			}
		}
	}
	sort.Slice(c.Balances, func(i, j int) bool { return c.Balances[i].Constraint.ID < c.Balances[j].Constraint.ID })
	if demand == nil || !demandComplete || unknownEvidence {
		c.Disposition = "unknown"
	} else if !allComplete {
		c.Disposition = "refresh-needed"
	} else if allFit {
		c.Disposition = "observed-fit"
	} else {
		c.Disposition = "insufficient"
	}
	return finishCapacity(c)
}
func policyAllows(p Policy, q Quota) bool {
	for _, s := range p.Sources {
		if s.ID == q.Origin.SourceID && s.Host == q.Origin.Host && s.HostVersion == q.Origin.HostVersion && s.AdapterVersion == q.Origin.AdapterVersion && contains(s.AuthModes, q.Origin.AuthMode) && contains(s.Metrics, q.Constraint.Metric) && authorityMatches(s.Authority, q.Constraint) && s.MaxAgeSeconds > 0 && s.MaxAgeSeconds <= 60 {
			return true
		}
	}
	return false
}

func authorityMatches(authority string, c Constraint) bool {
	return authority == c.Provider || authority == c.Service || authority == c.Provider+"/"+c.Service
}

func quotaBindingAllowed(bindings []Binding, q Quota, cid string) bool {
	for _, b := range bindings {
		if b.PoolID != q.Constraint.PoolID || b.Provider != q.Constraint.Provider || b.Service != q.Constraint.Service || b.Host != q.Origin.Host || b.HostVersion != q.Origin.HostVersion || b.AuthMode != q.Origin.AuthMode || b.SourceID != q.Origin.SourceID {
			continue
		}
		for _, id := range b.ConstraintIDs {
			if id == cid {
				return true
			}
		}
	}
	return false
}
func sourceAge(p Policy, q Quota) int {
	for _, s := range p.Sources {
		if s.ID == q.Origin.SourceID && s.Host == q.Origin.Host && s.HostVersion == q.Origin.HostVersion && s.AdapterVersion == q.Origin.AdapterVersion && contains(s.AuthModes, q.Origin.AuthMode) && contains(s.Metrics, q.Constraint.Metric) {
			return s.MaxAgeSeconds
		}
	}
	return 0
}
func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
func ptrNum(n json.Number) *json.Number { return &n }
func mustRat(n json.Number) *big.Rat {
	r, _ := NumberRat(n)
	if r == nil {
		return new(big.Rat)
	}
	return r
}
func finishCapacity(c CapacitySnapshot) CapacitySnapshot {
	c.Digest = ""
	c.Digest = Digest(c)
	return c
}
