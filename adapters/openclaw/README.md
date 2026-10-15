# OpenClaw Adapter

Status: active.  
Support level: `generated-bundle`.  
Host version: untracked (see `docs/host-compatibility.md`).

**Ops guide (SoT for import steps):** [`docs/adapters/openclaw.md`](../../docs/adapters/openclaw.md).

The OpenClaw adapter renders AOK neutral content into OpenClaw-ready task and
role contracts as a **generated import bundle**.

## Prerequisites

- OpenClaw host that accepts agent definitions and assignments
- Kit checkout or release `*-bundles.zip`

## Generate or download

```bash
./scripts/aok render bundle <pack> --target openclaw
# → build/bundles/openclaw/<pack>/
```

Release zip: `bundles/openclaw/<pack>/`.

## Import steps (summary)

1. Obtain `bundles/openclaw/<pack>/`.
2. Map roles → OpenClaw agent definitions (adapt fields if schemas differ).
3. Import skills, lifecycle (`hooks.md`), and task packets as host workflows.
4. AOK does not run OpenClaw installers or tool calls.

Full steps, verification, and failure modes:
[`docs/adapters/openclaw.md`](../../docs/adapters/openclaw.md).

## Render Shape

- Roles render as agent definitions.
- Skills render as reusable workflow guidance.
- Hooks render as lifecycle checklists.
- Task packets render as assignment contracts with verification fields.
- Bundle metadata is emitted under `targetRender.openclaw` / `aok-bundle.json`.

## Supported Commands

- `aok render pack <pack> --target openclaw`
- `aok render bundle <pack> --target openclaw`

## Known Gaps

- Native OpenClaw installer behavior and tool calls are pending host integration.
- Host version untracked until recorded in `docs/targets-support-matrix.md`.
