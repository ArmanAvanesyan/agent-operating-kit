package contracts

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
)

type RemoteIdentity struct {
	Provider    string `json:"provider"`
	TenantID    string `json:"tenantId"`
	ContainerID string `json:"containerId"`
	RemoteID    string `json:"remoteId"`
	ItemKind    string `json:"itemKind"`
}
type AssetRef struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}
type PlanStepRef struct {
	PlanRef Reference `json:"planRef"`
	StepID  string    `json:"stepId"`
}
type ExternalAuthorityRef struct {
	Reference       string         `json:"reference"`
	Scope           RemoteIdentity `json:"scope"`
	Operation       string         `json:"operation"`
	OperationDigest string         `json:"operationDigest"`
}
type EvidenceRef struct {
	Asset      AssetRef `json:"asset"`
	Purpose    string   `json:"purpose"`
	ObservedAt string   `json:"observedAt"`
}
type FieldMapping struct {
	CanonicalField  string `json:"canonicalField"`
	ProviderFieldID string `json:"providerFieldId"`
	Owner           string `json:"owner"`
	Supported       bool   `json:"supported"`
}

type WorkItemBinding struct {
	DocumentHeader
	Identity               RemoteIdentity `json:"identity"`
	DisplayKey             string         `json:"displayKey,omitempty"`
	URL                    string         `json:"url,omitempty"`
	Packet                 AssetRef       `json:"packet"`
	ObservedSourceRevision string         `json:"observedSourceRevision"`
	MappingVersion         string         `json:"mappingVersion"`
	FieldMappings          []FieldMapping `json:"fieldMappings"`
}
type HostTaskBinding struct {
	DocumentHeader
	WorkItemRef      Reference    `json:"workItemRef"`
	AssignmentRef    Reference    `json:"assignmentRef"`
	Host             string       `json:"host"`
	HostVersion      string       `json:"hostVersion"`
	SessionID        string       `json:"sessionId"`
	ThreadID         string       `json:"threadId,omitempty"`
	NativeTaskID     string       `json:"nativeTaskId,omitempty"`
	AttemptID        string       `json:"attemptId"`
	AssignmentSHA256 string       `json:"assignmentSha256"`
	PlanStepRef      *PlanStepRef `json:"planStepRef,omitempty"`
}
type RunRecord struct {
	DocumentHeader
	HostTaskBindingRef  Reference     `json:"hostTaskBindingRef"`
	Runtime             string        `json:"runtime"`
	RuntimeVersion      string        `json:"runtimeVersion"`
	RuntimeRunID        string        `json:"runtimeRunId"`
	AttemptID           string        `json:"attemptId"`
	InputRevision       string        `json:"inputRevision"`
	ExecutionProfileRef Reference     `json:"executionProfileRef"`
	AssignmentSHA256    string        `json:"assignmentSha256"`
	LeaseID             *string       `json:"leaseId,omitempty"`
	Fence               *json.Number  `json:"fence,omitempty"`
	PolicyRefs          []Reference   `json:"policyRefs"`
	CheckpointRefs      []EvidenceRef `json:"checkpointRefs"`
	OutputRefs          []EvidenceRef `json:"outputRefs"`
	ObservedAt          string        `json:"observedAt"`
	Disposition         string        `json:"disposition"`
}
type OperationCapability struct {
	Operation        string `json:"operation"`
	Supported        bool   `json:"supported"`
	ConditionalWrite bool   `json:"conditionalWrite"`
	Idempotent       bool   `json:"idempotent"`
	Concurrency      string `json:"concurrency"`
}
type ConnectorCapability struct {
	DocumentHeader
	Provider       string                `json:"provider"`
	AdapterID      string                `json:"adapterId"`
	AdapterVersion string                `json:"adapterVersion"`
	Scope          RemoteIdentity        `json:"scope"`
	Operations     []OperationCapability `json:"operations"`
}
type FieldChange struct {
	ProviderFieldID string          `json:"providerFieldId"`
	Value           json.RawMessage `json:"value"`
}
type SyncChange struct {
	Fields           []FieldChange   `json:"fields,omitempty"`
	TransitionID     string          `json:"transitionId,omitempty"`
	Comment          *AssetRef       `json:"comment,omitempty"`
	RelationType     string          `json:"relationType,omitempty"`
	RelationIdentity *RemoteIdentity `json:"relationIdentity,omitempty"`
}
type SyncOperation struct {
	DocumentHeader
	WorkItemBindingRef     Reference            `json:"workItemBindingRef"`
	ConnectorCapabilityRef Reference            `json:"connectorCapabilityRef"`
	OperationID            string               `json:"operationId"`
	OperationKind          string               `json:"operationKind"`
	ExpectedRevision       string               `json:"expectedRevision"`
	Change                 SyncChange           `json:"change"`
	Authority              ExternalAuthorityRef `json:"authority"`
	OperationDigest        string               `json:"operationDigest"`
}
type SyncReceipt struct {
	DocumentHeader
	SyncOperationRef Reference     `json:"syncOperationRef"`
	OperationID      string        `json:"operationId"`
	OperationDigest  string        `json:"operationDigest"`
	ReceiptID        string        `json:"receiptId"`
	ObservedAt       string        `json:"observedAt"`
	Disposition      string        `json:"disposition"`
	DispatchEvidence []EvidenceRef `json:"dispatchEvidence"`
	ReadbackEvidence []EvidenceRef `json:"readbackEvidence"`
}

// OperationDigest hashes the canonical, typed operation identity. Field values
// remain RawMessage so their numeric lexemes are never rounded through float64.
func OperationDigest(binding Reference, identity RemoteIdentity, capability Reference, operationKind, expectedRevision string, change SyncChange) (string, error) {
	b, err := CanonicalOperationBytes(binding, identity, capability, operationKind, expectedRevision, change)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// CanonicalOperationBytes returns the stable digest preimage for an operation.
func CanonicalOperationBytes(binding Reference, identity RemoteIdentity, capability Reference, operationKind, expectedRevision string, change SyncChange) ([]byte, error) {
	if operationKind == "update-fields" {
		change.Fields = append([]FieldChange(nil), change.Fields...)
		sort.Slice(change.Fields, func(i, j int) bool { return change.Fields[i].ProviderFieldID < change.Fields[j].ProviderFieldID })
		for i, f := range change.Fields {
			if f.ProviderFieldID == "" || len(f.Value) == 0 {
				return nil, fmt.Errorf("field change requires providerFieldId and value")
			}
			if i > 0 && change.Fields[i-1].ProviderFieldID == f.ProviderFieldID {
				return nil, fmt.Errorf("duplicate provider field id %q", f.ProviderFieldID)
			}
		}
	}
	canonical := struct {
		Binding          Reference      `json:"binding"`
		Identity         RemoteIdentity `json:"identity"`
		Capability       Reference      `json:"capability"`
		OperationKind    string         `json:"operationKind"`
		ExpectedRevision string         `json:"expectedRevision"`
		Change           SyncChange     `json:"change"`
	}{binding, identity, capability, operationKind, expectedRevision, change}
	return json.Marshal(canonical)
}
