package github

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/cli"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/connector"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/operations"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/writeback"
)

// ServicesConfig contains root-protected, exact-operation inputs assembled by
// the embedding operator. Task data can select the matching ID but cannot
// provide or replace these operation, identity, policy, grant, writer or
// authority values.
type ServicesConfig struct {
	OperationID      string
	OperationDigest  string
	Dispatch         cli.TrackerDispatchInputs
	Reconciliation   cli.TrackerReconciliationInputs
	Writer           connector.Writer
	Activation       *operations.Activation
	RouteScope       string
	EvidenceVerifier writeback.EvidenceVerifier
}

// Services assembles only the protected GitHub CLI writeback hooks. It does
// not collect evidence, approve a grant, create a writer credential, or dispatch
// an operation. The caller must supply a writer pinned to the same GitHub
// identity and an externally created activation and evidence verifier.
func Services(c ServicesConfig) (cli.Phase9Services, error) {
	var out cli.Phase9Services
	if c.OperationID == "" || !validDigest(c.OperationDigest) || nilLike(c.Writer) || c.Activation == nil || c.RouteScope == "" || c.Activation.ScopeSHA256() != c.RouteScope || c.EvidenceVerifier == nil {
		return out, errors.New("protected GitHub operation, writer, activation and evidence verifier required")
	}
	dispatch := c.Dispatch
	if err := cloneJSONInto(&dispatch.Operation, c.Dispatch.Operation); err != nil {
		return out, errors.New("protected GitHub operation snapshot unavailable")
	}
	if err := cloneJSONInto(&dispatch.Capability, c.Dispatch.Capability); err != nil {
		return out, errors.New("protected GitHub capability snapshot unavailable")
	}
	if err := cloneJSONInto(&dispatch.Identity, c.Dispatch.Identity); err != nil {
		return out, errors.New("protected GitHub identity snapshot unavailable")
	}
	if err := cloneJSONInto(&dispatch.Grant, c.Dispatch.Grant); err != nil {
		return out, errors.New("protected GitHub grant snapshot unavailable")
	}
	if err := cloneJSONInto(&dispatch.Policy, c.Dispatch.Policy); err != nil {
		return out, errors.New("protected GitHub policy snapshot unavailable")
	}
	if err := cloneJSONInto(&dispatch.Revocation, c.Dispatch.Revocation); err != nil {
		return out, errors.New("protected GitHub revocation snapshot unavailable")
	}
	if err := cloneJSONInto(&dispatch.Current, c.Dispatch.Current); err != nil {
		return out, errors.New("protected GitHub grant context unavailable")
	}
	var reconciliation cli.TrackerReconciliationInputs
	if err := cloneJSONInto(&reconciliation, c.Reconciliation); err != nil {
		return out, errors.New("protected GitHub reconciliation snapshot unavailable")
	}
	if dispatch.Operation.OperationID != c.OperationID || dispatch.Operation.OperationDigest != c.OperationDigest || dispatch.Identity.Provider != "github" || dispatch.Operation.Authority.Scope != dispatch.Identity || dispatch.Operation.Authority.OperationDigest != c.OperationDigest {
		return out, errors.New("protected GitHub operation identity or digest mismatch")
	}
	if _, diagnostics := connector.ValidateOperation(dispatch.Operation, dispatch.Capability, dispatch.Identity); len(diagnostics) != 0 {
		return out, errors.New("protected GitHub operation failed canonical validation")
	}
	grant := dispatch.Grant
	if grant.OperationID != c.OperationID || grant.OperationDigest != c.OperationDigest || grant.Operation != dispatch.Operation.OperationKind || grant.Scope != dispatch.Identity || grant.ID != dispatch.Operation.Authority.Reference {
		return out, errors.New("protected GitHub grant does not bind exact operation and identity")
	}
	if c.Dispatch.Owner == "" || reconciliation.Operation.OperationID != c.OperationID || reconciliation.Operation.OperationDigest != c.OperationDigest || reconciliation.Identity != dispatch.Identity {
		return out, errors.New("protected GitHub dispatch or reconciliation route mismatch")
	}
	dispatch.Writer = c.Writer
	dispatch.Activation = c.Activation
	dispatch.RouteScope = c.RouteScope
	dispatch.VerifyEvidence = c.EvidenceVerifier
	dispatchSnapshot := dispatch
	reconciliationSnapshot := reconciliation
	out.TrackerDispatch = func(ctx context.Context, id string) (cli.TrackerDispatchInputs, error) {
		if ctx == nil || id != c.OperationID {
			return cli.TrackerDispatchInputs{}, errors.New("operation is outside protected GitHub route")
		}
		copy := dispatchSnapshot
		if cloneJSONInto(&copy.Operation, dispatchSnapshot.Operation) != nil || cloneJSONInto(&copy.Capability, dispatchSnapshot.Capability) != nil || cloneJSONInto(&copy.Identity, dispatchSnapshot.Identity) != nil || cloneJSONInto(&copy.Grant, dispatchSnapshot.Grant) != nil || cloneJSONInto(&copy.Policy, dispatchSnapshot.Policy) != nil || cloneJSONInto(&copy.Revocation, dispatchSnapshot.Revocation) != nil || cloneJSONInto(&copy.Current, dispatchSnapshot.Current) != nil {
			return cli.TrackerDispatchInputs{}, errors.New("protected GitHub route snapshot unavailable")
		}
		return copy, nil
	}
	out.TrackerReconciliation = func(ctx context.Context, id string) (cli.TrackerReconciliationInputs, error) {
		if ctx == nil || id != c.OperationID {
			return cli.TrackerReconciliationInputs{}, errors.New("operation is outside protected GitHub readback route")
		}
		var copy cli.TrackerReconciliationInputs
		if cloneJSONInto(&copy, reconciliationSnapshot) != nil {
			return cli.TrackerReconciliationInputs{}, errors.New("protected GitHub readback snapshot unavailable")
		}
		return copy, nil
	}
	out.WritebackEvidenceVerifier = func(op contracts.SyncOperation, g writeback.Grant) (writeback.EvidenceVerifier, error) {
		if op.OperationID != c.OperationID || op.OperationDigest != c.OperationDigest || g.OperationID != c.OperationID || g.OperationDigest != c.OperationDigest || g.ID != grant.ID || g.Scope != dispatch.Identity {
			return nil, errors.New("evidence verifier requested for a different GitHub operation")
		}
		return c.EvidenceVerifier, nil
	}
	return out, nil
}

func validDigest(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}
func nilLike(v any) bool {
	if v == nil {
		return true
	}
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return r.IsNil()
	}
	return false
}
func cloneJSONInto[T any](dst *T, src T) error {
	b, err := json.Marshal(src)
	if err != nil {
		return err
	}
	var clone T
	if err := json.Unmarshal(b, &clone); err != nil {
		return err
	}
	*dst = clone
	return nil
}
