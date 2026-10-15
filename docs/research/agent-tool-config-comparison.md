# Agent Tool Configuration Comparison: Claude Code, Codex, Cursor, OpenCode

Reference comparison of how four coding agents are configured: where config lives,
how instruction files layer, and how commands, skills, hooks, plugins, subagents, MCP
servers, permissions, memory, models and headless modes are declared.

This is reference material. It does not change AOK adapters or renders. The same facts
are available as structured data in `agent-tool-config-matrix.json` (one record per tool
and dimension, with sources and local verification).

## Scope and versions

| Tool | Version checked locally | Docs root | Local config studied |
|---|---|---|---|
| Claude Code | `claude 2.1.281` | https://code.claude.com/docs | `~/.claude/`, `~/.claude.json` |
| Codex | `codex-cli 0.162.1` | https://learn.chatgpt.com/docs (developers.openai.com/codex redirects here) | `~/.codex/`, `~/.agents/`, one project `.codex/` |
| Cursor | `cursor-agent 2026.04.17-787b533` (GUI binary not on PATH) | https://cursor.com/docs | `~/.cursor/`, 17 cached plugins, 25 built-in skills |
| OpenCode | `opencode 1.18.35` | https://opencode.ai/docs, schema https://opencode.ai/config.json | `~/.config/opencode/`, `~/.opencode/`, plugin SDK types |

Docs checked on 2026-10-15. Verification used current primary-source text, pinned OpenCode source, CLI version/help output, and deterministic comparison of the Codex selector bundle. No inference sessions or sandbox/hook execution tests were run. Local evidence fields retain draft metadata observations, not fresh runtime tests. Facts were taken from official docs first, then the open
source repos where schemas live, then the local installs (structure and key names only,
never secret values). Appendix C lists what was confirmed on disk.

Two caveats that affect the whole document:

- OpenCode's published schema is ahead of the installed build. It deprecates the singular
  keys `agent`, `command`, `plugin`, `provider`, `permission` and the `mcp` map in favour
  of plural keys, an ordered `permissions` rule array and `mcp.servers`. The 1.18.35 build
  and current docs still use the singular keys. This document shows the v1 keys and notes
  v2 where it matters.
- Cursor has two config stacks: the IDE (VS Code style settings plus account-synced
  Customize and dashboard Team settings) and the CLI (`cli-config.json`). Content
  directories under `.cursor/` are shared by both.

## Concept equivalence map

| Neutral concept | Claude Code | Codex | Cursor | OpenCode |
|---|---|---|---|---|
| User config file | `~/.claude/settings.json` (JSON) | `~/.codex/config.toml` (TOML) | `~/.cursor/cli-config.json` (CLI) and IDE `settings.json` | `~/.config/opencode/opencode.json(c)` |
| Project config file | `.claude/settings.json` + `.claude/settings.local.json` | `.codex/config.toml` (trusted projects only) | `.cursor/cli.json` (permissions only) + `.cursor/*` content dirs | `opencode.json(c)` at root or in `.opencode/` |
| Managed / enterprise | `managed-settings.json`, MDM, server-managed | `/etc/codex/config.toml`, `requirements.toml`, MDM | Dashboard Team settings, MDM `hooks.json` | `/Library/Application Support/opencode/`, `/etc/opencode/`, MDM plist |
| Instruction file | `CLAUDE.md` (+ `.claude/rules/*.md`); `AGENTS.md` fallback | `AGENTS.md`, `AGENTS.override.md` | `.cursor/rules/*.mdc`; `AGENTS.md`, `CLAUDE.md` read | `AGENTS.md` (falls back to `CLAUDE.md`); `instructions` globs |
| Custom command | `.claude/commands/*.md` (merged into skills) | `~/.codex/prompts/*.md` (deprecated for skills) | `.cursor/commands/*.md` (superseded by skills) | `.opencode/command/*.md` or `command` config |
| Skill | `skills/<name>/SKILL.md` | `.agents/skills/<name>/SKILL.md` | `.cursor/skills/<name>/SKILL.md` (+ `.agents/skills`) | `.opencode/skill/<name>/SKILL.md` (+ `.claude/skills`, `.agents/skills`) |
| Hook | `hooks` in settings or plugin `hooks/hooks.json`; 34 PascalCase events | `.codex/hooks.json` or `[hooks]` TOML; 12 PascalCase events | `.cursor/hooks.json` `{version:1, hooks}`; camelCase events | Plugin functions (`tool.execute.before` etc.); no hooks file |
| Plugin manifest | `.claude-plugin/plugin.json` | `.codex-plugin/plugin.json` | `.cursor-plugin/plugin.json` | `plugin` array (npm, git, local `.ts/.js`) |
| Marketplace | `.claude-plugin/marketplace.json`; `extraKnownMarketplaces` | `.agents/plugins/marketplace.json`; `[marketplaces.x]` | cursor.com/marketplace; `.cursor-plugin/marketplace.json`; also `.agents/plugins/marketplace.json` | none (npm registry) |
| Subagent | `.claude/agents/*.md` | `[agents]` + `.codex/agents/*.toml` | `.cursor/agents/*.md` | `.opencode/agent/*.md` or `agent` config |
| MCP server | `.mcp.json`, `~/.claude.json`, `claude mcp add` | `[mcp_servers.x]`, `codex mcp add` | `.cursor/mcp.json`, `~/.cursor/mcp.json` | `mcp.<name>` in config |
| Permission rule | `Tool(specifier)` in `permissions.allow/ask/deny` | `approval_policy`, `sandbox_mode`, Starlark `prefix_rule` | `Shell()/Read()/Write()/WebFetch()/Mcp()` in `permissions` | `permission.<tool>` with `allow/ask/deny` patterns |
| Sandbox | Seatbelt / bubblewrap, off by default | Seatbelt / bubblewrap+seccomp, on by default | Seatbelt / Landlock; IDE auto-run enabled, CLI default disabled | none described |
| Memory | `~/.claude/projects/<p>/memory/MEMORY.md` | `~/.codex/memories/` (off by default) | current Rules; legacy memory history unverified | none beyond AGENTS.md |
| Headless | `claude -p`, Agent SDK, GitHub Action | `codex exec --json`, SDKs, Cloud, Action | `agent -p`, Cloud Agents API, SDK | `opencode run`, `serve`, SDK, Action |

