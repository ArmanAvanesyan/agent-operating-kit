// Package accessv1 contains opt-in operational tool/auth contracts. Historical
// workkit/v1alpha1 and workkit-read/v1 documents are not rewritten.
package accessv1

import "encoding/json"

const Version = "workkit-access/v1"

type LocalPin struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}
type ToolPin struct {
	Runtime       *LocalPin   `json:"runtime,omitempty"`
	Configuration *LocalPin   `json:"configuration,omitempty"`
	ID            string      `json:"id"`
	Path          string      `json:"path"`
	SHA256        string      `json:"sha256"`
	VersionOutput string      `json:"versionOutput"`
	Platform      string      `json:"platform"`
	Extensions    []Extension `json:"extensions"`
}
type Extension struct {
	Layout  string `json:"layout,omitempty"`
	ID      string `json:"id"`
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
	Path    string `json:"path"`
}
type Observation struct {
	Version           string  `json:"version"`
	Kind              string  `json:"kind"`
	Tool              ToolPin `json:"tool"`
	ObservedAt        string  `json:"observedAt"`
	Verified          bool    `json:"verified"`
	LiveQualification string  `json:"liveQualification"`
}
type Artifact struct {
	Layout           string   `json:"layout,omitempty"`
	RedirectOrigins  []string `json:"redirectOrigins"`
	ExecutableSHA256 string   `json:"executableSha256"`
	ToolID           string   `json:"toolId"`
	ToolVersion      string   `json:"toolVersion"`
	Platform         string   `json:"platform"`
	URL              string   `json:"url"`
	SHA256           string   `json:"sha256"`
	Format           string   `json:"format"`
	Executable       string   `json:"executable"`
	MaxBytes         int64    `json:"maxBytes"`
	VersionOutput    string   `json:"versionOutput"`
}
type InstallPlan struct {
	PreviousPath         string   `json:"previousPath"`
	Version              string   `json:"version"`
	Kind                 string   `json:"kind"`
	PolicySHA256         string   `json:"policySha256"`
	Artifact             Artifact `json:"artifact"`
	Destination          string   `json:"destination"`
	Action               string   `json:"action"`
	PreviousSHA256       string   `json:"previousSha256"`
	ObservedAt           string   `json:"observedAt"`
	AuthorizationGranted bool     `json:"authorizationGranted"`
	ExecutionReady       bool     `json:"executionReady"`
}
type Issuer struct {
	ID        string `json:"id"`
	PublicKey string `json:"publicKey"`
}
type Policy struct {
	LedgerRoot         string     `json:"ledgerRoot"`
	Version            string     `json:"version"`
	Kind               string     `json:"kind"`
	Issuers            []Issuer   `json:"issuers"`
	Artifacts          []Artifact `json:"artifacts"`
	AllowedConnections []string   `json:"allowedConnections"`
	AllowedActions     []string   `json:"allowedActions"`
	RevokedOperations  []string   `json:"revokedOperations"`
	NotBefore          string     `json:"notBefore"`
	ExpiresAt          string     `json:"expiresAt"`
}
type Grant struct {
	Version              string `json:"version"`
	Kind                 string `json:"kind"`
	OperationID          string `json:"operationId"`
	Action               string `json:"action"`
	RequestSHA256        string `json:"requestSha256"`
	PolicySHA256         string `json:"policySha256"`
	ImplementationSHA256 string `json:"implementationSha256"`
	Output               string `json:"output"`
	Issuer               string `json:"issuer"`
	IssuedAt             string `json:"issuedAt"`
	ExpiresAt            string `json:"expiresAt"`
	Signature            string `json:"signature"`
}
type Configuration struct {
	Read             *ReadInputs       `json:"read,omitempty"`
	Native           *NativeEnrollment `json:"native,omitempty"`
	ProviderLookup   string            `json:"providerLookup"`
	Version          string            `json:"version"`
	Kind             string            `json:"kind"`
	ConnectionID     string            `json:"connectionId"`
	Profile          json.RawMessage   `json:"profile"`
	Provider         string            `json:"provider"`
	Origin           string            `json:"origin"`
	BasePath         string            `json:"basePath"`
	NativePrincipal  string            `json:"nativePrincipal"`
	TenantID         string            `json:"tenantId"`
	Containers       []string          `json:"containers"`
	Mode             string            `json:"mode"`
	SecretReferences map[string]string `json:"secretReferences"`
	Tool             *ToolPin          `json:"tool,omitempty"`
	Broker           *ToolPin          `json:"broker,omitempty"`
	OAuth            *OAuth            `json:"oauth,omitempty"`
	TLS              *TLS              `json:"tls,omitempty"`
}
type TLS struct {
	CAPath        string `json:"caPath"`
	CASHA256      string `json:"caSha256"`
	ServerVersion string `json:"serverVersion"`
}
type OAuth struct {
	ClientID         string   `json:"clientId"`
	AuthorizationURL string   `json:"authorizationUrl"`
	TokenURL         string   `json:"tokenUrl"`
	RevokeURL        string   `json:"revokeUrl"`
	RedirectURI      string   `json:"redirectUri"`
	Scopes           []string `json:"scopes"`
}
type Binding struct {
	ProviderInteraction  string `json:"providerInteraction"`
	EvidenceTrust        string `json:"evidenceTrust"`
	Version              string `json:"version"`
	Kind                 string `json:"kind"`
	ConfigurationSHA256  string `json:"configurationSha256"`
	ConnectionID         string `json:"connectionId"`
	Principal            string `json:"principal"`
	TenantID             string `json:"tenantId"`
	Mode                 string `json:"mode"`
	Generation           string `json:"generation"`
	Status               string `json:"status"`
	ObservedAt           string `json:"observedAt"`
	StorageMode          string `json:"storageMode"`
	ScopedReadVerified   bool   `json:"scopedReadVerified"`
	LiveQualification    string `json:"liveQualification"`
	AuthorizationGranted bool   `json:"authorizationGranted"`
	ExecutionReady       bool   `json:"executionReady"`
}
type Receipt struct {
	Tool                 *ToolPin   `json:"tool,omitempty"`
	Extension            *Extension `json:"extension,omitempty"`
	Version              string     `json:"version"`
	Kind                 string     `json:"kind"`
	OperationID          string     `json:"operationId"`
	Action               string     `json:"action"`
	RequestSHA256        string     `json:"requestSha256"`
	PolicySHA256         string     `json:"policySha256"`
	GrantSHA256          string     `json:"grantSha256"`
	ImplementationSHA256 string     `json:"implementationSha256"`
	StartedAt            string     `json:"startedAt"`
	FinishedAt           string     `json:"finishedAt"`
	Status               string     `json:"status"`
	LocalEffect          string     `json:"localEffect"`
	ProviderEffect       string     `json:"providerEffect"`
	Diagnostic           string     `json:"diagnostic"`
	Binding              *Binding   `json:"binding,omitempty"`
	MigrationActivation  string     `json:"migrationActivation"`
	LiveQualification    string     `json:"liveQualification"`
	ProductionActivation string     `json:"productionActivation"`
}

