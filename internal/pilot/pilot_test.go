package pilot

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func fixtureManifest(p Purpose) Manifest {
	h := strings.Repeat("a", 64)
	parts := map[string]string{}
	for i := 0; i < 30; i++ {
		parts[fmtID(i)] = "locked-audit"
	}
	route := Route{Host: "codex", HostVersion: "1", HostSHA256: h, Provider: "local", Platform: "macos-arm64", Transport: "native-test", ControlPolicySHA256: h, ConfigurationSHA256: h, ProfileSHA256: h, RouteManifestSHA256: h, NativeTenant: "test-tenant", API: "local-fixture", Origin: "local", VerifierSHA256: h, RubricSHA256: h, EnvironmentSHA256: h, AccountScopeSHA256: h, Account: "acct-1", Operation: "execution", AuthMode: "managed"}
	input := testHash("input-1")
	return Manifest{Version: Version, ID: "pilot-1", Purpose: p, Risk: "standard", FrozenAt: "2026-10-01T00:00:00Z", ConfigurationSHA256: h, DatasetSHA256: h, ProtocolSHA256: h, FamilyPartitions: parts, RepetitionsByFamily: fixtureRepetitions(parts), MinimumShadowOpportunities: 100, Routes: []Route{route}, AllowedRepositories: []string{"repo-1"}, AllowedAccounts: []string{"acct-1"}, Operators: []string{"operator"}, MaximumAttempts: 1, MaximumSeconds: 60, MaximumConsumption: json.Number("10"), ConsumptionUnit: "tokens", RetentionDays: 30, Trials: []TrialSpec{{ID: "trial-1", Repository: "repo-1", Account: "acct-1", InputSHA256: input, RouteSHA256: digest(route), MaximumAttempts: 1, MaximumSeconds: 60, MaximumConsumption: json.Number("10"), ConsumptionUnit: "tokens"}}, Metrics: fixtureMetrics()}
}
func fmtID(i int) string { return "family-" + strconv.Itoa(i) }

func TestAssessKeepsShadowAbstentionsAndBlocksMissingRouteQualification(t *testing.T) {
	m := fixtureManifest(Production)
	b := Bundle{Version: Version, ManifestSHA256: digest(m)}
	for i := 0; i < 100; i++ {
		b.Shadow = append(b.Shadow, ShadowOpportunity{ID: fmtID(i), InputSHA256: testHash(fmt.Sprintf("input-%03d", i)), ObservedAt: "2026-10-20T00:00:00Z", Eligible: true, Abstained: i == 99})
	}
	for i := 0; i < 30; i++ {
		v := json.Number("1")
		for _, rep := range []string{"r1", "r2", "r3"} {
			b.Observations = append(b.Observations, Observation{ID: fmtID(i) + rep, RouteSHA256: digest(m.Routes[0]), FamilyID: fmtID(i), RepetitionID: rep, ReceiptSHA256: testHash(fmtID(i) + rep), VerifierSHA256: strings.Repeat("c", 64), ObservedAt: "2026-10-20T00:00:00Z", Metric: "quality", Value: &v, Unit: "proportion", Accepted: true})
		}
	}
	p := Policy{Version: "aok-pilot-policy/v1", ManifestSHA256: digest(m), ExpiresAt: "2027-01-01T00:00:00Z"}
	a, e := Assess(m, p, b, time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC))
	if e != nil {
		t.Fatal(e)
	}
	if a.ProductionEligible || a.AuthorizationGranted || a.ExecutionReady || a.ShadowEligible != 100 || a.ShadowAbstentions != 1 || a.Status != "insufficient-evidence" {
		t.Fatalf("unexpected assessment: %+v", a)
	}
	for _, mr := range a.Metrics {
		if mr.Name == "quota-avoidance" && (!mr.Supported || mr.Pass || mr.N != 0) {
			t.Fatal("account-window metric passed without typed observations")
		}
	}
}

