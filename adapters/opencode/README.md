# OpenCode Adapter

Status: active.  
Support level: `generated-bundle`.  
Host version: untracked (see `docs/host-compatibility.md`).

The OpenCode adapter renders AOK roles, skills, hooks, and task packets into a
**generated import bundle**. AOK does not ship a native OpenCode installer.

## Generated-bundle import expectations

```bash
./scripts/aok render bundle <pack> --target opencode
# → build/bundles/opencode/<pack>/
```

Import the directory (or the matching tree from release `*-bundles.zip`) into
the project workspace:

- Use `aok-bundle.json` as the parseable contract.
- Use `roles.md` / `hooks.md` / `skills.md` / `tasks.md` (and per-item JSON) as
  project guidance.
- Follow `import.json` + pack `targetRender.opencode.importInstructions` for
  the intended file set.

There is no automatic OpenCode package registration from AOK.

## Render Shape

- Skills and roles render as structured JSON contracts.
- Task packets include reusable execution scope and verification fields.
- MCP config may appear on the pack render contract for local agent runtimes.
- Bundle metadata is emitted under `targetRender.opencode` / `aok-bundle.json`.

## Supported Commands

- `aok render pack <pack> --target opencode`
- `aok render bundle <pack> --target opencode`

## Known Gaps

- OpenCode native package installer wiring is follow-up work.
- Host OpenCode version is untracked until someone records a tested build in
  `docs/targets-support-matrix.md`.
