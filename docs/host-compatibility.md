# Host Compatibility

AOK has **no locked host-tool runtime dependencies**. Compatibility risk is
adapter format drift and untested host versions, not a kit lockfile.

## Current policy

| Surface | Version tracking |
|---|---|
| Codex Desktop / App | Untracked |
| Claude Code | Untracked (marketplace plugin versions live in pack/`plugin.json` assets) |
| Cursor | Untracked |
| OpenCode | Untracked |
| Gemini CLI / app | Untracked |
| OpenClaw | Untracked |
| Hermes Agent | Untracked |
| swarmd (MCP) | Untracked — see [swarmd integration](swarmd-integration.md) |

Kit version (see `.codex-plugin/plugin.json`, currently `0.6.0`) is independent
of host versions. Pack and Claude marketplace plugin versions may also differ
from the kit; that skew is intentional unless a release notes otherwise.

## Per-target summary

| Target | Support level | What you get today | Main gap |
|---|---|---|---|
| Codex | `native-install` | Multi-platform installers + Codex plugin bundle | Keep docs/manifests multi-platform |
| Claude Code | `compatibility-asset` | Marketplace + preserved pack plugins | Host-specific hook roots |
| Cursor | `generated-bundle` | Rules/skills/hooks/tasks as bundle files | Native Cursor package follow-up |
| OpenCode / Gemini / OpenClaw / Hermes | `generated-bundle` | Importable `aok-bundle.json` + markdown/JSON items | Native install / host wiring |

Full columns (Min / Tested, import paths, notes):
[Targets support matrix](targets-support-matrix.md).
Adapter detail: `adapters/<target>/README.md`.

## Recording a tested host version

When you verify AOK against a specific host build:

1. Fill the **test log template** below (keep a copy in the PR body or as a
   short footnote under the matrix).
2. Update that target’s **Tested** cell in
   [targets-support-matrix.md](targets-support-matrix.md) (or the swarmd
   section in [swarmd-integration.md](swarmd-integration.md)) from `Untracked`
   to `tested YYYY-MM-DD: <version>`.
3. Only set **Min** when you intentionally declare a floor; otherwise leave
   Min as `Untracked`.
4. Keep older notes in CHANGELOG or a “Previously tested” bullet if useful.
5. Do **not** invent minimum or tested versions without a real test run.

### Test log template

```markdown
### Host compatibility smoke

| Field | Value |
|---|---|
| Product | Codex Desktop / Claude Code / Cursor / … |
| Version or build id | |
| Date (UTC) | YYYY-MM-DD |
| Kit version | (from `.codex-plugin/plugin.json`) |
| Surfaces tested | install / render import / MCP call / … |
| Pack(s) | e.g. engineering-guardrails |
| Result | pass / fail + brief note |
| Tester | |
```

Example footnote style:

```markdown
Tested: Cursor 1.x.y (2026-10-14) — imported `build/bundles/cursor/engineering-guardrails/` rules and hooks markdown into a sample workspace.
```

## Optional release smoke

Before or after tagging a kit release, optionally smoke the three primary
hosts (Codex, Claude Code, Cursor) and fill the first real **Tested** rows.
See the optional step in [release-checklist.md](release-checklist.md).