func TestCensoredFamiliesBlockSurvivorOnlyCostAndTime(t *testing.T) {
	m := fixtureManifest(Production)
	m.FamilyPartitions["family-censored"] = "locked-audit"
	m.RepetitionsByFamily["family-censored"] = []string{"r1", "r2", "r3"}
	b := Bundle{Version: Version, ManifestSHA256: digest(m)}
	for i := 0; i < 31; i++ {
		family := fmtID(i)
		if i == 30 {
			family = "family-censored"
		}
		for j, rep := range []string{"r1", "r2", "r3"} {
			b.Episodes = append(b.Episodes, EpisodeObservation{ID: family + rep, RouteSHA256: digest(m.Routes[0]), FamilyID: family, RepetitionID: rep, ObservedAt: "2026-10-20T00:00:00Z", Outcome: "accepted", Accepted: true, Censored: i == 30 && j == 2, Cost: json.Number("10"), SetupCost: json.Number("1"), ReviewCost: json.Number("1"), RecoveryCost: json.Number("1"), Currency: "USD", ElapsedSeconds: json.Number("20"), SetupSeconds: json.Number("2"), ReviewSeconds: json.Number("2"), RecoverySeconds: json.Number("2"), ReceiptSHA256: testHash(family + rep), VerifierSHA256: strings.Repeat("c", 64)})
		}
	}
	cost, pass, err := assessEpisodeMetric(metricByName(m, "accepted-result-cost"), m, b, time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if pass || cost.Pass || cost.N != 30 || cost.Censored != 1 || cost.Missing != 1 {
		t.Fatalf("censored expensive family was omitted in a survivor-only pass: %+v", cost)
	}
	timeResult, timePass, err := assessEpisodeMetric(metricByName(m, "accepted-result-time"), m, b, time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if timePass || timeResult.Pass || timeResult.Censored != 1 || timeResult.Missing != 1 {
		t.Fatalf("censored time episode did not block claim: %+v", timeResult)
	}
}

func TestAcceptedResultCostAndTimeUseCampaignWideRatioIncludingZeroSuccessFamilies(t *testing.T) {
	m := fixtureManifest(Production)
	b := Bundle{Version: Version, ManifestSHA256: digest(m)}
	acceptedTotal := 0
	for familyIndex := 0; familyIndex < 30; familyIndex++ {
		family := fmtID(familyIndex)
		acceptedInFamily := 3
		if familyIndex == 0 {
			acceptedInFamily = 0
		} else if familyIndex == 1 {
			acceptedInFamily = 1
		}
		acceptedTotal += acceptedInFamily
		for repIndex, rep := range []string{"r1", "r2", "r3"} {
			accepted := repIndex < acceptedInFamily
			outcome := "failed"
			if accepted {
				outcome = "accepted"
			}
			id := family + rep
			b.Episodes = append(b.Episodes, EpisodeObservation{ID: id, RouteSHA256: digest(m.Routes[0]), FamilyID: family, RepetitionID: rep, ObservedAt: "2026-10-20T00:00:00Z", Outcome: outcome, Accepted: accepted, Cost: json.Number("10"), SetupCost: json.Number("1"), ReviewCost: json.Number("1"), RecoveryCost: json.Number("1"), Currency: "USD", ElapsedSeconds: json.Number("20"), SetupSeconds: json.Number("2"), ReviewSeconds: json.Number("2"), RecoverySeconds: json.Number("2"), ReceiptSHA256: testHash(id + "receipt"), VerifierSHA256: testHash(id + "verifier")})
		}
	}
	cost, _, err := assessEpisodeMetric(metricByName(m, "accepted-result-cost"), m, b, time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	wantCost := 900.0 / float64(acceptedTotal)
	gotCost, _ := parseFinite(cost.Estimate)
	if cost.N != 30 || cost.Missing != 0 || math.Abs(gotCost-wantCost) > 1e-12 {
		t.Fatalf("cost was not campaign-wide total/accepted ratio (including zero-success family): %+v want=%f", cost, wantCost)
	}
	resultTime, _, err := assessEpisodeMetric(metricByName(m, "accepted-result-time"), m, b, time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	gotTime, _ := parseFinite(resultTime.Estimate)
	wantTime := 1800.0 / float64(acceptedTotal)
	if resultTime.N != 30 || resultTime.Missing != 0 || math.Abs(gotTime-wantTime) > 1e-12 {
		t.Fatalf("time was not campaign-wide total/accepted ratio: %+v want=%f", resultTime, wantTime)
	}
}

func TestTypedAccountWindowsUsePairedIndependentBlocks(t *testing.T) {
	m := fixtureManifest(Production)
	rule := metricByName(m, "quota-avoidance")
	rule.Target = json.Number("0.05")
	b := Bundle{}
	for i := 0; i < 30; i++ {
		block := fmtID(i)
		seq := "baseline-first"
		if i%2 == 1 {
			seq = "treatment-first"
		}
		for _, arm := range []string{"baseline", "treatment"} {
			denials := 2
			if arm == "treatment" {
				denials = 1
			}
			b.AccountWindows = append(b.AccountWindows, AccountWindowObservation{ID: block + arm, RouteSHA256: digest(m.Routes[0]), BlockID: block, AccountScopeSHA256: m.Routes[0].AccountScopeSHA256, Arm: arm, Sequence: seq, ObservedAt: "2026-10-20T00:00:00Z", EligibleBeforeCapacity: 10, CapacityDeferrals: 1, ProviderAttempts: 10, QuotaDenials: denials, AdmittedStarts: 10, ReceiptSHA256: testHash(block + arm + "receipt"), VerifierSHA256: testHash(block + arm + "verifier")})
		}
	}
	result, pass, err := assessWindowMetric(rule, m, b, time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	estimate, _ := parseFinite(result.Estimate)
	if !pass || !result.Pass || result.N != 30 || math.Abs(estimate-0.1) > 1e-12 {
		t.Fatalf("paired quota-avoidance estimate unexpected: %+v", result)
	}
	b.AccountWindows[0].Censored = true
	censoredResult, censoredPass, err := assessWindowMetric(rule, m, b, time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if censoredPass || censoredResult.Pass || censoredResult.Censored != 1 {
		t.Fatalf("censored account-window was omitted from claim: %+v", censoredResult)
	}
	b.AccountWindows[0].Censored = false
	b.AccountWindows[0].AccountScopeSHA256 = testHash("other-account")
	if _, _, err = assessWindowMetric(rule, m, b, time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)); err == nil {
		t.Fatal("account-window from another route/account pool accepted")
	}
}

func TestPilotV1IsSingleRouteAndRejectsCrossRouteRows(t *testing.T) {
	m := fixtureManifest(Production)
	route := m.Routes[0]
	route.Host = "other-host"
	route.HostSHA256 = testHash("other-host")
	m.Routes = append(m.Routes, route)
	if err := ValidateManifest(m); err == nil {
		t.Fatal("multi-route campaign accepted in aok-pilot/v1")
	}

	m = fixtureManifest(Production)
	b := Bundle{Version: Version, ManifestSHA256: digest(m)}
	v := json.Number("1")
	for _, rep := range []string{"r1", "r2", "r3"} {
		b.Observations = append(b.Observations, Observation{ID: "row-" + rep, RouteSHA256: testHash("other-route"), FamilyID: "family-0", RepetitionID: rep, ObservedAt: "2026-10-20T00:00:00Z", Metric: "quality", Value: &v, Unit: "proportion", Accepted: true, ReceiptSHA256: testHash("receipt-" + rep), VerifierSHA256: testHash("verifier-" + rep)})
	}
	if _, _, err := assessMetrics(m, b, time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)); err == nil {
		t.Fatal("observation from another route accepted")
	}
	b.Observations = nil
	for _, rep := range []string{"r1", "r2", "r3"} {
		b.Episodes = append(b.Episodes, EpisodeObservation{ID: "episode-" + rep, RouteSHA256: testHash("other-route"), FamilyID: "family-0", RepetitionID: rep, ObservedAt: "2026-10-20T00:00:00Z", Outcome: "accepted", Accepted: true, Cost: json.Number("10"), SetupCost: json.Number("1"), ReviewCost: json.Number("1"), RecoveryCost: json.Number("1"), Currency: "USD", ElapsedSeconds: json.Number("20"), SetupSeconds: json.Number("2"), ReviewSeconds: json.Number("2"), RecoverySeconds: json.Number("2"), ReceiptSHA256: testHash("episode-receipt-" + rep), VerifierSHA256: testHash("episode-verifier-" + rep)})
	}
	if _, _, err := assessEpisodeMetric(metricByName(m, "accepted-result-cost"), m, b, time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)); err == nil {
		t.Fatal("episode from another route accepted")
	}
}

func TestFixedMetricSemanticsRejectUnsafeDirectionAndUnknownMetric(t *testing.T) {
	m := fixtureManifest(Production)
	for i := range m.Metrics {
		if m.Metrics[i].Name == "provider-quota-denial-rate" {
			m.Metrics[i].Direction = "minimum"
		}
	}
	if err := ValidateManifest(m); err == nil {
		t.Fatal("quota denial rule that rewards higher denial rate was accepted")
	}
	m = fixtureManifest(Production)
	m.Metrics = append(m.Metrics, MetricRule{Name: "invented-metric", Unit: "count", Direction: "maximum", Target: json.Number("1"), MinimumN: 2, AnalysisUnit: "independent-family", Denominator: "families", ConfidenceMethod: "normal-approx-95", ConfidenceLevel: json.Number("0.95"), Baseline: "pinned", MissingRule: "exclude-and-report", CensoringRule: "retain-and-report-no-imputation", StopThreshold: json.Number("1")})
	if err := ValidateManifest(m); err == nil {
		t.Fatal("unknown metric with no estimator was accepted")
	}
}

func TestAllQuotaDenialsCannotPassAndSafetyUsesDeterministicCounters(t *testing.T) {
	m := fixtureManifest(Production)
	rule := metricByName(m, "provider-quota-denial-rate")
	rule.Target = json.Number("0.1")
	b := Bundle{}
	for i := 0; i < 30; i++ {
		block := fmtID(i)
		seq := "baseline-first"
		if i%2 == 1 {
			seq = "treatment-first"
		}
		for _, arm := range []string{"baseline", "treatment"} {
			b.AccountWindows = append(b.AccountWindows, AccountWindowObservation{ID: block + arm, RouteSHA256: digest(m.Routes[0]), BlockID: block, AccountScopeSHA256: m.Routes[0].AccountScopeSHA256, Arm: arm, Sequence: seq, ObservedAt: "2026-10-20T00:00:00Z", ProviderAttempts: 10, QuotaDenials: 10, ReceiptSHA256: testHash(block + arm + "receipt"), VerifierSHA256: testHash(block + arm + "verifier")})
		}
	}
	denials, pass, err := assessWindowMetric(rule, m, b, time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if pass || denials.Pass || denials.Estimate != "1" {
		t.Fatalf("100%% provider quota denial passed: %+v", denials)
	}
	zero := safetyCounterMetric(metricByName(m, "operational-safety"), Bundle{})
	if !zero.Pass || zero.N != 6 || zero.Estimate != "0" {
		t.Fatalf("zero hard counters not summarized deterministically: %+v", zero)
	}
	hard := safetyCounterMetric(metricByName(m, "operational-safety"), Bundle{HardGateBypasses: 1})
	if hard.Pass || hard.Estimate != "1" {
		t.Fatalf("hard gate bypass did not fail deterministic safety metric: %+v", hard)
	}
	bundle := Bundle{Version: Version, ManifestSHA256: digest(m), Observations: []Observation{{Metric: "operational-safety"}}}
	if _, _, err := assessMetrics(m, bundle, time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)); err == nil {
		t.Fatal("fabricated normal observation accepted as operational safety evidence")
	}
	bundle.Observations = nil
	bundle.HardGateBypasses = -1
	policy := Policy{Version: "aok-pilot-policy/v1", ManifestSHA256: digest(m), ExpiresAt: "2027-01-01T00:00:00Z"}
	if _, err := Assess(m, policy, bundle, time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)); err == nil {
		t.Fatal("negative hard-safety counter accepted")
	}
}

func TestStopRulesTriggerOnBoundsEvenWhenMetricTargetPasses(t *testing.T) {
	m := fixtureManifest(Production)
	results := make([]MetricResult, 0, len(m.Metrics))
	for _, rule := range m.Metrics {
		estimate, lower, upper := json.Number("0"), json.Number("0"), json.Number("0")
		if rule.Direction == "minimum" {
			estimate, lower, upper = json.Number("1"), json.Number("0.95"), json.Number("1")
		}
		if rule.Direction == "maximum" {
			estimate, lower, upper = json.Number("0"), json.Number("0"), json.Number("0")
		}
		if rule.Name == "provider-quota-denial-rate" {
			// The preregistered acceptance target is 0.1, so this result passes it;
			// the stricter stop threshold is 0.05 and must still halt the campaign.
			estimate, lower, upper = json.Number("0.04"), json.Number("0.02"), json.Number("0.08")
		}
		results = append(results, MetricResult{Name: rule.Name, N: rule.MinimumN, Estimate: estimate, Lower: lower, Upper: upper, Pass: true, Supported: true})
	}
	triggered, reasons := EvaluateStopRules(m.Metrics, results)
	if !triggered || len(reasons) != 1 || !strings.Contains(reasons[0], "provider-quota-denial-rate") {
		t.Fatalf("stricter stop bound did not trigger despite passing final target: %v %v", triggered, reasons)
	}
	for i := range results {
		if results[i].Name == "provider-quota-denial-rate" {
			results[i].Upper = json.Number("0.05")
		}
	}
	triggered, _ = EvaluateStopRules(m.Metrics, results)
	if triggered {
		t.Fatal("equality with strict stop threshold triggered")
	}
}

func TestAssessmentRecordsStopReasonAndBlocksCampaign(t *testing.T) {
	m := fixtureManifest(Production)
	b := Bundle{Version: Version, ManifestSHA256: digest(m)}
	for i := 0; i < 100; i++ {
		b.Shadow = append(b.Shadow, ShadowOpportunity{ID: fmtID(i), InputSHA256: testHash("stop-shadow-" + fmt.Sprint(i)), ObservedAt: "2026-10-20T00:00:00Z", Eligible: true})
	}
	for i := 0; i < 30; i++ {
		for _, arm := range []string{"baseline", "treatment"} {
			seq := "baseline-first"
			if i%2 == 1 {
				seq = "treatment-first"
			}
			denials := 8
			if arm == "baseline" {
				denials = 0
			}
			b.AccountWindows = append(b.AccountWindows, AccountWindowObservation{ID: fmtID(i) + arm, RouteSHA256: digest(m.Routes[0]), BlockID: fmtID(i), AccountScopeSHA256: m.Routes[0].AccountScopeSHA256, Arm: arm, Sequence: seq, ObservedAt: "2026-10-20T00:00:00Z", ProviderAttempts: 100, QuotaDenials: denials, ReceiptSHA256: testHash(fmtID(i) + arm + "receipt"), VerifierSHA256: testHash(fmtID(i) + arm + "verifier")})
		}
	}
	p := Policy{Version: "aok-pilot-policy/v1", ManifestSHA256: digest(m), ExpiresAt: "2027-01-01T00:00:00Z"}
	a, err := Assess(m, p, b, time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if !a.StopTriggered || a.Status != "blocked" || len(a.StopReasons) == 0 {
		t.Fatalf("stop crossing was not recorded as blocked campaign: %+v", a)
	}
}

func metricByName(m Manifest, name string) MetricRule {
	for _, r := range m.Metrics {
		if r.Name == name {
			return r
		}
	}
	return MetricRule{}
}

func TestCalibrationGrantNeedsSignatureScopeAndVerifiedGuards(t *testing.T) {
	pub, priv, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	m := fixtureManifest(Calibration)
	inputHash := testHash("input-1")
	routeHash := m.Trials[0].RouteSHA256
	scope := trialScope{TrialID: "trial-1", Repository: "repo-1", Account: "acct-1", InputSHA256: inputHash, RouteSHA256: routeHash, ConfigurationSHA256: m.ConfigurationSHA256, ProtocolSHA256: m.ProtocolSHA256, Attempt: 1, MaxSeconds: 30, Consumption: "8", ConsumptionUnit: "tokens"}
	sh := digest(scope)
	g := SignCalibrationGrant(CalibrationGrant{Version: GrantVersion, ID: "grant-1", ManifestSHA256: digest(m), RepositoryAllowlist: []string{"repo-1"}, AccountAllowlist: []string{"acct-1"}, InputSHA256Allowlist: []string{inputHash}, RouteSHA256Allowlist: []string{routeHash}, ConfigurationSHA256: m.ConfigurationSHA256, ProtocolSHA256: m.ProtocolSHA256, TrialIDs: []string{"trial-1"}, MaxAttempts: 1, MaxSeconds: 60, MaxConsumption: json.Number("10"), ConsumptionUnit: "tokens", ExpiresAt: "2027-01-01T00:00:00Z"}, priv)
	r := CalibrationRequest{TrialID: "trial-1", Repository: "repo-1", Account: "acct-1", InputSHA256: inputHash, RouteSHA256: routeHash, ConfigurationSHA256: m.ConfigurationSHA256, ProtocolSHA256: m.ProtocolSHA256, Attempt: 1, MaxSeconds: 30, Consumption: json.Number("8"), ConsumptionUnit: "tokens"}
	for _, gate := range []string{"controls", "capability", "capacity", "verification"} {
		r.Guards = append(r.Guards, GuardReceipt{Gate: gate, EvidenceSHA256: strings.Repeat("b", 64), ScopeSHA256: sh})
	}
	if CalibrationScope(r) != sh {
		t.Fatal("exported calibration scope differs from signed guard scope")
	}
	verifiedScope, e := VerifyCalibration(m, g, r, pub, acceptGuards{}, time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC))
	if e != nil || verifiedScope != sh {
		t.Fatalf("read-only calibration verification failed: scope=%q err=%v", verifiedScope, e)
	}
	cg := consumeGuards{seen: map[string]bool{}}
	if e = AdmitCalibration(m, g, r, pub, acceptGuards{}, cg); e != nil {
		t.Fatal(e)
	}
	if e = AdmitCalibration(m, g, r, pub, acceptGuards{}, cg); e == nil {
		t.Fatal("replayed trial consumed twice")
	}
	tooLarge := g
	tooLarge.ID = "grant-too-large"
	tooLarge.MaxConsumption = json.Number("10.0000000000000000000000000001")
	tooLarge = SignCalibrationGrant(tooLarge, priv)
	if e = AdmitCalibration(m, tooLarge, r, pub, acceptGuards{}, consumeGuards{seen: map[string]bool{}}); e == nil {
		t.Fatal("grant cap above manifest cap admitted")
	}
	r.Account = "other"
	if e = AdmitCalibration(m, g, r, pub, acceptGuards{}, consumeGuards{}); e == nil {
		t.Fatal("out-of-scope account admitted")
	}
	g.Signature = "invalid"
	if e = AdmitCalibration(m, g, r, pub, acceptGuards{}, consumeGuards{}); e == nil {
		t.Fatal("bad signature admitted")
	}
}

type acceptGuards struct{}

func (acceptGuards) Verify(r GuardReceipt, scope string, asOf time.Time) error {
	if !validSHA256(r.EvidenceSHA256) || r.ScopeSHA256 != scope || asOf.IsZero() {
		return errors.New("invalid test receipt")
	}
	return nil
}

func TestReplayIsDeterministicAndNeverDispatches(t *testing.T) {
	s := ReplayScenario{Version: Version, Opportunities: []ShadowOpportunity{{ID: "1", Eligible: true, Abstained: true}, {ID: "2"}}, CapacityBlocks: 2, QuotaDenials: 1, AdmittedStarts: 3, Accepted: 2, Failed: 1}
	a, e := Replay(s)
	if e != nil {
		t.Fatal(e)
	}
	b, e := Replay(s)
	if e != nil {
		t.Fatal(e)
	}
	if a != b {
		t.Fatalf("replay changed: %+v %+v", a, b)
	}
	s.Opportunities[0].Dispatched = true
	if _, e = Replay(s); e == nil {
		t.Fatal("dispatch accepted")
	}
}

func TestQualityRequiresEveryFrozenRepetitionForFamilySuccess(t *testing.T) {
	m := fixtureManifest(Production)
	b := Bundle{Version: Version, ManifestSHA256: digest(m)}
	for i := 0; i < 30; i++ {
		for _, rep := range []string{"r1", "r2"} {
			v := json.Number("1")
			b.Observations = append(b.Observations, Observation{ID: fmt.Sprintf("%s-%s", fmtID(i), rep), RouteSHA256: digest(m.Routes[0]), FamilyID: fmtID(i), RepetitionID: rep, ObservedAt: "2026-10-20T00:00:00Z", Metric: "quality", Value: &v, Unit: "proportion", Accepted: true, ReceiptSHA256: testHash(fmt.Sprintf("receipt-%d-%s", i, rep)), VerifierSHA256: strings.Repeat("c", 64)})
		}
	}
	result, pass, e := assessMetrics(m, b, time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC))
	if e != nil {
		t.Fatal(e)
	}
	if pass || result[0].N != 0 || result[0].Missing != 30 {
		t.Fatalf("incomplete families counted as successes: %+v", result[0])
	}
}

func TestWilsonSuccessesUseOnlyCompleteFamilyDenominator(t *testing.T) {
	m := fixtureManifest(Production)
	m.FamilyPartitions["family-extra"] = "locked-audit"
	m.RepetitionsByFamily["family-extra"] = []string{"r1", "r2", "r3"}
	b := Bundle{Version: Version, ManifestSHA256: digest(m)}
	for i := 0; i < 30; i++ {
		v := json.Number("1")
		for _, rep := range []string{"r1", "r2", "r3"} {
			b.Observations = append(b.Observations, Observation{ID: fmt.Sprintf("%d-%s", i, rep), RouteSHA256: digest(m.Routes[0]), FamilyID: fmtID(i), RepetitionID: rep, ObservedAt: "2026-10-20T00:00:00Z", Metric: "quality", Value: &v, Unit: "proportion", Accepted: true, ReceiptSHA256: testHash(fmt.Sprintf("receipt-%d-%s", i, rep)), VerifierSHA256: strings.Repeat("c", 64)})
		}
	}
	for _, rep := range []string{"r1", "r2"} {
		v := json.Number("1")
		b.Observations = append(b.Observations, Observation{ID: "extra-" + rep, RouteSHA256: digest(m.Routes[0]), FamilyID: "family-extra", RepetitionID: rep, ObservedAt: "2026-10-20T00:00:00Z", Metric: "quality", Value: &v, Unit: "proportion", Accepted: true, ReceiptSHA256: testHash("extra-" + rep), VerifierSHA256: strings.Repeat("c", 64)})
	}
	results, _, e := assessMetrics(m, b, time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC))
	if e != nil {
		t.Fatal(e)
	}
	if results[0].N != 30 || results[0].Estimate != "1" {
		t.Fatalf("incomplete family inflated success count: %+v", results[0])
	}
	b.Observations[len(b.Observations)-1].ReceiptSHA256 = b.Observations[0].ReceiptSHA256
	if _, _, e = assessMetrics(m, b, time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)); e == nil {
		t.Fatal("receipt replay across families accepted")
	}
}

