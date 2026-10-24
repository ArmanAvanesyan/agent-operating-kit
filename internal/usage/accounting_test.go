package usage

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestExactDecimalAndCumulativeSummary(t *testing.T) {
	got, err := NumberRat(json.Number("0.125"))
	if err != nil || Decimal(got) != "0.125" {
		t.Fatalf("exact decimal roundtrip: %v %v", got, err)
	}
	base := Event{Origin: Origin{SourceID: "s", Host: "h", ObservedAt: "2026-10-23T00:00:00Z"}, BindingID: "b", SessionID: "x", CallID: "c", Metric: "tokens", Unit: "token", Semantics: "cumulative", Billable: true, DedupeCoverage: "strong"}
	a, b := base, base
	a.ID = "a"
	a.Sequence = 1
	a.Value = "10"
	a.Origin.ObservedAt = "2026-10-23T00:00:01Z"
	b.ID = "b"
	b.Sequence = 2
	b.Value = "16"
	b.Origin.ObservedAt = "2026-10-23T00:00:02Z"
	s := SummaryFor(Export{Events: []Event{b, a}}, Policy{}, time.Date(2026, 10, 23, 0, 1, 0, 0, time.UTC), nil)
	if len(s.Totals) != 1 || s.Totals[0].Value != "16" {
		t.Fatalf("cumulative samples must replace: %+v", s.Totals)
	}
}

func TestSummaryUsesAsOfPoolAndFlagsCounterDecrease(t *testing.T) {
	bindings := []Binding{{ID: "b1", PoolID: "pool-a"}, {ID: "b2", PoolID: "pool-b"}}
	makeEvent := func(id, binding string, sequence int64, value, at string) Event {
		return Event{ID: id, Origin: Origin{SourceID: "s", ObservedAt: at}, BindingID: binding, SessionID: "session", CallID: "call", Metric: "requests", Unit: "requests", Value: json.Number(value), Semantics: "cumulative", Sequence: sequence, Billable: true, DedupeCoverage: "strong"}
	}
	events := []Event{makeEvent("1", "b1", 1, "10", "2026-10-23T00:00:01Z"), makeEvent("2", "b1", 2, "8", "2026-10-23T00:00:03Z"), makeEvent("3", "b2", 1, "4", "2026-10-23T00:00:10Z")}
	s := SummaryFor(Export{Bindings: bindings, Events: events}, Policy{}, time.Date(2026, 10, 23, 0, 0, 4, 0, time.UTC), []string{"pool"})
	if s.Status != "partial" || len(s.Totals) != 0 {
		t.Fatalf("decrease must be partial and future sample excluded: %+v", s)
	}
	if len(s.Diagnostics) == 0 || s.Diagnostics[len(s.Diagnostics)-1].Code != "counter-discontinuity" {
		t.Fatalf("expected counter discontinuity: %+v", s.Diagnostics)
	}
}

func TestSummaryPoolGroupingUsesBoundPool(t *testing.T) {
	e := Event{ID: "e", Origin: Origin{SourceID: "s", ObservedAt: "2026-10-23T00:00:01Z"}, BindingID: "binding-id", Metric: "requests", Unit: "requests", Value: "2", Semantics: "incremental", Billable: true, DedupeCoverage: "strong"}
	s := SummaryFor(Export{Bindings: []Binding{{ID: "binding-id", PoolID: "shared-pool"}}, Events: []Event{e}}, Policy{}, time.Date(2026, 10, 23, 0, 1, 0, 0, time.UTC), []string{"pool"})
	if len(s.Totals) != 1 || s.Totals[0].Group != "pool=shared-pool" {
		t.Fatalf("pool grouping should use the binding's real pool: %+v", s.Totals)
	}
}

