package usage

import "testing"

func TestWindowFamilySeparatesLimitsAndIsGenerationNeutral(t *testing.T) {
	a := Constraint{PoolID: "p", LimitID: "five-hour", WindowKind: "fixed", Scope: "account", Metric: "percentage", Unit: "percentage-points"}
	b := a
	b.LimitID = "weekly"
	if WindowFamily(a) == WindowFamily(b) {
		t.Fatal("different limits pooled")
	}
	a.ID = "oldconstraint-id"
	b = a
	b.ID = "newconstraint-id"
	if WindowFamily(a) != WindowFamily(b) {
		t.Fatal("mutable ID affected family")
	}
}
