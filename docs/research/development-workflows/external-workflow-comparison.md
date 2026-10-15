# External workflow approaches and proposed AOK additions

Reviewed 2026-10-15 against AOK baseline
`ea04d30ed5b27ab302f2f42a596adec9d9b68a83`. This comparison saves the seven requested
references and maps useful patterns to the existing workflow proposal. Exact URLs,
repository revisions, selected files and limitations are in
[external-comparison-sources.json](external-comparison-sources.json).

Status: **research and proposed additions**. External repositories contain implementations
or instructions of their own; those are not existing AOK capabilities. No framework,
workflow, scanner, CrewAI system or GitHub automation was installed or executed.

## Comparison

| Source | Useful contribution | Proposed AOK application |
|---|---|---|
| MatrixFounder Agentic-development | Incremental reverse engineering, artifact ownership and staged review | Bounded discovery, component summaries, documentation reconciliation and planning acceptance |
| SuperiorByteWorks agent-project | Review scope, local/CI consistency, tiered specialist review and memory governance | Explicit review inputs, remediation traceability and scoped accepted exceptions |
| Agent-ready repository article | Canonical commands, entrypoints and explicit boundaries | Observable project-readiness criteria |
| Azure with AJ | Introduction to event-triggered automation and controlled outputs | Orientation for an optional integration; verify exact behavior against primary docs |
| GitHub Next Agentics | Concrete CI diagnosis, contribution checking, research and maintenance workflows | Optional GitHub automation projection and a bounded pilot |
| OpenAI harness engineering | Navigable knowledge, enforceable architecture and observable application behavior | Context, verification and feedback readiness |
| QuantumBlack | Structured artifacts, traceability, deterministic transitions and bounded evaluations | Workflow/artifact contracts with a clear external-runtime boundary |

## Source findings

### C1 MatrixFounder Agentic-development

