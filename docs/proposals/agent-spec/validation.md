# Proposal validation

Checked 2026-10-15. Reproduce from the repository root:

```sh
python3 docs/proposals/agent-spec/validate_examples.py
./scripts/aok validate
git diff --check
```

The proposal checker needs the optional Python `jsonschema` package with `referencing`
(available in the authoring environment). It adds no dependency to the AOK CLI and
fetches no remote schemas. Schemas resolve from a local registry. It validates seventeen
draft schema documents, seventeen positive example instances, thirty-nine negative cases,
the exploration task packet against the existing operational schema, and local
proposal Markdown links. The inventory output and invocation input schemas are checked separately. The checker
also verifies assignment inputs, snapshot hashes/content, run manifest hashes and
blocked/not-run fixture consistency.

Observed final results:

- Draft schema/example checks: passed.
- Existing kit validation: passed.
- Whitespace checks, including new proposal files: passed.

An initial link check identified this validation document before it had been created;
the completed proposal was checked again. No application source changed, so full
runtime regression suites were not rerun for these design assets.

## Limits

These checks establish schema shape and fixture consistency, not a production resolver.
Production cross-object resolution, permission enforcement, model compatibility, executable
tool bindings, transitive provenance, output facts, template parameters, and native round trips
still require implementation. Native Markdown examples were inspected against official
format documentation, not installed or exercised in live hosts. The blocked manifest
and adapter report are deliberately not proof of a successful agent run.

External sources were read on 2026-10-15; their field/version/scope claims are dated
and must be refreshed before implementing native adapters. Existing model inventories
are dated evidence and remain independent of capability evaluations.
