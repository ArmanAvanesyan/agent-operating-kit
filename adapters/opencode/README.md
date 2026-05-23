# OpenCode Adapter

Status: scaffolded.

The OpenCode adapter defines a target for rendering AOK roles, skills, hooks,
and task packets into OpenCode-compatible project guidance.

## Render Shape

- Skills render as Markdown workflow commands.
- Roles render as reusable agent instructions.
- Task packets render as local work contracts.
- MCP config can reference external runtimes such as `swarmd`.

## Known Gaps

- Native OpenCode packaging details are intentionally deferred.
- Hook execution is manual until the adapter is implemented.
