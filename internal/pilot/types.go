// Package pilot defines offline, versioned controlled-pilot contracts. It has
// no network, credential, executor, or accounting implementation.
package pilot

import (
	"crypto/ed25519"
	"encoding/json"
	"time"
)

const Version = "aok-pilot/v1"

type Purpose string

const (
	Calibration Purpose = "calibration-experiment"
	Production  Purpose = "production-policy-pilot"
)

type MetricRule struct {
	Name             string      `json:"name"`
	Unit             string      `json:"unit"`
	Direction        string      `json:"direction"` // minimum or maximum
	Target           json.Number `json:"target"`
	MinimumN         int         `json:"minimumN"`
	AnalysisUnit     string      `json:"analysisUnit"`
	Denominator      string      `json:"denominator"`
	ConfidenceMethod string      `json:"confidenceMethod"`
	ConfidenceLevel  json.Number `json:"confidenceLevel"`
	Baseline         string      `json:"baseline"`
	MissingRule      string      `json:"missingRule"`
	CensoringRule    string      `json:"censoringRule"`
	StopThreshold    json.Number `json:"stopThreshold"`
}

type Route struct {
	Host                string `json:"host"`
	HostVersion         string `json:"hostVersion"`
	HostSHA256          string `json:"hostSha256"`
	Provider            string `json:"provider"`
	Platform            string `json:"platform"`
	Transport           string `json:"transport"`
	ControlPolicySHA256 string `json:"controlPolicySha256"`
	ConfigurationSHA256 string `json:"configurationSha256"`
	ProfileSHA256       string `json:"profileSha256"`
	RouteManifestSHA256 string `json:"routeManifestSha256"`
	NativeTenant        string `json:"nativeTenant"`
	API                 string `json:"api"`
	Origin              string `json:"origin"`
	VerifierSHA256      string `json:"verifierSha256"`
	RubricSHA256        string `json:"rubricSha256"`
	EnvironmentSHA256   string `json:"environmentSha256"`
	AccountScopeSHA256  string `json:"accountScopeSha256"`
	Account             string `json:"account"`
	Operation           string `json:"operation"`
	AuthMode            string `json:"authMode"`
}
type TrialSpec struct {
	ID                 string      `json:"id"`
	Repository         string      `json:"repository"`
	Account            string      `json:"account"`
	InputSHA256        string      `json:"inputSha256"`
	RouteSHA256        string      `json:"routeSha256"`
	MaximumAttempts    int         `json:"maximumAttempts"`
	MaximumSeconds     int         `json:"maximumSeconds"`
	MaximumConsumption json.Number `json:"maximumConsumption"`
	ConsumptionUnit    string      `json:"consumptionUnit"`
}

type Manifest struct {
	Version                    string              `json:"version"`
	ID                         string              `json:"id"`
	Purpose                    Purpose             `json:"purpose"`
	Risk                       string              `json:"risk"`
	FrozenAt                   string              `json:"frozenAt"`
	ConfigurationSHA256        string              `json:"configurationSha256"`
	DatasetSHA256              string              `json:"datasetSha256"`
	ProtocolSHA256             string              `json:"protocolSha256"`
	FamilyPartitions           map[string]string   `json:"familyPartitions"`
	RepetitionsByFamily        map[string][]string `json:"repetitionsByFamily"`
	Metrics                    []MetricRule        `json:"metrics"`
	MinimumShadowOpportunities int                 `json:"minimumShadowOpportunities"`
	Routes                     []Route             `json:"routes"`
	AllowedRepositories        []string            `json:"allowedRepositories"`
	AllowedAccounts            []string            `json:"allowedAccounts"`
	Operators                  []string            `json:"operators"`
	MaximumAttempts            int                 `json:"maximumAttempts"`
	MaximumSeconds             int                 `json:"maximumSeconds"`
	MaximumConsumption         json.Number         `json:"maximumConsumption"`
	ConsumptionUnit            string              `json:"consumptionUnit"`
	RetentionDays              int                 `json:"retentionDays"`
	Trials                     []TrialSpec         `json:"trials"`
}

// Policy is supplied by an operator through a trusted channel. This package
// deliberately does not decide who may sign it or load trust anchors.
type Policy struct {
	Version          string              `json:"version"`
	ManifestSHA256   string              `json:"manifestSha256"`
	Evidence         []EvidenceReference `json:"evidence"`
	ExpiresAt        string              `json:"expiresAt"`
	FreezeReceipt    FreezeReceipt       `json:"freezeReceipt"`
	AuditOpenReceipt AuditOpenReceipt    `json:"auditOpenReceipt"`
}

type FreezeReceipt struct {
	Version        string `json:"version"`
	CampaignSHA256 string `json:"campaignSha256"`
	DatasetSHA256  string `json:"datasetSha256"`
	FrozenAt       string `json:"frozenAt"`
	Issuer         string `json:"issuer"`
	Signature      string `json:"signature"`
}
type AuditOpenReceipt struct {
	Version             string `json:"version"`
	CampaignSHA256      string `json:"campaignSha256"`
	DatasetSHA256       string `json:"datasetSha256"`
	FreezeReceiptSHA256 string `json:"freezeReceiptSha256"`
	OpenedAt            string `json:"openedAt"`
	Issuer              string `json:"issuer"`
	Signature           string `json:"signature"`
}

