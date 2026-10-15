# Validation and limitations

Run from the repository root:

```sh
python3 docs/proposals/planning-decisions-optimization/validate_examples.py
python3 docs/proposals/agent-spec/validate_examples.py
./scripts/aok validate
python3 -m py_compile scripts/aok
git diff --check
```

The proposal checker uses optional authoring packages `jsonschema` and `referencing`,
already available in the authoring environment. No operational dependency is added.
It checks eight draft schema documents, seven examples, fourteen schema rejection cases
and six semantic negative fixtures (cycles, missing/duplicate steps, data split leakage
and protected-field overlap). It checks local links, source-register JSON and example
references. The additional input schema is checked separately.

These are design checks, not a production planner or verifier. Real semantic validation
must additionally resolve assignments/verifiers/methods/datasets, evaluate predicates,
validate all decision types, enforce permissions and budgets, pin versions/hashes and
verify native adapter behavior. No inference, provider request, DSPy/GEPA experiment or
RL training was performed. Dataset IDs and candidate results are explicitly synthetic
or not-run. Conversation assistant answers were unavailable; see provenance in README.

Final authoring validation on 2026-10-15: proposal checks, agent-spec checks, kit validation,
Python compilation and whitespace checks passed. Native mappings remain unimplemented.