func TestSummarySeparatesCumulativeEstimatedCurrency(t *testing.T) {
	base := Origin{SourceID: "s", Host: "h", HostVersion: "1", AdapterVersion: "a", AuthMode: "api", ObservedAt: "2026-10-23T00:00:01Z", ReceivedAt: "2026-10-23T00:00:01Z"}
	est1 := Event{ID: "cost-1", Origin: base, BindingID: "b", CallID: "call", Metric: "currency", Unit: "USD", Value: "2.50", Semantics: "cumulative", Sequence: 1, Billable: false, DedupeCoverage: "strong"}
	est2 := est1
	est2.ID = "cost-2"
	est2.Value = "3.25"
	est2.Sequence = 2
	est2.Origin.ObservedAt = "2026-10-23T00:00:02Z"
	actual := Event{ID: "actual", Origin: base, BindingID: "b", CallID: "other", Metric: "currency", Unit: "USD", Value: "5", Semantics: "incremental", Billable: true, DedupeCoverage: "strong"}
	context := Event{ID: "context", Origin: base, BindingID: "b", CallID: "context", Metric: "input-tokens", Unit: "tokens", Value: "100", Semantics: "cumulative", Billable: false, DedupeCoverage: "strong"}
	p := Policy{Version: Version, Sources: []SourceRule{{ID: "s", Host: "h", HostVersion: "1", AdapterVersion: "a", AuthModes: []string{"api"}, Metrics: []string{"currency", "input-tokens"}, Authority: "fixture", MaxAgeSeconds: 60}}}
	s := SummaryFor(Export{Events: []Event{est1, est2, actual, context}}, p, time.Date(2026, 10, 23, 0, 1, 0, 0, time.UTC), nil)
	if len(s.Totals) != 1 || s.Totals[0].Value != "5" {
		t.Fatalf("estimated currency must not enter billable totals: %+v", s.Totals)
	}
	if len(s.Estimates) != 1 || s.Estimates[0].Value != "3.25" || s.Excluded != 1 {
		t.Fatalf("latest estimated cost should be visible separately while context sample stays excluded: estimates=%+v excluded=%d", s.Estimates, s.Excluded)
	}
}

func TestSummaryExcludesAmbiguousInclusiveParentButCountsSeparateRequests(t *testing.T) {
	now := time.Date(2026, 10, 23, 0, 1, 0, 0, time.UTC)
	poolBinding := Binding{ID: "b", PoolID: "pool", Host: "h", HostVersion: "1", SourceID: "s"}
	origin := Origin{SourceID: "s", Host: "h", HostVersion: "1", AdapterVersion: "a", AuthMode: "api", ObservedAt: "2026-10-23T00:00:01Z"}
	p := Policy{Sources: []SourceRule{{ID: "s", Host: "h", HostVersion: "1", AdapterVersion: "a", AuthModes: []string{"api"}, Metrics: []string{"requests"}, Authority: "fixture"}}}
	parentID := "parent"
	child := Event{ID: "child-event", Origin: origin, BindingID: "b", CallID: "child-call", ParentCallID: &parentID, RequestID: "child-request", Metric: "requests", Unit: "requests", Value: "4", Semantics: "incremental", Billable: true, DedupeCoverage: "strong"}
	parent := Event{ID: "parent-event", Origin: origin, BindingID: "b", CallID: parentID, Metric: "requests", Unit: "requests", Value: "10", Semantics: "incremental", Billable: true, DedupeCoverage: "strong"}
	s := SummaryFor(Export{Bindings: []Binding{poolBinding}, Events: []Event{parent, child}}, p, now, nil)
	if len(s.Totals) != 1 || s.Totals[0].Value != "4" || s.Excluded != 1 || !hasDiagnostic(s, "parent-rollup-ambiguous") {
		t.Fatalf("ambiguous inclusive parent must be excluded: %+v", s)
	}
	parent.RequestID = "parent-request"
	s = SummaryFor(Export{Bindings: []Binding{poolBinding}, Events: []Event{parent, child}}, p, now, nil)
	if len(s.Totals) != 1 || s.Totals[0].Value != "14" {
		t.Fatalf("distinct provider requests are separate billed calls: %+v", s.Totals)
	}
	parent.SubsetOf = &child.ID
	parent.RequestID = ""
	s = SummaryFor(Export{Bindings: []Binding{poolBinding}, Events: []Event{parent, child}}, p, now, nil)
	if len(s.Totals) != 1 || s.Totals[0].Value != "4" || hasDiagnostic(s, "parent-rollup-ambiguous") {
		t.Fatalf("explicit subset parent must not be double counted or flagged ambiguous: %+v", s)
	}
}

