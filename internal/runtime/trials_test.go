package runtime

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/evaluation"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/hostbridge"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/operations"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/pilot"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/pilot/experiment"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/selection"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
)

type trialCounter struct{ checks, budgets, launches int }

func (s *trialCounter) CheckTrial(context.Context, TrialScope, time.Time) error {
	s.checks++
	return nil
}
func (s *trialCounter) ConsumeTrialBudget(context.Context, TrialScope) error { s.budgets++; return nil }
func (s *trialCounter) VerifyTrialLaunch(context.Context, TrialScope, hostbridge.Prepared, hostbridge.Policy, hostbridge.Launch) error {
	s.launches++
	return nil
}

type experimentCounter struct{ calls int }

func (g *experimentCounter) ConsumeExperimentTrial(string, string, string) error {
	g.calls++
	return nil
}

func TestTrialDispatchWithoutCompleteProtectedServicesHasNoEffects(t *testing.T) {
	c, _ := controllerFixture(t)
	services, consumption := &trialCounter{}, &experimentCounter{}
	_, err := c.DispatchExperimentOnce(context.Background(), ExperimentDispatch{Consumption: consumption, Services: services, RunID: "run-one"})
	if err != nil {
		t.Fatal(err)
	}
	if services.checks != 0 || services.budgets != 0 || services.launches != 0 || consumption.calls != 0 {
		t.Fatalf("incomplete experiment services caused effects: services=%+v consumption=%d", services, consumption.calls)
	}
	// Calibration has a distinct purpose and private admission path. A production
	// manifest cannot be used to reach it, even when service-shaped values exist.
	_, err = c.DispatchCalibrationOnce(context.Background(), CalibrationDispatch{Manifest: pilot.Manifest{Purpose: pilot.Production}, Activation: &operations.Activation{}, Services: services, Consumption: unusedCalibrationConsumer{}, RunID: "run-one"})
	if err != nil {
		t.Fatal(err)
	}
	if services.checks != 0 || services.budgets != 0 || services.launches != 0 {
		t.Fatalf("production/calibration confusion crossed service boundary: %+v", services)
	}
}

type unusedCalibrationConsumer struct{}

func (unusedCalibrationConsumer) ConsumeCalibrationTrial(string, string, int, string) error {
	panic("must not consume")
}

func TestCheckExecutionWindowStopsBeforeQuotaReset(t *testing.T) {
	at := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	reset := at.Add(30 * time.Second).Format(time.RFC3339Nano)
	cap := usage.CapacitySnapshot{Report: usage.Report{Status: "valid"}, Disposition: "observed-fit", Balances: []usage.Balance{{ResetAt: &reset}}}
	if err := checkExecutionWindow(cap, 20*time.Second, at); err != nil {
		t.Fatalf("within-window run rejected: %v", err)
	}
	if err := checkExecutionWindow(cap, 30*time.Second, at); err == nil {
		t.Fatal("run ending exactly at reset was accepted")
	}
	if err := checkExecutionWindow(cap, 31*time.Second, at); err == nil {
		t.Fatal("run crossing reset was accepted")
	}
	bad := "not-a-time"
	cap.Balances[0].ResetAt = &bad
	if err := checkExecutionWindow(cap, time.Second, at); err == nil {
		t.Fatal("malformed reset was accepted")
	}
	cap.Status = "blocked"
	if err := checkExecutionWindow(cap, time.Second, at); err == nil {
		t.Fatal("non-valid capacity was accepted")
	}
}

