// Package v2 binds a frozen P9 pilot to the current compiled host driver and
// delegates enrollment and dispatch to the P7 runtime controller.
package v2

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/hostbridge"
	hostv2 "github.com/ArmanAvanesyan/agent-operating-kit/internal/hostbridge/v2"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/readiness"
	readinessv2 "github.com/ArmanAvanesyan/agent-operating-kit/internal/readiness/v2"
	runtimep7 "github.com/ArmanAvanesyan/agent-operating-kit/internal/runtime"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/runtime/records"
)

const Version = "aok-pilot/v2"

type Request struct {
	Route              readinessv2.Route
	Readiness          readiness.Report
	ReadinessProof     json.RawMessage
	ReadinessAuthority readinessv2.Authority
	HostScope          hostv2.Scope
	HostProof          json.RawMessage
	HostVerifier       hostv2.QualificationVerifier
	Pilot              runtimep7.PilotDispatch
}

// Dispatch composes exact readiness/host proofs with existing P9 promotion,
// P3 accounting, P7 claim/checkpoint and P8 acceptance boundaries. It refuses
// to launch a host for which the P7 controller has no native transport path.
func Dispatch(ctx context.Context, c *runtimep7.Controller, r Request) (records.Report, error) {
	if c == nil || ctx == nil || r.HostVerifier == nil {
		return records.Report{}, errors.New("invalid v2 pilot route request")
	}
	a, err := Admit(ctx, r)
	if err != nil {
		return records.Report{}, err
	}
	if a.Status != "eligible" {
		return records.Report{}, errors.New(a.Reason)
	}
	if r.Route.MethodologyRequired && !c.HasMethodologyBinding(r.Route.MethodologyBindingSHA256) {
		return records.Report{}, errors.New("protected methodology authority required by v2 pilot route")
	}
	if len(r.Pilot.Manifest.Routes) != 1 {
		return records.Report{}, errors.New("pilot v2 requires the existing single-route frozen campaign")
	}
	pr := r.Pilot.Manifest.Routes[0]
	if r.Pilot.RouteSHA256 != a.RouteSHA256 || pr.Host != r.Route.Legacy.Host || pr.HostVersion != r.Route.Legacy.HostVersion || pr.HostSHA256 != r.Route.Legacy.HostSHA256 || pr.ConfigurationSHA256 != r.Route.Legacy.ConfigurationSHA256 || pr.ProfileSHA256 != r.Route.Legacy.ProfileSHA256 || pr.Transport != r.Route.Legacy.Transport {
		return records.Report{}, errors.New("frozen pilot route differs from qualified v2 route")
	}
	ctx, err = runtimep7.WithV2LaunchAdmission(ctx, func(ctx context.Context, run records.Run, spec records.StepSpec, step records.Step, prepared hostbridge.Prepared, policy hostbridge.Policy, launch hostbridge.Launch) error {
		s := r.HostScope
		if run.Manifest.ID != s.RunID || spec.Host != s.Host || prepared.Host != s.Host || prepared.HostDriverSHA256 != s.DescriptorSHA256 || prepared.HostProtocol != r.Route.Protocol || policy.HostDriverSHA256 != s.DescriptorSHA256 || policy.HostProtocol != r.Route.Protocol || prepared.Digest != s.PreparedSHA256 || prepared.PolicySHA256 != s.PolicySHA256 || prepared.HostVersion != s.HostVersion || prepared.Workspace != s.Workspace || prepared.Model != s.Model || prepared.ProfileSHA256 != s.ProfileSHA256 || hostbridge.Digest(prepared.Configuration) != s.ConfigurationSHA256 || policy.ExecutableSHA256 != s.ExecutableSHA256 || policy.AccountScope != s.AccountScope || policy.AuthMode != s.AuthMode || hostbridge.Digest(prepared.Input) != s.InputSHA256 || hostbridge.Digest(launch) != s.LaunchSHA256 || launch.Executable != policy.Executable || launch.Dir != s.Workspace {
			return errors.New("actual P7 pilot assignment or launch differs from qualified v2 scope")
		}
		return r.HostVerifier.Verify(ctx, s, r.HostProof)
	})
	if err != nil {
		return records.Report{}, err
	}
	return c.DispatchPilotOnce(ctx, r.Pilot)
}

func Admit(ctx context.Context, r Request) (readinessv2.Admission, error) {
	a, err := readinessv2.Admit(r.Route, r.Readiness, r.ReadinessProof, r.ReadinessAuthority)
	if err != nil || a.Status != "eligible" {
		return a, err
	}
	if r.HostVerifier == nil || !json.Valid(r.HostProof) {
		return a, errors.New("protected host qualification is required")
	}
	s := r.HostScope
	if s.RunID != r.Pilot.RunID || s.Host != r.Route.Legacy.Host || s.DescriptorSHA256 != r.Route.DriverSHA256 || s.HostVersion != r.Route.Legacy.HostVersion || s.PreparedSHA256 != r.Route.PreparedSHA256 || s.PolicySHA256 != r.Route.PolicySHA256 || s.LaunchSHA256 != r.Route.LaunchSHA256 || s.InputSHA256 != r.Route.InputSHA256 || s.ExecutableSHA256 != r.Route.Legacy.HostSHA256 || s.ProfileSHA256 != r.Route.Legacy.ProfileSHA256 || s.ConfigurationSHA256 != r.Route.Legacy.ConfigurationSHA256 || s.AccountScope != r.Route.Legacy.Account || s.AuthMode != r.Route.Legacy.AuthMode {
		return a, errors.New("host proof scope differs from frozen route")
	}
	if err := r.HostVerifier.Verify(ctx, s, r.HostProof); err != nil {
		return a, errors.New("protected host qualification rejected exact route")
	}
	return a, nil
}
