package contracts

import (
	"encoding/json"
)

type PlanningPredicate struct {
	ID          string    `json:"id"`
	Description string    `json:"description"`
	VerifierRef Reference `json:"verifierRef"`
	Required    bool      `json:"required"`
}
type GoalLimits struct {
	MaxAttempts    json.Number `json:"maxAttempts"`
	MaxReplans     json.Number `json:"maxReplans"`
	TimeoutSeconds json.Number `json:"timeoutSeconds"`
}
type GoalContract struct {
	DocumentHeader
	Outcome            string              `json:"outcome"`
	Scope              GoalScope           `json:"scope"`
	SuccessPredicates  []PlanningPredicate `json:"successPredicates"`
	Invariants         []PlanningPredicate `json:"invariants"`
	Limits             GoalLimits          `json:"limits"`
	StopConditions     []string            `json:"stopConditions"`
	VerificationPolicy string              `json:"verificationPolicy"`
	PlanRef            *Reference          `json:"planRef,omitempty"`
}
type GoalScope struct {
	Repositories []string `json:"repositories"`
	WriteRoots   []string `json:"writeRoots"`
}
type PlanDefinition struct {
	DocumentHeader
	GoalRef               Reference           `json:"goalRef"`
	Strategy              string              `json:"strategy"`
	Steps                 []PlanStep          `json:"steps"`
	MethodRefs            []string            `json:"methodRefs"`
	Inventory             []PlanInventoryItem `json:"inventory"`
	AggregateVerification []PlanningPredicate `json:"aggregateVerification"`
	ConcurrencyLimit      json.Number         `json:"concurrencyLimit"`
	Replanning            PlanReplanning      `json:"replanning"`
}
type PlanStep struct {
	ID               string              `json:"id"`
	Description      string              `json:"description"`
	DependsOn        []string            `json:"dependsOn"`
	Preconditions    []PlanningPredicate `json:"preconditions"`
	Outputs          []string            `json:"outputs"`
	CompletionChecks []PlanningPredicate `json:"completionChecks"`
	AssignmentRef    Reference           `json:"assignmentRef"`
	VerifierRef      Reference           `json:"verifierRef"`
	WorkflowRef      string              `json:"workflowRef"`
	Ownership        []string            `json:"ownership"`
	Recovery         string              `json:"recovery"`
}
type PlanInventoryItem struct {
	ID          string   `json:"id"`
	StepRefs    []string `json:"stepRefs"`
	Disposition string   `json:"disposition"`
	Rationale   string   `json:"rationale"`
}
type PlanReplanning struct {
	MaxRevisions              json.Number `json:"maxRevisions"`
	PreserveCompletedEvidence bool        `json:"preserveCompletedEvidence"`
}
type PlanRevision struct {
	DocumentHeader
	BeforePlan          Reference    `json:"beforePlan"`
	AfterPlan           Reference    `json:"afterPlan"`
	Predecessor         *Reference   `json:"predecessor"`
	Ordinal             json.Number  `json:"ordinal"`
	Trigger             string       `json:"trigger"`
	Evidence            []string     `json:"evidence"`
	Changes             []PlanChange `json:"changes"`
	PreservedEvidence   []string     `json:"preservedEvidence"`
	InvalidatedEvidence []string     `json:"invalidatedEvidence"`
	AuthorizationRef    *string      `json:"authorizationRef"`
	State               string       `json:"state"`
}
type PlanChange struct {
	StepID    string `json:"stepId"`
	Operation string `json:"operation"`
	Reason    string `json:"reason"`
}
type DecisionContract struct {
	DocumentHeader
	DecisionType     string               `json:"decisionType"`
	InputSchema      string               `json:"inputSchema"`
	Choices          []string             `json:"choices"`
	ScoreRange       *DecisionScoreRange  `json:"scoreRange,omitempty"`
	Mechanism        string               `json:"mechanism"`
	AdapterRef       string               `json:"adapterRef"`
	EvidenceRequired bool                 `json:"evidenceRequired"`
	Uncertainty      DecisionUncertainty  `json:"uncertainty"`
	Consequences     DecisionConsequences `json:"consequences"`
}
type DecisionScoreRange struct {
	Min json.Number `json:"min"`
	Max json.Number `json:"max"`
}
type DecisionUncertainty struct {
	AllowAbstention   bool    `json:"allowAbstention"`
	OnMissingEvidence string  `json:"onMissingEvidence"`
	CalibrationRef    *string `json:"calibrationRef"`
}
type DecisionConsequences struct {
	Authority       string   `json:"authority"`
	AllowedOutcomes []string `json:"allowedOutcomes"`
}
type DecisionResult struct {
	DocumentHeader
	DecisionRef          Reference       `json:"decisionRef"`
	Fixture              bool            `json:"fixture"`
	Status               string          `json:"status"`
	Value                json.RawMessage `json:"value"`
	Evidence             []string        `json:"evidence"`
	Confidence           *json.Number    `json:"confidence"`
	ConfidenceMeaning    string          `json:"confidenceMeaning"`
	Reason               string          `json:"reason"`
	ObservedProvider     *string         `json:"observedProvider"`
	AuthorizationGranted bool            `json:"authorizationGranted"`
}
type OptimizationExperiment struct {
	DocumentHeader
	AgentRef        Reference           `json:"agentRef"`
	BaselineVersion string              `json:"baselineVersion"`
	ProfileRefs     []Reference         `json:"profileRefs"`
	Optimizer       ExperimentOptimizer `json:"optimizer"`
	MutableFields   []string            `json:"mutableFields"`
	ProtectedFields []string            `json:"protectedFields"`
	Datasets        ExperimentDatasets  `json:"datasets"`
	Evaluator       ExperimentEvaluator `json:"evaluator"`
	Budget          ExperimentBudget    `json:"budget"`
	Promotion       ExperimentPromotion `json:"promotion"`
	Status          string              `json:"status"`
	Results         []ExperimentResult  `json:"results"`
}
type ExperimentOptimizer struct {
	AdapterRef string `json:"adapterRef"`
	Version    string `json:"version"`
}
type ExperimentDatasets struct {
	Train      []string `json:"train"`
	Validation []string `json:"validation"`
	Test       []string `json:"test"`
}
type ExperimentEvaluator struct {
	Ref       Reference `json:"ref"`
	Immutable bool      `json:"immutable"`
}
type ExperimentBudget struct {
	MaxCandidateEvaluations json.Number `json:"maxCandidateEvaluations"`
	MaxProposerCostUSD      json.Number `json:"maxProposerCostUsd"`
	MaxEvaluationCostUSD    json.Number `json:"maxEvaluationCostUsd"`
}
type ExperimentPromotion struct {
	Automatic    bool     `json:"automatic"`
	Requirements []string `json:"requirements"`
}
type ExperimentResult struct {
	CandidateRef string   `json:"candidateRef"`
	Evidence     []string `json:"evidence"`
	Disposition  string   `json:"disposition"`
}
type SDLCTraceability struct {
	DocumentHeader
	GoalRef  Reference          `json:"goalRef"`
	PlanRef  Reference          `json:"planRef"`
	Links    []TraceabilityLink `json:"links"`
	Coverage string             `json:"coverage"`
	Evidence []string           `json:"evidence"`
}
type TraceabilityLink struct {
	Requirement       string    `json:"requirement"`
	PredicateRefs     []string  `json:"predicateRefs"`
	WorkflowType      string    `json:"workflowType"`
	StepRef           string    `json:"stepRef"`
	AssignmentRef     Reference `json:"assignmentRef"`
	OutputContractRef Reference `json:"outputContractRef"`
	VerificationRef   Reference `json:"verificationRef"`
	RunRefs           []string  `json:"runRefs"`
}
type PlanningEvidence struct {
	DocumentHeader
	PlanRef           Reference         `json:"planRef"`
	ContextSHA256     string            `json:"contextSHA256"`
	PlanSHA256        string            `json:"planSHA256"`
	EvaluatedAt       string            `json:"evaluatedAt"`
	ExpiresAt         string            `json:"expiresAt"`
	Outcomes          []PlanningOutcome `json:"outcomes"`
	CompletedSteps    []string          `json:"completedSteps"`
	InventoryCoverage []string          `json:"inventoryCoverage"`
	RevisionsUsed     json.Number       `json:"revisionsUsed"`
	AttemptsUsed      json.Number       `json:"attemptsUsed"`
	ElapsedSeconds    json.Number       `json:"elapsedSeconds"`
	Cancelled         bool              `json:"cancelled"`
}
type PlanningOutcome struct {
	PredicateID  string              `json:"predicateId"`
	Status       string              `json:"status"`
	ArtifactRefs []ArtifactReference `json:"artifactRefs"`
}
type ArtifactReference struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}
