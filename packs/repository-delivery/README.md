# Repository delivery

AOK owns this embedded capability. The Python package, eight namespaced skills,
25 evidence schemas and deterministic tests are maintained here. There is no
runtime dependency on a sibling checkout or download from repo-delivery.

Use `aok delivery --help` and `aok delivery doctor --json` from an installed AOK.
Only delivery helpers require Python 3.11+; existing Go commands stay Python-free.
Alternatively run `python3 -I scripts/repo-delivery --help` from this pack or its
complete bundled support tree. Never copy only a skill file without its resources.

Read [usage](docs/README.md), [delivery](docs/delivery.md),
[integrations](docs/integrations.md), and [design](docs/design.md).
[Import provenance](import-provenance.json) is a closed import of the upstream
snapshot and intentional integration changes, not a live upstream. Historical
upstream validation is evidence of that upstream revision, not evidence of a new
AOK run. The standalone repository is retired; this pack is the only maintained copy.

No setup/render/install action registers a hook, stores credentials, creates a
runtime producer, merges a PR or cleans a retained worktree. Those effects require
explicit user scope and supported host capabilities. Linux/macOS are the initial
supported helper platforms; security-sensitive POSIX behavior is not relaxed.

| Canonical workflow | Use |
|---|---|
| [Setup](skills/repo-delivery-setup/SKILL.md) | Discover prerequisites without installing hooks or connecting services |
| [Workspace inspection](skills/repo-delivery-workspace-inspect/SKILL.md) | Inventory HEADs, dirty content and retained worktrees |
| [Checkpoint and port](skills/repo-delivery-checkpoint-port/SKILL.md) | Private preservation, verification and selective replay |
| [Issue to PR](skills/repo-delivery-issue-to-pr/SKILL.md) | Isolated delivery, discovered gates and independent review |
| [PR convergence](skills/repo-delivery-pr-converge/SKILL.md) | Exact-head remote checks and explicitly authorized publication |
| [Reconciliation](skills/repo-delivery-workspace-reconcile/SKILL.md) | Plan cleanup while retaining useful and dirty work |
| [Architecture to backlog](skills/repo-delivery-architecture-to-backlog/SKILL.md) | Traceable work breakdown and scoped authorized issue updates |
| [Integration proof](skills/repo-delivery-real-integration-proof/SKILL.md) | Distinguish real, mock and mixed evidence; optional runtime boundaries |

The versioned records under `config/schemas/` remain delivery evidence contracts.
AOK's root `schemas/` retain portable contract ownership. Historical upstream
validation documents are provenance; the current AOK integration validation is
recorded in the kit's `docs/evidence/repository-delivery-validation.md`.
