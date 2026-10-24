// Package hostbridge provides opt-in host execution; host completion is never AOK acceptance.
package hostbridge

import (
	"context"
	"encoding/json"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/modelcatalog"
)

const Version = "aok-host-bridge/v1"
const VersionV2 = "aok-host-bridge/v2"
const MaxFrameBytes = 4 << 20
const MaxCaptureBytes = 64 << 20

type Diagnostic struct {
	Code    string `json:"code"`
	Field   string `json:"field"`
	Message string `json:"message"`
}
type Report struct {
	Version              string       `json:"version"`
	Status               string       `json:"status"`
	Diagnostics          []Diagnostic `json:"diagnostics"`
	AuthorizationGranted bool         `json:"authorizationGranted"`
	ExecutionReady       bool         `json:"executionReady"`
	Accepted             bool         `json:"accepted"`
	LiveGate             string       `json:"liveGate"`
	RuntimeGate          string       `json:"runtimeGate"`
}
type Asset struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}
type Control struct {
	Name        string `json:"name"`
	Owner       string `json:"owner"`
	Evidence    Asset  `json:"evidence"`
	ObservedAt  string `json:"observedAt"`
	ExpiresAt   string `json:"expiresAt"`
	Workspace   string `json:"workspace"`
	HostVersion string `json:"hostVersion"`
}
type Policy struct {
	Permissions         contracts.Permissions      `json:"permissions"`
	Catalog             string                     `json:"catalog,omitempty"`
	CatalogPolicy       *Asset                     `json:"catalogPolicy,omitempty"`
	CatalogAsOf         string                     `json:"catalogAsOf,omitempty"`
	AccountScope        string                     `json:"accountScope"`
	ControlIssuers      map[string]string          `json:"controlIssuers"`
	TaskPlan            *Asset                     `json:"taskPlan,omitempty"`
	Version             string                     `json:"version"`
	HostDriverSHA256    string                     `json:"hostDriverSha256,omitempty"`
	HostProtocol        string                     `json:"hostProtocol,omitempty"`
	Host                string                     `json:"host"`
	Executable          string                     `json:"executable"`
	ExecutableSHA256    string                     `json:"executableSha256"`
	HostVersion         string                     `json:"hostVersion"`
	AuthMode            string                     `json:"authMode"`
	WorkspaceRoots      []string                   `json:"workspaceRoots"`
	Workspace           string                     `json:"workspace"`
	Models              []string                   `json:"models"`
	Tools               []string                   `json:"tools"`
	Environment         []string                   `json:"environment"`
	ConfigurationAssets []Asset                    `json:"configurationAssets"`
	Controls            []Control                  `json:"controls"`
	TimeoutSeconds      int                        `json:"timeoutSeconds"`
	AllowExecution      bool                       `json:"allowExecution"`
	AllowNativeTasks    bool                       `json:"allowNativeTasks"`
	Settings            json.RawMessage            `json:"settings"`
	CodexConfig         map[string]json.RawMessage `json:"codexConfig"`
	Configuration       modelcatalog.Configuration `json:"configuration"`
	UsageBinding        *Asset                     `json:"usageBinding,omitempty"`
	UsagePolicy         *Asset                     `json:"usagePolicy,omitempty"`
	WorkItemBinding     *Asset                     `json:"workItemBinding,omitempty"`
	root                string
	digest              string
	seal                string
}
type Prepared struct {
	HostDriverSHA256 string                     `json:"hostDriverSha256,omitempty"`
	HostProtocol     string                     `json:"hostProtocol,omitempty"`
	CatalogSHA256    string                     `json:"catalogSha256,omitempty"`
	WorkItemRef      *contracts.Reference       `json:"workItemRef,omitempty"`
	Version          string                     `json:"version"`
	Host             string                     `json:"host"`
	HostVersion      string                     `json:"hostVersion"`
	AssignmentSHA256 string                     `json:"assignmentSha256"`
	SnapshotSHA256   string                     `json:"snapshotSha256"`
	ProfileSHA256    string                     `json:"profileSha256"`
	PolicySHA256     string                     `json:"policySha256"`
	ConfigurationID  string                     `json:"configurationId"`
	Configuration    modelcatalog.Configuration `json:"configuration"`
	Model            string                     `json:"model"`
	Workspace        string                     `json:"workspace"`
	Sources          []Asset                    `json:"sources"`
	Instructions     string                     `json:"instructions"`
	Input            json.RawMessage            `json:"input"`
	OutputSchema     json.RawMessage            `json:"outputSchema"`
	Snapshot         json.RawMessage            `json:"snapshot"`
	NativeTasks      []TaskStep                 `json:"nativeTasks"`
	Digest           string                     `json:"digest"`
}
type Event struct {
	Sequence  int             `json:"sequence"`
	NativeID  string          `json:"nativeId"`
	Type      string          `json:"type"`
	SessionID string          `json:"sessionId"`
	ThreadID  string          `json:"threadId"`
	TurnID    string          `json:"turnId"`
	Data      json.RawMessage `json:"data"`
}
type Transcript struct {
	Events        []Event           `json:"events"`
	Output        json.RawMessage   `json:"output"`
	SessionID     string            `json:"sessionId"`
	ThreadID      string            `json:"threadId"`
	TurnID        string            `json:"turnId"`
	Model         string            `json:"model"`
	HostOutcome   string            `json:"hostOutcome"`
	ObservedTools []string          `json:"observedTools"`
	UsageFrames   []json.RawMessage `json:"usageFrames"`
	Diagnostics   []Diagnostic      `json:"diagnostics"`
}
type Receipt struct {
	ObservedModel    string `json:"observedModel"`
	UsageSHA256      string `json:"usageSha256"`
	Version          string `json:"version"`
	AttemptID        string `json:"attemptId"`
	PreparedSHA256   string `json:"preparedSha256"`
	PolicySHA256     string `json:"policySha256"`
	PreviousSHA256   string `json:"previousSha256"`
	Host             string `json:"host"`
	HostVersion      string `json:"hostVersion"`
	Workspace        string `json:"workspace"`
	Model            string `json:"model"`
	ConfigurationID  string `json:"configurationId"`
	SessionID        string `json:"sessionId"`
	ThreadID         string `json:"threadId"`
	TurnID           string `json:"turnId"`
	HostOutcome      string `json:"hostOutcome"`
	OutputValidated  bool   `json:"outputValidated"`
	ObservedAt       string `json:"observedAt"`
	TranscriptSHA256 string `json:"transcriptSha256"`
	Report           Report `json:"report"`
	Digest           string `json:"digest"`
}
type TaskStep struct {
	ID           string   `json:"id"`
	Subject      string   `json:"subject"`
	Description  string   `json:"description"`
	Dependencies []string `json:"dependencies"`
	NativeID     string   `json:"nativeId"`
	AOKState     string   `json:"aokState"`
}
type TaskProjection struct {
	Version       string     `json:"version"`
	AttemptSHA256 string     `json:"attemptSha256"`
	PlanSHA256    string     `json:"planSha256"`
	Steps         []TaskStep `json:"steps"`
	Report        Report     `json:"report"`
}
type Capabilities struct {
	Version          string            `json:"version"`
	Host             string            `json:"host"`
	HostVersion      string            `json:"hostVersion"`
	ExecutableSHA256 string            `json:"executableSha256"`
	Operations       map[string]string `json:"operations"`
	Report           Report            `json:"report"`
}
type ReplayManifest struct {
	Version  string   `json:"version"`
	Prepared Prepared `json:"prepared"`
	Frames   []Asset  `json:"frames"`
	Previous *Receipt `json:"previous,omitempty"`
}
type Bundle struct {
	Prepared   *Prepared
	Receipt    *Receipt
	Transcript *Transcript
	Files      map[string][]byte
}

// Launch is an internal, already-authorized argv invocation; never shell text.
type Launch struct {
	Executable     string
	Args           []string
	Dir            string
	Env            []string
	TimeoutSeconds int
	MaxFrameBytes  int
	MaxTotalBytes  int
}
type Process interface {
	Send([]byte) error
	Receive(context.Context) ([]byte, error)
	Stop() error
	Close() error
	Wait() error
}
type Runner interface {
	Start(context.Context, Launch) (Process, error)
}
type AttemptGuard interface {
	Check(context.Context, string) error
}
type Approval struct {
	AttemptID string
	ThreadID  string
	TurnID    string
	RequestID json.RawMessage
	Method    string
	Params    json.RawMessage
}
type ApprovalBroker interface {
	Decide(context.Context, Approval) (json.RawMessage, error)
}

var _ = contracts.MaxDocumentBytes