type EvidenceReference struct {
	Kind        string `json:"kind"`
	SHA256      string `json:"sha256"`
	ScopeSHA256 string `json:"scopeSha256"`
}

// Qualification is returned only by an injected verifier that checks signed
// evidence and its protected trust roots. The default offline assessor has no
// verifier and always leaves execution and promotion blocked.
type Qualification struct {
	Authorization bool
	Native        bool
	P4            bool
	P3            bool
	Route         bool
}
type QualificationVerifier interface {
	Verify(Manifest, Policy, Bundle, time.Time) (Qualification, error)
}

type Observation struct {
	ID             string       `json:"id"`
	RouteSHA256    string       `json:"routeSha256"`
	FamilyID       string       `json:"familyId"`
	RepetitionID   string       `json:"repetitionId"`
	ObservedAt     string       `json:"observedAt"`
	Metric         string       `json:"metric"`
	Value          *json.Number `json:"value"`
	Unit           string       `json:"unit"`
	Accepted       bool         `json:"accepted"`
	Censored       bool         `json:"censored"`
	Unknown        bool         `json:"unknown"`
	ReceiptSHA256  string       `json:"receiptSha256"`
	VerifierSHA256 string       `json:"verifierSha256"`
}

// Episode contains complete per-attempt cost/time evidence. Cost must include
// setup, review, recovery and failed/interrupted work; totals stay unknown when
// any component is censored or missing.
type EpisodeObservation struct {
	ID              string      `json:"id"`
	RouteSHA256     string      `json:"routeSha256"`
	FamilyID        string      `json:"familyId"`
	RepetitionID    string      `json:"repetitionId"`
	ObservedAt      string      `json:"observedAt"`
	Outcome         string      `json:"outcome"`
	Accepted        bool        `json:"accepted"`
	Censored        bool        `json:"censored"`
	Cost            json.Number `json:"cost"`
	SetupCost       json.Number `json:"setupCost"`
	ReviewCost      json.Number `json:"reviewCost"`
	RecoveryCost    json.Number `json:"recoveryCost"`
	Currency        string      `json:"currency"`
	ElapsedSeconds  json.Number `json:"elapsedSeconds"`
	SetupSeconds    json.Number `json:"setupSeconds"`
	ReviewSeconds   json.Number `json:"reviewSeconds"`
	RecoverySeconds json.Number `json:"recoverySeconds"`
	ReceiptSHA256   string      `json:"receiptSha256"`
	VerifierSHA256  string      `json:"verifierSha256"`
}

// AccountWindowObservation is one independent randomized account-window
// cluster arm. The same block must contain exactly one baseline and treatment
// window before a controlled difference is computed.
type AccountWindowObservation struct {
	ID                     string `json:"id"`
	RouteSHA256            string `json:"routeSha256"`
	BlockID                string `json:"blockId"`
	AccountScopeSHA256     string `json:"accountScopeSha256"`
	Arm                    string `json:"arm"`
	Sequence               string `json:"sequence"`
	ObservedAt             string `json:"observedAt"`
	Censored               bool   `json:"censored"`
	Unknown                bool   `json:"unknown"`
	EligibleBeforeCapacity int    `json:"eligibleBeforeCapacity"`
	CapacityDeferrals      int    `json:"capacityDeferrals"`
	ProviderAttempts       int    `json:"providerAttempts"`
	QuotaDenials           int    `json:"quotaDenials"`
	AdmittedStarts         int    `json:"admittedStarts"`
	ReceiptSHA256          string `json:"receiptSha256"`
	VerifierSHA256         string `json:"verifierSha256"`
}

type ShadowOpportunity struct {
	ID          string `json:"id"`
	InputSHA256 string `json:"inputSha256"`
	ObservedAt  string `json:"observedAt"`
	Eligible    bool   `json:"eligible"`
	Abstained   bool   `json:"abstained"`
	Dispatched  bool   `json:"dispatched"`
}

type Bundle struct {
	Version              string                     `json:"version"`
	ManifestSHA256       string                     `json:"manifestSha256"`
	Observations         []Observation              `json:"observations"`
	Episodes             []EpisodeObservation       `json:"episodes"`
	AccountWindows       []AccountWindowObservation `json:"accountWindows"`
	Shadow               []ShadowOpportunity        `json:"shadow"`
	ConsequentialMisses  int                        `json:"consequentialMisses"`
	UnauthorizedRequests int                        `json:"unauthorizedRequests"`
	HardGateBypasses     int                        `json:"hardGateBypasses"`
	FalseAcceptances     int                        `json:"falseAcceptances"`
	OverAdmissions       int                        `json:"overAdmissions"`
	AmbiguousResends     int                        `json:"ambiguousResends"`
}

