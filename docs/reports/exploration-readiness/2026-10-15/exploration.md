# AOK project exploration report

Date: 2026-10-15. Base revision: `9dec920f6cb202e4733f5d8d9504441d51cfe729`.
Branch: `codex/verify-exploration-readiness`. The checkout was clean before this task.
The saved inventory includes this report directory created by the run; it is not a
byte-for-byte snapshot of the untouched base tree. Fixes are captured in the linked
source-state record and patch, rather than attributed to the base commit.

Exploration outcome: **complete for repository architecture, contributor development and
local verification**. Environment outcome is assessed separately in [readiness.md](readiness.md).
No global agent settings, production systems or remote work resources were changed.

## Project map

AOK is a cross-tool contract/content/adapter project. It defines neutral task packets,
roles, hooks, skills, packs, methodology artifacts and work-item types. Its product is
configuration, procedures, exports and release tooling; this checkout is not a web service.

| Component | Responsibility | Source |
|---|---|---|
| Neutral model | Shared objects and policy boundaries | [core/model.md](../../../../core/model.md), [schemas](../../../../schemas) |
| Contributor CLI | Validation, initialization, methodology/work-item/work-system helpers and rendering | [scripts/aok](../../../../scripts/aok) |
| Distributed CLI | Go command router and implementation packages | [cmd/aok/main.go](../../../../cmd/aok/main.go), [internal/cli](../../../../internal/cli) |
| Content | Curated roles, hooks, skills, templates and task packets | [packs](../../../../packs), [skills](../../../../skills) |
| Target projections | Per-tool mappings and support limits | [adapters](../../../../adapters), [support matrix](../../../targets-support-matrix.md) |
| Methodology/work items | Independent planning/classification axes and templates | [methodology profiles](../../../methodology-profiles.md), [work items](../../../work-item-types.md) |
| Release tooling | Binary/archive/installer construction | [internal/releasebuild](../../../../internal/releasebuild), [build wrapper](../../../../scripts/release/build.sh) |
| CI | Python validation/tests/compilation and Go build/tests | [ci.yml](../../../../.github/workflows/ci.yml) |

These relationships are supported by inspected source, not inferred from folder names
alone. No CODEOWNERS-based human ownership assignment was established. Relevant owning
areas are the CLI, contracts, adapters, packs and release tooling; named maintainer
assignment remains a project governance decision.

## Execution paths

Observed in source: `scripts/aok` builds an argparse command tree, parses arguments and
calls the selected `args.func`. Validation routes to kit checks. Errors are surfaced through
CLI exception handling. The outline/text evidence locates its declarations and main entry.

Observed in source: the Go binary calls `cli.Run(os.Args[1:], ...)`; the router dispatches
to command implementations such as validation/rendering. A fresh binary was built outside
the checkout and its `validate` command ran successfully. This is actual execution evidence
for that path, not every Go subcommand.

Static release path: `scripts/release/build.sh` runs `go run ./cmd/aok-release`, which uses
`internal/releasebuild`. Go tests exercised release construction; this run did not publish
a release or certify installers on their destination OSes.

AOK owns portable intent/content. It does not own a durable worker queue, sandbox or
infrastructure. The swarmd mapping remains documented with untracked compatibility;
no swarmd binary, database or MCP method was exercised here.

## Technology and dependency inventory

Observed environment: Python 3.14.6, Go 1.27.2 on darwin/amd64, ripgrep 15.2.0,
ast-grep 0.51.0 and Tokei 15.0.0. Exact version output is saved under `evidence/`.
The contributor helpers require Python 3.10+; `go.mod` declares Go 1.22 and the local CI
uses the 1.22.x toolchain. Passing locally with 1.27.2 does not independently certify 1.22.

The bounded initial inventory enumerated 326 files, including 34 Go, 12 Python, 9 shell
and 1 Ruby file by extension. It explicitly reports 2 hidden-directory exclusions,
9 fixed-name exclusions and 8 depth omissions. Counts are metadata coverage, not code
size or architectural importance. The extensionless `scripts/aok` is not counted as Python
by inventory's extension heuristic; its language is established by inspected source/shebang.

The Go module declares no additional module requirements. The core validation/test path
uses Python's standard library. No database, queue, browser or cloud credential was needed
for the selected local-development scope. Optional structural tools were available through
existing `/tmp/aok-exploration-tools` binaries added only to child-process PATH.

## Command matrix

Commands run from the repository root unless noted. Each observed result is linked through
[commands.json](commands.json), which records exact argv, timing, exit status and logs.

| Purpose | Command | Prerequisites | Result |
|---|---|---|---|
| Inventory | `python3 skills/project-exploration/scripts/inventory.py . --max-depth 4 --max-files 2000 --max-entries 10000` | Python | Observed partial coverage with explicit omissions |
| Contributor validation | `./scripts/aok validate` | Python | Observed pass |
| Python tests | `python3 -W error::ResourceWarning -m unittest discover -s tests -v` | Python; optional CLIs for live backend tests | See final test log |
| Go tests | `go test ./...` | Go and platform build support | Observed pass |
| Build distributed CLI | `go build -o /tmp/aok-readiness-build/aok ./cmd/aok` | Go | Observed pass |
| Fresh binary smoke | `/tmp/aok-readiness-build/aok validate` | Built binary and kit checkout | Observed pass |
| Python compilation | `python3 -m py_compile ...` | Python | Observed pass; exact files in command record |
| Downstream initialization | `./scripts/aok project init <temporary-project>` | Python; writable scratch directory | Observed pass |
| Downstream validation | `project validate` and `codex local-env validate` for the scratch project | Generated operating files | Observed pass |
| Render smoke | `render pack engineering-guardrails --target codex --out <temporary-file>` | Kit content | Observed pass |
| Structural queries | `structure.py` text/outline/pattern/stats modes | Appropriate optional Rust backend | Observed results; syntax/statistics only |

The downstream scratch directory was cleaned automatically. Do not run project init against
the AOK source checkout just to demonstrate bootstrap. Installation and user-level setup
commands were intentionally not needed for this profile.

## Issues found and resolved

1. **Extensionless entrypoint rejected.** `structure.py` rejected `scripts/aok` despite a
   Python shebang. The before log reproduces exit 2. The helper now recognizes extensionless
   Python/shell shebangs and stages a matching suffix without executing the source. Two
   regression tests cover accepted scripts, rejected unknown/dot/config files and filename
   preservation. The after outline, text and Tokei invocations succeed.
2. **Stale core rendering description.** `core/model.md` described Cursor skills as rules
   and OpenCode as commands. It now reflects Cursor skills alongside rules and OpenCode's
   structured contract/Markdown guidance, consistent with the existing support matrix.

## Evidence quality and handoff

Observed: source inspection, bounded inventory, backend results, fresh build, tests and
scratch-project commands. Documented but untested: native agent discovery in external
hosts, real GitHub/Azure board setup, native Goal/runtime behavior, installer behavior on
other operating systems and user-level installation health. Unknown: production consumer
usage or external integration behavior not exercised by this local task.

Next work can use the two CLIs and existing task/verification contracts. Wider adapter
projections, a real application forward-test and durable workflow integration remain
separate tasks. Revalidate affected evidence after changes; do not treat this dated report
as a permanent environment guarantee.
