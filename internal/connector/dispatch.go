package connector

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/operations"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/writeback"
)

// DispatchOnce is the bounded bridge from a durable outbox to an injected writer.
// The caller supplies protected current policy/revocation inputs; there is no
// implicit provider discovery, credential lookup, retry, or alternate route.
func DispatchOnce(ctx context.Context, box *writeback.Outbox, writer Writer, op contracts.SyncOperation, cap contracts.ConnectorCapability, id contracts.RemoteIdentity, grant writeback.Grant, policy writeback.TrustPolicy, rev writeback.RevocationSnapshot, current writeback.GrantContext, verifyEvidence writeback.EvidenceVerifier, owner string, now time.Time) (writeback.Entry, error) {
	return dispatchOnce(ctx, box, writer, op, cap, id, grant, policy, rev, current, verifyEvidence, owner, now, nil)
}

// DispatchOnceActivated adds an external anti-rollback consumption boundary to
// normal signed-grant/outbox checks. The protected coordinator binds routeScope
// to the exact RouteManifest and must retain the same logical operation ID.
func DispatchOnceActivated(ctx context.Context, box *writeback.Outbox, writer Writer, op contracts.SyncOperation, cap contracts.ConnectorCapability, id contracts.RemoteIdentity, grant writeback.Grant, policy writeback.TrustPolicy, rev writeback.RevocationSnapshot, current writeback.GrantContext, verifyEvidence writeback.EvidenceVerifier, owner string, now time.Time, activation *operations.Activation, routeScope string) (writeback.Entry, error) {
	if activation == nil || routeScope == "" || activation.ScopeSHA256() != routeScope {
		return writeback.Entry{}, fmt.Errorf("qualified exact-route activation required")
	}
	return dispatchOnce(ctx, box, writer, op, cap, id, grant, policy, rev, current, verifyEvidence, owner, now, activation)
}

func dispatchOnce(ctx context.Context, box *writeback.Outbox, writer Writer, op contracts.SyncOperation, cap contracts.ConnectorCapability, id contracts.RemoteIdentity, grant writeback.Grant, policy writeback.TrustPolicy, rev writeback.RevocationSnapshot, current writeback.GrantContext, verifyEvidence writeback.EvidenceVerifier, owner string, now time.Time, activation *operations.Activation) (writeback.Entry, error) {
	if ctx.Err() != nil {
		return writeback.Entry{}, ctx.Err()
	}
	if box == nil || writer == nil {
		return writeback.Entry{}, fmt.Errorf("outbox and qualified writer required")
	}
	if _, ds := ValidateOperation(op, cap, id); len(ds) != 0 {
		return writeback.Entry{}, fmt.Errorf("invalid operation")
	}
	if _, err := writeback.VerifyGrantBound(grant, policy, rev, op, cap, id, current, verifyEvidence, now); err != nil {
		return writeback.Entry{}, err
	}
	if err := box.ImportRevocation(policy, rev, now); err != nil {
		return writeback.Entry{}, err
	}
	entry, err := box.Claim(op.OperationID, owner, now, 30*time.Second)
	if err != nil {
		return entry, err
	}
	approved, err := writeback.VerifyGrantBound(grant, policy, rev, op, cap, id, current, verifyEvidence, now)
	if err != nil {
		return entry, err
	}
	if activation != nil {
		// Conservative ordering: a crash after this point spends the external ID
		// even if no local intent was committed. Reconcile rather than resend.
		if err = activation.Consume(ctx, op.OperationID, "provider-send", op.OperationDigest); err != nil {
			return entry, err
		}
	}
	entry, err = box.CommitSendIntentBound(op.OperationID, owner, entry.Fence, now, approved, op)
	if err != nil {
		return entry, err
	}
	auth, err := authorize(op, cap, id, grant, approved, now, box, owner, entry.Fence)
	if err != nil {
		return entry, err
	}
	result, sendErr := Apply(ctx, writer, op, cap, id, auth)
	state := "unknown-awaiting-reconciliation"
	// A mutation response alone is never evidence of readback success.
	if sendErr == nil && result.Disposition == Supported && len(result.Value.DispatchEvidence) > 0 && validateTyped(result.Value) == nil {
		state = "response-observed"
	}
	if result.Value.Disposition == "rejected" {
		state = "rejected"
	}
	if result.Disposition == Conflict || result.Value.Disposition == "conflict" {
		state = "conflict"
	}
	detail, _ := json.Marshal(result.Value)
	final, recordErr := box.RecordOutcome(op.OperationID, owner, entry.Fence, state, string(detail))
	if recordErr != nil {
		return entry, recordErr
	}
	return final, sendErr
}
