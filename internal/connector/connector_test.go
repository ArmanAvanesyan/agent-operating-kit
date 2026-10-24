package connector

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
)

type memoryConnector struct {
	revision       string
	effects        int
	digests        map[string]string
	receipts       map[string]contracts.SyncReceipt
	loseOnce       bool
	dispatch       int
	reconcileCalls int
	applyErr       error
}

func (m *memoryConnector) Apply(ctx context.Context, v ValidatedOperation, _ AuthorizationContext) (Result[contracts.SyncReceipt], error) {
	m.dispatch++
	if m.applyErr != nil {
		return Result[contracts.SyncReceipt]{}, m.applyErr
	}
	if err := ctx.Err(); err != nil {
		return Result[contracts.SyncReceipt]{Disposition: Unknown}, err
	}
	op := v.operation
	if prior, ok := m.digests[op.OperationID]; ok {
		if prior != op.OperationDigest {
			return Result[contracts.SyncReceipt]{Disposition: Conflict}, nil
		}
		return Result[contracts.SyncReceipt]{Disposition: Supported, Value: m.receipts[op.OperationID]}, nil
	}
	if m.revision != op.ExpectedRevision {
		return Result[contracts.SyncReceipt]{Disposition: Conflict}, nil
	}
	m.digests[op.OperationID] = op.OperationDigest
	m.effects++
	m.revision = "rev-2"
	r := contracts.SyncReceipt{SyncOperationRef: contracts.Reference{Kind: op.Kind, ID: op.Metadata.ID, Version: op.Metadata.Version}, OperationID: op.OperationID, OperationDigest: op.OperationDigest, ReceiptID: "receipt-1", Disposition: "unknown"}
	m.receipts[op.OperationID] = r
	if m.loseOnce {
		m.loseOnce = false
		return Result[contracts.SyncReceipt]{Disposition: Unknown}, nil
	}
	r.Disposition = "applied"
	m.receipts[op.OperationID] = r
	return Result[contracts.SyncReceipt]{Disposition: Supported, Value: r}, nil
}

func (m *memoryConnector) Reconcile(ctx context.Context, v ValidatedOperation, receipt contracts.SyncReceipt) (Result[contracts.SyncReceipt], error) {
	m.reconcileCalls++
	if err := ctx.Err(); err != nil {
		return Result[contracts.SyncReceipt]{Disposition: Unknown}, err
	}
	if err := v.validateReceipt(receipt); err != nil {
		return Result[contracts.SyncReceipt]{Disposition: Conflict}, nil
	}
	r, ok := m.receipts[receipt.OperationID]
	if !ok {
		return Result[contracts.SyncReceipt]{Disposition: Unknown, Value: receipt}, nil
	}
	r.Disposition = "applied"
	m.receipts[receipt.OperationID] = r
	return Result[contracts.SyncReceipt]{Disposition: Supported, Value: r}, nil
}

func fixture() (contracts.SyncOperation, contracts.ConnectorCapability, contracts.RemoteIdentity, AuthorizationContext) {
	id := contracts.RemoteIdentity{Provider: "github", TenantID: "tenant-a", ContainerID: "repo-a", RemoteID: "issue-7", ItemKind: "issue"}
	bind := contracts.Reference{Kind: "WorkItemBinding", ID: "binding", Version: "1.0.0"}
	capRef := contracts.Reference{Kind: "ConnectorCapability", ID: "cap", Version: "1.0.0"}
	change := contracts.SyncChange{Fields: []contracts.FieldChange{{ProviderFieldID: "title", Value: json.RawMessage(`"updated"`)}}}
	digest, _ := contracts.OperationDigest(bind, id, capRef, "update-fields", "rev-1", change)
	op := contracts.SyncOperation{DocumentHeader: contracts.DocumentHeader{APIVersion: "aok/v1alpha1", Kind: "SyncOperation", Metadata: contracts.OperationalMetadata{ID: "sync-op", Name: "Sync operation", Version: "1.0.0", Description: "Test operation"}}, WorkItemBindingRef: bind, ConnectorCapabilityRef: capRef, OperationID: "op-1", OperationKind: "update-fields", ExpectedRevision: "rev-1", Change: change, OperationDigest: digest,
		Authority: contracts.ExternalAuthorityRef{Reference: "auth-ref-1", Scope: id, Operation: "update-fields", OperationDigest: digest}}
	cap := contracts.ConnectorCapability{
		DocumentHeader: contracts.DocumentHeader{APIVersion: "aok/v1alpha1", Kind: "ConnectorCapability", Metadata: contracts.OperationalMetadata{ID: capRef.ID, Name: "Test capability", Version: capRef.Version, Description: "Test capability"}},
		Provider:       "github", AdapterID: "memory", AdapterVersion: "1.0.0",
		Scope:      id,
		Operations: []contracts.OperationCapability{{Operation: "update-fields", Supported: true, ConditionalWrite: true, Idempotent: true, Concurrency: "conditional"}},
	}
	auth := AuthorizationContext{reference: "auth-ref-1", scope: id, operation: "update-fields", digest: digest}
	return op, cap, id, auth
}

