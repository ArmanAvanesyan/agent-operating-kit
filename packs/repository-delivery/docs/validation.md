> Imported historical upstream record. Its validation claims describe the pinned source, not this AOK integration. Current evidence is in AOK docs/evidence/repository-delivery-validation.md.

# Validation records

## Milestone 1

Validated locally on 2026-10-18. The [roadmap](roadmap.md) marks milestone 1 complete; the [usage guide](README.md) contains the reproduced developer commands.

| Check | Result |
| --- | --- |
| Local quality gate | `make quality` passed: 22 unique real Git fixture tests, 95% coverage, compilation and whitespace checks. |
| Build and installed command | Wheel and source distribution built; installed CLI help passed. |
| Plugin manifest | Validated against the published plugin manifest schema. |
| Documentation and skills | README shell examples ran against disposable Git fixtures; both skills validated and cross-links resolved. |
| Independent review | Findings fixed for embedded tracked repositories, archive structural completeness, whitespace paths, and symlink hashes. Required checks reran after fixes. |
| Source preservation | Four swarmd checkouts retained matching patches/status and 15,239 file hashes; all 31 durable checkpoint checksums unchanged. |

CI is configured for Ubuntu with Python 3.11 and 3.13. These remote jobs have not been run as part of this local delivery. The project has not been installed into Codex, and no remote repository has been created.

At milestone 1, versioned schemas under [`config/schemas/`](../config/schemas/) covered inventory, checkpoint, preservation receipt, and comparison records. Milestone 2 adds the [delivery evidence contracts](design.md#evidence-and-authorization).


## Milestone 2

Completed locally on 2026-10-18.

| Check | Result |
| --- | --- |
| Final quality gate | `make quality` passed: 72 tests, 95% total coverage, compilation and whitespace checks clean. |
| Build and installed command | Wheel and source distribution built; installed delivery and PR-readiness CLI help passed. Packaged skills, schemas, adapter, and delivery guide verified. |
| Evidence contracts | All 14 JSON schemas validate; actual emitted M2 records pass positive and negative schema tests. |
| Independent review | Separate reviewer context approved after regression-tested fixes for staged/ignored scope, terminal PR identity, failed gate exit status, checkpoint output protection, and PR creation races. See [review record](reviews/milestone-2.md). |
| Documentation and skills | Disposable source-to-isolated-checkout context/gate/status example executed; six skills and 58 relative links validated. |
| Source preservation | Four swarmd checkouts retained matching patches/status and 15,239 file hashes; all 31 durable checkpoint checksums unchanged before and after implementation. |

Git tests use real temporary repositories, including recovery/port/cleanup scenarios. GitHub adapter tests mock the remote boundary: no actual PR was opened or merged, no hosted CI was run, and no actual submitted GitHub review is claimed. CI remains configured for Ubuntu Python 3.11 and 3.13; those hosted runs await publication. The plugin has not been installed into Codex and no remote repository was created.


## Milestone 3

Completed locally on 2026-10-18 within the supported adapter boundaries documented in the [integration guide](integrations.md).

| Check | Result |
| --- | --- |
| Final quality gate | `REPO_DELIVERY_SWARMD_BINARY=<isolated-binary> make quality` passed: 122 tests, no skips, 94% coverage, compilation and whitespace checks clean. |
| Actual runtime gate | `make test-runtime-integration` passed both real process/SQLite/MCP tests against swarmd source revision `e67ce018ee18c51bb9e15ed1afb17f7f3dfc580a`, exported with `git archive` and built outside the primary checkout. All databases were fresh temporary fixtures. |
| HTTP integration proof | Tests contacted an actual loopback HTTP version service, verified version mismatch/unavailability blockers, and rejected mock/mixed substitution for a required real policy. This does not claim production sidecar coverage. |
| Package and contracts | Wheel and source distribution built; installed integration/runtime CLI smoke checks passed with the optional AOK extra. All 24 JSON schemas validate; emitted M3 records have positive/negative schema tests. |
| Upstream contract compatibility | AOK task packet schema pinned to commit `892398550accd0538a188ab984dbdb2ae99f3cae` and SHA-256 recorded in fixture provenance. Runtime export mappings reflect the inspected committed swarmd contracts. |
| Independent review | Approved after regression-tested fixes for CLI routing, dirty-source push certification, and duplicate live issue matching. See [review record](reviews/milestone-3.md). |
| Documentation and skills | M3 command help and disposable mock-proof example executed; both new skills and 75 relative links validated before final completion records. |
| Preservation | Before/after checks confirm four swarmd checkouts retain matching patches/status and 15,239 file hashes; all 31 durable checkpoint checksums remain unchanged. |

GitHub backlog mutations use mocked remote responses; no live issues or PRs were changed. Real swarmd tests cover task persistence, MCP reads/errors, and empty attempt exports; nonempty attempt-reference mapping is fixture-contract coverage. At that milestone, managed attempt finalization was refused because its pinned runtime MCP did not expose a supported leased-attempt mutation API. The later [fenced-finalization follow-up](roadmap.md#fenced-managed-finalization-follow-up) extends that boundary. No user hook was installed, no live runtime state was touched, and no hosted CI or submitted GitHub review is claimed.

## Fenced managed-finalization follow-up

Dated 2026-10-18. Eight real process/SQLite/MCP tests pass against isolated swarmd and test-only attempt-fixture binaries. They cover persistence and reads, successful handoff creation, failed outcomes, duplicate finalization, wrong credentials/revision/outcome, changed artifacts, expired/revoked ownership and token-free CLI output. Every test uses a fresh temporary database. These results do not establish live queue mutations, external review approval or hosted CI success. Repo-delivery quality passed 137 configured tests without skips, with 94% overall coverage and 100% coverage for `runtime_finalize`. Independent review approved both implementations without remaining findings and confirmed unchanged reviewed source digests and the exact companion CI pin. swarmd vet, targeted/full race tests, quality-fast and release-check passed; the release gate included real Docker integration, 60 DSPy tests and a final build. The final eight-test runtime rerun passed in 2.483 seconds against that binary. The companion is swarmd commit `f7a515320f4e939e608b13c15dca8b7f4aa25b88`, proposed in [PR #50](https://github.com/ArmanAvanesyan/swarmd/pull/50). Hosted CI remains pending. Details and limitations are recorded in the [follow-up review record](reviews/fenced-finalization.md).
