# Codex Adapter

Status: active.

The Codex adapter renders neutral pack content into structured JSON contracts and
supports bootstrap helpers through existing `scripts/aok` workflows.

## Render Shape

- Codex support is a native AOK plugin bundle rooted at `.codex-plugin/plugin.json`.
- Bundle metadata is expected to come from the Codex plugin manifest fields
  `name`, `version`, `description`, `skills`, `hooks`, and `interface`.
- Skills and hooks render through normalized contract payloads.
- Task packets include `task.create`, `memory.put`, and `event.append` projection
  references for `swarmd` integration.
- MCP config contracts can be rendered for local agent runtimes.

## Notes

- Native plugin metadata is preserved when running
  `aok render bundle <pack> --target codex`.
- Runtime automation is generated for portability and then adapted to Codex usage by
  project-level workflow owners.