// Broker messages are private transient IPC. They are never bundle assets.
type BrokerRequest struct {
	ScopeSHA256        string            `json:"scopeSha256"`
	Version            string            `json:"version"`
	Operation          string            `json:"operation"`
	ConnectionID       string            `json:"connectionId"`
	ExpectedGeneration string            `json:"expectedGeneration"`
	Secrets            map[string]string `json:"secrets"`
}
type BrokerResponse struct {
	ScopeSHA256  string            `json:"scopeSha256"`
	Version      string            `json:"version"`
	ConnectionID string            `json:"connectionId"`
	Generation   string            `json:"generation"`
	StorageMode  string            `json:"storageMode"`
	Secrets      map[string]string `json:"secrets"`
}

// BrokerConfig is operator-local configuration. It never contains credentials.
type BrokerConfig struct {
	Version    string   `json:"version"`
	Kind       string   `json:"kind"`
	Namespace  string   `json:"namespace"`
	LedgerRoot string   `json:"ledgerRoot"`
	Store      string   `json:"store"`
	Helper     *ToolPin `json:"helper,omitempty"`
	BusAddress string   `json:"busAddress"`
}

type ReadInputs struct {
	Configuration LocalPin `json:"configuration"`
	Policy        LocalPin `json:"policy"`
	Enrollment    LocalPin `json:"enrollment"`
	Output        string   `json:"output"`
}
type DiscoveryRequest struct {
	Version string   `json:"version"`
	Kind    string   `json:"kind"`
	Roots   []string `json:"roots"`
	Tools   []string `json:"tools"`
}
type DiscoveryFinding struct {
	ID       string `json:"id"`
	Path     string `json:"path"`
	SHA256   string `json:"sha256"`
	Platform string `json:"platform"`
	Status   string `json:"status"`
}
type ConnectionEntry struct {
	ID                  string `json:"id"`
	ConfigurationSHA256 string `json:"configurationSha256"`
	ConfigurationBase64 string `json:"configurationBase64"`
}
type ConnectionRegistry struct {
	Version             string            `json:"version"`
	Kind                string            `json:"kind"`
	Revision            int               `json:"revision"`
	PreviousSHA256      string            `json:"previousSha256"`
	ObservedAt          string            `json:"observedAt"`
	Connections         []ConnectionEntry `json:"connections"`
	MigrationActivated  bool              `json:"migrationActivated"`
	ProductionActivated bool              `json:"productionActivated"`
}
type AccessState struct {
	Version             string `json:"version"`
	Kind                string `json:"kind"`
	ConfigurationSHA256 string `json:"configurationSha256"`
	OperationID         string `json:"operationId"`
	Status              string `json:"status"`
	Generation          string `json:"generation"`
	ReceiptPath         string `json:"receiptPath"`
	ReceiptSHA256       string `json:"receiptSha256"`
}

// NativeEnrollment attests inherited external containment, never installs it.
// It is issued by the external policy authority for one operation/process.
type NativeEnrollment struct {
	ProcessStart         string            `json:"processStart"`
	CredentialMode       string            `json:"credentialMode"`
	StatusSHA256         string            `json:"statusSha256"`
	ProfileRoot          string            `json:"profileRoot"`
	ProfileSHA256        string            `json:"profileSha256"`
	ProcessID            int               `json:"processId"`
	ProcessSHA256        string            `json:"processSha256"`
	ToolSHA256           string            `json:"toolSha256"`
	ScopeSHA256          string            `json:"scopeSha256"`
	OperationID          string            `json:"operationId"`
	ImplementationSHA256 string            `json:"implementationSha256"`
	ObservedAt           string            `json:"observedAt"`
	ExpiresAt            string            `json:"expiresAt"`
	Controls             map[string]string `json:"controls"`
	Issuer               string            `json:"issuer"`
	Signature            string            `json:"signature"`
}
