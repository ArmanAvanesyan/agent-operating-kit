# Cursor Adapter

Status: active.

The Cursor adapter renders AOK content into Cursor-friendly task guidance and
project rule artifacts.

## Render Shape

- Roles render as task policy markdown recommendations.
- Task packets render as structured Markdown contracts and JSON companion payloads.
- Hooks render as actionable checklists when native Cursor hook execution is not
  directly available.

## Supported Commands

- `aok render pack <pack> --target cursor`

## Known Gaps

- Native Cursor package generator is in follow-up work; JSON contract and generated
  markdown are the cross-tool source of truth.