func TestSummaryDeduplicatesProviderRequestsOnlyWithinKnownPool(t *testing.T) {
	now := time.Date(2026, 10, 23, 0, 1, 0, 0, time.UTC)
	bindings := []Binding{{ID: "cli-a", PoolID: "pool-a", Host: "host-a", HostVersion: "1", SourceID: "source-a"}, {ID: "cli-b", PoolID: "pool-a", Host: "host-b", HostVersion: "1", SourceID: "source-b"}, {ID: "cli-c", PoolID: "pool-b", Host: "host-c", HostVersion: "1", SourceID: "source-c"}}
	provider := "vendor"
	model := "model-x"
	request := "provider-request-1"
	makeEvent := func(id, binding, source, host string) Event {
		return Event{ID: id, Origin: Origin{SourceID: source, Host: host, HostVersion: "1", AdapterVersion: "a", AuthMode: "api", ObservedAt: "2026-10-23T00:00:01Z"}, BindingID: binding, RequestID: request, CallID: id, Attribution: Attribution{Provider: &provider, Model: &model}, Metric: "requests", Unit: "requests", Value: "7", Semantics: "incremental", Billable: true, DedupeCoverage: "strong"}
	}
	events := []Event{makeEvent("a", "cli-a", "source-a", "host-a"), makeEvent("b", "cli-b", "source-b", "host-b"), makeEvent("c", "cli-c", "source-c", "host-c")}
	p := Policy{Sources: []SourceRule{{ID: "source-a", Host: "host-a", HostVersion: "1", AdapterVersion: "a", AuthModes: []string{"api"}, Metrics: []string{"requests"}, Authority: "fixture"}, {ID: "source-b", Host: "host-b", HostVersion: "1", AdapterVersion: "a", AuthModes: []string{"api"}, Metrics: []string{"requests"}, Authority: "fixture"}, {ID: "source-c", Host: "host-c", HostVersion: "1", AdapterVersion: "a", AuthModes: []string{"api"}, Metrics: []string{"requests"}, Authority: "fixture"}}}
	s := SummaryFor(Export{Bindings: bindings, Events: events}, p, now, []string{"pool"})
	if len(s.Totals) != 2 || s.Totals[0].Group != "pool=pool-a" || s.Totals[0].Value != "7" || s.Totals[1].Group != "pool=pool-b" || s.Totals[1].Value != "7" {
		t.Fatalf("provider request dedupe must be shared-pool scoped: %+v", s.Totals)
	}
	conflicting := append([]Event{}, events...)
	conflicting[1].Value = "8"
	s = SummaryFor(Export{Bindings: bindings, Events: conflicting}, p, now, []string{"pool"})
	if s.Status != "partial" || len(s.Totals) != 1 || s.Totals[0].Group != "pool=pool-b" || !hasDiagnostic(s, "provider-request-conflict") {
		t.Fatalf("conflicting duplicate provider request must remain unknown/partial: %+v", s)
	}
}