## 1. Config files and precedence

| | Claude Code | Codex | Cursor | OpenCode |
|---|---|---|---|---|
| Format | JSON | TOML | JSON (CLI), JSONC (IDE) | JSON or JSONC |
| User | `~/.claude/settings.json`; `~/.claude.json` for account, trust, local MCP | `~/.codex/config.toml` (`$CODEX_HOME`) | `~/.cursor/cli-config.json`; IDE `~/Library/Application Support/Cursor/User/settings.json` | `~/.config/opencode/opencode.json(c)` |
| Project | `.claude/settings.json` (shared), `.claude/settings.local.json` (gitignored) | `.codex/config.toml`, root to cwd, only when trusted | `.cursor/cli.json` (permissions only, session only) | `opencode.json(c)` walked up to worktree; every `.opencode/` dir on the walk |
| Profiles | none (use `--settings`) | `$CODEX_HOME/<name>.config.toml` via `--profile` | none | none |
| Managed | `managed-settings.json` + `managed-settings.d/`, MDM plist, registry, server-managed | `/etc/codex/config.toml`, `managed_config.toml`, `requirements.toml`, MDM | Team dashboard; MDM `hooks.json` | managed dir per OS; MDM plist `ai.opencode.managed` |
| Precedence (high to low) | managed > `--settings` > local > project > user | CLI `-c` > project > profile > user > cloud-managed > `/etc` > defaults; `requirements.toml` above all | Team > project > user | MDM plist > managed dir > `OPENCODE_CONFIG_CONTENT` > `.opencode` dirs > project > `OPENCODE_CONFIG` > global > remote well-known |
| Merge | scalars override; `permissions.allow` and sandbox arrays merge; stricter-wins exceptions | layer by key | deeper `.cursor/cli.json` wins; permissions merge | deep merge; `instructions` and `plugin` arrays union |
| Env var substitution | `${VAR}` in MCP fields; settings `env` block overrides shell | none in config; `-c key=value` overrides | `${env:NAME}` in `mcp.json` | `{env:VAR}` and `{file:path}` only; `${VAR}` not expanded |
| Key env vars | `CLAUDE_CONFIG_DIR`, `ANTHROPIC_*` | `CODEX_HOME`, `CODEX_API_KEY`, `OPENAI_API_KEY` | `CURSOR_CONFIG_DIR`, `CURSOR_API_KEY` | `OPENCODE_CONFIG`, `OPENCODE_CONFIG_DIR`, `OPENCODE_CONFIG_CONTENT`, `OPENCODE_DISABLE_PROJECT_CONFIG` |

Notes:

- Claude Code ignores scope-restricted keys such as `defaultMode: bypassPermissions` when
  they appear in the committed project file. Any `permissions.deny` at any scope wins.
- Codex marks some keys as not portable to project config: `model_provider`,
  `model_providers`, `notify`, `profile`, auth and base URL keys. Untrusted projects skip
  project config, hooks and rules entirely.
- Cursor's `.cursor/cli.json` is read but never written back. Approvals the user accepts
  land in `~/.cursor/cli-config.json`.
- OpenCode fails startup on unknown top-level keys and has no hot reload.

## 2. Instruction files

| | Claude Code | Codex | Cursor | OpenCode |
|---|---|---|---|---|
| Primary file | `CLAUDE.md` | `AGENTS.md` | `.cursor/rules/*.mdc` | `AGENTS.md` |
| User level | `~/.claude/CLAUDE.md`, `~/.claude/rules/*.md` | `~/.codex/AGENTS.override.md` else `~/.codex/AGENTS.md` | User Rules (account synced) | `~/.config/opencode/AGENTS.md`, fallback `~/.claude/CLAUDE.md` |
| Project walk | every ancestor `CLAUDE.md` or `.claude/CLAUDE.md`, root first | root to cwd, one file per dir, root first, 32 KiB cap | `.cursor/rules/` and nested `**/.cursor/rules/`; `AGENTS.md` root and nested | `AGENTS.md` then `CLAUDE.md` then `CONTEXT.md`, first match per dir |
| Local override | `CLAUDE.local.md` | `AGENTS.override.md` replaces that level | none | none |
| Nested lazy load | `<subdir>/CLAUDE.md` when files there are touched | not documented | nested AGENTS.md combined, more specific wins | nested AGENTS.md attached when a file beneath is read |
| Path-scoped rules | `.claude/rules/*.md` with `paths:` frontmatter | none | `.mdc` with `globs:` | `instructions` globs |
| Rule activation modes | always (CLAUDE.md), path match (rules) | always | Always, Auto Attached (globs), Agent Requested (description), Manual | always |
| Imports | `@path`, 4 hops | none | `@file` mentions in rules | `instructions` array accepts globs, `~/` paths and URLs |
| Other tools' files | `AGENTS.md` only when no `CLAUDE.md` present (2.1.277+) | none | reads `AGENTS.md` and root `CLAUDE.md`; `.cursorrules` deprecated | reads `CLAUDE.md`; `OPENCODE_DISABLE_CLAUDE_CODE` turns it off |

Notes:

- Only Cursor has typed rule activation. The `.mdc` frontmatter is `description`, `globs`,
  `alwaysApply`. The built-in `/migrate-to-skills` converts Agent Requested rules into
  skills, which is where Cursor is steering reusable guidance.
- Codex `project_doc_fallback_filenames` and `project_doc_max_bytes` are the only knobs.
  Codex does not read `CLAUDE.md`.
- AGENTS.md is the de facto shared file: Codex and OpenCode treat it as primary, Cursor
  reads it, Claude Code reads it as a fallback.

## 3. Custom commands and prompts

