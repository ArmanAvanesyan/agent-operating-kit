# Cursor Adapter

Status: active.  
Support level: `generated-bundle`.  
Host version: untracked (see `docs/host-compatibility.md`).

The Cursor adapter renders AOK pack content into a **self-contained import
bundle**. AOK does not install a native Cursor package today.

## What `aok render bundle` produces

```bash
./scripts/aok render bundle <pack> --target cursor
# → build/bundles/cursor/<pack>/
```

Typical bundle layout (same shape for every generated-bundle target):

| Path | Role |
|---|---|
| `aok-bundle.json` | Canonical contract (`kind: aok-bundle`) plus embedded pack render |
| `import.json` | Lightweight import pointer (`kind: aok-target-import`) |
| `README.md` | Pack/target/support-level summary |
| `roles.md`, `hooks.md`, `skills.md`, `tasks.md` | Human-readable projections |
| `roles/`, `hooks/`, `skills/`, `templates/`, `task-packets/` | Per-item JSON |
| `sources/` | Copied pack/source refs when declared |

Pack manifests may also describe Cursor-oriented `targetRender.cursor.payload`
(for example hook-checklist contracts). That metadata is carried in the render
contract inside `aok-bundle.json`; it is not a separate Cursor marketplace
package.

## Import story (rules / skills / hooks)

Until a native Cursor package exists, treat the bundle as the import source:

1. Generate or download the Cursor pack directory (`bundles/cursor/<pack>/` in
   a release `*-bundles.zip`, or regenerate with `aok render bundle`).
2. Copy or map **rules / policy** from `roles.md` and `roles/*.json` into the
   project’s Cursor rules location (project rules / AGENTS-style guidance as
   your Cursor version expects).
3. Use `skills.md` / `skills/*.json` as skill or workflow prompts; wire them
   manually into Cursor skills or docs the host understands.
4. Use `hooks.md` / `hooks/*.json` as **checklists** (pre/after guidance). Native
   Cursor hook execution is not emitted by this adapter.
5. Keep `aok-bundle.json` in-repo if you want a parseable audit trail of what
   was imported.

## Supported Commands

- `aok render pack <pack> --target cursor`
- `aok render bundle <pack> --target cursor`
- `aok render bundles --target cursor` (or `--target all`)

## Known Gaps

- **Native Cursor package** (first-class Cursor rules/skills/hooks packaging) is
  explicit follow-up work. JSON contract + generated markdown remain the
  cross-tool source of truth.
- Host Cursor version is untracked; record a tested version in
  `docs/targets-support-matrix.md` when verified.
