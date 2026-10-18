# Native agent format inventory

Host inventory observed 2026-10-16; implementation status updated 2026-10-18.
This is a feasibility inventory, not adapter completion or
runtime enforcement evidence. Version/help inspection did not run inference,
install software, or change user configuration. Official documentation may be
newer than installed hosts; each adapter must pin its tested format and host.

| Target | Native format and candidate scope | Local host | Remaining native gate |
|---|---|---|---|
| Codex | Project `.codex/agents/<name>.toml`; project `.codex/config.toml` controls concurrency | `codex-cli 0.162.1`; temporary npm `0.163.0` | [No-turn interface audit](../../evidence/native-adapters/codex-cursor-discovery-2026.10.18.json); [startup parser acceptance verified](../../evidence/native-adapters/codex-0.162.1.json); child-agent loading/selection remains unverified |
| Claude Code | Project `.claude/agents/*.md`, YAML frontmatter and prompt body; user/plugin scopes differ | `2.1.281` | Project discovery verified; [evidence](../../evidence/native-adapters/claude-2.1.281.json); plugin loading/runtime enforcement unverified |
| OpenCode | Project `.opencode/agents/*.md` or `opencode.json` agent entries | `1.18.35` | Project loading and round trip verified; [evidence](../../evidence/native-adapters/opencode-1.18.35.json); runtime enforcement unverified |
| Cursor | Project `.cursor/agents/*.md`, YAML frontmatter and prompt body | `cursor-agent 2026.04.17-787b533` | [ACP startup/privacy blocker](../../evidence/native-adapters/codex-cursor-discovery-2026.10.18.json); shipped-component discovery verified, full CLI gate open |
| Gemini | Project `.gemini/agents/*.md`; personal `~/.gemini/agents/*.md` | Temporary npm CLI 0.61.0 | Guarded project discovery verified before auth; [evidence](../../evidence/native-adapters/gemini-cli-0.61.0.json); invocation/runtime enforcement unverified |
| OpenClaw | Config roster plus workspace bootstrap files; isolated configuration-home scope | Temporary npm 2026.9.4 / Linux Node 26.7.0 | Isolated roster/workspace discovery verified; [container evidence](../../evidence/native-adapters/openclaw-container-2026.9.4.json); Darwin CLI and runtime enforcement unverified |
| Hermes Agent | Candidate isolated profile/home, with config and persona files; broader than specialist definition | Direct installed CLI `vunknown (2026.9.24)`, Python 3.14.7; source hashes recorded | [Profile-list and separate metadata/SOUL loading verified](../../evidence/native-adapters/hermes-cli-profile-list.json); package-manager launcher selection and full profile sufficiency unverified |
| agy | Workspace `.agents/agents/<name>.md` or `<name>/agent.md`, YAML frontmatter | `1.3.6` | [Copied 1.3.6 CLI audit](../../evidence/native-adapters/agy-1.3.6-loading.json): listing aliases return no entries; interactive manager requires login |

## Official sources and limits

- [Codex subagents](https://learn.chatgpt.com/docs/agent-configuration/subagents):
  native TOML examples include name, description, model,
  model_reasoning_effort and developer_instructions. Do not assume every global
  config field is accepted in an agent definition.
- [Claude Code subagents](https://code.claude.com/docs/en/sub-agents): native
  frontmatter configures identity, tools/model and other scope-sensitive fields.
  Plugin agents do not have the same hooks, MCP and permission-mode behavior as
  project agents. Export must report unsupported required controls as blocked.
- [OpenCode agents](https://opencode.ai/docs/agents/): current project Markdown
  directory is plural `agents`; native JSON agent entries also exist. Preserve
  unknown fields without interpreting commands. Avoid copying stale singular
  paths from older repository research.
- [Cursor subagents](https://cursor.com/docs/subagents): native project and user
  Markdown agents, plus compatibility directories, are documented. Compatibility
  paths do not establish complete field equivalence with Claude or Codex.
- [Gemini subagents](https://geminicli.com/docs/core/subagents/) and
  [commands](https://geminicli.com/docs/cli/commands/): `/agents list` and
  `/agents reload` discover native definitions. `GEMINI.md` is contextual guidance,
  not a substitute for an agent definition.
- [OpenClaw multi-agent](https://docs.openclaw.ai/concepts/multi-agent/),
  [agent CLI](https://docs.openclaw.ai/cli/agents), and
  [workspace](https://docs.openclaw.ai/agent-workspace): roster and workspace form
  the agent. Current documentation describes `agents.entries` and legacy
  `agents.list` migration; the tested host version must determine accepted shape.
  `AGENTS.md` alone is incomplete. CLI `agents list` is a candidate safe probe.
- [Hermes profile guide](https://github.com/NousResearch/hermes-agent/blob/main/website/docs/user-guide/profiles.md)
  and [CLI reference](https://github.com/NousResearch/hermes-agent/blob/main/website/docs/reference/cli-commands.md):
  profiles cover a wider configuration home and archives can include runtime
  state. Local `hermes import-agent --help` accepts Claude Code and Codex inputs;
  that is not evidence of a standalone Hermes agent export format. AOK must not
  archive secrets, sessions or private memories to claim compatibility.
- [Antigravity subagents](https://antigravity.google/docs/subagents/) and
  [agent commands](https://www.antigravity.google/docs/cli/commands/agents/):
  documented workspace frontmatter includes identity, tools, model and execution
  policy. Flat and nested agent paths are documented; test both explicitly.
  `agy agent` returned no agents before creating any test configuration.

## Evidence classification

`command -v` located Codex, Claude, OpenCode, Cursor, Hermes and agy. It did not
locate Gemini or OpenClaw initially. Installed CLI versions came from `--version`;
subsequent temporary package versions and source-only Hermes identity are
recorded with their distinct provenance above.
The earlier Hermes/agy help and listing probes were intended to be read-only;
the subsequent bootstrap/update side effects and audit are recorded below. OpenCode, Claude and Gemini project adapters plus the OpenClaw isolated-home Linux adapter have generated-file discovery
evidence linked above. The other three native gates remain open. Gemini verification
used a temporary install; its earlier unintended global updater install was attributed
and removed, and agy was restored from its verified pre-update backup. See the
[follow-up audit](../../evidence/native-adapters/loading-followup.md). Future evidence must record
exact commands, sanitized output, isolated configuration paths, native fixture
hashes, host version, scope, and whether the observation proves syntax, discovery,
loading or runtime enforcement. Never conflate those categories.
