# Structural analysis with Rust CLI tools

Use the smallest suitable query:

| Question | Backend | Evidence |
|---|---|---|
| Where is a name/string? | ripgrep (`rg`) | Text matches |
| Where is this syntax? | ast-grep | AST-pattern matches |
| Which declarations/imports are present? | ast-grep outline, if supported | File outline |
| What is the file's language/code composition? | Tokei | Statistics, not architectural importance |

The wrapper stages one explicitly selected source file in a temporary workspace, isolates
ast-grep project configuration and requests JSON. It never passes rewrite/update flags.
It rejects symlink paths, outside-root paths, oversized files and non-source extensions. Extensionless Python/shell scripts require a recognized
shebang and are staged with a matching language suffix; they are never executed.
Results retain the original relative filename and tool version; backend JSON shapes remain
backend-specific. Time/output failures are incomplete, not empty success.

```bash
python3 scripts/structure.py /path/to/repo --file src/server.py --mode text --pattern 'route'
python3 scripts/structure.py /path/to/repo --file src/server.py --mode pattern --lang python --pattern '$APP.get($$$ARGS)'
python3 scripts/structure.py /path/to/repo --file src/server.py --mode outline --lang python
python3 scripts/structure.py /path/to/repo --file src/server.py --mode stats
```

Run from this skill directory or use the script's absolute path. Tool discovery uses PATH:
`rg`, `ast-grep` (not ambiguous `sg`) and `tokei`. An unsupported outline command is reported
explicitly. No tool is auto-installed. Stage/query focused files rather than bypassing
repository exclusions recursively. The wrapper's fixed language-extension scope is smaller
than the backends' full support; use an independently checked tool directly when necessary.

For Go/Python/TypeScript, select a small representative file and an appropriate language
pattern first. Expand only after confirming what the query matches and misses. Comments,
strings, aliases, decorators, generated code and dynamic registration need interpretation.
Imports do not prove runtime calls; no match in one file is not repository-wide absence.
Use compiler/LSP navigation for resolved references and tests/logs for runtime evidence.

Optional future work: evaluate Probe retrieval against this baseline. It is not integrated.
Rust projects can use Cargo metadata/tree and rust-analyzer separately when relevant;
this skill does not require Rust projects or a Rust toolchain just to inspect files.

Primary tool references (checked 2026-10-15):

- https://github.com/BurntSushi/ripgrep
- https://ast-grep.github.io/reference/cli/run
- https://ast-grep.github.io/reference/cli/outline
- https://github.com/XAMPPRocky/tokei
