# Operational contracts (v1alpha1)

This package is the Go-only operational boundary. Embedded schemas are stored
here and compiled from the embedded corpus with no network loader. The proposal
files under `docs/proposals/` remain historical design assets.

`Parse` rejects malformed UTF-8, BOMs, duplicate keys at every depth, trailing
values, documents over 4 MiB, nesting deeper than 64, unsupported API versions
and kinds, invalid metadata IDs/versions, and schema violations. It returns a
typed `Envelope` with the original JSON document preserved as `json.RawMessage`;
per-kind typed decoding and semantic reference resolution follow in PR3.
Failures return stable diagnostic codes and messages. `Reference` is the
canonical operational reference structure `{kind,id,version}`; versions are
exact numeric `major.minor.patch` values.

The embedded schemas are a separate, closed operational corpus. Managed
document references use exact `{kind,id,version}` pins. Assignment documents
use the common `metadata` envelope. Paths, capability/tool identifiers, and
external environment requirement names remain strings where they identify
files or runtime symbols rather than managed corpus documents. The proposal
files remain unchanged. Fixtures cover each of the eight supported kinds.
Capability registry identifiers may contain dots; metadata IDs use the
canonical lowercase, dash-separated slug form. The eight supported documents
are Agent, ExecutionProfile, AgentAssignment, CapabilityRegistry,
InvocationContract, ContextPolicy, OutputContract, and EnvironmentProfile.
LifecyclePolicy, EvaluationContract, and MigrationContract are valid reference
kind values only; their standalone document schemas are deferred.
Planning `GoalContract`, `PlanDefinition`, `PlanRevision`, `DecisionContract`,
`SDLCTraceability`, and optimization kinds are deferred to planning PRs.
