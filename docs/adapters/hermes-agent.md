# Hermes Agent import guide

This guide covers generated **pack bundles**. The separate Go CLI native-agent
adapter and its bounded loading evidence are documented in the
[native operator guide](../native-adapter-operations.md) and
[completion audit](../native-adapter-completion-audit.md). Pack bundle loading,
native agent discovery and runtime enforcement have separate acceptance gates.

Status: active · Support level: `generated-bundle` · Host Min/Tested: see
[targets-support-matrix.md](../targets-support-matrix.md).

Adapter README: [`adapters/hermes-agent/README.md`](../../adapters/hermes-agent/README.md).

## Prerequisites

- Hermes Agent (or compatible runtime) that can load assignment/role/skill files
- Optional MCP runtime if you want event hooks beyond checklists
- AOK kit checkout **or** release `*-bundles.zip`

## Obtain the bundle

```bash
./scripts/aok render bundle engineering-guardrails --target hermes-agent
# → build/bundles/hermes-agent/engineering-guardrails/
```

Release zip path: `bundles/hermes-agent/<pack>/`.

## Exact paths in the bundle

| Path | Use when importing |
|---|---|
| `aok-bundle.json` | Canonical contract / audit |
| `import.json` | Import pointer |
| `roles.md`, `roles/*.json` | Role + escalation guidance |
| `skills.md`, `skills/*.json` | Reusable skills |
| `hooks.md`, `hooks/*.json` | Lifecycle checklists |
| `tasks.md`, `task-packets/` | Structured assignment specs |
| `README.md` | Pack summary |

## Host import steps

1. Generate or extract `bundles/hermes-agent/<pack>/`.
2. Import assignment (task packet) files into the Hermes workflow as work items
   or templates.
3. Import roles and skills into the agent’s instruction set.
4. Use hooks markdown as lifecycle checklists. Runtime event wiring is
   **host-defined** — connect MCP or Hermes hooks manually using contract
   fields from `aok-bundle.json` when present.
5. Optional: keep `.aok/imports/hermes-agent/<pack>/aok-bundle.json` for audits.

## Verification

- Hermes can load a task packet title/spec from the imported files.
- Role escalation guidance is visible to the agent.
- Bundle `target` is `"hermes-agent"`.

## Failure modes

| Symptom | Likely cause |
|---|---|
| Events never fire | MCP/host hooks not wired — AOK does not install runtime listeners |
| Expecting a Hermes installer | Not supported — generated-bundle only |
| Stale assignments | Re-render after pack updates and refresh the import directory |

## Known gaps

- Runtime-specific event wiring remains manual.
- Host Hermes version Untracked until recorded in the matrix.
