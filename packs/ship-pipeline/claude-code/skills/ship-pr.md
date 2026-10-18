---
name: ship-pr
description: Compatibility entrypoint for preservation-aware repository delivery.
---

Use AOK's `repository-delivery` capability: `aok delivery doctor --json`, then
read the installed `repo-delivery-issue-to-pr` and `repo-delivery-pr-converge`
canonical guides. For a rendered bundle, the complete support payload is under
`sources/packs/repository-delivery/`; run its `scripts/repo-delivery` with Python
3.11+ and isolated mode (`-I`). If neither installation exists, stop and install
AOK's repository-delivery pack. Never fetch helpers at runtime.

Inspect and preserve staged, unstaged, untracked and selected ignored work before
editing or creating isolated delivery checkouts. Select only authorized files for
staging. Discover project gates; never assume generic formatting or test commands.
Bind gates and independent review to the exact snapshot; invalidate them after
changes. Agent review is distinct from submitted GitHub approval. Require explicit
user authority for push, PR creation, merge, branch deletion, hooks and cleanup.
A green CI run does not grant authority. No unconditional merge or deletion.
Keep `fix-ci` and engineering guardrails as complementary guidance.
