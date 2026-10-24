package replay

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func fixture() Dataset {
	return Dataset{Version: Version, Kind: "recorded-state", FrozenPolicySHA256: strings.Repeat("a", 64), FrozenConfigSHA256: strings.Repeat("b", 64), FrozenEstimateSHA256: strings.Repeat("c", 64), FrozenPolicyPath: "frozen/policy.json", FrozenConfigPath: "frozen/config.json", FrozenEstimatePath: "frozen/estimate.json", FrozenAt: "2026-01-01T00:00:00Z", AuditAt: "2026-01-02T00:00:00Z",
		Tasks: []Task{{ID: "t", Class: "repair", Repository: "repo", Family: "f", NearDuplicateGroup: "d", Partition: "locked-test"}},
		Runs:  []Run{{TaskID: "t", Policy: "pinned", Configuration: "cfg", Repetition: 1, Decision: "accept", Gates: Gates{Capability: true, Authorization: true, QualityFloor: true, DataFresh: true}, Outcome: "accepted", Cost: ptr(3), Seconds: ptr(10), EvidenceLabel: "recorded"}},
	}
}

func TestLoadVerifiesFrozenBytesAndSealsReport(t *testing.T) {
	d := fixture()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "frozen"), 0700); err != nil {
		t.Fatal(err)
	}
	artifacts := []struct {
		path   string
		data   []byte
		target *string
	}{{d.FrozenPolicyPath, []byte(`{"policy":"frozen"}`), &d.FrozenPolicySHA256}, {d.FrozenConfigPath, []byte(`{"config":"frozen"}`), &d.FrozenConfigSHA256}, {d.FrozenEstimatePath, []byte(`{"estimate":"frozen"}`), &d.FrozenEstimateSHA256}}
	for _, a := range artifacts {
		if err := os.WriteFile(filepath.Join(root, a.path), a.data, 0600); err != nil {
			t.Fatal(err)
		}
		*a.target = digest(a.data)
	}
	path := filepath.Join(root, "dataset.json")
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	r, err := Replay(loaded, Config{Partition: "locked-test"})
	if err != nil {
		t.Fatal(err)
	}
	if !r.FrozenArtifactsVerified || r.DatasetSHA256 != digest(b) || !r.SuppliedOutcomeLabelsUnverified || r.AcceptanceEvidenceQualified || r.DecisionProjectionSource != "supplied-projection" {
		t.Fatalf("expected verified frozen inputs and dataset digest: %+v", r)
	}
	loaded.AuditAt = "2026-01-03T00:00:00Z"
	r, err = Replay(loaded, Config{Partition: "locked-test"})
	if err != nil {
		t.Fatal(err)
	}
	if r.FrozenArtifactsVerified {
		t.Fatal("mutated loaded dataset retained its verification seal")
	}
}

func TestCheckedInRecordedFixtureLoadsAndReplays(t *testing.T) {
	d, err := Load(filepath.Join("testdata", "recorded.json"))
	if err != nil {
		t.Fatal(err)
	}
	r, err := Replay(d, Config{Partition: "locked-test"})
	if err != nil {
		t.Fatal(err)
	}
	if !r.FrozenArtifactsVerified || r.Metrics["pinned"].Accepted != 1 || !r.SuppliedOutcomeLabelsUnverified || r.AcceptanceEvidenceQualified {
		t.Fatalf("fixture provenance or metrics unexpected: %+v", r)
	}
}
func ptr(v float64) *json.Number { n := json.Number(strconv.FormatFloat(v, 'f', -1, 64)); return &n }
func num(v string) json.Number   { return json.Number(v) }

