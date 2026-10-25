// Package readv1 defines the independently versioned operational read protocol.
// These records cannot activate a binding, sender, qualification or production.
package readv1

import (
	"encoding/json"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/readsource"
)

const Version = "workkit-read/v1"
const AdapterVersion = "2.0.0"

type Configuration struct {
	Version      string            `json:"version"`
	ConnectionID string            `json:"connectionId"`
	Profile      json.RawMessage   `json:"profile"`
	Source       readsource.Source `json:"source"`
}
type Asset struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Role   string `json:"role"`
}
type TransportIdentity struct {
	Kind             string `json:"kind"`
	Version          string `json:"version"`
	ExecutableSHA256 string `json:"executableSha256"`
	Principal        string `json:"principal"`
	AuthMode         string `json:"authMode"`
}
type Manifest struct {
	Version              string                  `json:"version"`
	ConnectionID         string                  `json:"connectionId"`
	ConfigurationSHA256  string                  `json:"configurationSha256"`
	SourceSHA256         string                  `json:"sourceSha256"`
	ReadPolicySHA256     string                  `json:"readPolicySha256"`
	AdapterVersion       string                  `json:"adapterVersion"`
	Transport            TransportIdentity       `json:"transport"`
	OriginalObservedAt   *string                 `json:"originalObservedAt"`
	LastObservedAt       *string                 `json:"lastObservedAt"`
	PreviousBundleSHA256 string                  `json:"previousBundleSha256"`
	Mode                 string                  `json:"mode"`
	Complete             bool                    `json:"complete"`
	Assets               []Asset                 `json:"assets"`
	Diagnostics          []readsource.Diagnostic `json:"diagnostics"`
	AuthorizationGranted bool                    `json:"authorizationGranted"`
	ExecutionReady       bool                    `json:"executionReady"`
	MigrationActivated   bool                    `json:"migrationActivated"`
	LiveQualification    string                  `json:"liveQualification"`
	ProductionActivated  bool                    `json:"productionActivated"`
	ImplementationSHA256 string                  `json:"implementationSha256"`
}
type Identity struct {
	Provider    string `json:"provider"`
	Origin      string `json:"origin"`
	BasePath    string `json:"basePath"`
	TenantID    string `json:"tenantId"`
	ContainerID string `json:"containerId"`
	ItemKind    string `json:"itemKind"`
	NativeID    string `json:"nativeId"`
}
type Record struct {
	ConnectionID string          `json:"connectionId"`
	Principal    string          `json:"principal"`
	Identity     Identity        `json:"identity"`
	Data         readsource.Item `json:"data"`
}
type CatalogEntry struct {
	Name            string          `json:"name"`
	Data            json.RawMessage `json:"data"`
	AssetSHA256     string          `json:"assetSha256"`
	JSONPointer     string          `json:"jsonPointer"`
	ProvenanceKnown bool            `json:"provenanceKnown"`
}
type Field struct {
	Type           string            `json:"type"`
	ProviderFields map[string]string `json:"providerFields"`
}
type Query struct {
	Version       string           `json:"version"`
	Dialect       string           `json:"dialect"`
	ConnectionID  string           `json:"connectionId"`
	Expression    string           `json:"expression"`
	Fields        map[string]Field `json:"fields"`
	Limit         int              `json:"limit"`
	MaxAgeSeconds int              `json:"maxAgeSeconds"`
}
type Coverage struct {
	ConnectionID string  `json:"connectionId"`
	BundleSHA256 string  `json:"bundleSha256"`
	Principal    string  `json:"principal"`
	Status       string  `json:"status"`
	Complete     bool    `json:"complete"`
	Fresh        bool    `json:"fresh"`
	ObservedAt   *string `json:"observedAt"`
	Evaluated    int     `json:"evaluated"`
	Unknown      int     `json:"unknown"`
}
type QueryResult struct {
	Records              []Record   `json:"records"`
	Coverage             []Coverage `json:"coverage"`
	Unknown              int        `json:"unknown"`
	Matched              int        `json:"matched"`
	Returned             int        `json:"returned"`
	Exhaustive           bool       `json:"exhaustive"`
	Filtering            string     `json:"filtering"`
	AuthorizationGranted bool       `json:"authorizationGranted"`
	ExecutionReady       bool       `json:"executionReady"`
	LiveQualification    string     `json:"liveQualification"`
}
type OperationSupport struct {
	Name          string `json:"name"`
	Support       string `json:"support"`
	Qualification string `json:"qualification"`
}
type ProviderDescriptor struct {
	Provider             string             `json:"provider"`
	AdapterVersion       string             `json:"adapterVersion"`
	Transports           []string           `json:"transports"`
	Operations           []OperationSupport `json:"operations"`
	ImplementationSHA256 string             `json:"implementationSha256"`
}
type ReadEnrollment struct {
	Version               string            `json:"version"`
	ConfigurationSHA256   string            `json:"configurationSha256"`
	ReadPolicySHA256      string            `json:"readPolicySha256"`
	Transport             TransportIdentity `json:"transport"`
	Executable            string            `json:"executable"`
	GitHubConfigDirectory string            `json:"githubConfigDirectory"`
	SecretEnvironment     map[string]string `json:"secretEnvironment"`
	TLSRootsPath          string            `json:"tlsRootsPath"`
	IssuedAt              string            `json:"issuedAt"`
	ValidUntil            string            `json:"validUntil"`
	RequestSeconds        int               `json:"requestSeconds"`
	CommandSeconds        int               `json:"commandSeconds"`
	QualificationOnly     bool              `json:"qualificationOnly"`
	Issuer                string            `json:"issuer"`
	Signature             string            `json:"signature"`
	NativePrincipalID     string            `json:"nativePrincipalId"`
	GitHubConfigSHA256    string            `json:"githubConfigSha256"`
	OutputPath            string            `json:"outputPath"`
	PreviousBundleSHA256  string            `json:"previousBundleSha256"`
	Action                string            `json:"action"`
	ImplementationSHA256  string            `json:"implementationSha256"`
}

type Issuer struct {
	ID        string `json:"id"`
	PublicKey string `json:"publicKey"`
}
type AccessPolicy struct {
	Version          string          `json:"version"`
	LegacyReadPolicy json.RawMessage `json:"legacyReadPolicy"`
	Issuers          []Issuer        `json:"issuers"`
}
