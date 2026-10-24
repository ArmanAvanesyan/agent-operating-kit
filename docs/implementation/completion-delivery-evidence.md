# Remaining P3–P10 implementation evidence

Date: 2026-10-24. This delivery implements offline/injected completion work under
the [existing P3–P10 plan](remaining-gates-and-first-pilot-plan.md). It introduces
no additional roadmap phase. Actual connected qualification, first live P9
end-to-end pilot and production activation remain blocked/pending.

## Delivered boundaries

| Existing phase | Implementation in this delivery | Qualification remaining |
| --- | --- | --- |
| P3/P7 | Before-admission and actual-launch quota-window bounds; changing reset observations invalidate the qualified execution horizon | Live collector/source/account conformance; attributable settlement; cross-window execution/settlement unsupported |
| P4/P9 | Disposable calibration coordinator preserves ordinary quality abstention while validating exact-cohort explicit upper bounds and all non-quality gates | Independent sufficient validation and locked-audit families; actual outcomes and protected issuer/verifier services |
| P7/P9 | Request-local calibration/experiment enrollment composes the existing runtime, claims, staging, accounting and native runner | Qualified controls, current identity/revocation, externally durable consumption/budget/witness/monitor and connected drills |
| P8/P9 | Protected readiness v2 requires scoped acceptance, handoff and provider-operation gates; delivered claims additionally require publication | Independent verifier, native preservation/recovery and operation-specific provider read/write/readback qualification |
| P9 | Closed domain-separated experiment contracts/grants; current-time signature/role/revocation validation; one bounded trial; production dispatcher unchanged | Actual experimental enrollment, live pilot, 100 eligible observed shadows, campaign assessment and narrow external production authorization |
| P3–P10 | Digest-bound inspection-only dependency/blocker ledger; explicit per-route optional gates | Ledger presence establishes no live, platform or production qualification |

The primary candidate remains one local Codex App Server configuration plus one
GitHub Issue comment operation in a disposable repository. Codex and Claude P6
gates remain independently blocked. Azure, Linear, Jira Cloud and Jira Data Center
retain independent operation gates. Additional local hosts, provisioning,
methodology activation, native Windows certification, Codex Cloud and campaign
activation retain the blockers named in the generated ledger.

## Safety properties and independent review

The two implementation workers used GPT-6 Luna at medium reasoning. A separate
Luna reviewer at high reasoning reviewed authority and recovery boundaries.

Review identified and the implementation corrected:

- Missing captured repository binding, incomplete cohort matching and missing
  enrolled spend/currency checks before reservation or launch.
- Missing completion gates at readiness admission, independently of assessment.
- Mutable aliases to approved bounds and record fields; captured copies and bound
  digests now retain enrollment identity across service checks.
- Mutable verified experiment reports; private integrity seals and real-time
  expiry checks now protect external trial consumption.

Experiment and calibration records cannot become production promotion grants.
Runtime records remain `accepted:false`; the original calibration selection
report remains an abstention rather than a fabricated successful recommendation.
The standard CLI installs no protected execution services. No credentials, live
host invocation, provider mutation, pilot, publication or production activation
were used to implement or test these boundaries.

## Acceptance record

Final checks passed on this checkout. Earlier phase test results do not qualify
this delivery or any connected route. Go checks use 1.22.12, `GOTOOLCHAIN=local`,
a private build cache, and canonical `TMPDIR=/private/tmp` so symlink protections
are not weakened for test fixtures.

- Full Go suite: `/private/tmp/aok-completion-full-go-final.log`.
- Broad P3–P10 race suites, including multiprocess accounting and runtime
  crash/recovery fixtures: `/private/tmp/aok-completion-race-final.log`.
  Final affected runtime/experiment/CLI/readiness/ledger race rerun:
  `/private/tmp/aok-completion-affected-race-final.log`.
- Python regressions: 268 tests, four existing skips, passed under the native
  test environment: `/private/tmp/aok-completion-python-tests-final.log`.
  TTY/confinement tests require running outside the outer development sandbox;
  no application protection was relaxed. Compilation and kit validation passed.
- Empty-PATH/no-credentials completion and existing P9/P10 smoke tests passed.
  OS-denied network smoke also passed outside the outer sandbox, whose nested
  sandbox policy initially prevented `sandbox-exec` itself from starting.
  Final packaged completion smoke:
  `/private/tmp/aok-completion-packaged-offline.log`.
- CGO-disabled Linux/macOS/Windows amd64/arm64 builds passed:
  `/private/tmp/aok-completion-six-targets.log`. The existing release builder
  packages five binary targets; Windows arm64 is a separate compilation check,
  not a new installer or platform certification claim.
- Release packaging, all 14 artifact checksums and extracted macOS Intel
  installer validation passed. Two installed delivery regressions passed in a
  disposable home: `/private/tmp/aok-completion-installed.log` and
  `/private/tmp/aok-completion-installed-report.json`. Artifacts are local under
  `/private/tmp/aok-completion-release-verified`; nothing was published.
- Independent review reported no remaining material issue after the recorded
  fixes. A targeted CLI regression also corrected malformed-time misuse to exit
  2; invalid/blocked results remain exit 1 and successful structural checks 0.
- Strict JSON/version/signature/tamper, unsafe-path/symlink, immutable-output,
  privacy, purpose-confusion, no-effect blocked admission, bound/cohort/spend and
  reset-window tests passed. Documentation links and whitespace were checked.

Live/native acceptance is not simulated by passing fixtures. Protected service
deployment, actual evidence collection and per-route end-to-end admission remain
external gates with no claimed completion date.
