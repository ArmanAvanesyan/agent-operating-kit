// Package selection makes deterministic offline recommendations, never grants execution.
package selection

import (
	"encoding/json"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/evaluation"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/modelcatalog"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
	"time"
)

const Version = "aok-selection/v1"

type Asset struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}
type TaskProfile struct {
	Version        string                     `json:"version"`
	Assignment     Asset                      `json:"assignment"`
	Packet         Asset                      `json:"packet"`
	InputRevision  Asset                      `json:"inputRevision"`
	TaskClass      string                     `json:"taskClass"`
	Language       string                     `json:"language"`
	Risk           string                     `json:"risk"`
	Requirements   []modelcatalog.Requirement `json:"requirements"`
	NamedTools     []string                   `json:"namedTools"`
	Regions        []string                   `json:"regions"`
	Permissions    []string                   `json:"permissions"`
	ContextTokens  json.Number                `json:"contextTokens"`
	OutputTokens   json.Number                `json:"outputTokens"`
	Deadline       string                     `json:"deadline"`
	SpendLimit     json.Number                `json:"spendLimit"`
	Currency       string                     `json:"currency"`
	ProtocolSHA256 string                     `json:"protocolSha256"`
	MidRun         bool                       `json:"midRun"`
	SafeCheckpoint bool                       `json:"safeCheckpoint"`
}
type ContextObservation struct {
	CoverageComplete    bool     `json:"coverageComplete"`
	BindingSHA256       string   `json:"bindingSha256"`
	AssignmentSHA256    string   `json:"assignmentSha256"`
	TaskSHA256          string   `json:"taskSha256"`
	InputRevisionSHA256 string   `json:"inputRevisionSha256"`
	ConfigurationID     string   `json:"configurationId"`
	ProfileSHA256       string   `json:"profileSha256"`
	AccountScope        string   `json:"accountScope"`
	Dimension           string   `json:"dimension"`
	Values              []string `json:"values"`
	ObservedAt          string   `json:"observedAt"`
	ExpiresAt           string   `json:"expiresAt"`
	Issuer              string   `json:"issuer"`
	Signature           string   `json:"signature"`
}
type Candidate struct {
	Assignment      *Asset               `json:"assignment,omitempty"`
	ConfigurationID string               `json:"configurationId"`
	Profile         Asset                `json:"profile"`
	CatalogSHA256   string               `json:"catalogSha256"`
	AccountScope    string               `json:"accountScope"`
	BindingIDs      []string             `json:"bindingIds"`
	Context         []ContextObservation `json:"context"`
}
type CandidateManifest struct {
	Version          string      `json:"version"`
	Candidates       []Candidate `json:"candidates"`
	OutcomeShards    []string    `json:"outcomeShards"`
	PreviousDecision *Asset      `json:"previousDecision"`
}
type Route struct {
	ConfigurationID string `json:"configurationId"`
	AccountScope    string `json:"accountScope"`
}
type Edge struct {
	From string `json:"from"`
	To   string `json:"to"`
}
type SelectionPolicy struct {
	UpperBounds     *[]ExplicitUpperBound `json:"upperBounds,omitempty"`
	Version         string                `json:"version"`
	DefaultsVersion string                `json:"defaultsVersion"`
	Routes          []Route               `json:"routes"`
	Priority        string                `json:"priority"`
	StandardFloor   json.Number           `json:"standardFloor"`
	HighRiskFloor   json.Number           `json:"highRiskFloor"`
	Fallback        []Edge                `json:"fallback"`
	Pin             string                `json:"pin"`
	WaiveHysteresis bool                  `json:"waiveHysteresis"`
}
type Gate struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Reason string `json:"reason"`
}
type Rank struct {
	Quality   json.Number `json:"quality"`
	CostIndex json.Number `json:"costIndex"`
	TimeIndex json.Number `json:"timeIndex"`
	Scarcity  json.Number `json:"scarcity"`
}
type CandidateResult struct {
	ConfigurationID string                       `json:"configurationId"`
	AccountScope    string                       `json:"accountScope"`
	ProfileSHA256   string                       `json:"profileSha256"`
	Gates           []Gate                       `json:"gates"`
	Compatibility   modelcatalog.CheckReport     `json:"compatibility"`
	Requirements    []modelcatalog.Finding       `json:"requirements"`
	Estimate        *evaluation.EstimateArtifact `json:"estimate"`
	Capacity        *usage.CapacitySnapshot      `json:"capacity"`
	Rank            *Rank                        `json:"rank"`
	Eligible        bool                         `json:"eligible"`
	Dominated       bool                         `json:"dominated"`
}
type SelectionDecision struct {
	Version              string            `json:"version"`
	AsOf                 string            `json:"asOf"`
	ValidUntil           string            `json:"validUntil"`
	Inputs               map[string]string `json:"inputs"`
	Candidates           []CandidateResult `json:"candidates"`
	Disposition          string            `json:"disposition"`
	ConfigurationID      string            `json:"configurationId"`
	AccountScope         string            `json:"accountScope"`
	WaitUntil            *string           `json:"waitUntil"`
	Explanation          []string          `json:"explanation"`
	NextEvidence         []string          `json:"nextEvidence"`
	AuthorizationGranted bool              `json:"authorizationGranted"`
	ExecutionReady       bool              `json:"executionReady"`
	Digest               string            `json:"digest"`
}
type Request struct {
	TaskPath, CandidatesPath, CatalogPath, CatalogPolicyPath, UsagePath, UsagePolicyPath, PolicyPath, EvidencePolicyPath string
	AsOf                                                                                                                 time.Time
}

// ExplicitUpperBound is an externally approved policy bound, not a measurement.
// Its source asset is captured and hashed; it cannot replace quality evidence.
type ExplicitUpperBound struct {
	Cohort       evaluation.Cohort  `json:"cohort"`
	Source       Asset              `json:"source"`
	ObservedAt   string             `json:"observedAt"`
	ValidUntil   string             `json:"validUntil"`
	CostUpper    *json.Number       `json:"costUpper"`
	Currency     string             `json:"currency"`
	SecondsUpper *json.Number       `json:"secondsUpper"`
	Bounds       []evaluation.Bound `json:"bounds"`
}
