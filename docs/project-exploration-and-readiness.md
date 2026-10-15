# Project exploration and environment readiness skills

AOK now includes two repository skills: `project-exploration` for source-backed discovery
and `environment-readiness` for task-specific setup and evidence. Their initial helpers
collect metadata, query selected source and assess supplied records. They do not install
tools, run autonomous setup or add workflow/campaign schemas.

## Availability

The root Codex plugin declares `./skills/` and release packaging includes that tree,
including references, assets and scripts. The new skills therefore use the existing root
plugin delivery path. They are not yet added to pack-specific renders or the separate
Claude compatibility plugin. Other hosts can use the skill files only through their
supported discovery/import mechanism; no new cross-target execution parity is claimed.

The helpers require Python 3.10+. Rust CLIs are optional external tools, not vendored
binaries. AOK's no-terminal installer does not install Python or these CLIs for the skills.
Reports and manual exploration remain possible without the helpers.

## Project exploration

Read [the skill](../skills/project-exploration/SKILL.md), then start with a bounded inventory:

```bash
python3 skills/project-exploration/scripts/inventory.py /path/to/project \
  --max-depth 4 --max-files 2000 --max-entries 10000
```

Run from the AOK checkout, or use the installed script's absolute path. JSON is printed
to stdout; no report is written into the target automatically. Inventory deliberately
includes relevant hidden configuration and skips symlinks/vendor/build trees. It uses
fixed exclusions, not full Git ignore semantics; coverage and omissions are explicit.

Optional read-only queries use one source file at a time:

```bash
python3 skills/project-exploration/scripts/structure.py /path/to/project \
  --file src/server.py --mode text --pattern 'route'
python3 skills/project-exploration/scripts/structure.py /path/to/project \
  --file src/server.py --mode pattern --lang python --pattern '$APP.get($$$ARGS)'
python3 skills/project-exploration/scripts/structure.py /path/to/project \
  --file src/server.py --mode outline --lang python
python3 skills/project-exploration/scripts/structure.py /path/to/project \
  --file src/server.py --mode stats
```

Modes use ripgrep, ast-grep and Tokei respectively. Outline support is detected; missing
backends, unsupported outline, invalid output, timeouts and output limits are explicit.
The wrapper stages a bounded copy in a temporary directory, isolates ast-grep configuration
and never enables rewrites. It records tool version, selected file and backend-specific
evidence. Results are syntax/text/statistics, not resolved call graphs or runtime proof.
Probe, LSP integration and bulk rule catalogs remain future work.

## Environment readiness

Read [the skill](../skills/environment-readiness/SKILL.md). Select inspection,
local-development, service-integration, application-verification or delivery, then perform
only necessary authorized setup and relevant verification. Reuse existing project commands
and package managers; preserve unrelated changes and keep secret values out of reports.

The optional helper checks the skill-local JSON format documented in
[profiles and evidence](../skills/environment-readiness/references/profiles-and-evidence.md):

```bash
python3 skills/environment-readiness/scripts/assess.py /path/to/report.json \
  --expected-revision ACTUAL_REVISION
```

It checks supplied evidence references, required dimensions, dispositions and revision
consistency. It does not execute commands or inspect evidence artifacts. Callers must
verify the actual observations and enumerate every task-required capability. Ready means
consistent supplied records, not an independently certified environment.

A required failure is not-ready; missing/unexecuted evidence is blocked. Optional failures
remain visible. A required not-applicable check cannot waive itself. Exploration completion
is separate from readiness. A successful AOK setup-helper exit is insufficient: that helper
tolerates diagnostic failures and does not install application dependencies.

## Validation and next steps

Run `python3 -m unittest discover -s tests -p test_exploration_readiness.py` for helper
behavior. Installed ripgrep/ast-grep backends are exercised when available; absent optional
backends skip their live tests. Tests cover bounds, hidden configuration, symlinks, failures,
output limits, read-only queries, readiness blockers and revision mismatch. Release tests
check that root-plugin packaging includes the skill resources.

This is the initial content/helper implementation of P1-09/P1-10 in the
[research implementation plan](research/development-workflows/implementation-plan.md).
Live agent forward-testing on unfamiliar applications, wider target projections, native
Goal integration and durable orchestration remain unimplemented.

Initial verification on 2026-10-15: 59 Python tests and `go test ./...` passed. After the
final process-cleanup changes, all 12 focused helper tests passed again with resource
warnings treated as errors. Both skills passed Skill Creator structural validation;
kit validation, Python compilation and whitespace checks passed. Live checks used
ripgrep, ast-grep 0.51.0 (Python/Go/TypeScript patterns and Python outline) and Tokei 15.0.0.
Optional test binaries were installed under a temporary directory, not globally.
