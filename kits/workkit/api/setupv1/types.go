// Package setupv1 defines offline setup evidence. None of these records grants
// provider mutation, migration, runtime or production authority.
package setupv1

import (
	"encoding/json"
	r "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/readv1"
	api "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/v1alpha1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/readsource"
)

const Version = "workkit-setup/v1"

type Scope struct {
	Provider         string `json:"provider"`
	Origin           string `json:"origin"`
	BasePath         string `json:"basePath"`
	TenantID         string `json:"tenantId"`
	ContainerID      string `json:"containerId"`
	APIVersion       string `json:"apiVersion"`
	DeploymentSHA256 string `json:"deploymentSha256"`
	Principal        string `json:"principal"`
}
type Provenance struct {
	Asset   api.Asset         `json:"asset"`
	Pointer string            `json:"pointer"`
	Fields  map[string]string `json:"fields"`
}
type Entity struct {
	Kind           string     `json:"kind"`
	ID             string     `json:"id"`
	ParentID       string     `json:"parentId"`
	Name           string     `json:"name"`
	Type           string     `json:"type"`
	NativeType     string     `json:"nativeType"`
	Options        []Option   `json:"options"`
	Required       bool       `json:"required"`
	FromStateID    string     `json:"fromStateId"`
	ToStateID      string     `json:"toStateId"`
	RequiredFields []string   `json:"requiredFields"`
	Provenance     Provenance `json:"provenance"`
}
type Option struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type Coverage struct {
	Category        string    `json:"category"`
	Status          string    `json:"status"`
	Complete        bool      `json:"complete"`
	Reason          string    `json:"reason"`
	Evidence        api.Asset `json:"evidence"`
	CompletePointer string    `json:"completePointer"`
}
type Observation struct {
	Version             string              `json:"version"`
	Kind                string              `json:"kind"`
	ConnectionRef       api.Ref             `json:"connectionRef"`
	ConfigurationSHA256 string              `json:"configurationSha256"`
	CaptureSHA256       string              `json:"captureSha256"`
	Scope               Scope               `json:"scope"`
	ObservedAt          string              `json:"observedAt"`
	AdapterVersion      string              `json:"adapterVersion"`
	Transport           r.TransportIdentity `json:"transport"`
	Revision            string              `json:"revision"`
	RevisionKind        string              `json:"revisionKind"`
	QualificationOnly   bool                `json:"qualificationOnly"`
	Coverage            []Coverage          `json:"coverage"`
	Entities            []Entity            `json:"entities"`
	Assets              []api.Asset         `json:"assets"`
	Diagnostics         []api.Diagnostic    `json:"diagnostics"`
}
type Change struct {
	ID        string          `json:"id"`
	Operation string          `json:"operation"`
	ParentID  string          `json:"parentId"`
	LogicalID string          `json:"logicalId"`
	Value     json.RawMessage `json:"value"`
}
type Report struct {
	Version              string                          `json:"version"`
	Kind                 string                          `json:"kind"`
	Status               string                          `json:"status"`
	AsOf                 string                          `json:"asOf"`
	Inputs               map[string]string               `json:"inputs"`
	Checks               []api.WorkBindingSpecChecksItem `json:"checks"`
	Changes              []Change                        `json:"changes"`
	Diagnostics          []api.Diagnostic                `json:"diagnostics"`
	AuthorizationGranted bool                            `json:"authorizationGranted"`
	ExecutionReady       bool                            `json:"executionReady"`
	MigrationActivated   bool                            `json:"migrationActivated"`
	ProductionActivated  bool                            `json:"productionActivated"`
	LiveQualification    string                          `json:"liveQualification"`
}
type MappingContext struct {
	Version                string   `json:"version"`
	Kind                   string   `json:"kind"`
	MappingSHA256          string   `json:"mappingSha256"`
	SourceSHA256           string   `json:"sourceSha256"`
	DestinationSHA256      string   `json:"destinationSha256"`
	RequiredFields         []string `json:"requiredFields"`
	SourceContainerID      string   `json:"sourceContainerId"`
	DestinationContainerID string   `json:"destinationContainerId"`
}
type ProposedValue struct {
	ItemID         string               `json:"itemId"`
	Canonical      string               `json:"canonical"`
	SourceFieldID  string               `json:"sourceFieldId"`
	TargetFieldID  string               `json:"targetFieldId"`
	State          string               `json:"state"`
	Value          json.RawMessage      `json:"value"`
	Provenance     []api.Asset          `json:"provenance"`
	SourcePointers []readsource.Pointer `json:"sourcePointers"`
}
type Preview struct {
	Report Report          `json:"report"`
	Values []ProposedValue `json:"values"`
}
type ViewDraft struct {
	Report             Report   `json:"report"`
	DefinitionSHA256   string   `json:"definitionSha256"`
	ParentID           string   `json:"parentId"`
	ExistingID         string   `json:"existingId"`
	Differences        []string `json:"differences"`
	ManualInstructions []string `json:"manualInstructions"`
}
type Policy struct {
	Version       string     `json:"version"`
	Kind          string     `json:"kind"`
	NotBefore     string     `json:"notBefore"`
	ExpiresAt     string     `json:"expiresAt"`
	MaxAgeSeconds int        `json:"maxAgeSeconds"`
	Issuers       []r.Issuer `json:"issuers"`
	RevokedIDs    []string   `json:"revokedIds"`
	AllowedScopes []Scope    `json:"allowedScopes"`
}
type Verification struct {
	Version              string            `json:"version"`
	Kind                 string            `json:"kind"`
	Purpose              string            `json:"purpose"`
	ID                   string            `json:"id"`
	Issuer               string            `json:"issuer"`
	Scope                Scope             `json:"scope"`
	Inputs               map[string]string `json:"inputs"`
	ImplementationSHA256 string            `json:"implementationSha256"`
	PolicySHA256         string            `json:"policySha256"`
	OutputPath           string            `json:"outputPath"`
	WorkspaceSHA256      string            `json:"workspaceSha256"`
	IssuedAt             string            `json:"issuedAt"`
	ExpiresAt            string            `json:"expiresAt"`
	OperationalMetadata  bool              `json:"operationalMetadata"`
	Signature            string            `json:"signature"`
	CandidateSHA256      string            `json:"candidateSha256"`
	WorkspacePath        string            `json:"workspacePath"`
	ConfigurationRoot    string            `json:"configurationRoot"`
}
type BindingReceipt struct {
	Report                  Report          `json:"report"`
	Binding                 api.WorkBinding `json:"binding"`
	Selected                bool            `json:"selected"`
	PreviousWorkspaceSHA256 string          `json:"previousWorkspaceSha256"`
	WorkspaceSHA256         string          `json:"workspaceSha256"`
	VerificationSHA256      string          `json:"verificationSha256"`
	CandidateSHA256         string          `json:"candidateSha256"`
}

type ViewInspection struct {
	Report     Report             `json:"report"`
	Definition api.ViewDefinition `json:"definition"`
	NativeID   string             `json:"nativeId"`
	SourceURLs []string           `json:"sourceUrls"`
}
