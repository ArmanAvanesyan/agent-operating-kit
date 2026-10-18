# Isolated native startup diagnostics — 2026-10-18

Cursor project command-picker discovery is now verified for the pinned host.
The agy project discovery gate remains open. The static adapters remain
implemented; runtime policy enforcement remains unverified.

## agy macOS launcher correction

The apparent startup failure occurred before the copied native CLI executed.
The gated Python launcher used Apple's `/usr/bin/python3` developer-tools shim;
its output contained an Xcode warning that the earlier classifier treated as a
native failure. The launcher now resolves the running Python interpreter and
records separate runner-started and native-exec-attempted sentinels.

The [before](evidence/native-adapters/agy-startup-diagnostic-2026-10-18.json)
and [after](evidence/native-adapters/agy-startup-python-fixed-2026-10-18.json)
reports bind the probe source and pinned Darwin x64 agy 1.4.2 binary. After the
correction, positive, absent and malformed controls all reached an authentication
barrier without a startup error. No `/agents` command was sent in those runs.
This resolves the launcher error, not the native-loading gate.

Fresh isolated sign-in is operator-approved. Preparation denies the original
home and `com.apple.securityd`, disables automatic updates, and permits OAuth
networking/browser launch. Credentials can only persist in a private temporary
HOME; reports retain neither native terminal text nor credentials. A ready
screen is explicitly not independent proof of backend authentication. Profiles
expire after fifteen minutes and are copied only after ownership, path, link,
permissions, size and freshness checks.

The first two [login](evidence/native-adapters/agy-login-attempt-02oqrhgi-2026-10-18.json)
[attempts](evidence/native-adapters/agy-login-attempt-5gb2d4o4-2026-10-18.json)
sent no Enter and opened no sign-in flow: flattened ANSI output did not expose
an exact native login-choice row. Their private homes were removed.

## Cursor startup localization

The pinned official Cursor 2026.10.16-e3cb207 wrapper successfully authenticated
against the backend using private copies of existing CLI credentials. Original
Keychain items were not modified. The original installed client was not run.

A diagnostic-only Node preload observed an allowlist of public startup marks.
The [initial trace](evidence/native-adapters/cursor-startup-trace-2026-10-18.json)
ended at `cli/willRunWorkspaceOpenHook`. This is the last observed mark, not proof
of the precise pending operation or a deadlock. Static inspection places plugin
configuration loading on that initialization path. Switching to the real
Command Line Tools Git alone did not reach a recognized discovery prompt.

An optional immutable Git shim refuses clone, fetch, pull, push, ls-remote and
submodule operations without retaining their arguments. The
[diagnostic trace with this policy](evidence/native-adapters/cursor-startup-git-denied-trace-2026-10-18.json)
advanced through resource construction and rendering. Refusal counts are
conservative operation counts, not verified network-request counts; some
submodule operations can be local. This altered isolation policy and diagnostic
preload do not establish ordinary host loading.

An [uninstrumented longer window](evidence/native-adapters/cursor-startup-long-window-2026-10-18.json)
and [terminal-response follow-up](evidence/native-adapters/cursor-startup-terminal-caps-2026-10-18.json)
did not recognize a discovery prompt. No ordinary prompt, agent invocation or
model inference was submitted. Authenticated status does not prove subagent
loading.

## Terminal handling and evidence boundaries

The probes reconstruct a bounded current terminal screen instead of deleting
cursor-movement escapes and concatenating positioned rows. The renderer handles
cursor positioning, erasure, insertion/deletion, scrolling and alternate screens.
Unknown or incomplete controls, concealment modifiers, and an unfinished
synchronized redraw fail closed. Known equal-color glyphs are rendered as
width-preserving spaces: no login or discovery input may be
sent from an ambiguous screen. Standard terminal-query replies are separate from
user input. The agy manager command uses carriage return, matching terminal Enter.

