// Package operationsv1 describes non-authorizing mutation drafts and observations.
package operationsv1

import (
	"encoding/json"
	r "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/readv1"
	s "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/setupv1"
	api "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/v1alpha1"
)

const Version = "workkit-operations/v1"

type Store struct {
	Version   string `json:"version"`
	Kind      string `json:"kind"`
	Path      string `json:"path"`
	ID        string `json:"id"`
	Namespace string `json:"namespace"`
	Owner     string `json:"owner"`
}
type Draft struct {
	Version              string                     `json:"version"`
	Kind                 string                     `json:"kind"`
	ID                   string                     `json:"id"`
	LogicalKey           string                     `json:"logicalKey"`
	Operation            string                     `json:"operation"`
	Target               r.Identity                 `json:"target"`
	ParentID             string                     `json:"parentId"`
	Revision             api.Revision               `json:"revision"`
	ObservedAt           string                     `json:"observedAt"`
	Inputs               map[string]string          `json:"inputs"`
	Changes              map[string]json.RawMessage `json:"changes"`
	DependsOn            []string                   `json:"dependsOn"`
	Status               string                     `json:"status"`
	RequiredConcurrency  string                     `json:"requiredConcurrency"`
	Diagnostics          []api.Diagnostic           `json:"diagnostics"`
	Assets               []api.Asset                `json:"assets"`
	Plan                 api.OperationPlan          `json:"plan"`
	AuthorizationGranted bool                       `json:"authorizationGranted"`
	ExecutionReady       bool                       `json:"executionReady"`
	MigrationActivated   bool                       `json:"migrationActivated"`
	ProductionActivated  bool                       `json:"productionActivated"`
}
type Desired struct {
	Version      string                     `json:"version"`
	Kind         string                     `json:"kind"`
	LogicalKey   string                     `json:"logicalKey"`
	Operation    string                     `json:"operation"`
	ParentID     string                     `json:"parentId"`
	ItemID       string                     `json:"itemId"`
	Fields       map[string]json.RawMessage `json:"fields"`
	DependsOn    []string                   `json:"dependsOn"`
	AllowCreate  bool                       `json:"allowCreate"`
	TypeID       string                     `json:"typeId"`
	SchemaSHA256 string                     `json:"schemaSha256"`
}
type Capability struct {
	FieldRoles          map[string]string   `json:"fieldRoles"`
	AuthenticationMode  string              `json:"authenticationMode"`
	ConfigurationSHA256 string              `json:"configurationSha256"`
	Transport           r.TransportIdentity `json:"transport"`
	Version             string              `json:"version"`
	Kind                string              `json:"kind"`
	Scope               s.Scope             `json:"scope"`
	Operation           string              `json:"operation"`
	Supported           bool                `json:"supported"`
	Concurrency         string              `json:"concurrency"`
	Qualification       string              `json:"qualification"`
	ObservedAt          string              `json:"observedAt"`
	Evidence            []api.Asset         `json:"evidence"`
}
type Enrollment struct {
	Version              string `json:"version"`
	Kind                 string `json:"kind"`
	Purpose              string `json:"purpose"`
	ID                   string `json:"id"`
	Issuer               string `json:"issuer"`
	OperationSHA256      string `json:"operationSha256"`
	Store                Store  `json:"store"`
	Action               string `json:"action"`
	PolicySHA256         string `json:"policySha256"`
	ImplementationSHA256 string `json:"implementationSha256"`
	IssuedAt             string `json:"issuedAt"`
	ExpiresAt            string `json:"expiresAt"`
	OutputPath           string `json:"outputPath"`
	Signature            string `json:"signature"`
}
type Policy struct {
	Version        string     `json:"version"`
	Kind           string     `json:"kind"`
	NotBefore      string     `json:"notBefore"`
	ExpiresAt      string     `json:"expiresAt"`
	Issuers        []r.Issuer `json:"issuers"`
	RevokedIDs     []string   `json:"revokedIds"`
	AllowedScopes  []s.Scope  `json:"allowedScopes"`
	MaxOperations  int        `json:"maxOperations"`
	AllowedActions []string   `json:"allowedActions"`
}
type Observation struct {
	Version              string           `json:"version"`
	Kind                 string           `json:"kind"`
	OperationID          string           `json:"operationId"`
	Digest               string           `json:"digest"`
	State                string           `json:"state"`
	Fence                int64            `json:"fence"`
	Attempts             int              `json:"attempts"`
	Store                Store            `json:"store"`
	ObservedAt           string           `json:"observedAt"`
	Evidence             []api.Asset      `json:"evidence"`
	Diagnostics          []api.Diagnostic `json:"diagnostics"`
	AuthorizationGranted bool             `json:"authorizationGranted"`
	ExecutionReady       bool             `json:"executionReady"`
	MigrationActivated   bool             `json:"migrationActivated"`
	ProductionActivated  bool             `json:"productionActivated"`
}
type Qualification struct {
	Version              string               `json:"version"`
	Kind                 string               `json:"kind"`
	Scope                s.Scope              `json:"scope"`
	Operation            string               `json:"operation"`
	ImplementationSHA256 string               `json:"implementationSha256"`
	Required             []string             `json:"required"`
	Provided             map[string]api.Asset `json:"provided"`
	Status               string               `json:"status"`
	Diagnostics          []api.Diagnostic     `json:"diagnostics"`
	AuthorizationGranted bool                 `json:"authorizationGranted"`
	ExecutionReady       bool                 `json:"executionReady"`
}
