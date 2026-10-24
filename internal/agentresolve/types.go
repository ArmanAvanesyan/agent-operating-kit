package agentresolve

import (
	"encoding/json"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/modelcatalog"
)

// Report contains static resolution evidence. A blocked report may include a
// useful snapshot, but never asserts that an executor can run it safely.
type Report struct {
	Status      string                 `json:"status"`
	Diagnostics []contracts.Diagnostic `json:"diagnostics"`
	Snapshot    *Snapshot              `json:"snapshot,omitempty"`
}

// Source identifies one captured corpus file. Hashes are over exact source
// bytes; paths are normalized corpus-relative POSIX paths.
type Source struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// Evidence names a check that resolution cannot establish without a trusted
// runtime or compatibility catalog. Status is deliberately "unverified".
type Evidence struct {
	Check   string `json:"check"`
	Status  string `json:"status"`
	Source  string `json:"source"`
	Field   string `json:"field"`
	Message string `json:"message"`
}

// Snapshot is the deterministic, declared configuration selected by an
// assignment. Raw source contents and machine-specific paths are omitted.
type Snapshot struct {
	CatalogEvidence       *CatalogEvidence             `json:"catalogEvidence,omitempty"`
	AssignmentPath        string                       `json:"assignmentPath"`
	Assignment            contracts.AgentAssignment    `json:"assignment"`
	Agent                 contracts.Agent              `json:"agent"`
	Profile               contracts.ExecutionProfile   `json:"executionProfile"`
	Output                contracts.OutputContract     `json:"outputContract"`
	Environment           contracts.EnvironmentProfile `json:"environmentProfile"`
	TaskPacketSHA256      string                       `json:"taskPacketSha256"`
	Inputs                contracts.ParameterValues    `json:"inputs"`
	OutputParameters      contracts.ParameterValues    `json:"outputParameters"`
	Permissions           contracts.Permissions        `json:"effectivePermissions"`
	ReadinessEvidence     []Evidence                   `json:"readinessEvidence"`
	CompatibilityEvidence []Evidence                   `json:"compatibilityEvidence"`
	Sources               []Source                     `json:"sources"`
	Digest                string                       `json:"digest"`
}

// CatalogEvidence binds static compatibility observations to captured catalog,
// source-policy and profile bytes. It is never execution permission or readiness.
type CatalogEvidence struct {
	CatalogSHA256 string                   `json:"catalogSha256"`
	PolicySHA256  string                   `json:"policySha256"`
	ProfileSHA256 string                   `json:"profileSha256"`
	AsOf          string                   `json:"asOf"`
	Check         modelcatalog.CheckReport `json:"check"`
}

func (s Snapshot) digestBytes() ([]byte, error) {
	s.Digest = ""
	return json.Marshal(s)
}
