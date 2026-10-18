# Gemini import guide

This guide covers generated **pack bundles**. The separate Go CLI native-agent
adapter and its bounded loading evidence are documented in the
[native operator guide](../native-adapter-operations.md) and
[completion audit](../native-adapter-completion-audit.md). Pack bundle loading,
native agent discovery and runtime enforcement have separate acceptance gates.

Status: active · Support level: `generated-bundle` · Host Min/Tested: see
[targets-support-matrix.md](../targets-support-matrix.md).

Adapter README: [`adapters/gemini/README.md`](../../adapters/gemini/README.md).

## Prerequisites

- Gemini CLI or Gemini app workflow that can take project prompts / context files
- AOK kit checkout **or** release `*-bundles.zip`

## Obtain the bundle

```bash
./scripts/aok render bundle engineering-guardrails --target gemini
# → build/bundles/gemini/engineering-guardrails/
```

Release zip path: `bundles/gemini/<pack>/`.

## Exact paths in the bundle

| Path | Use when importing |
|---|---|
| `aok-bundle.json` | Canonical contract |
| `import.json` | Import pointer |
| `roles.md`, `roles/*.json` | Instruction-level policy |
| `skills.md`, `skills/*.json` | Reusable execution guidance |
| `hooks.md`, `hooks/*.json` | Lifecycle checklists (emitted; adapter `renders` includes `hook`) |
| `tasks.md`, `task-packets/` | Structured work specs |
| `README.md` | Pack summary |

## Host import steps

1. Generate or extract `bundles/gemini/<pack>/`.
2. Load role/skill/task markdown into Gemini as system or project context
   (paste, `@file` / context attach, or host project docs — depends on Gemini
   surface).
3. Use `hooks.md` as operator checklists around Gemini runs (preflight /
   after-action). AOK does not register Gemini runtime hooks.
4. Keep `aok-bundle.json` if you need a parseable record of the imported pack.
5. AOK does **not** invoke Gemini APIs or install a Gemini-native package.

## Verification

- A Gemini session that includes the imported role text refuses or follows the
  pack’s stated policy when prompted with a conflicting request.
- Bundle `target` is `"gemini"`; `hooks.md` is present for packs that define
  hooks.

## Failure modes

| Symptom | Likely cause |
|---|---|
| Model ignores policy | Role/skill text not in the active Gemini context window |
| Looking for `hooks` in adapter metadata but missing | Older adapter.json omitted `hook` in `renders` — fixed to match emitted `hooks.md` |
| Expecting a Gemini marketplace install | Not supported |

## Known gaps

- Runtime invocation remains host-defined.
- No native Gemini pack installer; the separate agent adapter has versioned project-discovery evidence.
