// Package connector defines the offline boundary for future work-system adapters.
// It contains no provider implementation or authorization resolver.
package connector

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
)

type Disposition string

const (
	Supported   Disposition = "supported"
	Unsupported Disposition = "unsupported"
	Conflict    Disposition = "conflict"
	Unknown     Disposition = "unknown"
)

type Result[T any] struct {
	Disposition Disposition
	Value       T
	Diagnostics []contracts.Diagnostic
}

type ReadResult struct {
	Identity contracts.RemoteIdentity
	Revision string
	Captured []byte
}

// Reader methods may perform I/O in a later implementation. P1 supplies no
// production Reader; context is always the cancellation boundary.
type Reader interface {
	Describe() contracts.ConnectorCapability
	Discover(context.Context, contracts.RemoteIdentity) (Result[[]contracts.RemoteIdentity], error)
	Read(context.Context, contracts.RemoteIdentity) (Result[ReadResult], error)
}

type PortableItem struct {
	Identity contracts.RemoteIdentity
	Revision string
	Fields   map[string]any
}

type Mapper interface {
	Normalize(ReadResult) (Result[PortableItem], error)
	PlanUpdate(current PortableItem, desired map[string]any, capability contracts.ConnectorCapability) (Result[contracts.SyncOperation], error)
}

// AuthorizationContext is intentionally opaque outside this package. A
// SyncOperation's authority declaration cannot be converted into this value.
type AuthorizationContext struct {
	reference string
	scope     contracts.RemoteIdentity
	operation string
	digest    string
	verified  *authorizationLease
}

// ValidatedOperation can only be produced by ValidateOperation.
type ValidatedOperation struct {
	operation contracts.SyncOperation
	identity  contracts.RemoteIdentity
}

func (v ValidatedOperation) Operation() contracts.SyncOperation { return cloneOperation(v.operation) }

type Writer interface {
	Apply(context.Context, ValidatedOperation, AuthorizationContext) (Result[contracts.SyncReceipt], error)
	Reconcile(context.Context, ValidatedOperation, contracts.SyncReceipt) (Result[contracts.SyncReceipt], error)
}

// ValidateOperation checks capability, exact scope, declared authority and
// canonical digest before creating the value accepted by Writer.Apply.
func ValidateOperation(op contracts.SyncOperation, capability contracts.ConnectorCapability, identity contracts.RemoteIdentity) (ValidatedOperation, []contracts.Diagnostic) {
	ds := make([]contracts.Diagnostic, 0)
	add := func(code, field, message string) {
		ds = append(ds, contracts.Diagnostic{Code: code, Field: field, Severity: "error", Message: message})
	}
	if err := validateTyped(op); err != nil {
		add("connector.operation.schema", "operation", err.Error())
	}
	if err := validateTyped(capability); err != nil {
		add("connector.capability.schema", "capability", err.Error())
	}
	if err := validatePayload(op); err != nil {
		add("connector.operation.payload", "change", err.Error())
	}
	if !validIdentity(identity) {
		add("connector.identity.required", "identity", "operation scope requires complete nonempty provider identity fields")
	}
	if op.Metadata.ID == "" || op.Metadata.Version == "" || op.OperationID == "" || op.ExpectedRevision == "" {
		add("connector.operation.required", "operation", "operation metadata, operationId and expectedRevision must be nonempty")
	}
	if op.WorkItemBindingRef.Kind != "WorkItemBinding" || op.WorkItemBindingRef.ID == "" || op.WorkItemBindingRef.Version == "" {
		add("connector.operation.binding-reference", "workItemBindingRef", "operation requires an exact work-item binding reference")
	}
	if capability.Metadata.ID == "" || capability.Metadata.Version == "" || capability.Kind != "ConnectorCapability" {
		add("connector.capability.required", "capability", "capability requires an exact ConnectorCapability identity")
	}
	if !sameIdentity(capability.Scope, identity) || capability.Provider != identity.Provider {
		add("connector.scope", "identity", "operation identity is outside connector capability scope")
	}
	if op.ConnectorCapabilityRef.Kind != capability.Kind || op.ConnectorCapabilityRef.ID != capability.Metadata.ID || op.ConnectorCapabilityRef.Version != capability.Metadata.Version {
		add("connector.capability.reference", "connectorCapabilityRef", "operation does not pin the supplied exact connector capability")
	}
	var supported *contracts.OperationCapability
	seenOperations := make(map[string]bool, len(capability.Operations))
	for i := range capability.Operations {
		entry := capability.Operations[i]
		name := entry.Operation
		if name == "" || seenOperations[name] {
			add("connector.capability.operation-duplicate", "operations", "capability operation names must be nonempty and unique")
		}
		seenOperations[name] = true
		if !entry.Supported && (entry.ConditionalWrite || entry.Idempotent || entry.Concurrency != "none") {
			add("connector.capability.unsupported-guarantee", "operations", "unsupported operation cannot claim guarantees")
		}
		if entry.Concurrency != "none" && entry.Concurrency != "conditional" && entry.Concurrency != "serialized" {
			add("connector.capability.concurrency", "operations", "concurrency must be none, conditional, or serialized")
		}
		if entry.ConditionalWrite && entry.Concurrency == "none" {
			add("connector.capability.guarantee-conflict", "operations", "conditional write support requires conditional or serialized concurrency")
		}
		if entry.Concurrency == "conditional" && !entry.ConditionalWrite {
			add("connector.capability.guarantee-conflict", "operations", "conditional concurrency requires conditional write support")
		}
		if name == op.OperationKind {
			supported = &capability.Operations[i]
		}
	}
	if supported == nil || !supported.Supported {
		add("connector.operation.unsupported", "operationKind", "connector does not support this operation")
	} else if (op.OperationKind == "update-fields" || op.OperationKind == "transition") && !supported.ConditionalWrite {
		add("connector.operation.conditional-write", "operationKind", "operation lacks conditional write support")
	}
	if op.Authority.Reference == "" || op.Authority.Operation != op.OperationKind || op.Authority.OperationDigest != op.OperationDigest || !sameIdentity(op.Authority.Scope, identity) {
		add("connector.authority.declaration", "authority", "authority declaration does not cover this operation, scope and digest")
	}
	want, err := contracts.OperationDigest(op.WorkItemBindingRef, identity, op.ConnectorCapabilityRef, op.OperationKind, op.ExpectedRevision, op.Change)
	if err != nil {
		add("connector.operation.invalid", "change", err.Error())
	} else if want != op.OperationDigest {
		add("connector.operation.digest", "operationDigest", "operation digest does not match its canonical contents")
	}
	if len(ds) != 0 {
		return ValidatedOperation{}, ds
	}
	return ValidatedOperation{operation: cloneOperation(op), identity: identity}, ds
}

