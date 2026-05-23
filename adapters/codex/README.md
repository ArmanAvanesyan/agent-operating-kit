# Codex Adapter

Status: active.

The Codex adapter maps AOK content to Codex skills, hooks, templates, and local
CLI workflows.

## Render Shape

- Skills render to `skills/*/SKILL.md`.
- Hook contracts render to `hooks/` and `hooks.json`.
- Project bootstrap files render to `templates/`.
- The local CLI is `scripts/aok`.

## Known Gaps

- Native rendering from neutral manifests is not yet automated.
- Project-specific facts still belong in the target project's `AGENTS.md`.
