package runtime

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"math/big"
	"path/filepath"
	platform "runtime"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/evaluation"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/hostbridge"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/operations"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/pilot"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/pilot/experiment"
	readinessv2 "github.com/ArmanAvanesyan/agent-operating-kit/internal/readiness/v2"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/runtime/records"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/selection"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
)

// TrialScope is supplied to protected operator services, not interpreted as an
// authorization record. Budget consumption and launch consumption are separate
// irreversible boundaries; a partial failure never refunds an attempt.
type TrialScope struct {
	Purpose, EnrollmentSHA256, GrantSHA256, RouteSHA256 string
	TrialID, RunID, InputSHA256, Account                string
	Repository, BoundsSHA256, Currency                  string
	MaximumSpend                                        json.Number
	MaximumSeconds                                      int
	MaximumConsumption                                  json.Number
	ConsumptionUnit                                     string
}

// TrialServices must verify enrollment, live controls, independent verification,
// current revocation, stop rules and cumulative bounds outside task data. The
// shipped binary installs no implementation. VerifyLaunch checks actual native
// configuration/controls; it must not equate a descriptor with qualification.
type TrialServices interface {
	CheckTrial(context.Context, TrialScope, time.Time) error
	ConsumeTrialBudget(context.Context, TrialScope) error
	VerifyTrialLaunch(context.Context, TrialScope, hostbridge.Prepared, hostbridge.Policy, hostbridge.Launch) error
}

type TrialReadiness struct {
	Route           readinessv2.Route
	Policy          readinessv2.Policy
	PolicyAuthority readinessv2.PolicyAuthority
	Validators      map[string]readinessv2.Validator
	Proof           json.RawMessage
	Authority       readinessv2.Authority
}

type ExperimentDispatch struct {
	Bundle      experiment.Bundle
	Trust       experiment.TrustPolicy
	Consumption experiment.TrialGuard
	Activation  *operations.Activation
	Services    TrialServices
	Readiness   TrialReadiness
	RunID       string
}

