# Codex Adapter

Status: active.

The Codex adapter renders neutral pack content into structured JSON contracts and
supports bootstrap helpers through existing `scripts/aok` workflows.

It also includes Codex App local-environment helpers for worktree setup scripts
and top-bar actions. AOK provides scripts and validation metadata under
`.codex`, plus a conservative `.codex/environments/environment.toml` based on a
locally observed Codex plugin example.

## Render Shape

- Codex support is a native AOK plugin bundle rooted at `.codex-plugin/plugin.json`.
- Bundle metadata is expected to come from the Codex plugin manifest fields
  `name`, `version`, `description`, `skills`, `hooks`, and `interface`.
- Skills and hooks render through normalized contract payloads.
- Task packets include `task.create`, `memory.put`, and `event.append` projection
  references for `swarmd` integration.
- MCP config contracts can be rendered for local agent runtimes.

## Local Environments

Initialize AOK setup and action helpers in a project:

```bash
aok codex local-env init /path/to/project
aok codex local-env validate /path/to/project
aok codex local-env actions /path/to/project
```

The installed files are `.codex/setup.sh`, `.codex/aok-action.sh`,
`.codex/aok-local-environment.json`, and
`.codex/environments/environment.toml`. See
`../../docs/codex-local-environments.md`.

## Notes

- Native plugin metadata is preserved when running
  `aok render bundle <pack> --target codex`.
- Runtime automation is generated for portability and then adapted to Codex usage by
  project-level workflow owners.
