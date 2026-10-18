# Repo delivery design

Design date: 2026-10-18. See the [implementation roadmap](roadmap.md) for shipped scope and [usage guide](README.md) for commands.

## Problem and scope

Repeated repository delivery work needs a reliable handoff between discovery, preservation, isolated implementation, verification, independent review, and authorized publication. Dirty worktrees, staged changes, historical checkpoints, and unmerged branches must remain recoverable while useful work moves forward. A successful gate or review applies to the source snapshot that produced it; an old passing result cannot certify later edits.

`repo-delivery` packages reusable agent skills and deterministic local helpers. The local foundation provides inspection, checkpoint creation and verification, and comparison against a resolved Git revision. [Delivery workflows](delivery.md) add isolated checkout and port planning, source-bound evidence, optional GitHub adapters, and authorized local cleanup. The host still performs implementation, commits, and pushes; installing this package does not add a daemon, scheduler, hooks, or a remote GitHub connection.

## Ownership

| Component | Owns | Integration boundary |
| --- | --- | --- |
| AOK | Portable desired state, work definitions, and policy contracts | Repo delivery can consume contracts; it does not redefine AOK's model. |
| Repo delivery | Workflow guidance, local Git evidence, preservation artifacts, and delivery adapters | Executes authorized actions through existing tools and repository gates. |
| swarmd, when used | Live task state, claims, execution attempts, and runtime fencing | References durable runtime records and invokes fenced recorded-attempt finalization without creating a competing task queue. |
| GitHub | Remote issues, pull requests, submitted reviews, and CI state | Adapters reconcile remote facts rather than infer them from local receipts. |
| Repository | Its implementation conventions and gate commands | Repository instructions remain the authority for implementation and validation. |
| Host/harness | Process execution, permissions, credentials, and isolation | Plugin checks are not an operating-system security boundary. |

The plugin is useful without AOK or swarmd. Neither is a dependency of the plugin.

## Local architecture

[Skills](../skills/repo-delivery-setup/SKILL.md) interpret user intent and route to the deterministic CLI. The CLI emits versioned JSON evidence for inventory, checkpoints, comparisons, delivery contexts, gates, reviews, and reconciliation. Versioned schemas in [`config/schemas/`](../config/schemas/) describe implemented records only; the artifact's identity and content hashes bind evidence to recorded bytes, not merely a mutable branch name.

Git commands run locally. Inspection and checkpoint collection preserve the source index, refs, and worktree. Checkpoint output belongs outside the source repository and its Git metadata. Comparison resolves the requested baseline to a commit before comparing local content, including untracked paths that might already exist at that baseline. Its `snapshot_sha256` binds the comparison to the collected local content snapshot.

A checkpoint records working files, staged and unstaged patches, index data and indexed blobs, and recoverable Git history. Ignored files require explicit selection. Artifacts are private local recovery material and may contain secrets; no upload is part of milestone 1. Hash verification detects corruption against recorded digests, not a maliciously rewritten archive and manifest.

## Preservation boundaries

The collector must reject unsupported submodules, embedded repositories, and split or sparse indexes instead of claiming complete preservation. Symlink payloads must retain links without traversing targets outside the repository. Path validation must reject absolute paths, traversal, and Git metadata masquerading as source files.

Generic file copying does not provide application-consistent live database backups. Stop writers or use the application's backup mechanism before selecting that data; a valid content hash only certifies the copied bytes. Git bundles do not archive external LFS stores or hosted services. A controlled port transfers selected working content into a new checkout; it is not a general restore command and does not reproduce the historical staged/unstaged split.

## Evidence and authorization

A task context records the objective, repository, baseline revision, files in scope, preservation references, gate profile, review policy, authorized actions, and completion evidence. GitHub mutations additionally require an explicit hosted repository binding; a local clone origin does not establish that identity. It is a local handoff record, not a task scheduler. Gate receipts identify the exact tested snapshot, command, outcome, and environment. The [integration proof](integrations.md#real-integration-evidence) records service versions, environment, and explicit real, mock, or mixed boundaries alongside source-bound gate evidence.

Self-review, an independent agent review, and a submitted GitHub review are separate evidence types. `review_policy` controls local independent-context review; the separate `github_review_required` policy defaults to requiring submitted GitHub approval and can be disabled only to reflect the repository's actual policy. An independent review must use a separate reviewer context and identify its reviewed snapshot. A source change invalidates affected gate and review evidence until rechecked. Local receipts record assertions and relationships; they do not authenticate a reviewer or make untrusted gate commands safe. Store evidence outside the source snapshot.

Authorization carries forward within the user's stated task. Editing does not by itself authorize pushing, merging, or deleting retained work; explicitly authorized actions do not require repeated confirmation. Resume reconciles current local and remote facts before taking the next action. A queued archive request is not proof that a worktree has been archived.

## Extension sequence

The [roadmap](roadmap.md) introduces delivery workflows only after the preservation foundation is tested. The [optional integrations](integrations.md) keep runtime contract inspection and separately installed hooks distinct from the portable plugin. A hook can invoke deterministic checks, but cannot supply missing user authority or certify remote review state.

## Managed runtime handoff

The [swarmd adapter](integrations.md#swarmd-runtime-references) may finalize an already recorded attempt through discovered `task.finalize`. A local expected reference binds task, attempt, worker, revision, tree and environment; it does not grant current lease ownership. The opaque credential is supplied separately from a private file. swarmd remains the authority for the live lease and immutable outcome, including atomic review-handoff creation.

The adapter never substitutes legacy completion when this capability is absent, rewrites a recorded gate/outcome, or automatically retries an ambiguous write. Starting, recording and heartbeating an attempt remain producer responsibilities; recovery remains an explicit runtime operator action. Local receipts report a successful observed response without replacing runtime state or remote review decisions.
