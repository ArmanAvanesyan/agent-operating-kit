# Native agent format inventory

Observed 2026-10-16. This is a feasibility inventory, not adapter completion or
runtime enforcement evidence. Version/help inspection did not run inference,
install software, or change user configuration. Official documentation may be
newer than installed hosts; each adapter must pin its tested format and host.

| Target | Native format and candidate scope | Local host | Remaining native gate |
|---|---|---|---|
| Codex | Project `.codex/agents/<name>.toml`; project `.codex/config.toml` controls concurrency | `codex-cli 0.162.1` | Isolated native loading; validate per-agent allowed fields and model/reasoning support |
| Claude Code | Project `.claude/agents/*.md`, YAML frontmatter and prompt body; user/plugin scopes differ | `2.1.281` | Isolated discovery and project/plugin negative scope tests |
| OpenCode | Project `.opencode/agents/*.md` or `opencode.json` agent entries | `1.18.35` | Isolated discovery and supported-field round trips |
| Cursor | Project `.cursor/agents/*.md`, YAML frontmatter and prompt body | `cursor-agent 2026.04.17-787b533` | Verify installed CLI discovery/loading; docs alone insufficient |
| Gemini | Project `.gemini/agents/*.md`; personal `~/.gemini/agents/*.md` | Not installed | Native host loading unavailable; documentation feasibility only |
| OpenClaw | Config roster plus workspace bootstrap files; isolated configuration-home scope | Not installed | Native host unavailable; pin roster version and validate bundle dependencies |
| Hermes Agent | Candidate isolated profile/home, with config and persona files; broader than specialist definition | `v0.21.6+636.gd4d2ab7 (2026.9.24)` | Verify minimally sufficient profile, exclude credentials/runtime state and demonstrate native loading |
| agy | Workspace `.agents/agents/<name>.md` or `<name>/agent.md`, YAML frontmatter | `1.3.6` | Verify discovery of test agent; listing models does not meet gate |

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
locate Gemini or OpenClaw. Versions above came from each host's `--version`.
Hermes profile/help and agy agent listing were read-only probes. No native fixture
has been loaded yet by this implementation effort. Future evidence must record
exact commands, sanitized output, isolated configuration paths, native fixture
hashes, host version, scope, and whether the observation proves syntax, discovery,
loading or runtime enforcement. Never conflate those categories.
