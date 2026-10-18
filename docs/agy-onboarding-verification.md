# Isolated agy onboarding investigation — 2026-10-18

The later [account-mode follow-up](agy-account-verification.md) establishes positive
project discovery in a fresh OAuth profile. The synthetic-provider results below
remain historical evidence; the full gate still awaits explicit malformed-file
rejection.

This investigation follows the post-code stop recorded in PR #75. agy 1.4.2
Darwin x64 remains hash-pinned; the installed client and original home/keychain
are not used. Native manager discovery is not established by a successful
browser sign-in, a theme preview, or a diagnostic checkpoint.

## Observed cause and limits

The generic screen matcher classified any `error`, `failed`, `fatal`, or
`invalid` word as a failure. The new
[post-code observation](evidence/native-adapters/agy-onboarding-2026-10-18/pfbu39s9.json)
places its matching words on row 25 of the native color-scheme screen. Neighboring
rows contain Go code, a warning example and a file link. The observation retains
fixed labels and coordinates, not the native terminal text or code.

The official [first-launch guide](https://antigravity.google/docs/getting-started?tab=cli)
describes color, rendering and workspace-trust configuration. The
[CLI changelog](https://www.antigravity.google/docs/changelog?tab=cli) documents
code-diff samples in setup previews (v1.3.5) and warning examples (v1.0.3).
The [native continuation evidence](evidence/native-adapters/agy-onboarding-2026-10-18/k20opabk.json)
confirms the cause: recognizing the exact boxed Go/error/warning/link sample
permitted one visual-selection Enter, which reached Terms of Service & Data Use.
The exclusion applies only to that exact sample row and surrounding layout.
Other errors still block input and readiness. This progression does not prove
backend authentication or native agent loading.

## Code-entry form correction

PR #75's tightened boxed-input assumption rejected the actual unboxed form.
The native placeholder is `Authorization code...`, followed by two blank rows
and the public `shift+up/down Navigate` footer. The renderer cursor ends at that
footer. The helper now recognizes this precise current-screen layout together
with the copy-code/browser/paste-below instruction. Other titles, missing or
altered footers, nonblank form contents and misplaced cursors are rejected.
The boxed-field path remains available. Title-only screens never authorize input.

The [form geometry evidence](evidence/native-adapters/agy-onboarding-2026-10-18/iadsn980.json)
explains the mismatch; the subsequent submitted-code observation proves that the
corrected native form gate recognized the real screen. File eligibility is
rechecked after fresh PTY output so a stale frame cannot trigger a file read.

## Isolation and privacy

The operator verification helpers copy the pinned binary into an owned private
root, deny original-home reads and native Keychain access, and audit process
identities before native execution. Only the owned process group is cleaned up.
A code file is private, single-link, bounded and removed before submission.
No codes, tokens, OAuth URLs, account identities or raw terminal streams belong
in repository evidence. Only redacted public controls from synthetic UI fixtures
are retained as rendered text.

An offline synthetic-provider test used a noncredential value and denied all
native networking. It stopped before onboarding and supplies no authentication
or native-loading evidence. Short native form diagnostics suppressed browser
launch and supplied no code. Their source drivers and overrides are recorded
alongside exact dependency hashes; they are not ordinary host verification runs.

Native UI actions are bounded. A visual color selection requires its exact
screen and continuation hint and must exclude errors, ambiguous terminal state,
trust prompts and terms acceptance. No agent or model prompt is submitted.

## Diagnostic checkpoints

`--retain-onboarding-profile` is an explicit exception to default failure
cleanup. After a submitted code reaches the recognized onboarding screen it
may retain the private HOME for diagnosis, with `valid: false`,
`readyPromptObserved: false`, a pinned binary hash, and the original expiration
(no more than fifteen minutes). It exits with status 1. This is not a loading
or authentication result; the normal manager profile consumer rejects it.

`--resume-login-root ROOT` validates that exact private marker, owned canonical
root/HOME, file types and bounds, and native binary hash. It starts a new audited
process group in the same private profile after removing only three named
launch sentinels. It never extends the profile's deadline. No additional code
input is accepted in resume mode. Resumes remove the private HOME on failure
unless explicit diagnostic retention is requested again.

The checkpoint can distinguish the color screen from a nonready stage reached
after a recorded color continuation. Retaining such a stage grants no permission
to select trust, accept terms, submit model input, or select an agent. Operators
must remove the retained root when diagnosis finishes or the checkpoint expires.
If resume validation or audit/source setup fails before a native process starts,
the existing invalid checkpoint can remain for operator cleanup; this is not a
successful resume. Checkpoint roots and native credentials must not be committed.

Diagnostic retention and permission to act are separate: an unrelated generic
error on a recognized preview can be retained for diagnosis, but continues to
block all input and readiness; the manager consumer rejects its invalid marker.
The regression suite checks this combination explicitly.

A new native process can restart the visual setup even after a previous process
reached terms. `--restart-visual-setup` is an explicit resume-only option to allow
one guarded visual selection in that new process. It preserves the original
profile deadline and never authorizes terms, trust or model input. Without the
option, the recorded after-color phase prevents replay. The expired diagnostic
profile was removed; no deadline was extended.

The user approved required service terms with optional interaction-data sharing
disabled. Consent handling must prove that optional sharing is unchecked before
submission; approval to sign in alone did not establish that choice.

## Native terms and owned-directory trust

The [native consent observation](evidence/native-adapters/agy-onboarding-2026-10-18/kx4067fq.json)
showed optional sharing checked by default. The operator controller disabled it
only while the checked box was focused, inspected the fresh unchecked screen,
navigated to Done, and submitted only after another fresh unchecked/Done-focused
screen. The native process then reached workspace trust. This records the UI
choice, not independently verified backend data handling.

Trust was confined to the canonical, owned temporary verification root. The
native prompt uses `Accessing workspace:` and `Yes, I trust this folder`.
Earlier inferred labels failed closed and sent no trust input. Three bounded
manual diagnostic recoveries preserved the original private profile deadline
without reading credential contents: stop the audited owned group, hold HOME,
interrupt and verify normal process cleanup, restore HOME, and revalidate the
invalid checkpoint. Source drivers and recovery records are archived. This is
operator evidence, not a general automated recovery or installation feature.

The [post-trust observation](evidence/native-adapters/agy-onboarding-2026-10-18/zcjalv2n.json)
then stopped on unsupported terminal syntax in `_numbers`. No `/agents` command
was sent. Normal failure cleanup removed the private credentials. Native loading
therefore remains unverified at this point; syntax or successful consent cannot
close the gate. Further diagnosis uses bounded CSI metadata and offline fixtures
before another authenticated attempt.

## Current result and remaining loading gate

The native panel opens safely in a new private synthetic-provider session. The
[settled three-case run](evidence/native-adapters/agy-onboarding-2026-10-18/local-manager-three-case-settled.json)
kept each manager open for at least five seconds:

| Input | `/agents` opened | Custom name/description | Explicit parse failure |
| --- | --- | --- | --- |
| AOK-rendered valid project definition | Yes | Absent | No |
| No custom definition | Yes | Absent | No |
| Invalid YAML definition | Yes | Absent | No |

A [minimal native definition](evidence/native-adapters/agy-onboarding-2026-10-18/local-manager-minimal.json),
[explicit `mainAgent: true`](evidence/native-adapters/agy-onboarding-2026-10-18/local-manager-main-agent.json),
and the [documented flat-file location](evidence/native-adapters/agy-onboarding-2026-10-18/local-manager-flat-diagnostic.json)
also showed only the default agent. These are controlled diagnostics, not
positive native-loading evidence. The fixture's exact cause of omission remains
unknown; these tests do not establish a field-specific adapter defect. No
speculative adapter rendering change was made.

The [final evidence](evidence/native-adapters/agy-onboarding-2026-10-18/local-manager-final.json)
records the AOK binary hash and exact rendered fixture as well as the pinned
native binary, source hash, each control, setup actions and cleanup. Native
loading remains `not-run` in the adapter descriptor, meaning no qualifying
positive loading result. The overall native-only release remains incomplete.
Backend authentication, agent selection, inference and runtime policy enforcement
are unverified. An authenticated account/provider-mode comparison remains open;
these observations do not prove that account mode causes the omission.

The corrected readiness matcher recognizes the observed ASCII prompt panel.
Only a complete native prompt or manager frame exempts its exact public
`Gemini API key` status row from the authentication matcher. Other auth text
continues to block input. Manager recognition never authorizes a second command
or agent selection. Fresh private roots use a name without `login` so the
workspace title cannot itself trigger authentication detection.

## Reproduce the bounded local-manager check

Prerequisites: Darwin x64, the already obtained hash-pinned agy 1.4.2 binary at
`/private/tmp/aok-agy-1.4.2-x64/antigravity`, Git and a built Go AOK binary. The
harness installs nothing and changes neither the original CLI nor its profile.
Only pass the three consent flags after the operator has approved those choices.
They apply to required terms, disabled optional sharing and owned synthetic
fixture directories, respectively.

```sh
go build -o /private/tmp/aok-agy-probe ./cmd/aok
python3 scripts/native-loading/probe_agy_local_manager.py \
  --aok /private/tmp/aok-agy-probe \
  --evidence-out docs/evidence/native-adapters/agy-new-local-manager.json \
  --accept-required-terms --disable-optional-sharing --trust-owned-fixtures
```

The output must be a new file under real, nonsymlink repository directories.
Directory-FD traversal and exclusive no-follow creation prevent output escapes.
The harness imports and exports the synthetic fixture through the Go CLI,
creates fresh private Git roots and HOME directories, and uses a fixed synthetic
provider value. External inbound/outbound connections are denied; loopback is
allowed for native initialization. A separate policy check observed successful
loopback binding and an outbound TEST-NET connection denied with a permission
error. This is a loopback-restricted test, not proof against indirect relaying by
a local service. No actual account or model prompt is used.

The only non-query terminal inputs are the exact observed setup controls and
one `/agents` command at a verified current prompt. Actions cannot replay in the
same process. No agent is selected. Every native process group and private root
is removed; audits retain source snapshots and sanitized observations. Matching
agent-load messages may be read from only the bounded known log inside the
synthetic HOME; outside-root targets and unrelated files are rejected. Rendered
public controls are redacted separately from raw terminal streams, which are
never saved.

Exit 0 requires all three native-loading controls to qualify. The observed
exit 1 records an unresolved gate, even though the panel itself opens. Diagnostic
`--fixture-variant` and `--fixture-layout` options are explicitly recorded; a
layout override does not qualify the adapter's unmodified output path.

## Sources and validation

- [Official CLI agent manager reference](https://www.antigravity.google/docs/cli/commands/agents): native locations and local panel controls.
- [Official custom-agent specification](https://antigravity.google/docs/subagents?tab=cli): documented fields and defaults; runtime behavior remains separately tested.
- [Kitty keyboard protocol source](https://raw.githubusercontent.com/kovidgoyal/kitty/master/docs/keyboard-protocol.rst): `CSI <1u` restores one keyboard-mode stack entry; it changes no screen cells.
- [XTerm control reference](https://invisible-island.net/xterm/ctlseqs/ctlseqs.html): `CSI 0 SP q` changes cursor style; the renderer does not model cursor appearance.

Regression coverage includes code-field geometry, fresh-frame file submission,
preview-plus-real-error blocking, private checkpoint validation/expiry, log-link
confinement, guarded visual replay, exact prompt/manager layouts, terms/trust
state, output symlink/traversal rejection and manager state retention. Source
hashes and sanitized attempts are indexed beside the final evidence. The [validation record](evidence/native-adapters/agy-onboarding-2026-10-18/validation.json)
records 229 Python tests run (two skipped), all successful, plus Go tests/build, kit and
proposal checks, compilation and a Codex render smoke check. The
[source audit](evidence/native-adapters/agy-onboarding-2026-10-18/source-validation.json)
verifies archived source hashes, cleanup and absence of credential patterns;
the original installed agy binary hash is unchanged.
