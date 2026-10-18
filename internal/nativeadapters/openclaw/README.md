# OpenClaw native projection

This adapter projects a deliberately small subset into the home-scoped native
OpenClaw configuration and workspace bootstrap format. It takes exactly two
explicit byte inputs: `.openclaw/openclaw.json` and
`.openclaw/workspace/AGENTS.md`. The caller materializes those relative paths
inside a disposable OpenClaw home/state directory; this package does not read
files, follow links, execute OpenClaw, or access a user's home directory.

The recognized fields are:

| AOK field | OpenClaw field |
|---|---|
| `agent.metadata.id` | key of the sole `agents.entries` item |
| `agent.identity.callsign` | `agents.entries.<id>.identity.name` |
| `executionProfile.model.selector` | `agents.entries.<id>.model` string |
| `agent.instructions.sources` | bytes of the workspace `AGENTS.md` file |

The isolated-home scope uses OpenClaw's documented default workspace path
`~/.openclaw/workspace`. A custom workspace path is rejected. Inputs with more
than one configured agent are rejected because this adapter has no selection
argument. Other config keys are retained as unmapped native values and are
re-emitted. Comments and formatting are not retained.

OpenClaw documents `openclaw.json` as JSON5, and explicitly accepts regular
JSON too. The parser in this package accepts JSON and the JSONC subset (comments
and trailing commas). JSON5-only syntax such as unquoted keys, single-quoted
strings, and numeric extensions is rejected. No claim is made that this parser
accepts every legal OpenClaw config file.

The current OpenClaw source defines `AgentsConfig.entries` as
`Record<string, AgentEntryConfig>` and examples document that the entry key is
the stable agent id. The documentation also defines per-agent `workspace`,
`model`, `identity.name`, and workspace `AGENTS.md` bootstrap instructions.
Sources checked for this mapping:

- [OpenClaw agent config types](https://github.com/openclaw/openclaw/blob/main/src/config/types.agents.ts)
- [OpenClaw configuration examples](https://docs.openclaw.ai/gateway/configuration-examples)
- [Per-agent entries and routing](https://docs.openclaw.ai/gateway/config-agents/entries-and-multi-agent)
- [Agent workspace](https://docs.openclaw.ai/agent-workspace)

OpenClaw 2026.9.4 was installed in a disposable prefix with lifecycle scripts
disabled. Its packaged `loadWorkspaceBootstrapFiles` loader returned the
rendered `AGENTS.md` bytes while reporting a missing file in a separate
negative control. An isolated CLI roster attempt returned
`{"ok":false,"error":{"type":"cli_error","message":"unable to open database file"}}`
despite exiting zero. Source inspection of the pinned state database coordinator
found its non-Windows lifecycle lock directory hardcoded to `/tmp`, ignoring
`TMPDIR`; the final Darwin probe therefore skips CLI roster execution under the strict
temp-root write sandbox. This Darwin result is component-loading evidence only, recorded in
`docs/evidence/native-adapters/openclaw-2026.9.4.json`.

A separate Linux container verification completed the official pinned package's
install lifecycle inside a disposable volume, then mounted the package read-only
for unprivileged, network-disabled CLI runs with private `/tmp` and home tmpfs.
No host files, credentials or Docker socket were mounted. `agents list --json`
returned the generated agent's id/model, the absent control omitted it, malformed
configuration failed with a parse diagnostic, and the native workspace loader
returned the exact rendered instructions with a missing-file control. The
version/digest/integrity and results are saved in
[`openclaw-container-2026.9.4.json`](../../../docs/evidence/native-adapters/openclaw-container-2026.9.4.json).
Reproduce with
[`verify_openclaw_container.py`](../../../scripts/native-loading/verify_openclaw_container.py)
using an explicit built AOK binary and the documented pinned Node image.
The descriptor marks isolated-home loading verified for this Linux package.
This does not establish Darwin CLI loading, agent invocation, runtime enforcement,
or canonical readiness. The engine keeps imports/exports blocked as incomplete
drafts, and required unsupported controls block inspection/export. Permission, purpose, capability, tool, routing, and
runtime controls are unsupported by this mapping.

OpenClaw validates its complete configuration schema. Preserved unknown fields
must still be valid in the tested host version; for example, the host rejects
unrecognized root keys even though the projection can retain them inertly.
The committed host-load fixture uses the documented `update.checkOnStart`
field for this preservation check.
