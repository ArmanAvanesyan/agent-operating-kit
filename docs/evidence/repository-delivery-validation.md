# Repository delivery integration validation

Local verification on macOS, 2026-10-18. Source baselines and imported bytes are
recorded in `packs/repository-delivery/import-provenance.json`. Machine evidence
is in [summary.json](repository-delivery/summary.json).

## Executed gates

- Embedded suite: **145 tests passed, zero skipped**, using pinned development
  requirements in a disposable virtual environment. **95% coverage**, above the
  80% threshold; `runtime_finalize` has **100% coverage**. See
  [test result](repository-delivery/engine-tests.txt) and [coverage](repository-delivery/coverage.txt).
- Real swarmd integration: eight tests passed against clean exports of both
  `0a4298b1cbbc3cd848f20a97527752e270abc6c3` and the original compatibility pin
  `f7a515320f4e939e608b13c15dca8b7f4aa25b88`. Binaries and test-only attempt
  producers were built outside source checkouts. Every test used fresh databases.
  Accepted, stale/mismatched references, changed artifacts, expired/revoked leases,
  failed outcomes and retry conflicts were exercised. Capability absence and
  ambiguous responses are covered by deterministic unit tests; no fallback or
  automatic retry was added. [Historical pin result](repository-delivery/historical-runtime.txt).
- AOK Go suite/build, Python suite, kit validation, compilation, both proposal
  checkers and release packaging passed locally. Root Python initially ran 266
  tests (three deliberate skips, including opt-in artifact verification); the
  artifact test was then explicitly executed, passing both installed-flow and
  shipping-authority checks. The final root suite also includes guide-escape tests.
- Both renderers generated all seven pack targets. **14 relocated delivery
  bundles** loaded their bundled launcher with AOK_ROOT/PYTHONPATH unset.
  Codex skill paths are strings; Claude/Cursor wrappers resolve to canonical
  bundled guides. Regression tests reject unsafe skill roots, guide escapes,
  symlink resources and release payloads, and exclude development caches.
- Built a real release containing kit, packs, Codex plugin, all installers,
  bundles and cross-compiled binaries. Installed the macOS Intel artifact under a
  disposable home, from outside both source repositories. Existing Go `validate`
  passed with an empty PATH. Inspection → checkpoint → verification → selective
  port → gate/review evidence passed, followed by rejection of stale evidence.
- Explicit hook tests prove installed helper paths are used, and that moved or
  missing content fails closed even when an ambient package could be imported.
  Installation/rendering itself installs no repository hooks.

## Reproduction

Use Python 3.11+ and the pinned `packs/repository-delivery/config/requirements-dev.txt`
in a disposable environment. Run `make quality-fast PYTHON=/path/to/venv/bin/python`
inside the pack. Opt into real runtime checks only with binaries built from clean
pinned exports, via `REPO_DELIVERY_SWARMD_BINARY` and
`REPO_DELIVERY_SWARMD_FIXTURE_BINARY`; never point tests at active databases.
Public CI requires no private swarmd secret.

Run `go test ./...`, `python3 -m unittest discover -s tests`,
`./scripts/aok validate`, `python3 -m py_compile scripts/aok`, and both proposal
`validate_examples.py` checkers. Build with `go run ./cmd/aok-release --root .
--dist /temporary/release`, then run `python3 scripts/testing/verify-delivery-release.py
--release-dir /temporary/release`. Linux/macOS CI repeats actual installation with
Go 1.22; the existing six-platform compile matrix remains authoritative.

## Independent review and preservation

Independent agent review identified shipping authority, native manifest/link,
resource metadata/filtering and release-symlink findings. These were fixed and
affected gates rerun. Exact reviewed commit and hosted CI facts are recorded in
the open PR, not inferred from this document. Agent review is separate from
submitted GitHub approval. This task does not authorize merging.

Original AOK and repo-delivery HEAD, tracked diff, status and retained-worktree
observations match the preflight. No private checkpoint contents were opened,
modified or published; no active runtime database was used.

A concurrent task advanced swarmd from `0a4298b` to `b113532` (PR #54) and changed
its worktree list, replacing the previously observed sqlite-agent-memory worktree
with guided-onboarding. Its untracked status and tracked diff remained unchanged.
This integration did not restore, delete or modify that external state. The
compatibility evidence remains bound to the two exact tested pins; the concurrently
advanced main is not claimed as tested. Observation-level checks cannot establish
byte-level integrity of private checkpoints that were deliberately not read.

## Hosted macOS linker correction

The first Go 1.22 macOS installation job failed before delivery ran: dyld rejected
an executable without LC_UUID. The installed-artifact job now applies
`GOFLAGS=-ldflags=-B=gobuildid` for its bootstrap and nested Go commands. Release
builds explicitly add the flag for Darwin targets and test both Mach-O images for
UUID commands. This uses the [Go 1.22 backport](https://go.googlesource.com/go/+/e3fd4ba7f954d7025b8ba9e3f7961344cccad922),
retaining the minimum-toolchain gate. The original failure was not counted as a
passing installation; the PR's current checks record the rerun.

## Remaining limits

Python 3.11+ is optional for AOK and required for delivery helper operations;
jsonschema, Git, gh and swarmd are required only by affected operations. No
interpreter or dependency is installed automatically. Helpers are initially
certified for Linux/macOS; Windows preservation is blocked. Generated host
entrypoints and static loading do not prove native runtime policy enforcement.
There is no producer, heartbeat, recovery, approval quorum or AOK submission.
At validation time the standalone repository was left unchanged. That repository
is now retired; this record remains evidence of the imported `cf7bae5` snapshot.