// DispatchExperimentOnce collects operational evidence only. Unlike production
// dispatch, it needs no assessment of its own future results, but it retains all
// normal P4 selection, P3 capacity and P7/P8 boundaries.
func (c *Controller) DispatchExperimentOnce(ctx context.Context, r ExperimentDispatch) (records.Report, error) {
	fail := func(e error) (records.Report, error) { return blockedReport(NewReport(), e.Error()), nil }
	if ctx == nil || c == nil || c.Store == nil || r.Services == nil || r.Consumption == nil || r.Activation == nil {
		return fail(fmt.Errorf("protected experiment services, consumption and activation required"))
	}
	// Records are captured once; caller-held slices/pointers cannot change the
	// meaning of enrollment while protected services are checking its digest.
	if e := cloneTrialRecord(r.Bundle, &r.Bundle); e != nil {
		return fail(e)
	}
	if e := cloneTrialRecord(r.Readiness.Route, &r.Readiness.Route); e != nil {
		return fail(e)
	}
	if e := cloneTrialRecord(r.Readiness.Policy, &r.Readiness.Policy); e != nil {
		return fail(e)
	}
	r.Trust.PublicKey = append(ed25519.PublicKey(nil), r.Trust.PublicKey...)
	r.Readiness.Proof = append(json.RawMessage(nil), r.Readiness.Proof...)
	verified, err := experiment.Verify(r.Bundle, r.Trust, time.Now().UTC())
	if err != nil {
		return fail(err)
	}
	m, g := r.Bundle.Manifest, r.Bundle.Grant
	scope := TrialScope{Purpose: experiment.Purpose, EnrollmentSHA256: experiment.ManifestDigest(m), GrantSHA256: experiment.GrantDigest(g), RouteSHA256: m.Trial.RouteSHA256, TrialID: m.Trial.ID, RunID: r.RunID, InputSHA256: m.Trial.InputSHA256, Account: m.Trial.Account, MaximumSeconds: g.MaximumSeconds, MaximumConsumption: g.MaximumConsumption, ConsumptionUnit: g.ConsumptionUnit}
	scope.Repository = m.Trial.Repository
	task, err := c.trialTask(r.RunID, scope.InputSHA256)
	if err != nil {
		return fail(err)
	}
	scope.MaximumSpend, scope.Currency = task.SpendLimit, task.Currency
	check := func(ctx context.Context) error {
		if _, e := experiment.Verify(r.Bundle, r.Trust, time.Now().UTC()); e != nil {
			return e
		}
		if e := r.Services.CheckTrial(ctx, scope, time.Now().UTC()); e != nil {
			return e
		}
		return checkTrialReadiness(r.Readiness, m.Route, time.Now().UTC())
	}
	if err = check(ctx); err != nil {
		return fail(err)
	}
	child, err := c.bindTrial(ctx, scope, m.Route, m.ProtocolSHA256, r.Activation, check, r.Services)
	if err != nil {
		return fail(err)
	}
	child.trialConsume = func(ctx context.Context) error {
		if e := check(ctx); e != nil {
			return e
		}
		current, e := experiment.Verify(r.Bundle, r.Trust, time.Now().UTC())
		if e != nil {
			return e
		}
		// Verify again at the effect boundary, rather than retaining an offline report.
		if current.ScopeSHA256 != verified.ScopeSHA256 {
			return fmt.Errorf("experiment scope changed")
		}
		if e = r.Services.ConsumeTrialBudget(ctx, scope); e != nil {
			return e
		}
		_, e = experiment.Consume(current, r.Consumption)
		return e
	}
	ctx, err = WithV2LaunchAdmission(ctx, func(ctx context.Context, run records.Run, s records.StepSpec, step records.Step, p hostbridge.Prepared, h hostbridge.Policy, l hostbridge.Launch) error {
		if e := check(ctx); e != nil {
			return e
		}
		q := r.Readiness.Route
		if run.ManifestSHA256 != scope.InputSHA256 || p.Digest != q.PreparedSHA256 || p.PolicySHA256 != q.PolicySHA256 || hostbridge.Digest(p.Input) != q.InputSHA256 || hostbridge.Digest(l) != q.LaunchSHA256 || p.HostDriverSHA256 != q.DriverSHA256 || p.HostProtocol != q.Protocol || l.Executable != h.Executable || l.Dir != p.Workspace {
			return fmt.Errorf("experiment actual launch differs from qualified route")
		}
		return r.Services.VerifyTrialLaunch(ctx, scope, p, h, l)
	})
	if err != nil {
		return fail(err)
	}
	return child.Dispatch(ctx, r.RunID)
}

func checkTrialReadiness(r TrialReadiness, expected pilot.Route, now time.Time) error {
	l := r.Route.Legacy
	if r.Route.Completion == nil || l.Host != expected.Host || l.HostVersion != expected.HostVersion || l.HostSHA256 != expected.HostSHA256 || l.ConfigurationSHA256 != expected.ConfigurationSHA256 || l.ProfileSHA256 != expected.ProfileSHA256 || l.Account != expected.Account || l.AuthMode != expected.AuthMode || l.ControlPolicySHA256 != expected.ControlPolicySHA256 || l.Provider != expected.Provider || l.Operation != expected.Operation || l.Origin != expected.Origin || l.NativeTenant != expected.NativeTenant || l.API != expected.API || l.VerifierSHA256 != expected.VerifierSHA256 || l.RubricSHA256 != expected.RubricSHA256 || l.EnvironmentSHA256 != expected.EnvironmentSHA256 || l.OS+"/"+l.Arch != expected.Platform || usage.Digest(r.Route) != expected.RouteManifestSHA256 {
		return fmt.Errorf("experiment readiness differs from enrolled route/milestone")
	}
	report, err := readinessv2.Assess(r.Route, r.Policy, now, r.PolicyAuthority, r.Validators)
	if err != nil {
		return err
	}
	a, err := readinessv2.Admit(r.Route, report.Report, r.Proof, r.Authority)
	if err != nil {
		return err
	}
	if a.Status != "eligible" {
		return fmt.Errorf("experiment readiness: %s", a.Reason)
	}
	return nil
}

