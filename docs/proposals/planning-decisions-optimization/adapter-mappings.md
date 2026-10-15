# Proposed adapter mappings

These mappings are design obligations, not implemented integrations. Follow the agent
spec's [native adoption and conformance rules](../agent-spec/adapters.md). Maintain three
separate adapter families so model providers do not become workflow runtimes implicitly.

| Family / target | Neutral input → native representation | Output / evidence | Limits and gaps |
|---|---|---|---|
| Client adapters: Codex, Claude Code, OpenCode and others | Resolved assignment/agent → verified native prompt/config/launch surface | Native run events mapped to proposed AgentRunRecord/Result | Existing AOK render support does not implement these planning contracts |
| Native Goal surfaces | Goal outcome/limits → supported host objective/settings | Native stop reason plus independent verifier evidence | Refresh version-specific behavior before implementation; native continuation is host-owned |
| External coordination runtime | Ready step/assignment → runtime dispatch/claim reference | Attempts, cancellation, receipts and results | AOK must not duplicate authoritative queue/claim state; documented swarmd payloads are not a tested bridge |
| Deterministic decision engine | Typed input + registered rule → checked boolean/choice/score/rank | Value and reproducible evidence | Rule registry/execution is proposed; do not evaluate arbitrary imported code |
| System One decision provider | Boolean/choice/score → versioned provider request | Typed answer, provider statistics, error or abstention | Ranking needs separately validated composition; confidence is not authorization |
| DSPy program adapter | Input/output contract → signature/module interface and metric | Program candidate, evaluator results and trace references | Requires explicit Python integration; existing CLI agents are not automatically DSPy modules |
| GEPA optimizer adapter | Approved textual components + frozen evaluator/data splits → optimization experiment | Candidate text changes, scores, diagnostics, spend and provenance | Optional offline dependency; enforce protected fields outside optimizer |
| RL experiment adapter | Registered environment, permitted actions, observations and reward | Policy artifact and independent evaluation evidence | Deferred research; no trainable policy schema/runtime is implemented here |

## Source-derived mappings

DSPy documents signatures/modules and metric-driven optimization. Use signatures to
represent component inputs/outputs; keep AOK's full permission/run contracts outside
that abstraction. GEPA accepts textual candidates and evaluators with feedback, making
it suitable for controlled instruction candidates. Its proposer cost and evaluation-call
budgets need separate accounting. See source IDs D1, G1 and G2 in [sources](sources.json).

System One's primitive documentation distinguishes normalized boolean answers from native
`noul` fields and describes choice/score interfaces. An adapter must normalize these
without erasing native provenance, statistics or errors. Never assume broad SDK types
are accepted by every model. Pin model/API/SDK versions and validate requests (S1).
No native request fixture is claimed verified in this proposal.

## Import/export and conformance

Native definitions/configuration enter through detect → parse → normalize → inspect →
adopt, with field/source provenance and preserved opaque extensions. Canonical export
uses resolve → render → verify, plus sidecars for neutral-only fields. Unknown native
commands stay inert; secrets are references/redacted. User-owned files require explicit
adoption and conflict review, not automatic overwrite.

For each contract field record native, executor-backed, guidance-only or unsupported.
Required invariants, permissions, limits, dataset isolation and completion gates must
have verified native/executor enforcement. Unsupported required fields block. Text in a
prompt may guide behavior but cannot certify policy or budget enforcement.

Conformance tests must cover: request/result types; false versus unknown; abstention;
provider errors; missing evidence; uncalibrated confidence; choice/ranking constraints;
stop-reason mapping; native scope expansion; source/sidecar drift; credential redaction;
protected-field changes; optimizer/test-data leakage; independent cost counters; and
failed goal verification after apparently successful subtasks. Use isolated fixtures first.

Do not claim semantic round trips when importing a compiled DSPy program or learned RL
policy into the neutral agent format. Preserve them as external artifacts with hashes,
versions and explicit opaque mappings. Text candidate adoption must retain evaluation
provenance; a lossy import cannot silently weaken acceptance or permissions.
