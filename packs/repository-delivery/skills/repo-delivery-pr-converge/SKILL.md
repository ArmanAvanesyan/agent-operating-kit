---
name: repo-delivery-pr-converge
description: Reconcile a pull request’s current CI and submitted reviews, fix findings, and merge only when required checks and source-bound local evidence support an authorized merge.
---

# Converge a pull request

Read the [delivery guide](../../docs/delivery.md) and inspect the requested PR using the host's existing GitHub CLI connection. Reconcile its repository, head revision, draft state, CI, submitted reviews, review threads, and merge state before deciding the next action. An already-merged PR is complete only after its repository, head, task context, sealed gate receipt, and current source snapshot identify the requested work; do not replay merge or publication actions.

Resolve failures and findings in an isolated checkout of the intended head. Read repository instructions and gates. Reuse existing authorized actions from the task context; source changes invalidate earlier gate and review receipts. Required independent review must use a separate reviewer context and the final snapshot; local review evidence cannot substitute for a required submitted GitHub approval. Keep local `review_policy` separate from the context's `github_review_required` policy, which defaults to true. Set the latter false only when the repository's actual policy permits it; an inspection override does not change the sealed merge policy.

Implement fixes with existing coding tools, run affected and required repository gates, and record evidence against the resulting source snapshot. Commit and push only within the user's authorization, then inspect refreshed remote state. Unknown, pending, missing, failing, or stale required checks and unresolved review requirements do not establish readiness.

Use the helper to evaluate readiness and perform an explicitly authorized merge only after current remote and local evidence agree on the PR head. Remote facts can change between inspection and mutation; retain the head guard and stop on conflict. If the host cannot determine required checks or review policy, report that gap rather than asserting success. Report the merged revision or the concrete remaining blocker.