The [repository](https://github.com/MatrixFounder/Agentic-development) offers a
[reverse-engineering skill](https://github.com/MatrixFounder/Agentic-development/blob/09765e1d1379bd70eae996fe87e483ce73cf96fa/.agent/skills/skill-reverse-engineering/SKILL.md)
that scans structure, inspects selected components, synthesizes architecture and identifies
drift while preserving existing architectural rationale. Its
[source-of-truth map](https://github.com/MatrixFounder/Agentic-development/blob/09765e1d1379bd70eae996fe87e483ce73cf96fa/System/Docs/SOURCE_OF_TRUTH.md)
names content owners and acknowledges that the fallback dispatcher's LLM loop is not wired.

Adopt the discovery and ownership patterns selectively. Keep Stub-First and adversarial
review as optional recipes. Do not mistake written workflow instructions for observed
runtime enforcement.

A concrete implementation caveat: the inspected
[scanner](https://github.com/MatrixFounder/Agentic-development/blob/09765e1d1379bd70eae996fe87e483ce73cf96fa/.agent/skills/skill-reverse-engineering/scripts/scan_structure.py)
accepts `--depth` but does not enforce the argument and skips hidden directories. An AOK
inventory needs bounded traversal and deliberate inclusion of agent/CI configuration.
This observation is static source inspection, not a scanner execution result.

### C2 SuperiorByteWorks agent-project

The [project](https://github.com/SuperiorByteWorks-LLC/agent-project) describes tiered local/CI
review. Its [scope ADR](https://github.com/SuperiorByteWorks-LLC/agent-project/blob/54b2b1e6616c8bcffd5cd082ecfb616db97e7185/.crewai/adr/ADR-004-review-scope-contract-and-tiering.md)
defines tier, diff strategy, base/head references, head SHA and merge base. That makes the
reviewed surface inspectable across branch and working-tree scenarios.

Its [memory governance](https://github.com/SuperiorByteWorks-LLC/agent-project/blob/54b2b1e6616c8bcffd5cd082ecfb616db97e7185/agentic/adr/ADR-008-persistent-review-memory-governance.md)
recognizes stale or broad suppressions as a risk. Selected specialist registry code also
shows scope-based routing and standardized outputs.

Adopt scope contracts and evidence-linked findings; choose review depth by impact rather
than a fixed agent count. Accepted exceptions should have an owner, rationale, narrow
scope and review date. Retain AOK's chosen work-system authority rather than introducing
competing Markdown issues, PRs and boards. The CrewAI system was not run here.

### C3 Agent-ready repository structure

The [article](https://medium.com/@huseyinkaplandev/agent-ready-repo-structure-2026-90af2ac8aed2)
advocates concise instructions, canonical commands, clear entrypoints, boundaries and links
to architecture/runbooks. Turn these into discoverable, verifiable project facts rather
than requiring its exact directory layout. Its broad compatibility and productivity
claims are not treated as tested guarantees.

AOK already generates generic operating files. Project-specific command discovery and
observed readiness remain proposed additions, not capabilities established by generation.

### C4 Azure with AJ

The [explainer](https://azurewithaj.com/agentic-workflows-reimagining-automation/)
introduces Markdown-defined automation with triggers, tools, limits and controlled outputs.
Use it for orientation; prefer current primary documentation for execution and security
semantics. Do not copy its examples as a tested adapter contract.

In particular, controlled outputs do not inherently mean that a human approves each action.
The primary-source clarification is recorded under C8.

### C5 GitHub Next Agentics

The [sample repository](https://github.com/githubnext/agentics) contains reusable workflows.
The inspected [CI Doctor](https://github.com/githubnext/agentics/blob/b4e175bab6674279e0fe158f7dc877b77bc9d92f/workflows/ci-doctor.md)
classifies failures, records evidence/confidence and consolidates duplicate reports.
The [contribution checker](https://github.com/githubnext/agentics/blob/b4e175bab6674279e0fe158f7dc877b77bc9d92f/workflows/contribution-guidelines-checker.md)
bounds comments/labels and distinguishes contribution compliance from code-quality review.

A proposed AOK projection could map neutral intent to GitHub Agentic Workflows compilation
and execution, then collect configured outputs and evidence. It would be separate from
GitHub Projects bindings and work-item classification. Start with artifact-only diagnosis;
publication requires an explicitly configured, authorized delivery capability. Examples
were not compiled or executed in this review.

### C6 OpenAI harness engineering

The [engineering report](https://openai.com/index/harness-engineering/) supports concise
navigation, repository knowledge, application observability and mechanical architecture
checks. It strengthens three proposed readiness dimensions: context, verification and
feedback. Successful dependency installation alone does not establish any of them.

Use concrete project invariants where established, such as dependency boundaries. AOK
can define their intent and projections; the project owns the actual rules. Reported
throughput and autonomy belong to the case study and were not reproduced here.

### C7 QuantumBlack

The [article](https://medium.com/quantumblack/agentic-workflows-for-software-development-dc8e64f4a79d)
describes deterministic outer orchestration with bounded agent execution, structured
artifacts, dependencies and evaluations. Persistent project context is separated from
feature-specific specifications. Structural checks precede judgment-based evaluation,
and remediation loops are bounded.

Adopt artifact contracts and requirement-to-evidence traceability. Keep consequential
transitions explicit while allowing adaptive investigation inside tasks. The article's
phase sequence is not mandatory for small edits; artifact approval must remain separate
from task completion. No implementation from the account was independently exercised.

### C8 Primary clarification on safe outputs

GitHub's [safe-output reference](https://github.github.com/gh-aw/reference/safe-outputs/)
describes read-only agent execution requesting structured actions that separate,
permission-controlled jobs process. Configured GitHub writes can occur automatically;
this is not an automatic human-approval boundary.

Any AOK adapter must inspect the selected compiler/version's defaults, allowed outputs,
permissions and generated jobs. An artifact-only pilot must explicitly avoid publishing
side effects; omission of configuration is not sufficient evidence of that property.
No GitHub Agentic Workflows integration exists in AOK merely because these examples are linked.

## Existing AOK capabilities and proposed additions

| Area | Existing at inspected AOK baseline | Proposed addition |
|---|---|---|
| Exploration/readiness | Saved topology and task/profile design | Implement and evaluate the two proposed skills |
| Bootstrap | Generic project initialization, doctor/validation and Codex helpers | Project-specific inventory and observed application readiness |
| Review | `code-review` and QA guidance; task roles and verification fields | Explicit scope contract, finding identity and remediation verification |
| Artifacts | Methodology artifacts and task-packet contracts | General workflow artifact contracts and traceability |
| Packs | Shipping, guardrails, rollout and conflict content | References/recipes extending those owners without duplication |
| Work systems | Scaffolded GitHub/Azure discovery and local bindings | No new provisioning implied; optional automation remains separate |
| Execution | External runtime boundary documented | Tested GitHub automation projection or coordination-runtime bridge |

## Recommended backlog

All entries below are proposals. They refine the existing plan rather than creating new
catalog types, task statuses or active methodology selections.

| ID | Priority | Addition | Owner | Acceptance |
|---|---|---|---|---|
| X01 | P0 | Bounded inventory including agent/CI configuration | `project-exploration` | Depth/exclusion behavior checked; hidden relevant configuration included; no unrelated traversal |
| X02 | P0 | Verified commands and three readiness dimensions | `environment-readiness` | Commands include directory/prerequisites; context, verification and feedback readiness have separate evidence |
| X03 | P0 | Review scope and revision contract | Review/remediation workflows | Uncommitted, commit and branch scope unambiguous; findings bound to reviewed revision |
| X04 | P1 | Specification, architecture and breakdown checks | Planning workflows | Required structure/references valid; acceptance testable; dependencies reconciled |
| X05 | P1 | Requirement-to-evidence traceability | Workflow/artifact contracts | Acceptance links to tasks, changes and current evidence without duplicate state authority |
| X06 | P1 | Documentation drift with decision preservation | Documentation workflows | Code/docs discrepancies evidenced; prior rationale retained or explicitly superseded |
| X07 | P2 | CI Doctor integration pilot | Optional GitHub projection | Revision-linked, deduplicated diagnosis; no publishing side effects in artifact-only mode |
| X08 | P2 | Governed review exceptions | Review policy | Scope, owner, rationale and review date; evaluate stale/broad suppression cases |

## Adoption boundaries

Adapt patterns before copying source. If code or templates are imported later, inspect the
exact files, dependencies and applicable license/notice requirements at the pinned revision.
No source code was imported by this comparison. Do not require a fixed agent team, a new
framework dependency or a second work-management authority to adopt these improvements.

The immediate order remains exploration and readiness, followed by review scope and
remediation. Contract validation and optional runtime integration follow the
[phased implementation plan](implementation-plan.md).