func calibrationAdmissionFixture(t *testing.T) (*calibrationAdmission, selection.SelectionDecision, hostbridge.Prepared, usage.Export, usage.Policy, time.Time) {
	t.Helper()
	exp, policy, err := usage.Load(filepath.Join("..", "usage", "testdata", "export.json"), filepath.Join("..", "usage", "testdata", "policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 23, 12, 0, 30, 0, time.UTC)
	profile := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	protocol := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	cost, seconds, upper := json.Number("1"), json.Number("2"), json.Number("1")
	windowFamily := usage.WindowFamily(usage.CurrentConstraints(exp, policy, "opaque-config", at).Balances[0].Constraint)
	bound := selection.ExplicitUpperBound{Cohort: evaluation.Cohort{ConfigurationID: "opaque-config", ProfileSHA256: profile, TaskClass: "small-go-change", Language: "go", Risk: "standard", AccountScope: "acct", ProtocolSHA256: protocol}, Source: selection.Asset{Path: "approved-bound.json", SHA256: "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"}, ObservedAt: at.Add(-time.Minute).Format(time.RFC3339Nano), ValidUntil: at.Add(time.Minute).Format(time.RFC3339Nano), CostUpper: &cost, Currency: "USD", SecondsUpper: &seconds, Bounds: []evaluation.Bound{{PoolID: "fixture-pool", WindowFamily: windowFamily, Metric: "requests", Unit: "requests", Upper: &upper}}}
	task := selection.TaskProfile{TaskClass: "small-go-change", Language: "go", Risk: "standard", ProtocolSHA256: protocol, Deadline: at.Add(time.Hour).Format(time.RFC3339Nano), SpendLimit: json.Number("2"), Currency: "USD"}
	scope := TrialScope{Account: "acct", Repository: "owner/repo", Currency: "USD", MaximumSpend: json.Number("2"), MaximumSeconds: 30, MaximumConsumption: json.Number("5"), ConsumptionUnit: "requests"}
	scope.BoundsSHA256 = usage.Digest(bound)
	a := &calibrationAdmission{bounds: bound, scope: scope, task: task, checkCurrent: func(context.Context) error { return nil }}
	gates := []selection.Gate{{Name: "identity", Status: "pass"}, {Name: "assignment-profile", Status: "pass"}, {Name: "route", Status: "pass"}, {Name: "account-binding", Status: "pass"}, {Name: "account", Status: "pass"}, {Name: "profile-compatibility", Status: "pass"}, {Name: "quality", Status: "reject", Reason: "no independent quality evidence"}, {Name: "cost-time", Status: "unknown"}, {Name: "capacity", Status: "unknown"}}
	d := selection.SelectionDecision{Candidates: []selection.CandidateResult{{ConfigurationID: "opaque-config", AccountScope: "acct", ProfileSHA256: profile, Gates: gates, Eligible: false}}, Inputs: map[string]string{"assignment:opaque-config": "assignment-sha"}}
	p := hostbridge.Prepared{ConfigurationID: "opaque-config", ProfileSHA256: profile, AssignmentSHA256: "assignment-sha"}
	return a, d, p, exp, policy, at
}

func TestCalibrationAdmissionPreservesQualityRejection(t *testing.T) {
	a, d, p, exp, policy, at := calibrationAdmissionFixture(t)
	got, _, validUntil, err := a.check(d, p, exp, policy, at)
	if err != nil {
		t.Fatal(err)
	}
	if got.Eligible || got.Rank != nil || got.Estimate == nil || got.Capacity == nil || got.Capacity.Disposition != "observed-fit" {
		t.Fatalf("calibration bypassed quality or failed capacity: %+v", got)
	}
	if got.Gates[6].Status != "reject" || got.Gates[6].Reason != "no independent quality evidence" {
		t.Fatalf("quality gate was rewritten: %+v", got.Gates[6])
	}
	if !validUntil.Equal(at.Add(time.Minute)) {
		t.Fatalf("unexpected admission deadline: %s", validUntil)
	}
}

func TestCalibrationAdmissionRejectsCohortCurrencyDeadlineAndBudgetDrift(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*calibrationAdmission, *selection.SelectionDecision, time.Time)
	}{
		{"wrong-task-risk", func(a *calibrationAdmission, _ *selection.SelectionDecision, _ time.Time) {
			a.bounds.Cohort.Risk = "high"
		}},
		{"missing-task-class", func(a *calibrationAdmission, _ *selection.SelectionDecision, _ time.Time) {
			a.bounds.Cohort.TaskClass = ""
		}},
		{"currency-missing", func(a *calibrationAdmission, _ *selection.SelectionDecision, _ time.Time) { a.bounds.Currency = "" }},
		{"deadline-expired", func(a *calibrationAdmission, _ *selection.SelectionDecision, at time.Time) {
			a.bounds.ValidUntil = at.Format(time.RFC3339Nano)
		}},
		{"task-deadline", func(a *calibrationAdmission, _ *selection.SelectionDecision, at time.Time) {
			a.task.Deadline = at.Add(time.Second).Format(time.RFC3339Nano)
		}},
		{"spend-over-cap", func(a *calibrationAdmission, _ *selection.SelectionDecision, _ time.Time) {
			x := json.Number("3")
			a.bounds.CostUpper = &x
		}},
		{"future-observation", func(a *calibrationAdmission, _ *selection.SelectionDecision, at time.Time) {
			a.bounds.ObservedAt = at.Add(time.Second).Format(time.RFC3339Nano)
		}},
		{"demand-over-cap", func(a *calibrationAdmission, _ *selection.SelectionDecision, _ time.Time) {
			x := json.Number("6")
			a.bounds.Bounds[0].Upper = &x
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, d, p, exp, policy, at := calibrationAdmissionFixture(t)
			tc.mutate(a, &d, at)
			if _, _, _, err := a.check(d, p, exp, policy, at); err == nil {
				t.Fatal("drifted calibration admission passed")
			}
		})
	}
}

