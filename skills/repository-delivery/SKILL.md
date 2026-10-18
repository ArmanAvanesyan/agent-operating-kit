---
name: repository-delivery
description: Preserve and deliver repository changes using snapshot-bound evidence and explicit authority.
---

Use the canonical [workflow index](../../packs/repository-delivery/README.md).
Start with `aok delivery doctor --json`. If using this installed plugin without a CLI,
run `python3 -I <plugin-root>/packs/repository-delivery/scripts/repo-delivery --help`.
Resolve paths relative to this skill, never a sibling checkout or downloaded code.

Select a canonical workflow from `../../packs/repository-delivery/skills/`:
`repo-delivery-setup`, `repo-delivery-workspace-inspect`, `repo-delivery-checkpoint-port`,
`repo-delivery-issue-to-pr`, `repo-delivery-pr-converge`,
`repo-delivery-workspace-reconcile`, `repo-delivery-architecture-to-backlog`,
`repo-delivery-real-integration-proof`.
Read its SKILL.md and referenced resources before acting. Installation does not
register hooks or authorize publication, merging, cleanup, or runtime finalization.