| | Claude Code | Codex | Cursor | OpenCode |
|---|---|---|---|---|
| Status | active; commands are merged into skills | deprecated in favour of skills | superseded by skills; no docs page remains | active |
| Location | `~/.claude/commands/`, `.claude/commands/`, plugin `commands/` | `~/.codex/prompts/*.md` only | `.cursor/commands/*.md`, `~/.cursor/commands/`, plugin `commands/` | `.opencode/command(s)/**/*.md`, `~/.config/opencode/command(s)/`, or `command.<name>` in config |
| Invocation | `/name`, `/sub:name`, `/plugin:name` | `/prompts:name` or `/name` | `/name` | `/name`; headless `opencode run --command name` |
| Frontmatter | same as SKILL.md minus `name`, `paths` | `description`, `argument-hint` | plain Markdown (plugins use `name`, `description`) | `description`, `agent`, `model`, `variant`, `subtask` |
| Arguments | `$ARGUMENTS`, `$ARGUMENTS[N]`, `$N`, named `$name` | `$1..$9`, `$ARGUMENTS`, `$NAME` | `$ARGUMENTS` seen only in plugin commands | `$ARGUMENTS`, `$1..$N` |
| Shell injection | `` !`cmd` `` | none | none | `` !`cmd` `` and `@path` |

Notes:

- Three of four tools now treat commands as a legacy shape of skills. OpenCode is the only
  one where commands remain a distinct first-class object with their own config key.
- Claude Code resolves `.claude/commands/deploy.md` and `.claude/skills/deploy/SKILL.md`
  to the same `/deploy`.

## 4. Skills

| | Claude Code | Codex | Cursor | OpenCode |
|---|---|---|---|---|
| File | `<name>/SKILL.md` | `<name>/SKILL.md` + optional `agents/openai.yaml` | `<name>/SKILL.md` | `<name>/SKILL.md` |
| Project paths | `.claude/skills/` (walked up), nested `<subdir>/.claude/skills/` | `.agents/skills/` in cwd, parents, repo root | `.cursor/skills/`, `.agents/skills/`; compat `.claude/skills/`, `.codex/skills/` | `.opencode/skill(s)/`, `.claude/skills/`, `.agents/skills/` |
| User paths | `~/.claude/skills/`, synced `~/.claude/skills/synced/` | `~/.agents/skills/`, `/etc/codex/skills/` | `~/.cursor/skills/`, `~/.agents/skills/`; built-ins in reserved `~/.cursor/skills-cursor/` | `~/.config/opencode/skill(s)/`, `~/.claude/skills/`, `~/.agents/skills/` |
| Required frontmatter | none (all optional) | `name`, `description` | `name` (max 64), `description` (max 1024) | `name` (kebab, 1 to 64), `description` |
| Notable optional fields | `disable-model-invocation`, `user-invocable`, `allowed-tools`, `model`, `effort`, `context: fork`, `agent`, `hooks`, `paths` | `agents/openai.yaml` with `interface`, `policy.allow_implicit_invocation`, `dependencies.tools` | `paths`, `disable-model-invocation`, `icon`, `color`, `metadata` | `license`, `compatibility`, `metadata` with `opencode/slash`, `opencode/autoinvoke` |
| Invocation | `/name`; auto by description unless disabled | `$skill-name`; implicit by description | `/name`, `@name`; auto by description | `skill` tool; `/name` if `opencode/slash: true` |
| Per-skill enable | `skillOverrides` setting | `[[skills.config]] path, enabled` | not documented | `permission.skill` patterns |
| Standard | agentskills.io plus extensions | open agent skills standard | same format | same format |

Notes:

- All four read the same `SKILL.md` shape and three of four read each other's skill
  directories. `~/.agents/skills` is honoured by Codex, Cursor and OpenCode, so it is the
  most portable location for personal skills. Claude Code reads only its own paths.
- Among these four reviewed tools, Codex adds a sidecar (`agents/openai.yaml`) for UI metadata and
  MCP dependencies.

## 5. Hooks and lifecycle events

| | Claude Code | Codex | Cursor | OpenCode |
|---|---|---|---|---|
| Where | settings files (all scopes), plugin `hooks/hooks.json`, SKILL and agent frontmatter | `~/.codex/hooks.json`, `[hooks]` in config.toml, `.codex/hooks.json`, plugin hooks, managed | `.cursor/hooks.json`, `~/.cursor/hooks.json`, MDM paths, team dashboard, plugin `hooks/hooks.json` | plugin module exporting hook functions |
| Shape | `{"hooks": {"<Event>": [{"matcher", "hooks": [{"type", "command", "timeout", ...}]}]}}` | same shape as Claude Code | `{"version": 1, "hooks": {"<event>": [{"command", "type", "timeout", "matcher", "failClosed"}]}}` | `async (input) => ({ "tool.execute.before": async (i, o) => {...} })` |
| Event count and case | 34, PascalCase | 12, PascalCase | 21, camelCase | 16 hook keys plus a bus `event` stream |
| Core tool events | `PreToolUse`, `PostToolUse`, `PostToolUseFailure`, `PermissionRequest` | `PreToolUse`, `PostToolUse`, `PermissionRequest` | `preToolUse`, `postToolUse`, `beforeShellExecution`, `afterFileEdit`, `beforeMCPExecution` | `tool.execute.before`, `tool.execute.after`, `permission.ask` |
| Session events | `SessionStart`, `SessionEnd`, `PreCompact`, `PostCompact`, `Stop` | `SessionStart`, `SessionEnd`, `PreCompact`, `PostCompact`, `Stop`, `Interrupt` | `sessionStart`, `sessionEnd`, `preCompact`, `stop` | bus `session.*`, `experimental.session.compacting` |
| Handler types | command, http, mcp_tool, prompt, agent | command, mcp_tool (prompt and agent parsed, skipped) | command, prompt | JavaScript function |
| Input | JSON on stdin | JSON on stdin | JSON on stdin | typed argument object |
| Block | exit 2 or JSON `permissionDecision: deny` | exit 2 or `permissionDecision: deny` | exit 2 or `{permission: deny}` | mutate output or throw |
| Default timeout | 600 s command | 600 s | per hook, platform default | none |
| Trust | project hooks need workspace trust | per-hook `trusted_hash` in `[hooks.state]`; `/hooks` to approve | project hooks run after trust | plugins run as code |

