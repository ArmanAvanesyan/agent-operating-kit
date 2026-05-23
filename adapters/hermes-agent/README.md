# Hermes Agent Adapter

Status: scaffolded.

The Hermes Agent adapter defines how AOK roles, task packets, hooks, and skills
should render into Hermes Agent workflows.

## Render Shape

- Task packets render as structured assignment specs.
- Roles render as agent responsibilities and escalation rules.
- Hooks render as lifecycle checks.
- MCP config can point Hermes workflows at `swarmd`.

## Known Gaps

- Native Hermes Agent package generation is not implemented yet.
- Runtime-specific event wiring is documented but not automated.
