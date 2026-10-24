package selection

import (
	"encoding/json"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/evaluation"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestExplicitBoundCannotReplaceQualityOrRenewMeasurements(t *testing.T) {
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	source := []byte("approved conservative maximum; unmeasured")
	if e = os.WriteFile(filepath.Join(root, "source.txt"), source, 0600); e != nil {
		t.Fatal(e)
	}
	at := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	cohort := evaluation.Cohort{ConfigurationID: "cfg"}
	cost, seconds := json.Number("1"), json.Number("2")
	bound := ExplicitUpperBound{Cohort: cohort, Source: Asset{"source.txt", usage.Hash(source)}, ObservedAt: at.Add(-time.Hour).Format(time.RFC3339), ValidUntil: at.Add(time.Hour).Format(time.RFC3339), CostUpper: &cost, Currency: "USD", SecondsUpper: &seconds, Bounds: []evaluation.Bound{{PoolID: "p", WindowFamily: "w", Metric: "requests", Unit: "requests", Upper: &cost}}}
	bounds := []ExplicitUpperBound{bound}
	policy := SelectionPolicy{UpperBounds: &bounds}
	estimate := evaluation.EstimateArtifact{Cohort: cohort, Families: 0, QualityLower: "0", QualityValidUntil: at.Add(-time.Hour).Format(time.RFC3339), DemandValidUntil: at.Add(-time.Hour).Format(time.RFC3339)}
	if e = applyUpperBounds(&estimate, policy, root, at); e != nil {
		t.Fatal(e)
	}
	if estimate.QualityLower != "0" || estimate.Families != 0 || estimate.CostUpper == nil || estimate.Bounds[0].Method != "trusted-policy-upper-bound; unmeasured" {
		t.Fatalf("quality promoted or consumption unlabeled %+v", estimate)
	}
	bounds[0].ObservedAt = at.Add(-8 * 24 * time.Hour).Format(time.RFC3339)
	if applyUpperBounds(&estimate, policy, root, at) == nil {
		t.Fatal("copied policy renewed old bounds")
	}
}
