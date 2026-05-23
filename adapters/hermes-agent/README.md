# Hermes Agent Adapter

Status: active.

The Hermes Agent adapter renders AOK roles, task packets, hooks, and skills into
Hermes-oriented contracts.

## Render Shape

- Task packets render as structured assignment specs.
- Roles and hooks render with escalation and execution guidance.
- MCP config references support external runtime hooks.
- Bundle metadata is emitted as a generated import bundle in
  `targetRender.hermes-agent.payload`; runtime event wiring remains external.

## Supported Commands

- `aok render bundle <pack> --target hermes-agent`

## Known Gaps

- Runtime-specific event wiring is documented and manually connected by host flows.
