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

The historical 1.3.6 `agent`/`agents` no-TTY calls returned empty output; the
captured help text was not retained, so those runs do not prove that these were
recognized listing aliases. Official headless `-p` is a prompt path and was
not used. Native loading remains unverified. See
[`agy-1.3.6-loading.json`](../../../docs/evidence/native-adapters/agy-1.3.6-loading.json)
for the prior isolated results, [the retained offline probe record](../../../docs/evidence/native-adapters/cursor-agy-offline-2026.10.18.json)
for the positive/absent/malformed controls and exact rendered bytes, and
[`agy-1.4.2.json`](../../../docs/evidence/native-adapters/agy-1.4.2.json) for
the historical version/restoration audit.

The official download page advertises CLI 1.4.0, while its official install
script resolves the darwin-x64 manifest to 1.4.2. The manifest-pinned archive
was checksum-verified in task-owned temporary storage; the binary was extracted
without running the installer or touching the restored 1.3.6 executable. In
1.4.2, `agent --help` and `agents --help` print `Usage: antigravity agent
[flags]` and `List available agents`. Direct no-TTY calls returned empty
output for positive, absent, and malformed workspaces. A PTY attempt failed
process-group signaling with EPERM; a later retry also occurred but its
cleanup path was not recorded. Both PTY observations are excluded. A targeted
audit later found no process matching the known executable paths or temporary
prefix, but full descendant cleanup remains unconfirmed because the first
attempt's PID/PGID was lost. No authentication or model invocation was
performed. See [the fresh 1.4.2 evidence](../../../docs/evidence/native-adapters/agy-1.4.2-fresh-loading-2026.10.18.json).

The earlier Darwin host reproducer draft was withdrawn after process-group
cleanup and audit gaps were found. It was not rerun. The retained historical
observations above remain separate and do not inherit later guard settings.

A fresh official Linux x64 1.4.2 package was checksum-verified as data and
tested in a disposable Docker Desktop container with a read-only root, no
network, no host mounts, closed stdin, and task-owned tmpfs. AOK produced the
expected blocked/incomplete import and export draft. The native CLI emitted no
agent identity or description for positive, absent, or malformed project
controls. Container and derived-image cleanup were confirmed. The probe status
remains `not-verified`; parsed output never auto-promotes this gate. See the
[Linux container evidence](../../../docs/evidence/native-adapters/agy-linux-container-2026-10-18.json)
and [bounded reproducer](../../../scripts/native-loading/probe-agy-linux-container.py).
Runtime enforcement remains unverified.

The adapter tests validate parsing, preservation, and semantic round-tripping;
they do not establish host discovery, selection, invocation, or enforcement.
No model or imported/custom command was run. Historical probe results retain
the isolation limits recorded with each observation; the strengthened guard
in the current reproducer does not apply retroactively.