func TestValidateManifestRequiresEveryDecisionMetric(t *testing.T) {
	m := fixtureManifest(Production)
	m.Metrics = m.Metrics[:len(m.Metrics)-1]
	if e := ValidateManifest(m); e == nil {
		t.Fatal("missing mandatory metric accepted")
	}
}

func TestOfflineTestdataContractsLoad(t *testing.T) {
	root := "testdata"
	m, e := ReadManifest(filepath.Join(root, "manifest.json"))
	if e != nil {
		t.Fatal(e)
	}
	p, e := ReadPolicy(filepath.Join(root, "policy.json"))
	if e != nil {
		t.Fatal(e)
	}
	b, e := ReadBundle(filepath.Join(root, "bundle.json"))
	if e != nil {
		t.Fatal(e)
	}
	s, e := ReadScenario(filepath.Join(root, "scenario.json"))
	if e != nil {
		t.Fatal(e)
	}
	if p.ManifestSHA256 != digest(m) || b.ManifestSHA256 != digest(m) || len(b.Shadow) != 100 || len(s.Opportunities) != 100 {
		t.Fatal("offline fixture bindings or opportunity counts mismatch")
	}
}

type testWitness struct{}

func (testWitness) VerifyCampaignChronology(c, f, a string, ft, at time.Time) error {
	if c == "" || f == "" || a == "" || !at.After(ft) {
		return errors.New("invalid witness order")
	}
	return nil
}
func TestCampaignCustodyRequiresDistinctSignedRolesAndWitness(t *testing.T) {
	m := fixtureManifest(Production)
	curatorPub, curatorPriv, _ := ed25519.GenerateKey(rand.Reader)
	auditPub, auditPriv, _ := ed25519.GenerateKey(rand.Reader)
	f := SignFreezeReceipt(FreezeReceipt{Version: "aok-pilot-freeze/v1", CampaignSHA256: digest(m), DatasetSHA256: m.DatasetSHA256, FrozenAt: m.FrozenAt, Issuer: "curator"}, curatorPriv)
	a := SignAuditOpenReceipt(AuditOpenReceipt{Version: "aok-pilot-audit-open/v1", CampaignSHA256: digest(m), DatasetSHA256: m.DatasetSHA256, FreezeReceiptSHA256: receiptDigest(f), OpenedAt: "2026-10-02T00:00:00Z", Issuer: "auditor"}, auditPriv)
	p := Policy{Version: "aok-pilot-policy/v1", ManifestSHA256: digest(m), ExpiresAt: "2027-01-01T00:00:00Z", FreezeReceipt: f, AuditOpenReceipt: a}
	if e := VerifyCampaignCustody(m, p, "curator", curatorPub, "auditor", auditPub, testWitness{}); e != nil {
		t.Fatal(e)
	}
	if e := VerifyCampaignCustody(m, p, "same", curatorPub, "same", auditPub, testWitness{}); e == nil {
		t.Fatal("same identity filled both roles")
	}
}

