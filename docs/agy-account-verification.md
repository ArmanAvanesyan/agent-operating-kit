# agy account-mode project discovery — 2026-10-18

Fresh isolated OAuth account mode discovers the AOK-rendered project agent in
agy 1.4.2 Darwin x64. Its name and description appear in `/agents`; absent and
malformed controls omit it. The full native-loading gate remains open because
no explicit native parse-error message was visible for malformed YAML.

This result advances the [onboarding investigation](agy-onboarding-verification.md).
It supersedes the earlier absence of positive discovery in synthetic API-key
mode. It does not establish the underlying reason for the difference or verify
backend authentication, selection, model inference, or runtime enforcement.
The adapter's `HostLoading.Status` remains `not-run`, meaning no qualifying
complete gate result; the overall native-only release remains incomplete.

## Native controls and evidence

[The account manager evidence](evidence/native-adapters/agy-account-2026-10-18/account-manager-final.json)
records the pinned native binary, built AOK binary, exact rendered fixture and
hash, per-case source/audit references, safe states, setup actions, and cleanup.
Each `/agents` panel was observed for at least five seconds after submission.

| Project input | `/agents` sent/opened | Name and description | Explicit parse error |
| --- | --- | --- | --- |
| AOK-rendered `.agents/agents/aok-probe/agent.md` | Yes | Both present | No |
| Absent definition | Yes | Both absent | No |
| Invalid YAML definition | Yes | Both absent | No |

The positive and absent controls qualify individually. The malformed control
proves omission, but fails the existing stricter `qualifies_malformed` predicate,
which requires `parseErrorObserved: true`. The comparator was not weakened and
the descriptor was not promoted. No native adapter rendering change was needed.
The evidence is scoped to this pinned client and isolated account profile.

The [fresh login observation](evidence/native-adapters/agy-account-2026-10-18/replacement-login.json)
records `prompt-ready`. Explicit user authorization covered browser sign-in,
required terms, optional interaction sharing disabled, and owned-directory trust.
The controller first proved the checkbox unchecked, then Done focused, before
accepting. This is a UI observation, not a backend data-handling guarantee.

All native processes ran in audited owned process groups. Native original-home
reads and Keychain lookup were denied. Each account-mode control copied only the
bounded validated temporary profile into a fresh private HOME and Git root;
network access was allowed for native metadata and refresh. Browser launching
was disabled during manager checks. The only query was one `/agents` command
per case, issued at a recognized current prompt. No agent was selected and no
ordinary/model prompt was sent. Native log files were not inspected in account
mode; account rows and raw terminal streams were not retained.

[Cleanup evidence](evidence/native-adapters/agy-account-2026-10-18/profile-cleanup.json)
confirms removal of the original temporary profile. Each manager copy was also
removed, with process-group cleanup recorded. Credentials, codes, OAuth URLs,
and account identifiers are excluded from repository artifacts. The original
installed client was not invoked or changed.

## Matcher correction and failed diagnostic

The previous prompt recognizer required the literal `Gemini API key` status
row. Account mode instead showed an email-shaped identifier followed by a
parenthesized Antigravity product/tier label. Redacted screen geometry retained
the surrounding version, Gemini model, separator, `>` prompt, and shortcuts
footer. The corrected matcher accepts that bounded account/product envelope
only inside the complete prompt or manager frame. Tier strings containing
known auth/error terms are rejected. Other auth/error rows continue to block
input. No account identifier is serialized, and a status row alone never
permits a command or establishes backend authentication.

The [initial attempt summary](evidence/native-adapters/agy-account-2026-10-18/initial-attempt-summary.json)
records an operator error. After the first fresh sign-in reached the prompt,
a temporary plan-label diagnostic indexed row 2 on an empty startup frame and
raised `IndexError`. Normal cleanup removed its credentials. Before that,
a bounded manual recovery had preserved the profile's original expiry and
invalid marker without reading credential contents; the fresh-run cleanup also
removed the copied executable, which was restored from the verified pinned
source before strict checkpoint validation. No deadline was extended.

The failing driver and all observations are retained as historical evidence,
not successful loading results. The replacement approved sign-in used a tested
helper that never indexes terminal rows. Empty and partial frames, hook cleanup,
consent stability, and action replay prevention have offline regression tests.
No further authentication was needed for the three manager controls.

An initial manager-wrapper attempt stopped before native execution because its
environment function tried to copy HOME twice: once for the signal preflight,
once for launch. The corrected function copies once per owned case root and
rejects a root change. [That failed preparation](evidence/native-adapters/agy-account-2026-10-18/account-manager.json)
is retained separately and is not native-loading evidence. Independent review
also found that exceptions while parsing post-run audit data could bypass copy
cleanup. The current helper removes the tracked copy in `finally` and attempts
marker invalidation without following links; tests cover invalidation failure
marker symlinks, and a FIFO marker. Marker open is nonblocking so a native-written FIFO cannot prevent removal. This cleanup refactor followed the live observation; source
hashes distinguish the observed driver from the final tested implementation.

## Operator reproduction

Use only after explicit authorization for a fresh browser sign-in and the
three consent/scope flags. Prepare a new private, owned code file under
`/private/tmp`, mode `0600`, inside a mode `0700` directory. Never reuse codes.
The login helper waits up to five minutes and removes the code file after use.

```sh
python3 scripts/native-loading/prepare_agy_account_login.py \
  --authorization-code-file /private/tmp/OWNED-DIRECTORY/authorization-code.txt \
  --evidence-out docs/evidence/native-adapters/NEW-login.json \
  --accept-required-terms --disable-optional-sharing --trust-owned-fixtures

python3 scripts/native-loading/probe_agy_account_manager.py \
  --aok /private/tmp/BUILT-AOK \
  --isolated-login-root /private/tmp/ROOT-FROM-LOGIN \
  --evidence-out docs/evidence/native-adapters/NEW-account-manager.json \
  --accept-required-terms --disable-optional-sharing --trust-owned-fixtures
```

Output files must be new and inside canonical nonsymlink repository directories.
The source profile must be unexpired and prompt-ready; an invalid diagnostic
checkpoint is rejected. No synthetic API key or provider setting is inserted.
The manager helper exits 0 only if the full three-control gate qualifies; this
observation exits 1 because explicit malformed-file rejection remains missing.
The input profile is left under its original deadline for operator cleanup;
remove it after verification. Copies are removed after each case even if audit
processing fails. The helpers install nothing and are separate from normal AOK
CLI operations. They do not dispatch workflows or models.

## Sources and validation

Official native format and manager references remain the
[custom-agent guide](https://antigravity.google/docs/subagents?tab=cli) and
[`/agents` reference](https://www.antigravity.google/docs/cli/commands/agents).
Live observations establish discovery behavior; format documentation alone does
not close a loading or runtime-enforcement gate.

The evidence index binds original attempt sources, current helper sources,
cleanup records and validation. Validation results are recorded alongside it.

[Validation](evidence/native-adapters/agy-account-2026-10-18/validation.json): 239 Python tests passed (two skipped), Go tests passed, kit validation, Python compilation, both proposal checkers, and Codex render smoke passed. The initial restricted test invocation failed existing OS-permission checks; the full rerun with PTY and nested-sandbox access passed. The installed agy SHA-256 is unchanged.
