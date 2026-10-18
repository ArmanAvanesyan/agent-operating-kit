# Agent Operating Kit

Agent Operating Kit (AOK) is the cross-tool operating system for agent
workflows. It defines one neutral model for tasks, roles, hooks, skills, packs,
and tool adapters, then renders that model into the agent tools a project
actually uses.

AOK is the source of truth. Tool-specific repositories and plugin formats are
adapters or content packs, not separate operating models.

## No-Terminal Install

Use this path when you want the shortest route to a working install without
cloning the repository. Release installers put a thin Go `aok` binary on
`PATH` so you do not need Python or a Go toolchain on the device. See
[docs/cli-distribution.md](docs/cli-distribution.md).

1. Open the latest GitHub release.
2. Download the installer for your platform:
   - `agent-operating-kit-<version>-macos-apple-silicon-installer.zip`
   - `agent-operating-kit-<version>-macos-intel-installer.zip`
   - `agent-operating-kit-<version>-windows-installer.zip`
   - `agent-operating-kit-<version>-linux-user-installer.tar.gz`
   - `agent-operating-kit-<version>-linux-system-installer.tar.gz`
3. Run the installer command inside the extracted package:
   - macOS: `install.command`
   - Windows: `install.cmd` (or `install.ps1`)
   - Linux user: `./install.sh`
   - Linux system: `sudo ./install.sh`
4. Open Codex Desktop and confirm Agent Operating Kit is available in the local
   plugin marketplace.

Installer outputs:

- macOS: `~/Library/Application Support/Agent Operating Kit/agent-operating-kit`
- Windows: `%LOCALAPPDATA%\Agent Operating Kit\agent-operating-kit`
- Linux user: `${XDG_DATA_HOME:-$HOME/.local/share}/agent-operating-kit/agent-operating-kit`
- Linux system: `/opt/agent-operating-kit/agent-operating-kit`

Each installer writes a setup report near its install root.

On Windows, AOK installs the plugin directory with a junction when possible and
falls back to a copy if junction creation is unavailable. The CLI entrypoint is
`~/.local/bin/aok.cmd` (release path: Go binary / shim, not a link to
`scripts/aok`).

## Install via package managers

GitHub Releases remain the source of truth for binaries. **Releases are
unsigned**; verify with the Release `SHA256SUMS` file (OS may warn once on
first run). Homebrew, Scoop, and winget install from this repository’s
published manifests — no separate tap/bucket repo required (see
[docs/cli-distribution.md](docs/cli-distribution.md) and
[`packaging/`](packaging/)).

| Channel | Install |
|---|---|
| Scoop | `scoop bucket add aok https://github.com/ArmanAvanesyan/agent-operating-kit` then `scoop install aok` (`bucket/aok.json`) |
| Homebrew | `brew install --formula https://raw.githubusercontent.com/ArmanAvanesyan/agent-operating-kit/main/packaging/homebrew/aok.rb` (classic tap deferred) |
| winget | Manifests under `packaging/winget/`; submit to `microsoft/winget-pkgs` when ready |
| PyPI | **Not planned** for the Go `aok` binary (wrong distribution model) |

Regenerate manifests from a release tag or local `dist/release` (updates
`bucket/`, `packaging/homebrew/aok.rb`, and `packaging/winget/*.yaml`):

```bash
./scripts/packaging/generate-from-release.sh --version 0.6.1
```

## Developer First Success Path

Use this path when you are comfortable with the terminal and want a validated
checkout. The developer CLI remains the Python entrypoint at `./scripts/aok`.

```bash
git clone https://github.com/ArmanAvanesyan/agent-operating-kit.git
cd agent-operating-kit
./scripts/aok codex setup
./scripts/aok validate
./scripts/aok render pack engineering-guardrails --target codex
```

Success means:

- `doctor` reports required files, marketplace registration, plugin path, and
  CLI shim as `OK`.
- `validate` prints `Kit validation passed.`
- the render command writes
  `build/renders/engineering-guardrails/codex.json`.

## Downloadable Bundles

Every release also includes `agent-operating-kit-<version>-bundles.zip`. It
contains self-contained, parseable bundles for every pack and every target:

- Codex: native plugin-oriented bundle.
- Claude Code: compatibility marketplace assets.
- Cursor, OpenCode, Gemini, OpenClaw, Hermes Agent: generated import bundles.

Technical users can regenerate the same bundles:

```bash
./scripts/aok render bundles --target all
```

## Codex App Local Environments

AOK includes Codex App local-environment helpers for worktree setup and top-bar
actions. Project init installs `.codex/setup.sh`, `.codex/aok-action.sh`,
`.codex/aok-local-environment.json`, `.codex/environments/environment.toml`, and
`.codex/config.toml`.

```bash
./scripts/aok codex local-env init /path/to/project
./scripts/aok codex local-env validate /path/to/project
./scripts/aok codex local-env actions /path/to/project
```

Codex can read `.codex/environments/environment.toml` for setup and actions.
The JSON manifest is AOK-owned metadata for validation and onboarding. See
`docs/codex-local-environments.md`.

