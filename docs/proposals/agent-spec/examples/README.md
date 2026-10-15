# Draft examples

All files are proposal fixtures. Nothing in this directory is installed or executable
AOK configuration. Paths in neutral documents resolve from the proposal root.
The task packet uses the existing operational schema and supplies exploration-specific
acceptance criteria. It does not schedule or execute the task.

| File | Draft schema / purpose |
|---|---|
| `agent.json` | `agent.schema.json`: repository-explorer identity and references |
| `execution-profile-opencode.json` | `execution-profile.schema.json`: candidate OpenCode profile |
| `execution-profile-agy.json` | `execution-profile.schema.json`: candidate agy variant; adapter still proposed |
| `output-contract.json` | `output-contract.schema.json`: Markdown report requirements |
| `assignment.json` | `assignment.schema.json`: binds agent/profile/task/outputs |
| `adapter-report.json` | `adapter-report.schema.json`: blocked partial native projection |
| `resolved-manifest.json` | `resolved-manifest.schema.json`: blocked envelope, not a resolver result |
| `instructions.md` | Example instruction source |
| `native/*.md` | Limited native format examples; not full neutral equivalents |
| `native/*.aok.json` | Illustrative sidecar structure; no runtime registration |
| `task-packet.json` | Existing operational task-packet schema fixture |

The roles `explorer` / `architecture-analyst`, capability IDs, `repository-analysis`
environment and named verification checks need future registry definitions. Existing
project-exploration and environment-readiness skills are real repository content.
Profile model selectors come from the saved model research. They establish neither
host readiness nor measured suitability. The blocked manifest contains illustrative time and snapshot provenance;
it must never be mistaken for execution evidence.

## Extended contracts (proposed)

Each following JSON example validates against its same-named draft schema:

| Example | Purpose |
|---|---|
| [Invocation](invocation-contract.json) | Subagent selection/exclusions with input schema |
| [Context](context-policy.json) | Scoped sources, inherited context and retention |
| [Capabilities](capability-registry.json) | Typed bindings; unknown installation/authorization not implied |
| [Environment](environment-profile.json) | Roots, discovery and readiness prerequisites |
| [Result](result-envelope.json) | Blocked handoff without fabricated artifacts |
| [Run](run-record.json) | Synthetic blocked attempt with no observed model or usage |
| [Lifecycle](lifecycle-policy.json) | Transitions, retry cap and disabled checkpointing |
| [Conformance](adapter-conformance.json) | Draft per-field support; native tests not-run |
| [Evaluation](evaluation-contract.json) | Proposed metrics/fixtures; both profiles not-run |
| [Migration](migration-contract.json) | Version/hash rules; no fabricated migrations |

[Invocation input schema](invocation-input.schema.json) validates assignment inputs.
The resolved manifest now snapshots selected proposal documents and instruction text
with checked hashes. Production still needs all transitive registry/policy/role/native
inputs resolved before readiness can pass. Registry `sourcePath` bindings refer to the
assignment's AOK checkout root, not this proposal directory. External MCP connection,
credential, executable and check references are illustrative and unresolved.
