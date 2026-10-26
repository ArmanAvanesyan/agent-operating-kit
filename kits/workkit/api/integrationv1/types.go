// Package integrationv1 separates local/read authority from provider sends.
package integrationv1

import (
	o "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/operationsv1"
	z "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/syncv2"
	api "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/v1alpha1"
)

const Version = "workkit-integration/v1"
const JournalVersion = "workkit-journal/v2"

type Store struct {
	Version   string `json:"version"`
	Kind      string `json:"kind"`
	Path      string `json:"path"`
	ID        string `json:"id"`
	Namespace string `json:"namespace"`
	Owner     string `json:"owner"`
}
type Limits struct {
	PollSeconds    int `json:"pollSeconds"`
	JitterSeconds  int `json:"jitterSeconds"`
	ProcessSeconds int `json:"processSeconds"`
	CycleSeconds   int `json:"cycleSeconds"`
	RequestSeconds int `json:"requestSeconds"`
	Requests       int `json:"requests"`
	Items          int `json:"items"`
	ResponseBytes  int `json:"responseBytes"`
	CaptureBytes   int `json:"captureBytes"`
	MaxOperations  int `json:"maxOperations"`
	MaxHints       int `json:"maxHints"`
	InboxBytes     int `json:"inboxBytes"`
}
type Configuration struct {
	Version         string            `json:"version"`
	Kind            string            `json:"kind"`
	Store           Store             `json:"store"`
	WorkspaceSHA256 string            `json:"workspaceSha256"`
	Listen          string            `json:"listen"`
	Routes          map[string]string `json:"routes"`
	Limits          Limits            `json:"limits"`
}
type Issuer struct {
	ID        string `json:"id"`
	PublicKey string `json:"publicKey"`
}
type Policy struct {
	Limits          Limits            `json:"limits"`
	Routes          map[string]string `json:"routes"`
	Version         string            `json:"version"`
	Kind            string            `json:"kind"`
	NotBefore       string            `json:"notBefore"`
	ExpiresAt       string            `json:"expiresAt"`
	Issuers         []Issuer          `json:"issuers"`
	RevokedIDs      []string          `json:"revokedIds"`
	AllowedPurposes []string          `json:"allowedPurposes"`
	Store           Store             `json:"store"`
}

// Enrollment signatures are domain-separated from all P2/P5/v1 envelopes.
type Enrollment struct {
	AttachedFences       map[string]int64 `json:"attachedFences,omitempty"`
	CallerProcessID      int              `json:"callerProcessId,omitempty"`
	CallerProcessStart   string           `json:"callerProcessStart,omitempty"`
	ProcessID            int              `json:"processId,omitempty"`
	ProcessStart         string           `json:"processStart,omitempty"`
	WorkerID             string           `json:"workerId"`
	Version              string           `json:"version"`
	Kind                 string           `json:"kind"`
	ID                   string           `json:"id"`
	Purpose              string           `json:"purpose"`
	SubjectSHA256        string           `json:"subjectSha256"`
	PolicySHA256         string           `json:"policySha256"`
	ImplementationSHA256 string           `json:"implementationSha256"`
	Store                Store            `json:"store"`
	OutputPath           string           `json:"outputPath"`
	IssuedAt             string           `json:"issuedAt"`
	ExpiresAt            string           `json:"expiresAt"`
	Issuer               string           `json:"issuer"`
	Signature            string           `json:"signature"`
	RouteID              string           `json:"routeId"`
	Fence                int64            `json:"fence"`
}
type Headers struct {
	DeliveryID   string `json:"deliveryId"`
	Event        string `json:"event"`
	Signature256 string `json:"signature256"`
}
type IngressReceipt struct {
	RouteID      string  `json:"routeId"`
	ID           string  `json:"id"`
	Ingress      string  `json:"ingress"`
	ConnectionID string  `json:"connectionId"`
	RepositoryID string  `json:"repositoryId"`
	Origin       string  `json:"origin"`
	BodySHA256   string  `json:"bodySha256"`
	Headers      Headers `json:"headers"`
	ReceivedAt   string  `json:"receivedAt"`
	ExpiresAt    string  `json:"expiresAt"`
	Issuer       string  `json:"issuer"`
	Signature    string  `json:"signature"`
}
type EventEnvelope struct {
	Version  string         `json:"version"`
	Kind     string         `json:"kind"`
	Provider string         `json:"provider"`
	RouteID  string         `json:"routeId"`
	Body     []byte         `json:"body"`
	Receipt  IngressReceipt `json:"receipt"`
}
type Hint struct {
	ID           string `json:"id"`
	RouteID      string `json:"routeId"`
	ConnectionID string `json:"connectionId"`
	RepositoryID string `json:"repositoryId"`
	DeliveryID   string `json:"deliveryId"`
	BodySHA256   string `json:"bodySha256"`
	ReceivedAt   string `json:"receivedAt"`
	Disposition  string `json:"disposition"`
}
type Event struct {
	Sequence      int64  `json:"sequence"`
	RouteID       string `json:"routeId"`
	Kind          string `json:"kind"`
	SubjectSHA256 string `json:"subjectSha256"`
	ObservedAt    string `json:"observedAt"`
}
type Checkpoint struct {
	Version             string `json:"version"`
	Kind                string `json:"kind"`
	RouteID             string `json:"routeId"`
	ConfigurationSHA256 string `json:"configurationSha256"`
	ConnectionID        string `json:"connectionId"`
	AccountSHA256       string `json:"accountSha256"`
	QuerySHA256         string `json:"querySha256"`
	APIVersion          string `json:"apiVersion"`
	Cursor              string `json:"cursor"`
	ObservedAt          string `json:"observedAt"`
	Complete            bool   `json:"complete"`
	Gap                 bool   `json:"gap"`
	CompletedWatermark  string `json:"completedWatermark"`
}
type Command struct {
	Batch         *BatchRequest  `json:"batch,omitempty"`
	Version       string         `json:"version"`
	Kind          string         `json:"kind"`
	Action        string         `json:"action"`
	RouteID       string         `json:"routeId"`
	SubjectSHA256 string         `json:"subjectSha256"`
	AfterSequence int64          `json:"afterSequence"`
	Consumer      string         `json:"consumer"`
	Policy        []byte         `json:"policy"`
	Enrollment    []byte         `json:"enrollment"`
	Envelope      *EventEnvelope `json:"envelope,omitempty"`
	Invocation    []byte         `json:"invocation,omitempty"`
}
type Report struct {
	CandidateSHA256      string           `json:"candidateSha256,omitempty"`
	Conflicts            []z.Conflict     `json:"conflicts"`
	State                *z.State         `json:"state,omitempty"`
	Notifications        int              `json:"notifications"`
	Delivered            int64            `json:"delivered"`
	RequestSHA256        string           `json:"requestSha256,omitempty"`
	Version              string           `json:"version"`
	Kind                 string           `json:"kind"`
	Status               string           `json:"status"`
	Store                Store            `json:"store"`
	RouteID              string           `json:"routeId"`
	Fence                int64            `json:"fence"`
	Cycles               int              `json:"cycles"`
	Hints                int              `json:"hints"`
	Acknowledged         int64            `json:"acknowledged"`
	Events               []Event          `json:"events"`
	Operations           []o.Observation  `json:"operations"`
	Diagnostics          []api.Diagnostic `json:"diagnostics"`
	AuthorizationGranted bool             `json:"authorizationGranted"`
	ExecutionReady       bool             `json:"executionReady"`
	MigrationActivated   bool             `json:"migrationActivated"`
	ProductionActivated  bool             `json:"productionActivated"`
}

