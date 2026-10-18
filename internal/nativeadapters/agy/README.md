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

The historical `agy agent` trial returned empty output and is inconclusive. A
subsequent isolated 1.3.6 probe copied the executable before invoking it. Installed help recognizes
`agent`/`agents` as listing aliases, but `agents` returned no output for the
retained AOK-rendered fixture, an empty project, or a malformed agent file.
The optional `--json` flag is not supported. Official headless `-p` is a
prompt path, so it was not used to invoke `/agents`; the documented interactive
manager showed an authentication screen. No authentication or model invocation
occurred. Native loading remains unverified. See
[`agy-1.3.6-loading.json`](../../../docs/evidence/native-adapters/agy-1.3.6-loading.json)
for the prior isolated results, [the retained offline probe record](../../../docs/evidence/native-adapters/cursor-agy-offline-2026.10.18.json)
for the positive/absent/malformed controls and exact rendered bytes, and
[`agy-1.4.2.json`](../../../docs/evidence/native-adapters/agy-1.4.2.json) for
the historical version/restoration audit.

The 2026-10-18 offline probe denied remote network access and allowed only
localhost bind/inbound for the listing command's local service startup.

The adapter tests validate parsing, preservation, and semantic round-tripping;
they do not establish host discovery, selection, invocation, or enforcement.
No model or imported/custom command was run. All CLI state created during the
probe was confined to the temporary HOME.
