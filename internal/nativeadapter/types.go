// Package nativeadapter defines a pure, explicit-input interface for native
// agent-format projections. It does not discover files, access the filesystem,
// execute native commands, or assert runtime enforcement.
package nativeadapter

import (
	"encoding/json"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
)

const BundleVersion = "aok-native-bundle/v1"

type File struct {
	Path   string `json:"path"`
	Format string `json:"format"`
	Data   []byte `json:"data"`
}

// Document is an incomplete mapping projection, never a complete canonical
// Agent. Fields use canonical AOK dotted paths; Unknown uses native paths.
type Document struct {
	Fields  map[string]json.RawMessage `json:"fields"`
	Unknown map[string]json.RawMessage `json:"unknown"`
}

type FieldCapability struct {
	NeutralPath      string `json:"neutralPath"`
	NativePath       string `json:"nativePath"`
	Support          string `json:"support"`          // native, executor-backed, guidance-only, unsupported
	EnforcementOwner string `json:"enforcementOwner"` // native, executor, advisory, none
}

// HostLoadingEvidence is separate from FieldCapability.EnforcementOwner:
// recognizing a configuration field does not establish that a host loads it.
type HostLoadingEvidence struct {
	Status      string   `json:"status"` // not-run, verified, unavailable
	HostVersion string   `json:"hostVersion,omitempty"`
	Evidence    []string `json:"evidence,omitempty"`
	Scopes      []string `json:"scopes,omitempty"`
}

type Descriptor struct {
	Target         string              `json:"target"`
	AdapterVersion string              `json:"adapterVersion"`
	Scopes         []string            `json:"scopes"`
	Fields         []FieldCapability   `json:"fields"`
	HostLoading    HostLoadingEvidence `json:"hostLoading"`
}

type Adapter interface {
	Descriptor() Descriptor
	Decode(scope string, files []File) (Document, []contracts.Diagnostic, error)
	Encode(scope string, doc Document) ([]File, []contracts.Diagnostic, error)
}

type Registry struct{ adapters map[string]Adapter }

func NewRegistry() *Registry { return &Registry{adapters: map[string]Adapter{}} }

type Control struct {
	Path     string `json:"path"`
	Required bool   `json:"required"`
}

type FileHash struct {
	Path   string `json:"path"`
	Format string `json:"format"`
	SHA256 string `json:"sha256"`
}

type BoundFile struct {
	File
	SHA256 string `json:"sha256"`
}

// Bundle binds source bytes, their hashes, and the semantic projection. It is
// explicitly incomplete until a canonical-assignment bridge supplies and
// validates all required AOK references.
type Bundle struct {
	Version        string      `json:"version"`
	Target         string      `json:"target"`
	Scope          string      `json:"scope"`
	AdapterVersion string      `json:"adapterVersion"`
	Completeness   string      `json:"completeness"` // draft-incomplete
	OriginalFiles  []BoundFile `json:"originalFiles"`
	RenderedFiles  []BoundFile `json:"renderedFiles,omitempty"`
	Document       Document    `json:"document"`
	Digest         string      `json:"digest"`
}

type Difference struct {
	Path   string `json:"path"`
	Status string `json:"status"` // missing, changed, added
}

type Report struct {
	Operation          string                 `json:"operation"`
	Target             string                 `json:"target"`
	Scope              string                 `json:"scope"`
	Status             string                 `json:"status"`             // valid, invalid, blocked
	RuntimeEnforcement string                 `json:"runtimeEnforcement"` // always unverified in this package
	HostLoading        HostLoadingEvidence    `json:"hostLoading"`
	MappingStatus      string                 `json:"mappingStatus,omitempty"` // inspected, draft, compatible, lossy
	Diagnostics        []contracts.Diagnostic `json:"diagnostics"`
	Projection         []string               `json:"projection,omitempty"`
	Unknown            []string               `json:"unknown,omitempty"`
	Differences        []Difference           `json:"differences,omitempty"`
	SourceHashes       []FileHash             `json:"sourceHashes,omitempty"`
}