Every native child runs in an owned process group with a private HOME and file
boundary. Cleanup closes the PTY, terminates only the audited owned group, and
removes credential copies. Source snapshots and sanitized process metadata are
retained separately from private credentials. Tests exercise parsing and input
boundaries; they do not substitute for native loading evidence.

## Official references

- [agy installation and authentication](https://www.antigravity.google/docs/install/)
- [agy agents manager](https://www.antigravity.google/docs/cli/commands/agents)
- [agy troubleshooting](https://www.antigravity.google/docs/cli/troubleshooting/)
- [agy changelog](https://www.antigravity.google/docs/changelog)
- [Cursor CLI parameters](https://cursor.com/docs/cli/reference/parameters)
- [Cursor configuration](https://cursor.com/docs/cli/reference/configuration)
- [Cursor hooks](https://cursor.com/docs/hooks)
- [Kitty keyboard protocol](https://sw.kovidgoyal.net/kitty/keyboard-protocol/)
- [XTerm control sequences](https://invisible-island.net/xterm/ctlseqs/ctlseqs.html)
- [Bubble Tea terminal initialization](https://github.com/charmbracelet/bubbletea/blob/main/tty.go)

Official documentation describes intended host behavior; the version-bound
reports above describe what was actually observed under isolation.

## Reproducing the bounded probes

These are operator verification helpers, not distributed AOK CLI commands. They
require macOS host permissions for PTYs and `sandbox-exec`; running inside a
second restrictive outer sandbox can fail before the native executable starts.
Keep the inner guard enabled. Package and binary hashes in the scripts are
pinned to the inspected versions; do not silently substitute another release.

```sh
python3 scripts/native-loading/prepare_agy_login.py --timeout 180
```

Run login preparation only with operator authorization to open the browser.
The printed private root may contain credentials and must never be committed.
If preparation reports `prompt-ready`, supply that root explicitly to
`probe-agy-macos-interactive-manager.py --isolated-login-root ROOT` along with
its required binary, AOK and evidence arguments. A prompt-ready marker expires
and never certifies backend authentication. Remove the owned login root after
verification, including credentials; preserve only the separate sanitized audit.
Failed/interrupted preparation removes its private HOME automatically.

Cursor accepts `--use-existing-cli-login` or an explicitly prepared
`--isolated-login-root`, plus required `--package`, `--aok` and `--evidence-out`.
Use `--trace-startup` only for diagnosis and label that evidence instrumented.
Use `--deny-git-network` only when explicitly recording that altered Git policy.
Do not interpret ordinary startup under either flag as native adapter completion.
No helper submits an ordinary prompt or selects a discovered agent.

Follow-up diagnostics retain the failing renderer function/line and, when it
matches a small numeric-only grammar, the unsupported CSI control signature.
They exclude terminal text and exception messages. This localized agy startup
queries and keyboard-mode negotiation separately from its authentication UI.
XTerm's `modifyOtherKeys` controls affect key encoding, while DECST8C resets the
standard eight-column tab stops; neither is ordinary prompt text. Parser support
must follow these documented semantics rather than treating all escape codes as
inert. Failed or ambiguous attempts still do not establish native loading.

The selected agy provider is a numbered `Google OAuth` entry, not a standalone
`Login` item; “Select login method” is the heading. Matching that heading as a
button would be incorrect. The isolated follow-up selected only the observed
Google OAuth row and reached the browser/code stage. OAuth completion and
subsequent `/agents` discovery require separate evidence.

The final Cursor screen parser accepts the observed keyboard and theme-reporting
controls. A header-only `/` probe never sends Enter and is not itself readiness
proof; readiness requires the actual slash-menu response. Successful backend
status, header rendering, or typing a draft cannot close the native-loading gate.

## Reviewed Cursor discovery conclusion

The [gate decision](evidence/native-adapters/cursor-project-discovery-decision-2026-10-18.json)
closes bounded project command-picker discovery for the pinned Cursor version.
The decisive oracle is the complete fixture description appearing only for the
positive project, with absent and blank-body controls suppressing it. Name echo
and the unmatched legacy menu marker are excluded. This finding supersedes the
preceding blocked diagnostic attempts; their original reports remain intact.

## agy manual OAuth follow-up and remaining blocker

The documented SSH code flow was exercised using explicitly simulated SSH
metadata, not an actual SSH connection. The parent opened only the complete
native Google OAuth URL; it did not exchange codes itself. A 160 by 80 terminal
was needed to show the code field below the wrapped URL. Earlier 80 by 24
attempts clipped that field. Standard device-attribute replies allowed terminal
initialization to finish.

The [file-channel failure](evidence/native-adapters/agy-login-attempt-lp9imnh7-2026-10-18.json)
stopped before code submission: a file-only input channel accidentally added
`None` to `select()`'s descriptor list. The condition is corrected and covered
by the file-channel regression test. The next
[submission](evidence/native-adapters/agy-login-attempt-qh34h1hs-2026-10-18.json)
encountered unsupported vertical absolute cursor positioning (`CSI d`); that
standard operation is now modeled with row clamping and column preservation.

The [fresh-code attempt](evidence/native-adapters/agy-login-attempt-bbte6y4v-2026-10-18.json)
submitted the operator-provided one-time code once. The native screen then showed
welcome/onboarding labels and a generic error, without terminal ambiguity.
No local ready prompt was established. The sanitized evidence cannot establish
whether token exchange or persistence succeeded, or identify the error's cause.
Its private HOME was removed, `/agents` was not sent, and neither backend
authentication nor native loading is claimed. That was the blocker at this historical checkpoint. The later
[onboarding investigation](agy-onboarding-verification.md) supersedes it.

Independent review tightened the current helper beyond those historical runs:
a standalone `Authorization code` title is insufficient to authorize input.
It now requires the native cursor inside a visible input box beneath the title,
with the copy-code/browser/paste-below instruction present. Explicit colon-ended
code-entry prompts remain supported. Unknown screens receive no input. These
post-run changes have regression coverage; their native behavior is unverified.

Private file input must be an owned, single-link regular file in an owned
private temporary directory outside the repository, with a final newline. The
helper waits for the recognized native field, removes the file before sending,
sends one code plus terminal Enter, clears its mutable buffer, and records only
a submission boolean. Cleanup also covers rejected owned regular files; it
never follows symlinks or deletes an outside path. TTY input disables echo.
No code, OAuth URL, token, account identity, or raw terminal text is retained
in repository reports. Historical source archives describe the code actually
executed, not subsequent fixes.

For an authorized manual flow, create an empty private code file first and run:

```sh
python3 scripts/native-loading/prepare_agy_login.py --timeout 300 \
  --manual-oauth --authorization-code-file /private/tmp/OWNED-DIRECTORY/code.txt
```

Complete the newest native browser flow and write the fresh code plus a newline
in that file while the bounded session is live. Do not commit the directory.
The current helper stops on the unresolved post-code screen; operator
investigation is still needed before a ready profile and manager verification
can be reported. A successful OAuth browser page alone cannot close this gate.

## Post-code onboarding and local manager investigation

The [onboarding report](agy-onboarding-verification.md) identifies the apparent
welcome error as theme-preview sample text, fixes the actual code-entry form,
and models only the observed Kitty keyboard-pop and cursor-style controls.
Native terms were submitted with optional sharing visibly disabled; trust was
confined to the owned temporary root. The original credential profile was removed.

A separate synthetic-provider harness now reaches `/agents` without inference.
Its positive, absent and malformed controls all show the default agent only,
including after a five-second observation period. The new report preserves
this loading blocker and documents explicit operator consent flags, source
snapshots, process cleanup and validation. Backend authentication is unverified.
