package github

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/cli"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/connector"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/operations"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/writeback"
)

type serviceWitness struct{ epoch operations.Epoch }

func (w serviceWitness) Current(context.Context, string) (operations.Epoch, error) {
	return w.epoch, nil
}
func (serviceWitness) Consume(context.Context, operations.Consumption) error { return nil }

type serviceWitnessVerifier struct{}

func (serviceWitnessVerifier) VerifyWitness(context.Context, operations.AuthorityWitness, string, string, []byte, time.Time) error {
	return nil
}

type serviceWriter struct{}

func (serviceWriter) Apply(context.Context, connector.ValidatedOperation, connector.AuthorizationContext) (connector.Result[contracts.SyncReceipt], error) {
	return connector.Result[contracts.SyncReceipt]{}, errors.New("fixture writer must not be invoked by assembly")
}
func (serviceWriter) Reconcile(context.Context, connector.ValidatedOperation, contracts.SyncReceipt) (connector.Result[contracts.SyncReceipt], error) {
	return connector.Result[contracts.SyncReceipt]{}, errors.New("fixture writer must not be invoked by assembly")
}

func serviceFixture(t *testing.T) ServicesConfig {
	t.Helper()
	now := time.Now().UTC()
	identity := contracts.RemoteIdentity{Provider: "github", TenantID: "org", ContainerID: "repository", RemoteID: "42", ItemKind: "issue"}
	binding := contracts.Reference{Kind: "WorkItemBinding", ID: "binding-1", Version: "1.0.0"}
	capRef := contracts.Reference{Kind: "ConnectorCapability", ID: "github-cap", Version: "1.0.0"}
	change := contracts.SyncChange{Comment: &contracts.AssetRef{Path: "evidence/comment.md", SHA256: strings.Repeat("a", 64)}}
	digest, err := contracts.OperationDigest(binding, identity, capRef, "comment", "revision-1", change)
	if err != nil {
		t.Fatal(err)
	}
	op := contracts.SyncOperation{DocumentHeader: contracts.DocumentHeader{APIVersion: "aok/v1alpha1", Kind: "SyncOperation", Metadata: contracts.OperationalMetadata{ID: "op-document", Name: "Issue comment", Version: "1.0.0", Description: "fixture"}}, WorkItemBindingRef: binding, ConnectorCapabilityRef: capRef, OperationID: "operation-42-comment", OperationKind: "comment", ExpectedRevision: "revision-1", Change: change, OperationDigest: digest, Authority: contracts.ExternalAuthorityRef{Reference: "grant-fixture", Scope: identity, Operation: "comment", OperationDigest: digest}}
	capability := contracts.ConnectorCapability{DocumentHeader: contracts.DocumentHeader{APIVersion: "aok/v1alpha1", Kind: "ConnectorCapability", Metadata: contracts.OperationalMetadata{ID: capRef.ID, Name: "GitHub issue comments", Version: capRef.Version, Description: "fixture"}}, Provider: "github", AdapterID: "github-native", AdapterVersion: "1.0.0", Scope: identity, Operations: []contracts.OperationCapability{{Operation: "comment", Supported: true, Idempotent: false, Concurrency: "serialized"}}}
	grant := writeback.Grant{ID: "grant-fixture", Principal: "principal-fixture", AuthRef: "opaque-ref", ProviderOrigin: "https://api.github.com", Scope: identity, Operation: "comment", OperationID: op.OperationID, OperationDigest: digest}
	epoch := operations.Epoch{Authority: "external-authority", Holder: "github-runner", ScopeSHA256: strings.Repeat("b", 64), Generation: 1, ObservedAt: now.Add(-time.Second), ValidUntil: now.Add(time.Minute)}
	activation, err := operations.Bind(context.Background(), serviceWitness{epoch}, serviceWitnessVerifier{}, epoch.Authority, epoch.Holder, epoch.ScopeSHA256, []byte("protected-proof"), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	return ServicesConfig{OperationID: op.OperationID, OperationDigest: digest, Dispatch: cli.TrackerDispatchInputs{Operation: op, Capability: capability, Identity: identity, Grant: grant, Owner: "protected-runner"}, Reconciliation: cli.TrackerReconciliationInputs{Operation: op, Identity: identity}, Writer: serviceWriter{}, Activation: activation, RouteScope: epoch.ScopeSHA256, EvidenceVerifier: func(writeback.EvidenceDependency) error { return nil }}
}

func TestServicesBindsDispatchReconciliationAndEvidenceToExactOperation(t *testing.T) {
	cfg := serviceFixture(t)
	services, err := Services(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := services.TrackerDispatch(context.Background(), "other-operation"); err == nil {
		t.Fatal("task-selected different operation was admitted")
	}
	dispatch, err := services.TrackerDispatch(context.Background(), cfg.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if dispatch.Operation.OperationDigest != cfg.OperationDigest || dispatch.Writer == nil || dispatch.Activation != cfg.Activation || dispatch.RouteScope != cfg.RouteScope || dispatch.VerifyEvidence == nil {
		t.Fatalf("dispatch did not retain protected bindings: %+v", dispatch)
	}
	dispatch.Operation.Change.Comment.Path = "caller-mutated.md"
	second, err := services.TrackerDispatch(context.Background(), cfg.OperationID)
	if err != nil || second.Operation.Change.Comment.Path != "evidence/comment.md" {
		t.Fatalf("returned route mutation altered protected snapshot: %+v %v", second.Operation.Change, err)
	}
	if _, err := services.TrackerReconciliation(context.Background(), cfg.OperationID); err != nil {
		t.Fatal(err)
	}
	if _, err := services.TrackerReconciliation(context.Background(), "other-operation"); err == nil {
		t.Fatal("readback operation was not exact-bound")
	}
	verify, err := services.WritebackEvidenceVerifier(cfg.Dispatch.Operation, cfg.Dispatch.Grant)
	if err != nil || verify == nil {
		t.Fatalf("exact evidence verifier unavailable: %v", err)
	}
	wrongGrant := cfg.Dispatch.Grant
	wrongGrant.OperationDigest = strings.Repeat("c", 64)
	if _, err := services.WritebackEvidenceVerifier(cfg.Dispatch.Operation, wrongGrant); err == nil {
		t.Fatal("evidence verifier accepted a grant for another digest")
	}
	wrongOp := cfg.Dispatch.Operation
	wrongOp.OperationID = "other-operation"
	if _, err := services.WritebackEvidenceVerifier(wrongOp, cfg.Dispatch.Grant); err == nil {
		t.Fatal("evidence verifier accepted another operation")
	}
}

func TestServicesRejectsMissingQualificationAndMismatchedProtectedInputs(t *testing.T) {
	base := serviceFixture(t)
	tests := []struct {
		name   string
		mutate func(*ServicesConfig)
	}{
		{"missing activation", func(c *ServicesConfig) { c.Activation = nil }},
		{"missing evidence verifier", func(c *ServicesConfig) { c.EvidenceVerifier = nil }},
		{"wrong route scope", func(c *ServicesConfig) { c.RouteScope = strings.Repeat("d", 64) }},
		{"wrong operation id", func(c *ServicesConfig) { c.Dispatch.Operation.OperationID = "different" }},
		{"wrong operation digest", func(c *ServicesConfig) { c.Dispatch.Operation.OperationDigest = strings.Repeat("e", 64) }},
		{"wrong grant digest", func(c *ServicesConfig) { c.Dispatch.Grant.OperationDigest = strings.Repeat("f", 64) }},
		{"wrong readback identity", func(c *ServicesConfig) { c.Reconciliation.Identity.RemoteID = "43" }},
		{"missing pinned writer", func(c *ServicesConfig) { c.Writer = nil }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base
			tc.mutate(&cfg)
			if _, err := Services(cfg); err == nil {
				t.Fatal("mismatched or incomplete protected config accepted")
			}
		})
	}
}

func TestServicesSnapshotsTaskMutableOperationData(t *testing.T) {
	cfg := serviceFixture(t)
	services, err := Services(cfg)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Dispatch.Operation.Change.Comment.Path = "task-mutated.md"
	got, err := services.TrackerDispatch(context.Background(), cfg.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Operation.Change.Comment.Path != "evidence/comment.md" {
		t.Fatalf("protected snapshot changed after assembly: %q", got.Operation.Change.Comment.Path)
	}
}
