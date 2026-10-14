# OpenClaw Adapter

Status: active.  
Support level: `generated-bundle`.  
Host version: untracked (see `docs/host-compatibility.md`).

The OpenClaw adapter renders AOK neutral content into OpenClaw-ready task and
role contracts as a **generated import bundle**.

## Generated-bundle import expectations

```bash
./scripts/aok render bundle <pack> --target openclaw
# → build/bundles/openclaw/<pack>/
```

Import the included role, skill, lifecycle (hooks), and task files from the
bundle directory (or release `*-bundles.zip`) into the host workflow. Use
`aok-bundle.json` as the audit/contract file. AOK does not run OpenClaw
installers or tool calls.

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
