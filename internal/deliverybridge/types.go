// Package deliverybridge validates offline repository-delivery evidence and
// drafts a narrowly scoped local handoff. It performs no publication or runtime
// finalization and grants no execution authority.
package deliverybridge

import (
	"encoding/json"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/verification"
)

const Version = "aok-delivery-bridge/v1"

// HandoffManifest is an explicit, offline evidence bundle. Engine receipts are
// kept as their original bytes so seals are recomputed over their actual data.
type HandoffManifest struct {
	Version                   string                          `json:"version"`
	ID                        string                          `json:"id"`
	Candidate                 verification.Candidate          `json:"candidate"`
	Policy                    verification.VerificationPolicy `json:"policy"`
	Acceptance                verification.AcceptanceReceipt  `json:"acceptance"`
	AcceptanceBytes           json.RawMessage                 `json:"acceptanceBytes"`
	AsOf                      time.Time                       `json:"asOf"`
	IndependentReview         verification.ReviewReceipt      `json:"independentReview"`
	SourceSnapshotSHA256      string                          `json:"sourceSnapshotSha256"`
	DestinationSnapshotSHA256 string                          `json:"destinationSnapshotSha256"`
	SourceSnapshotBytes       json.RawMessage                 `json:"sourceSnapshotBytes"`
	DestinationSnapshotBytes  json.RawMessage                 `json:"destinationSnapshotBytes"`
	ContextBytes              json.RawMessage                 `json:"contextBytes"`
	GateBytes                 json.RawMessage                 `json:"gateBytes"`
	ReviewBytes               json.RawMessage                 `json:"reviewBytes"`
	IntegrationBytes          json.RawMessage                 `json:"integrationBytes,omitempty"`
}

type ScopeContextDraft struct {
	Version                   string          `json:"version"`
	ID                        string          `json:"id"`
	CandidateSHA256           string          `json:"candidateSha256"`
	AcceptanceSHA256          string          `json:"acceptanceSha256"`
	SourceSnapshotSHA256      string          `json:"sourceSnapshotSha256"`
	DestinationSnapshotSHA256 string          `json:"destinationSnapshotSha256"`
	Repository                string          `json:"repository"`
	BaselineRevision          string          `json:"baselineRevision"`
	FilesInScope              []string        `json:"filesInScope"`
	GateCommands              [][]string      `json:"gateCommands"`
	Environment               json.RawMessage `json:"environment"`
	EngineContextSHA256       string          `json:"engineContextSha256"`
	EngineGateSHA256          string          `json:"engineGateSha256"`
	EngineReviewSHA256        string          `json:"engineReviewSha256"`
	EngineIntegrationSHA256   string          `json:"engineIntegrationSha256,omitempty"`
	Actions                   []string        `json:"actions"`
	AuthorizationGranted      bool            `json:"authorizationGranted"`
	ExecutionReady            bool            `json:"executionReady"`
}

type Report struct {
	Version              string   `json:"version"`
	Valid                bool     `json:"valid"`
	AuthorizationGranted bool     `json:"authorizationGranted"`
	ExecutionReady       bool     `json:"executionReady"`
	Diagnostics          []string `json:"diagnostics"`
}