Notes:

- Codex deliberately adopted the Claude Code hook schema. A `.codex/hooks.json` written in
  Claude Code's format is accepted as is, and the local specimen in the organizy repo is
  exactly that. The event set is a subset, with `Interrupt` and `PermissionRequest` added.
- Cursor uses the same stdin and exit-code contract but camelCase names and a `version`
  field, plus IDE-only events for Tab and `workspaceOpen`.
- OpenCode has no declarative hooks. Every lifecycle customisation is a plugin, which is
  more powerful and less portable.

## 6. Plugins and marketplaces

| | Claude Code | Codex | Cursor | OpenCode |
|---|---|---|---|---|
| Manifest | `.claude-plugin/plugin.json` (optional) | `.codex-plugin/plugin.json` | `.cursor-plugin/plugin.json` (`name` required) | none; `plugin` array entries |
| Bundled content | skills, commands, agents, hooks, `.mcp.json`, `.lsp.json`, output styles, workflows, themes, monitors | skills, `.mcp.json`, `.app.json`, hooks, `interface` metadata | rules, skills, agents, commands, hooks, `mcpServers`, `variables` | code: hooks, custom tools, auth, providers |
| Marketplace file | `.claude-plugin/marketplace.json` | `.agents/plugins/marketplace.json` | `.cursor-plugin/marketplace.json`; also reads `.agents/plugins/marketplace.json` | none |
| Registration | `extraKnownMarketplaces` setting, `claude plugin marketplace add` | `[marketplaces.x]` in config.toml, `codex plugin marketplace add` | cursor.com/marketplace, Customize panel | npm, git+https, local path |
| Enable | `enabledPlugins["name@marketplace"]` | `[plugins."name@marketplace"] enabled` | Customize > Install, user or project scope | presence in `plugin` array |
| Cache | `~/.claude/plugins/cache/<marketplace>/<plugin>/<version>/` | `~/.codex/plugins/cache/<marketplace>/<plugin>/<version>/` | `~/.cursor/plugins/cache/<marketplace>/<plugin>/<sha>/` | Bun install at startup |
| Plugin root variable | `${CLAUDE_PLUGIN_ROOT}`, `${CLAUDE_PLUGIN_DATA}` | `PLUGIN_ROOT`, `PLUGIN_DATA` (Claude aliases accepted) | `${CURSOR_PLUGIN_ROOT}` | `directory`, `worktree` in plugin input |
| Org controls | `strictKnownMarketplaces`, `blockedMarketplaces`, managed `enabledPlugins` | `requirements.toml` | Organization Groups; Default Off / On / Required | none |

Notes:

- The three manifest-based tools share a layout convention: a dot-directory manifest at
  the plugin root plus `skills/`, `hooks/hooks.json`, `agents/` and an MCP file. The
  superpowers plugin cached locally ships all three manifests from one repo.
- Both Codex and Cursor read the open `.agents/plugins/marketplace.json` format, which
  makes `.agents/` the second shared convention after `SKILL.md`.
- Cursor's official plugins use keys the docs do not list: `displayName`, `category`,
  `tags`, `primaryColor`, `minClientVersions`.

## 7. Subagents and custom agents

| | Claude Code | Codex | Cursor | OpenCode |
|---|---|---|---|---|
| Definition | `.claude/agents/**/*.md`, `~/.claude/agents/`, plugin `agents/`, `--agents` JSON | TOML in `~/.codex/agents/` or `.codex/agents/`; `[agents]` table | `.cursor/agents/*.md`, `~/.cursor/agents/`; compat `.claude/agents`, `.codex/agents` | `.opencode/agent(s)/**/*.md` or `agent.<name>` config |
| Frontmatter | `name`, `description`, `tools`, `disallowedTools`, `model`, `permissionMode`, `maxTurns`, `skills`, `mcpServers`, `hooks`, `memory`, `isolation: worktree` | `name`, `description`, `developer_instructions`, `model`, reasoning keys, `skills.*` | `name`, `description`, `model` (`inherit`, id, `fast`, `id[fast=,effort=]`), `readonly`, `is_background` | `description`, `mode`, `model`, `temperature`, `prompt`, `permission`, `tools`, `hidden`, `steps`, `color` |
| Modes | subagent only; `--agent` runs session as one | roles `default`, `worker`, `explorer` | subagent only | `primary`, `subagent`, `all` |
| Built-ins | Explore, Plan, general-purpose | default, worker, explorer | Explore, Browser | build, plan, general, explore |
| Invocation | auto by description; `@agent-name` | user request, AGENTS.md, skills, proactive | auto; `/name`; parallel via worktrees | `@name`; `task` tool; Tab cycles primaries |
| Limits | `maxTurns`; description budget warning | `max_concurrent_threads_per_session` | none documented | `subagent_depth` (default 1), `steps` |
| Cannot override | plugin agents ignore hooks, mcpServers, permissionMode | `sandbox_mode`, `approval_policy`, `model_provider`, `mcp_servers` | no tools allowlist documented | none |

Notes:

- Claude Code and Cursor share the Markdown-with-frontmatter format closely enough that
  Cursor reads `.claude/agents` directly. Codex alone uses TOML.
- OpenCode explicitly distinguishes primary/subagent/all modes; Claude Code also supports a session persona. An OpenCode primary agent is the persona the user talks
  to, selected with Tab.

## 8. MCP servers

