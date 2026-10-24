package runtime

import (
	"context"
	"path/filepath"
	goruntime "runtime"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/hostbridge"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/operations"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/pilot"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/runtime/records"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/selection"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
)

// PilotBudgetGuard is installed by the protected coordinator and atomically
// consumes campaign enrollment against the signed cumulative bounds. It cannot
// replace P3 accounting or be reconstructed from a caller's report booleans.
// Failure after consumption leaves a spent trial; no automatic refund/retry.
type PilotBudgetGuard interface {
	ConsumePilotEnrollment(context.Context, pilot.Manifest, pilot.PromotionGrant, pilot.TrialSpec) error
}

// PilotCampaignMonitor reads current protected outcomes/incidents and evaluates
// the preregistered stopping rules, including changes after grant issuance.
// Absence or uncertainty blocks new admissions and cancels guarded attempts.
type PilotCampaignMonitor interface {
	CheckPilotCampaign(context.Context, pilot.Manifest, pilot.PromotionGrant, time.Time) error
}

type PilotDispatch struct {
	Manifest       pilot.Manifest
	Policy         pilot.Policy
	Evidence       pilot.Bundle
	Grant          pilot.PromotionGrant
	PromotionTrust pilot.PromotionTrust
	Verifier       pilot.QualificationVerifier
	Activation     *operations.Activation
	Budget         PilotBudgetGuard
	Monitor        PilotCampaignMonitor
	RouteSHA256    string
	TrialID        string
	RunID          string
}

// PilotActivationScope binds external single-writer authority to both the exact
// frozen campaign and signed grant. A renewed grant never resets logical IDs.
func PilotActivationScope(m pilot.Manifest, g pilot.PromotionGrant, routeSHA256 string) string {
	return usage.Digest(struct{ Campaign, Grant, Route string }{usage.Digest(m), usage.Digest(g), routeSHA256})
}

