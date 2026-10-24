// Package verification contains offline candidate verification and acceptance
// contracts. It deliberately has no runtime, provider, or process authority.
package verification

import "encoding/json"

const Version = "aok-verification/v1"
const MaxPolicyFrameBytes = 4 << 20
const MaxCandidateCaptureBytes = 64 << 20
const MaxVerifierOutputBytes = 4 << 20
const MaxVerifierTimeoutSeconds = 30

type Asset struct {
	Path       string `json:"path"`
	SHA256     string `json:"sha256"`
	Executable bool   `json:"executable"`
	Role       string `json:"role"`
}
type CandidateManifest struct {
	Version               string   `json:"version"`
	ID                    string   `json:"id"`
	Origin                string   `json:"origin"`
	Repository            string   `json:"repository"`
	BaseRevision          string   `json:"baseRevision"`
	SourceRevision        string   `json:"sourceRevision"`
	PlanSHA256            string   `json:"planSha256"`
	GoalSHA256            string   `json:"goalSha256"`
	AssignmentSHA256      string   `json:"assignmentSha256"`
	InputSHA256           string   `json:"inputSha256"`
	ConfigurationSHA256   string   `json:"configurationSha256"`
	ProfileSHA256         string   `json:"profileSha256"`
	PreparedReceiptSHA256 string   `json:"preparedReceiptSha256"`
	HostReceiptSHA256     string   `json:"hostReceiptSha256"`
	CheckpointSHA256      string   `json:"checkpointSha256"`
	AuthorPrincipal       string   `json:"authorPrincipal"`
	RunID                 string   `json:"runId"`
	StepID                string   `json:"stepId"`
	AttemptID             string   `json:"attemptId"`
	Fence                 uint64   `json:"fence"`
	CandidateRevision     uint64   `json:"candidateRevision"`
	Quiescent             bool     `json:"quiescent"`
	Assets                []Asset  `json:"assets"`
	Evidence              []string `json:"evidence"`
}
type VerifierSpec struct {
	ID                   string            `json:"id"`
	Kind                 string            `json:"kind"`
	ImplementationSHA256 string            `json:"implementationSha256"`
	RubricSHA256         string            `json:"rubricSha256"`
	Argv                 []string          `json:"argv"`
	Toolchain            string            `json:"toolchain"`
	Environment          map[string]string `json:"environment"`
	TimeoutSeconds       int               `json:"timeoutSeconds"`
	MaxOutputBytes       int64             `json:"maxOutputBytes"`
}
type Criterion struct {
	ID                 string `json:"id"`
	Kind               string `json:"kind"`
	Required           bool   `json:"required"`
	AllowNotApplicable bool   `json:"allowNotApplicable"`
	Expected           string `json:"expected,omitempty"`
	VerifierID         string `json:"verifierId,omitempty"`
	Independent        bool   `json:"independent,omitempty"`
}
type VerificationPolicy struct {
	Version          string          `json:"version"`
	ID               string          `json:"id"`
	Issuers          []TrustedIssuer `json:"issuers"`
	AuthorBindings   []AuthorBinding `json:"authorBindings"`
	Verifiers        []VerifierSpec  `json:"verifiers"`
	Criteria         []Criterion     `json:"criteria"`
	MaxAgeSeconds    int             `json:"maxAgeSeconds"`
	RequireQuiescent bool            `json:"requireQuiescent"`
	ReviewMinimum    int             `json:"reviewMinimum"`
}
type AuthorBinding struct {
	AssignmentSHA256 string `json:"assignmentSha256,omitempty"`
	CandidateSHA256  string `json:"candidateSha256,omitempty"`
	Principal        string `json:"principal"`
}
type TrustedIssuer struct {
	ID             string `json:"id"`
	PublicKey      string `json:"publicKey"`
	Role           string `json:"role"`
	Principal      string `json:"principal"`
	ExecutorSHA256 string `json:"executorSha256"`
}
type PredicateResult struct {
	CriterionID string   `json:"criterionId"`
	Result      string   `json:"result"`
	Evidence    []string `json:"evidence"`
	Rationale   string   `json:"rationale,omitempty"`
}
type VerificationReceipt struct {
	Version              string            `json:"version"`
	ID                   string            `json:"id"`
	CandidateSHA256      string            `json:"candidateSha256"`
	PolicySHA256         string            `json:"policySha256"`
	VerifierID           string            `json:"verifierId"`
	Principal            string            `json:"principal"`
	ImplementationSHA256 string            `json:"implementationSha256"`
	VerifierSpecSHA256   string            `json:"verifierSpecSha256"`
	RubricSHA256         string            `json:"rubricSha256"`
	EnvironmentSHA256    string            `json:"environmentSha256"`
	Results              []PredicateResult `json:"results"`
	SnapshotBefore       string            `json:"snapshotBefore"`
	SnapshotAfter        string            `json:"snapshotAfter"`
	Scope                string            `json:"scope"`
	ObservedAt           string            `json:"observedAt"`
	Signature            string            `json:"signature"`
}
type ReviewReceipt struct {
	Version         string `json:"version"`
	ID              string `json:"id"`
	CandidateSHA256 string `json:"candidateSha256"`
	SnapshotSHA256  string `json:"snapshotSha256"`
	PolicySHA256    string `json:"policySha256"`
	Principal       string `json:"principal"`
	Disposition     string `json:"disposition"`
	Issuer          string `json:"issuer"`
	ObservedAt      string `json:"observedAt"`
	Signature       string `json:"signature"`
}
type ControlProof struct {
	Version         string   `json:"version"`
	PolicySHA256    string   `json:"policySha256"`
	VerifierSHA256  string   `json:"verifierSha256"`
	CandidateSHA256 string   `json:"candidateSha256"`
	SnapshotSHA256  string   `json:"snapshotSha256"`
	Scope           string   `json:"scope"`
	ExecutorSHA256  string   `json:"executorSha256"`
	ObservedAt      string   `json:"observedAt"`
	ValidUntil      string   `json:"validUntil"`
	Controls        []string `json:"controls"`
	Issuer          string   `json:"issuer"`
	Signature       string   `json:"signature"`
}
type AcceptanceReceipt struct {
	Version           string            `json:"version"`
	ID                string            `json:"id"`
	CandidateSHA256   string            `json:"candidateSha256"`
	CandidateRevision uint64            `json:"candidateRevision"`
	SnapshotSHA256    string            `json:"snapshotSha256"`
	RunID             string            `json:"runId"`
	StepID            string            `json:"stepId"`
	AttemptID         string            `json:"attemptId"`
	Fence             uint64            `json:"fence"`
	InputSHA256       string            `json:"inputSha256"`
	PolicySHA256      string            `json:"policySha256"`
	PlanSHA256        string            `json:"planSha256"`
	GoalSHA256        string            `json:"goalSha256"`
	CriteriaSHA256    string            `json:"criteriaSha256"`
	VerificationIDs   []string          `json:"verificationIds"`
	Results           []PredicateResult `json:"results"`
	// Digest references point to separately preserved immutable signed evidence.
	VerificationSHA256s []string `json:"verificationSha256s"`
	ReviewSHA256s       []string `json:"reviewSha256s"`
	Disposition         string   `json:"disposition"`
	Issuer              string   `json:"issuer"`
	IssuedAt            string   `json:"issuedAt"`
	ValidUntil          string   `json:"validUntil"`
	Signature           string   `json:"signature"`
}
type ExistingReceipt struct {
	Kind            string          `json:"kind"`
	Digest          string          `json:"digest"`
	SnapshotSHA256  string          `json:"snapshotSha256"`
	CandidateSHA256 string          `json:"candidateSha256"`
	ReceiptBytes    json.RawMessage `json:"receiptBytes"`
}
type DeliveryHandoff struct {
	Version                   string            `json:"version"`
	ID                        string            `json:"id"`
	CandidateSHA256           string            `json:"candidateSha256"`
	AcceptanceSHA256          string            `json:"acceptanceSha256"`
	SourceSnapshotSHA256      string            `json:"sourceSnapshotSha256"`
	DestinationSnapshotSHA256 string            `json:"destinationSnapshotSha256"`
	Receipts                  []ExistingReceipt `json:"receipts"`
	Actions                   []string          `json:"actions"`
}
type DeliveryProof struct {
	Version       string   `json:"version"`
	HandoffSHA256 string   `json:"handoffSha256"`
	Valid         bool     `json:"valid"`
	Diagnostics   []string `json:"diagnostics"`
}
type Candidate struct {
	BundlePath     string
	Manifest       CandidateManifest
	Digest         string
	SnapshotSHA256 string
}
type Report struct {
	CandidateSHA256 string            `json:"candidateSha256"`
	PolicySHA256    string            `json:"policySha256"`
	Results         []PredicateResult `json:"results"`
	Disposition     string            `json:"disposition"`
}
