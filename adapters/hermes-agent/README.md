# Hermes Agent Adapter

Status: active.  
Support level: `generated-bundle`.  
Host version: untracked (see `docs/host-compatibility.md`).

**Ops guide (SoT for import steps):** [`docs/adapters/hermes-agent.md`](../../docs/adapters/hermes-agent.md).

The Hermes Agent adapter renders AOK roles, task packets, hooks, and skills into
Hermes-oriented contracts as a **generated import bundle**.

## Prerequisites

- Hermes Agent (or compatible runtime)
- Optional MCP runtime for event wiring beyond checklists
- Kit checkout or release `*-bundles.zip`

## Generate or download

```bash
./scripts/aok render bundle <pack> --target hermes-agent
# → build/bundles/hermes-agent/<pack>/
```

Release zip: `bundles/hermes-agent/<pack>/`.

## Import steps (summary)

1. Obtain `bundles/hermes-agent/<pack>/`.
2. Import task packets as assignments; roles/skills as agent instructions.
3. Use hooks as lifecycle checklists; connect MCP/host events manually.
4. Keep `aok-bundle.json` for a parseable record.

Full steps, verification, and failure modes:
[`docs/adapters/hermes-agent.md`](../../docs/adapters/hermes-agent.md).

## Render Shape

- Task packets render as structured assignment specs.
- Roles and hooks render with escalation and execution guidance.
- MCP config references support external runtime hooks on the pack render.
- Bundle metadata is emitted under `targetRender.hermes-agent` / `aok-bundle.json`.

## Supported Commands

- `aok render pack <pack> --target hermes-agent`
- `aok render bundle <pack> --target hermes-agent`

## Known Gaps

- Runtime-specific event wiring is documented and manually connected by host flows.
- Host Hermes version untracked until recorded in `docs/targets-support-matrix.md`.