For a single chat-friendly setup flow from the AOK checkout:

```bash
./scripts/aok codex setup
```

Add `--claude` to also register the Claude Code marketplace, or `--report
/path/to/report.md` to write the setup summary to disk.

Use `--project /path/to/project` when you also want AOK to initialize Codex
local-environment files and `AGENTS.md` content in a downstream project. Do not
point `--project` at the AOK repo checkout itself.

## Claude Code First Success Path

Register AOK's Claude Code compatibility marketplace with the canonical key
`aok-claude-code`:

```bash
aok claude setup --enable engineering-guardrails
```

The setup helper preserves a timestamped backup of existing settings before it
updates `~/.claude/settings.json`. The resulting settings entry is:

```json
{
  "extraKnownMarketplaces": {
    "aok-claude-code": {
      "source": {
        "source": "directory",
        "path": "/path/to/agent-operating-kit/adapters/claude-code/marketplace"
      }
    }
  },
  "enabledPlugins": {
    "agent-operating-kit@aok-claude-code": true,
    "engineering-guardrails@aok-claude-code": true
  }
}
```

Then render the same pack for Claude Code:

```bash
./scripts/aok render pack engineering-guardrails --target claude-code
```

Success means Claude Code can see the root `agent-operating-kit` plugin and the
`engineering-guardrails` pack through `aok-claude-code`, and AOK writes
`build/renders/engineering-guardrails/claude-code.json`.

## Pack Chooser

| Pack | Choose it when you need | Main concepts |
|---|---|---|
| `engineering-guardrails` | Session reminders, pre-commit freshness checks, migration conventions, and assert/future-annotation guardrails. | hooks, role, task packet |
| `ship-pipeline` | Pull request shipping flow, CI failure recovery, and a CI fixer role. | skills, roles, task packet |
| `phased-rollout-template` | A starter scaffold for phased projects with CI, deploy, infra, and process docs. | templates, skill, role |
| `batch-resolve-conflicts` | Coordinated conflict resolution across multiple PRs or branches. | skill, role, task packet |

Render any pack with:

```bash
./scripts/aok render pack <pack-id> --target <target>
```

## Supported Targets

All current packs declare active render support for:

| Target | Support | Output style |
|---|---|---|
| `codex` | Active | Generated Codex JSON contract |
| `claude-code` | Active compatibility adapter | Preserved native Claude Code assets plus compatibility render |
| `cursor` | Active | Generated structured contract |
| `opencode` | Active | Generated structured contract |
| `gemini` | Active | Generated structured contract |
| `openclaw` | Active | Generated structured contract |
| `hermes-agent` | Active | Generated structured contract |

See `docs/targets-support-matrix.md` for the detailed matrix.

## Project Init

Initialize AOK operating files in another project:

```bash
./scripts/aok project init /path/to/project
./scripts/aok project validate /path/to/project
```

Initialized projects include Codex local-environment helpers:
`.codex/setup.sh`, `.codex/aok-action.sh`,
`.codex/aok-local-environment.json`, `.codex/environments/environment.toml`,
and `.codex/config.toml`.

If the project already has `AGENTS.md`, AOK preserves it by default and writes
`AGENTS.aok-proposed.md` plus `AGENTS.aok.diff`. Use `--force` only when you
intentionally want AOK to replace `AGENTS.md` after writing a backup.

See `docs/project-init-guide.md` for the full guide.

Scaffolded delivery frameworks can be inspected without selecting them:

```bash
./scripts/aok methodology scaffold /path/to/project --flow fdd
./scripts/aok methodology scaffold /path/to/project --flow dsdm
./scripts/aok methodology scaffold /path/to/project --flow aup
./scripts/aok methodology validate-feature --name "Generate PDF for Invoice"
./scripts/aok methodology validate-requirement \
  --id REQ-014 \
  --outcome "Produce customer-ready invoice PDF" \
  --project-moscow must \
  --acceptance "The generated PDF matches the approved invoice content"
./scripts/aok methodology validate-iteration \
  --id I-12 \
  --stage construction \
  --objective "Deliver invoice generation increment" \
  --work-item use-case=UC-03 \
  --discipline model=docs/architecture.md \
  --discipline implementation=PR-142 \
  --discipline test=reports/test-results.xml \
  --discipline deployment=not-applicable:No-release-activity \
  --discipline configuration-management=build-142 \
  --discipline project-management=docs/iteration-plan.md \
  --discipline environment=ci/run-912
```

The export contains methodology-specific roles, artifacts, lifecycle stages,
ceremonies, and gates. It does not modify project methodology state.
FDD exports curated domain, feature-list, planning, design, inspection, and
milestone-tracking templates; the validator checks short and classic FDD names
without activating the flow.

## Work-System Bootstrap

After local project initialization, inspect an existing GitHub Project or Azure
DevOps board against AOK's neutral workflow contract:

```bash
./scripts/aok work-system setup github --owner OWNER --project-number 12
./scripts/aok work-system setup azure-devops \
  --organization https://dev.azure.com/ORG --project PROJECT
./scripts/aok work-system validate /path/to/project
```