// OperationalReadRequest is signed as the exact enrollment subject. Existing
// P2 qualification-only enrollment is never repurposed as operational authority.
type OperationalReadRequest struct {
	Version               string   `json:"version"`
	Kind                  string   `json:"kind"`
	RouteID               string   `json:"routeId"`
	ConfigurationSHA256   string   `json:"configurationSha256"`
	ConnectionSHA256      string   `json:"connectionSha256"`
	Principal             string   `json:"principal"`
	AuthenticationMode    string   `json:"authenticationMode"`
	Origin                string   `json:"origin"`
	AllowedEndpointSHA256 []string `json:"allowedEndpointSha256"`
	QuerySHA256           string   `json:"querySha256"`
	CheckpointSHA256      string   `json:"checkpointSha256"`
	Limits                Limits   `json:"limits"`
}

type BatchRequest struct {
	Plan       string `json:"plan"`
	PlanSHA256 string `json:"planSha256"`
	Invocation string `json:"invocation"`
	Policy     string `json:"policy"`
	Enrollment string `json:"enrollment"`
	Output     string `json:"output"`
	Action     string `json:"action"`
}

type WorkerRecovery struct {
	Version             string `json:"version"`
	Kind                string `json:"kind"`
	Store               Store  `json:"store"`
	RouteID             string `json:"routeId"`
	PriorFence          int64  `json:"priorFence"`
	NewWorkerID         string `json:"newWorkerId"`
	ConfigurationSHA256 string `json:"configurationSha256"`
	QuiescenceSHA256    string `json:"quiescenceSha256"`
}

type OperationalReadObservation struct {
	Version              string                 `json:"version"`
	Kind                 string                 `json:"kind"`
	RequestSHA256        string                 `json:"requestSha256"`
	ParserBundleSHA256   string                 `json:"parserBundleSha256"`
	ObservedAt           string                 `json:"observedAt"`
	Request              OperationalReadRequest `json:"request"`
	Policy               Policy                 `json:"policy"`
	Enrollment           Enrollment             `json:"enrollment"`
	AuthorizationGranted bool                   `json:"authorizationGranted"`
	ExecutionReady       bool                   `json:"executionReady"`
	MigrationActivated   bool                   `json:"migrationActivated"`
	ProductionActivated  bool                   `json:"productionActivated"`
}
