> Imported historical upstream record. Its validation claims describe the pinned source, not this AOK integration. Current evidence is in AOK docs/evidence/repository-delivery-validation.md.

# Implementation roadmap

Updated 2026-10-18. The [design](design.md) defines ownership and preservation boundaries. The [usage guide](README.md) describes the current interface.

## Milestone 1: local preservation foundation

Completed locally on 2026-10-18. See the [validation record](validation.md) for executed checks and remaining remote validation.

Scope: a standalone plugin manifest, setup and inspection skills, and a local Python CLI. This milestone deliberately excludes remote delivery actions.

| Capability | Acceptance criteria |
| --- | --- |
| Repository inventory | Resolve the actual repository, HEAD, branches, worktrees, and local status without changing source state. Handle dirty, detached, and unborn repositories. |
| Durable checkpoints | Capture selected working content, staged and unstaged changes, indexed blobs and index data, and reachable Git history. Explicitly select ignored content; preserve symlinks and file modes; refuse unsafe destinations and unsupported layouts. |
| Checkpoint verification | Validate versioned structure, artifact paths, and content hashes. Reject missing, changed, or unexpected artifacts and incomplete checkpoints. |
| Baseline comparison | Pin the baseline revision and classify actual local content, including untracked paths already represented at the baseline. |
| Evidence contracts | Save versioned schemas for implemented JSON records under `config/schemas/`; future handoff fields remain design contracts, without claimed execution support. |
| Regression coverage | Exercise real temporary Git repositories, dirty staged/unstaged combinations, unmerged history, binary data, symlinks, corruption, and preservation invariants. |

Completion requires passing the repository's Python gates, runnable documented examples, and confirmation that the source repository and retained historical checkpoints were untouched. Test fixtures must not use the user's live dirty repositories as mutation targets.

## Milestone 2: delivery workflows

Completed locally on 2026-10-18 after independent review, 72 passing tests, and package checks. See the [validation record](validation.md#milestone-2).

Depends on milestone 1's preservation and evidence contracts. The [delivery guide](delivery.md) describes the implemented helper and host boundaries.

| Workflow | Acceptance criteria |
| --- | --- |
| Issue to PR | Read the issue and repository rules, define scope, implement in an isolated checkout, run required gates, obtain independent review when required, and open a PR only when authorized. |
| Checkpoint port | Select useful unmerged work by content comparison, preserve the source checkpoint, port onto the chosen baseline, and identify superseded or deliberately omitted changes. |
| PR convergence | Reconcile CI and submitted reviews, address findings, rerun affected gates, and merge only with required checks and existing user authority. |
| Workspace reconciliation | Produce a concrete cleanup plan distinguishing merged work, useful unmerged work, dirty worktrees, and durable checkpoints. Apply only authorized cleanup and verify completion. |
| Snapshot evidence | Bind gates and reviews to exact source snapshots, invalidate stale evidence after changes, and resume without replaying already-completed remote actions. |

Git and GitHub adapters preserve repository ownership of gates and remote ownership of CI/review facts. The plugin coordinates the workflow; host tools implement code, commit, and push. GitHub mutations require existing host authentication and explicit action authority. Boundary tests use mocked GitHub responses; live remote mutation is not part of this milestone's local validation.

Local cleanup uses commit ancestry and retains squash-merged candidates when ancestry is inconclusive. It does not remove remote branches, checkpoints, or reset main. Ports transfer selected conflict-free working content into a new checkout, preserving the archive rather than replaying its historical index state.

## Milestone 3: planning and optional integrations

Completed locally on 2026-10-18 after independent review, 122 passing tests with no skips, and package checks. See the [validation record](validation.md#milestone-3) and [integration guide](integrations.md). Supported runtime reads and exported references shipped in milestone 3. The subsequent [fenced-finalization follow-up](#fenced-managed-finalization-follow-up) adds supported managed finalization, with no legacy fallback.

Depends on milestone 2's reconciled delivery state and evidence receipts.

| Capability | Acceptance criteria |
| --- | --- |
| Architecture to backlog | Compare implemented behavior, research, ADRs, and roadmap; reuse existing issues and add missing work with dependencies and observable acceptance criteria. |
| Real integration proof | Distinguish real, mock, and mixed evidence; record service versions and environment; report environment blockers without substituting mock results for required real tests. |
| AOK adapter | Consume portable policy and work contracts without taking ownership of their definition. |
| swarmd adapter | Read tasks through supported MCP and validate exported durable attempt references; maintain no parallel task-state authority. The milestone 3 baseline refused managed finalization; the follow-up below supplies the supported leased-attempt API. |
| Optional hooks | Provide separately installed host-specific checks with explicit configuration and tested failure behavior. Keep the portable plugin independent of hooks. |

A hosted service, workflow engine, embedded scheduler, credential store, and replacement for GitHub's review model are outside this roadmap.

## Fenced managed-finalization follow-up

Dated 2026-10-18. Extends milestone 3 against swarmd's `task.finalize` API; see the [integration boundary](integrations.md#swarmd-runtime-references) and [review record](reviews/fenced-finalization.md).

Acceptance requires capability discovery and schema validation; exact attempt/task/worker/revision/tree/environment and expected-outcome matching; a separately supplied private lease credential; and a single fenced call with no legacy fallback or ambiguous-response retry. Tests must cover both accepted outcomes and stale, revoked, expired or mismatched ownership/evidence using an actual swarmd binary with fresh temporary databases. Independent review and repository gates remain required before publication. This adds no producer lifecycle or recovery API.