| | Claude Code | Codex | Cursor | OpenCode |
|---|---|---|---|---|
| Project file | `.mcp.json` `{"mcpServers": {...}}` | `[mcp_servers.x]` in `.codex/config.toml` | `.cursor/mcp.json` `{"mcpServers": {...}}` | `mcp.<name>` in `opencode.json` |
| User storage | `~/.claude.json` (`mcpServers` or per-project) | `~/.codex/config.toml` | `~/.cursor/mcp.json` | `~/.config/opencode/opencode.json` |
| Transports | stdio, http, sse, ws, sdk | stdio (`command`), http (`url`) | stdio, http, sse | `local` (`command[]`), `remote` (`url`) |
| Auth | OAuth with DCR or CIMD; `claude mcp login` | `auth = oauth\|chatgpt`, `bearer_token_env_var`, `codex mcp login` | OAuth; `agent mcp login`; `auth {CLIENT_ID, CLIENT_SECRET}` | `oauth {clientId, clientSecret, scope, callbackPort}`; DCR on 401 |
| Env substitution | `${VAR}`, `${VAR:-default}` | `env_vars[]`, `env_http_headers{}` | `${env:NAME}`, `${workspaceFolder}` | `{env:VAR}`, `{file:path}` |
| Tool gating | `mcp__server__tool` permission rules | `enabled_tools`, `disabled_tools`, `default_tools_approval_mode` | enterprise per-tool allowlists | `tools: {"server*": false}` |
| Timeouts | `MCP_TIMEOUT`, `MCP_TOOL_TIMEOUT` env; `timeout` ms | `startup_timeout_sec` (10), `tool_timeout_sec` (60) | not documented | `timeout` ms (5000) |
| CLI | `claude mcp add\|list\|get\|remove\|login` | `codex mcp add\|list\|get\|remove\|login\|logout` | `agent mcp list\|list-tools\|login\|enable\|disable` | `opencode mcp add\|list\|auth\|logout\|debug` |
| Plugin MCP | `.mcp.json` or manifest `mcpServers` | `.mcp.json` | manifest `mcpServers` or `mcp.json` | plugin code |

Notes:

- Claude Code and Cursor use the same `mcpServers` JSON shape in a project file. Codex uses
  TOML tables and OpenCode a config object with an explicit `type`. A single MCP
  definition needs four destinations but three structural shapes: shared Claude/Cursor JSON, Codex TOML, and OpenCode typed config.
- Claude Code project `.mcp.json` requires per-user approval; `enableAllProjectMcpServers`
  or `enabledMcpjsonServers` pre-approve. Codex gates project MCP behind project trust.

## 9. Permissions, approvals and sandboxing

| | Claude Code | Codex | Cursor | OpenCode |
|---|---|---|---|---|
| Rule syntax | `Tool(specifier)`: `Bash(npm run *)`, `Edit(src/**)`, `WebFetch(domain:x)`, `mcp__s__t` | Starlark `prefix_rule(pattern=[...], decision=allow\|prompt\|forbidden)` | `Shell(cmd)`, `Read(glob)`, `Write(glob)`, `WebFetch(domain)`, `Mcp(server:tool)` | `permission.<tool>: allow\|ask\|deny` or `{pattern: action}` |
| Rule files | `permissions.allow/ask/deny` in settings | `~/.codex/rules/*.rules`, `.codex/rules/` | `permissions.allow/deny` in `cli-config.json`, `.cursor/cli.json`; IDE `permissions.json` | `permission` object in config; `OPENCODE_PERMISSION` env |
| Evaluation | deny, then ask, then allow; first match | most restrictive wins | deny beats allow | last matching rule wins |
| Modes | `default`, `acceptEdits`, `plan`, `auto`, `dontAsk`, `bypassPermissions` | `approval_policy`: `on-request`, `never`, granular; `sandbox_mode`: `read-only`, `workspace-write`, `danger-full-access` | CLI `approvalMode: allowlist` or `--force`; IDE Run Modes Auto-review, Allowlist, Run Everything | `--auto`; `plan` agent is `edit: deny` |
| Sandbox | Seatbelt, bubblewrap + socat; off by default (`sandbox.enabled`) | Seatbelt, bubblewrap + seccomp; on by default | Seatbelt, Landlock + seccomp; `sandbox.mode`, `readBoundary` | none described |
| Network policy | `sandbox.network.allowedDomains`, proxy | `[sandbox_workspace_write] network_access`; permission profiles `network.domains` | `sandbox.networkAccess`, `networkAllowlist` | none |
| Protected paths | `Read(./.env)` style deny rules | `.git`, `.agents`, `.codex`, `.aws` read-only | `.git/config`, `.git/hooks`, `.vscode`, `.cursorignore` | `.env` reads denied by default |
| Bypass flag | `--dangerously-skip-permissions` | `--dangerously-bypass-approvals-and-sandbox` (`--yolo`) | `--force` (`--yolo`) | `--auto` |
| Org lock | `allowManagedPermissionRulesOnly`, `disableBypassPermissionsMode` | `allowed_approval_policies`, `allowed_sandbox_modes` | Team dashboard fixes Run Modes | none |

Notes:

- Codex and Cursor converge on OS-level sandboxing by default. Claude Code ships it off and
  leans on the permission classifier. OpenCode relies on prompts alone.
- Claude Code's `Read` deny also blocks `Edit`, `Write` and recognised shell file commands.
  Codex's Starlark rules split linear shell chains per command. Cursor's rule strings are
  exact matches with globs.
- Codex retired `untrusted` and `--full-auto`; current docs show only `on-request`, `never`
  and the granular object.

## 10. Memory and persistence

