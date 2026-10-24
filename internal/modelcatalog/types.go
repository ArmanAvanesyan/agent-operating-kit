// Package modelcatalog implements an explicit-input, offline model evidence
// catalog. Catalog facts never grant authority or prove runtime enforcement.
package modelcatalog

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
)

const Version = "aok-model-catalog/v1"
const ManifestVersion = "aok-model-manifest/v1"
const PolicyVersion = "aok-model-policy/v1"
const ProbeVersion = "aok-model-probe/v1"
const ImporterVersion = "1.0.0"

const Published = "published-declaration"
const Listed = "observed-cli-listing"
const Invoked = "successful-invocation"
const Evaluated = "reproduced-evaluation"
const Derived = "repository-guidance"

type Asset struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}
type SourceInput struct {
	ID       string  `json:"id"`
	Format   string  `json:"format"`
	Artifact Asset   `json:"artifact"`
	Evidence []Asset `json:"evidence"`
}
type Mapping struct {
	SourceID      string  `json:"sourceId"`
	SourceModelID string  `json:"sourceModelId"`
	Selector      string  `json:"selector"`
	Provider      *string `json:"provider"`
	Pointer       string  `json:"pointer"`
}
type Manifest struct {
	Version  string        `json:"version"`
	Sources  []SourceInput `json:"sources"`
	Mappings []Mapping     `json:"mappings"`
}
type SourceRule struct {
	ID             string   `json:"id"`
	ArtifactSHA256 string   `json:"artifactSha256"`
	Categories     []string `json:"categories"`
}
type Policy struct {
	Version       string         `json:"version"`
	Sources       []SourceRule   `json:"sources"`
	ValidityHours map[string]int `json:"validityHours"`
}

// Configuration is a complete structural identity. Nil is explicitly unknown,
// never a wildcard, a default provider or an inferred model revision.
type Configuration struct {
	Host                  *string                       `json:"host"`
	HostVersion           *string                       `json:"hostVersion"`
	Provider              *string                       `json:"provider"`
	ModelID               *string                       `json:"modelId"`
	ModelRevision         *string                       `json:"modelRevision"`
	ReasoningMode         *string                       `json:"reasoningMode"`
	Effort                *string                       `json:"effort"`
	ReasoningBudgetTokens *json.Number                  `json:"reasoningBudgetTokens"`
	Generation            *contracts.GenerationSettings `json:"generation"`
	ServiceTier           *string                       `json:"serviceTier"`
	AuthMode              *string                       `json:"authMode"`
	Deployment            *string                       `json:"deployment"`
	ToolProfile           *string                       `json:"toolProfile"`
}
type ConfigurationRecord struct {
	ID       string        `json:"id"`
	Identity Configuration `json:"identity"`
}
type Source struct {
	ID         string   `json:"id"`
	Format     string   `json:"format"`
	ObservedAt string   `json:"observedAt"`
	URLs       []string `json:"urls"`
	Artifact   Asset    `json:"artifact"`
	Evidence   []Asset  `json:"evidence"`
}
type Entry struct {
	ID               string   `json:"id"`
	SourceID         string   `json:"sourceId"`
	SourceModelID    string   `json:"sourceModelId"`
	Name             string   `json:"name"`
	Selector         *string  `json:"selector"`
	Aliases          []string `json:"aliases"`
	ConfigurationID  string   `json:"configurationId"`
	ScheduledRemoval *string  `json:"scheduledRemoval"`
}
type Assertion struct {
	ID         string          `json:"id"`
	EntryID    string          `json:"entryId"`
	SourceID   string          `json:"sourceId"`
	Pointer    string          `json:"pointer"`
	Dimension  string          `json:"dimension"`
	Value      json.RawMessage `json:"value"`
	Category   string          `json:"category"`
	Scope      Configuration   `json:"scope"`
	ObservedAt string          `json:"observedAt"`
	ExpiresAt  string          `json:"expiresAt"`
	Polarity   string          `json:"polarity"`
}
type Guidance struct {
	ID         string          `json:"id"`
	SourceID   string          `json:"sourceId"`
	Pointer    string          `json:"pointer"`
	Category   string          `json:"category"`
	ObservedAt string          `json:"observedAt"`
	ExpiresAt  string          `json:"expiresAt"`
	Content    json.RawMessage `json:"content"`
}
type ProbeObservation struct {
	Dimension string          `json:"dimension"`
	Value     json.RawMessage `json:"value"`
}

// ProbeEvidence is a redacted, supplied observation, not an executable request.
// EvidenceSHA256 is a declared reference to externally retained redacted
// evidence; it does not prove authorship and is never treated as raw evidence.
type ProbeEvidence struct {
	Version        string             `json:"version"`
	ID             string             `json:"id"`
	Configuration  Configuration      `json:"configuration"`
	ProfileSHA256  string             `json:"profileSha256"`
	ObservedAt     string             `json:"observedAt"`
	AccountScope   string             `json:"accountScope"`
	Outcome        string             `json:"outcome"`
	Observations   []ProbeObservation `json:"observations"`
	EvidenceSHA256 string             `json:"evidenceSha256"`
}
type Catalog struct {
	Version         string                `json:"version"`
	ImporterVersion string                `json:"importerVersion"`
	Sources         []Source              `json:"sources"`
	Entries         []Entry               `json:"entries"`
	Configurations  []ConfigurationRecord `json:"configurations"`
	Assertions      []Assertion           `json:"assertions"`
	Guidance        []Guidance            `json:"guidance"`
	Probes          []ProbeEvidence       `json:"probes"`
}

// Capture owns explicit captured bytes. Callers must not re-read source files
// after loading when constructing an evidence-bound result.
type Capture struct {
	validationSeal string
	// PolicyExternal is set only when the caller explicitly supplies a policy.
	// Captured bundle policy alone supports descriptive inspection, not checks.
	PolicyExternal bool
	Catalog        Catalog
	Policy         Policy
	Files          map[string][]byte
	CatalogSHA256  string
	PolicySHA256   string
}
type Report struct {
	Status               string                 `json:"status"`
	Diagnostics          []contracts.Diagnostic `json:"diagnostics"`
	Counts               map[string]int         `json:"counts,omitempty"`
	AuthorizationGranted bool                   `json:"authorizationGranted"`
	ExecutionReady       bool                   `json:"executionReady"`
	ProbeGate            string                 `json:"probeGate"`
}
type Finding struct {
	Field        string         `json:"field"`
	Status       string         `json:"status"`
	Message      string         `json:"message"`
	AssertionIDs []string       `json:"assertionIds"`
	Category     string         `json:"category,omitempty"`
	Scope        *Configuration `json:"scope,omitempty"`
}
type CheckReport struct {
	Report
	Compatibility string    `json:"compatibility"`
	Findings      []Finding `json:"findings"`
}
type Inspection struct {
	Report
	AsOf      string            `json:"asOf"`
	Freshness map[string]string `json:"freshness"`
	Catalog   Catalog           `json:"catalog"`
}
type Change struct {
	Category string `json:"category"`
	ID       string `json:"id"`
	Change   string `json:"change"`
}
type DiffReport struct {
	Report
	Changes []Change `json:"changes"`
}

func String(s string) *string                { return &s }
func Hash(data []byte) string                { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func ConfigurationID(c Configuration) string { b, _ := json.Marshal(c); return Hash(b) }
func DefaultValidityHours() map[string]int {
	return map[string]int{Listed: 168, Published: 720, "pricing": 24, Derived: 2160, Invoked: 24, Evaluated: 2160}
}
