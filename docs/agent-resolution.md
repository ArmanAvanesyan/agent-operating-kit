# Static agent resolution

The Go binary provides:

```text
aok agent resolve /path/to/corpus/assignment.json --out /path/to/new-report --json
```

Use `--root /path/to/corpus` when the assignment is nested. Otherwise its parent
is the corpus root. This command is not available in the contributor Python
entrypoint. It does not launch a client, run imported commands, access credentials,
probe tools, or install anything.

Resolution captures a private copy of the corpus and validates that copy. The
source manifest conservatively hashes every regular corpus file, including
supporting assets and transitive local schemas. Thus unrelated files in this
explicit corpus can change the digest. Keep the corpus narrow: at most 512 files,
64 MiB in total, and 4 MiB per file. Symlinks and special files are rejected.
The digest uses
stable ordering and contains no timestamp or machine-specific root path. The
snapshot is evidence of captured bytes, not an atomic filesystem transaction
against concurrent writers or a signed attestation.

The report selects pinned definitions, combines instruction/output defaults with
assignment parameters, and intersects declared agent, execution-profile and
assignment permission ceilings. Omitted restrictions inherit; explicit empty
sets deny access. Symbolic roots and host names are compared exactly; no shell,
environment variable, wildcard or template expression is evaluated. Environment
network denial further restricts the result. Policy calculation is not runtime
enforcement or authorization.

The current readiness and native compatibility results remain blocked because
host availability and enforcement have not been verified. A statically resolved
snapshot can therefore accompany a blocked result. Later native adapters must
supply evidence rather than treating syntax acceptance as enforcement.

`--out` is required and must name a new directory with an existing real parent.
Its path must not overlap the input corpus or traverse symlinks. The report is
written as `resolution.json`, with private directory/file permissions, including
blocked results. Existing directories are never reused or overwritten. An unsafe
output location is not written. `--json` also prints the report for automation.
Exit codes are 0 for successful resolution, 1 for invalid or blocked results,
and 2 for command misuse. Current enforcement gaps prevent a runnable success.

Source hashes reveal changes, not source contents. Configuration values are
user-authored declarations; use credential references, never literal secrets.
The resolver does not expand credential references or include process environment
variables. Protect report directories according to their configuration contents.