Both providers are currently scaffolded and read-only. A conformant existing
work system can be recorded with `--write-binding`; remote provisioning is not
yet enabled. See `docs/work-system-bootstrap.md`.

## Methodology Profiles

Methodology, work-item, and work-system commands currently use the contributor
Python entrypoint (`./scripts/aok`), not the distributed Go binary.

Projects use one flow and optional practice overlays independently of their
GitHub or Azure DevOps binding:

```bash
./scripts/aok project init /path/to/project \
  --flow kanban --overlay xp --overlay lean
./scripts/aok methodology validate /path/to/project
```

Kanban is the active default flow. XP and Lean are active overlays. FDD, DSDM,
and AUP are scaffolded catalog entries, and AM is planned. See
`docs/methodology-profiles.md`.

## Work-Item Templates

Generate methodology-aware neutral templates, GitHub Issue Forms, and Azure
DevOps desired-state guidance without changing remote resources:

```bash
./scripts/aok work-item list --flow kanban --overlay xp
./scripts/aok work-item scaffold /path/to/project
./scripts/aok work-item validate /path/to/project
```

Every export includes Epic, Feature, Task, Bug, Spike, Incident, Risk, and
Change. Selected flows and overlays add their native types. See
`docs/work-item-types.md`.

## Project Exploration and Readiness

The root Codex plugin includes `project-exploration` and `environment-readiness` skills.
They provide bounded discovery, optional Rust CLI structural queries and evidence-based
readiness guidance. Their Python helpers install nothing; full workflow/runtime automation
remains separate. See [project exploration and readiness](docs/project-exploration-and-readiness.md).

## Repository Relationships

- `ArmanAvanesyan/agent-operating-kit` is the canonical public project.
- `ArmanAvanesyan/claude-code-toolbox` is represented here as Claude Code
  adapter assets and curated packs.
- `ArmanAvanesyan/swarmd` remains separate. AOK integrates with it through MCP
  by generating config and mapping task packets, roles, decisions, and events
  onto the `swarmd` MCP surface.

## Render Contracts

AOK can render each pack into parseable target contracts:

```bash
./scripts/aok render pack <pack-id> --target <target> [--out PATH]
./scripts/aok render all --target all [--out DIR]
./scripts/aok render bundle <pack-id> --target <target> [--out DIR]
./scripts/aok render bundles --target all [--out DIR]
```

`target` is one of `codex`, `claude-code`, `cursor`, `opencode`, `gemini`,
`openclaw`, `hermes-agent`, or `all` where supported by the command.

## Usage Model

1. Initialize the project and select its flow and optional overlays.
2. Create or sync a task packet.
3. Select a role for implementation and one role for QA or review.
4. Run pre-hooks to check project context, scope, worktree safety, and risk.
5. Execute one issue-sized change.
6. Run verification and after-hooks.
7. Record GitHub, swarmd, or local lifecycle updates.

The neutral schema lives in `schemas/`. Tool-specific target behavior lives in
`adapters/`. Methodology profiles live in `methodologies/`, work-item contracts
live in `work-items/`, and reusable workflow content lives in `packs/`.

## Claude Code Toolbox Migration

The former Claude Code Toolbox content is now represented as AOK packs:

- `adapters/claude-code/aok-plugin` as the root AOK Claude Code control plugin
- `packs/engineering-guardrails`
- `packs/ship-pipeline`
- `packs/phased-rollout-template`
- `packs/batch-resolve-conflicts`

Claude Code compatibility assets are preserved under each pack's
`claude-code/` directory. See
`docs/migration-from-claude-code-toolbox.md` for before/after settings and
migration guidance.

## Operations

- Install and first-run onboarding: `docs/install-and-onboarding.md`
- CLI distribution (Go binary vs Python): `docs/cli-distribution.md`
- Codex local environments: `docs/codex-local-environments.md`
- Project initialization: `docs/project-init-guide.md`
- Methodology-aware work items: `docs/work-item-types.md`
- Target support: `docs/targets-support-matrix.md`
- Host compatibility: `docs/host-compatibility.md`
- Release checklist: `docs/release-checklist.md`
- Uninstall and rollback: `docs/uninstall-and-rollback.md`
- `swarmd` integration: `docs/swarmd-integration.md`

## Validation

Run:

```bash
./scripts/aok validate
python3 -m py_compile scripts/aok
```

Validation is dependency-free and checks required docs, schemas, adapter
manifests, pack manifests, pack content, JSON syntax, Claude Code marketplace
metadata, and supported target coverage.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for local validation, render checks, and
conventions for packs, adapters, and schema changes.

## Static agent and planning foundations (Go CLI)

The built Go binary provides canonical agent validation/resolution, planning
and decision validation, and explicit native agent import/export/compare for eight
clients. See the [operator guide](docs/native-adapter-operations.md),
[agent resolution](docs/agent-resolution.md), and
[planning foundations](docs/planning-foundations.md). These commands do not
execute workflows or models. Four native-loading gates remain open; runtime
policy enforcement is unverified. agy native registration is separate from the
seven-target pack renderer. The Python contributor entrypoint is unchanged.
