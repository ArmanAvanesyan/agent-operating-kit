package nativeparse

import (
	"bytes"
	"errors"
)

// stripJSONC removes comments and trailing commas only outside JSON strings.
// It deliberately leaves string contents and escapes byte-for-byte unchanged.
func stripJSONC(src []byte) ([]byte, error) {
	out := make([]byte, 0, len(src))
	inString, escaped := false, false
	for i := 0; i < len(src); {
		c := src[i]
		if inString {
			out = append(out, c)
			i++
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				inString = false
			}
			continue
		}
		if c == '"' {
			inString = true
			out = append(out, c)
			i++
			continue
		}
		if c == '/' && i+1 < len(src) && src[i+1] == '/' {
			out = append(out, ' ', ' ')
			i += 2
			for i < len(src) && src[i] != '\n' && src[i] != '\r' {
				out = append(out, ' ')
				i++
			}
			continue
		}
		if c == '/' && i+1 < len(src) && src[i+1] == '*' {
			out = append(out, ' ', ' ')
			i += 2
			closed := false
			for i < len(src) {
				if src[i] == '*' && i+1 < len(src) && src[i+1] == '/' {
					out = append(out, ' ', ' ')
					i += 2
					closed = true
					break
				}
				if src[i] == '\n' || src[i] == '\r' {
					out = append(out, src[i])
				} else {
					out = append(out, ' ')
				}
				i++
			}
			if !closed {
				return nil, errors.New("unterminated JSONC block comment")
			}
			continue
		}
		out = append(out, c)
		i++
	}
	// The JSON parser reports unterminated strings and bad escapes. Remove a
	// comma only when the next non-whitespace byte closes its current container.
	clean := make([]byte, 0, len(out))
	inString, escaped = false, false
	for i := 0; i < len(out); i++ {
		if inString {
			clean = append(clean, out[i])
			if escaped {
				escaped = false
			} else if out[i] == '\\' {
				escaped = true
			} else if out[i] == '"' {
				inString = false
			}
			continue
		}
		if out[i] == '"' {
			inString = true
			clean = append(clean, out[i])
			continue
		}
		if out[i] == ',' {
			j := i + 1
			for j < len(out) && (out[j] == ' ' || out[j] == '\t' || out[j] == '\r' || out[j] == '\n') {
				j++
			}
			k := i - 1
			for k >= 0 && (out[k] == ' ' || out[k] == '\t' || out[k] == '\r' || out[k] == '\n') {
				k--
			}
			if k >= 0 && !bytes.ContainsRune([]byte("[{,:"), rune(out[k])) && j < len(out) && (out[j] == ']' || out[j] == '}') {
				clean = append(clean, ' ')
				continue
			}
		}
		clean = append(clean, out[i])
	}
	// Preserve a distinct buffer: callers may hold both source and decoded data.
	return bytes.Clone(clean), nil
}
