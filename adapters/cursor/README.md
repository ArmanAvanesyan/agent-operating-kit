# Cursor Adapter

Status: active.  
Support level: `generated-bundle` (with additive native project tree).  
Host version: untracked (see `docs/host-compatibility.md`).

**Ops guide (SoT):** [`docs/adapters/cursor.md`](../../docs/adapters/cursor.md).

`aok render bundle --target cursor` emits:

1. The **generic import bundle** (`aok-bundle.json`, markdown/JSON) — audit/fallback
2. A **project-local native tree** under `.cursor/` (rules + skills + hooks checklist)

## Format lock (v1)

| Emit | Source |
|---|---|
| `.cursor/rules/<role>.mdc` | Pack roles |
| `.cursor/skills/<skill>/SKILL.md` | Pack skills (`sourcePath` when set) |
| `.cursor/hooks-checklist.md` | Pack hooks as **non-executable** guidance |
| *(not emitted)* `.cursor/hooks.json` | Executable Cursor hooks — host-owned |
| *(not emitted)* `.cursor-plugin/` | Marketplace package — deferred |

See the format spike notes in [`docs/adapters/cursor.md`](../../docs/adapters/cursor.md).

## Prerequisites

- Cursor with project rules / skills support
- Kit checkout or release `*-bundles.zip`

## Generate or download

```bash
./scripts/aok render bundle <pack> --target cursor
# → build/bundles/cursor/<pack>/
```

## Import steps (summary)

1. Obtain `bundles/cursor/<pack>/`.
2. Copy `.cursor/rules/` and `.cursor/skills/` into the project root.
3. Use `.cursor/hooks-checklist.md` as a checklist (not auto-wired hooks).
4. Keep `aok-bundle.json` for audits.

Full steps, verification, and failure modes:
[`docs/adapters/cursor.md`](../../docs/adapters/cursor.md).

## Supported Commands

- `aok render pack <pack> --target cursor`
- `aok render bundle <pack> --target cursor`
- `aok render bundles --target cursor` (or `--target all`)

## Known Gaps

- Executable Cursor `hooks.json` and marketplace `.cursor-plugin` packaging are
  not claimed.
- Host Cursor version is untracked until recorded in
  `docs/targets-support-matrix.md`.