type CalibrationDispatch struct {
	Manifest    pilot.Manifest
	Grant       pilot.CalibrationGrant
	Request     pilot.CalibrationRequest
	TrustedKey  ed25519.PublicKey
	Guards      pilot.GuardVerifier
	Consumption pilot.ConsumptionGuard
	Bounds      selection.ExplicitUpperBound
	Activation  *operations.Activation
	Services    TrialServices
	RunID       string
}

func CalibrationActivationScope(r CalibrationDispatch) string {
	return usage.Digest(struct{ Purpose, Manifest, Grant, Request, Bounds string }{"calibration-execution/v1", usage.Digest(r.Manifest), usage.Digest(r.Grant), pilot.CalibrationScope(r.Request), usage.Digest(r.Bounds)})
}

// DispatchCalibrationOnce is the only entrance to the private non-quality
// admission path. No flag, manifest purpose, or ordinary Dispatch parameter can
// enable it. Actual independent P4 outcomes remain required for ordinary runs.
func (c *Controller) DispatchCalibrationOnce(ctx context.Context, r CalibrationDispatch) (records.Report, error) {
	fail := func(e error) (records.Report, error) { return blockedReport(NewReport(), e.Error()), nil }
	if ctx == nil || c == nil || c.Store == nil || r.Services == nil || r.Activation == nil || r.Consumption == nil {
		return fail(fmt.Errorf("protected calibration services, consumption and activation required"))
	}
	for _, pair := range []struct{ src, dst any }{{r.Manifest, &r.Manifest}, {r.Grant, &r.Grant}, {r.Request, &r.Request}, {r.Bounds, &r.Bounds}} {
		if e := cloneTrialRecord(pair.src, pair.dst); e != nil {
			return fail(e)
		}
	}
	r.TrustedKey = append(ed25519.PublicKey(nil), r.TrustedKey...)
	if r.Manifest.Purpose != pilot.Calibration || len(r.Manifest.Routes) != 1 || r.Manifest.Risk != "standard" || r.Manifest.MaximumAttempts != 1 || r.Grant.MaxAttempts != 1 || r.Request.Attempt != 1 || r.Request.MaxSeconds > 60 || r.Request.MaxSeconds < 1 {
		return fail(fmt.Errorf("initial calibration requires one standard-risk bounded attempt"))
	}
	route := r.Manifest.Routes[0]
	if usage.Digest(route) != r.Request.RouteSHA256 || r.Bounds.Cohort.ConfigurationID != r.Manifest.ConfigurationSHA256 || r.Bounds.Cohort.ProfileSHA256 != route.ProfileSHA256 || r.Bounds.Cohort.AccountScope != r.Request.Account || r.Bounds.Cohort.ProtocolSHA256 != r.Manifest.ProtocolSHA256 {
		return fail(fmt.Errorf("calibration bounds differ from frozen route/cohort"))
	}
	scope := TrialScope{Purpose: string(pilot.Calibration), EnrollmentSHA256: usage.Digest(r.Manifest), GrantSHA256: usage.Digest(r.Grant), RouteSHA256: r.Request.RouteSHA256, TrialID: r.Request.TrialID, RunID: r.RunID, InputSHA256: r.Request.InputSHA256, Account: r.Request.Account, MaximumSeconds: r.Request.MaxSeconds, MaximumConsumption: r.Request.Consumption, ConsumptionUnit: r.Request.ConsumptionUnit}
	scope.Repository, scope.BoundsSHA256 = r.Request.Repository, usage.Digest(r.Bounds)
	task, e := c.trialTask(r.RunID, scope.InputSHA256)
	if e != nil {
		return fail(e)
	}
	scope.MaximumSpend, scope.Currency = task.SpendLimit, task.Currency
	if r.Bounds.Cohort.TaskClass != task.TaskClass || r.Bounds.Cohort.Language != task.Language || r.Bounds.Cohort.Risk != task.Risk {
		return fail(fmt.Errorf("calibration bounds differ from captured task class/language/risk"))
	}
	state, e := c.Store.Snapshot(ctx)
	if e != nil {
		return fail(e)
	}
	captured, e := findRun(state, r.RunID)
	if e != nil {
		return fail(e)
	}
	if findAssetSHA(captured.Manifest.Assets, r.Bounds.Source.Path) != r.Bounds.Source.SHA256 {
		return fail(fmt.Errorf("calibration bound source is not part of captured signed input"))
	}
	if _, e = readAsset(filepath.Join(c.runRoot(r.RunID), "inputs"), records.Asset{Path: r.Bounds.Source.Path, SHA256: r.Bounds.Source.SHA256}); e != nil {
		return fail(fmt.Errorf("calibration bound provenance: %w", e))
	}
	check := func(ctx context.Context) error {
		if _, e := pilot.VerifyCalibration(r.Manifest, r.Grant, r.Request, r.TrustedKey, r.Guards, time.Now().UTC()); e != nil {
			return e
		}
		return r.Services.CheckTrial(ctx, scope, time.Now().UTC())
	}
	if e := check(ctx); e != nil {
		return fail(e)
	}
	if r.Activation.ScopeSHA256() != CalibrationActivationScope(r) {
		return fail(fmt.Errorf("calibration activation must bind bounds and exact purpose"))
	}
	child, e := c.bindTrial(ctx, scope, route, r.Manifest.ProtocolSHA256, r.Activation, check, r.Services)
	if e != nil {
		return fail(e)
	}
	child.trialConsume = func(ctx context.Context) error {
		if e := check(ctx); e != nil {
			return e
		}
		if e := r.Services.ConsumeTrialBudget(ctx, scope); e != nil {
			return e
		}
		return pilot.AdmitCalibration(r.Manifest, r.Grant, r.Request, r.TrustedKey, r.Guards, r.Consumption)
	}
	ctx, e = WithV2LaunchAdmission(ctx, func(ctx context.Context, run records.Run, s records.StepSpec, step records.Step, p hostbridge.Prepared, h hostbridge.Policy, l hostbridge.Launch) error {
		if e := check(ctx); e != nil {
			return e
		}
		return r.Services.VerifyTrialLaunch(ctx, scope, p, h, l)
	})
	if e != nil {
		return fail(e)
	}
	admission := &calibrationAdmission{bounds: r.Bounds, scope: scope, task: task, checkCurrent: check}
	return child.dispatch(ctx, r.RunID, admission)
}