type currentPromotion struct{ revoked bool }

func (c currentPromotion) VerifyPromotionGrantCurrent(g PromotionGrant, now time.Time) error {
	if c.revoked || g.ID == "" || now.IsZero() {
		return errors.New("grant revoked or unverifiable")
	}
	return nil
}

func TestPromotionTrustBindsPrincipalTTLAndCurrentRevocation(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	m := fixtureManifest(Production)
	b := Bundle{Version: Version, ManifestSHA256: digest(m)}
	a := Assessment{Version: Version, ManifestSHA256: digest(m), BundleSHA256: digest(b), Status: "eligible-for-review", ShadowEligible: m.MinimumShadowOpportunities, ShadowRequired: m.MinimumShadowOpportunities}
	for _, rule := range m.Metrics {
		a.Metrics = append(a.Metrics, MetricResult{Name: rule.Name, N: rule.MinimumN, Estimate: json.Number("1"), Lower: json.Number("1"), Upper: json.Number("1"), Pass: true, Supported: true})
	}
	a = sealAssessment(a)
	now := time.Now().UTC()
	issued := now.Add(-time.Minute)
	expires := now.Add(5 * time.Minute)
	g := SignPromotionGrant(PromotionGrant{Version: PromotionGrantVersion, ID: "grant-1", ApprovedBy: "pilot-approver", ManifestSHA256: digest(m), AssessmentSHA256: digest(a), BundleSHA256: digest(b), RouteSHA256Allowlist: []string{digest(m.Routes[0])}, MaximumAttempts: 1, MaximumSeconds: 30, MaximumConsumption: json.Number("5"), ConsumptionUnit: m.ConsumptionUnit, IssuedAt: issued.Format(time.RFC3339), ExpiresAt: expires.Format(time.RFC3339)}, priv)
	trust := PromotionTrust{Principal: "pilot-approver", Role: "pilot-promotion-authority", PublicKey: pub, MaxTTL: 10 * time.Minute, RevocationVerifier: currentPromotion{}}
	if _, err = VerifyPromotionGrant(m, a, b, g, trust); err != nil {
		t.Fatal(err)
	}
	wrongPrincipal := trust
	wrongPrincipal.Principal = "different-principal"
	if _, err = VerifyPromotionGrant(m, a, b, g, wrongPrincipal); err == nil {
		t.Fatal("grant self-asserted approver accepted")
	}
	wrongRole := trust
	wrongRole.Role = "operator"
	if _, err = VerifyPromotionGrant(m, a, b, g, wrongRole); err == nil {
		t.Fatal("non-promotion role accepted")
	}
	tooLong := g
	tooLong.IssuedAt = now.Add(-time.Hour).Format(time.RFC3339)
	tooLong = SignPromotionGrant(tooLong, priv)
	if _, err = VerifyPromotionGrant(m, a, b, tooLong, trust); err == nil {
		t.Fatal("grant lifetime beyond trusted max TTL accepted")
	}
	revoked := trust
	revoked.RevocationVerifier = currentPromotion{revoked: true}
	if _, err = VerifyPromotionGrant(m, a, b, g, revoked); err == nil {
		t.Fatal("revoked grant accepted")
	}
}

