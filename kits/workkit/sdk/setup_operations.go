package sdk

import (
	"context"
	"errors"
	o "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/operationsv1"
	s "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/setupv1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/engine"
	check "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/setup"
	"time"
)

type SetupHandoff struct {
	Version              string        `json:"version"`
	Kind                 string        `json:"kind"`
	PlanSHA256           string        `json:"planSha256"`
	Change               s.Change      `json:"change"`
	Observation          s.Observation `json:"observation"`
	AuthorizationGranted bool          `json:"authorizationGranted"`
	ExecutionReady       bool          `json:"executionReady"`
}
type PreparedSetupOperation struct {
	handoff SetupHandoff
	files   map[string][]byte
}

func (p PreparedSetupOperation) Handoff() SetupHandoff     { return cloneRead(p.handoff) }
func (p PreparedSetupOperation) Digest() string            { return check.Hash(p.handoff) }
func (p PreparedSetupOperation) Assets() map[string][]byte { return cloneBytes(p.files) }
func PrepareSetupOperation(ctx context.Context, root, id, asOf string) (PreparedSetupOperation, error) {
	files, err := loadSetupReportBundle(ctx, root)
	if err != nil {
		return PreparedSetupOperation{}, err
	}
	if asOf == "" {
		var original s.Report
		if DecodeRead(files["report.json"], &original) != nil {
			return PreparedSetupOperation{}, errors.New("invalid original report")
		}
		asOf = original.AsOf
	}
	report, err := validateCapturedSetupPlan(files, asOf)
	if err != nil {
		return PreparedSetupOperation{}, err
	}

	var meta s.Observation
	if DecodeRead(files["assets/observation.json"], &meta) != nil {
		return PreparedSetupOperation{}, errors.New("pinned setup observation required")
	}
	var selected s.Change
	found := false
	for _, c := range report.Changes {
		if c.ID == id {
			if found {
				return PreparedSetupOperation{}, errors.New("ambiguous change")
			}
			selected = c
			found = true
		}
	}
	if !found || selected.Operation != "field.create" && selected.Operation != "option.create" {
		return PreparedSetupOperation{}, errors.New("exact additive selected change required")
	}
	var original s.Report
	if DecodeRead(files["report.json"], &original) != nil {
		return PreparedSetupOperation{}, errors.New("original plan required")
	}
	if original.Status == "blocked" {
		return PreparedSetupOperation{}, errors.New("blocked setup plan")
	}
	return PreparedSetupOperation{SetupHandoff{Version: o.Version, Kind: "SetupHandoff", PlanSHA256: SHA256(files["assets/setup-plan.json"]), Change: selected, Observation: meta}, files}, nil
}

type SetupOperationBackend interface {
	ExecuteSetup(context.Context, PreparedSetupOperation, []byte, []byte) (o.Observation, error)
}

func (svc *Service) WithSetupOperationBackend(b SetupOperationBackend) *Service {
	v := *svc
	v.setupOperations = b
	return &v
}
func ValidateSetupOperationAuthority(p PreparedSetupOperation, policy, enraw []byte) (o.Enrollment, error) {
	e, err := validateMutationAuthority(policy, enraw, p.Digest(), "setup-action", []s.Scope{p.handoff.Observation.Scope}, time.Now().UTC())
	if err != nil {
		return e, err
	}
	if e.Action != "apply" && e.Action != "reconcile" {
		return e, errors.New("unsupported setup action")
	}
	if e.Action == "apply" {
		report := check.NewReport("SetupFreshness", time.Now().UTC().Format(time.RFC3339Nano))
		check.Fresh(&report, p.handoff.Observation, report.AsOf, 300)
		if report.Status != "ok" {
			return e, errors.New("fresh setup observation required")
		}
	}
	return e, nil
}
func SetupOperationEnrollmentSigningBytes(e o.Enrollment) []byte { return mutationSigningBytes(e) }

type ExecuteSetupOperationRequest struct{ Plan, Operation, Policy, Enrollment, Action, Out string }

func (svc *Service) ExecuteSetupOperation(ctx context.Context, q ExecuteSetupOperationRequest) (o.Observation, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	var result o.Observation
	if err := setupOutput(q.Out, q.Plan); err != nil {
		return result, err
	}
	p, err := PrepareSetupOperation(ctx, q.Plan, q.Operation, "")
	if err != nil {
		return result, err
	}
	policy, err := ReadLocal(ctx, q.Policy)
	if err != nil {
		return result, err
	}
	enraw, err := ReadLocal(ctx, q.Enrollment)
	if err != nil {
		return result, err
	}
	en, err := ValidateSetupOperationAuthority(p, policy, enraw)
	if err != nil || en.Action != q.Action || en.OutputPath != q.Out {
		return result, operationFailure("setup.authority", "exact setup purpose authority required", 3, err)
	}
	if svc.setupOperations == nil {
		return result, operationFailure("setup.unavailable", "protected existing provisioning boundary unavailable", 3, nil)
	}
	w, err := engine.ReserveBundle(ctx, q.Out)
	if err != nil {
		return result, err
	}
	result, err = svc.setupOperations.ExecuteSetup(ctx, p, policy, enraw)
	if result.OperationID != q.Operation || result.Digest != p.Digest() || result.Store != en.Store || result.AuthorizationGranted || result.ExecutionReady || result.MigrationActivated || result.ProductionActivated {
		return result, operationFailure("setup.receipt", "unbound provisioning result", 5, nil)
	}
	err = setupOperationOutcomeError(result, err)
	if e := w.Write(ctx, operationalReportFiles(o.Version, result, map[string][]byte{"assets/handoff.json": engine.Encode(p.handoff), "assets/policy.json": policy, "assets/enrollment.json": enraw})); e != nil {
		return result, operationFailure("setup.receipt", "provisioning evidence uncertain", 5, e)
	}
	if e := w.Sync(ctx); e != nil {
		return result, operationFailure("setup.receipt", "provisioning durability uncertain", 5, e)
	}
	return result, err
}

func setupOperationOutcomeError(result o.Observation, cause error) error {
	switch result.State {
	case "readback-observed", "readback-verified":
		if cause == nil {
			return nil
		}
		return operationFailure("setup.uncertain", "effect/readback observed but durable receipt failed; reconcile without resend", 5, cause)
	case "blocked", "prepared", "authorized":
		if cause != nil {
			var typed *Error
			if errors.As(cause, &typed) {
				return cause
			}
		}
		return operationFailure("setup.blocked", "protected setup prerequisites unavailable", 3, cause)
	case "rejected":
		return operationFailure("setup.rejected", "provider rejection retained", 1, cause)
	default:
		return operationFailure("setup.uncertain", "setup effect remains unresolved; reconcile without resend", 5, cause)
	}
}
