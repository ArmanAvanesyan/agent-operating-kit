# Cursor Adapter

Status: scaffolded.

The Cursor adapter defines how AOK content should render into Cursor-friendly
rules, task Markdown, and project instructions.

## Render Shape

- Roles and workflow policy render as project rules or instruction Markdown.
- Task packets render as issue-sized Markdown work contracts.
- Hooks render as manual or scriptable checklists until native hook support is
  available.

## Known Gaps

- Native Cursor packaging is not implemented yet.
- Hook execution is documented, not automatic.
