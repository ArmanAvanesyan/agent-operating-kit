package authority

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/hostbridge"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/operations"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/pilot"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/pilot/experiment"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/readiness"
	readinessv2 "github.com/ArmanAvanesyan/agent-operating-kit/internal/readiness/v2"
	workflow "github.com/ArmanAvanesyan/agent-operating-kit/internal/runtime"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
)

// Remote is assembled from protected, signed operator configuration. Bindings
// must be the COMPLETE enrollment binding map. It never discovers trust roots.
// Its checks authenticate independently supplied live proofs; they do not
// perform the measurements or establish independence of the authority service.
type Remote struct {
	Client          Client
	Bindings        map[string]string
	Epoch           operations.Epoch
	ReadinessPolicy *readinessv2.Policy
}

func (r *Remote) check(ctx context.Context, required map[string]string) (Response, error) {
	if r == nil || len(r.Bindings) == 0 {
		return Response{}, ErrBlocked
	}
	for k, v := range required {
		if !validHash(v) || r.Bindings[k] != v {
			return Response{}, ErrBlocked
		}
	}
	b := make(map[string]string, len(r.Bindings))
	for k, v := range r.Bindings {
		b[k] = v
	}
	return r.Client.Call(ctx, Request{Action: "check", Bindings: b})
}
func (r *Remote) CheckTrial(ctx context.Context, s workflow.TrialScope, at time.Time) error {
	if r == nil || at.IsZero() || time.Since(at) > 5*time.Second || at.After(time.Now().Add(time.Second)) || usage.Digest(s) != usage.Digest(r.Client.Scope) {
		return ErrBlocked
	}
	_, e := r.check(ctx, map[string]string{"trial-scope": r.Bindings["trial-scope"]})
	return e
}
func (r *Remote) ConsumeTrialBudget(ctx context.Context, s workflow.TrialScope) error {
	if e := r.CheckTrial(ctx, s, time.Now()); e != nil {
		return e
	}
	_, e := r.Client.Call(ctx, Request{Action: "budget", LogicalID: s.TrialID})
	return e
}
func (r *Remote) VerifyTrialLaunch(ctx context.Context, s workflow.TrialScope, p hostbridge.Prepared, h hostbridge.Policy, l hostbridge.Launch) error {
	if e := r.CheckTrial(ctx, s, time.Now()); e != nil {
		return e
	}
	_, e := r.check(ctx, map[string]string{"prepared": p.Digest, "host-policy": hostbridge.PolicyHash(h), "launch": hostbridge.Digest(l)})
	return e
}
func (r *Remote) Current(ctx context.Context, authority string) (operations.Epoch, error) {
	if r == nil || authority != r.Epoch.Authority {
		return operations.Epoch{}, ErrBlocked
	}
	out, e := r.Client.Call(ctx, Request{Action: "current"})
	if e != nil {
		return operations.Epoch{}, e
	}
	if out.Epoch.Authority != r.Epoch.Authority || out.Epoch.Holder != r.Epoch.Holder || out.Epoch.ScopeSHA256 != r.Epoch.ScopeSHA256 || out.Epoch.Generation != r.Epoch.Generation {
		return operations.Epoch{}, ErrBlocked
	}
	return out.Epoch, nil
}
func (r *Remote) Consume(ctx context.Context, c operations.Consumption) error {
	e, err := r.Current(ctx, c.Epoch.Authority)
	if err != nil {
		return err
	}
	if c.Epoch.Generation != e.Generation || c.Epoch.Holder != e.Holder || c.Epoch.ScopeSHA256 != e.ScopeSHA256 {
		return ErrBlocked
	}
	_, err = r.Client.Call(ctx, Request{Action: "consume", LogicalID: c.LogicalID, Boundary: c.Boundary, InputSHA256: c.InputSHA256, Generation: e.Generation})
	return err
}
func (r *Remote) VerifyWitness(ctx context.Context, w operations.AuthorityWitness, authority, scope string, proof []byte, at time.Time) error {
	if w != r || authority != r.Epoch.Authority || scope != r.Epoch.ScopeSHA256 || at.IsZero() {
		return ErrBlocked
	}
	_, e := r.check(ctx, map[string]string{"witness-proof": byteHash(proof)})
	if e != nil {
		return e
	}
	_, e = r.Current(ctx, authority)
	return e
}
func (r *Remote) Stop(ctx context.Context, reasonSHA256 string) error {
	_, e := r.Client.Call(ctx, Request{Action: "stop", InputSHA256: reasonSHA256})
	return e
}
func (r *Remote) consumeTrial(grantID, trialID, scope string) error {
	if r == nil || trialID != r.Client.Scope.TrialID || usage.Digest(grantID) != r.Bindings["grant-id"] || scope != r.Bindings["trial-scope"] {
		return ErrBlocked
	}
	if _, e := r.check(context.Background(), map[string]string{"trial-scope": scope}); e != nil {
		return e
	}
	_, e := r.Client.Call(context.Background(), Request{Action: "trial", LogicalID: trialID, InputSHA256: scope})
	return e
}
func (r *Remote) ConsumeCalibrationTrial(g, t string, attempt int, s string) error {
	if attempt != 1 {
		return ErrBlocked
	}
	return r.consumeTrial(g, t, s)
}
func (r *Remote) ConsumeExperimentTrial(g, t, s string) error { return r.consumeTrial(g, t, s) }
func (r *Remote) CheckExperimentGrantCurrent(g experiment.Grant, at time.Time) error {
	if r == nil || experiment.GrantDigest(g) != r.Client.Scope.GrantSHA256 {
		return ErrBlocked
	}
	return r.CheckTrial(context.Background(), r.Client.Scope, at)
}