type MetricResult struct {
	Name      string      `json:"name"`
	N         int         `json:"n"`
	Missing   int         `json:"missing"`
	Censored  int         `json:"censored"`
	Unknown   int         `json:"unknown"`
	Estimate  json.Number `json:"estimate"`
	Lower     json.Number `json:"lower"`
	Upper     json.Number `json:"upper"`
	Pass      bool        `json:"pass"`
	Supported bool        `json:"supported"`
}

type Assessment struct {
	Version              string         `json:"version"`
	ManifestSHA256       string         `json:"manifestSha256"`
	BundleSHA256         string         `json:"bundleSha256"`
	Status               string         `json:"status"` // insufficient-evidence, blocked, eligible-for-review
	ProductionEligible   bool           `json:"productionEligible"`
	AuthorizationGranted bool           `json:"authorizationGranted"`
	ExecutionReady       bool           `json:"executionReady"`
	StopTriggered        bool           `json:"stopTriggered"`
	StopReasons          []string       `json:"stopReasons"`
	ShadowEligible       int            `json:"shadowEligible"`
	ShadowAbstentions    int            `json:"shadowAbstentions"`
	ShadowRequired       int            `json:"shadowRequired"`
	Metrics              []MetricResult `json:"metrics"`
	Reasons              []string       `json:"reasons"`
	seal                 [32]byte
}

type PromotionGrant struct {
	Version              string      `json:"version"`
	ID                   string      `json:"id"`
	ApprovedBy           string      `json:"approvedBy"`
	ManifestSHA256       string      `json:"manifestSha256"`
	AssessmentSHA256     string      `json:"assessmentSha256"`
	BundleSHA256         string      `json:"bundleSha256"`
	RouteSHA256Allowlist []string    `json:"routeSha256Allowlist"`
	MaximumAttempts      int         `json:"maximumAttempts"`
	MaximumSeconds       int         `json:"maximumSeconds"`
	MaximumConsumption   json.Number `json:"maximumConsumption"`
	ConsumptionUnit      string      `json:"consumptionUnit"`
	IssuedAt             string      `json:"issuedAt"`
	ExpiresAt            string      `json:"expiresAt"`
	Signature            string      `json:"signature"`
}

// PromotionTrust is selected through a protected caller boundary. The grant's
// ApprovedBy field is an identity claim and must match this independently
// resolved principal and role.
type PromotionTrust struct {
	Principal          string
	Role               string
	PublicKey          ed25519.PublicKey
	MaxTTL             time.Duration
	RevocationVerifier PromotionRevocationVerifier
}

// PromotionRevocationVerifier checks the exact signed grant against current
// protected revocation state. Missing implementations fail closed.
type PromotionRevocationVerifier interface {
	VerifyPromotionGrantCurrent(PromotionGrant, time.Time) error
}

// VerifiedPromotionGrant is an opaque result for a separately qualified
// admission controller. It is not an executor token and grants no gate bypass.
type VerifiedPromotionGrant struct{ grant PromotionGrant }

func (v *VerifiedPromotionGrant) ID() string {
	if v == nil {
		return ""
	}
	return v.grant.ID
}

type OperationalDrillReceipt struct {
	Version         string      `json:"version"`
	ID              string      `json:"id"`
	ManifestSHA256  string      `json:"manifestSha256"`
	RouteSHA256     string      `json:"routeSha256"`
	Drill           string      `json:"drill"`
	Outcome         string      `json:"outcome"`
	ObservedAt      string      `json:"observedAt"`
	MeasuredSeconds json.Number `json:"measuredSeconds"`
	MaximumSeconds  int         `json:"maximumSeconds"`
	Issuer          string      `json:"issuer"`
	Signature       string      `json:"signature"`
}

type ReplayScenario struct {
	Version        string              `json:"version"`
	Opportunities  []ShadowOpportunity `json:"opportunities"`
	CapacityBlocks int                 `json:"capacityBlocks"`
	QuotaDenials   int                 `json:"quotaDenials"`
	AdmittedStarts int                 `json:"admittedStarts"`
	Accepted       int                 `json:"accepted"`
	Failed         int                 `json:"failed"`
}

type ReplayResult struct {
	Version            string `json:"version"`
	Opportunities      int    `json:"opportunities"`
	Eligible           int    `json:"eligible"`
	Abstentions        int    `json:"abstentions"`
	CapacityBlocks     int    `json:"capacityBlocks"`
	QuotaDenials       int    `json:"quotaDenials"`
	AdmittedStarts     int    `json:"admittedStarts"`
	Accepted           int    `json:"accepted"`
	Failed             int    `json:"failed"`
	ProductionEligible bool   `json:"productionEligible"`
}

// Executor is only a seam for a separately qualified caller. No executor is
// provided by this package, and implementation does not convey authority.
type Executor interface {
	Execute(Manifest, Policy) (Bundle, error)
}
