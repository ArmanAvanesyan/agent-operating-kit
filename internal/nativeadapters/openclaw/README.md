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

The host is not installed in the development environment. Accordingly,
`Descriptor().HostLoading.Status` is `unavailable`; no host compatibility or
runtime enforcement is inferred from the source-defined schema. Syntax and
projection inspection only establish that the supplied bytes are understood;
they do not establish that OpenClaw loads or enforces them. The native engine
keeps imported/exported bundles blocked as incomplete drafts, and required
unsupported controls block inspection/export. Permission,
purpose, capability, tool, routing, and runtime controls are unsupported by
this mapping. The separate host readiness gate must remain blocked until
OpenClaw is available and a bounded loading probe is recorded.