func TestRecordedMetricsKeepUnknownAndSuppliedLabelSemantics(t *testing.T) {
	d := fixture()
	d.Runs = append(d.Runs, Run{TaskID: "t", Policy: "pinned", Configuration: "cfg", Repetition: 2, Decision: "accept", Gates: Gates{Capability: true, Authorization: true, QualityFloor: true, DataFresh: true}, Outcome: "rejected", EvidenceLabel: "recorded"})
	r, err := Replay(d, Config{Partition: "locked-test"})
	if err != nil {
		t.Fatal(err)
	}
	m := r.Metrics["pinned"]
	if m.Accepted != 1 || m.ConsequentialMisses != 1 || m.UnknownCostAttempts != 1 || m.CostPerAccepted != nil {
		t.Fatalf("unknown cost must prevent a fabricated free-cost aggregate: %+v", m)
	}
	if m.LabelSource != "recorded-state" || !m.SafetyDenominator {
		t.Fatalf("unexpected provenance: %+v", m)
	}
}

func TestBlockedAttemptsDoNotAccrueCostTimeOrUnknownUsage(t *testing.T) {
	d := fixture()
	d.Runs = append(d.Runs,
		Run{TaskID: "t", Policy: "quota-aware", Configuration: "cfg", Repetition: 7, Decision: "accept", Gates: Gates{Capability: true, Authorization: true, QualityFloor: true, DataFresh: true}, Capacity: num("10"), Reserved: num("0"), Required: num("5"), QuotaStateKnown: true, Outcome: "accepted", Cost: ptr(4), Seconds: ptr(8), EvidenceLabel: "recorded"},
		Run{TaskID: "t", Policy: "quota-aware", Configuration: "cfg", Repetition: 8, Decision: "accept", Gates: Gates{Capability: true, Authorization: true, QualityFloor: true, DataFresh: true}, Capacity: num("2"), Reserved: num("0"), Required: num("5"), QuotaStateKnown: true, Outcome: "unknown", Cost: ptr(99), Seconds: ptr(99), EvidenceLabel: "recorded"},
	)
	r, err := Replay(d, Config{Partition: "locked-test"})
	if err != nil {
		t.Fatal(err)
	}
	m := r.Metrics["quota-aware"]
	if m.Opportunities != 2 || m.AttemptedStarts != 2 || m.AdmittedStarts != 1 || m.Accepted != 1 || m.QuotaBlocked != 1 {
		t.Fatalf("unexpected attempt accounting: %+v", m)
	}
	if m.UnknownCostAttempts != 0 || m.UnknownTimeAttempts != 0 || m.CostPerAccepted == nil || *m.CostPerAccepted != 4 || m.SecondsPerAccepted == nil || *m.SecondsPerAccepted != 8 {
		t.Fatalf("blocked attempt polluted cost/time: %+v", m)
	}
}

