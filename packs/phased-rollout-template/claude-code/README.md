# phased-rollout-template

Scaffolds a new project with the standard structure we converged on across
phases 1–4 of real production rollouts: a load-bearing `CLAUDE.md`, a
pre-deploy accounts checklist, a 13-section deploy runbook, a 9-job CI
pipeline, and engineering process / code-review / security docs.

Provides one skill: `/init-phased-project`.

## What it scaffolds

```
.
├── CLAUDE.md                       # canonical project guidance for Claude Code
├── pyproject.toml                  # uv + ruff + mypy + pytest config
├── Dockerfile                      # multi-stage Python image with ffmpeg + libmagic
├── .dockerignore
├── infra/
│   ├── README.md                   # quick-reference card
│   ├── ACCOUNTS.md                 # pre-deploy 9-service signup checklist
│   ├── DEPLOY.md                   # 13-section production deploy runbook
│   └── cloud_run.yaml              # declarative Cloud Run spec
├── docs/
│   └── engineering/
│       ├── process.md              # branching, DoR/DoD, deploy gates
│       ├── code-review.md          # PR review checklist + blocking-vs-suggesting
│       └── security.md             # threat model, secret discipline, audit logging
└── .github/
    └── workflows/
        ├── ci.yml                  # 9 jobs: lint, typecheck, test, alembic round-trip,
        │                           # migration conventions, NDA scan, pip-audit, hadolint, bandit
        └── deploy.yml              # manual workflow_dispatch deploy via WIF
```

The templates ship as `.tmpl` files with `{{PLACEHOLDER}}` tokens; the skill
substitutes them at scaffold time.

## Placeholders

| Token | Replaced with |
|---|---|
| `{{PROJECT_NAME}}` | `--name` arg (required) |
| `{{PROJECT_DESCRIPTION}}` | `--description` arg (optional, defaults to a generic line) |
| `{{CURRENT_YEAR}}` | the current calendar year (reserved — no template currently uses it, but the skill will substitute it if you add it) |

The templates intentionally keep all the load-bearing conventional content
in place — the 9-check CI pipeline, the ADR-011 migration-convention check,
the NDA-data scan, the bandit + hadolint + pip-audit jobs, the Phase 1-2-3-4
rollout framing in `CLAUDE.md`, the 13-section `DEPLOY.md` structure, and
the 9-service `ACCOUNTS.md` table. Those are the value proposition.

## Example

```bash
mkdir my-new-bot && cd my-new-bot
claude  # start a Claude Code session
```

Inside Claude Code:

```
/init-phased-project --name my-new-bot --description "Slack bot that summarizes engineering standups"
```

The skill verifies the cwd is empty (or only has `.git/`, `.gitignore`,
`README.md`), substitutes placeholders, writes every template to its
matching path, and prints next-step instructions.

## Customizing the templates

The templates live at:

```
~/claude-code-toolbox/plugins/phased-rollout-template/templates/
```

Edit any `.tmpl` file there to change what future scaffolds produce. Commit
the change and `git pull` on every device that uses the toolbox marketplace.

To add a new placeholder, edit the skill (`skills/init-phased-project.md`)
to define and substitute it, then reference `{{YOUR_TOKEN}}` wherever you
need it in any `.tmpl` file.

## Install

Per-project, in `.claude/settings.local.json` (or user-global, in
`~/.claude/settings.json`):

```json
{
  "enabledPlugins": {
    "phased-rollout-template@toolbox": true
  }
}
```

## Roadmap

Possible additions (open an issue or PR):

- A `--with-telegram` flag that wires aiogram boilerplate into `app/bot/`.
- A `--with-stripe` flag that adds `app/billing/` and the Stripe webhook
  route stub.
- A `--minimal` flag that drops the GCP-specific infra/ files and keeps
  only CLAUDE.md + CI + pyproject.
