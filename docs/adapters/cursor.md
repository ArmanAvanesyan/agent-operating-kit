# Cursor import guide

Status: active · Support level: `generated-bundle` · Host Min/Tested: see
[targets-support-matrix.md](../targets-support-matrix.md).

Adapter README: [`adapters/cursor/README.md`](../../adapters/cursor/README.md).

## Format spike (locked for v1)

Against current Cursor docs
([Rules](https://cursor.com/docs/context/rules),
[Skills](https://cursor.com/docs/context/skills),
[Hooks](https://cursor.com/docs/agent/hooks)):

| Surface | Cursor path | AOK mapping |
|---|---|---|
| Project rules | `.cursor/rules/*.mdc` (YAML frontmatter + body) | Pack **roles** → one `.mdc` per role (`alwaysApply: false`, description from role name) |
| Skills | `.cursor/skills/<name>/SKILL.md` | Pack **skills** → skill folder; body from `sourcePath` when present (`name` forced to folder slug) |
| Hooks | `.cursor/hooks.json` (executable) | **Not emitted** — pack hooks become `.cursor/hooks-checklist.md` (non-executable). Claude `CLAUDE_PLUGIN_ROOT` commands are not Cursor hooks. |
| Plugin / marketplace | `.cursor-plugin/` + marketplace | **Deferred** — no stable in-repo marketplace publish path claimed |

The generic import bundle (`aok-bundle.json`, markdown/JSON item dirs) remains
forever as audit/fallback. Native tree emission is **additive**.

## Prerequisites

- Cursor IDE (or Agent) that loads project rules from `.cursor/rules` and skills
  from `.cursor/skills`
- AOK kit checkout **or** a release `agent-operating-kit-<version>-bundles.zip`
- Optional: Go/`aok` CLI or Python `./scripts/aok` to regenerate bundles

## Obtain the bundle

```bash
./scripts/aok render bundle engineering-guardrails --target cursor
# → build/bundles/cursor/engineering-guardrails/
```

Release zip path: `bundles/cursor/<pack>/`.

## Exact paths in the bundle

| Path | Use when importing |
|---|---|
| `.cursor/rules/*.mdc` | **Copy into project** `.cursor/rules/` (native) |
| `.cursor/skills/*/SKILL.md` | **Copy into project** `.cursor/skills/` (native; when pack has skills) |
| `.cursor/hooks-checklist.md` | Operator checklist only — not `hooks.json` |
| `aok-bundle.json` | Keep in-repo as audit/contract |
| `import.json` | Lightweight pointer |
| `roles.md`, `hooks.md`, `skills.md`, `tasks.md` | Human-readable projections / fallback |
| `roles/`, `hooks/`, `skills/`, `task-packets/` | Per-item JSON |

## Host import steps

1. Generate or extract `bundles/cursor/<pack>/`.
2. Copy the emitted `.cursor/rules/` tree into the project root’s `.cursor/rules/`
   (merge carefully if rules already exist).
3. Copy `.cursor/skills/` the same way when present.
4. Optionally keep `.cursor/hooks-checklist.md` or fold it into PR/process docs.
   Do **not** expect AOK to write executable `.cursor/hooks.json`.
5. Keep `aok-bundle.json` under e.g. `.aok/imports/cursor/<pack>/` for audits.

## Verification

- Cursor Customize → Rules shows the imported `.mdc` files.
- For skill packs (e.g. `ship-pipeline`), `/ship-pr` (or the skill name) appears
  under Skills / Agent chat `/` menu.
- `aok-bundle.json` `target` is `"cursor"` and `files` lists `.cursor/...` paths.

## Failure modes

| Symptom | Likely cause |
|---|---|
| Rules ignored | Wrong extension (must be `.mdc`) or not under project `.cursor/rules/` |
| Skill name mismatch | Folder name must match `SKILL.md` frontmatter `name` — AOK rewrites this |
| “Hooks don’t run” | Expected — checklist only; wire `.cursor/hooks.json` yourself if needed |
| Missing `.cursor/` | Old renderer; upgrade kit and re-run `aok render bundle` |

## Known gaps

- No Cursor marketplace / `.cursor-plugin` installer from AOK.
- Host Cursor version remains Untracked until recorded in the matrix.
