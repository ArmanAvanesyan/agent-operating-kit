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