// bindTrial uses a request-local controller view: experiment configuration does
// not remain installed on the ordinary controller after success or failure.
func (c *Controller) bindTrial(ctx context.Context, s TrialScope, r pilot.Route, protocol string, a *operations.Activation, check func(context.Context) error, services TrialServices) (*Controller, error) {
	if c.Policy.MaxConcurrent != 1 || c.Policy.MaxAttempts != 1 || s.RunID == "" || s.MaximumSeconds < 1 || s.MaximumSeconds > 60 {
		return nil, fmt.Errorf("trial requires singleton ownership, one attempt and <=60 seconds")
	}
	if _, ok := c.Runner.(hostbridge.NativeRunner); !ok {
		return nil, fmt.Errorf("trial requires qualified native runner")
	}
	if a == nil || a.ScopeSHA256() == "" {
		return nil, fmt.Errorf("external activation required")
	}
	if s.Purpose == experiment.Purpose {
		expected := usage.Digest(struct{ Purpose, Enrollment, Grant, Route string }{experiment.Purpose, s.EnrollmentSHA256, s.GrantSHA256, s.RouteSHA256})
		if a.ScopeSHA256() != expected {
			return nil, fmt.Errorf("experiment activation scope differs")
		}
	}
	state, e := c.Store.Snapshot(ctx)
	if e != nil {
		return nil, e
	}
	run, e := findRun(state, s.RunID)
	if e != nil {
		return nil, e
	}
	if len(run.Manifest.Steps) != 1 || run.ManifestSHA256 != s.InputSHA256 || run.Manifest.Repository == "" || run.Manifest.Repository != s.Repository || run.Cancelled {
		return nil, fmt.Errorf("trial must bind one captured uncancelled P7 run")
	}
	if e = VerifyCapture(c.runRoot(s.RunID), run.Manifest, run.ManifestSHA256); e != nil {
		return nil, e
	}
	spec := run.Manifest.Steps[0]
	step, e := findStep(run, spec.ID)
	if e != nil {
		return nil, e
	}
	if step.Attempts != 0 {
		return nil, fmt.Errorf("trial attempt already consumed; recovery required")
	}
	root := filepath.Join(c.runRoot(s.RunID), "inputs")
	cb, e := readAsset(root, records.Asset{Path: spec.Selection.Candidates, SHA256: findAssetSHA(run.Manifest.Assets, spec.Selection.Candidates)})
	if e != nil {
		return nil, e
	}
	var candidates selection.CandidateManifest
	if usage.Decode(cb, &candidates) != nil || len(candidates.Candidates) != 1 {
		return nil, fmt.Errorf("trial requires one exact candidate")
	}
	x := candidates.Candidates[0]
	if x.ConfigurationID != r.ConfigurationSHA256 || x.Profile.SHA256 != r.ProfileSHA256 || x.AccountScope != s.Account {
		return nil, fmt.Errorf("trial candidate/profile/account mismatch")
	}
	tb, e := readAsset(root, records.Asset{Path: spec.Selection.Task, SHA256: findAssetSHA(run.Manifest.Assets, spec.Selection.Task)})
	if e != nil {
		return nil, e
	}
	var task selection.TaskProfile
	if usage.Decode(tb, &task) != nil || task.Risk != "standard" || task.ProtocolSHA256 != protocol || task.MidRun {
		return nil, fmt.Errorf("trial requires standard-risk frozen task protocol without resume")
	}
	if spec.Host != r.Host || r.Platform != platform.GOOS+"/"+platform.GOARCH || r.Account != s.Account {
		return nil, fmt.Errorf("trial host/platform/account mismatch")
	}
	hp, e := c.route(spec.Host, time.Now().UTC(), 1, spec.ID)
	if e != nil {
		return nil, e
	}
	if hp.HostVersion != r.HostVersion || hp.ExecutableSHA256 != r.HostSHA256 || hostbridge.PolicyHash(hp) != r.ControlPolicySHA256 || hp.AccountScope != s.Account || hp.AuthMode != r.AuthMode || hp.TimeoutSeconds > s.MaximumSeconds {
		return nil, fmt.Errorf("trial native policy differs or exceeds frozen bounds")
	}
	child := *c
	if e = child.ConfigurePilotActivation(ctx, a, a.ScopeSHA256(), []PilotTrialBinding{{RunID: s.RunID, StepID: spec.ID, TrialID: s.TrialID, ManifestSHA256: run.ManifestSHA256, HostPolicySHA256: hostbridge.PolicyHash(hp), ConfigurationSHA256: r.ConfigurationSHA256, ProfileSHA256: r.ProfileSHA256, NativeRunner: true}}); e != nil {
		return nil, e
	}
	child.pilotRevalidate = check
	child.trialValidateDemand = func(d usage.Demand, est evaluation.EstimateArtifact, task selection.TaskProfile, at time.Time) error {
		return checkTrialDemand(s, d, est, task, at)
	}
	return &child, nil
}

