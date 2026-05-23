# Gemini Adapter

Status: scaffolded.

The Gemini adapter defines how AOK content should be presented to Gemini-based
agent workflows.

## Render Shape

- Task packets render as Markdown specs.
- Roles render as system or project instructions.
- Skills render as reusable command guidance.
- MCP config can be generated for runtimes such as `swarmd` where supported.

## Known Gaps

- Native Gemini packaging is not implemented.
- Tool invocation details must be supplied by the host environment.
