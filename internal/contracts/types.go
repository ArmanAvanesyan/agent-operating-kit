package contracts

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
)

// DocumentHeader is the common top-level shape of every operational resource.
// It is intentionally separate from Resource so typed documents are ordinary
// JSON values and can be re-encoded without consulting the original bytes.
type DocumentHeader struct {
	APIVersion string              `json:"apiVersion"`
	Kind       string              `json:"kind"`
	Metadata   OperationalMetadata `json:"metadata"`
}

// OperationalMetadata is the typed metadata shape used by operational docs.
// A pointer to Tags distinguishes an absent member from an explicit empty list.
type OperationalMetadata struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Version     string    `json:"version"`
	Description string    `json:"description"`
	Tags        *[]string `json:"tags,omitempty"`
}

// ParameterValues retain the raw JSON representation of schema-defined
// parameter values, including null and exact numeric lexemes.
type ParameterValues map[string]json.RawMessage

type ParameterDefinition struct {
	Type    string             `json:"type"`
	Enum    *[]json.RawMessage `json:"enum,omitempty"`
	Default json.RawMessage    `json:"default,omitempty"`
}

type Agent struct {
	DocumentHeader
	Identity           AgentIdentity     `json:"identity"`
	Roles              AgentRoles        `json:"roles"`
	Instructions       AgentInstructions `json:"instructions"`
	Execution          AgentExecution    `json:"execution"`
	Capabilities       AgentCapabilities `json:"capabilities"`
	Permissions        Permissions       `json:"permissions"`
	Environment        AgentEnvironment  `json:"environment"`
	Outputs            AgentOutputs      `json:"outputs"`
	Verification       AgentVerification `json:"verification"`
	Delegation         AgentDelegation   `json:"delegation"`
	InvocationContract Reference         `json:"invocationContract"`
	ContextPolicy      Reference         `json:"contextPolicy"`
	LifecyclePolicy    Reference         `json:"lifecyclePolicy"`
	EvaluationContract Reference         `json:"evaluationContract"`
	MigrationContract  Reference         `json:"migrationContract"`
}

type AgentIdentity struct {
	Purpose       string              `json:"purpose"`
	Callsign      *string             `json:"callsign,omitempty"`
	Communication *AgentCommunication `json:"communication,omitempty"`
	Boundaries    []string            `json:"boundaries"`
}
type AgentCommunication struct {
	Style    string `json:"style"`
	Evidence string `json:"evidence"`
}
type AgentRoles struct {
	Primary    string    `json:"primary"`
	Supporting *[]string `json:"supporting,omitempty"`
}
type AgentInstructions struct {
	Sources    []InstructionSource             `json:"sources"`
	Parameters *map[string]ParameterDefinition `json:"parameters,omitempty"`
}
type InstructionSource struct {
	Path string `json:"path"`
}
type AgentExecution struct {
	DefaultProfile   Reference    `json:"defaultProfile"`
	AllowedProfiles  []Reference  `json:"allowedProfiles"`
	FallbackPolicy   string       `json:"fallbackPolicy"`
	FallbackProfiles *[]Reference `json:"fallbackProfiles,omitempty"`
}
type AgentCapabilities struct {
	Tools   []CapabilityReference `json:"tools"`
	Skills  []CapabilityReference `json:"skills"`
	MCPs    []CapabilityReference `json:"mcps"`
	Plugins []CapabilityReference `json:"plugins"`
}
type CapabilityReference struct {
	Kind     string    `json:"kind"`
	ID       string    `json:"id"`
	Version  string    `json:"version"`
	Required bool      `json:"required"`
	Tools    *[]string `json:"tools,omitempty"`
}
type AgentEnvironment struct {
	Profile           Reference `json:"profile"`
	ReadinessRequired bool      `json:"readinessRequired"`
}
type AgentOutputs struct {
	Contract         Reference        `json:"contract"`
	AllowedContracts []Reference      `json:"allowedContracts"`
	Parameters       *ParameterValues `json:"parameters,omitempty"`
}
type AgentVerification struct {
	Required []string `json:"required"`
}
type AgentDelegation struct {
	Enabled       bool         `json:"enabled"`
	AllowedAgents *[]Reference `json:"allowedAgents,omitempty"`
	MaxDepth      *json.Number `json:"maxDepth,omitempty"`
}

type Permissions struct {
	Filesystem        FilesystemPermissions `json:"filesystem"`
	Network           NetworkPermissions    `json:"network"`
	Secrets           ModePolicy            `json:"secrets"`
	ExternalMutations ModePolicy            `json:"externalMutations"`
	Actions           ActionPermissions     `json:"actions"`
}
type FilesystemPermissions struct {
	Read  []string `json:"read"`
	Write []string `json:"write"`
}
type NetworkPermissions struct {
	Mode  string    `json:"mode"`
	Hosts *[]string `json:"hosts,omitempty"`
}
type ModePolicy struct {
	Mode string `json:"mode"`
}
type ActionPermissions struct {
	Default string       `json:"default"`
	Rules   []ActionRule `json:"rules"`
}
type ActionRule struct {
	Action           string `json:"action"`
	Resource         string `json:"resource"`
	Decision         string `json:"decision"`
	EnforcementOwner string `json:"enforcementOwner"`
}

