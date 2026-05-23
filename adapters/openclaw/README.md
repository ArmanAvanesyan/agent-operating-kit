# OpenClaw Adapter

Status: active.

The OpenClaw adapter renders AOK neutral content into OpenClaw-ready task and
role contracts.

## Render Shape

- Roles render as agent definitions.
- Skills render as reusable workflow guidance.
- Hooks render as lifecycle checklists.
- Task packets render as assignment contracts with verification fields.
- Bundle metadata is emitted as a generated import bundle in
  `targetRender.openclaw.payload`; native installer behavior remains follow-up
  host integration.

## Supported Commands

- `aok render bundle <pack> --target openclaw`

## Known Gaps

- Native OpenClaw installer behavior and tool calls are pending host integration.
