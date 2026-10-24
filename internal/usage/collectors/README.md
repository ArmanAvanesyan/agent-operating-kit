# Usage collector fixtures and source boundary

These fixtures pin the shapes normalized by adapter `aok-usage-collector/v1`.
They are sanitized protocol examples, not evidence that live collection has
passed its host-version/auth-mode gate. `Describe` keeps `LiveGate: pending`.

| Fixture | Protocol/version pin | Source |
|---|---|---|
| `testdata/codex-app-server-v2.jsonl` | Codex App Server v2 JSON-RPC frames, documentation checked 2026-10-23 | [App Server protocol](https://learn.chatgpt.com/docs/app-server); [official v2 thread protocol](https://github.com/openai/codex/blob/main/codex-rs/app-server-protocol/src/protocol/v2/thread.rs); [official token counter implementation](https://github.com/openai/codex/blob/main/codex-rs/protocol/src/protocol.rs) |
| `testdata/claude-statusline-2.1.284.json` | Claude Code status-line schema v2.1.284, documentation checked 2026-10-23 | [Status-line documentation](https://code.claude.com/docs/en/statusline) |

Codex `thread/tokenUsage/updated.tokenUsage.total` is a cumulative thread
counter; `last` is the increment represented by that update. The adapter records
only `total`, and marks cached input as a subset of input and reasoning output as
a subset of output. `account/rateLimits/read` replies and the documented
`account/rateLimits/updated` notification are observation markers. Rate-limit
percentages remain percentage points and are never converted into tokens.
Quota `revision` is zero unless the caller uses `NormalizeCapture` or an
`OrderedReadOnlyTransport` that supplies a positive serialized capture ordinal
for the exact bound stream. That ordinal is local ordering metadata, not a
provider revision; callers must not synthesize it from request completion or
arrival races. Without it, conflicting observations remain ambiguous.

Claude context token fields describe the live context for the most recent API
response, not total session consumption, so this adapter does not import them
as session usage. The session cost is an estimated cumulative USD value. Status-line
redraws do not prove that rate-limit values were refreshed, so imported Claude
quota rows intentionally have unknown freshness and no inferred expiration.
Gateway spend percentages and the separately refreshed USD estimate are kept in
distinct constraints; the USD amount is marked estimated and is never derived
from percentage points.

The six other hosts return explicit unknown discovery declarations. No host
process, login, credential store, model invocation, dashboard scrape, or
unverified endpoint is used. Codex live reads are possible only through the
injected `ReadOnlyTransport`, which permits the single documented
`account/rateLimits/read` operation with a five-second context bound.
