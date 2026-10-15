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

Full columns (import paths, notes): [Targets support matrix](targets-support-matrix.md).
Adapter detail: `adapters/<target>/README.md`.

## Recording a tested host version

When you verify AOK against a specific host build:

1. Add a short row or footnote in [targets-support-matrix.md](targets-support-matrix.md)
   (or the swarmd section in [swarmd-integration.md](swarmd-integration.md)) with
   **product name**, **version or build id**, **date (UTC)**, and **what was
   tested** (install, render import, or MCP call).
2. Change that target’s “Host version” cell from `Untracked` to the tested value
   (keep older notes in changelog or a “Previously tested” bullet if useful).
3. Do **not** invent minimum versions without a real test run.

Example footnote style:

```markdown
Tested: Cursor 1.x.y (2026-10-14) — imported `build/bundles/cursor/engineering-guardrails/` rules and hooks markdown into a sample workspace.
```
