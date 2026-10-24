// Package usage provides offline accounting contracts. Observed headroom never
// grants account access, execution authority, or provider-side reservations.
package usage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
)

const Version = "aok-usage/v1"

type Origin struct {
	SourceID       string `json:"sourceId"`
	Host           string `json:"host"`
	HostVersion    string `json:"hostVersion"`
	AdapterVersion string `json:"adapterVersion"`
	AuthMode       string `json:"authMode"`
	ObservedAt     string `json:"observedAt"`
	ReceivedAt     string `json:"receivedAt"`
	FreshnessKnown bool   `json:"freshnessKnown"`
}
type SourceRule struct {
	ID                string   `json:"id"`
	Host              string   `json:"host"`
	HostVersion       string   `json:"hostVersion"`
	AdapterVersion    string   `json:"adapterVersion"`
	AuthModes         []string `json:"authModes"`
	Metrics           []string `json:"metrics"`
	Authority         string   `json:"authority"`
	MaxAgeSeconds     int      `json:"maxAgeSeconds"`
	CollectionAllowed bool     `json:"collectionAllowed"`
}
type Policy struct {
	rawSHA256       string
	canonicalSHA256 string
	Version         string                 `json:"version"`
	Sources         []SourceRule           `json:"sources"`
	SafetyMargins   map[string]json.Number `json:"safetyMargins"`
}

// PolicyHash retains the exact supplied policy bytes while rejecting a stale
// raw hash when a caller changes its typed contents after loading.
func PolicyHash(p Policy) string {
	canonical := Digest(p)
	if p.rawSHA256 != "" && p.canonicalSHA256 == canonical {
		return p.rawSHA256
	}
	return canonical
}

