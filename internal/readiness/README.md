# Readiness snapshots

`RouteManifest` and `Policy` use the closed `aok-readiness/v1` JSON contract.
`ReadManifest` and `ReadPolicy` reject unknown fields, duplicate keys, trailing
values, oversized files and symlinked paths. `Assess` needs an explicit UTC
`asOf`; it does not execute commands or authorize work.

The policy digest is canonical JSON over the decoded policy. A caller may pass
that exact digest in `Inputs.PolicyPin` along with the principal and
`readiness-policy-authority` role obtained from its protected policy-selection
boundary. The caller must also inject a `PolicyAuthorityVerifier` that resolves
the principal and role through protected trust. The digest protects integrity,
not authorship. An absent pin or verifier appears as a blocking gate; a
mismatched pin is an input error.

Every mandatory P3-P9 gate, including distinct P8 acceptance and delivery
evidence gates, is present in the policy validator's fixed inventory.
Each requires a matching named native validator and live-origin evidence.
Without an installed adapter, the gate is `unknown` and the report is blocked.
Evidence declarations alone cannot pass a gate. Native adapters own signature,
revocation, supersession, provenance and native scope verification. The package
checks declared scope, time bounds, safe asset paths, byte digests and fixture
versus live classification before invoking an adapter.

`testdata/route.json` and `testdata/policy.json` demonstrate the contract. The
policy intentionally has no evidence bindings, so an assessment remains
blocked until exact live evidence and qualified native validators are supplied.
An offline report is a reproducible snapshot, never a launch token or production
qualification.

`NativeAdapters` wires host control checks to `hostbridge.ReadPolicy`,
`hostbridge.ValidatePolicy`, and `hostbridge.VerifyControls`; its trust roots and
platform tuple come from `HostControlTrust`, outside the evidence bundle.
`AcceptanceValidator` reuses the P8 verification signature, freshness, criteria,
and candidate binding checks with externally supplied issuer keys.
`DeliveryEvidenceValidator` delegates full handoff validation to the existing
delivery bridge and requires the handoff policy's issuers to equal the external
trusted set. It validates local repository handoff evidence only; it does not
establish remote publication or readback. The separate `p8.publication` gate
binds the exact provider tuple and delivery milestone and stays unknown until a
qualified publication validator is supplied. Other validators remain unknown
until supplied by a qualified implementation.
