package pilot

import (
	"encoding/json"
	"testing"
)

func TestUnknownMetricNumbersRemainNull(t *testing.T) {
	b, e := json.Marshal(MetricResult{Name: "unknown"})
	if e != nil {
		t.Fatal(e)
	}
	var m map[string]any
	if e = json.Unmarshal(b, &m); e != nil {
		t.Fatal(e)
	}
	for _, k := range []string{"estimate", "lower", "upper"} {
		if m[k] != nil {
			t.Fatalf("unknown %s became zero: %s", k, b)
		}
	}
}
