# AOK Neutral Model

AOK separates agent workflow intent from tool-specific file formats.

## Concepts

### Task Packet

A task packet is the work contract. It carries the problem, scope, acceptance
criteria, target repo, selected roles, verification plan, lifecycle status, and
external links. It can render to Markdown for local agents or map to `swarmd`
MCP calls for runtime state.

### Role

A role is a reusable operating posture such as implementer, reviewer, QA, or
orchestrator. Roles define responsibilities, required evidence, escalation
rules, and optional callsigns for runtime assignment.

### Hook

A hook is a lifecycle check. Pre-hooks run before implementation. After-hooks
record evidence, QA, GitHub updates, PR readiness, escalation, and closeout.
Tool adapters decide whether a hook is a command, a prompt, a checklist, or a
native tool event.

### Skill

A skill is reusable task guidance or executable workflow content. The neutral
skill schema records the intent, inputs, body/source paths, and target render
hints. Tool adapters render skills as Codex skills, Claude Code slash commands,
Cursor rules, OpenCode commands, or equivalent target formats.

### Pack

A pack groups related skills, hooks, roles, scripts, and templates. Packs are
portable content units. Tool-specific files belong under an adapter directory
inside the pack.

### Methodology

A methodology selection composes exactly one flow profile with zero or more
compatible practice overlays. It changes planning, WIP, role emphasis, and hook
expectations while preserving the neutral task packet, phases, and statuses.
Methodology is project configuration, not work-system provider configuration.
Scaffolded flows may also describe a second, higher-level lifecycle with named
roles, artifacts, ceremonies, transition gates, and mappings to the neutral
task phases. That lifecycle never replaces task phase or board status.

### Artifact

An artifact is a methodology deliverable such as a business case, feature
list, design package, test result, or deployment plan. Artifact contracts
identify owners, approvers, lifecycle placement, evolution mode, and editable
template sections. Human approval remains explicit and cannot be delegated to
an agent.

FDD artifacts may provide curated source templates instead of generic section
stubs. Its feature contract defines Subject Area → Business Activity → Feature,
structured feature-name parts, bounded effort, and sequential milestone
weights. The derived progress percentage is a projection, not stored task
state.

DSDM artifacts use curated governance, requirements, architecture, planning,
delivery, review, deployment, and benefits templates. Its requirement contract
defines an optional Theme → Requirement → Acceptance Criterion hierarchy,
project/increment/timebox MoSCoW priorities, and fixed-time timebox checkpoints.
DSDM progress is expressed through delivered and accepted requirements, not
FDD milestone weights.

AUP artifacts use curated vision, model, iteration, implementation, testing,
deployment, risk, and configuration templates. Its iteration contract selects
typed requirements into a lifecycle-stage iteration, records evidence or a
justified exception for seven delivery disciplines, and derives the LCO, LCA,
IOC, or PR milestone target from the stage. No milestone percentage is stored.

### Adapter

An adapter describes how neutral AOK content renders into one tool target. It
lists supported concepts, output paths, capabilities, limitations, and any
runtime assumptions.

## Neutral Render Contract

- `pack.content.json` files hold canonical role/hook/skill/template/task packet
  content used by renderers.
- `pack.json` maps each pack to supported targets and target render mode.
- `aok render pack <pack-id> --target <target>` renders a parseable JSON contract
  for any supported target.
- `aok render all --target all` renders contract bundles for all packs/targets.

## Rendering Rules

1. Neutral schemas are the source of truth.
2. Adapter assets are generated or curated projections of neutral content.
3. If a target lacks native support for a concept, render a Markdown contract.
4. Tool-specific environment variables stay isolated under that adapter.
5. Runtime state belongs outside AOK unless it is an example or generated
   config; `swarmd` owns task, memory, and event persistence.

## Neutral to Target Mapping

- `task.packet` → `task.create` payload for `swarmd`.
- `roles.callsign` → task `assignee` in generated lifecycle contracts.
- role/task decisions → `memory.put(namespace="decision")` payload.
- lifecycle events → `event.append` payloads.

## Minimal Example

```json
{
  "name": "qa-verification",
  "kind": "skill",
  "description": "Validate completion against acceptance criteria.",
  "source": {
    "bodyPath": "skills/qa-verification/SKILL.md"
  },
  "targets": {
    "codex": {
      "renderAs": "skill"
    },
    "claude-code": {
      "renderAs": "skill"
    }
  }
}
```