// Spend/currency come from the captured task whose bytes are transitively bound
// by the signed trial input manifest, not from a caller-supplied allowance.
func (c *Controller) trialTask(id, input string) (selection.TaskProfile, error) {
	var task selection.TaskProfile
	state, e := c.Store.Snapshot(context.Background())
	if e != nil {
		return task, e
	}
	run, e := findRun(state, id)
	if e != nil {
		return task, e
	}
	if run.ManifestSHA256 != input || len(run.Manifest.Steps) != 1 {
		return task, fmt.Errorf("trial task not bound to signed captured input")
	}
	if e = VerifyCapture(c.runRoot(id), run.Manifest, input); e != nil {
		return task, e
	}
	path := run.Manifest.Steps[0].Selection.Task
	raw, e := readAsset(filepath.Join(c.runRoot(id), "inputs"), records.Asset{Path: path, SHA256: findAssetSHA(run.Manifest.Assets, path)})
	if e != nil {
		return task, e
	}
	if e = usage.Decode(raw, &task); e != nil {
		return task, e
	}
	if e = selection.ValidateJSON(raw, "task"); e != nil {
		return task, e
	}
	return task, nil
}

func ExperimentActivationScope(b experiment.Bundle) string {
	return usage.Digest(struct{ Purpose, Enrollment, Grant, Route string }{experiment.Purpose, experiment.ManifestDigest(b.Manifest), experiment.GrantDigest(b.Grant), b.Manifest.Trial.RouteSHA256})
}

