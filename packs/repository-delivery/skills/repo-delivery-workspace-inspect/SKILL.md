---
name: repo-delivery-workspace-inspect
description: Inventory a Git workspace, preserve selected local changes in a verified checkpoint, or compare local content against a pinned revision before consolidation.
---

# Inspect and preserve a workspace

Read the [usage guide](../../docs/README.md) and use the bundled [helper](../../scripts/repo-delivery). Keep the user's chosen repository and scope; a request to inspect does not imply permission to merge, clean, reset, or delete work.

Run `inspect --repo PATH` first. Treat staged, unstaged, untracked, ignored, detached, and unmerged work as different preservation concerns. Identify other worktrees from inventory; a checkpoint of one worktree does not certify every other worktree's uncommitted content.

For an authorized checkpoint, create a new destination outside the source repository and its Git metadata. Select ignored paths explicitly when needed, then run `checkpoint verify PATH`. Retain source work and existing checkpoints. Hash verification establishes integrity against recorded digests, not authenticity or application-consistent live database backup; read the [preservation boundaries](../../docs/design.md#preservation-boundaries) before interpreting coverage.

Use `compare --repo PATH --baseline REF` to compare current bytes against the resolved baseline, including untracked paths that may already be committed there. Do not infer that a historical branch is useless solely because its commits were squash-merged. Report equivalent content, useful differences, and unsupported comparisons with evidence.

Inspection collects local evidence. Continue through [checkpoint-port](../repo-delivery-checkpoint-port/SKILL.md) or [workspace reconciliation](../repo-delivery-workspace-reconcile/SKILL.md) only when the user requests those actions. A complete checkpoint is preservation evidence, not proof that a port, gate, review, publication, or cleanup has occurred.
