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

`Descriptor().HostLoading.Status` is `not-run`. The installed AGY CLI is
reported as version 1.3.6, but prior isolated agent probes returned empty
output and do not prove that generated definitions load. These tests validate
only parsing, preservation, and semantic round-tripping; they do not establish
host discovery, selection, invocation, or enforcement. No model or native
command was run by this adapter.
