# OpenCode import guide

This guide covers generated **pack bundles**. The separate Go CLI native-agent
adapter and its bounded loading evidence are documented in the
[native operator guide](../native-adapter-operations.md) and
[completion audit](../native-adapter-completion-audit.md). Pack bundle loading,
native agent discovery and runtime enforcement have separate acceptance gates.

Status: active · Support level: `generated-bundle` · Host Min/Tested: see
[targets-support-matrix.md](../targets-support-matrix.md).

Adapter README: [`adapters/opencode/README.md`](../../adapters/opencode/README.md).

## Prerequisites

- OpenCode (CLI or app) with a project workspace you can edit
- AOK kit checkout **or** release `*-bundles.zip`
- Ability to point OpenCode at project instruction/config files (host-specific)

## Obtain the bundle

```bash
./scripts/aok render bundle engineering-guardrails --target opencode
# → build/bundles/opencode/engineering-guardrails/
```

Release zip path: `bundles/opencode/<pack>/`.

## Exact paths in the bundle

| Path | Use when importing |
|---|---|
| `aok-bundle.json` | Canonical contract / audit trail |
| `import.json` | Import pointer |
| `roles.md`, `roles/*.json` | Agent role / policy instructions |
| `skills.md`, `skills/*.json` | Reusable workflow prompts |
| `hooks.md`, `hooks/*.json` | Lifecycle checklists (not auto-registered) |
| `tasks.md`, `task-packets/` | Structured work specs |
| `README.md` | Pack summary |

## Host import steps

1. Generate or extract `bundles/opencode/<pack>/`.
2. Copy markdown/JSON into the OpenCode project tree where the host reads
   instructions (project docs, agent config, or prompt files — OpenCode layout
   varies by version; AOK does not rewrite OpenCode config files).
3. Prefer keeping a stable import directory, e.g.
   `.aok/imports/opencode/<pack>/`, and reference those files from OpenCode
   config rather than scattering copies.
4. If the pack render includes MCP config on the contract, wire MCP separately
   in the host; AOK only emits the contract fields inside `aok-bundle.json`.
5. There is **no** automatic OpenCode package registration from AOK.

## Verification

- OpenCode agent can quote a role or skill name from the imported files.
- `aok-bundle.json` shows `"target": "opencode"` and the expected pack id.
- Re-running render and diffing `.aok/imports/opencode/<pack>/` shows only
  intentional pack changes.

## Failure modes

| Symptom | Likely cause |
|---|---|
| OpenCode never sees guidance | Files not referenced from the host’s instruction/config path |
| Expecting an installer | Not supported — generated-bundle only |
| MCP tools missing | MCP must be configured in the host from contract hints |

## Known gaps

- No native OpenCode installer or auto-registration.
- Pack/install host smoke remains Untracked in the pack matrix; the separate agent adapter has versioned native-loading evidence.