func testHash(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }

func fixtureRepetitions(parts map[string]string) map[string][]string {
	r := map[string][]string{}
	for id := range parts {
		r[id] = []string{"r1", "r2", "r3"}
	}
	return r
}

func fixtureMetrics() []MetricRule {
	names := []string{"quality", "consequential-miss", "accepted-result-cost", "accepted-result-time", "capacity-deferral-rate", "provider-quota-denial-rate", "quota-avoidance", "observation-coverage", "prediction-coverage", "operational-safety", "recovery-latency", "stability"}
	out := make([]MetricRule, 0, len(names))
	for _, name := range names {
		method := "normal-approx-95"
		direction := "maximum"
		target := "10"
		unit := "count"
		analysisUnit := "independent-family"
		denominator := "complete-locked-audit-families"
		minimumN := 30
		switch name {
		case "accepted-result-cost":
			method = "ratio-delta-normal-approx-95"
			unit, denominator = "currency-per-accepted", "accepted-results-in-complete-locked-audit-families"
		case "accepted-result-time":
			method = "ratio-delta-normal-approx-95"
			unit, denominator = "seconds-per-accepted", "accepted-results-in-complete-locked-audit-families"
		case "observation-coverage", "prediction-coverage":
			method, direction, unit, denominator = "wilson-one-sided-95", "minimum", "proportion", "all-locked-audit-repetitions"
			target = "0.8"
		case "recovery-latency":
			unit, denominator = "seconds", "complete-recovery-drills"
		case "stability":
			unit = "proportion"
			target = "0.1"
		}
		if name == "capacity-deferral-rate" || name == "provider-quota-denial-rate" || name == "quota-avoidance" {
			analysisUnit = "account-window"
			method = "paired-normal-approx-95"
			unit = "proportion"
			if name == "capacity-deferral-rate" {
				denominator = "eligible-before-capacity"
			}
			if name == "provider-quota-denial-rate" {
				denominator = "provider-attempts"
			}
			if name == "quota-avoidance" {
				denominator = "paired-provider-quota-denial-rate-improvement"
			}
		}
		if name == "quality" {
			method = "wilson-one-sided-95"
			direction = "minimum"
			target = "0.8"
			unit = "proportion"
		}
		if name == "consequential-miss" {
			method = "wilson-one-sided-95"
			target = "0.1"
			unit = "proportion"
		}
		if name == "capacity-deferral-rate" || name == "provider-quota-denial-rate" {
			target = "0.1"
		}
		if name == "operational-safety" {
			method, denominator, minimumN, target = "deterministic-count", "six-hard-safety-counters", 1, "0"
		}
		if name == "quota-avoidance" {
			direction = "minimum"
			target = "0.05"
		}
		if name == "quota-avoidance" {
			direction, target = "minimum", "0.05"
		}
		stop := "8"
		if direction == "minimum" {
			stop = "0.9"
		}
		if unit == "proportion" && direction == "maximum" {
			stop = "0.05"
		}
		if name == "operational-safety" {
			stop = "0"
		}
		if name == "recovery-latency" {
			stop = "8"
		}
		if name == "quota-avoidance" {
			stop = "0.1"
		}
		out = append(out, MetricRule{Name: name, Unit: unit, Direction: direction, Target: json.Number(target), MinimumN: minimumN, AnalysisUnit: analysisUnit, Denominator: denominator, ConfidenceMethod: method, ConfidenceLevel: json.Number("0.95"), Baseline: "pinned route", MissingRule: "exclude-and-report", CensoringRule: "retain-and-report-no-imputation", StopThreshold: json.Number(stop)})
	}
	return out
}

type consumeGuards struct{ seen map[string]bool }

func (c consumeGuards) ConsumeCalibrationTrial(g, t string, a int, _ string) error {
	key := fmt.Sprintf("%s/%s/%d", g, t, a)
	if c.seen[key] {
		return errors.New("already consumed")
	}
	c.seen[key] = true
	return nil
}