func TestProviderRequestFragmentsSumPerObserverBeforeCrossCLIComparison(t *testing.T) {
	now := time.Date(2026, 10, 23, 0, 1, 0, 0, time.UTC)
	provider := "vendor"
	model := "model-x"
	bindings := []Binding{{ID: "cli-a", PoolID: "shared", Host: "a", HostVersion: "1", SourceID: "source-a"}, {ID: "cli-b", PoolID: "shared", Host: "b", HostVersion: "1", SourceID: "source-b"}}
	makeEvent := func(id, binding, source, host, request, value string) Event {
		return Event{ID: id, Origin: Origin{SourceID: source, Host: host, HostVersion: "1", AdapterVersion: "a", AuthMode: "api", ObservedAt: "2026-10-23T00:00:01Z"}, BindingID: binding, RequestID: request, CallID: id, Attribution: Attribution{Provider: &provider, Model: &model}, Metric: "requests", Unit: "requests", Value: json.Number(value), Semantics: "incremental", Billable: true, DedupeCoverage: "strong"}
	}
	events := []Event{makeEvent("a1", "cli-a", "source-a", "a", "req-fragments", "2"), makeEvent("a2", "cli-a", "source-a", "a", "req-fragments", "3"), makeEvent("b1", "cli-b", "source-b", "b", "req-fragments", "5"), makeEvent("same1", "cli-a", "source-a", "a", "req-identical-fragments", "2"), makeEvent("same2", "cli-a", "source-a", "a", "req-identical-fragments", "2")}
	p := Policy{Sources: []SourceRule{{ID: "source-a", Host: "a", HostVersion: "1", AdapterVersion: "a", AuthModes: []string{"api"}, Metrics: []string{"requests"}, Authority: "fixture"}, {ID: "source-b", Host: "b", HostVersion: "1", AdapterVersion: "a", AuthModes: []string{"api"}, Metrics: []string{"requests"}, Authority: "fixture"}}}
	s := SummaryFor(Export{Bindings: bindings, Events: events}, p, now, []string{"pool"})
	if len(s.Totals) != 1 || s.Totals[0].Value != "9" {
		t.Fatalf("same-observer fragments must sum before cross-observer dedupe: %+v", s.Totals)
	}
}

func hasDiagnostic(s Summary, code string) bool {
	for _, d := range s.Diagnostics {
		if d.Code == code {
			return true
		}
	}
	return false
}

func TestNormalizeEventNamespaceAndDecimalCanonicalization(t *testing.T) {
	a := Event{ID: "same", Origin: Origin{SourceID: "source"}, BindingID: "one", SessionID: "session", Metric: "requests", Unit: "requests", Value: "1", Semantics: "incremental"}
	b := a
	b.BindingID = "two"
	out, ds := NormalizeEvents([]Event{a, b})
	if len(out) != 2 || len(ds) != 0 {
		t.Fatalf("same opaque ID in separate bindings must remain distinct: %+v %+v", out, ds)
	}
	replayed := a
	replayed.Origin.AdapterVersion = "new-adapter"
	replayed.Origin.ReceivedAt = "later"
	out, ds = NormalizeEvents([]Event{a, replayed})
	if len(out) != 1 || len(ds) != 0 {
		t.Fatalf("adapter upgrades and later receipt must replay the same physical event: %+v %+v", out, ds)
	}
	weak := Event{Origin: Origin{SourceID: "s"}, BindingID: "b", SessionID: "x", Metric: "requests", Unit: "requests", Value: "1", Billable: true, DedupeCoverage: "weak"}
	out, ds = NormalizeEvents([]Event{weak, weak})
	if len(out) != 2 || len(ds) != 0 {
		t.Fatalf("weak-dedupe events must be retained without exactly-once claims: %+v %+v", out, ds)
	}
	r, _ := NumberRat(json.Number("0.375"))
	if Decimal(r) != "0.375" {
		t.Fatalf("decimal should be canonical: %s", Decimal(r))
	}
}

