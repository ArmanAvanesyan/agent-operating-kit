// Package records defines independently versioned runtime envelopes. These
// observations never establish P8 acceptance or provider allowance.
package records

import (
	"encoding/json"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
	accounting "github.com/ArmanAvanesyan/agent-operating-kit/internal/usage/store"
	"time"
)

const Version = "aok-runtime/v1"
const VersionV2 = "aok-runtime/v2"
const LifecycleVersion = "aok-runtime-lifecycle/v1"

type Asset struct {
	Path       string `json:"path"`
	SHA256     string `json:"sha256"`
	Executable bool   `json:"executable,omitempty"`
}
type HostRoute struct {
	StepID        string `json:"stepId,omitempty"`
	Attempt       int    `json:"attempt,omitempty"`
	Host          string `json:"host"`
	Policy        Asset  `json:"policy"`
	Qualification *Asset `json:"qualification,omitempty"`
}
type Policy struct {
	ReconciliationIssuers map[string]string `json:"reconciliationIssuers,omitempty"`
	Version               string            `json:"version"`
	Authority             string            `json:"authority"`
	RepositoryRoots       []string          `json:"repositoryRoots"`
	StagingRoot           string            `json:"stagingRoot"`
	AccountingPath        string            `json:"accountingPath"`
	UsagePolicy           Asset             `json:"usagePolicy"`
	Hosts                 []HostRoute       `json:"hosts"`
	AllowDispatch         bool              `json:"allowDispatch"`
	MaxConcurrent         int               `json:"maxConcurrent"`
	LeaseSeconds          int               `json:"leaseSeconds"`
	HeartbeatSeconds      int               `json:"heartbeatSeconds"`
	MaxAttempts           int               `json:"maxAttempts"`
}
type SelectionInputs struct {
	Task           string `json:"task"`
	Candidates     string `json:"candidates"`
	Catalog        string `json:"catalog"`
	CatalogPolicy  string `json:"catalogPolicy"`
	Policy         string `json:"policy"`
	EvidencePolicy string `json:"evidencePolicy"`
}
type StepSpec struct {
	ID         string          `json:"id"`
	Assignment string          `json:"assignment"`
	Host       string          `json:"host"`
	Selection  SelectionInputs `json:"selection"`
}
type Manifest struct {
	RepositoryAssets []Asset    `json:"repositoryAssets"`
	AllowedUntracked []string   `json:"allowedUntracked"`
	Version          string     `json:"version"`
	ID               string     `json:"id"`
	Repository       string     `json:"repository"`
	InputRevision    string     `json:"inputRevision"`
	Corpus           string     `json:"corpus"`
	Plan             string     `json:"plan"`
	Evidence         string     `json:"evidence"`
	Steps            []StepSpec `json:"steps"`
	Assets           []Asset    `json:"assets"`
}
type Token struct {
	RunID     string `json:"runId"`
	StepID    string `json:"stepId"`
	AttemptID string `json:"attemptId"`
	Owner     string `json:"owner"`
	Epoch     string `json:"epoch"`
	Fence     int64  `json:"fence"`
}
type Event struct {
	Sequence       int64  `json:"sequence"`
	ID             string `json:"id"`
	OperationKey   string `json:"operationKey"`
	RequestSHA256  string `json:"requestSha256"`
	RunID          string `json:"runId"`
	StepID         string `json:"stepId"`
	AttemptID      string `json:"attemptId"`
	Phase          string `json:"phase"`
	At             string `json:"at"`
	PreviousSHA256 string `json:"previousSha256"`
	PayloadSHA256  string `json:"payloadSha256"`
	SHA256         string `json:"sha256"`
}
type Step struct {
	ID                string             `json:"id"`
	Phase             string             `json:"phase"`
	Token             Token              `json:"token"`
	LeaseUntil        string             `json:"leaseUntil"`
	Attempts          int                `json:"attempts"`
	Reason            string             `json:"reason"`
	Workspace         string             `json:"workspace"`
	Checkpoint        *Checkpoint        `json:"checkpoint,omitempty"`
	Reservation       *usage.Reservation `json:"reservation,omitempty"`
	AccountingToken   *accounting.Token  `json:"accountingToken,omitempty"`
	DecisionSHA256    string             `json:"decisionSha256"`
	Demand            *usage.Demand      `json:"demand,omitempty"`
	PreparedSHA256    string             `json:"preparedSha256"`
	HostReceiptSHA256 string             `json:"hostReceiptSha256"`
	StopObserved      bool               `json:"stopObserved"`
	LaunchPossible    bool               `json:"launchPossible"`
	Accepted          bool               `json:"accepted"`
}
type Run struct {
	Manifest       Manifest `json:"manifest"`
	ManifestSHA256 string   `json:"manifestSha256"`
	Cancelled      bool     `json:"cancelled"`
	CancelReason   string   `json:"cancelReason"`
	Steps          []Step   `json:"steps"`
	Revision       int64    `json:"revision"`
}
type State struct {
	Version          string  `json:"version"`
	LifecycleVersion string  `json:"lifecycleVersion"`
	Authority        string  `json:"authority"`
	Epoch            string  `json:"epoch"`
	PolicySHA256     string  `json:"policySha256"`
	Revision         int64   `json:"revision"`
	LastAt           string  `json:"lastAt"`
	Runs             []Run   `json:"runs"`
	Events           []Event `json:"events"`
}
type Checkpoint struct {
	Version       string  `json:"version"`
	ID            string  `json:"id"`
	AttemptID     string  `json:"attemptId"`
	InputRevision string  `json:"inputRevision"`
	Workspace     string  `json:"workspace"`
	Repository    string  `json:"repository"`
	BaseRevision  string  `json:"baseRevision"`
	SnapshotRoot  string  `json:"snapshotRoot"`
	Assets        []Asset `json:"assets"`
	ObservedAt    string  `json:"observedAt"`
	Quiescent     bool    `json:"quiescent"`
	Digest        string  `json:"digest"`
}
type Admission struct {
	Version         string   `json:"version"`
	AttemptID       string   `json:"attemptId"`
	DecisionSHA256  string   `json:"decisionSha256"`
	ProfileSHA256   string   `json:"profileSha256"`
	ConfigurationID string   `json:"configurationId"`
	LedgerRevision  int64    `json:"ledgerRevision"`
	LeaseUntil      string   `json:"leaseUntil"`
	Status          string   `json:"status"`
	Reasons         []string `json:"reasons"`
	Accepted        bool     `json:"accepted"`
}
type CapacityView struct {
	ConfigurationID string          `json:"configurationId"`
	AsOf            string          `json:"asOf"`
	Disposition     string          `json:"disposition"`
	Status          string          `json:"status"`
	Balances        []usage.Balance `json:"balances"`
}
type Report struct {
	Capacity       []CapacityView `json:"capacity,omitempty"`
	Version        string         `json:"version"`
	Status         string         `json:"status"`
	Reasons        []string       `json:"reasons"`
	CoreGate       string         `json:"coreGate"`
	CodexLiveGate  string         `json:"codexLiveGate"`
	ClaudeLiveGate string         `json:"claudeLiveGate"`
	RuntimeGate    string         `json:"runtimeGate"`
	Accepted       bool           `json:"accepted"`
	State          *State         `json:"state,omitempty"`
	Admission      *Admission     `json:"admission,omitempty"`
}
type RecoveryEvidence struct {
	ManifestSHA256 string         `json:"manifestSha256"`
	ObservedAt     string         `json:"observedAt"`
	Issuer         string         `json:"issuer"`
	Signature      string         `json:"signature"`
	StopObserved   bool           `json:"stopObserved"`
	Actual         []usage.Amount `json:"actual"`

	Version    string `json:"version"`
	RunID      string `json:"runId"`
	StepID     string `json:"stepId"`
	AttemptID  string `json:"attemptId"`
	Receipt    Asset  `json:"receipt"`
	Transcript Asset  `json:"transcript"`
}

// AcceptanceAuthorityState is a short-lived signed snapshot from the external
// protected acceptance trust and revocation authority.
type AcceptanceAuthorityState struct {
	Version              string `json:"version"`
	PolicySHA256         string `json:"policySha256"`
	TrustGeneration      int64  `json:"trustGeneration"`
	RevocationGeneration int64  `json:"revocationGeneration"`
	ObservedAt           string `json:"observedAt"`
	ValidUntil           string `json:"validUntil"`
	Issuer               string `json:"issuer"`
	Signature            string `json:"signature"`
}

const AcceptanceAuthorityVersion = "aok-runtime-acceptance-authority/v1"
const AcceptanceAuthorityRole = "acceptance-projection-authority"

type Clock interface{ Now() time.Time }
type RealClock struct{}

func (RealClock) Now() time.Time { return time.Now().UTC() }

type Scenario struct {
	Version  string          `json:"version"`
	Policy   Policy          `json:"policy"`
	Manifest Manifest        `json:"manifest"`
	Events   []ScenarioEvent `json:"events"`
}
type ScenarioEvent struct {
	Operation string          `json:"operation"`
	StepID    string          `json:"stepId"`
	At        string          `json:"at"`
	Payload   json.RawMessage `json:"payload"`
}