type ExecutionProfile struct {
	DocumentHeader
	Client                 ClientSettings              `json:"client"`
	Model                  ModelSettings               `json:"model"`
	Compatibility          CompatibilitySettings       `json:"compatibility"`
	PermissionRestrictions *Permissions                `json:"permissionRestrictions,omitempty"`
	Limits                 *ExecutionLimits            `json:"limits,omitempty"`
	Environment            ProfileEnvironment          `json:"environment"`
	Extensions             *map[string]json.RawMessage `json:"extensions,omitempty"`
}
type ClientSettings struct {
	Adapter           string  `json:"adapter"`
	Interface         string  `json:"interface"`
	VersionConstraint *string `json:"versionConstraint,omitempty"`
}
type ModelSettings struct {
	Selector   string              `json:"selector"`
	Provider   *string             `json:"provider,omitempty"`
	Reasoning  ReasoningSettings   `json:"reasoning"`
	Generation *GenerationSettings `json:"generation,omitempty"`
}
type ReasoningSettings struct {
	Mode         string       `json:"mode"`
	Effort       *string      `json:"effort,omitempty"`
	BudgetTokens *json.Number `json:"budgetTokens,omitempty"`
}
type GenerationSettings struct {
	MaxOutputTokens *json.Number `json:"maxOutputTokens,omitempty"`
	Temperature     *json.Number `json:"temperature,omitempty"`
	TopP            *json.Number `json:"topP,omitempty"`
}
type CompatibilitySettings struct {
	UnsupportedSettings string `json:"unsupportedSettings"`
}
type ExecutionLimits struct {
	TimeoutSeconds *json.Number `json:"timeoutSeconds,omitempty"`
	MaxTurns       *json.Number `json:"maxTurns,omitempty"`
	MaxToolCalls   *json.Number `json:"maxToolCalls,omitempty"`
	MaxCost        *MaximumCost `json:"maxCost,omitempty"`
}
type MaximumCost struct {
	Amount   json.Number `json:"amount"`
	Currency string      `json:"currency"`
}
type ProfileEnvironment struct {
	Profile Reference `json:"profile"`
}

type AgentAssignment struct {
	APIVersion             string              `json:"apiVersion"`
	Kind                   string              `json:"kind"`
	Agent                  Reference           `json:"agent"`
	TaskPacket             string              `json:"taskPacket"`
	ExecutionProfile       Reference           `json:"executionProfile"`
	Inputs                 ParameterValues     `json:"inputs"`
	Outputs                AssignmentOutputs   `json:"outputs"`
	PermissionRestrictions *Permissions        `json:"permissionRestrictions,omitempty"`
	Metadata               OperationalMetadata `json:"metadata"`
}
type AssignmentOutputs struct {
	Contract   Reference        `json:"contract"`
	Parameters *ParameterValues `json:"parameters,omitempty"`
}

type CapabilityRegistry struct {
	DocumentHeader
	Entries []CapabilityEntry `json:"entries"`
}
type CapabilityEntry struct {
	ID            string            `json:"id"`
	Version       string            `json:"version"`
	Type          string            `json:"type"`
	Binding       CapabilityBinding `json:"binding"`
	Dependencies  []Reference       `json:"dependencies"`
	Installation  string            `json:"installation"`
	Authorization string            `json:"authorization"`
}
type CapabilityBinding struct {
	Type            string          `json:"type"`
	Adapter         *string         `json:"adapter,omitempty"`
	Name            *string         `json:"name,omitempty"`
	ExecutableRef   *string         `json:"executableRef,omitempty"`
	Arguments       *[]string       `json:"arguments,omitempty"`
	SourcePath      *string         `json:"sourcePath,omitempty"`
	ConnectionRef   *string         `json:"connectionRef,omitempty"`
	Tools           *[]string       `json:"tools,omitempty"`
	Components      *[]string       `json:"components,omitempty"`
	CredentialRefs  *[]string       `json:"credentialRefs,omitempty"`
	SchemaPath      *string         `json:"schemaPath,omitempty"`
	ParameterSchema json.RawMessage `json:"parameterSchema,omitempty"`
}

type InvocationContract struct {
	DocumentHeader
	ExecutionMode string              `json:"executionMode"`
	Selection     InvocationSelection `json:"selection"`
	InputSchema   string              `json:"inputSchema"`
	Examples      []InvocationExample `json:"examples"`
}
type InvocationSelection struct {
	When        []string `json:"when"`
	ExcludeWhen []string `json:"excludeWhen"`
	Automatic   bool     `json:"automatic"`
}
type InvocationExample struct {
	Description       string                     `json:"description"`
	Inputs            map[string]json.RawMessage `json:"inputs"`
	ExpectedSelection bool                       `json:"expectedSelection"`
}

