# OpenCode Adapter

Status: active.

The OpenCode adapter renders AOK roles, skills, hooks, and task packets into
OpenCode-compatible project guidance.

## Render Shape

- Skills and roles render as structured JSON contracts.
- Task packets include reusable execution scope and verification fields.
- MCP config is emitted as a project-level integration payload.
- Bundle metadata is emitted as a generated import bundle in
  `targetRender.opencode.payload`; AOK does not claim native OpenCode installer
  packaging.

## Supported Commands

- `aok render bundle <pack> --target opencode`

## Known Gaps

- OpenCode native package installer wiring is tracked as follow-up work.