// DispatchPilotOnce composes existing P7 admission/execution. It is deliberately
// limited to the initial single-step, single-candidate, standard-risk pilot.
// Calibration records cannot enter this production path; P4 remains mandatory.
func (c *Controller) DispatchPilotOnce(ctx context.Context, r PilotDispatch) (records.Report, error) {
	report := NewReport()
	fail := func(reason string) (records.Report, error) { return blockedReport(report, reason), nil }
	if r.Manifest.Purpose != pilot.Production || r.Manifest.Risk != "standard" {
		return fail("production pilot requires standard-risk production-policy purpose")
	}
	if r.Verifier == nil || r.Activation == nil || r.Budget == nil || r.Monitor == nil || c.Store == nil {
		return fail("protected native verifier, activation, cumulative budget and runtime required")
	}
	now := time.Now().UTC()
	if err := r.Monitor.CheckPilotCampaign(ctx, r.Manifest, r.Grant, now); err != nil {
		return fail("campaign stop monitor: " + err.Error())
	}
	a, err := pilot.AssessWithVerifier(r.Manifest, r.Policy, r.Evidence, now, r.Verifier)
	if err != nil {
		return report, err
	}
	if _, err = pilot.VerifyPromotionGrant(r.Manifest, a, r.Evidence, r.Grant, r.PromotionTrust); err != nil {
		return fail("promotion: " + err.Error())
	}
	if !has(r.Grant.RouteSHA256Allowlist, r.RouteSHA256) || r.Activation.ScopeSHA256() != PilotActivationScope(r.Manifest, r.Grant, r.RouteSHA256) {
		return fail("pilot activation does not bind exact campaign, grant and route")
	}
	if c.Policy.MaxConcurrent != 1 || c.Policy.MaxAttempts != 1 {
		return fail("initial pilot requires concurrency one and no automatic retry")
	}
	if _, ok := c.Runner.(hostbridge.NativeRunner); !ok {
		return fail("initial production pilot requires the qualified native runner")
	}
	var trial *pilot.TrialSpec
	for i := range r.Manifest.Trials {
		if r.Manifest.Trials[i].ID == r.TrialID {
			trial = &r.Manifest.Trials[i]
		}
	}
	if trial == nil || trial.RouteSHA256 != r.RouteSHA256 {
		return fail("trial is outside frozen route enrollment")
	}
	state, err := c.Store.Snapshot(ctx)
	if err != nil {
		return report, err
	}
	run, err := findRun(state, r.RunID)
	if err != nil {
		return report, err
	}
	// Runtime enrollment defines trial input as the captured complete P7 manifest,
	// not a mutable workspace, display key, raw git commit, or selection report.
	if len(run.Manifest.Steps) != 1 || run.Manifest.Repository != trial.Repository || run.ManifestSHA256 != trial.InputSHA256 {
		return fail("trial input must bind exact singleton captured runtime manifest")
	}
	spec := run.Manifest.Steps[0]
	inputRoot := filepath.Join(c.runRoot(r.RunID), "inputs")
	cb, err := readAsset(inputRoot, records.Asset{Path: spec.Selection.Candidates, SHA256: findAssetSHA(run.Manifest.Assets, spec.Selection.Candidates)})
	if err != nil {
		return fail("captured pinned candidates unavailable")
	}
	var candidates selection.CandidateManifest
	if usage.Decode(cb, &candidates) != nil || len(candidates.Candidates) != 1 || candidates.Candidates[0].ConfigurationID != r.Manifest.ConfigurationSHA256 {
		return fail("initial pilot requires one exact frozen configuration")
	}
	tb, err := readAsset(inputRoot, records.Asset{Path: spec.Selection.Task, SHA256: findAssetSHA(run.Manifest.Assets, spec.Selection.Task)})
	var task selection.TaskProfile
	if err != nil || usage.Decode(tb, &task) != nil || task.Risk != "standard" || task.ProtocolSHA256 != r.Manifest.ProtocolSHA256 {
		return fail("task risk/protocol differs from campaign")
	}
	routeFound := false
	var pinnedHostPolicySHA, profileSHA string
	for _, route := range r.Manifest.Routes {
		if usage.Digest(route) == r.RouteSHA256 && route.Host == spec.Host && route.Account == trial.Account {
			candidate := candidates.Candidates[0]
			if candidate.Profile.SHA256 != route.ProfileSHA256 || candidate.ConfigurationID != route.ConfigurationSHA256 || candidate.AccountScope != trial.Account || route.Platform != goruntime.GOOS+"/"+goruntime.GOARCH {
				return fail("pinned configuration/profile/account/platform differs from frozen route")
			}
			hp, routeErr := c.route(spec.Host, c.now(), 1, spec.ID)
			if routeErr != nil {
				return fail("native route: " + routeErr.Error())
			}
			if hp.HostVersion != route.HostVersion || hp.ExecutableSHA256 != route.HostSHA256 || hostbridge.PolicyHash(hp) != route.ControlPolicySHA256 || hp.AccountScope != trial.Account || hp.AuthMode != route.AuthMode || hp.TimeoutSeconds > trial.MaximumSeconds || hp.TimeoutSeconds > r.Grant.MaximumSeconds {
				return fail("native policy exceeds or differs from frozen route and time cap")
			}
			expectedTransport := "app-server"
			if spec.Host == "claude" || spec.Host == "claude-code" {
				expectedTransport = "stream-json"
			}
			if route.Transport != expectedTransport {
				return fail("pilot transport differs from native protocol")
			}
			pinnedHostPolicySHA = hostbridge.PolicyHash(hp)
			profileSHA = route.ProfileSHA256
			routeFound = true
		}
	}
	if !routeFound {
		return fail("host/account route differs from frozen trial")
	}
	if err = r.Activation.Check(ctx); err != nil {
		return fail(err.Error())
	}
	if err = r.Budget.ConsumePilotEnrollment(ctx, r.Manifest, r.Grant, *trial); err != nil {
		return fail("campaign budget: " + err.Error())
	}
	if err = c.ConfigurePilotActivation(ctx, r.Activation, PilotActivationScope(r.Manifest, r.Grant, r.RouteSHA256), []PilotTrialBinding{{RunID: r.RunID, StepID: spec.ID, TrialID: r.TrialID, ManifestSHA256: run.ManifestSHA256, HostPolicySHA256: pinnedHostPolicySHA, ConfigurationSHA256: r.Manifest.ConfigurationSHA256, ProfileSHA256: profileSHA, NativeRunner: true}}); err != nil {
		return report, err
	}
	c.pilotRevalidate = func(ctx context.Context) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := r.Monitor.CheckPilotCampaign(ctx, r.Manifest, r.Grant, time.Now().UTC()); err != nil {
			return err
		}
		assessment, err := pilot.AssessWithVerifier(r.Manifest, r.Policy, r.Evidence, time.Now().UTC(), r.Verifier)
		if err != nil {
			return err
		}
		_, err = pilot.VerifyPromotionGrant(r.Manifest, assessment, r.Evidence, r.Grant, r.PromotionTrust)
		return err
	}
	return c.Dispatch(ctx, r.RunID)
}
func findAssetSHA(xs []records.Asset, path string) string {
	for _, x := range xs {
		if x.Path == path {
			return x.SHA256
		}
	}
	return ""
}