func TestLoadedRawPolicyDigestWorksForCapacity(t *testing.T) {
	e, p, err := Load("testdata/export.json", "testdata/policy.json")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("testdata/demand.json")
	if err != nil {
		t.Fatal(err)
	}
	var d Demand
	if err = Decode(data, &d); err != nil {
		t.Fatal(err)
	}
	asOf := time.Date(2026, 10, 23, 12, 0, 30, 0, time.UTC)
	got := Capacity(e, p, "opaque-config", &d, asOf)
	if got.Disposition != "observed-fit" {
		t.Fatalf("loaded raw-byte policy digest should match capacity: %+v", got)
	}
}

func TestSharedPoolQuotaAppliesAcrossCLIBindingsOnce(t *testing.T) {
	e, p, err := Load("testdata/export.json", "testdata/policy.json")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("testdata/demand.json")
	if err != nil {
		t.Fatal(err)
	}
	var d Demand
	if err = Decode(data, &d); err != nil {
		t.Fatal(err)
	}
	constraintID := e.Bindings[0].ConstraintIDs[0]
	e.Bindings = append(e.Bindings, Binding{ID: "other-cli", PoolID: "fixture-pool", Provider: "fixture-provider", Service: "fixture-service", Host: "other-cli", HostVersion: "v1", AuthMode: "api", SourceID: "other-source", StreamID: "other-stream", ConfigurationIDs: []string{"other-config"}, ConstraintIDs: []string{constraintID}, ConstraintsComplete: true})
	d.ConfigurationID = "other-config"
	now := time.Date(2026, 10, 23, 12, 0, 30, 0, time.UTC)
	e.Reservations = append(e.Reservations, Reservation{ID: "held-by-first-cli", Authority: "local", AuthorityEpoch: "epoch-1", Owner: "owner", AttemptID: "attempt", ConfigurationID: "opaque-config", PolicySHA256: PolicyHash(p), SnapshotSHA256: strings.Repeat("b", 64), ProfileSHA256: d.ProfileSHA256, DemandSHA256: Digest(d), DemandValidUntil: d.ValidUntil, Fence: 1, LeaseUntil: now.Add(time.Minute).Format(time.RFC3339Nano), State: "held", Amounts: []Amount{{ConstraintID: constraintID, Generation: "fixture-window", Unit: "requests", Amount: "60"}}})
	d.Amounts[0].Amount = "50"
	got := Capacity(e, p, "other-config", &d, now)
	if got.Disposition != "insufficient" || len(got.Balances) != 1 || got.Balances[0].Held != "60" {
		t.Fatalf("shared pool must share one quota and account for another CLI hold: %+v", got)
	}
}

func TestOldGenerationActiveReservationBlocksCapacityFit(t *testing.T) {
	e, p, err := Load("testdata/export.json", "testdata/policy.json")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("testdata/demand.json")
	if err != nil {
		t.Fatal(err)
	}
	var d Demand
	if err = Decode(data, &d); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 23, 12, 0, 30, 0, time.UTC)
	d.Amounts[0].Amount = "1"
	e.Reservations = append(e.Reservations, Reservation{ID: "old-window-inflight", Authority: "local", AuthorityEpoch: "epoch-1", Owner: "owner", AttemptID: "attempt", ConfigurationID: "opaque-config", PolicySHA256: PolicyHash(p), SnapshotSHA256: strings.Repeat("b", 64), ProfileSHA256: d.ProfileSHA256, DemandSHA256: Digest(d), DemandValidUntil: d.ValidUntil, Fence: 1, LeaseUntil: now.Add(-time.Hour).Format(time.RFC3339Nano), State: "uncertain", Amounts: []Amount{{ConstraintID: e.Bindings[0].ConstraintIDs[0], Generation: "old-window", Unit: "requests", Amount: "10"}}})
	got := Capacity(e, p, "opaque-config", &d, now)
	if got.Disposition != "unknown" {
		t.Fatalf("an active old-generation commitment must block observed fit: %+v", got)
	}
}

