// Package tracker captures external work items and prepares local, non-authorizing drafts.
package readsource

import (
	"context"
	"encoding/json"
)

const Version = "aok-tracker/v1"
const Version2 = "aok-tracker/v2"
const AdapterVersion = "1.0.0"

type Auth struct {
	Mode        string `json:"mode"`
	TokenEnv    string `json:"tokenEnv"`
	UsernameEnv string `json:"usernameEnv"`
}
type Scope struct {
	ItemIDs   []string `json:"itemIds"`
	Query     string   `json:"query"`
	Comments  bool     `json:"comments"`
	Relations bool     `json:"relations"`
	Projects  bool     `json:"projects"`
}
type Limits struct {
	RequestSeconds int `json:"requestSeconds"`
	CommandSeconds int `json:"commandSeconds"`
	Requests       int `json:"requests"`
	Items          int `json:"items"`
	ResponseBytes  int `json:"responseBytes"`
	TotalBytes     int `json:"totalBytes"`
}

// Deployment pins a Data Center installation independently of Cloud identity.
type Deployment struct {
	BasePath       string `json:"basePath"`
	ServerVersion  string `json:"serverVersion"`
	TLSTrustSHA256 string `json:"tlsTrustSha256"`
}
type Source struct {
	Deployment         *Deployment `json:"deployment,omitempty"`
	ReadPolicySHA256   string      `json:"readPolicySha256,omitempty"`
	Version            string      `json:"version"`
	Provider           string      `json:"provider"`
	Origin             string      `json:"origin"`
	APIVersion         string      `json:"apiVersion"`
	NativeTenantID     string      `json:"nativeTenantId"`
	ContainerID        string      `json:"containerId"`
	ContainerKey       string      `json:"containerKey"`
	PrincipalPseudonym string      `json:"principalPseudonym"`
	Auth               Auth        `json:"auth"`
	Scope              Scope       `json:"scope"`
	MappingVersion     string      `json:"mappingVersion"`
	Limits             *Limits     `json:"limits,omitempty"`
}
type Diagnostic struct {
	Code    string `json:"code"`
	Field   string `json:"field"`
	Message string `json:"message"`
}
type Report struct {
	Version              string       `json:"version"`
	Status               string       `json:"status"`
	Diagnostics          []Diagnostic `json:"diagnostics"`
	AuthorizationGranted bool         `json:"authorizationGranted"`
	ExecutionReady       bool         `json:"executionReady"`
	LiveReadGate         string       `json:"liveReadGate"`
	WritebackGate        string       `json:"writebackGate"`
}
type Request struct {
	baseScopeValidated bool
	Method             string          `json:"method"`
	Path               string          `json:"path"`
	Body               json.RawMessage `json:"body"`
	Kind               string          `json:"kind"`
	ItemID             string          `json:"itemId"`
}
type Response struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
	Body    []byte            `json:"-"`
}
type Transport interface {
	Do(context.Context, Source, Request) (Response, error)
}
type Relation struct {
	Type      string `json:"type"`
	TargetID  string `json:"targetId"`
	URL       string `json:"url"`
	Supported bool   `json:"supported"`
}
type Comment struct {
	ID        string `json:"id"`
	Body      string `json:"body"`
	UpdatedAt string `json:"updatedAt"`
}
type Transition struct {
	ID                string   `json:"id"`
	Name              string   `json:"name"`
	RequiredFields    []string `json:"requiredFields"`
	RequirementsKnown bool     `json:"requirementsKnown"`
}
type Pointer struct {
	AssetSHA256 string `json:"assetSha256"`
	JSONPointer string `json:"jsonPointer"`
}
type Item struct {
	Identity       RemoteIdentity             `json:"identity"`
	NativeTenantID string                     `json:"nativeTenantId"`
	DisplayKey     string                     `json:"displayKey"`
	URL            string                     `json:"url"`
	Revision       string                     `json:"revision"`
	RevisionKind   string                     `json:"revisionKind"`
	UpdatedAt      string                     `json:"updatedAt"`
	Fields         map[string]json.RawMessage `json:"fields"`
	Relations      []Relation                 `json:"relations"`
	Comments       []Comment                  `json:"comments"`
	ProjectFields  map[string]json.RawMessage `json:"projectFields"`
	Transitions    []Transition               `json:"transitions"`
	Provenance     map[string][]Pointer       `json:"provenance"`
	LossyFields    []string                   `json:"lossyFields"`
	Complete       bool                       `json:"complete"`
}
type PageResult struct {
	Items       []Item
	Discovery   map[string]json.RawMessage
	Next        []Request
	Diagnostics []Diagnostic
	Complete    bool
}
type Adapter interface {
	Provider() string
	Initial(Source, string, *Checkpoint) ([]Request, error)
	Decode(Source, Request, Response) (PageResult, error)
}
type Page struct {
	Request    Request           `json:"request"`
	Status     int               `json:"status"`
	Headers    map[string]string `json:"headers"`
	Asset      AssetRef          `json:"asset"`
	ObservedAt string            `json:"observedAt"`
}
type Checkpoint struct {
	Version            string    `json:"version"`
	ScopeSHA256        string    `json:"scopeSha256"`
	Pending            []Request `json:"pending"`
	CompletedWatermark string    `json:"completedWatermark"`
	Complete           bool      `json:"complete"`
	Digest             string    `json:"digest"`
}
type Manifest struct {
	Version    string      `json:"version"`
	Source     Source      `json:"source"`
	Mode       string      `json:"mode"`
	Pages      []Page      `json:"pages"`
	Checkpoint *Checkpoint `json:"checkpoint,omitempty"`
}
type Snapshot struct {
	Version    string                     `json:"version"`
	Source     Source                     `json:"source"`
	Mode       string                     `json:"mode"`
	Pages      []Page                     `json:"pages"`
	Items      []Item                     `json:"items"`
	Discovery  map[string]json.RawMessage `json:"discovery"`
	Complete   bool                       `json:"complete"`
	ObservedAt string                     `json:"observedAt"`
	Checkpoint Checkpoint                 `json:"checkpoint"`
	Report     Report                     `json:"report"`
	Digest     string                     `json:"digest"`
}
type Capture struct {
	Snapshot Snapshot
	Files    map[string][]byte
	seal     string
}

type RemoteIdentity struct {
	Provider    string `json:"provider"`
	TenantID    string `json:"tenantId"`
	ContainerID string `json:"containerId"`
	RemoteID    string `json:"remoteId"`
	ItemKind    string `json:"itemKind"`
}
type AssetRef struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}
