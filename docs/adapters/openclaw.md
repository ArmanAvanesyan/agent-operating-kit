# OpenClaw import guide

This guide covers generated **pack bundles**. The separate Go CLI native-agent
adapter and its bounded loading evidence are documented in the
[native operator guide](../native-adapter-operations.md) and
[completion audit](../native-adapter-completion-audit.md). Pack bundle loading,
native agent discovery and runtime enforcement have separate acceptance gates.

Status: active · Support level: `generated-bundle` · Host Min/Tested: see
[targets-support-matrix.md](../targets-support-matrix.md).

Adapter README: [`adapters/openclaw/README.md`](../../adapters/openclaw/README.md).

## Prerequisites

- OpenClaw host/runtime that accepts agent definitions and task assignments
- AOK kit checkout **or** release `*-bundles.zip`

## Obtain the bundle

```bash
./scripts/aok render bundle engineering-guardrails --target openclaw
# → build/bundles/openclaw/engineering-guardrails/
```

Release zip path: `bundles/openclaw/<pack>/`.

## Exact paths in the bundle

| Path | Use when importing |
|---|---|
| `aok-bundle.json` | Canonical contract / audit |
| `import.json` | Import pointer |
| `roles.md`, `roles/*.json` | Agent definitions / role contracts |
| `skills.md`, `skills/*.json` | Workflow guidance |
| `hooks.md`, `hooks/*.json` | Lifecycle checklists |
| `tasks.md`, `task-packets/` | Assignment contracts + verification fields |
| `README.md` | Pack summary |

## Host import steps

1. Generate or extract `bundles/openclaw/<pack>/`.
2. Import roles as OpenClaw agent definitions (map JSON fields to the host’s
   agent schema where names differ).
3. Import skills as reusable workflow docs or tool prompts the agent can call.
4. Import hooks as lifecycle checklists around OpenClaw runs (pre/after). Native
   OpenClaw tool-call wiring is **not** emitted by AOK.
5. Import task packets as assignment specs; keep verification fields when the
   host supports structured acceptance checks.
6. Optional: store the bundle under `.aok/imports/openclaw/<pack>/`.

## Verification

- OpenClaw shows the imported role/agent name.
- A sample task packet title/body is visible as an assignment.
- `aok-bundle.json` `target` is `"openclaw"`.

## Failure modes

| Symptom | Likely cause |
|---|---|
| Agent definition rejected | Field mapping mismatch — adapt JSON to host schema |
| Tool calls missing | Expected — native installer / tool-call wiring is follow-up |
| Empty lifecycle | Pack has no hooks; check another pack (e.g. engineering-guardrails) |

## Known gaps

- Native OpenClaw installer and tool-call wiring are pending.
- Pack/install host smoke remains Untracked in the pack matrix; the separate agent adapter has versioned native-loading evidence.
