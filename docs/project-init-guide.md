# Project Init Guide

`aok project init` installs operating files into a project so agents have a
shared workflow, role definitions, task template, hooks guide, and Codex config.

## Initialize A Project

From the AOK checkout:

```bash
./scripts/aok project init /path/to/project
```

Then validate:

```bash
./scripts/aok project validate /path/to/project
```

Validation passes when these files exist:

- `AGENTS.md`
- `agents/workflow.md`
- `agents/github-project.md`
- `agents/hooks.md`
- `agents/tasks/task-template.md`
- `agents/roles/orchestrator.md`
- `agents/roles/implementer.md`
- `agents/roles/qa-reality-checker.md`
- `agents/roles/code-reviewer.md`
- `.codex/config.toml`

## Existing `AGENTS.md`

AOK is conservative with an existing `AGENTS.md`.

By default, it preserves the current file and writes:

- `AGENTS.aok-proposed.md`
- `AGENTS.aok.diff`

Review the diff, then merge the parts you want.

Use `--force` only when you intentionally want the AOK template to replace the
existing `AGENTS.md`:

```bash
./scripts/aok project init /path/to/project --force
```

When a file is replaced, AOK writes a backup under `.aok-backups/<timestamp>/`.

## What Init Records

Every init writes `.aok/project.json` with:

- initialization time
- detected workspace kind
- AOK kit version

Workspace kind is detected as:

- `single-repo` when the target itself has `.git`
- `multi-repo` when child directories have `.git`
- `plain-directory` otherwise

## Recommended First Project Flow

1. Run `./scripts/aok project init /path/to/project`.
2. Read the command output and note created, unchanged, proposed, or backed up
   files.
3. If `AGENTS.aok.diff` exists, merge the proposed instructions manually.
4. Run `./scripts/aok project validate /path/to/project`.
5. Commit the project-side operating files in that project, not in the AOK
   checkout.

## Rollback

For generated files that did not previously exist, remove the files listed in
the init output.

For replaced files, restore from `.aok-backups/<timestamp>/`.

For a preserved `AGENTS.md`, remove `AGENTS.aok-proposed.md` and
`AGENTS.aok.diff` if you decide not to adopt the proposed template.
