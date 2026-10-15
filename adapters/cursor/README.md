# Cursor Adapter

Status: active.  
Support level: `generated-bundle`.  
Host version: untracked (see `docs/host-compatibility.md`).

**Ops guide (SoT for import steps):** [`docs/adapters/cursor.md`](../../docs/adapters/cursor.md).

The Cursor adapter renders AOK pack content into a **self-contained import
bundle**. AOK does not install a native Cursor package on this path (native
project-tree emission is separate roadmap work).

## Prerequisites

- Cursor IDE/Agent with project rules (and optional skills) support
- Kit checkout or release `*-bundles.zip`

## Generate or download

```bash
./scripts/aok render bundle <pack> --target cursor
# → build/bundles/cursor/<pack>/
```

Release zip: `bundles/cursor/<pack>/`.

## Bundle layout

| Path | Role |
|---|---|
| `aok-bundle.json` | Canonical contract (`kind: aok-bundle`) |
| `import.json` | Lightweight import pointer |
| `README.md` | Pack/target summary |
| `roles.md`, `hooks.md`, `skills.md`, `tasks.md` | Human-readable projections |
| `roles/`, `hooks/`, `skills/`, `templates/`, `task-packets/` | Per-item JSON |
| `sources/` | Copied pack/source refs when declared |

## Import steps (summary)

1. Obtain `bundles/cursor/<pack>/`.
2. Map `roles.md` / `roles/*.json` → Cursor project rules (e.g. `.cursor/rules/`).
3. Map `skills.md` / `skills/*.json` → Cursor skills or prompts manually.
4. Use `hooks.md` as **checklists** only (not executable Cursor hooks).
5. Keep `aok-bundle.json` for audits.

Full steps, verification, and failure modes:
[`docs/adapters/cursor.md`](../../docs/adapters/cursor.md).

## Supported Commands

- `aok render pack <pack> --target cursor`
- `aok render bundle <pack> --target cursor`
- `aok render bundles --target cursor` (or `--target all`)

## Known Gaps

- **Native Cursor package** (first-class rules/skills/hooks packaging) is
  explicit follow-up work.
- Host Cursor version is untracked; record a tested version in
  `docs/targets-support-matrix.md` when verified.
