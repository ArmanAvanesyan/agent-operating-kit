package cli

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/connector"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/operations"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/writeback"
)

// TrackerDispatchInputs comes only from an installed protected route resolver.
// Credentials are neither inputs nor output: the writer owns an opaque resolver.
type TrackerDispatchInputs struct {
	Operation      contracts.SyncOperation
	Capability     contracts.ConnectorCapability
	Identity       contracts.RemoteIdentity
	Grant          writeback.Grant
	Policy         writeback.TrustPolicy
	Revocation     writeback.RevocationSnapshot
	Current        writeback.GrantContext
	VerifyEvidence writeback.EvidenceVerifier
	Writer         connector.Writer
	Activation     *operations.Activation
	RouteScope     string
	Owner          string
}

// TrackerReconciliationInputs requires an independently signed readback report.
// Reconciliation has read authority independent of mutation grant expiry.
type TrackerReconciliationInputs struct {
	Operation contracts.SyncOperation
	Identity  contracts.RemoteIdentity
	Policy    writeback.TrustPolicy
	Report    writeback.ReconciliationReport
}

func phase9TrackerDispatch(ctx context.Context, boxPath, operationID string, services Phase9Services, out io.Writer) (int, error) {
	inputs, err := services.TrackerDispatch(ctx, operationID)
	if err != nil {
		return 1, fmt.Errorf("qualified exact-operation route unavailable")
	}
	if inputs.Operation.OperationID != operationID || inputs.Owner == "" {
		return 1, fmt.Errorf("protected route operation binding mismatch")
	}
	box, err := writeback.Open(boxPath)
	if err != nil {
		return 1, err
	}
	defer box.Close()
	entry, err := connector.DispatchOnceActivated(ctx, box, inputs.Writer, inputs.Operation, inputs.Capability, inputs.Identity, inputs.Grant, inputs.Policy, inputs.Revocation, inputs.Current, inputs.VerifyEvidence, inputs.Owner, time.Now().UTC(), inputs.Activation, inputs.RouteScope)
	if err != nil {
		return 1, err
	}
	return phase8Print(out, entry)
}
func phase9TrackerReconcile(ctx context.Context, boxPath, operationID string, services Phase9Services, out io.Writer) (int, error) {
	inputs, err := services.TrackerReconciliation(ctx, operationID)
	if err != nil {
		return 1, fmt.Errorf("qualified readback route unavailable")
	}
	if inputs.Operation.OperationID != operationID {
		return 1, fmt.Errorf("reconciliation operation binding mismatch")
	}
	verified, err := writeback.VerifyReconciliation(inputs.Report, inputs.Policy, inputs.Operation, inputs.Identity, time.Now().UTC())
	if err != nil {
		return 1, err
	}
	box, err := writeback.Open(boxPath)
	if err != nil {
		return 1, err
	}
	defer box.Close()
	entry, err := box.ReconcileVerified(verified)
	if err != nil {
		return 1, err
	}
	return phase8Print(out, entry)
}
