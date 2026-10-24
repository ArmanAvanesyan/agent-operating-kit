// Package readiness evaluates a closed, offline readiness snapshot. It does
// not execute work or grant launch/publication authority.
package readiness

import "time"

const Version = "aok-readiness/v1"
const MaxDocumentBytes = 4 << 20
const MaxEvidenceBytes = 64 << 20

type RouteManifest struct {
	Version             string `json:"version"`
	ID                  string `json:"id"`
	Host                string `json:"host"`
	HostVersion         string `json:"hostVersion"`
	HostSHA256          string `json:"hostSha256"`
	OS                  string `json:"os"`
	Arch                string `json:"arch"`
	Transport           string `json:"transport"`
	ControlPolicySHA256 string `json:"controlPolicySha256"`
	ConfigurationSHA256 string `json:"configurationSha256"`
	ProfileSHA256       string `json:"profileSha256"`
	Account             string `json:"account"`
	AuthMode            string `json:"authMode"`
	Pool                string `json:"pool"`
	VerifierSHA256      string `json:"verifierSha256"`
	RubricSHA256        string `json:"rubricSha256"`
	EnvironmentSHA256   string `json:"environmentSha256"`
	Provider            string `json:"provider,omitempty"`
	Origin              string `json:"origin,omitempty"`
	NativeTenant        string `json:"nativeTenant,omitempty"`
	Container           string `json:"container,omitempty"`
	ItemKind            string `json:"itemKind,omitempty"`
	Operation           string `json:"operation,omitempty"`
	API                 string `json:"api,omitempty"`
	ProviderAuth        string `json:"providerAuth,omitempty"`
	DeliveryMilestone   string `json:"deliveryMilestone,omitempty"`
}

type GateRequirement struct {
	ID             string            `json:"id"`
	Validator      string            `json:"validator"`
	EvidenceOrigin string            `json:"evidenceOrigin"` // fixture, live, or either
	Applicability  string            `json:"applicability"`  // applicable or not-applicable under exact route scope
	Required       bool              `json:"required"`
	Scope          map[string]string `json:"scope"`
	Bindings       []EvidenceBinding `json:"bindings"`
}

type EvidenceBinding struct {
	ID         string            `json:"id"`
	Path       string            `json:"path"`
	SHA256     string            `json:"sha256"`
	Issuer     string            `json:"issuer"`
	Role       string            `json:"role"`
	ObservedAt string            `json:"observedAt"`
	ValidUntil string            `json:"validUntil"`
	Revoked    bool              `json:"revoked"`
	Supersedes []string          `json:"supersedes,omitempty"`
	Origin     string            `json:"origin"` // fixture or live; classification is validated by adapters.
	Scope      map[string]string `json:"scope"`
}

// Policy is supplied separately from the route and evidence. Its trust must
// come from the caller's protected policy-selection boundary; a digest pin is
// an integrity check and does not itself prove authorship.
type Policy struct {
	Version               string            `json:"version"`
	ID                    string            `json:"id"`
	Requirements          []GateRequirement `json:"requirements"`
	MaxEvidenceAgeSeconds int64             `json:"maxEvidenceAgeSeconds"`
}

type PolicyPin struct {
	SHA256    string
	Principal string
	Role      string
}

// PolicyAuthorityVerifier resolves identity and authority through an external
// protected trust boundary. A digest pin alone only checks integrity.
type PolicyAuthorityVerifier interface{ VerifyPolicyAuthority(Policy, PolicyPin) error }
type PolicyAuthorityFunc func(Policy, PolicyPin) error

func (f PolicyAuthorityFunc) VerifyPolicyAuthority(p Policy, pin PolicyPin) error { return f(p, pin) }

type ValidationInput struct {
	Requirement   GateRequirement
	Route         RouteManifest
	EvidenceRoot  string
	Evidence      []EvidenceBinding
	EvidenceBytes map[string][]byte
	AsOf          time.Time
}
type Inputs struct {
	EvidenceRoot    string
	PolicyPin       PolicyPin
	PolicyAuthority PolicyAuthorityVerifier
}

// NativeValidator must validate the evidence using the native P2-P8
// validator, including signature, scope, provenance, revocation and origin.
// A binding's own fields are never sufficient to produce passed.
type NativeValidator interface {
	Validate(ValidationInput) ValidationResult
}
type NativeValidatorFunc func(ValidationInput) ValidationResult

func (f NativeValidatorFunc) Validate(i ValidationInput) ValidationResult { return f(i) }

type Adapters map[string]NativeValidator
type ValidationResult struct {
	Status         string   `json:"status"`
	Reason         string   `json:"reason"`
	EvidenceSHA256 []string `json:"evidenceSha256"`
	ValidUntil     string   `json:"validUntil,omitempty"`
}
type GateResult struct {
	ID             string   `json:"id"`
	Required       bool     `json:"required"`
	Status         string   `json:"status"`
	Reason         string   `json:"reason"`
	EvidenceSHA256 []string `json:"evidenceSha256"`
}
type Report struct {
	Version              string       `json:"version"`
	ManifestSHA256       string       `json:"manifestSha256"`
	PolicySHA256         string       `json:"policySha256"`
	AsOf                 string       `json:"asOf"`
	InputSHA256s         []string     `json:"inputSha256s"`
	Gates                []GateResult `json:"gates"`
	Status               string       `json:"status"`
	PermittedClaim       string       `json:"permittedClaim"`
	AuthorizationGranted bool         `json:"authorizationGranted"`
	ExecutionReady       bool         `json:"executionReady"`
	EarliestValidUntil   string       `json:"earliestValidUntil,omitempty"`
	NextActions          []string     `json:"nextActions"`
}
