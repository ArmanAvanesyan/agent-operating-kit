// Package v2 composes additive host qualification with the existing P7
// controller. It has no executor, ledger, accountant, or acceptance store.
package v2

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/hostbridge"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/hostbridge/v2"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/readiness"
	readinessv2 "github.com/ArmanAvanesyan/agent-operating-kit/internal/readiness/v2"
	runtimep7 "github.com/ArmanAvanesyan/agent-operating-kit/internal/runtime"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/runtime/records"
)

const Version = "aok-runtime/v2"

type Request struct {
	RunID              string
	Route              readinessv2.Route
	Readiness          readiness.Report
	ReadinessProof     json.RawMessage
	ReadinessAuthority readinessv2.Authority
	HostScope          v2.Scope
	HostProof          json.RawMessage
	HostVerifier       v2.QualificationVerifier
}
type Result struct {
	Version   string                `json:"version"`
	Admission readinessv2.Admission `json:"admission"`
	Report    records.Report        `json:"report"`
}

// DispatchQualified first verifies exact readiness and host proofs, then hands
// all claims, P3 holds, host launch, and P8 projection to the existing P7
// Controller.Dispatch path. Non-wired drivers remain blocked by readiness/v2.
func DispatchQualified(ctx context.Context, c *runtimep7.Controller, r Request) (Result, error) {
	out := Result{Version: Version}
	if ctx == nil || c == nil || r.RunID == "" || r.HostVerifier == nil {
		return out, errors.New("invalid v2 P7 dispatch request")
	}
	a, err := Admit(ctx, r)
	if err != nil {
		return out, err
	}
	out.Admission = a
	if a.Status != "eligible" {
		return out, errors.New(a.Reason)
	}
	if r.Route.MethodologyRequired && !c.HasMethodologyBinding(r.Route.MethodologyBindingSHA256) {
		return out, errors.New("protected methodology authority required by v2 route")
	}
	ctx, err = runtimep7.WithV2LaunchAdmission(ctx, func(ctx context.Context, run records.Run, spec records.StepSpec, step records.Step, prepared hostbridge.Prepared, policy hostbridge.Policy, launch hostbridge.Launch) error {
		s := r.HostScope
		inputSHA := hostbridge.Digest(prepared.Input)
		if run.Manifest.ID != r.RunID || spec.Host != s.Host || prepared.Host != s.Host || prepared.HostDriverSHA256 != s.DescriptorSHA256 || prepared.HostProtocol != r.Route.Protocol || policy.HostDriverSHA256 != s.DescriptorSHA256 || policy.HostProtocol != r.Route.Protocol || prepared.Digest != s.PreparedSHA256 || prepared.PolicySHA256 != s.PolicySHA256 || prepared.HostVersion != s.HostVersion || prepared.Workspace != s.Workspace || prepared.Model != s.Model || prepared.ProfileSHA256 != s.ProfileSHA256 || hostbridge.Digest(prepared.Configuration) != s.ConfigurationSHA256 || policy.ExecutableSHA256 != s.ExecutableSHA256 || policy.AccountScope != s.AccountScope || policy.AuthMode != s.AuthMode || inputSHA != s.InputSHA256 || hostbridge.Digest(launch) != s.LaunchSHA256 || launch.Executable != policy.Executable || launch.Dir != s.Workspace {
			return errors.New("actual P7 prepared assignment or launch differs from qualified v2 scope")
		}
		return r.HostVerifier.Verify(ctx, s, r.HostProof)
	})
	if err != nil {
		return out, err
	}
	report, err := c.Dispatch(ctx, r.RunID)
	out.Report = report
	if err != nil {
		return out, err
	}
	return out, nil
}

func Admit(ctx context.Context, r Request) (readinessv2.Admission, error) {
	if ctx == nil || r.RunID == "" || r.HostVerifier == nil {
		return readinessv2.Admission{}, errors.New("invalid v2 P7 dispatch request")
	}
	a, err := readinessv2.Admit(r.Route, r.Readiness, r.ReadinessProof, r.ReadinessAuthority)
	if err != nil {
		return a, err
	}
	if a.Status != "eligible" {
		return a, nil
	}
	if r.HostScope.RunID != r.RunID || r.HostScope.Host != r.Route.Legacy.Host || r.HostScope.DescriptorSHA256 != r.Route.DriverSHA256 || r.HostScope.HostVersion != r.Route.Legacy.HostVersion || r.HostScope.PreparedSHA256 != r.Route.PreparedSHA256 || r.HostScope.PolicySHA256 != r.Route.PolicySHA256 || r.HostScope.LaunchSHA256 != r.Route.LaunchSHA256 || r.HostScope.InputSHA256 != r.Route.InputSHA256 || r.HostScope.ExecutableSHA256 != r.Route.Legacy.HostSHA256 || r.HostScope.ProfileSHA256 != r.Route.Legacy.ProfileSHA256 || r.HostScope.ConfigurationSHA256 != r.Route.Legacy.ConfigurationSHA256 || r.HostScope.AccountScope != r.Route.Legacy.Account || r.HostScope.AuthMode != r.Route.Legacy.AuthMode {
		return a, errors.New("host qualification scope differs from readiness route")
	}
	if !json.Valid(r.HostProof) {
		return a, errors.New("missing host qualification proof")
	}
	if err := r.HostVerifier.Verify(ctx, r.HostScope, r.HostProof); err != nil {
		return a, errors.New("protected host qualification rejected exact route")
	}
	return a, nil
}
