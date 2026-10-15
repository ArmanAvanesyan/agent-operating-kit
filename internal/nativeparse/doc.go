/*
Package nativeparse provides bounded conversion of native text documents to
inert JSON-compatible object maps. Decode retains unknown keys and does not
interpret commands, expand environment references, or fetch external schemas.

Input is limited to 4 MiB, must be valid UTF-8 without a BOM, and may be nested
at most 64 levels. JSON decoding rejects duplicate keys at every object depth,
non-object roots, and trailing values. JSONC supports line/block comments and
trailing commas outside strings, then applies the same strict JSON validation.
YAML rejects duplicate keys, all explicit tags, anchors, aliases, merge keys,
and timestamp scalars. TOML rejects duplicate keys through its parser and
rejects date/time values. Both native formats reject values that cannot be
represented by the JSON-compatible model. Parsed numbers use json.Number where
needed to preserve integer precision; integer-looking YAML values beyond the
supported integer range fail instead of being coerced to floats. Decimal
json.Number values that cannot be emitted without precision loss are rejected.

Frontmatter requires an opening and closing YAML delimiter, parses only its
metadata, and returns the Markdown body unchanged. Encode emits JSON, YAML, or
TOML; TOML has no null representation and only supports signed 64-bit integers.
*/
package nativeparse
