package usage

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
)

var digestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var safePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/@+ -]{0,255}$`)
var pinVersionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

func safe(s string) bool {
	if strings.TrimSpace(s) == "" || !safePattern.MatchString(s) {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func timestamp(s string) bool {
	if s == "" {
		return false
	}
	_, e := time.Parse(time.RFC3339Nano, s)
	return e == nil
}
func nonnegative(n json.Number) bool { r, err := NumberRat(n); return err == nil && r.Sign() >= 0 }
func number(n *json.Number) bool     { return n == nil || nonnegative(*n) }
func signedNumber(n *json.Number) bool {
	if n == nil {
		return true
	}
	_, err := NumberRat(*n)
	return err == nil
}
func unique(xs []string) bool {
	seen := map[string]bool{}
	for _, x := range xs {
		if !safe(x) || seen[x] {
			return false
		}
		seen[x] = true
	}
	return true
}

func ValidatePolicy(p Policy) error {
	if p.Version != Version {
		return fmt.Errorf("unsupported policy version %q", p.Version)
	}
	seen := map[string]bool{}
	for _, s := range p.Sources {
		if !safe(s.ID) || seen[s.ID] {
			return fmt.Errorf("empty or duplicate policy source %q", s.ID)
		}
		seen[s.ID] = true
		if !safe(s.Host) || !safe(s.HostVersion) || !safe(s.AdapterVersion) || !safe(s.Authority) || s.MaxAgeSeconds < 1 || s.MaxAgeSeconds > 86400 || len(s.AuthModes) == 0 || len(s.Metrics) == 0 || !unique(s.AuthModes) || !unique(s.Metrics) {
			return fmt.Errorf("invalid source policy %q", s.ID)
		}
		for _, m := range s.Metrics {
			if !validMetric(m) {
				return fmt.Errorf("unsupported metric %q", m)
			}
		}
	}
	for id, n := range p.SafetyMargins {
		if !safe(id) || !nonnegative(n) {
			return fmt.Errorf("invalid safety margin %q", id)
		}
	}
	return nil
}
func ValidateBinding(b Binding) error {
	if !safe(b.ID) || !safe(b.PoolID) || !safe(b.Provider) || !safe(b.Service) || !safe(b.Host) || !safe(b.HostVersion) || !safe(b.AuthMode) || !safe(b.SourceID) || !safe(b.StreamID) || !unique(b.ConfigurationIDs) || !unique(b.ConstraintIDs) {
		return fmt.Errorf("invalid binding %q", b.ID)
	}
	return nil
}
func ValidateDemand(d Demand) error {
	if d.Version != Version || !safe(d.ConfigurationID) || !digestPattern.MatchString(d.ProfileSHA256) || !safe(d.Source) || !timestamp(d.ValidUntil) || len(d.Amounts) == 0 {
		return fmt.Errorf("invalid demand")
	}
	seen := map[string]bool{}
	for _, a := range d.Amounts {
		if !digestPattern.MatchString(a.ConstraintID) || !safe(a.Generation) || !validUnit(a.Unit) || !nonnegative(a.Amount) || seen[a.ConstraintID] {
			return fmt.Errorf("invalid or duplicate demand amount")
		}
		seen[a.ConstraintID] = true
	}
	return nil
}
func ValidateExport(e Export, p Policy) error {
	if err := ValidatePolicy(p); err != nil {
		return err
	}
	if e.Version != Version {
		return fmt.Errorf("unsupported export version %q", e.Version)
	}
	if e.LedgerRevision < 0 || !digestPattern.MatchString(e.PolicySHA256) {
		return fmt.Errorf("invalid export revision or policy digest")
	}
	binds := map[string]Binding{}
	for _, b := range e.Bindings {
		if err := ValidateBinding(b); err != nil {
			return err
		}
		if _, ok := binds[b.ID]; ok {
			return fmt.Errorf("duplicate binding %q", b.ID)
		}
		binds[b.ID] = b
	}
	constraints := map[string]Constraint{}
	quotaIDs := map[string]bool{}
	for _, b := range e.Bindings {
		for _, id := range b.ConstraintIDs {
			constraints[id] = Constraint{ID: id}
		}
	}
	for _, q := range e.Quotas {
		c := q.Constraint
		if !validConstraint(c) || ConstraintID(c) != c.ID {
			return fmt.Errorf("invalid constraint %q", c.ID)
		}
		if declared, ok := constraints[c.ID]; !ok || (declared.PoolID != "" && declared.PoolID != c.PoolID) {
			return fmt.Errorf("quota %q references an unbound constraint", q.ID)
		}
		if old, ok := constraints[c.ID]; ok && old.PoolID != "" && old != c {
			return fmt.Errorf("constraint identity collision")
		}
		constraints[c.ID] = c
		if !safe(q.ID) || quotaIDs[q.ID] {
			return fmt.Errorf("empty or duplicate quota %q", q.ID)
		}
		quotaIDs[q.ID] = true
		if !safe(q.ID) || !safe(q.Generation) || q.Revision < 0 || !validOrigin(q.Origin) || !timestamp(q.ExpiresAt) || q.Precision == "" || !in(q.Completeness, "full", "sparse", "unavailable") || !signedNumber(q.Remaining) || !number(q.Used) || !number(q.Limit) {
			return fmt.Errorf("invalid quota %q", q.ID)
		}
		if q.ResetAt != nil && !timestamp(*q.ResetAt) {
			return fmt.Errorf("invalid reset time")
		}
		if !unique(q.CoveredChargeIDs) {
			return fmt.Errorf("invalid covered charge ids")
		}
		streams := map[string]bool{}
		for _, binding := range e.Bindings {
			if binding.SourceID == q.Origin.SourceID && binding.Host == q.Origin.Host && binding.HostVersion == q.Origin.HostVersion && binding.AuthMode == q.Origin.AuthMode && binding.PoolID == c.PoolID && binding.Provider == c.Provider && binding.Service == c.Service {
				streams[binding.StreamID] = true
			}
		}
		if len(streams) > 1 {
			return fmt.Errorf("quota source identity ambiguously names multiple capture streams")
		}
	}
	events := map[string]Event{}
	for _, v := range e.Events {
		key := eventScopeKey(v)
		if !safe(v.ID) || events[key].ID != "" {
			return fmt.Errorf("empty or duplicate event %q in source scope", v.ID)
		}
		events[key] = v
		if !validOrigin(v.Origin) || !validMetric(v.Metric) || !validUnit(v.Unit) || !nonnegative(v.Value) || !in(v.Semantics, "incremental", "cumulative", "snapshot") || !in(v.DedupeCoverage, "strong", "scoped", "weak", "none") || v.Sequence < 0 {
			return fmt.Errorf("invalid event %q", v.ID)
		}
		binding, ok := binds[v.BindingID]
		if !ok {
			return fmt.Errorf("event %q references unknown binding", v.ID)
		}
		if v.Origin.SourceID != binding.SourceID || v.Origin.Host != binding.Host || v.Origin.HostVersion != binding.HostVersion || v.Origin.AuthMode != binding.AuthMode {
			return fmt.Errorf("event %q origin does not match binding", v.ID)
		}
		if v.CorrectionOf != nil && !safe(*v.CorrectionOf) {
			return fmt.Errorf("invalid correction reference")
		}
		if v.SubsetOf != nil && !safe(*v.SubsetOf) {
			return fmt.Errorf("invalid subset reference")
		}
		if v.ParentCallID != nil && !safe(*v.ParentCallID) {
			return fmt.Errorf("invalid parent reference")
		}
		if err := validateAttribution(v.Attribution); err != nil {
			return fmt.Errorf("event %q: %w", v.ID, err)
		}
	}
	for _, v := range e.Events {
		if v.CorrectionOf != nil {
			target, ok := events[scopedEventKey(v, *v.CorrectionOf)]
			if !ok {
				return fmt.Errorf("event %q correction target is missing or outside its source scope", v.ID)
			}
			if target.Metric != v.Metric || target.Unit != v.Unit || target.CallID != v.CallID || target.CounterEpoch != v.CounterEpoch {
				return fmt.Errorf("event %q correction target has different series identity", v.ID)
			}
		}
	}
	if err := validateCorrectionCycles(e.Events, events); err != nil {
		return err
	}
	chargeIDs := map[string]bool{}
	for _, c := range e.Charges {
		if !safe(c.ID) || chargeIDs[c.ID] || !digestPattern.MatchString(c.ConstraintID) || !safe(c.Generation) || !validUnit(c.Unit) || !nonnegative(c.Amount) {
			return fmt.Errorf("invalid charge %q", c.ID)
		}
		if known, ok := constraints[c.ConstraintID]; !ok || (known.PoolID != "" && known.Unit != c.Unit) {
			return fmt.Errorf("charge %q constraint or unit mismatch", c.ID)
		}
		chargeIDs[c.ID] = true
	}
	charges := map[string]Charge{}
	for _, c := range e.Charges {
		charges[c.ID] = c
	}
	for _, q := range e.Quotas {
		for _, id := range q.CoveredChargeIDs {
			c, ok := charges[id]
			if !ok {
				return fmt.Errorf("quota %q covers unknown charge %q", q.ID, id)
			}
			if c.ConstraintID != q.Constraint.ID || c.Generation != q.Generation {
				return fmt.Errorf("quota %q covers charge from another constraint generation", q.ID)
			}
		}
	}
	resIDs := map[string]bool{}
	reservationByID := map[string]Reservation{}
	for _, r := range e.Reservations {
		if !safe(r.ID) || resIDs[r.ID] || !safe(r.Authority) || !safe(r.AuthorityEpoch) || !safe(r.Owner) || !safe(r.AttemptID) || !safe(r.ConfigurationID) || !digestPattern.MatchString(r.PolicySHA256) || !digestPattern.MatchString(r.SnapshotSHA256) || !digestPattern.MatchString(r.ProfileSHA256) || !digestPattern.MatchString(r.DemandSHA256) || !timestamp(r.DemandValidUntil) || r.Fence < 1 || !timestamp(r.LeaseUntil) || !in(r.State, "held", "in-flight", "uncertain", "settled", "released") {
			return fmt.Errorf("invalid reservation %q", r.ID)
		}
		resIDs[r.ID] = true
		reservationByID[r.ID] = r
		amountIDs := map[string]bool{}
		for _, a := range r.Amounts {
			if !digestPattern.MatchString(a.ConstraintID) || amountIDs[a.ConstraintID] || !safe(a.Generation) || !validUnit(a.Unit) || !nonnegative(a.Amount) {
				return fmt.Errorf("invalid reservation amount")
			}
			known, ok := constraints[a.ConstraintID]
			if !ok || (known.PoolID != "" && known.Unit != a.Unit) {
				return fmt.Errorf("reservation references unknown constraint or mismatched unit")
			}
			amountIDs[a.ConstraintID] = true
		}
	}
	for _, c := range e.Charges {
		if c.ReservationID != nil {
			r, ok := reservationByID[*c.ReservationID]
			if !ok {
				return fmt.Errorf("charge %q references unknown reservation", c.ID)
			}
			found := false
			for _, a := range r.Amounts {
				if a.ConstraintID == c.ConstraintID && a.Generation == c.Generation && a.Unit == c.Unit {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("charge %q does not match its reservation amount", c.ID)
			}
		}
	}
	paths := map[string]bool{}
	for _, a := range e.Evidence {
		if !safeRelative(a.Path) || paths[a.Path] || !digestPattern.MatchString(a.SHA256) {
			return fmt.Errorf("invalid evidence asset %q", a.Path)
		}
		paths[a.Path] = true
	}
	return nil
}
func validOrigin(o Origin) bool {
	return safe(o.SourceID) && safe(o.Host) && safe(o.HostVersion) && safe(o.AdapterVersion) && safe(o.AuthMode) && timestamp(o.ObservedAt) && timestamp(o.ReceivedAt)
}
func validConstraint(c Constraint) bool {
	return c.ID != "" && safe(c.PoolID) && safe(c.Provider) && safe(c.Service) && safe(c.LimitID) && validMetric(c.Metric) && validUnit(c.Unit) && metricUnitMatch(c.Metric, c.Unit) && safe(c.Scope) && in(c.WindowKind, "fixed", "rolling", "token-bucket", "concurrency", "non-resetting", "unknown")
}
func validMetric(s string) bool {
	return in(s, "input-tokens", "output-tokens", "cached-input-tokens", "cache-creation-tokens", "reasoning-tokens", "requests", "percentage", "credits", "currency")
}
func validUnit(s string) bool {
	return in(s, "tokens", "requests", "percentage-points", "credits", "USD", "EUR", "GBP", "JPY", "CAD", "AUD", "CHF")
}
func safeRelative(s string) bool {
	if s == "" || strings.HasPrefix(s, "/") || strings.Contains(s, "\\") || strings.Contains(s, ":") {
		return false
	}
	for _, x := range strings.Split(s, "/") {
		if x == "" || x == "." || x == ".." {
			return false
		}
	}
	return true
}
func in(s string, xs ...string) bool {
	for _, x := range xs {
		if s == x {
			return true
		}
	}
	return false
}

func eventScopeKey(v Event) string { return scopedEventKey(v, v.ID) }
func scopedEventKey(v Event, id string) string {
	return v.Origin.SourceID + "\x00" + v.BindingID + "\x00" + v.SessionID + "\x00" + id
}
func validateAttribution(a Attribution) error {
	for _, x := range []*string{a.ConfigurationID, a.Provider, a.Model, a.ProjectID, a.AttemptID} {
		if x != nil && !safe(*x) {
			return fmt.Errorf("unsafe attribution value")
		}
	}
	if a.CatalogSHA256 != nil && !digestPattern.MatchString(*a.CatalogSHA256) {
		return fmt.Errorf("invalid catalog digest")
	}
	if a.WorkItemRef != nil && (a.WorkItemRef.Kind != "WorkItemBinding" || !safe(a.WorkItemRef.ID) || !pinVersionPattern.MatchString(a.WorkItemRef.Version)) {
		return fmt.Errorf("invalid P1 work-item reference pin")
	}
	if a.RunRef != nil && (a.RunRef.Kind != "RunRecord" || !safe(a.RunRef.ID) || !pinVersionPattern.MatchString(a.RunRef.Version)) {
		return fmt.Errorf("invalid P1 run reference pin")
	}
	return nil
}
func validateCorrectionCycles(all []Event, by map[string]Event) error {
	for _, start := range all {
		seen := map[string]bool{}
		cur := start
		for cur.CorrectionOf != nil {
			key := eventScopeKey(cur)
			if seen[key] {
				return fmt.Errorf("correction cycle at event %q", cur.ID)
			}
			seen[key] = true
			next, ok := by[scopedEventKey(cur, *cur.CorrectionOf)]
			if !ok {
				break
			}
			cur = next
		}
	}
	return nil
}

func metricUnitMatch(metric, unit string) bool {
	switch metric {
	case "input-tokens", "output-tokens", "cached-input-tokens", "cache-creation-tokens", "reasoning-tokens":
		return unit == "tokens"
	case "requests":
		return unit == "requests"
	case "percentage":
		return unit == "percentage-points"
	case "credits":
		return unit == "credits"
	case "currency":
		return in(unit, "USD", "EUR", "GBP", "JPY", "CAD", "AUD", "CHF")
	}
	return false
}
