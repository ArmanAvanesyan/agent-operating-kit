// Package syncv1 contains immutable one-way sync proposals, never send permits.
package syncv1

import (
	"encoding/json"
	o "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/operationsv1"
	r "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/readv1"
	api "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/v1alpha1"
)

const Version = "workkit-sync/v1"

type Correlation struct {
	ID          string     `json:"id"`
	Source      r.Identity `json:"source"`
	Destination r.Identity `json:"destination"`
}
type Cell struct {
	Present bool            `json:"present"`
	Value   json.RawMessage `json:"value"`
}
type Baseline struct {
	CorrelationID        string `json:"correlationId"`
	Field                string `json:"field"`
	Source               Cell   `json:"source"`
	Destination          Cell   `json:"destination"`
	ConfirmedOperationID string `json:"confirmedOperationId"`
}
type Configuration struct {
	Version                      string        `json:"version"`
	Kind                         string        `json:"kind"`
	Route                        api.SyncRoute `json:"route"`
	SourceObservationSHA256      string        `json:"sourceObservationSha256"`
	DestinationObservationSHA256 string        `json:"destinationObservationSha256"`
	MappingContextSHA256         string        `json:"mappingContextSha256"`
	SourceBindingSHA256          string        `json:"sourceBindingSha256"`
	DestinationBindingSHA256     string        `json:"destinationBindingSha256"`
	QuerySHA256                  string        `json:"querySha256"`
	Correlations                 []Correlation `json:"correlations"`
	AllowCreate                  bool          `json:"allowCreate"`
	MaxOperations                int           `json:"maxOperations"`
}
type State struct {
	Version            string     `json:"version"`
	Kind               string     `json:"kind"`
	RouteSHA256        string     `json:"routeSha256"`
	Revision           int64      `json:"revision"`
	Initialized        bool       `json:"initialized"`
	Paused             bool       `json:"paused"`
	Baselines          []Baseline `json:"baselines"`
	PendingPlanSHA256  string     `json:"pendingPlanSha256"`
	CompletedWatermark string     `json:"completedWatermark"`
	Unresolved         []string   `json:"unresolved"`
}
type Conflict struct {
	CorrelationID string `json:"correlationId"`
	Field         string `json:"field"`
	Code          string `json:"code"`
	Source        Cell   `json:"source"`
	Destination   Cell   `json:"destination"`
}
type Plan struct {
	Version              string            `json:"version"`
	Kind                 string            `json:"kind"`
	RouteSHA256          string            `json:"routeSha256"`
	Inputs               map[string]string `json:"inputs"`
	AsOf                 string            `json:"asOf"`
	Initialize           bool              `json:"initialize"`
	Status               string            `json:"status"`
	Proposals            []o.Desired       `json:"proposals"`
	Operations           []o.Draft         `json:"operations"`
	Conflicts            []Conflict        `json:"conflicts"`
	Updates              []Baseline        `json:"updates"`
	Candidate            State             `json:"candidate"`
	Complete             bool              `json:"complete"`
	Diagnostics          []api.Diagnostic  `json:"diagnostics"`
	AuthorizationGranted bool              `json:"authorizationGranted"`
	ExecutionReady       bool              `json:"executionReady"`
	MigrationActivated   bool              `json:"migrationActivated"`
	ProductionActivated  bool              `json:"productionActivated"`
}
type Run struct {
	Version              string          `json:"version"`
	Kind                 string          `json:"kind"`
	PlanSHA256           string          `json:"planSha256"`
	State                State           `json:"state"`
	Operations           []o.Observation `json:"operations"`
	Status               string          `json:"status"`
	AuthorizationGranted bool            `json:"authorizationGranted"`
	ExecutionReady       bool            `json:"executionReady"`
	MigrationActivated   bool            `json:"migrationActivated"`
	ProductionActivated  bool            `json:"productionActivated"`
}

// Invocation references only immutable, separately prepared operations and grants.
type Invocation struct {
	Version    string                `json:"version"`
	Kind       string                `json:"kind"`
	PlanSHA256 string                `json:"planSha256"`
	Operations []InvocationOperation `json:"operations"`
}
type InvocationOperation struct {
	Plan       string `json:"plan"`
	Policy     string `json:"policy"`
	Enrollment string `json:"enrollment"`
	Output     string `json:"output"`
}
