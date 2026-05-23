# Codex Local Environments

AOK ships Codex App local-environment helpers for projects initialized with
`aok project init`. They provide one setup script for new worktrees and a small
set of repeatable actions that can be registered from Codex App settings.

The official Codex docs describe local environments as project-level setup
scripts and common actions stored under the repository's `.codex` directory:
https://developers.openai.com/codex/app/local-environments.

## Files

`aok project init` and `aok codex local-env init` install:

- `.codex/setup.sh`
- `.codex/aok-action.sh`
- `.codex/aok-local-environment.json`
- `.codex/environments/environment.toml`
- `.codex/config.toml`

The TOML file is the Codex local-environment config AOK ships for worktree setup
and actions. The public docs confirm the `.codex` storage model but do not
publish the full schema on the page; this TOML shape is based on a locally
observed Codex plugin example and stays conservative.

The JSON file is an AOK manifest. It records the same setup script and action
commands so AOK can validate and document the expected local-environment
surface.

## Setup Script

Use this script in Codex App settings as the default setup script:

```bash
.codex/setup.sh
```

It resolves the AOK CLI by preferring a repo-local `./scripts/aok`, then the
installed `aok` command on `PATH`. It writes a setup report to:

```text
.codex/aok-setup-report.md
```

The setup script does not require secrets and does not force network access.

## Actions

Codex can read `.codex/environments/environment.toml` for these actions. If you
manage local environments through Codex App settings, keep the same action
commands:

| Action | Script |
|---|---|
| AOK Doctor | `.codex/aok-action.sh doctor` |
| AOK Validate | `.codex/aok-action.sh validate` |
| AOK Render Bundles | `.codex/aok-action.sh render-bundles` |
| AOK Project Init | `.codex/aok-action.sh project-init` |
| AOK Claude Setup | `.codex/aok-action.sh claude-setup` |
| AOK Release Build | `.codex/aok-action.sh release-build` |

`AOK Release Build` is intended for the AOK checkout itself. In downstream
projects, use the other actions for project validation, setup, rendering, and
Claude Code migration.

`AOK Render Bundles` writes generated bundles to `.codex/aok-bundles` in the
project where the action runs.

## CLI

Initialize or repair local-environment files:

```bash
aok codex local-env init /path/to/project
```

Validate a project:

```bash
aok codex local-env validate /path/to/project --json
```

Print the action list:

```bash
aok codex local-env actions /path/to/project
```
