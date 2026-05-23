# Codex Adapter

Status: active.

The Codex adapter renders neutral pack content into structured JSON contracts and
supports bootstrap helpers through existing `scripts/aok` workflows.

## Render Shape

- Skills and hooks render through normalized contract payloads.
- Task packets include `task.create`, `memory.put`, and `event.append` projection
  references for `swarmd` integration.
- MCP config contracts can be rendered for local agent runtimes.

## Notes

- Native files are emitted via `aok render pack <pack> --target codex`.
- Runtime automation is generated for portability and then adapted to Codex usage by
  project-level workflow owners.