func validateTyped(value any) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	parsed, _, err := contracts.Parse(b)
	if err != nil {
		return err
	}
	_, err = contracts.DecodeTyped(parsed.(contracts.Resource))
	return err
}

var sha256Hex = regexp.MustCompile(`^[0-9a-f]{64}$`)

func validatePayload(op contracts.SyncOperation) error {
	c := op.Change
	switch op.OperationKind {
	case "update-fields":
		if len(c.Fields) == 0 || c.TransitionID != "" || c.Comment != nil || c.RelationType != "" || c.RelationIdentity != nil {
			return fmt.Errorf("update-fields requires only a nonempty fields list")
		}
		seen := map[string]bool{}
		for _, field := range c.Fields {
			if field.ProviderFieldID == "" || seen[field.ProviderFieldID] || !scalarOrScalarArray(field.Value) {
				return fmt.Errorf("field changes require unique providerFieldId values and scalar or scalar-array values")
			}
			seen[field.ProviderFieldID] = true
		}
	case "transition":
		if c.TransitionID == "" || len(c.Fields) != 0 || c.Comment != nil || c.RelationType != "" || c.RelationIdentity != nil {
			return fmt.Errorf("transition requires only a nonempty transitionId")
		}
	case "comment":
		if c.Comment == nil || c.Comment.Path == "" || !sha256Hex.MatchString(c.Comment.SHA256) || len(c.Fields) != 0 || c.TransitionID != "" || c.RelationType != "" || c.RelationIdentity != nil {
			return fmt.Errorf("comment requires only an asset reference with a lowercase SHA-256")
		}
	case "link":
		if c.RelationType == "" || c.RelationIdentity == nil || !validIdentity(*c.RelationIdentity) || len(c.Fields) != 0 || c.TransitionID != "" || c.Comment != nil {
			return fmt.Errorf("link requires only a relation type and complete remote identity")
		}
	default:
		return fmt.Errorf("unsupported operation kind %q", op.OperationKind)
	}
	return nil
}

func scalarOrScalarArray(raw json.RawMessage) bool {
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	var value any
	if len(raw) == 0 || dec.Decode(&value) != nil {
		return false
	}
	scalar := func(v any) bool {
		switch v.(type) {
		case nil, bool, string, json.Number:
			return true
		default:
			return false
		}
	}
	if scalar(value) {
		return true
	}
	values, ok := value.([]any)
	if !ok {
		return false
	}
	for _, item := range values {
		if !scalar(item) {
			return false
		}
	}
	return true
}