type calibrationAdmission struct {
	bounds       selection.ExplicitUpperBound
	scope        TrialScope
	task         selection.TaskProfile
	checkCurrent func(context.Context) error
}

func (a *calibrationAdmission) check(d selection.SelectionDecision, p hostbridge.Prepared, exp usage.Export, up usage.Policy, at time.Time) (selection.CandidateResult, *usage.Demand, time.Time, error) {
	fail := func(reason string) (selection.CandidateResult, *usage.Demand, time.Time, error) {
		return selection.CandidateResult{}, nil, time.Time{}, fmt.Errorf("calibration admission: %s", reason)
	}
	if a == nil || len(d.Candidates) != 1 || d.AuthorizationGranted || d.ExecutionReady {
		return fail("exact static decision required")
	}
	x := d.Candidates[0]
	b := a.bounds
	if usage.Digest(b) != a.scope.BoundsSHA256 {
		return fail("approved bound digest changed")
	}
	if b.Cohort.TaskClass != a.task.TaskClass || b.Cohort.Language != a.task.Language || b.Cohort.Risk != a.task.Risk || b.Cohort.Risk != "standard" || b.Cohort.AccountScope != a.scope.Account || b.Cohort.ProtocolSHA256 != a.task.ProtocolSHA256 {
		return fail("exact task cohort required")
	}
	if x.ConfigurationID != b.Cohort.ConfigurationID || x.ProfileSHA256 != b.Cohort.ProfileSHA256 || p.ConfigurationID != x.ConfigurationID || p.ProfileSHA256 != x.ProfileSHA256 || x.AccountScope != a.scope.Account || d.Inputs["assignment:"+x.ConfigurationID] != p.AssignmentSHA256 {
		return fail("prepared identity mismatch")
	}
	if a.checkCurrent == nil || a.checkCurrent(context.Background()) != nil {
		return fail("protected current services unavailable")
	}
	seen := map[string]bool{}
	for _, g := range x.Gates {
		if seen[g.Name] {
			return fail("duplicate static gate")
		}
		seen[g.Name] = true
		if g.Name == "quality" || g.Name == "cost-time" || g.Name == "capacity" {
			continue
		}
		if g.Status != "pass" {
			return fail("non-quality gate " + g.Name + " " + g.Status)
		}
	}
	for _, required := range []string{"identity", "assignment-profile", "route", "account-binding", "account", "profile-compatibility", "quality", "cost-time"} {
		if !seen[required] {
			return fail("missing mandatory static gate " + required)
		}
	}
	observed, oe := time.Parse(time.RFC3339Nano, b.ObservedAt)
	until, ue := time.Parse(time.RFC3339Nano, b.ValidUntil)
	if oe != nil || ue != nil || observed.After(at) || !until.After(at) || b.CostUpper == nil || b.SecondsUpper == nil || b.Currency == "" {
		return fail("fresh explicit finite bounds required")
	}
	cost, ce := usage.NumberRat(*b.CostUpper)
	seconds, se := usage.NumberRat(*b.SecondsUpper)
	if ce != nil || se != nil || cost.Sign() < 0 || seconds.Sign() <= 0 || seconds.Cmp(big.NewRat(int64(a.scope.MaximumSeconds), 1)) > 0 {
		return fail("bounds exceed time enrollment")
	}
	cap, ce := usage.NumberRat(a.scope.MaximumConsumption)
	if ce != nil || cap.Sign() <= 0 {
		return fail("invalid consumption cap")
	}
	total := new(big.Rat)
	seenBounds := map[string]bool{}
	for _, bound := range b.Bounds {
		key := usage.Digest(struct{ Pool, Window, Metric, Unit string }{bound.PoolID, bound.WindowFamily, bound.Metric, bound.Unit})
		if bound.Upper == nil || bound.Unit != a.scope.ConsumptionUnit || seenBounds[key] {
			return fail("missing, duplicate or incompatible bound")
		}
		seenBounds[key] = true
		n, e := usage.NumberRat(*bound.Upper)
		if e != nil || n.Sign() < 0 {
			return fail("invalid bound")
		}
		total.Add(total, n)
	}
	if len(b.Bounds) == 0 || total.Cmp(cap) > 0 {
		return fail("demand exceeds enrolled consumption")
	}
	est := evaluation.EstimateArtifact{Version: evaluation.Version, Cohort: b.Cohort, CostUpper: b.CostUpper, Currency: b.Currency, SecondsUpper: b.SecondsUpper, Bounds: b.Bounds, DemandValidUntil: b.ValidUntil, Method: "unmeasured-calibration-upper-bound", DemandProvenance: []string{usage.Digest(b)}, QualityLower: json.Number("0")}
	demand := selection.MaterializeDemand(exp, up, x.ConfigurationID, x.ProfileSHA256, est, at)
	demand.Source = "aok-calibration-admission/v1:" + usage.Digest(b)
	if e := checkTrialDemand(a.scope, demand, est, a.task, at); e != nil {
		return fail(e.Error())
	}
	capacity := usage.Capacity(exp, up, x.ConfigurationID, &demand, at)
	if capacity.Status != "valid" || capacity.Disposition != "observed-fit" {
		return fail("fresh bounded capacity required")
	}
	x.Estimate = &est
	x.Capacity = &capacity
	x.Eligible = false
	x.Rank = nil
	// Preserve the quality rejection and the original advisory decision verbatim.
	return x, &demand, until, nil
}

