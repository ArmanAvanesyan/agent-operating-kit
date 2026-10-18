# Antigravity CLI (AGY) native agent adapter

This adapter handles one explicitly supplied, project-scoped Markdown agent
file. AGY's official docs describe both `.agents/agents/<name>.md` and
`.agents/agents/<name>/agent.md`; the adapter accepts either and emits the
nested `agent.md` form. It does not discover files or write a project. Global
customizations under `~/.gemini/config/agents/` are outside this adapter's
scope.

The native subset is intentionally small:

| AOK field | Native value | Handling |
|---|---|---|
| `agent.metadata.id` | YAML `name` | Required non-empty string, which must match the file's agent name. |
| `agent.identity.purpose` | YAML `description` | Required non-empty string. |
| `executionProfile.model.selector` | YAML `model` | Optional; supported values are `inherit`, `flash`, and `pro`. Omission stays omitted. |
| `agent.instructions.sources` | Markdown body | One materialized `instructions.md` asset; body bytes are preserved. |

Other frontmatter fields are preserved under `Unknown` and re-emitted without
inventing AOK equivalents. This includes `tools`, `mainAgent`, `subagent`,
`commandExecutionPolicy`, `mcpServers`, `skills`, and `plugins`. Official docs
list defaults including `tools: []`, `mainAgent: true`, `subagent: true`,
`model: inherit`, and `commandExecutionPolicy: sandbox`; omitted values remain
omitted in the projection. Required AOK tool, filesystem, network, or MCP
controls are unsupported and block through the shared native adapter engine.
Unknown frontmatter syntax must be representable by the bounded YAML parser;
YAML comments and formatting are not retained.

Sources checked:

- [Custom subagents](https://www.antigravity.google/docs/subagents/)
- [CLI `/agents` command](https://www.antigravity.google/docs/cli/commands/agents)
- [CLI subagent guide](https://antigravity.google/docs/cli-subagents)

`Descriptor().HostLoading.Status` remains `not-run`. A bounded probe observed
the installed executable change from 1.3.6 to 1.4.2 after non-isolated
help/list invocations. The exact pre-probe 1.3.6 backup was later version-checked
as a copy under a write guard and atomically restored to the original path; the
updated executable was quarantined. Neither original executable was invoked
during that restoration audit. The restored backup SHA-256 is recorded in
[`agy-1.4.2.json`](../../../docs/evidence/native-adapters/agy-1.4.2.json).

For the 1.4.2 probe, the executable was copied before invocation. The
documented `agy agent` roster command ran with a generated AOK fixture inside
an isolated project and HOME. It exited successfully with empty stdout and
stderr; the same command on an empty control project produced the same result.
This does not prove discovery, so the native loading gate remains open. The
command's evidence and fixture hash are recorded in
[`agy-1.4.2.json`](../../../docs/evidence/native-adapters/agy-1.4.2.json) and
can be repeated with [`probe-gemini-agy.sh`](../../../scripts/native-loading/probe-gemini-agy.sh).

The adapter tests validate parsing, preservation, and semantic round-tripping;
they do not establish host discovery, selection, invocation, or enforcement.
No model or imported/custom command was run. All CLI state created during the
probe was confined to the temporary HOME.