| | Claude Code | Codex | Cursor | OpenCode |
|---|---|---|---|---|
| Agent-written memory | `~/.claude/projects/<project>/memory/MEMORY.md` plus topic files; types user, feedback, project, reference | `~/.codex/memories/` (`MEMORY.md`, `raw_memories.md`, summaries); off by default, machine-global | Rules; cloud Automations have separate persistent notes | none |
| Toggle | `/memory`, `autoMemoryEnabled`, `CLAUDE_CODE_DISABLE_AUTO_MEMORY` | `[features] memories = true`, `[memories]` keys | n/a | n/a |
| Transcripts | `~/.claude/projects/<project>/<session>.jsonl` | `~/.codex/sessions/<YYYY>/...`, `session_index.jsonl` | `~/.cursor/chats/<hash>/<uuid>/store.db` | `~/.local/share/opencode/opencode.db` (SQLite) |
| Resume | `--continue`, `--resume <id>`, `--fork-session` | `codex resume [id\|name] [--last]`, `codex fork` | `agent resume`, `--resume`, `--continue`, `/fork` | `-c`, `-s <id>`, `--fork`, `/sessions` |
| Prompt history | `~/.claude/history.jsonl` | `~/.codex/history.jsonl`, `[history] persistence` | `~/.cursor/prompt_history.json` | in database |
| Retention | `cleanupPeriodDays` (30), `claude purge` | `max_rollout_age_days` (30), `codex archive\|delete` | not documented | `opencode session delete`, `export`, `import` |
| Plans | `~/.claude/plans/*.md` | n/a | `~/.cursor/plans/*.plan.md` with `name`, `overview`, `todos`, `isProject` | n/a |
| Sharing | n/a | n/a | n/a | `share: manual\|auto\|disabled`; `opncd.ai/s/<id>` |

## 11. Model and provider configuration

| | Claude Code | Codex | Cursor | OpenCode |
|---|---|---|---|---|
| Model key | `model` in settings; `--model`; aliases `opus`, `sonnet`, `haiku`, `fable`, `[1m]` | `model`, `model_provider`, `model_reasoning_effort`, `service_tier` | cli-config `model {modelId, ...}`, `--model`; IDE picker, Auto modes, Max Mode | `model: provider/model#variant`, `small_model` |
| Selection order | `/model` > `--model` > `ANTHROPIC_MODEL` > `model` > `ANTHROPIC_DEFAULT_MODEL` | CLI > config layers | CLI flag > cli-config | flag > config > last used |
| Custom providers | Bedrock, Vertex (Agent Platform), Foundry, Claude Platform on AWS, `ANTHROPIC_BASE_URL` gateway | `[model_providers.x] base_url, env_key, wire_api` | BYOK: OpenAI, Anthropic, Google, Azure, Bedrock | `provider.<id>` with `npm`, `options.baseURL`, `models`; models.dev catalog |
| Local models | none | `--oss`, `--local-provider lmstudio\|ollama` | none | `@ai-sdk/openai-compatible` to Ollama, LM Studio, llama.cpp |
| Auth | OAuth subscription or `ANTHROPIC_API_KEY`, `apiKeyHelper` | `codex login`, `CODEX_API_KEY`, `OPENAI_API_KEY`, credential store | Cursor account; `CURSOR_API_KEY` for CLI | `opencode auth login`; `~/.local/share/opencode/auth.json` |
| Effort | `effortLevel`, `--effort`, `/effort` | `model_reasoning_effort` low to ultra | `model[effort=]` for subagents | `variant`, `reasoningEffort` option |
| Org controls | `availableModels`, `deniedModels`, `modelOverrides` | `forced_login_method`, `allowed_login_methods` | Model Access approvals | `enabled_providers`, `disabled_providers` |

## 12. Headless and automation surfaces

| | Claude Code | Codex | Cursor | OpenCode |
|---|---|---|---|---|
| One-shot | `claude -p "..."` | `codex exec "..."` | `agent -p "..."` | `opencode run "..."` |
| Output formats | `text`, `json`, `stream-json`; `--json-schema` | `--json` JSONL events; `--output-schema` | `text`, `json`, `stream-json` | `default`, `json` |
| Permissions in CI | `--allowedTools`, `--permission-mode`, `--dangerously-skip-permissions` | `-s`, `-a`, `--approve-for-me`, `--yolo` | `--force`, `--trust`, `--approve-mcps` | `--auto` |
| Server mode | none (SDK in-process) | `codex app-server`, `exec-server` | none | `opencode serve`, `opencode web`, `/doc` OpenAPI |
| SDK | `@anthropic-ai/claude-agent-sdk`, `claude-agent-sdk` (PyPI) | `@openai/codex-sdk`, `openai-codex` (PyPI) | `@cursor/sdk`, `cursor-sdk` (PyPI) | `@opencode-ai/sdk` |
| Cloud | `--cloud`, `--bg`, remote control, routines | `codex cloud exec\|status\|apply`, `@codex` on GitHub | Cloud Agents, `api.cursor.com/v1/agents`, Automations, Bugbot | none; GitHub action |
| GitHub Action | `anthropics/claude-code-action@v1` | `openai/codex-action@v1` | GitHub app plus Bugbot | `anomalyco/opencode/github@latest` |
| Editor protocol | none | none | n/a | `opencode acp` (Zed, JetBrains, Neovim) |

## Cross-cutting observations

**What has converged**

- `SKILL.md` is a shared format. Codex, Cursor and OpenCode read `~/.agents/skills` and the
  project `.agents/skills`; Cursor and OpenCode also read `.claude/skills`. A skill written
  with shared `name` and `description` can be reused when installed in each tool's supported discovery paths; optional fields are not universally portable.
- `AGENTS.md` is the shared instruction file. Codex and OpenCode treat it as primary,
  Cursor reads it alongside rules, Claude Code falls back to it when no `CLAUDE.md` exists.
- Claude Code and Codex share a command-hook envelope, but supported events, handlers,
  outputs, environment variables and trust differ. Cursor also uses JSON on stdin, with
  its own event names and event-specific output semantics.
- `.agents/plugins/marketplace.json` is read by both Codex and Cursor.
- Markdown-with-frontmatter subagents are shared between Claude Code and Cursor.

**What remains proprietary**

- Config formats: JSON, TOML, JSONC, MDC. No tool reads another's settings file.
- Permission rule languages: four syntaxes with different evaluation orders.
- MCP declarations: two JSON shapes, one TOML shape, one typed object.
- OpenCode's code-first plugins and hooks have no declarative equivalent elsewhere.
- Codex's Starlark execution policy and TOML subagents are unique.

**Portability guidance for a cross-tool kit**