type ContextPolicy struct {
	DocumentHeader
	RepositoryScope   []string      `json:"repositoryScope"`
	RequiredDocuments []string      `json:"requiredDocuments"`
	Conversation      string        `json:"conversation"`
	RetrievalSources  []string      `json:"retrievalSources"`
	UntrustedContent  string        `json:"untrustedContent"`
	Limits            ContextLimits `json:"limits"`
	Retention         string        `json:"retention"`
	RedactionRequired bool          `json:"redactionRequired"`
}
type ContextLimits struct {
	MaxInputTokens json.Number `json:"maxInputTokens"`
	Overflow       string      `json:"overflow"`
}

type OutputContract struct {
	DocumentHeader
	Deliverables []Deliverable    `json:"deliverables"`
	Validation   OutputValidation `json:"validation"`
	ResultSchema string           `json:"resultSchema"`
}
type Deliverable struct {
	ID               string    `json:"id"`
	Format           string    `json:"format"`
	Template         *string   `json:"template,omitempty"`
	Schema           *string   `json:"schema,omitempty"`
	Destination      string    `json:"destination"`
	RequiredSections *[]string `json:"requiredSections,omitempty"`
	Required         bool      `json:"required"`
}
type OutputValidation struct {
	EvidenceReferences         string `json:"evidenceReferences"`
	MissingRequiredDeliverable string `json:"missingRequiredDeliverable"`
}

type EnvironmentProfile struct {
	DocumentHeader
	Roots               EnvironmentRoots         `json:"roots"`
	Requirements        []EnvironmentRequirement `json:"requirements"`
	Isolation           string                   `json:"isolation"`
	Network             string                   `json:"network"`
	CredentialRefs      []string                 `json:"credentialRefs"`
	ReadinessChecks     []string                 `json:"readinessChecks"`
	MutationOnDiscovery bool                     `json:"mutationOnDiscovery"`
}
type EnvironmentRoots struct {
	Workspace string `json:"workspace"`
	Reports   string `json:"reports"`
}
type EnvironmentRequirement struct {
	Ref       string `json:"ref"`
	Required  bool   `json:"required"`
	Discovery string `json:"discovery"`
}

// DecodeTyped converts a previously Parse-validated resource into its concrete
// per-kind type. The second decode is strict as a defense in depth boundary.
func DecodeTyped(resource Resource) (any, error) {
	parsed, _, err := Parse(resource.Document)
	if err != nil {
		return nil, err
	}
	verified := parsed.(Resource)
	if !reflect.DeepEqual(verified.Envelope, resource.Envelope) {
		return nil, errors.New("resource envelope does not match document")
	}
	switch resource.Kind {
	case "Agent":
		return decodeResource[Agent](resource.Document)
	case "ExecutionProfile":
		return decodeResource[ExecutionProfile](resource.Document)
	case "AgentAssignment":
		return decodeResource[AgentAssignment](resource.Document)
	case "CapabilityRegistry":
		return decodeResource[CapabilityRegistry](resource.Document)
	case "InvocationContract":
		return decodeResource[InvocationContract](resource.Document)
	case "ContextPolicy":
		return decodeResource[ContextPolicy](resource.Document)
	case "OutputContract":
		return decodeResource[OutputContract](resource.Document)
	case "EnvironmentProfile":
		return decodeResource[EnvironmentProfile](resource.Document)
	case "LifecyclePolicy":
		return decodeResource[LifecyclePolicy](resource.Document)
	case "EvaluationContract":
		return decodeResource[EvaluationContract](resource.Document)
	case "MigrationContract":
		return decodeResource[MigrationContract](resource.Document)
	case "GoalContract":
		return decodeResource[GoalContract](resource.Document)
	case "PlanDefinition":
		return decodeResource[PlanDefinition](resource.Document)
	case "PlanRevision":
		return decodeResource[PlanRevision](resource.Document)
	case "DecisionContract":
		return decodeResource[DecisionContract](resource.Document)
	case "DecisionResult":
		return decodeResource[DecisionResult](resource.Document)
	case "OptimizationExperiment":
		return decodeResource[OptimizationExperiment](resource.Document)
	case "SDLCTraceability":
		return decodeResource[SDLCTraceability](resource.Document)
	case "PlanningEvidence":
		return decodeResource[PlanningEvidence](resource.Document)
	case "WorkItemBinding":
		return decodeResource[WorkItemBinding](resource.Document)
	case "HostTaskBinding":
		return decodeResource[HostTaskBinding](resource.Document)
	case "RunRecord":
		return decodeResource[RunRecord](resource.Document)
	case "ConnectorCapability":
		return decodeResource[ConnectorCapability](resource.Document)
	case "SyncOperation":
		return decodeResource[SyncOperation](resource.Document)
	case "SyncReceipt":
		return decodeResource[SyncReceipt](resource.Document)
	default:
		return nil, fmt.Errorf("unsupported resource kind %q", resource.Kind)
	}
}

func decodeResource[T any](document json.RawMessage) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(document))
	dec.UseNumber()
	dec.DisallowUnknownFields()
	var value T
	if err := dec.Decode(&value); err != nil {
		return nil, err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, errors.New("multiple JSON values")
		}
		return nil, fmt.Errorf("trailing JSON data: %w", err)
	}
	return value, nil
}