func TestCalibrationDispatchRejectsProductionPurposeAndCohortDriftBeforeServices(t *testing.T) {
	for _, tc := range []struct{ name, drift string }{{"production-purpose", "purpose"}, {"account", "account"}, {"protocol", "protocol"}} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := controllerFixture(t)
			services := &trialCounter{}
			route := pilot.Route{ProfileSHA256: strings.Repeat("a", 64), Account: "acct"}
			protocol := strings.Repeat("b", 64)
			purpose := pilot.Calibration
			if tc.drift == "purpose" {
				purpose = pilot.Production
			}
			m := pilot.Manifest{Purpose: purpose, Risk: "standard", ConfigurationSHA256: "cfg", ProtocolSHA256: protocol, MaximumAttempts: 1, Routes: []pilot.Route{route}}
			req := pilot.CalibrationRequest{TrialID: "trial", Account: "acct", RouteSHA256: usage.Digest(route), Attempt: 1, MaxSeconds: 30, Consumption: json.Number("1"), ConsumptionUnit: "requests"}
			bounds := selection.ExplicitUpperBound{Cohort: evaluation.Cohort{ConfigurationID: "cfg", ProfileSHA256: route.ProfileSHA256, AccountScope: "acct", ProtocolSHA256: protocol}}
			if tc.drift == "account" {
				bounds.Cohort.AccountScope = "other"
			}
			if tc.drift == "protocol" {
				bounds.Cohort.ProtocolSHA256 = strings.Repeat("c", 64)
			}
			_, err := c.DispatchCalibrationOnce(context.Background(), CalibrationDispatch{Manifest: m, Grant: pilot.CalibrationGrant{MaxAttempts: 1}, Request: req, Bounds: bounds, Activation: &operations.Activation{}, Services: services, Consumption: unusedCalibrationConsumer{}, RunID: "run-one"})
			if err != nil {
				t.Fatal(err)
			}
			if services.checks != 0 || services.budgets != 0 || services.launches != 0 {
				t.Fatalf("invalid calibration contract reached protected services: %+v", services)
			}
		})
	}
}

func TestInvalidExperimentRecordCannotReachTrialServices(t *testing.T) {
	c, _ := controllerFixture(t)
	services, consumption := &trialCounter{}, &experimentCounter{}
	_, err := c.DispatchExperimentOnce(context.Background(), ExperimentDispatch{Bundle: experiment.Bundle{Version: experiment.BundleVersion}, Trust: experiment.TrustPolicy{}, Consumption: consumption, Activation: &operations.Activation{}, Services: services, RunID: "run-one"})
	if err != nil {
		t.Fatal(err)
	}
	if services.checks != 0 || services.budgets != 0 || services.launches != 0 || consumption.calls != 0 {
		t.Fatalf("invalid experiment reached service, launch, or consumption boundary: services=%+v consumption=%d", services, consumption.calls)
	}
}
