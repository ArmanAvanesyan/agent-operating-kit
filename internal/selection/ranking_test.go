package selection

import (
	"encoding/json"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
	"strings"
	"testing"
	"time"
)

func candidate(id, q, cost, seconds, scarcity string) CandidateResult {
	return CandidateResult{ConfigurationID: id, AccountScope: "account", Eligible: true, Gates: []Gate{{"quality", "pass", "accepted"}}, Rank: &Rank{json.Number(q), json.Number(cost), json.Number(seconds), json.Number(scarcity)}}
}
func TestRankingHysteresisAndHardGates(t *testing.T) {
	at := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
	policy := SelectionPolicy{Priority: "quality"}
	task := TaskProfile{Deadline: at.Add(time.Hour).Format(time.RFC3339)}
	cases := []struct {
		name     string
		age      time.Duration
		q        string
		want     string
		eligible bool
	}{{"retain-first-five-minutes", time.Minute, "0.99", "a", true}, {"switch-at-threshold", 6 * time.Minute, "0.93", "b", true}, {"retain-below-threshold", 6 * time.Minute, "0.919", "a", true}, {"hard-failure-defeats-retention", time.Minute, "0.99", "b", false}}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			a, b := candidate("a", "0.91", "2", "3", "0.1"), candidate("b", tt.q, "1", "2", "0.1")
			a.Eligible = tt.eligible
			d := SelectionDecision{Candidates: []CandidateResult{a, b}}
			prev := SelectionDecision{ConfigurationID: "a", AccountScope: "account", Disposition: "selected", AsOf: at.Add(-tt.age).Format(time.RFC3339)}
			decide(&d, task, policy, &prev, at)
			if d.ConfigurationID != tt.want || d.AuthorizationGranted || d.ExecutionReady {
				t.Fatalf("%+v", d)
			}
		})
	}
	if improvement(Rank{"0.9", "0", "1", "1"}, Rank{"0.9", "0", "1", "1"}, "cost") {
		t.Fatal("zero is non-improvable")
	}
}
func TestDispositionsResetsAndFallback(t *testing.T) {
	at := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
	a := at.Add(time.Minute).Format(time.RFC3339)
	b := at.Add(2 * time.Minute).Format(time.RFC3339)
	c := at.Add(3 * time.Minute).Format(time.RFC3339)
	head, dem := json.Number("0"), json.Number("1")
	mk := func(id string, resets ...*string) CandidateResult {
		r := candidate(id, "0.9", "1", "1", "1")
		r.Eligible = false
		r.Gates = []Gate{{"quality", "pass", "accepted"}, {"capacity", "unknown", "insufficient"}}
		r.Capacity = &usage.CapacitySnapshot{Disposition: "insufficient"}
		for _, reset := range resets {
			r.Capacity.Balances = append(r.Capacity.Balances, usage.Balance{Headroom: &head, Demand: &dem, ResetAt: reset})
		}
		return r
	}
	d := SelectionDecision{Candidates: []CandidateResult{mk("a", &a, &c), mk("b", &b)}}
	decide(&d, TaskProfile{Deadline: at.Add(time.Hour).Format(time.RFC3339)}, SelectionPolicy{Priority: "quality"}, nil, at)
	if d.Disposition != "wait-until" || d.WaitUntil == nil || *d.WaitUntil != b {
		t.Fatalf("latest per route then earliest route: %+v", d)
	}
	d = SelectionDecision{Candidates: []CandidateResult{mk("a", nil)}}
	decide(&d, TaskProfile{Deadline: c}, SelectionPolicy{Priority: "quality"}, nil, at)
	if d.Disposition != "defer" {
		t.Fatal(d.Disposition)
	}
	x, y := candidate("a", "0.9", "1", "1", "1"), candidate("b", "0.99", "1", "1", "1")
	x.Eligible = false
	x.Gates = []Gate{{"quality", "fail", "below floor"}}
	d = SelectionDecision{Candidates: []CandidateResult{x, y}}
	decide(&d, TaskProfile{Deadline: c}, SelectionPolicy{Priority: "quality", Pin: "a"}, nil, at)
	if d.Disposition == "selected" {
		t.Fatal("pin silently falls back")
	}
	d = SelectionDecision{Candidates: []CandidateResult{x, y}}
	decide(&d, TaskProfile{Deadline: c}, SelectionPolicy{Priority: "quality", Pin: "a", Fallback: []Edge{{"a", "b"}}}, nil, at)
	if d.ConfigurationID != "b" {
		t.Fatal("explicit single fallback not honored")
	}
}
func TestClosedVersionedSelectionSchema(t *testing.T) {
	for _, b := range []string{`{"version":"aok-selection/v9"}`, `{"version":"aok-selection/v1","rogue":true}`, `{"version":"aok-selection/v1","candidates":[],"outcomeShards":[],"previousDecision":null,"rogue":true}`} {
		if ValidateJSON([]byte(b), "candidates") == nil {
			t.Fatal("accepted invalid schema", b)
		}
	}
	if _, e := Schema("missing"); e == nil {
		t.Fatal("missing schema")
	}
}
func TestScarcityExactAndNoFutureCapacity(t *testing.T) {
	at := time.Now()
	head, dem := json.Number("100"), json.Number("10")
	r := at.Add(12 * time.Hour).Format(time.RFC3339Nano)
	cap := usage.CapacitySnapshot{Balances: []usage.Balance{{Headroom: &head, Demand: &dem, ResetAt: &r}}}
	s := scarcity(cap, at)
	if rat(s).Cmp(ratNumber("0.05")) > 0 || !strings.HasPrefix(string(s), "0.0") {
		t.Fatal(s)
	}
	if cap.Disposition != "" {
		t.Fatal("ranking changed fit")
	}
}
func TestNumericallyEqualRankDoesNotMutuallyDominate(t *testing.T) {
	a := Rank{"0.90", "1.0", "2", "0"}
	b := Rank{"0.9", "1", "2.0", "0.0"}
	if dominates(a, b) || dominates(b, a) {
		t.Fatal("equal numbers dominated by spelling")
	}
}