- Ship skills under `.agents/skills/<name>/SKILL.md` with `name` and `description` only in
  the shared frontmatter. Put tool-specific fields in `metadata` or sidecars.
- Ship instructions as `AGENTS.md`. For Claude Code, either rely on the fallback or add a
  one-line `CLAUDE.md` that imports it with `@AGENTS.md`.
- Render hooks per tool, validating event, handler, output and environment compatibility.
  Shared command logic can be reused; schema resemblance does not prove execution parity.
  OpenCode requires plugin functions.
- Render MCP to four destinations using the three config shapes above.
- Treat permissions as per-tool policy. Only the intent (allow, ask, deny per tool and
  pattern) is portable.

## Appendix A. File location reference

### Claude Code

| Scope | Paths |
|---|---|
| User | `~/.claude/settings.json`, `~/.claude/CLAUDE.md`, `~/.claude/rules/`, `~/.claude/skills/`, `~/.claude/agents/`, `~/.claude/commands/`, `~/.claude/plugins/`, `~/.claude/projects/<project>/memory/`, `~/.claude.json` |
| Project | `.claude/settings.json`, `.claude/settings.local.json`, `CLAUDE.md`, `.claude/CLAUDE.md`, `CLAUDE.local.md`, `.claude/rules/`, `.claude/skills/`, `.claude/agents/`, `.claude/commands/`, `.mcp.json`, `AGENTS.md` (fallback) |
| Plugin | `.claude-plugin/plugin.json`, `skills/`, `commands/`, `agents/`, `hooks/hooks.json`, `.mcp.json`, `.lsp.json` |
| Managed | `/Library/Application Support/ClaudeCode/`, `/etc/claude-code/`, `C:\Program Files\ClaudeCode\`; `managed-settings.json`, `managed-mcp.json`, `CLAUDE.md`, `.claude/skills/`, `.claude/agents/` |

### Codex

| Scope | Paths |
|---|---|
| User | `~/.codex/config.toml`, `~/.codex/<profile>.config.toml`, `~/.codex/AGENTS.md`, `~/.codex/AGENTS.override.md`, `~/.codex/hooks.json`, `~/.codex/rules/*.rules`, `~/.codex/agents/*.toml`, `~/.codex/prompts/` (deprecated), `~/.codex/memories/`, `~/.codex/sessions/`, `~/.codex/plugins/cache/`, `~/.agents/skills/`, `~/.agents/plugins/marketplace.json` |
| Project | `.codex/config.toml`, `.codex/hooks.json`, `.codex/rules/`, `.codex/agents/`, `AGENTS.md`, `AGENTS.override.md`, `.agents/skills/`, `.agents/plugins/marketplace.json` |
| Plugin | `.codex-plugin/plugin.json`, `skills/`, `.mcp.json`, `.app.json`, `hooks/hooks.json` |
| Managed | `/etc/codex/config.toml`, `/etc/codex/managed_config.toml`, `/etc/codex/requirements.toml`, `/etc/codex/skills/`, MDM `com.openai.codex` |

### Cursor

| Scope | Paths |
|---|---|
| User | `~/.cursor/cli-config.json`, `~/.cursor/hooks.json`, `~/.cursor/mcp.json`, `~/.cursor/skills/`, `~/.cursor/agents/`, `~/.cursor/commands/`, `~/.cursor/permissions.json`, `~/.cursor/sandbox.json`, `~/.cursor/plans/`, `~/.cursor/plugins/{cache,local}/`, `~/.cursor/skills-cursor/` (reserved built-ins), IDE `settings.json` |
| Project | `.cursor/cli.json`, `.cursor/rules/*.mdc`, `.cursor/hooks.json`, `.cursor/mcp.json`, `.cursor/skills/`, `.cursor/agents/`, `.cursor/commands/`, `.cursor/permissions.json`, `.cursor/sandbox.json`, `.cursor/BUGBOT.md`, `.cursor/worktrees.json`, `.cursor/environment.json`, `AGENTS.md`, `CLAUDE.md`, `.cursorrules` (deprecated), `.cursorignore` |
| Plugin | `.cursor-plugin/plugin.json`, `rules/`, `skills/`, `agents/`, `commands/`, `hooks/hooks.json`, `mcp.json` |
| Managed | `/Library/Application Support/Cursor/hooks.json`, `/etc/cursor/hooks.json`, `C:\ProgramData\Cursor\hooks.json`; dashboard Team settings |

### OpenCode

| Scope | Paths |
|---|---|
| Global | `~/.config/opencode/opencode.json(c)`, `~/.config/opencode/AGENTS.md`, `~/.config/opencode/{agent,command,plugin,skill,tool}(s)/`, `~/.config/opencode/package.json`, `~/.opencode/`, `~/.local/share/opencode/` (data), `~/.cache/opencode/` |
| Project | `opencode.json(c)`, `.opencode/opencode.json(c)`, `.opencode/{agent,command,plugin,skill,tool}(s)/`, `AGENTS.md`, `CLAUDE.md` (fallback), `.claude/skills/`, `.agents/skills/` |
| Plugin | npm package or `.opencode/plugin/*.ts\|js` exporting `(input) => Hooks` |
| Managed | `/Library/Application Support/opencode/`, `/etc/opencode/`, `%ProgramData%\opencode`, MDM `ai.opencode.managed` |

## Appendix B. Sources

Full per-record source lists with titles are in `agent-tool-config-matrix.json`. Primary
pages by tool:

- Claude Code: https://code.claude.com/docs/en/settings, /settings-reference, /memory,
  /skills, /commands, /hooks, /plugins/manifest-reference, /plugins/marketplace-reference,
  /plugins/loading, /sub-agents, /mcp, /permissions, /permission-modes, /sandboxing,
  /model-config, /headless, /cli-reference, /agent-sdk/overview, /github-actions.
- Codex: https://learn.chatgpt.com/docs/config-file/config-reference, /config-file/config-advanced,
  /enterprise/managed-configuration, /agent-configuration/agents-md, /build-skills,
  /custom-prompts, /hooks, /plugins, /agent-configuration/subagents, /extend/mcp,
  /agent-approvals-security, /sandboxing, /agent-configuration/rules, /permissions,
  /customization/memories, /non-interactive-mode, /codex-sdk, /cloud, /github-action.
- Cursor: https://cursor.com/docs/cli/reference/configuration, /cli/reference/permissions,
  /cli/reference/parameters, /cli/headless, /context/rules, /context/skills, /agent/hooks,
  /plugins/overview, /plugins/building, /agent/subagents, /context/mcp,
  /agent/security/run-modes, /models, /cloud-agent, /cloud-agent/api/endpoints,
  /automations, /bugbot, /enterprise/security-hardening; https://github.com/cursor/plugins.
- OpenCode: https://opencode.ai/docs/config, /rules, /commands, /skills, /plugins,
  /custom-tools, /agents, /mcp-servers, /permissions, /providers, /models, /cli, /server,
  /sdk, /share, /github, /acp; https://opencode.ai/config.json;
  https://github.com/anomalyco/opencode at tag v1.18.35 (`packages/opencode/src/config/`,
  `packages/plugin/src/`).
- Third-party hook introduction dates and Cursor memory-removal history were excluded
  from verified conclusions because current primary sources did not establish them.

## Appendix C. Local verification log

The table preserves the draft's local metadata snapshot, dated 2026-10-15, across previously inspected projects; it is not a fresh inventory of this checkout. CLI versions were rechecked during review. These path/count observations do not verify runtime behavior. "Present" means the path or key exists;
"absent" is expected for managed paths and unused optional files.

| Tool | Present | Absent on this machine |
|---|---|---|
| Claude Code | `~/.claude/settings.json` (keys: env, permissions, model, skillOverrides, enabledPlugins, extraKnownMarketplaces, tui, autoMode), `~/.claude/plugins/{installed_plugins.json,known_marketplaces.json,cache}`, `~/.claude/skills/` (34), `~/.claude/agents/`, `~/.claude/projects/<p>/memory/MEMORY.md`, `~/.claude/history.jsonl`, project `.claude/settings.json`, `.mcp.json`, `CLAUDE.md`, `AGENTS.md`, plugin `hooks/hooks.json` with SessionStart, UserPromptSubmit, PostToolUse, Stop | managed dirs, `~/.claude/CLAUDE.md`, `~/.claude/rules/`, `CLAUDE.local.md` |
| Codex | `~/.codex/config.toml` (features, agents, memories, tui, history, projects, plugins, marketplaces, mcp_servers, hooks.state), seven `*.config.toml` profiles, `~/.codex/AGENTS.md`, `AGENTS.override.md`, `~/.codex/rules/default.rules` (11 prefix_rule lines), `~/.codex/plugins/cache/`, `~/.agents/skills/` (109), `~/.agents/plugins/marketplace.json`, project `.codex/hooks.json` and `.codex/config.toml`, `.agents/skills/` | `/etc/codex/*`, `~/.codex/hooks.json`, `~/.codex/prompts/`, `~/.codex/agents/` |
| Cursor | `~/.cursor/cli-config.json` (permissions, approvalMode, sandbox, model, attribution), `~/.cursor/plugins/cache/cursor-public/` (17 plugins with `.cursor-plugin/plugin.json`), plugin `hooks/hooks-cursor.json`, `~/.cursor/skills-cursor/` (25 built-ins), `~/.cursor/plans/` (38), `~/.cursor/sandbox-policies/`, `~/.cursor/chats/` | `~/.cursor/hooks.json`, `~/.cursor/mcp.json`, `~/.cursor/permissions.json`, `~/.cursor/sandbox.json`, IDE `settings.json`, MDM paths, any project `.cursor/rules` |
| OpenCode | `~/.config/opencode/opencode.jsonc` (`$schema` only), `~/.config/opencode/package.json` and `~/.opencode/package.json` (`@opencode-ai/plugin` 1.4.10), `~/.opencode/node_modules/@opencode-ai/{plugin,sdk}` types, `~/.local/share/opencode/`, `opencode debug skill` output | `~/.config/opencode/AGENTS.md`, any `.opencode/` content dir, managed paths, `~/.cache/opencode/node_modules/` |

Facts the research could not confirm from a primary source are marked `partial` or
listed in `details` as unverified in the matrix file. The notable ones:

- Codex: whether legacy `[profiles.<name>]` tables still work in 0.162.1; `max_depth` and
  `job_max_runtime_seconds` under `[agents]` exist locally but not in current docs;
  `~/.codex/skills` exists locally but is not a documented discovery path; hooks
  introduction versions were not established by primary sources and are omitted.
- Cursor: `$ARGUMENTS` in user-level commands; dashboard team commands; CLI print-mode
  exit codes; whether `argument-hint` and `allowed-tools` in skills are honoured.
- OpenCode: current permission docs confirm `doom_loop` and `external_directory` default
  to `ask`; installed-runtime behavior was not exercised. The plugin cache path and
  `scout` built-in agent remain unverified.
- Claude Code: `--max-turns` is in the CLI reference but absent from the captured 2.1.281
  `--help` output.

## Appendix D. AOK checkout observations

Reviewed against main commit `edbba46a8514aa18135221e2feaebc18e579047b`.
These observations do not change adapters or local installations.

- `adapters/cursor/adapter.json` and `docs/targets-support-matrix.md` describe generated
  bundles with native `.cursor/rules` and `.cursor/skills`. Executable hooks and a
  `.cursor-plugin` marketplace package are not emitted; hooks remain checklist guidance.
- OpenCode support is a generated contract JSON bundle and Markdown guidance, rather
  than a native installer (`docs/targets-support-matrix.md`).
- `core/model.md` still describes Cursor skills as rules and OpenCode skills as commands;
  that conceptual wording does not fully describe current Cursor bundle output.
- This research does not establish native runtime execution or installation health for
  any AOK adapter. No setup or install command was run during review.
