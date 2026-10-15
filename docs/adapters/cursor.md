# Cursor import guide

Status: active · Support level: `generated-bundle` · Host Min/Tested: see
[targets-support-matrix.md](../targets-support-matrix.md).

This guide covers **today’s generated-bundle** path. Native Cursor project-tree
emission (`.cursor/rules`, skills) is tracked separately on the roadmap.

Adapter README: [`adapters/cursor/README.md`](../../adapters/cursor/README.md).

## Prerequisites

- Cursor IDE (or Agent) that can load project rules / skills from the workspace
- AOK kit checkout **or** a release `agent-operating-kit-<version>-bundles.zip`
- Optional: Go/`aok` CLI or Python `./scripts/aok` to regenerate bundles

## Obtain the bundle

From a kit checkout:

```bash
./scripts/aok render bundle engineering-guardrails --target cursor
# → build/bundles/cursor/engineering-guardrails/
```

From a release zip, extract:

```text
bundles/cursor/<pack>/
```

## Exact paths in the bundle

| Path | Use when importing |
|---|---|
| `aok-bundle.json` | Keep in-repo as audit/contract (`kind: aok-bundle`) |
| `import.json` | Lightweight pointer (`kind: aok-target-import`) |
| `roles.md`, `roles/*.json` | Map into Cursor **project rules** / policy guidance |
| `skills.md`, `skills/*.json` | Map into Cursor **skills** or reusable prompts |
| `hooks.md`, `hooks/*.json` | **Checklists only** — not executable Cursor hooks |
| `tasks.md`, `task-packets/` | Task packet specs for agents/humans |
| `README.md` | Pack/target summary |

## Host import steps

1. Open (or create) the target project in Cursor.
2. Copy rule content from `roles.md` / `roles/*.json` into the project’s Cursor
   rules location for your Cursor build (commonly `.cursor/rules/` `.mdc` files,
   or project rules UI). Prefer one rule file per AOK role when practical.
3. Copy skill guidance from `skills.md` / `skills/*.json` into Cursor skills
   (or linked docs the agent can read). AOK does not emit Cursor skill package
   folders on this path.
4. Treat `hooks.md` as a pre/after checklist in chat or PR templates. Do **not**
   expect native Cursor hook execution from this bundle.
5. Optionally commit `aok-bundle.json` under something like
   `.aok/imports/cursor/<pack>/aok-bundle.json` for drift audits.

## Verification

- Ask the agent to summarize a named role from the imported rules (e.g. a
  guardrail role from `engineering-guardrails`).
- Confirm the rule text appears in Cursor’s project rules UI or on disk under
  `.cursor/rules/`.
- Confirm `aok-bundle.json` `target` is `"cursor"` and `pack` matches.

## Failure modes

| Symptom | Likely cause |
|---|---|
| Agent ignores imported policy | Rules not on Cursor’s active rules path for this project |
| “Hooks don’t run” | Expected — checklist only on generated-bundle path |
| Missing `hooks.md` / empty roles | Wrong pack or render failed; re-run `aok render bundle` |
| Stale guidance after pack bump | Re-render bundle and re-copy changed markdown/JSON |

## Known gaps

- No native Cursor marketplace / `.cursor-plugin` install from AOK yet.
- Host Cursor version remains Untracked until recorded in the matrix.