// Guard separates the calibration receipt Verify method from route validators.
type Guard struct{ Remote *Remote }

func (g Guard) Verify(q pilot.GuardReceipt, scope string, at time.Time) error {
	if g.Remote == nil || q.ScopeSHA256 != scope || scope != g.Remote.Bindings["trial-scope"] || at.IsZero() {
		return ErrBlocked
	}
	_, e := g.Remote.check(context.Background(), map[string]string{"guard:" + q.Gate: usage.Digest(q)})
	return e
}
func (r *Remote) VerifyRoutePolicy(route readinessv2.Route, p readinessv2.Policy) error {
	_, e := r.check(context.Background(), map[string]string{"readiness-route": usage.Digest(route), "readiness-policy": usage.Digest(p)})
	return e
}
func (r *Remote) ValidateRouteGate(route readinessv2.Route, p readinessv2.Policy, q readiness.GateRequirement, at time.Time) readiness.ValidationResult {
	fail := readiness.ValidationResult{Status: "blocked", Reason: "external exact-scope live gate unavailable"}
	if r == nil || q.EvidenceOrigin != "live" || len(q.Bindings) == 0 || at.IsZero() || time.Since(at) > 5*time.Second || at.After(time.Now().Add(time.Second)) {
		return fail
	}
	if e := r.VerifyRoutePolicy(route, p); e != nil {
		return fail
	}
	out, e := r.check(context.Background(), map[string]string{"readiness-gate:" + q.ID: usage.Digest(q)})
	if e != nil {
		return fail
	}
	until, err := time.Parse(time.RFC3339Nano, out.ValidUntil)
	if err != nil {
		return fail
	}
	hashes := []string{}
	for _, b := range q.Bindings {
		obs, oe := time.Parse(time.RFC3339Nano, b.ObservedAt)
		exp, ee := time.Parse(time.RFC3339Nano, b.ValidUntil)
		if oe != nil || ee != nil || b.Revoked || b.Origin != "live" || obs.After(at) || at.Sub(obs) > 5*time.Minute || !at.Before(exp) || !validHash(b.SHA256) {
			return fail
		}
		if exp.Before(until) {
			until = exp
		}
		hashes = append(hashes, b.SHA256)
	}
	return readiness.ValidationResult{Status: "passed", Reason: "current externally enrolled scoped evidence", EvidenceSHA256: hashes, ValidUntil: until.Format(time.RFC3339Nano)}
}
func (r *Remote) VerifyReadinessRoute(route readinessv2.Route, report readiness.Report, proof json.RawMessage) error {
	if r == nil || r.ReadinessPolicy == nil || report.PolicySHA256 != r.Bindings["readiness-policy"] || report.Status != "ready-offline" {
		return ErrBlocked
	}
	at, e := time.Parse(time.RFC3339Nano, report.AsOf)
	if e != nil || time.Since(at) > 5*time.Second || at.After(time.Now().Add(time.Second)) {
		return ErrBlocked
	}
	if _, e = r.check(context.Background(), map[string]string{"readiness-route": usage.Digest(route), "readiness-proof": byteHash(proof)}); e != nil {
		return e
	}
	validators := map[string]readinessv2.Validator{}
	for _, q := range r.ReadinessPolicy.Requirements {
		validators[q.Validator] = r
	}
	fresh, e := readinessv2.Assess(route, *r.ReadinessPolicy, at, r, validators)
	if e != nil || fresh.Report.Status != "ready-offline" {
		return ErrBlocked
	}
	// Recompute every gate and input binding. Only a SHORTER validity boundary
	// than our fresh assessment is acceptable; no caller-chosen report is trusted.
	limit, le := time.Parse(time.RFC3339Nano, fresh.Report.EarliestValidUntil)
	provided, pe := time.Parse(time.RFC3339Nano, report.EarliestValidUntil)
	if le != nil || pe != nil || provided.After(limit) || !time.Now().Before(provided) {
		return ErrBlocked
	}
	fresh.Report.EarliestValidUntil = report.EarliestValidUntil
	if usage.Digest(fresh.Report) != usage.Digest(report) {
		return ErrBlocked
	}
	return nil
}

var _ workflow.TrialServices = (*Remote)(nil)
var _ operations.AuthorityWitness = (*Remote)(nil)
var _ operations.WitnessVerifier = (*Remote)(nil)
var _ readinessv2.PolicyAuthority = (*Remote)(nil)
var _ readinessv2.Validator = (*Remote)(nil)
var _ readinessv2.Authority = (*Remote)(nil)

func byteHash(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