func cloneTrialRecord(src, dst any) error {
	raw, e := json.Marshal(src)
	if e != nil {
		return e
	}
	return usage.Decode(raw, dst)
}

func checkTrialDemand(s TrialScope, d usage.Demand, est evaluation.EstimateArtifact, task selection.TaskProfile, at time.Time) error {
	cap, e := usage.NumberRat(s.MaximumConsumption)
	if e != nil || cap.Sign() <= 0 || len(d.Amounts) == 0 {
		return fmt.Errorf("enrolled positive consumption cap required")
	}
	total := new(big.Rat)
	for _, a := range d.Amounts {
		n, e := usage.NumberRat(a.Amount)
		if e != nil || n.Sign() < 0 || a.Unit != s.ConsumptionUnit {
			return fmt.Errorf("demand unit or amount outside trial enrollment")
		}
		total.Add(total, n)
	}
	if total.Cmp(cap) > 0 {
		return fmt.Errorf("demand exceeds enrolled consumption")
	}
	if est.CostUpper == nil || est.SecondsUpper == nil || est.Currency != s.Currency || task.Currency != s.Currency || task.SpendLimit != s.MaximumSpend {
		return fmt.Errorf("exact enrolled spend/currency required")
	}
	cost, e := usage.NumberRat(*est.CostUpper)
	spend, se := usage.NumberRat(s.MaximumSpend)
	seconds, te := usage.NumberRat(*est.SecondsUpper)
	deadline, de := time.Parse(time.RFC3339Nano, task.Deadline)
	if e != nil || se != nil || te != nil || de != nil || cost.Sign() < 0 || spend.Sign() < 0 || cost.Cmp(spend) > 0 || seconds.Sign() <= 0 || seconds.Cmp(big.NewRat(int64(s.MaximumSeconds), 1)) > 0 || seconds.Cmp(big.NewRat(int64(deadline.Sub(at)), int64(time.Second))) > 0 {
		return fmt.Errorf("trial exceeds enrolled spend/time/deadline")
	}
	return nil
}

func checkExecutionWindow(cap usage.CapacitySnapshot, duration time.Duration, at time.Time) error {
	if cap.Status != "valid" || cap.Disposition != "observed-fit" || duration <= 0 || duration > 60*time.Second {
		return fmt.Errorf("bounded observed-fit execution window required")
	}
	for _, b := range cap.Balances {
		if b.ResetAt != nil {
			reset, e := time.Parse(time.RFC3339Nano, *b.ResetAt)
			if e != nil || !at.Add(duration).Before(reset) {
				return fmt.Errorf("cross-window execution unsupported; refresh and rerun after observed reset")
			}
		}
	}
	return nil
}
