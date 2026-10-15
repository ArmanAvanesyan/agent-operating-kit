# Research on agent development workflows

The reviewed sources support combining observable acceptance, resumable planning,
bounded execution and evidence-driven iteration. They describe different mechanisms:
a plan stores the intended route, a Goal defines completion, a harness enables execution,
and a campaign coordinates many related units. The proposed AOK taxonomy is in
[catalog.md](catalog.md); these are its design inputs, not vendor-defined workflow IDs.

Reviewed 2026-10-15. Exact URLs, publishers, source types, supported claims and limitations
are in [sources.json](sources.json). The sources were read as official documentation,
recipes or engineering reports. No vendor experiments or runtime examples were reproduced.

## OpenAI findings

### O1

[Using PLANS.md for multi-hour problem solving](https://developers.openai.com/cookbook/articles/codex_exec_plans)
proposes a repository-defined ExecPlan for complex implementation. The living document
contains intent, progress, discoveries, decisions, context, concrete steps, acceptance
and recovery guidance. It should allow work to resume without relying on prior chat.
Instructions explain when to use the convention; the filename is not an execution engine.

The page is now marked archived. Its planning structure is useful as an example;
its historical model/API recommendation is not a current baseline.

AOK interpretation: offer a plan template for complex tasks, with links to authoritative
acceptance and evidence. Small tasks should not acquire a multi-page plan by default.

### O2

[Harness engineering](https://openai.com/index/harness-engineering/) describes an
agent-first engineering experiment. Useful practices include concise repository navigation,
linked knowledge, enforceable architecture, observable application behavior, maintained
plans and recurring cleanup. The repository serves as an accessible knowledge system.

AOK interpretation: repository readiness is an engineering workflow of its own. Define
where agents find commands, boundaries, owners and diagnostics; verify those affordances.
The reported throughput and autonomy belong to the described team and are not portable
performance guarantees.

### O3

[Custom Code Review rules for Codex](https://developers.openai.com/blog/custom-code-review-rules-for-codex)
describes scoped review guidance in `AGENTS.md`. Good rules name a consequential invariant,
explain its context and identify a safe alternative. Evaluate a violation, a safe exception
and an unrelated change to check both detection and restraint.

AOK interpretation: give a review rule an owner, scope, rationale and evaluation cases.
Keep formatting and other deterministic requirements in CI. Automated review augments
hard checks and required approvals; instructions alone are not enforcement.

### O4

[Maintaining software with the GPT-6 family](https://developers.openai.com/blog/maintaining-software-with-the-gpt-6-family)
reports evidence-led dead-code and experiment cleanup. The process investigates actual
use, operational intent and configuration, proposes small changes, prioritizes benefit,
and expands after review. Reviewer feedback informs later prompts.

AOK interpretation: separate candidate discovery from approved changes; classify confidence
and preserve emergency controls. Measure value where possible instead of optimizing deletion
counts or PR volume. Reported model/cost comparisons and maintenance gains were not reproduced
here and should not become fixed routing rules for all repositories.

### O5

[Migrate a Legacy Codebase with Sandbox Agents](https://developers.openai.com/cookbook/examples/agents_sdk/sandboxed-code-migration/sandboxed_code_migration_agent?site_locale=en)
uses trusted host-side orchestration with isolated execution workspaces. Scoped tasks
receive code, run checks and return patches, reports and audit artifacts. Host-side
validation checks returned outputs before use. The example separates sandbox-provider
creation from the migration procedure.

AOK interpretation: describe the unit workspace, allowed inputs, output validation and
cleanup contract. This is an execution profile to map to an external implementation,
not evidence that AOK already supplies isolation. Host-side credentials and policy still
need least-privilege handling; host placement alone does not establish safety.

### O6

[Iterating Development Workflows with Codex](https://developers.openai.com/cookbook/examples/codex/iterating-development-workflows-with-codex?site_locale=en)
organizes goals, plans, prompts, build phases, material context, build evidence and review
artifacts with distinct ownership. It then uses observed workflow friction to improve
instructions, references, validators or skills and forward-test the improvement.

The extra files and layout are optional conventions. Its example approval policy must
be reconciled with actual user authorization and project policy rather than copied as
a universal request to reconfirm every edit.

AOK interpretation: prefer improving the existing owner of a procedure; add a new skill
only for a distinct reusable responsibility. Keep plans and claims separate from observations.

### O7

[Using Goals in Codex](https://developers.openai.com/cookbook/examples/codex/using_goals_in_codex)
describes persistent thread-scoped objectives, evidence-based completion, continuation
controls and budget accounting. A useful objective defines outcome, verification,
constraints, boundaries, iteration policy and a defensible stopping condition.

AOK interpretation: store portable Goal intent separately from the target's thread state.
Do not equate a repository goal document with native lifecycle persistence or assume a
Goal provides scheduling or additional permissions. Native lifecycle mapping requires
an adapter and compatibility testing.

## Anthropic findings

### A1

[Claude Code best practices](https://code.claude.com/docs/en/best-practices) recommends
specific context and executable verification. Explore and plan when the approach or impact
is uncertain; execute small clear edits directly. Evidence should show what was checked
rather than merely assert success.

AOK interpretation: right-size planning and checks to uncertainty and impact. Context
management, targeted verification and relevant examples are reusable procedure elements,
not a requirement that every task launch more agents.

### A2

[Common workflows](https://code.claude.com/docs/en/common-workflows) provides recipes
for repository understanding, locating code, debugging, refactoring, tests, documentation,
PR preparation, resumed sessions, worktrees and scripted use.

AOK interpretation: classify workflow types by engineering intent and observable output.
Keep vendor command syntax in adapters and separate environment isolation from logical
task decomposition. These examples do not establish AOK target execution support.

### A3

[Effective harnesses for long-running agents](https://www.anthropic.com/engineering/effective-harnesses-for-long-running-agents)
uses an initializer and incremental coding sessions with startup instructions, a feature
inventory, progress notes and Git history. Each new session recovers current state and
checks basic functionality before extending the system.

AOK interpretation: define a restart protocol and preserve task completion evidence.
Finishing a session is not finishing the project. This is an experimental pattern,
initially centered on full-stack applications, rather than proof that a particular
multi-agent architecture is always superior.

### A4

[Harness design for long-running application development](https://www.anthropic.com/engineering/harness-design-long-running-apps)
explores separate generation and evaluation, including interaction with the live app.
Evaluator calibration matters, and model improvements can make some scaffolding unnecessary.

AOK interpretation: test whether an evaluator or decomposition stage materially improves
outcomes. Assess complete interactions rather than screenshots alone. Compare overhead,
missed behavior and recovery as well as apparent polish. A separate model is not an
infallible or statistically independent judge.

### A5

[Building effective agents](https://www.anthropic.com/engineering/building-effective-agents)
distinguishes predictable workflows from more adaptive agent execution. It presents
chaining, routing, parallelization, orchestrator/worker and evaluator patterns, while
recommending the simplest solution that meets the need.

AOK interpretation: workflow recipes should declare who chooses the next step. A fixed
pipeline and an adaptive agent may use the same task contract with different control logic.
More agents and more framework layers are not success criteria.

### A6

[Effective context engineering for AI agents](https://www.anthropic.com/engineering/effective-context-engineering-for-ai-agents)
treats context as a limited resource. Relevant retrieval, compaction, structured notes and
appropriate context separation can preserve useful information through long tasks.

AOK interpretation: use concise navigation and link canonical artifacts. Capture only
material decisions, constraints and unresolved matters. Summaries and memory are aids to
recovery; source files and observed evidence remain authoritative for current behavior.

### A7

[Keep Claude working toward a goal](https://code.claude.com/docs/en/goal) describes a
session-scoped `/goal` with a separate evaluator after turns. It has documented conditions
for automatic clearing, including completion, judged impossibility and certain errors.
A Goal does not change permission mode. It is distinct from interval loops and persistent
Stop hooks.

AOK interpretation: model the desired objective portably but retain native lifecycle
semantics. Do not project Codex thread persistence or lifecycle controls onto Claude.
A cleared native objective should not silently mark an AOK assignment complete without evidence.

### A8

[Dynamic workflows](https://code.claude.com/docs/en/workflows) codify orchestration in a
JavaScript script executed by a runtime. Documentation distinguishes scripted next-step
control and intermediate state from skills, subagents and agent teams, and describes
run management and resumption behavior.

AOK interpretation: declare scripted orchestration as a capability with vendor-specific
availability and limits. Do not treat a reusable skill as a durable scheduler. Multiple
contexts do not isolate mutable files, databases, ports or external resources.

### A9

[How Anthropic runs large-scale code migrations](https://claude.com/resources/articles/ai-code-migration)
emphasizes a judge that compares old and new behavior, translation rules stress-tested
before scale, mechanical work inventories, repair queues and investigation of repeated
failure classes. Shared expensive operations can be centralized rather than repeated by
all workers.

AOK interpretation: distinguish structural translation from redesign. A generated file is
unit progress, not sufficient campaign acceptance. Require parity, integration and
nonfunctional evidence, and use checkpointed units to recover incomplete campaigns.

## Cross-source synthesis

The following are AOK design conclusions, not additional vendor capability claims.

| Design conclusion | Supporting source IDs | Proposed AOK owner |
|---|---|---|
| Observable acceptance should precede adaptive execution | O1, O7, A1, A9 | Task and Goal contracts |
| Plans, evidence and runtime state need different ownership | O1, O6, O7, A3 | Artifact model and runtime bridge |
| Repository navigation and diagnostics are productively engineered | O2, A2, A6 | Readiness workflows |
| Small changes and representative pilots reduce scaling mistakes | O4, O5, A9 | Maintenance and migration recipes |
| Review should test restraint as well as detection | O3, A4 | Review-rule evaluation |
| Independent assessment should earn its overhead | A4, A5 | Execution strategy and evaluation suite |
| Tool lifecycle semantics require explicit adapters | O7, A7, A8 | Target capability contracts |
| Improve the owning procedure before creating new abstractions | O6, A5 | Pack and workflow maintenance |

## Applicability boundaries

The catalog covers source-backed agent engineering patterns and proposed applications to
AOK. It does not claim exhaustive coverage of either vendor's products or every software
delivery discipline. Specialized domains can add recipes while retaining the same axes.

Repository claims are grounded in the baseline files linked in [README.md](README.md).
The existing methodology artifact schema has methodology-specific fields and constrained
output paths; it is not a general execution-evidence schema. The existing hook schema is
also narrower than all native event surfaces. New artifact and gate concepts need design
and implementation rather than being declared operational through documentation.
