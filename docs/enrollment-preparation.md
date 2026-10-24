# First Codex + GitHub enrollment preparation

This is local preparation within P3–P10, not another phase or a live enrollment.
`aok-enrolled prepare` creates deterministic non-executable drafts and an editable
operator inventory. `preflight` reports missing inputs, integrity and narrow
signature checks. Neither command reads environment credential values, signs
records, invokes Codex/gh/Docker, contacts services, or grants execution authority.

## Commands

Build the protected executable with Go 1.22 or later. The stock empty-pin build
can perform offline preparation; active services remain disabled.

```sh
go build -o build/aok-enrolled ./cmd/aok-enrolled
mkdir -p build/enrollment
build/aok-enrolled prepare --out build/enrollment/codex-github-first-live-run
build/aok-enrolled preflight --bundle build/enrollment/codex-github-first-live-run \
  --as-of 2026-10-24T12:00:00Z --json
```

Use the actual intended RFC3339 inspection instant; the example is not a fresh
observation. Preparation exits 0 when it creates the new private directory.
Preflight exits **1** with a structured blocked report, including on an intact
new package: operator inputs and live qualification are absent. Command misuse
exits 2. Preflight writes only stdout; redirect a saved report outside the bundle.
Preparation refuses an existing output directory, unsafe paths and symlink
ancestors. Preflight never edits the package. Use `--help` for either command.

## Files and operator workflow

`manifest.json` records the compiled required-input list and immutable draft
hashes. `README.md` and `drafts/*.json` are immutable template references.
`operator-inputs.json` is intentionally editable and outside that hash set.

Every draft is wrapped with `executable:false`, an explicit draft instruction,
and a typed `value`. Unknown identity, time, budget, hash and signature fields
remain empty/default-valued; defaults do not establish an approved zero budget
or measured consumption. The wrapper is not accepted by a live bootstrap reader.
Do not edit hashed templates or feed them directly to active commands. Copy the
needed typed value into an externally reviewed working payload, fill it from
real evidence, validate it under the corresponding contract, and have the
appropriate external signer issue the exact bytes. The preparation tool has no
key generation, payload promotion or signing command.

The templates cover anchor/authority/verifier services, all five synthetic
qualification actions, calibration and ordinary experiment workers, post-run
verify/accept/handoff, GitHub dispatch/reconcile, host controls, capacity records,
verifier policy, grants, acceptance and readback records. `manifest.json` lists
the exact expected paths under `inputs/` and `signed/`. These files are deliberately
absent until supplied; filling an inventory field does not create them.

Fill the inventory with the actual repository and immutable tenant/repository/
issue IDs, positive REST issue number, one Unix platform, exact binary hashes,
chosen gh version, exact selector/P2 configuration/profile, account/pool, named
operator and recovery owner. The package pins no invented account or model.
Codex qualification retains the current adapter's exact 0.162.1 version.

Record only opaque secret references for these seven categories:

- `github-read-token`: repository-restricted Issues read + Metadata read.
- `github-write-token`: Issues write + Metadata read for the same enrolled route.
- `anchor-bearer`, `authority-bearer`, `verifier-bearer`: separate service credentials.
- `anchor-response-key`, `authority-response-key`: protected signing-key references.

Those are reference categories, not environment values or mandated environment
variable names. Native ChatGPT login remains in the approved isolated HOME;
provide independent account/HOME/environment measurements without copying its
auth cache. Bootstrap/enrollment/control/acceptance/readback private keys remain
with their external custodians. Service `secretEnv` mappings are created later
under the exact signed bootstrap contract. No private key or token belongs in
this package. Supply public trust roots, system-trusted TLS chains and non-secret
credential-provisioning evidence separately.

## What preflight proves

The checker strictly parses bounded JSON, checks manifest version and compiled
requirements, validates immutable templates and safe file paths, and enumerates
missing/invalid operator fields and credential references. For supplied protected
envelopes it verifies exact-byte Ed25519 signatures against the package public
key, dates at the supplied instant and the expected service/action/mode. That
status is **signature-valid-only**: the public key still needs independent pin
approval, nested policies and live fields need their own validators, and no
configuration or deployment becomes qualified from this result.

Other supplied evidence is **present-unverified**. File existence, a JSON claim,
a signature, or a nonempty credential reference cannot clear a live gate.
The report always retains `authorizationGranted:false`, `executionReady:false`
and `productionEligible:false`. Preflight is a preparation blocker inventory,
not an operational readiness admission check; it deliberately cannot turn green
for live execution. It does not recursively inspect arbitrary paths embedded in
service configurations or resolve their secret references.

Findings are stage-labelled: qualification, calibration, postrun and ordinary-pilot.
Post-run candidate proofs/acceptance and final readback are expected only after
the corresponding immutable output exists. Their current absence does not imply
they should be pre-signed. Never fabricate placeholders to remove a blocker.

## Services and gates still required

Provision protected worker/enforcer/native observer, verifier isolation and
acceptance services, GitHub broker/readback signer, capacity/runtime/outbox
stores, stop/witness services and independently retained anchor storage. The
reference anchor is single-stream: create separate approved instances/genesis
per operation stream, or use a qualified compatible multistream service. The
single anchor template is a template per stream, not a multistream deployment.

Run only separately signed synthetic qualification operations first. Each uses
purpose `synthetic-conformance-only`, an external one-use anchor and a five-minute
deadline; host conformance has at most ten starts and thirty seconds per turn.
It requires measured filesystem/network/ambient controls and a positive approved
bound for the entire matrix in actual P3 units. Its whole usage hold remains
uncertain pending valid attribution; token counts do not supply percentages.

After independent review, issue fresh live proofs for controls, identity,
capability, capacity, verification, runtime, authorization, platform, provider-read,
provider-operation, witness, budget and stop-monitor. Calibration skips only the
ordinary quality eligibility requirement. Ordinary selection additionally needs
qualified independent P4 quality/demand evidence and its exact advisory decision.
Use the protected runtime's admission and current evidence validators at launch.

Create post-run proof and one-use verifier grant only after the actual runtime
candidate exists; obtain independent acceptance before handoff. Final GitHub
writeback needs its own exact one-comment P8 grant and complete signed readback.
Lost responses do not authorize resend. Production/shadow/promotion gates remain
separate and pending.

See [operator enrollment](implementation/operator-enrollment-first-codex-github-pilot.md)
and [protected service operation](enrolled-live-services.md) for live contracts,
key encodings, custody and acceptance requirements.
