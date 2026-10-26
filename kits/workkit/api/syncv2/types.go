// Package syncv2 contains directional proposals, never execution authorization.
package syncv2

import (
	o "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/operationsv1"
	r "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/readv1"
	y "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/syncv1"
	api "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/v1alpha1"
)

const Version = "workkit-sync/v2"

type Rule struct {
	ID               string `json:"id"`
	SourceField      string `json:"sourceField"`
	DestinationField string `json:"destinationField"`
	Owner            string `json:"owner"`
	Category         string `json:"category"`
}
type Configuration struct {
	Version                        string          `json:"version"`
	Kind                           string          `json:"kind"`
	ID                             string          `json:"id"`
	Revision                       int64           `json:"revision"`
	SourceConfigurationSHA256      string          `json:"sourceConfigurationSha256"`
	DestinationConfigurationSHA256 string          `json:"destinationConfigurationSha256"`
	SourceBindingSHA256            string          `json:"sourceBindingSha256"`
	DestinationBindingSHA256       string          `json:"destinationBindingSha256"`
	ForwardMappingSHA256           string          `json:"forwardMappingSha256"`
	ReverseMappingSHA256           string          `json:"reverseMappingSha256"`
	SourceQuerySHA256              string          `json:"sourceQuerySha256"`
	DestinationQuerySHA256         string          `json:"destinationQuerySha256"`
	Rules                          []Rule          `json:"rules"`
	Correlations                   []y.Correlation `json:"correlations"`
}
type Baseline struct {
	CorrelationID       string   `json:"correlationId"`
	RuleID              string   `json:"ruleId"`
	Source              y.Cell   `json:"source"`
	Destination         y.Cell   `json:"destination"`
	ConfirmedOperations []string `json:"confirmedOperations"`
}
type State struct {
	Version             string     `json:"version"`
	Kind                string     `json:"kind"`
	RouteID             string     `json:"routeId"`
	ConfigurationSHA256 string     `json:"configurationSha256"`
	Revision            int64      `json:"revision"`
	Initialized         bool       `json:"initialized"`
	Paused              bool       `json:"paused"`
	Baselines           []Baseline `json:"baselines"`
	PendingPlanSHA256   string     `json:"pendingPlanSha256"`
	Unresolved          []string   `json:"unresolved"`
	CompletedWatermark  string     `json:"completedWatermark"`
}
type Change struct {
	Direction     string     `json:"direction"`
	CorrelationID string     `json:"correlationId"`
	RuleID        string     `json:"ruleId"`
	Target        r.Identity `json:"target"`
	Desired       o.Desired  `json:"desired"`
	Next          Baseline   `json:"next"`
}
type Conflict struct {
	ID             string `json:"id"`
	CorrelationID  string `json:"correlationId"`
	RuleID         string `json:"ruleId"`
	Code           string `json:"code"`
	BaselineSHA256 string `json:"baselineSha256"`
	InputsSHA256   string `json:"inputsSha256"`
	Source         y.Cell `json:"source"`
	Destination    y.Cell `json:"destination"`
}
type Resolution struct {
	Version    string   `json:"version"`
	Kind       string   `json:"kind"`
	PlanSHA256 string   `json:"planSha256"`
	Choices    []Choice `json:"choices"`
}
type Choice struct {
	ConflictID string `json:"conflictId"`
	Choice     string `json:"choice"`
}
type Plan struct {
	ImplementationSHA256 string            `json:"implementationSha256"`
	AdapterVersion       string            `json:"adapterVersion"`
	Version              string            `json:"version"`
	Kind                 string            `json:"kind"`
	RouteID              string            `json:"routeId"`
	ConfigurationSHA256  string            `json:"configurationSha256"`
	StateSHA256          string            `json:"stateSha256"`
	AsOf                 string            `json:"asOf"`
	Inputs               map[string]string `json:"inputs"`
	Changes              []Change          `json:"changes"`
	Conflicts            []Conflict        `json:"conflicts"`
	Advances             []Baseline        `json:"advances"`
	Candidate            State             `json:"candidate"`
	Status               string            `json:"status"`
	Complete             bool              `json:"complete"`
	Diagnostics          []api.Diagnostic  `json:"diagnostics"`
	AuthorizationGranted bool              `json:"authorizationGranted"`
	ExecutionReady       bool              `json:"executionReady"`
	MigrationActivated   bool              `json:"migrationActivated"`
	ProductionActivated  bool              `json:"productionActivated"`
}

// Invocation binds a fresh exact approved operation bundle for every change.
type Invocation struct {
	Version    string `json:"version"`
	Kind       string `json:"kind"`
	PlanSHA256 string `json:"planSha256"`
	Operations []Call `json:"operations"`
}
type Call struct {
	Direction    string `json:"direction"`
	ChangeSHA256 string `json:"changeSha256"`
	Plan         string `json:"plan"`
	Policy       string `json:"policy"`
	Enrollment   string `json:"enrollment"`
	Output       string `json:"output"`
}
type ResolutionPreview struct {
	Version            string `json:"version"`
	Kind               string `json:"kind"`
	OriginalPlanSHA256 string `json:"originalPlanSha256"`
	ResolutionSHA256   string `json:"resolutionSha256"`
	Plan               Plan   `json:"plan"`
}
