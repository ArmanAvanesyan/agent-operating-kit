package contracts

import "encoding/json"

// LifecyclePolicy describes lifecycle rules; validation does not dispatch transitions.
type LifecyclePolicy struct {
	DocumentHeader
	States       []string              `json:"states"`
	Transitions  []LifecycleTransition `json:"transitions"`
	Retry        RetryPolicy           `json:"retry"`
	Checkpoint   CheckpointPolicy      `json:"checkpoint"`
	Cancellation CancellationPolicy    `json:"cancellation"`
}
type LifecycleTransition struct {
	From      string `json:"from"`
	To        string `json:"to"`
	Condition string `json:"condition"`
}
type RetryPolicy struct {
	MaxAttempts     json.Number `json:"maxAttempts"`
	RetryableErrors []string    `json:"retryableErrors"`
	BackoffSeconds  json.Number `json:"backoffSeconds"`
	SideEffects     string      `json:"sideEffects"`
}
type CheckpointPolicy struct {
	Enabled        bool     `json:"enabled"`
	FormatVersion  string   `json:"formatVersion"`
	ResumeRequires []string `json:"resumeRequires"`
}
type CancellationPolicy struct {
	StopNewActions        bool   `json:"stopNewActions"`
	RecordInflightActions bool   `json:"recordInflightActions"`
	Rollback              string `json:"rollback"`
}

// EvaluationContract records declarations and supplied results, not evaluator execution.
type EvaluationContract struct {
	DocumentHeader
	Agent       Reference           `json:"agent"`
	Profiles    []Reference         `json:"profiles"`
	Fixtures    []EvaluationFixture `json:"fixtures"`
	Metrics     []EvaluationMetric  `json:"metrics"`
	Repetitions json.Number         `json:"repetitions"`
	Acceptance  []string            `json:"acceptance"`
	Results     []EvaluationResult  `json:"results"`
}
type EvaluationFixture struct {
	ID              string `json:"id"`
	TaskPacket      string `json:"taskPacket"`
	DatasetRevision string `json:"datasetRevision"`
}
type EvaluationMetric struct {
	ID        string       `json:"id"`
	Unit      string       `json:"unit"`
	Direction string       `json:"direction"`
	Threshold *json.Number `json:"threshold"`
}
type EvaluationResult struct {
	Profile  Reference `json:"profile"`
	Status   string    `json:"status"`
	Evidence []string  `json:"evidence"`
}

// MigrationContract is inert metadata. No transforms or rollback are performed.
type MigrationContract struct {
	DocumentHeader
	SchemaCompatibility SchemaCompatibility `json:"schemaCompatibility"`
	ReferencePinning    string              `json:"referencePinning"`
	Deprecations        []Deprecation       `json:"deprecations"`
	Migrations          []Migration         `json:"migrations"`
	Upgrade             UpgradePolicy       `json:"upgrade"`
	Rollback            RollbackPolicy      `json:"rollback"`
}
type SchemaCompatibility struct {
	Current          string   `json:"current"`
	ReadableVersions []string `json:"readableVersions"`
	UnknownVersion   string   `json:"unknownVersion"`
}
type Deprecation struct {
	Field              string `json:"field"`
	Replacement        string `json:"replacement"`
	RemoveAfterVersion string `json:"removeAfterVersion"`
}
type Migration struct {
	From         string `json:"from"`
	To           string `json:"to"`
	TransformRef string `json:"transformRef"`
	Automatic    bool   `json:"automatic"`
	LossPolicy   string `json:"lossPolicy"`
}
type UpgradePolicy struct {
	DryRunRequired    bool `json:"dryRunRequired"`
	BackupRequired    bool `json:"backupRequired"`
	ReResolveRequired bool `json:"reResolveRequired"`
}
type RollbackPolicy struct {
	RestoreDefinitions     bool `json:"restoreDefinitions"`
	RestoreGeneratedFiles  bool `json:"restoreGeneratedFiles"`
	ReverseExternalEffects bool `json:"reverseExternalEffects"`
}
