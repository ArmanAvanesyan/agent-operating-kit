# agy malformed-YAML verification — 2026-10-18

The final native-loading blocker remains open for agy 1.4.2 Darwin x64.
Fresh isolated account-mode `/agents` discovers the valid AOK agent and omits
absent/malformed definitions. The malformed fixture produces no recognized
YAML/frontmatter rejection in either the settled panel or the bounded, readable
native CLI log. Its omission alone does not satisfy the existing strict gate.

This investigation adds a verified diagnostic channel and a conservative probe
extension. It does not change native rendering, weaken the three-control
comparator, or promote `HostLoading.Status`, which remains `not-run`. The overall
native-only release remains incomplete. Backend authentication, agent selection,
model inference, and runtime policy enforcement are not independently verified.

## What was checked

The [previous account-mode report](agy-account-verification.md) established
positive discovery but excluded native log reads. Its captured terminal frames
showed no explicit malformed-file rejection. The current gate requires both a
settled manager result and `parseErrorObserved: true` for the malformed control.

The official [agent manager reference](https://www.antigravity.google/docs/cli/commands/agents)
and [custom-agent reference](https://antigravity.google/docs/subagents?tab=cli)
describe discovery locations and frontmatter, but do not promise a visible
malformed-YAML error. Their absence of such a promise does not prove silent
rejection. Source references and scoped conclusions are recorded in
[sources.json](evidence/native-adapters/agy-malformed-2026-10-18/sources.json).

Read-only inspection of the pinned binary found frontmatter-related parser
strings. These are static labels, not evidence that the fixture exercised those
paths. No usable loader implementation was recovered from the stripped symbol
information. The binary was never modified or committed.

A no-account synthetic-provider run checked transient wire markers and bounded
legacy log diagnostics. It observed no parser marker and did not discover the
valid agent, so it cannot qualify native loading. A separately audited native
`--help` invocation confirmed the supported flag:

```text
--log-file    Override CLI log file path
```

The launcher can now opt into exactly `--log-file <owned-root>/native-parser.log`.
It accepts no arbitrary native argument or log destination through this option.
The synthetic-provider rerun proved that this fixed log exists and is readable,
but still supplied no parser markers. It remains a diagnostic control only.

## Account-mode result

A fresh user-completed sign-in reached a verified local prompt. Required terms
were accepted only with optional interaction sharing visibly unchecked; trust
was confined to owned roots. Three fresh copies of that unexpired temporary
profile then ran `/agents`, with at least five seconds of settled observation.
No new model/ordinary prompt or agent selection was submitted.

The [account parser controls](evidence/native-adapters/agy-malformed-2026-10-18/account-parser-controls.json)
first confirmed a readable, untruncated log without recognized parser markers.
The [coverage run](evidence/native-adapters/agy-malformed-2026-10-18/account-parser-coverage.json)
repeated the controls with bounded byte/row counts to distinguish an empty log
from a nonempty log without relevant diagnostics:

| Input | Custom name/description | Log bytes / rows | Recognized parser rows |
| --- | --- | --- | --- |
| Valid AOK-rendered fixture | Both present | 22,298 / 172 | 0 |
| No definition | Both absent | 24,206 / 184 | 0 |
| Invalid YAML | Both absent | 22,933 / 176 | 0 |

All three logs were `available` and `truncated: false`. No exact-fixture parser
rejection was attributed. Fixed-word context counts are substring row counts,
not error severities or proof of a particular internal code path. Generic
`error`/`load` text was not treated as a YAML rejection.

The inherited startup classifier also scans the whole accumulated wire stream,
not only the final rendered screen. Its saved fixed-word evidence for the
malformed account case contains none of `error`, `warning`, `failed`, `fatal`, or
`invalid`. A redraw regression confirms that a transient failure word survives
this classification after a screen clear. [Transient analysis](evidence/native-adapters/agy-malformed-2026-10-18/transient-analysis.json)
records that derived result. This excludes the recognized failure vocabulary in
the captured stream; it is not an exhaustive detector of every possible native
message or alternate output channel.

Both runs return `candidateNativeLoadingEvidence: false` and exit 1. They do
not establish whether agy silently rejects syntax, filters incomplete metadata,
or reports the failure through another channel. No supported standalone native
agent-definition validator was established by the inspected help/docs. This is
a limit of the tested interface/version and evidence, not a claim about all agy
versions. A future gate-closing change needs an observable, attributable native
rejection or a separately reviewed replacement acceptance contract supported by
stronger controls; no such replacement was made here.

## Diagnostic implementation and privacy

[`agy_parser_diagnostics.py`](../scripts/native-loading/agy_parser_diagnostics.py)
reads only `native-parser.log` directly under a canonical current-user-owned
mode-0700 root. It uses descriptor-relative, no-follow, nonblocking file access;
checks regular-file type, owner, single-link count and inode/device identity;
and reads at most 1 MiB plus a truncation sentinel. Symlinks, FIFOs, hardlinks,
unsafe roots, unrelated destinations and incomplete reads cannot qualify a
rejection. The final root identity is checked again after reading.

The helper returns only stable labels, counts, availability and attribution
booleans. It never returns native log text, account identifiers, tokens, codes,
or arbitrary exception messages. A parser phrase/YAML error and the exact
absolute fixture-path token must occur on the same row. Prefix/suffix paths,
cross-row coincidences, library messages without the fixture path and truncated
logs cannot attribute rejection. Static binary strings alone cannot qualify it.

`probe_agy_account_manager.py --parser-diagnostics` explicitly opts into this
reader. Native log collection happens after the owned native process group is
cleaned, before the copy is deleted. The existing default account probe still
retains no native log diagnostics. A complete exact-fixture diagnostic may set
`fixtureParserLogRejectionObserved`; the original final-frame flag is retained
as `terminalParseErrorObserved`. Authentication/error barriers remain separate
and unchanged. The full positive/absent/malformed comparator is still required.

The probe stores only aggregate diagnostics, then invalidates and removes each
copy in `finally`, including audit-processing failures. The source account
profile was removed after verification; [cleanup evidence](evidence/native-adapters/agy-malformed-2026-10-18/profile-cleanup.json)
records its unchanged original expiry and deletion. Original-home reads and
Keychain lookup were denied, and manager runs disabled browser launching.
Networking was allowed only as recorded for native account metadata/refresh;
this is not a network-enforcement claim. Raw terminal streams and log contents
were never written to repository evidence. The installed agy binary is unchanged.

## Reproduction and remaining limit

Build the Go AOK binary, prepare a fresh isolated account profile using the
approved [account login procedure](agy-account-verification.md#operator-reproduction),
then run the opt-in check with new output files:

```sh
python3 scripts/native-loading/probe_agy_account_manager.py \
  --aok /private/tmp/BUILT-AOK \
  --isolated-login-root /private/tmp/ROOT-FROM-LOGIN \
  --evidence-out docs/evidence/native-adapters/NEW-parser-controls.json \
  --accept-required-terms --disable-optional-sharing --trust-owned-fixtures \
  --parser-diagnostics
```

The source profile must be unexpired and prompt-ready; code submission and
consent require explicit operator authorization. Remove that source profile
after verification. The probe removes its copies. No global configuration,
installation, release publication, native agent execution or model call is part
of this procedure. These helpers remain operator verification tooling, separate
from the static Go agent commands.

[The evidence index](evidence/native-adapters/agy-malformed-2026-10-18/index.json)
binds observations, exact helper/driver snapshots, source hashes and cleanup.
[Validation](evidence/native-adapters/agy-malformed-2026-10-18/validation.json)
records regression checks. Historical wrappers and helper revisions are marked
separately; final coverage evidence names the exact diagnostic helper hash used
by that run.

Local validation passed: 252 Python tests (two skipped), 81 targeted tests, all Go packages and a focused agy/CLI rerun, a Go build used by native controls, kit validation, Python compilation, both proposal checkers and Codex render smoke. Evidence JSON, archived source hashes, fixture/binary hashes, cleanup and credential-pattern checks passed.
