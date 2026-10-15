# Project Init Guide

`aok project init` installs operating files into a project so agents have a
shared workflow, role definitions, task template, hooks guide, Codex config, and
Codex App local-environment helpers.

## Initialize A Project

From the AOK checkout:

```bash
./scripts/aok project init /path/to/project
```

Kanban is selected by default. Optional active practice overlays can be
composed during initialization:

```bash
./scripts/aok project init /path/to/project \
  --flow kanban --overlay xp --overlay lean
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
- `.codex/setup.sh`
- `.codex/aok-action.sh`
- `.codex/aok-local-environment.json`
- `.codex/environments/environment.toml`

## Codex Local Environment Only

To add or repair only the Codex local-environment files:

```bash
./scripts/aok codex local-env init /path/to/project
./scripts/aok codex local-env validate /path/to/project
./scripts/aok codex local-env actions /path/to/project
```

Codex can read `.codex/environments/environment.toml` for the setup script and
actions. The AOK manifest is intentionally AOK-owned metadata for validation and
documentation of the same action surface.

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

Remote boards are a separate follow-up. Run `./scripts/aok work-system setup github` or
`./scripts/aok work-system setup azure-devops` after local initialization. A verified
binding written to `.aok/project.json` is preserved by later project init runs.
See `work-system-bootstrap.md`.

Methodology can be changed later with `./scripts/aok methodology set`. Existing explicit
methodology and work-system bindings are preserved when init is run without
methodology flags. See `methodology-profiles.md`.

Scaffolded methodology flows remain separate from project initialization. To
inspect FDD, DSDM, or AUP roles and artifact templates without selecting the
flow, run:

```bash
./scripts/aok methodology scaffold /path/to/project --flow fdd
```

This local export leaves `.aok/project.json` and remote work systems unchanged.

Work-item templates are a separate explicit export:

```bash
./scripts/aok work-item scaffold /path/to/project
./scripts/aok work-item validate /path/to/project
```

This writes neutral task templates, GitHub Issue Forms, and Azure DevOps
desired-state guidance based on the selected methodology. It does not change
the project methodology or any remote work system. See `work-item-types.md`.

## Rollback

For generated files that did not previously exist, remove the files listed in
the init output.

For replaced files, restore from `.aok-backups/<timestamp>/`.

For a preserved `AGENTS.md`, remove `AGENTS.aok-proposed.md` and
`AGENTS.aok.diff` if you decide not to adopt the proposed template.
