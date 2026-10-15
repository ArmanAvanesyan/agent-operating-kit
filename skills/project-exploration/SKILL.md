---
name: project-exploration
description: Explore an unfamiliar repository, project, or subsystem; map architecture, development commands, dependencies, and verification prerequisites with source-backed evidence. Use before scoped development when project context is missing, or to reconcile stale architecture documentation.
---

# Project Exploration

Produce enough reliable context to begin the requested task without repeating discovery.
Explore the relevant scope; do not automatically scan every repository or install a stack.

1. Inspect applicable instructions, repository roots, branch, changes and worktree boundaries.
2. Read existing architecture and development guidance. Identify relevant manifests,
   entrypoints, interfaces, consumers, commands and verification prerequisites.
3. Use `scripts/inventory.py <root>` for bounded metadata. It deliberately includes agent/CI
   directories, skips symlinks and reports exclusions. It is not a complete dependency graph.
4. Use `rg` for text discovery. For syntax-aware questions, use the optional Rust backends
   through `scripts/structure.py`; read [structural analysis](references/structural-analysis.md).
5. Inspect representative source and follow consequential relationships. Distinguish
   syntactic references, resolved relationships and observed runtime behavior.
6. Deliver a project map, command matrix, evidence/unknowns and next-task handoff. Reuse
   canonical docs; the optional [report template](assets/exploration-report.md) is a starting point.

For multi-repository scope and evidence ownership, read
[exploration procedure](references/exploration-procedure.md).

Exploration is read-oriented. Do not run package scripts or setup merely to discover their
requirements. Preserve existing changes; writing a requested report does not authorize
application/configuration changes. Environment preparation is a separate outcome: use the
`environment-readiness` skill if available, otherwise provide its prerequisites and blockers.

These helpers require Python 3.10+ and install nothing. Missing optional tools are coverage
limitations, not an excuse to claim no matching structure exists. Keep secret values out
of reports and query only relevant source. A structural result is evidence, not a complete
architectural conclusion.
