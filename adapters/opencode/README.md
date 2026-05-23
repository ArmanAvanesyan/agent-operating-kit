# OpenCode Adapter

Status: active.

The OpenCode adapter renders AOK roles, skills, hooks, and task packets into
OpenCode-compatible project guidance.

## Render Shape

- Skills and roles render as structured JSON contracts.
- Task packets include reusable execution scope and verification fields.
- MCP config is emitted as a project-level integration payload.

## Supported Commands

- `aok render pack <pack> --target opencode`

## Known Gaps

- OpenCode native package installer wiring is tracked as follow-up work.
