# Codex Adapter

Status: active.  
Support level: `native-install`.  
Host version: untracked (see `docs/host-compatibility.md`).

The Codex adapter renders neutral pack content into structured JSON contracts and
supports bootstrap helpers through existing `scripts/aok` workflows.

It also includes Codex App local-environment helpers for worktree setup scripts
and top-bar actions. AOK provides scripts and validation metadata under
`.codex`, plus a conservative `.codex/environments/environment.toml` based on a
locally observed Codex plugin example.

## Multi-platform install (0.4.0+)

Codex is the native AOK install path. GitHub Releases ship **multi-platform**
installers (not macOS-only):

- `agent-operating-kit-<version>-macos-apple-silicon-installer.zip`
- `agent-operating-kit-<version>-macos-intel-installer.zip`
- `agent-operating-kit-<version>-windows-installer.zip`
- `agent-operating-kit-<version>-linux-user-installer.tar.gz`
- `agent-operating-kit-<version>-linux-system-installer.tar.gz`

Each installer registers Agent Operating Kit as a Codex local plugin. See the
root `README.md` and `docs/install-and-onboarding.md` for platform paths and
CLI shims.

Pack-oriented Codex bundles (plugin metadata + contracts) are also available
via `aok render bundle <pack> --target codex` and the release `*-bundles.zip`.

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
- Keep adapter/pack `importInstructions` aligned with multi-platform installers
  when editing manifests.