func TestNormalizeEventsConflictAndCorrection(t *testing.T) {
	e := Event{ID: "one", Origin: Origin{SourceID: "s"}, BindingID: "b", Metric: "m", Unit: "u", Value: "10", Billable: true}
	corrected := e
	corrected.ID = "two"
	corrected.Value = "13"
	corrected.CorrectionOf = &e.ID
	out, ds := NormalizeEvents([]Event{corrected, e})
	if len(ds) != 0 || len(out) != 2 {
		t.Fatalf("normalization must preserve original and correction evidence: %+v %+v", out, ds)
	}
	conflict := e
	conflict.Value = "11"
	out, ds = NormalizeEvents([]Event{e, conflict})
	if len(out) != 2 || len(ds) != 1 || ds[0].Code != "event-conflict" {
		t.Fatalf("conflict must quarantine: %+v %+v", out, ds)
	}
}

func TestCapacityRequiresDemandAndFreshScopedQuota(t *testing.T) {
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	c := Constraint{PoolID: "p", Provider: "vendor", Service: "api", LimitID: "weekly", Metric: "requests", Unit: "requests", Scope: "account", WindowKind: "fixed"}
	c.ID = ConstraintID(c)
	remaining := json.Number("8")
	reset := now.Add(time.Hour).Format(time.RFC3339Nano)
	origin := Origin{SourceID: "s", Host: "h", HostVersion: "1", AdapterVersion: "a", AuthMode: "api", ObservedAt: now.Add(-time.Second).Format(time.RFC3339Nano), ReceivedAt: now.Format(time.RFC3339Nano), FreshnessKnown: true}
	p := Policy{Version: Version, Sources: []SourceRule{{ID: "s", Host: "h", HostVersion: "1", AdapterVersion: "a", AuthModes: []string{"api"}, Metrics: []string{"requests"}, Authority: "vendor", MaxAgeSeconds: 60}}}
	e := Export{Version: Version, PolicySHA256: Digest(p), LedgerRevision: 3, Bindings: []Binding{{ID: "b", PoolID: "p", Provider: "vendor", Service: "api", Host: "h", HostVersion: "1", AuthMode: "api", SourceID: "s", StreamID: "stream", ConfigurationIDs: []string{"cfg"}, ConstraintIDs: []string{c.ID}, ConstraintsComplete: true}}, Quotas: []Quota{{ID: "q", Origin: origin, Constraint: c, Generation: "g1", Revision: 1, Completeness: "full", Remaining: &remaining, ResetAt: &reset, ExpiresAt: now.Add(time.Minute).Format(time.RFC3339Nano), Precision: "exact"}}}
	without := Capacity(e, p, "cfg", nil, now)
	if without.Disposition != "unknown" || len(without.Balances) != 1 || without.Balances[0].Headroom == nil {
		t.Fatalf("balance may be reported, fit must remain unknown: %+v", without)
	}
	d := &Demand{Version: Version, ConfigurationID: "cfg", ProfileSHA256: strings.Repeat("a", 64), Source: "caller", ValidUntil: now.Add(time.Minute).Format(time.RFC3339Nano), Amounts: []Amount{{ConstraintID: c.ID, Generation: "g1", Unit: "requests", Amount: "9"}}}
	with := Capacity(e, p, "cfg", d, now)
	if with.Disposition != "insufficient" {
		t.Fatalf("demand must fit every constraint: %s", with.Disposition)
	}
	mismatch := *d
	mismatch.ConfigurationID = "other"
	if got := Capacity(e, p, "cfg", &mismatch, now); got.Disposition != "unknown" {
		t.Fatalf("mismatched demand must remain unknown: %s", got.Disposition)
	}
}

func TestCapacityRejectsInvalidTypedEvidence(t *testing.T) {
	got := Capacity(Export{}, Policy{}, "cfg", nil, time.Now())
	if got.Status != "invalid" || got.Disposition != "unknown" {
		t.Fatalf("invalid structures must fail closed: %+v", got)
	}
}