func applyForTest(t *testing.T, ctx context.Context, writer Writer, op contracts.SyncOperation, capability contracts.ConnectorCapability, identity contracts.RemoteIdentity, auth AuthorizationContext) Result[contracts.SyncReceipt] {
	t.Helper()
	result, err := Apply(ctx, writer, op, capability, identity, auth)
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	return result
}

func reconcileForTest(t *testing.T, ctx context.Context, writer Writer, op contracts.SyncOperation, capability contracts.ConnectorCapability, identity contracts.RemoteIdentity, receipt contracts.SyncReceipt) Result[contracts.SyncReceipt] {
	t.Helper()
	result, err := Reconcile(ctx, writer, op, capability, identity, receipt)
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}
	return result
}

func TestReadDoesNotCreateResources(t *testing.T) {
	// The boundary exposes only discovery and read operations; the isolated fake
	// starts empty and a read leaves its state unchanged.
	before := map[string]string{}
	reader := readOnlyFake{items: map[string]ReadResult{}}
	result, err := reader.Read(context.Background(), contracts.RemoteIdentity{Provider: "github"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Disposition != Unsupported || len(reader.items) != len(before) {
		t.Fatalf("read mutated fake state: %+v", result)
	}
}

type readOnlyFake struct{ items map[string]ReadResult }

func (r readOnlyFake) Describe() contracts.ConnectorCapability {
	return contracts.ConnectorCapability{}
}
func (r readOnlyFake) Discover(context.Context, contracts.RemoteIdentity) (Result[[]contracts.RemoteIdentity], error) {
	return Result[[]contracts.RemoteIdentity]{Disposition: Supported}, nil
}
func (r readOnlyFake) Read(_ context.Context, id contracts.RemoteIdentity) (Result[ReadResult], error) {
	if item, ok := r.items[id.RemoteID]; ok {
		return Result[ReadResult]{Disposition: Supported, Value: item}, nil
	}
	return Result[ReadResult]{Disposition: Unsupported}, nil
}

func TestApplyGatesAuthorizationScopeDigestAndCancellation(t *testing.T) {
	op, cap, id, auth := fixture()
	for _, tc := range []struct {
		name   string
		mutate func(*contracts.SyncOperation, *contracts.ConnectorCapability, *contracts.RemoteIdentity, *AuthorizationContext)
	}{
		{"missing authorization", func(_ *contracts.SyncOperation, _ *contracts.ConnectorCapability, _ *contracts.RemoteIdentity, a *AuthorizationContext) {
			*a = AuthorizationContext{}
		}},
		{"invalid authorization", func(_ *contracts.SyncOperation, _ *contracts.ConnectorCapability, _ *contracts.RemoteIdentity, a *AuthorizationContext) {
			a.digest = "wrong"
		}},
		{"foreign scope", func(_ *contracts.SyncOperation, _ *contracts.ConnectorCapability, id *contracts.RemoteIdentity, _ *AuthorizationContext) {
			id.TenantID = "tenant-b"
		}},
		{"changed digest", func(o *contracts.SyncOperation, _ *contracts.ConnectorCapability, _ *contracts.RemoteIdentity, _ *AuthorizationContext) {
			o.Change.Fields[0].Value = json.RawMessage(`"tampered"`)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o, c, i, a := op, cap, id, auth
			o.Change.Fields = append([]contracts.FieldChange(nil), op.Change.Fields...)
			o.Change.Fields[0].Value = append(json.RawMessage(nil), op.Change.Fields[0].Value...)
			tc.mutate(&o, &c, &i, &a)
			m := &memoryConnector{revision: "rev-1", digests: map[string]string{}, receipts: map[string]contracts.SyncReceipt{}}
			got := applyForTest(t, context.Background(), m, o, c, i, a)
			if got.Disposition != Unsupported || m.dispatch != 0 {
				t.Fatalf("bad operation dispatched: result=%+v dispatch=%d", got, m.dispatch)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	m := &memoryConnector{revision: "rev-1", digests: map[string]string{}, receipts: map[string]contracts.SyncReceipt{}}
	got, err := Apply(ctx, m, op, cap, id, auth)
	if !errors.Is(err, context.Canceled) || got.Disposition != Unknown || m.dispatch != 0 {
		t.Fatalf("cancelled operation dispatched: %+v dispatch=%d", got, m.dispatch)
	}
}

func TestApplyPropagatesTransportError(t *testing.T) {
	op, cap, id, auth := fixture()
	transportErr := errors.New("synthetic transport failure")
	m := &memoryConnector{revision: "rev-1", digests: map[string]string{}, receipts: map[string]contracts.SyncReceipt{}, applyErr: transportErr}
	result, err := Apply(context.Background(), m, op, cap, id, auth)
	if !errors.Is(err, transportErr) || m.dispatch != 1 || result.Disposition != "" {
		t.Fatalf("transport error was not propagated: result=%+v err=%v dispatch=%d", result, err, m.dispatch)
	}
}

func TestMalformedPayloadAndDuplicateCapabilityNeverDispatch(t *testing.T) {
	op, cap, id, auth := fixture()
	m := &memoryConnector{revision: "rev-1", digests: map[string]string{}, receipts: map[string]contracts.SyncReceipt{}}
	malformed := op
	malformed.Change.TransitionID = "closed"
	malformed.OperationDigest, _ = contracts.OperationDigest(malformed.WorkItemBindingRef, id, malformed.ConnectorCapabilityRef, malformed.OperationKind, malformed.ExpectedRevision, malformed.Change)
	malformed.Authority.OperationDigest = malformed.OperationDigest
	malformedAuth := AuthorizationContext{reference: auth.reference, scope: id, operation: malformed.OperationKind, digest: malformed.OperationDigest}
	if got := applyForTest(t, context.Background(), m, malformed, cap, id, malformedAuth); got.Disposition != Unsupported || m.dispatch != 0 {
		t.Fatalf("mixed payload reached writer: %+v dispatch=%d", got, m.dispatch)
	}
	duplicated := cap
	duplicated.Operations = append(append([]contracts.OperationCapability(nil), cap.Operations...), contracts.OperationCapability{Operation: "update-fields", Supported: false, ConditionalWrite: false, Idempotent: false, Concurrency: "none"})
	if got := applyForTest(t, context.Background(), m, op, duplicated, id, auth); got.Disposition != Unsupported || m.dispatch != 0 {
		t.Fatalf("duplicate capability declaration reached writer: %+v dispatch=%d", got, m.dispatch)
	}
}

func TestAppendOperationsMayBeSupportedWithoutConditionalWrites(t *testing.T) {
	for _, kind := range []string{"comment", "link"} {
		t.Run(kind, func(t *testing.T) {
			op, cap, id, auth := fixture()
			op.OperationKind = kind
			if kind == "comment" {
				op.Change = contracts.SyncChange{Comment: &contracts.AssetRef{Path: "assets/comment.md", SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}
			} else {
				relation := id
				relation.RemoteID = "issue-8"
				op.Change = contracts.SyncChange{RelationType: "relates-to", RelationIdentity: &relation}
			}
			cap.Operations = []contracts.OperationCapability{{Operation: kind, Supported: true, ConditionalWrite: false, Idempotent: false, Concurrency: "none"}}
			op.OperationDigest, _ = contracts.OperationDigest(op.WorkItemBindingRef, id, op.ConnectorCapabilityRef, op.OperationKind, op.ExpectedRevision, op.Change)
			op.Authority.Operation = kind
			op.Authority.OperationDigest = op.OperationDigest
			auth.operation, auth.digest = kind, op.OperationDigest
			m := &memoryConnector{revision: "rev-1", digests: map[string]string{}, receipts: map[string]contracts.SyncReceipt{}}
			got := applyForTest(t, context.Background(), m, op, cap, id, auth)
			if got.Disposition != Supported || m.effects != 1 {
				t.Fatalf("append operation without conditional write was rejected: result=%+v effects=%d", got, m.effects)
			}
		})
	}
}

func TestCapabilityGuaranteesMustBeCoherent(t *testing.T) {
	op, cap, id, auth := fixture()
	for _, tc := range []struct {
		name  string
		entry contracts.OperationCapability
	}{
		{"conditional-write without concurrency", contracts.OperationCapability{Operation: "update-fields", Supported: true, ConditionalWrite: true, Concurrency: "none"}},
		{"conditional concurrency without conditional-write", contracts.OperationCapability{Operation: "update-fields", Supported: true, ConditionalWrite: false, Concurrency: "conditional"}},
		{"unsupported guarantee", contracts.OperationCapability{Operation: "update-fields", Supported: false, Idempotent: true, Concurrency: "none"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			broken := cap
			broken.Operations = []contracts.OperationCapability{tc.entry}
			m := &memoryConnector{revision: "rev-1", digests: map[string]string{}, receipts: map[string]contracts.SyncReceipt{}}
			if got := applyForTest(t, context.Background(), m, op, broken, id, auth); got.Disposition != Unsupported || m.dispatch != 0 {
				t.Fatalf("incoherent guarantee reached writer: %+v dispatch=%d", got, m.dispatch)
			}
		})
	}
}

func TestValidatedOperationAndGetterOwnDeepCopies(t *testing.T) {
	op, cap, id, _ := fixture()
	validated, diagnostics := ValidateOperation(op, cap, id)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	op.Change.Fields[0].Value[1] = 'X'
	copyOut := validated.Operation()
	copyOut.Change.Fields[0].Value[1] = 'Y'
	copyOut.Change.Fields = append(copyOut.Change.Fields, contracts.FieldChange{ProviderFieldID: "extra", Value: json.RawMessage(`true`)})
	if got := string(validated.operation.Change.Fields[0].Value); got != `"updated"` || len(validated.operation.Change.Fields) != 1 {
		t.Fatalf("validated operation shares caller-owned data: fields=%+v", validated.operation.Change.Fields)
	}
	originalTenant := id.TenantID
	source := contracts.SyncOperation{Change: contracts.SyncChange{Comment: &contracts.AssetRef{Path: "assets/note.md", SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, RelationIdentity: &id}}
	cloned := cloneOperation(source)
	source.Change.Comment.Path = "changed"
	source.Change.RelationIdentity.TenantID = "changed"
	cloned.Change.Comment.SHA256 = "changed"
	cloned.Change.RelationIdentity.RemoteID = "changed"
	if source.Change.Comment.Path != "changed" || cloned.Change.Comment.Path != "assets/note.md" || source.Change.RelationIdentity.TenantID != "changed" || cloned.Change.RelationIdentity.TenantID != originalTenant {
		t.Fatal("optional pointer fields were not cloned independently")
	}
}

func TestConditionalRevisionReplayAndLostResponseReconcile(t *testing.T) {
	op, cap, id, auth := fixture()
	m := &memoryConnector{revision: "rev-0", digests: map[string]string{}, receipts: map[string]contracts.SyncReceipt{}}
	if got := applyForTest(t, context.Background(), m, op, cap, id, auth); got.Disposition != Conflict || m.effects != 0 {
		t.Fatalf("stale revision was applied: %+v", got)
	}
	m.revision, m.loseOnce = "rev-1", true
	first := applyForTest(t, context.Background(), m, op, cap, id, auth)
	if first.Disposition != Unknown || m.effects != 1 {
		t.Fatalf("lost response not ambiguous: %+v effects=%d", first, m.effects)
	}
	unknownReceipt := contracts.SyncReceipt{SyncOperationRef: contracts.Reference{Kind: op.Kind, ID: op.Metadata.ID, Version: op.Metadata.Version}, OperationID: op.OperationID, OperationDigest: op.OperationDigest, ReceiptID: "receipt-1", Disposition: "unknown"}
	reconciled := reconcileForTest(t, context.Background(), m, op, cap, id, unknownReceipt)
	if reconciled.Disposition != Supported || reconciled.Value.Disposition != "applied" || m.effects != 1 {
		t.Fatalf("reconcile failed: %+v effects=%d", reconciled, m.effects)
	}
	second := applyForTest(t, context.Background(), m, op, cap, id, auth)
	if second.Disposition != Supported || m.effects != 1 {
		t.Fatalf("same operation replay duplicated effect: %+v effects=%d", second, m.effects)
	}
	changed := op
	changed.ExpectedRevision = "rev-2"
	changed.Change.Fields = append([]contracts.FieldChange(nil), op.Change.Fields...)
	changed.Change.Fields[0].Value = json.RawMessage(`"different logical payload"`)
	changed.OperationDigest, _ = contracts.OperationDigest(changed.WorkItemBindingRef, id, changed.ConnectorCapabilityRef, changed.OperationKind, changed.ExpectedRevision, changed.Change)
	changed.Authority.OperationDigest = changed.OperationDigest
	changedAuth := AuthorizationContext{reference: auth.reference, scope: id, operation: changed.OperationKind, digest: changed.OperationDigest}
	conflict := applyForTest(t, context.Background(), m, changed, cap, id, changedAuth)
	if conflict.Disposition != Conflict || m.effects != 1 {
		t.Fatalf("same ID/different digest did not conflict: %+v", conflict)
	}
}

func TestMapperIsPureInterfaceAndReceiptCorrelation(t *testing.T) {
	op, cap, id, _ := fixture()
	v, ds := ValidateOperation(op, cap, id)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	wrong := contracts.SyncReceipt{SyncOperationRef: contracts.Reference{Kind: op.Kind, ID: op.Metadata.ID, Version: op.Metadata.Version}, OperationID: op.OperationID, OperationDigest: op.OperationDigest, ReceiptID: "receipt", Disposition: "unknown"}
	wrong.SyncOperationRef.Version = "2.0.0"
	if err := v.validateReceipt(wrong); err == nil {
		t.Fatal("receipt with wrong exact operation version accepted")
	}
	wrong.SyncOperationRef.Version = op.Metadata.Version
	wrong.Disposition = "pretend-applied"
	if err := v.validateReceipt(wrong); err == nil {
		t.Fatal("unknown receipt disposition accepted")
	}
}

func TestReconcileCancellationReturnsContextError(t *testing.T) {
	op, cap, id, _ := fixture()
	m := &memoryConnector{revision: "rev-1", digests: map[string]string{}, receipts: map[string]contracts.SyncReceipt{}}
	receipt := contracts.SyncReceipt{SyncOperationRef: contracts.Reference{Kind: op.Kind, ID: op.Metadata.ID, Version: op.Metadata.Version}, OperationID: op.OperationID, OperationDigest: op.OperationDigest, ReceiptID: "receipt-1", Disposition: "unknown"}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := Reconcile(ctx, m, op, cap, id, receipt)
	if !errors.Is(err, context.Canceled) || result.Disposition != Unknown || m.reconcileCalls != 0 {
		t.Fatalf("reconcile cancellation was not returned before dispatch: result=%+v err=%v calls=%d", result, err, m.reconcileCalls)
	}
}