func validIdentity(id contracts.RemoteIdentity) bool {
	return id.Provider != "" && id.TenantID != "" && id.ContainerID != "" && id.RemoteID != "" && id.ItemKind != ""
}

func cloneOperation(op contracts.SyncOperation) contracts.SyncOperation {
	op.Change.Fields = append([]contracts.FieldChange(nil), op.Change.Fields...)
	for i := range op.Change.Fields {
		op.Change.Fields[i].Value = append(json.RawMessage(nil), op.Change.Fields[i].Value...)
	}
	if op.Change.Comment != nil {
		comment := *op.Change.Comment
		op.Change.Comment = &comment
	}
	if op.Change.RelationIdentity != nil {
		relation := *op.Change.RelationIdentity
		op.Change.RelationIdentity = &relation
	}
	return op
}

// Apply runs all pure gates before dispatch and checks the independent context.
func Apply(ctx context.Context, writer Writer, op contracts.SyncOperation, capability contracts.ConnectorCapability, identity contracts.RemoteIdentity, auth AuthorizationContext) (Result[contracts.SyncReceipt], error) {
	validated, diagnostics := ValidateOperation(op, capability, identity)
	if len(diagnostics) != 0 {
		return Result[contracts.SyncReceipt]{Disposition: Unsupported, Diagnostics: diagnostics}, nil
	}
	if auth.reference == "" || auth.reference != op.Authority.Reference || auth.operation != op.OperationKind || auth.digest != op.OperationDigest || !sameIdentity(auth.scope, identity) {
		return Result[contracts.SyncReceipt]{Disposition: Unsupported, Diagnostics: []contracts.Diagnostic{{Code: "connector.authorization.missing-or-mismatch", Field: "authorizationContext", Severity: "error", Message: "independent authorization context does not match the validated operation"}}}, nil
	}
	if err := ctx.Err(); err != nil {
		return Result[contracts.SyncReceipt]{Disposition: Unknown, Diagnostics: []contracts.Diagnostic{{Code: "connector.context.cancelled", Field: "context", Severity: "error", Message: err.Error()}}}, err
	}
	if auth.verified != nil && !authorizationCurrent(auth, validated, time.Now()) {
		return Result[contracts.SyncReceipt]{Disposition: Unsupported}, fmt.Errorf("authorization expired, revoked, or outside its exact scope")
	}
	return writer.Apply(ctx, validated, auth)
}

func Reconcile(ctx context.Context, writer Writer, op contracts.SyncOperation, capability contracts.ConnectorCapability, identity contracts.RemoteIdentity, receipt contracts.SyncReceipt) (Result[contracts.SyncReceipt], error) {
	validated, diagnostics := ValidateOperation(op, capability, identity)
	if len(diagnostics) != 0 {
		return Result[contracts.SyncReceipt]{Disposition: Unsupported, Diagnostics: diagnostics}, nil
	}
	if err := validated.validateReceipt(receipt); err != nil {
		return Result[contracts.SyncReceipt]{Disposition: Conflict, Diagnostics: []contracts.Diagnostic{{Code: "connector.receipt.correlation", Field: "receipt", Severity: "error", Message: err.Error()}}}, nil
	}
	if err := ctx.Err(); err != nil {
		return Result[contracts.SyncReceipt]{Disposition: Unknown, Diagnostics: []contracts.Diagnostic{{Code: "connector.context.cancelled", Field: "context", Severity: "error", Message: err.Error()}}}, err
	}
	return writer.Reconcile(ctx, validated, receipt)
}

func sameIdentity(a, b contracts.RemoteIdentity) bool {
	return a.Provider == b.Provider && a.TenantID == b.TenantID && a.ContainerID == b.ContainerID && a.RemoteID == b.RemoteID && a.ItemKind == b.ItemKind
}

func (v ValidatedOperation) validateReceipt(r contracts.SyncReceipt) error {
	opRef := contracts.Reference{Kind: v.operation.Kind, ID: v.operation.Metadata.ID, Version: v.operation.Metadata.Version}
	if r.SyncOperationRef != opRef || r.OperationID != v.operation.OperationID || r.OperationDigest != v.operation.OperationDigest {
		return fmt.Errorf("receipt does not correlate with validated operation")
	}
	if r.ReceiptID == "" {
		return fmt.Errorf("receiptId must be nonempty")
	}
	switch r.Disposition {
	case "applied", "not-applied", "conflict", "rejected", "unknown":
	default:
		return fmt.Errorf("invalid receipt disposition %q", r.Disposition)
	}
	return nil
}
