---
name: init-phased-project
description: Scaffold a new project with the standard phased-rollout structure (CLAUDE.md, infra/ACCOUNTS+DEPLOY, .github/workflows/ci, pyproject.toml, Dockerfile). Args: --name <project> [--description "<desc>"]. Run from inside an empty or near-empty directory.
---

# /init-phased-project

You are initializing a new project from the templates in this plugin.

## Args

- `--name <project>` (required) — the project name. Used to substitute `{{PROJECT_NAME}}`.
- `--description "<desc>"` (optional) — one-line description. Used to substitute `{{PROJECT_DESCRIPTION}}`. Defaults to `"A new project scaffolded with phased-rollout-template."`.
- `--stack "<stack>"` (optional, informational only) — e.g., `"python,fastapi,postgres"`. Default: `"python,fastapi,postgres"`. The templates are tuned for this stack today; if the user passes anything else, warn that the templates may need manual editing.

## Steps

1. **Verify cwd is empty or near-empty.** List the directory. Only allow these entries: `.git/`, `.gitignore`, `README.md`. If anything else is present, **stop and ask the user** whether to proceed (and let them remove offending files first). Do not silently overwrite.

2. **Resolve templates path.** Templates live at `${CLAUDE_PLUGIN_ROOT}/templates/`. Use that env var verbatim — do **not** hard-code an absolute path. The directory layout you'll be copying:

   ```
   templates/
   ├── CLAUDE.md.tmpl
   ├── pyproject.toml.tmpl
   ├── Dockerfile.tmpl
   ├── .dockerignore.tmpl
   ├── infra/
   │   ├── README.md.tmpl
   │   ├── ACCOUNTS.md.tmpl
   │   ├── DEPLOY.md.tmpl
   │   └── cloud_run.yaml.tmpl
   ├── docs/engineering/
   │   ├── process.md.tmpl
   │   ├── code-review.md.tmpl
   │   └── security.md.tmpl
   └── .github/workflows/
       ├── ci.yml.tmpl
       └── deploy.yml.tmpl
   ```

3. **Substitute + write.** For each `*.tmpl` file under templates/ (recursively):
   - Read the file
   - Replace `{{PROJECT_NAME}}` with the `--name` arg (literal string substitution; do not URL-encode or quote)
   - Replace `{{PROJECT_DESCRIPTION}}` with the `--description` arg (or its default)
   - Replace `{{CURRENT_YEAR}}` with today's year (4-digit, e.g., `2026`) — currently unused by any template, but supported for future templates that need a copyright/year line
   - Write to the corresponding path in cwd, dropping the `.tmpl` extension
   - Create parent directories as needed (e.g., `infra/`, `docs/engineering/`, `.github/workflows/`)
   - Preserve the file's content byte-for-byte except for the substitutions

4. **Confirm structure.** Run `ls -la` (or equivalent) on cwd and on each created subdirectory so the user can verify the scaffolding succeeded.

5. **Print next steps.** Tell the user:
   a. `git init && git add . && git commit -m "feat: initial project scaffolding"`
   b. Create a GitHub repo: `gh repo create --source=. --push` (or via the GitHub UI)
   c. Read `CLAUDE.md` and the three engineering docs (`docs/engineering/process.md`, `code-review.md`, `security.md`) before starting work
   d. Read `infra/ACCOUNTS.md` to know what external services you need to register with **before** deploying
   e. Follow `infra/DEPLOY.md` for the first production deploy (do not skip §1 pre-flight checks)
   f. Replace the placeholder Notion / ADR URLs in CLAUDE.md with the actual ones for this project

## Refuses / safety

- **Never overwrite an existing file in cwd without explicit confirmation.** If a target path already exists, stop and ask the user whether to overwrite, skip, or abort the whole scaffold.
- **Never write outside cwd.** All output paths are relative to the directory the skill was invoked from.
- **Do not commit on the user's behalf.** Print the suggested `git init && git add && git commit` command — do not run it. The user should review the scaffold first.
- **Do not push to a remote.** Same reason — the user should choose where this lives.

## On error

If a template file is missing or malformed, stop immediately and surface the path. Do not partially scaffold — that leaves the project in a confusing half-state. If a partial scaffold has already happened, list the files written so the user can clean up.