func TestSyntheticReservationsAndClockPassage(t *testing.T) {
	d := fixture()
	d.Kind = "synthetic-closed-loop"
	d.Runs[0].EvidenceLabel = "supplied-synthetic"
	d.Runs[0].Pool = "x"
	d.Events = []Event{{At: 0, Type: "replenish", Pool: "x", Amount: num("10")}, {At: 0, Type: "reserve", Pool: "x", Amount: num("8")}, {At: 1, Type: "reserve", Pool: "x", Amount: num("4")}, {At: 99, Type: "release", Pool: "x", Amount: num("4")}}
	r, err := Replay(d, Config{Partition: "locked-test"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Metrics["pinned"].OversubscriptionEvents != 1 {
		t.Fatalf("expected guarded over-admission signal: %+v", r.Metrics["pinned"])
	}
	// An empty interval adds no event and therefore cannot replenish a pool.
	d.Events = []Event{{At: 0, Type: "replenish", Pool: "x", Amount: num("10")}, {At: 500, Type: "reserve", Pool: "x", Amount: num("11")}}
	r, err = Replay(d, Config{Partition: "locked-test"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Metrics["pinned"].OversubscriptionEvents != 1 {
		t.Fatalf("clock passage changed quota: %+v", r.Metrics["pinned"])
	}
}

func TestSyntheticReplayInterleavesAdmissionsWithExplicitQuotaEvents(t *testing.T) {
	d := fixture()
	d.Kind = "synthetic-closed-loop"
	d.Events = []Event{{At: 0, Type: "replenish", Pool: "pool", Amount: num("5")}}
	makeRun := func(policy string, rep int, at int64, required float64) Run {
		return Run{TaskID: "t", Policy: policy, Configuration: "cfg", Repetition: rep, At: at, Pool: "pool", Decision: "accept", Gates: Gates{Capability: true, Authorization: true, QualityFloor: true, DataFresh: true}, Required: num(strconv.FormatFloat(required, 'f', -1, 64)), Outcome: "accepted", EvidenceLabel: "supplied-synthetic"}
	}
	d.Runs = []Run{makeRun("quota-aware", 1, 0, 4), makeRun("quota-aware", 2, 1, 2), makeRun("reservation-aware", 1, 0, 4), makeRun("reservation-aware", 2, 1, 2)}
	d.Events = append(d.Events, Event{At: 0, Type: "reserve", Pool: "pool", Amount: num("1")})
	r, err := Replay(d, Config{Partition: "locked-test"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Metrics["quota-aware"].Accepted != 1 || r.Metrics["quota-aware"].QuotaBlocked != 1 {
		t.Fatalf("quota-aware admission was not guarded: %+v", r.Metrics["quota-aware"])
	}
	if r.Metrics["reservation-aware"].Accepted != 1 || r.Metrics["reservation-aware"].QuotaBlocked != 1 {
		t.Fatalf("reservation-aware admission ignored reservations: %+v", r.Metrics["reservation-aware"])
	}
	if r.Metrics["quota-aware"].OversubscriptionEvents != 0 || r.Metrics["reservation-aware"].OversubscriptionEvents != 0 {
		t.Fatal("guarded starts oversubscribed capacity")
	}
}

func TestFractionalQuotaBoundaryDoesNotOverAdmit(t *testing.T) {
	d := fixture()
	d.Kind = "synthetic-closed-loop"
	d.Events = []Event{{At: 0, Type: "replenish", Pool: "p", Amount: num("0.4")}, {At: 0, Type: "reserve", Pool: "p", Amount: num("0.3")}}
	d.Runs = []Run{{TaskID: "t", Policy: "reservation-aware", Configuration: "cfg", Repetition: 1, At: 1, Pool: "p", Decision: "accept", Gates: Gates{Capability: true, Authorization: true, QualityFloor: true, DataFresh: true}, Required: num("0.10000000000000002"), Outcome: "accepted", EvidenceLabel: "supplied-synthetic"}}
	r, err := Replay(d, Config{Partition: "locked-test"})
	if err != nil {
		t.Fatal(err)
	}
	m := r.Metrics["reservation-aware"]
	if m.Accepted != 0 || m.QuotaBlocked != 1 || m.OversubscriptionEvents != 0 {
		t.Fatalf("fractional demand exceeded exact reserved headroom: %+v", m)
	}
}

func TestCommonGateCannotBeOverriddenByCapacityAblation(t *testing.T) {
	d := fixture()
	d.Runs[0].Gates.QualityFloor = false
	r, err := Replay(d, Config{Partition: "locked-test", UnsafeCapacityAblation: true})
	if err != nil {
		t.Fatal(err)
	}
	if r.Metrics["pinned"].Accepted != 0 || r.Metrics["pinned"].ConsequentialMisses != 1 {
		t.Fatalf("common quality gate was bypassed: %+v", r.Metrics["pinned"])
	}
}

func TestUnsafeAblationExcludedFromSafetyDenominator(t *testing.T) {
	d := fixture()
	d.Kind = "synthetic-closed-loop"
	d.Runs[0].EvidenceLabel = "supplied-synthetic"
	d.Runs[0].Pool = "x"
	r, err := Replay(d, Config{Partition: "locked-test", UnsafeCapacityAblation: true})
	if err != nil {
		t.Fatal(err)
	}
	if r.Metrics["pinned"].SafetyDenominator {
		t.Fatal("unsafe counterfactual entered safety denominator")
	}
}

func TestAdmissionSharesSafetyGatesAndReservationAwareCapacity(t *testing.T) {
	g := Gates{Capability: true, Authorization: true, QualityFloor: true, DataFresh: true}
	state := CapacityState{Capacity: num("10"), Reserved: num("7"), Required: num("4"), Known: true}
	if got := Admit("quota-aware", g, state, false); !got.Allowed {
		t.Fatalf("quota-aware should see common capacity: %+v", got)
	}
	if got := Admit("reservation-aware", g, state, false); got.Allowed || got.Reason != "insufficient-capacity" {
		t.Fatalf("reservation-aware must account for reservations: %+v", got)
	}
	g.QualityFloor = false
	if got := Admit("pinned", g, state, true); got.Allowed || got.Reason != "quality-floor" {
		t.Fatalf("ablation bypassed quality gate: %+v", got)
	}
}

func TestUnknownQuotaStateIsReportedAndDoesNotBecomeZero(t *testing.T) {
	d := fixture()
	d.Runs[0].Policy = "quota-aware"
	d.Runs[0].Capacity = num("0")
	d.Runs[0].QuotaStateKnown = false
	r, err := Replay(d, Config{Partition: "locked-test"})
	if err != nil {
		t.Fatal(err)
	}
	m := r.Metrics["quota-aware"]
	if m.Accepted != 0 || m.UnknownCapacityAttempts != 1 || m.QuotaBlocked != 0 {
		t.Fatalf("unknown capacity was scored as zero or as a provider block: %+v", m)
	}
}

func TestCompareRequiresMatchedFivePolicyTrialGrid(t *testing.T) {
	d := fixture()
	base := d.Runs[0]
	d.Runs = nil
	for _, p := range Policies {
		r := base
		r.Policy = p
		r.Configuration = "cfg"
		d.Runs = append(d.Runs, r)
	}
	if _, err := Compare(d, Config{Partition: "locked-test"}); err != nil {
		t.Fatal(err)
	}
	d.Runs = d.Runs[:len(d.Runs)-1]
	if _, err := Compare(d, Config{Partition: "locked-test"}); err == nil {
		t.Fatal("incomplete comparison accepted")
	}
}

func TestFreezeMustPrecedeAuditAndHashesAreHex(t *testing.T) {
	d := fixture()
	d.FrozenPolicySHA256 = "p"
	if err := Validate(d); err == nil {
		t.Fatal("non-SHA256 hash accepted")
	}
	d = fixture()
	d.AuditAt = "2025-12-31T23:59:59Z"
	if err := Validate(d); err == nil {
		t.Fatal("audit preceding freeze accepted")
	}
}

func TestPartitionLeakageAndPilotCounts(t *testing.T) {
	d := fixture()
	x := d.Tasks[0]
	x.ID = "t2"
	x.Partition = "development"
	d.Tasks = append(d.Tasks, x)
	if err := Validate(d); err == nil || !strings.Contains(err.Error(), "partition leakage") {
		t.Fatalf("expected leakage rejection, got %v", err)
	}
	d = fixture()
	d.PilotOnly = true
	if err := Validate(d); err == nil || !strings.Contains(err.Error(), "pilot requires") {
		t.Fatalf("expected pilot count rejection, got %v", err)
	}
}

func TestLoadRejectsSymlinkAndDuplicateJSON(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "data.json")
	if err := os.WriteFile(target, []byte(`{"version":"aok-evaluation/v1","version":"aok-evaluation/v1"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(target); err == nil {
		t.Fatal("duplicate JSON key accepted")
	}
	link := filepath.Join(dir, "link.json")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(link); err == nil {
		t.Fatal("symlink document accepted")
	}
}
