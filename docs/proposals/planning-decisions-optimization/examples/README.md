# Documentation fixtures

All seven examples are proposed shapes, not observed execution. Each JSON document
validates against the same-named schema in `../schemas/`.

| Example | Meaning |
|---|---|
| [Goal](goal.json) | Report validity, coverage and source-preservation predicates |
| [Plan](plan.json) | Three-step fixed DAG with final aggregate verification |
| [Revision](plan-revision.json) | Proposed future change, not accepted or evidenced |
| [Decision](decision.json) | Deterministic depth choice with abstention |
| [Decision result](decision-result.json) | Synthetic abstention, no provider call |
| [Optimization](optimization-experiment.json) | Not-run experiment with synthetic dataset IDs |
| [Traceability](traceability.json) | Planned requirement-to-verifier links; no actual run |

The referenced `repository-explorer`, profile, output contract and evaluation contract
come from [agent-spec examples](../../agent-spec/examples/README.md). Stage-specific
assignment IDs and verifier IDs remain illustrative references requiring future
registry/assignment definitions. They do not imply that one existing assignment was
split or executed. Dataset IDs are distinct placeholders, not real train/test data.

The decision input schema is a shape fixture. Actual deterministic depth-selection
rules are not implemented here. A revision has no evidence and remains proposed; it
cannot be adopted as an evidence-triggered change until evidence is attached.
