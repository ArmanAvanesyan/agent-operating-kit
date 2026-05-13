---
name: project-bootstrap
description: Use when installing the Agent Operating Kit, initializing a repository or multi-repo workspace with AGENTS.md, agents templates, hooks, roles, task packets, or validating a project setup.
---

# Project Bootstrap

Use the CLI for deterministic setup:

```bash
aok install
aok doctor
aok project init
aok project validate
```

## Initialization Rules

- Detect single-repo versus multi-repo workspace from local git layout.
- Add generic operating instructions only; keep project-specific facts in the target project.
- Never overwrite existing instructions silently. Create a backup, proposed file, or diff.
- Keep secrets out of templates and generated files.
- Prefer Markdown task packets and role cards as the stable interface.

## Generated Project Files

- `AGENTS.md`
- `agents/workflow.md`
- `agents/github-project.md`
- `agents/hooks.md`
- `agents/hooks/pre/*.md`
- `agents/hooks/after/*.md`
- `agents/roles/*.md`
- `agents/tasks/task-template.md`
- `.codex/config.toml`

Read `references/migration-from-existing-project.md` when adapting an existing project.
