# Operational contracts (v1alpha1)

This package is the Go-only operational boundary. Embedded schemas are stored
here and compiled from the embedded corpus with no network loader. The proposal
files under `docs/proposals/` remain historical design assets.

`Parse` rejects malformed UTF-8, BOMs, duplicate keys at every depth, trailing
values, documents over 4 MiB, nesting deeper than 64, unsupported API versions
and kinds, invalid metadata IDs/versions, and schema violations. It returns a
typed `Envelope` with the original JSON document preserved as `json.RawMessage`;
`DecodeTyped` validates and decodes every supported kind without losing declared fields. Semantic reference resolution is provided by `internal/agentvalidate`.
Failures return stable diagnostic codes and messages. `Reference` is the
canonical operational reference structure `{kind,id,version}`; versions are
exact numeric `major.minor.patch` values.

The embedded schemas are a separate, closed operational corpus. Managed
document references use exact `{kind,id,version}` pins. Assignment documents
use the common `metadata` envelope. Paths, capability/tool identifiers, and
external environment requirement names remain strings where they identify
files or runtime symbols rather than managed corpus documents. The proposal
files remain unchanged. Fixtures cover each supported kind.
Capability registry identifiers may contain dots; metadata IDs use the
canonical lowercase, dash-separated slug form. The supported documents
are Agent, ExecutionProfile, AgentAssignment, CapabilityRegistry,
InvocationContract, ContextPolicy, OutputContract, EnvironmentProfile,
LifecyclePolicy, EvaluationContract, and MigrationContract. Delegated agents
use exact Agent references; evaluation declarations pin their agent and profiles.
Planning adds GoalContract, PlanDefinition, PlanRevision, DecisionContract,
DecisionResult, SDLCTraceability, OptimizationExperiment and PlanningEvidence.
Their cross-contract and evidence semantics are checked by `internal/planning`,
`internal/decision` and `internal/experiment`.

To bound schema-engine exact arithmetic, numeric lexemes are limited to 1024
bytes and exponents to -10000 through 10000 before schema validation.
