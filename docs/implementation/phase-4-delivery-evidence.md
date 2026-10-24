# Phase 4 delivery evidence

Implementation date: 2026-10-23. This report covers the offline advisory core.

| Gate | Status | Evidence and limits |
|---|---|---|
| P4-core | Delivered | Versioned contracts, signed outcomes, independent quality/calibration estimates, static/capacity bridges, selector, explanations, real-selector replay and supplied-trace comparison |
| Quality/policy evidence | Insufficient for production qualification | Tests use synthetic signed outcomes and deliberately controlled fixtures; no production family cohorts, controlled superiority study or 100-decision shadow promotion is claimed |
| Live evidence | Pending | P2 live invocation and P3 per-CLI collector conformance remain separate gates |
| Runtime dispatch | Pending P6/P7 | Selection does not launch, reserve, enforce, retry, fall back at runtime, change billing or update trackers |

## Implementation

- `internal/selection`: independently versioned `aok-selection/v1` task/candidate/
  policy/decision records, closed schemas, exact asset and assignment binding,
  trusted scoped context, hard gates, estimates, deterministic Pareto/ranking,
  pins/one-edge fallback, hysteresis, checkpoint handling and dispositions.
- `internal/evaluation`: `aok-evaluation/v1` closed embedded schemas, externally
  approved Ed25519 shard and independent verifier receipts, immutable verified
  captures, deduplication/leakage checks and family-level statistical estimates.
- `internal/modelcatalog/requirements.go`: P2 requirement checking reusing trust,
  exact scope, freshness and conflict helpers. The existing profile probe path
  also now checks probe trust before accepting observations.
- `internal/usage/current.go`: current constraints delegate to P3's projector.
  Generation-neutral window identities include the particular limit and scope;
  account context signs the full configuration binding inventory digest.
- `internal/evaluation/replay`: exact-decimal recorded/synthetic accounting,
  explicit replenishment events, common gates, frozen artifact checks, admitted
  cost/time metrics, trace comparison and actual selector replay across five
  frozen policy variants under identical supplied states.
- `internal/cli/selection.go`: opt-in `models select` and `evaluation validate`,
  `estimate`, `replay`, `compare`. New outputs use the existing private immutable
  evidence writer. No new dependency or live executor was introduced.

The original research and P1–P3 serialized formats are unchanged. Optional
candidate-specific assignment assets preserve the baseline agent, inputs, outputs,
packet bytes and assignment permission restrictions; they explicitly bind alternate
profile references without rewriting source documents.

## Acceptance evidence

The positive integration fixture imports and reloads a valid P2 catalog, supplies
independently signed task-family outcomes and earlier validation calibration,
binds signed account/tool/region/permission context, and consumes fresh P3 capacity.
It selects the exact route without granting execution authority. Negative fixtures
cover changed bytes, unknown selectors/capabilities, insufficient quality, stale
usage, censored demand, repeated receipts, cohort relabeling, partition leakage,
key aliases, unsafe paths, output immutability and controlled-policy tampering.

Quality uses locked-test families only. Consumption is calibrated on separate
validation families only, with at least 30 families and validation observations
strictly preceding the audit. Audit-only cohorts retain quality but cannot supply
measured demand. Repetitions do not inflate sample counts; all registered
repetitions must pass for family success. Tests verify the 51/52-family boundary
for the high-risk Wilson floor and the conformal rank. No audit consumption is
used to fit demand.

Deterministic selector fixtures test retention, switching thresholds, zero-value
hysteresis, exact ties, pins, explicit fallback, reset hints and unknown handling.
No hard-gate bypass or false acceptance occurs in these fixtures. Guarded synthetic
traces have zero start over-admission, including the adversarial exact-decimal
`0.4 - 0.3` headroom versus `0.10000000000000002` demand case. External
oversubscription events remain visible rather than being erased.

Verification commands:

```text
Go 1.22.12: go test -count=1 -p 1 ./...
Go 1.22.12: go test -race -count=1 ./internal/selection ./internal/evaluation/... ./internal/usage/...
python3 -m unittest discover -s tests
python3 -m py_compile scripts/aok
bash -n scripts/release/build.sh
./scripts/aok validate
git diff --check
```

The full Go suite includes release packaging and extracted-kit checks. Python
regressions pass: 267 tests, 4 skips. Terminal/process-confinement tests required
running outside the filesystem sandbox; their initial sandbox failures were
rerun successfully. `scripts/release/build.py` does not exist in this checkout;
the present release shell entrypoint was syntax checked instead.

Six CGO-disabled Go 1.22 builds cover Linux/macOS/Windows on amd64 and arm64,
including compilation of the replay test package. Built-executable tests run
selection, signed-evidence validation/estimation, actual replay and comparison
with empty PATH and an allowlisted environment without credentials or host CLI
helpers. Go module fetching is disabled for verification (`GOPROXY=off`,
`GOSUMDB=off`); the offline core has no network transport.

Two Luna-medium workers implemented evidence/estimation and replay/evaluation.
A separate Luna-high reviewer independently identified trust, receipt binding,
assignment, account-scope, cost accounting and controlled-comparison issues; the
fixes have regression coverage. The final read-only review found no blocking
issue in the integrated core, including validation-only calibration and explicit
candidate assignment routing.

## Limits preserved for rollout

Signed issuers attest experimental acceptance and complete account inventory;
hashes and signatures do not independently establish objective truth, runtime
isolation or enforcement. Source observation dates are retained. Reset hints
require new observations and a rerun, never promised future fit.

Trace outcome labels and decision projections are explicitly supplied/unverified;
checking frozen artifact hashes does not turn them into independent verifier
receipts. Actual-selector replay reports conditional decisions under identical
supplied states. Its partition labels are explicitly unverified supplied metadata,
and freeze timestamps are attestations. Dataset curation and trusted freeze
chronology remain experiment-process responsibilities; no causal capacity savings
or broad policy superiority is claimed.

The 48-packet / six-class / 24–12–12 pilot remains a pipeline and abstention pilot,
with 288–432 attempts at two or three configurations and three repetitions.
Qualified cohort evidence, live benchmark execution, controlled pilots, shadow
promotion and automatic runtime routing require their separate later gates.

See [the operator guide](../model-selection.md) for contracts and commands.
