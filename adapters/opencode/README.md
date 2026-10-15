# OpenCode Adapter

Status: active.  
Support level: `generated-bundle`.  
Host version: untracked (see `docs/host-compatibility.md`).

**Ops guide (SoT for import steps):** [`docs/adapters/opencode.md`](../../docs/adapters/opencode.md).

The OpenCode adapter renders AOK roles, skills, hooks, and task packets into a
**generated import bundle**. AOK does not ship a native OpenCode installer.

## Prerequisites

- OpenCode CLI/app with an editable project workspace
- Kit checkout or release `*-bundles.zip`

## Generate or download

```bash
./scripts/aok render bundle <pack> --target opencode
# → build/bundles/opencode/<pack>/
```

Release zip: `bundles/opencode/<pack>/`.

## Import steps (summary)

1. Obtain `bundles/opencode/<pack>/`.
2. Copy or reference markdown/JSON from a stable import directory (recommended:
   `.aok/imports/opencode/<pack>/`) into OpenCode’s instruction/config paths.
3. Wire any MCP hints from `aok-bundle.json` in the host yourself.
4. No automatic OpenCode package registration.

Full steps, verification, and failure modes:
[`docs/adapters/opencode.md`](../../docs/adapters/opencode.md).

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
- Host OpenCode version is untracked until recorded in
  `docs/targets-support-matrix.md`.
