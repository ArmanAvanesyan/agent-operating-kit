---
name: repo-delivery-checkpoint-port
description: Port selected useful local changes from a verified checkpoint onto a pinned baseline while retaining source work and reporting omitted or superseded changes.
---

# Port selected checkpoint content

Read the [delivery guide](../../docs/delivery.md) and [preservation boundaries](../../docs/design.md#preservation-boundaries). Verify the selected checkpoint before using its contents. Inspect source and target history and compare actual file content against the chosen baseline; commit reachability alone cannot detect squash-merged or superseded edits.

Create a port plan for a new destination and branch pinned to the intended baseline. Review each selected path, its recorded working content, and its baseline relationship. Keep the selection narrow; report equivalent, omitted, and conflicting changes instead of silently copying an entire historical checkout. A working-content port does not reproduce the original staged/unstaged split or restore a complete runtime environment.

Apply only an authorized plan through the bundled [helper](../../scripts/repo-delivery). The source repository and checkpoint remain intact. Treat stale plans or changed destination/source facts as a reconciliation step; generate a new plan instead of bypassing checks or overwriting existing content.

After porting, inspect the diff and implement any adaptations with the host's coding tools. Follow the [issue-to-PR workflow](../repo-delivery-issue-to-pr/SKILL.md) for scope, source-bound gates, independent review, and authorized publication. Completion reports must name deliberately omitted work and any unsupported recovery material.
