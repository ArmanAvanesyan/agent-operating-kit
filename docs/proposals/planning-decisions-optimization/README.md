# Planning, Decisions and Optimization

Status: **proposed, not implemented**. Baseline: AOK
`0aa56707fe77ed60abe6133117c2c33133612b01`, reviewed 2026-10-15.
These schemas and examples are documentation assets. They add no operational CLI
commands, runtime integrations, model calls, optimizer dependencies or native agents.

This proposal connects outcomes to bounded plans, typed decisions, agent assignments,
verified results and controlled improvement experiments. It complements the
[agent specification](../agent-spec/README.md) and
[development workflow proposal](../../research/development-workflows/proposal.md).

```text
Goal → Plan → Workflow step → Agent assignment → External execution
  ↑                                             ↓
  └──────────── Verification and evidence ───────┘

Recorded results → Evaluation → Candidate optimization → Review → New version
```

## Existing capabilities and proposed additions

| Concern | Existing baseline | Proposed addition |
|---|---|---|
| Work contracts | Operational task packets, roles, skills, hooks and packs | GoalContract, PlanDefinition and explicit goal verification |
| Workflow taxonomy | Saved research catalog: 12 groups, 48 types, 144 activities | Traceability from requirements to goals, steps, assignments and evidence |
| Agent configuration | Merged **proposal** with draft schemas and examples | Planning references to proposed AgentAssignment/Result/Evaluation contracts |
| Exploration | Existing exploration/readiness skills and helpers | First bounded planning/evaluation pilot |
| Decisions | No operational decision-provider abstraction established by this review | DecisionContract and DecisionResult with abstention |
| Native adapters | Existing render/setup/assets according to target support | Versioned planning/decision/optimizer mappings and conformance tests |
| Optimization | No DSPy, GEPA or RL integration established by this review | Optional offline experiments with protected fields and held-out evaluation |
| Execution/state | External executor/runtime boundary | Tested handoff; no new AOK scheduler or daemon |

The agent spec's presence in main is not operational agent support. Native host features
are not AOK capabilities merely because documentation describes them.

## Contents

- [Contracts and algorithms](design.md)
- [Adapter mappings and boundaries](adapter-mappings.md)
- [Phased implementation plan](implementation-plan.md)
- [Source register](sources.json)
- [Draft examples and their limits](examples/README.md)
- [Validation](validation.md)

## Conversation provenance

The user referenced ChatGPT conversation `6ad12451-9fbc-83eb-947a-0f409587b746`, titled
“Explain AI Concepts”. The conversation reader returned three questions about
DSPy/GEPA/RL/System One, goal decomposition and SDLC workflows. Its assistant answers
were content-reference placeholders, so they could not be reviewed. This proposal is
AOK-specific synthesis based on those topics, repository inspection and the primary
sources below; it does not claim to extract the unavailable answers.
