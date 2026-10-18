---
name: repo-delivery-workspace-reconcile
description: Plan and apply authorized cleanup of merged local branches and unused worktrees while retaining dirty, ignored, unmerged work and durable checkpoints.
---

# Reconcile repository workspaces

Read the [delivery guide](../../docs/delivery.md) and use the bundled [helper](../../scripts/repo-delivery) to inventory branches and worktrees. Inspect retained checkpoints separately; never propose them as ordinary cleanup targets. Classify work by content and history, including squash merges, and distinguish source preservation from a branch's merge status.

Produce a concrete cleanup plan naming each candidate and the evidence for removing it. Dirty, ignored-content, unmerged, active, and otherwise unsupported worktrees must remain retained. A useful unmerged change belongs in a [checkpoint-port](../repo-delivery-checkpoint-port/SKILL.md) plan, not forced branch deletion. Plans must be pinned to observed repository facts and rejected when those facts change.

Apply only the user's authorized cleanup actions and verify the observed result. Do not treat a queued archive request as completion, manually race managed-worktree removal, or bypass ownership held by the host. The local helper does not remove remote branches; inspect remote merge state and use existing host tools only when that separate cleanup is authorized.

Update a local main branch only through an authorized, verified fast-forward that preserves its worktree. Report what was removed, what remains useful, and the verified state of preserved work and checkpoints. Do not reset or discard retained edits to make reconciliation appear complete.
