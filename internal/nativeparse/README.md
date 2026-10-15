# Inert native parsing

This Go package parses native configuration data without executing commands,
expanding variables, resolving dependencies, or writing configuration. It does
not implement a client adapter or prove that a client loads a document.

- `Decode` supports JSON, JSONC, YAML and TOML object roots. Unknown fields remain
  in the returned object. JSONC accepts comments and trailing commas, not missing
  values. JSON and integer values retain `json.Number` precision; YAML/TOML
  fractional values use the pinned libraries' finite float64 representation.
- `Frontmatter` requires leading YAML delimiters and returns metadata plus an
  unchanged copy of the Markdown body.
- `Encode` supports JSON, YAML and TOML. It rejects unsupported values, including
  TOML nulls, integers beyond format limits, and decimal `json.Number` values
  that would change when converted for YAML/TOML output.

Inputs must be UTF-8 without BOM, at most 4 MiB and at most 64 nested levels.
Duplicate keys, extra documents/values, YAML explicit tags, anchors, aliases,
merge keys, nonfinite values and native date/time values are rejected. Empty
YAML frontmatter must contain an explicit object rather than an empty document.

Formatting, comments and scalar spelling are not represented in decoded maps.
Adapters must retain original bytes and hashes in provenance sidecars, and must
not claim byte-preserving round trips from `Encode`. A later conformance layer
must detect any unsupported semantic loss before export. Date/time or other
unsupported values block import; they are never silently coerced into strings.
