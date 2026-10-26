// Package nativev1 describes observation evidence, never a native writer grant.
package nativev1

import (
	"encoding/json"
	api "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/v1alpha1"
)

const Version = "workkit-native/v1"
const AdapterVersion = "1.0.0-observation"

type Scope struct {
	Host             string `json:"host"`
	HostVersion      string `json:"hostVersion"`
	Protocol         string `json:"protocol"`
	Deployment       string `json:"deployment"`
	Origin           string `json:"origin"`
	Principal        string `json:"principal"`
	Workspace        string `json:"workspace"`
	SessionID        string `json:"sessionId"`
	Owner            string `json:"owner"`
	AssignmentSHA256 string `json:"assignmentSha256"`
	ProfileSHA256    string `json:"profileSha256"`
	MappingSHA256    string `json:"mappingSha256"`
	Mode             string `json:"mode"`
	AttemptID        string `json:"attemptId,omitempty"`
	Fence            string `json:"fence,omitempty"`
}
type Step struct {
	Key         string   `json:"key"`
	Description string   `json:"description"`
	DependsOn   []string `json:"dependsOn"`
}
type Assignment struct {
	Version string `json:"version"`
	Kind    string `json:"kind"`
	Steps   []Step `json:"steps"`
}
type Handle struct {
	Version      string `json:"version"`
	Kind         string `json:"kind"`
	Scope        Scope  `json:"scope"`
	ObservedAt   string `json:"observedAt"`
	ExpiresAt    string `json:"expiresAt"`
	SchemaSHA256 string `json:"schemaSha256"`
}
type Capability struct {
	Operation string `json:"operation"`
	Status    string `json:"status"`
	Category  string `json:"category"`
	Source    string `json:"source"`
}
type Host struct {
	Host              string       `json:"host"`
	Capabilities      []Capability `json:"capabilities"`
	LiveQualification string       `json:"liveQualification"`
}

// Event is an owner-scoped protocol fragment. Data retains original provider bytes.
// Native IDs may only be adopted from successful paired results, never a call.
type Event struct {
	ID         string          `json:"id"`
	Sequence   int64           `json:"sequence"`
	ObservedAt string          `json:"observedAt"`
	Scope      Scope           `json:"scope"`
	Type       string          `json:"type"`
	CallID     string          `json:"callId,omitempty"`
	Data       json.RawMessage `json:"data"`
}
type Evidence struct {
	Disposition string  `json:"disposition,omitempty"`
	Version     string  `json:"version"`
	Kind        string  `json:"kind"`
	Scope       Scope   `json:"scope"`
	ObservedAt  string  `json:"observedAt"`
	Complete    bool    `json:"complete"`
	Events      []Event `json:"events"`
}
type Entry struct {
	SourceEventID string          `json:"sourceEventId"`
	Category      string          `json:"category"`
	NativeID      string          `json:"nativeId,omitempty"`
	StepKey       string          `json:"stepKey,omitempty"`
	State         string          `json:"state"`
	Value         json.RawMessage `json:"value"`
}
type Report struct {
	AdapterVersion       string            `json:"adapterVersion"`
	ImplementationSHA256 string            `json:"implementationSha256"`
	Operations           []OperationDraft  `json:"operations"`
	Version              string            `json:"version"`
	Kind                 string            `json:"kind"`
	Scope                Scope             `json:"scope"`
	AsOf                 string            `json:"asOf"`
	ObservedAt           string            `json:"observedAt"`
	Status               string            `json:"status"`
	Complete             bool              `json:"complete"`
	CurrentAuthority     string            `json:"currentAuthority"`
	LiveQualification    string            `json:"liveQualification"`
	Inputs               map[string]string `json:"inputs"`
	Steps                []Step            `json:"steps"`
	Entries              []Entry           `json:"entries"`
	Diagnostics          []api.Diagnostic  `json:"diagnostics"`
	Assets               []api.Asset       `json:"assets"`
	AuthorizationGranted bool              `json:"authorizationGranted"`
	ExecutionReady       bool              `json:"executionReady"`
	MigrationActivated   bool              `json:"migrationActivated"`
	ProductionActivated  bool              `json:"productionActivated"`
}

// ReadPolicy is operator-supplied and must also be approved by protected custody.
type ReadPolicy struct {
	Version      string `json:"version"`
	Kind         string `json:"kind"`
	Scope        Scope  `json:"scope"`
	SchemaSHA256 string `json:"schemaSha256"`
	NotBefore    string `json:"notBefore"`
	ExpiresAt    string `json:"expiresAt"`
}
type Enrollment struct {
	Version              string `json:"version"`
	Kind                 string `json:"kind"`
	Purpose              string `json:"purpose"`
	HandleSHA256         string `json:"handleSha256"`
	PolicySHA256         string `json:"policySha256"`
	ImplementationSHA256 string `json:"implementationSha256"`
	IssuedAt             string `json:"issuedAt"`
	ExpiresAt            string `json:"expiresAt"`
	SecretReference      string `json:"secretReference"`
	Issuer               string `json:"issuer"`
	Signature            string `json:"signature"`
}

// ProgressLink is an explicit reviewed correlation, never fuzzy matching or an
// acceptance receipt. Entry ordinals are bound to the exact observation digest.
type ProgressLink struct {
	Version           string         `json:"version"`
	Kind              string         `json:"kind"`
	ObservationSHA256 string         `json:"observationSha256"`
	ScopeSHA256       string         `json:"scopeSha256"`
	BindingSHA256     string         `json:"bindingSha256"`
	MappingSHA256     string         `json:"mappingSha256"`
	Target            api.Identity   `json:"target"`
	Rules             []ProgressRule `json:"rules"`
}
type ProgressRule struct {
	EntryIndex    int             `json:"entryIndex"`
	ExpectedState string          `json:"expectedState"`
	Field         string          `json:"field"`
	Value         json.RawMessage `json:"value"`
}

// OperationRequest describes intent only; it never authorizes a native call.
type OperationRequest struct {
	Version        string `json:"version"`
	Kind           string `json:"kind"`
	Operation      string `json:"operation"`
	StepKey        string `json:"stepKey"`
	TargetNativeID string `json:"targetNativeId,omitempty"`
}
type OperationDraft struct {
	LogicalID string           `json:"logicalId"`
	Request   OperationRequest `json:"request"`
	Status    string           `json:"status"`
	Reason    string           `json:"reason"`
}
