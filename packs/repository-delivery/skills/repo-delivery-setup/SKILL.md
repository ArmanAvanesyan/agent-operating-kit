---
name: repo-delivery-setup
description: Orient a repository to repo-delivery's local inspection and preservation helpers without changing its configuration or connecting remote services.
---

# Set up repo delivery

Use the bundled [CLI](../../scripts/repo-delivery) from the plugin directory; do not assume it is on PATH. Read the [usage guide](../../docs/README.md) for the supported commands and the [design](../../docs/design.md) for ownership boundaries.

Identify the user's target repository and its applicable instructions. Run `inspect --repo PATH` with the helper and summarize the baseline, local edits, branches, and worktrees. Discover repository-specific gates from its own configuration; do not substitute a generic gate profile or modify its instructions.

If preservation is part of the request, choose a private checkpoint destination outside the source repository and run `checkpoint create --repo PATH --output PATH`, followed by `checkpoint verify PATH`. Ignored files are excluded unless individually selected with repeated `--include-ignored PATH`. Explain the resulting coverage, exclusions, and unsupported layouts before representing the checkpoint as complete.

Setup does not install hooks, change host configuration, store credentials, or create runtime tasks. For delivery, route to [issue-to-PR](../repo-delivery-issue-to-pr/SKILL.md), [checkpoint-port](../repo-delivery-checkpoint-port/SKILL.md), [PR convergence](../repo-delivery-pr-converge/SKILL.md), or [workspace reconciliation](../repo-delivery-workspace-reconcile/SKILL.md) within the user's authorized scope. GitHub operations require the host's existing optional GitHub CLI connection.
