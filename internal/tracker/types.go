// Package tracker captures external work items and prepares local, non-authorizing drafts.
package tracker

import (
	"context"
	"encoding/json"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/readsource"
	"time"
)

const Version = "aok-tracker/v1"
const Version2 = "aok-tracker/v2"
const AdapterVersion = "1.0.0"

type Auth = readsource.Auth
type Scope = readsource.Scope
type Limits = readsource.Limits

// Deployment pins a Data Center installation independently of Cloud identity.
type Deployment = readsource.Deployment
type Source = readsource.Source
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
type Request = readsource.Request
type Response = readsource.Response
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
	Identity       contracts.RemoteIdentity   `json:"identity"`
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
	Request    Request            `json:"request"`
	Status     int                `json:"status"`
	Headers    map[string]string  `json:"headers"`
	Asset      contracts.AssetRef `json:"asset"`
	ObservedAt string             `json:"observedAt"`
}
type Checkpoint = readsource.Checkpoint
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
type FieldRule struct {
	PacketField     string                     `json:"packetField"`
	ProviderFieldID string                     `json:"providerFieldId"`
	Literal         *json.RawMessage           `json:"literal,omitempty"`
	ValueMap        map[string]json.RawMessage `json:"valueMap"`
	Owner           string                     `json:"owner"`
}
type Mapping struct {
	RelationTypes  []string    `json:"relationTypes"`
	Version        string      `json:"version"`
	ID             string      `json:"id"`
	Provider       string      `json:"provider"`
	MappingVersion string      `json:"mappingVersion"`
	Rules          []FieldRule `json:"rules"`
}
type Desired struct {
	Version          string                     `json:"version"`
	Identity         contracts.RemoteIdentity   `json:"identity"`
	ExpectedRevision string                     `json:"expectedRevision"`
	Fields           map[string]json.RawMessage `json:"fields"`
	HumanOwnedFields []string                   `json:"humanOwnedFields"`
	TransitionID     string                     `json:"transitionId"`
	Comment          *contracts.AssetRef        `json:"comment,omitempty"`
	Relations        []Relation                 `json:"relations"`
}
type TrackerUpdatePlan struct {
	Version          string                   `json:"version"`
	Identity         contracts.RemoteIdentity `json:"identity"`
	SnapshotSHA256   string                   `json:"snapshotSha256"`
	MappingSHA256    string                   `json:"mappingSha256"`
	DesiredSHA256    string                   `json:"desiredSha256"`
	AsOf             string                   `json:"asOf"`
	ExpectedRevision string                   `json:"expectedRevision"`
	RevisionKind     string                   `json:"revisionKind"`
	ConditionalWrite string                   `json:"conditionalWrite"`
	Changes          Desired                  `json:"changes"`
	Prerequisites    []string                 `json:"prerequisites"`
	Report           Report                   `json:"report"`
	Digest           string                   `json:"digest"`
}
type DraftPlanner interface {
	Plan(*Capture, Mapping, Desired, time.Time) (TrackerUpdatePlan, error)
}