type Binding struct {
	ID                  string   `json:"id"`
	PoolID              string   `json:"poolId"`
	Provider            string   `json:"provider"`
	Service             string   `json:"service"`
	Host                string   `json:"host"`
	HostVersion         string   `json:"hostVersion"`
	AuthMode            string   `json:"authMode"`
	SourceID            string   `json:"sourceId"`
	StreamID            string   `json:"streamId"`
	ConfigurationIDs    []string `json:"configurationIds"`
	ConstraintIDs       []string `json:"constraintIds"`
	ConstraintsComplete bool     `json:"constraintsComplete"`
}
type Attribution struct {
	ConfigurationID *string              `json:"configurationId"`
	CatalogSHA256   *string              `json:"catalogSha256"`
	Provider        *string              `json:"provider"`
	Model           *string              `json:"model"`
	ProjectID       *string              `json:"projectId"`
	WorkItemRef     *contracts.Reference `json:"workItemRef"`
	RunRef          *contracts.Reference `json:"runRef"`
	AttemptID       *string              `json:"attemptId"`
}
type Event struct {
	ID             string      `json:"id"`
	Origin         Origin      `json:"origin"`
	BindingID      string      `json:"bindingId"`
	SessionID      string      `json:"sessionId"`
	TurnID         string      `json:"turnId"`
	RequestID      string      `json:"requestId"`
	CallID         string      `json:"callId"`
	ParentCallID   *string     `json:"parentCallId"`
	Attribution    Attribution `json:"attribution"`
	Metric         string      `json:"metric"`
	Unit           string      `json:"unit"`
	Value          json.Number `json:"value"`
	Semantics      string      `json:"semantics"`
	CounterEpoch   string      `json:"counterEpoch"`
	Sequence       int64       `json:"sequence"`
	CorrectionOf   *string     `json:"correctionOf"`
	SubsetOf       *string     `json:"subsetOf"`
	Billable       bool        `json:"billable"`
	DedupeCoverage string      `json:"dedupeCoverage"`
}
type Constraint struct {
	ID         string `json:"id"`
	PoolID     string `json:"poolId"`
	Provider   string `json:"provider"`
	Service    string `json:"service"`
	LimitID    string `json:"limitId"`
	Metric     string `json:"metric"`
	Unit       string `json:"unit"`
	Scope      string `json:"scope"`
	WindowKind string `json:"windowKind"`
}
type Quota struct {
	ID               string       `json:"id"`
	Origin           Origin       `json:"origin"`
	Constraint       Constraint   `json:"constraint"`
	Generation       string       `json:"generation"`
	Revision         int64        `json:"revision"`
	Completeness     string       `json:"completeness"`
	Remaining        *json.Number `json:"remaining"`
	Used             *json.Number `json:"used"`
	Limit            *json.Number `json:"limit"`
	ResetAt          *string      `json:"resetAt"`
	ExpiresAt        string       `json:"expiresAt"`
	CoveredChargeIDs []string     `json:"coveredChargeIds"`
	Precision        string       `json:"precision"`
}
type Charge struct {
	ID            string      `json:"id"`
	ConstraintID  string      `json:"constraintId"`
	Generation    string      `json:"generation"`
	Unit          string      `json:"unit"`
	Amount        json.Number `json:"amount"`
	ReservationID *string     `json:"reservationId"`
}
type Amount struct {
	ConstraintID string      `json:"constraintId"`
	Generation   string      `json:"generation"`
	Unit         string      `json:"unit"`
	Amount       json.Number `json:"amount"`
}
type Demand struct {
	Version         string   `json:"version"`
	ConfigurationID string   `json:"configurationId"`
	ProfileSHA256   string   `json:"profileSha256"`
	Source          string   `json:"source"`
	ValidUntil      string   `json:"validUntil"`
	Amounts         []Amount `json:"amounts"`
}
type Reservation struct {
	ID               string   `json:"id"`
	Authority        string   `json:"authority"`
	AuthorityEpoch   string   `json:"authorityEpoch"`
	Owner            string   `json:"owner"`
	AttemptID        string   `json:"attemptId"`
	ConfigurationID  string   `json:"configurationId"`
	PolicySHA256     string   `json:"policySha256"`
	SnapshotSHA256   string   `json:"snapshotSha256"`
	ProfileSHA256    string   `json:"profileSha256"`
	DemandSHA256     string   `json:"demandSha256"`
	DemandValidUntil string   `json:"demandValidUntil"`
	Fence            int64    `json:"fence"`
	LeaseUntil       string   `json:"leaseUntil"`
	State            string   `json:"state"`
	Amounts          []Amount `json:"amounts"`
}
type Asset struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}
type Export struct {
	Version        string        `json:"version"`
	LedgerRevision int64         `json:"ledgerRevision"`
	PolicySHA256   string        `json:"policySha256"`
	Bindings       []Binding     `json:"bindings"`
	Events         []Event       `json:"events"`
	Quotas         []Quota       `json:"quotas"`
	Charges        []Charge      `json:"charges"`
	Reservations   []Reservation `json:"reservations"`
	Evidence       []Asset       `json:"evidence"`
}
type Diagnostic struct {
	Code    string `json:"code"`
	Field   string `json:"field,omitempty"`
	Message string `json:"message"`
}
type Report struct {
	Status               string       `json:"status"`
	Diagnostics          []Diagnostic `json:"diagnostics"`
	AuthorizationGranted bool         `json:"authorizationGranted"`
	ExecutionReady       bool         `json:"executionReady"`
	LiveCollectorGate    string       `json:"liveCollectorGate"`
	RuntimeGate          string       `json:"runtimeGate"`
}
type Total struct {
	Group    string      `json:"group"`
	Metric   string      `json:"metric"`
	Unit     string      `json:"unit"`
	Value    json.Number `json:"value"`
	Coverage string      `json:"coverage"`
}
type Summary struct {
	Report
	AsOf         string  `json:"asOf"`
	Totals       []Total `json:"totals"`
	Estimates    []Total `json:"estimates"`
	Unattributed int     `json:"unattributed"`
	Excluded     int     `json:"excluded"`
}
type Balance struct {
	Constraint        Constraint   `json:"constraint"`
	Generation        string       `json:"generation"`
	QuotaID           string       `json:"quotaId"`
	Status            string       `json:"status"`
	ReportedRemaining *json.Number `json:"reportedRemaining"`
	PendingCharges    json.Number  `json:"pendingCharges"`
	Held              json.Number  `json:"held"`
	SafetyMargin      json.Number  `json:"safetyMargin"`
	Headroom          *json.Number `json:"headroom"`
	Overdrawn         json.Number  `json:"overdrawn"`
	Demand            *json.Number `json:"demand"`
	ResetAt           *string      `json:"resetAt"`
	Conservative      bool         `json:"conservative"`
}
type CapacitySnapshot struct {
	Report
	Version         string    `json:"version"`
	AsOf            string    `json:"asOf"`
	LedgerRevision  int64     `json:"ledgerRevision"`
	ExportSHA256    string    `json:"exportSha256"`
	PolicySHA256    string    `json:"policySha256"`
	DemandSHA256    string    `json:"demandSha256"`
	ConfigurationID string    `json:"configurationId"`
	Disposition     string    `json:"disposition"`
	Balances        []Balance `json:"balances"`
	Digest          string    `json:"digest"`
}
type CollectorDescriptor struct {
	Host           string `json:"host"`
	AdapterVersion string `json:"adapterVersion"`
	Usage          string `json:"usage"`
	Capacity       string `json:"capacity"`
	Streaming      string `json:"streaming"`
	Identity       string `json:"identity"`
	LiveGate       string `json:"liveGate"`
	Notes          string `json:"notes"`
}

func Hash(b []byte) string             { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func Digest(v any) string              { b, _ := json.Marshal(v); return Hash(b) }
func ConstraintID(c Constraint) string { c.ID = ""; return Digest(c) }
func BaseReport() Report {
	return Report{Status: "valid", Diagnostics: []Diagnostic{}, LiveCollectorGate: "pending", RuntimeGate: "pending-p6-p7"}
}
func EmptyExport() Export {
	return Export{Version: Version, Bindings: []Binding{}, Events: []Event{}, Quotas: []Quota{}, Charges: []Charge{}, Reservations: []Reservation{}, Evidence: []Asset{}}
}
