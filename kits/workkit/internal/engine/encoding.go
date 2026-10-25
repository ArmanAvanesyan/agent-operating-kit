package engine

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	jcs "github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
	"gopkg.in/yaml.v3"
)

const MaxDocumentBytes = 4 << 20
const MaxDepth = 64

var numberSyntax = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?$`)

func Hash(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func Numeric(s string) error {
	if len(s) > 1024 || !numberSyntax.MatchString(s) {
		return errors.New("invalid or oversized JSON number")
	}
	if i := strings.IndexAny(s, "eE"); i >= 0 {
		e, err := strconv.Atoi(s[i+1:])
		if err != nil || e < -10000 || e > 10000 {
			return errors.New("numeric exponent limit")
		}
	}
	return nil
}

// ValidateUnicode rejects malformed escaped UTF-16 instead of json.Unmarshal's
// replacement-character recovery. Decoded string identities must be exact.
func ValidateUnicode(b []byte) error {
	if !utf8.Valid(b) || bytes.HasPrefix(b, []byte{0xef, 0xbb, 0xbf}) {
		return errors.New("invalid UTF-8 or BOM")
	}
	in := false
	for i := 0; i < len(b); i++ {
		if b[i] == '"' {
			in = !in
			continue
		}
		if !in || b[i] != '\\' {
			continue
		}
		i++
		if i >= len(b) {
			return errors.New("truncated string escape")
		}
		if b[i] != 'u' {
			continue
		}
		if i+4 >= len(b) {
			return errors.New("truncated Unicode escape")
		}
		v, err := strconv.ParseUint(string(b[i+1:i+5]), 16, 16)
		if err != nil {
			return err
		}
		i += 4
		if v >= 0xdc00 && v <= 0xdfff {
			return errors.New("lone low surrogate")
		}
		if v >= 0xd800 && v <= 0xdbff {
			if i+6 >= len(b) || b[i+1] != '\\' || b[i+2] != 'u' {
				return errors.New("lone high surrogate")
			}
			low, e := strconv.ParseUint(string(b[i+3:i+7]), 16, 16)
			if e != nil || low < 0xdc00 || low > 0xdfff {
				return errors.New("invalid surrogate pair")
			}
			i += 6
		}
	}
	return nil
}
func JSONValue(b []byte) (any, error) {
	if len(b) > MaxDocumentBytes {
		return nil, errors.New("document exceeds 4 MiB")
	}
	if err := ValidateUnicode(b); err != nil {
		return nil, err
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	var parse func(int) (any, error)
	parse = func(depth int) (any, error) {
		if depth > MaxDepth {
			return nil, errors.New("nesting exceeds 64")
		}
		token, err := d.Token()
		if err != nil {
			return nil, err
		}
		if delimiter, ok := token.(json.Delim); ok {
			switch delimiter {
			case '{':
				m := map[string]any{}
				for d.More() {
					key, e := d.Token()
					if e != nil {
						return nil, e
					}
					k, ok := key.(string)
					if !ok {
						return nil, errors.New("non-string key")
					}
					if _, exists := m[k]; exists {
						return nil, errors.New("duplicate JSON key")
					}
					v, e := parse(depth + 1)
					if e != nil {
						return nil, e
					}
					m[k] = v
				}
				_, err = d.Token()
				return m, err
			case '[':
				a := []any{}
				for d.More() {
					v, e := parse(depth + 1)
					if e != nil {
						return nil, e
					}
					a = append(a, v)
				}
				_, err = d.Token()
				return a, err
			default:
				return nil, errors.New("unexpected delimiter")
			}
		}
		if n, ok := token.(json.Number); ok {
			if e := Numeric(string(n)); e != nil {
				return nil, e
			}
		}
		return token, nil
	}
	value, err := parse(0)
	if err != nil {
		return nil, err
	}
	if _, e := d.Token(); e != io.EOF {
		return nil, errors.New("trailing or malformed JSON")
	}
	return value, nil
}
func yamlValue(b []byte) (any, error) {
	if len(b) > MaxDocumentBytes || !utf8.Valid(b) || bytes.HasPrefix(b, []byte{0xef, 0xbb, 0xbf}) {
		return nil, errors.New("invalid YAML encoding/size")
	}
	var n yaml.Node
	d := yaml.NewDecoder(bytes.NewReader(b))
	if err := d.Decode(&n); err != nil {
		return nil, err
	}
	var extra yaml.Node
	if e := d.Decode(&extra); e != io.EOF {
		return nil, errors.New("multiple YAML documents")
	}
	var walk func(*yaml.Node, int) (any, error)
	walk = func(n *yaml.Node, depth int) (any, error) {
		if depth > MaxDepth || n.Anchor != "" || n.Kind == yaml.AliasNode || n.Style&yaml.TaggedStyle != 0 {
			return nil, errors.New("YAML depth/anchor/alias/tag rejected")
		}
		switch n.Kind {
		case yaml.DocumentNode:
			if len(n.Content) != 1 {
				return nil, errors.New("empty YAML document")
			}
			return walk(n.Content[0], depth)
		case yaml.MappingNode:
			m := map[string]any{}
			for i := 0; i < len(n.Content); i += 2 {
				k := n.Content[i]
				if k.Kind != yaml.ScalarNode || k.Tag != "!!str" || k.Anchor != "" || k.Style&yaml.TaggedStyle != 0 || k.Value == "<<" {
					return nil, errors.New("YAML key/merge rejected")
				}
				if _, ok := m[k.Value]; ok {
					return nil, errors.New("duplicate YAML key")
				}
				v, e := walk(n.Content[i+1], depth+1)
				if e != nil {
					return nil, e
				}
				m[k.Value] = v
			}
			return m, nil
		case yaml.SequenceNode:
			a := []any{}
			for _, child := range n.Content {
				v, e := walk(child, depth+1)
				if e != nil {
					return nil, e
				}
				a = append(a, v)
			}
			return a, nil
		case yaml.ScalarNode:
			switch n.Tag {
			case "!!str":
				return n.Value, nil
			case "!!null":
				if n.Value != "null" {
					return nil, errors.New("use explicit JSON null")
				}
				return nil, nil
			case "!!bool":
				if n.Value != "true" && n.Value != "false" {
					return nil, errors.New("ambiguous YAML boolean")
				}
				return n.Value == "true", nil
			case "!!int", "!!float":
				if err := Numeric(n.Value); err != nil {
					return nil, err
				}
				return json.Number(n.Value), nil
			}
		}
		return nil, errors.New("YAML value outside restricted JSON model")
	}
	return walk(&n, 0)
}
func Canonical(b []byte, format string) (map[string]any, []byte, error) {
	var v any
	var err error
	switch format {
	case "json":
		v, err = JSONValue(b)
	case "yaml", "yml":
		v, err = yamlValue(b)
	default:
		return nil, nil, errors.New("unsupported encoding")
	}
	if err != nil {
		return nil, nil, err
	}
	object, ok := v.(map[string]any)
	if !ok {
		return nil, nil, errors.New("resource must be an object")
	}
	normalized, err := json.Marshal(v)
	if err != nil {
		return nil, nil, err
	}
	c, err := jcs.Transform(normalized)
	if err != nil {
		return nil, nil, err
	}
	cv, err := JSONValue(c)
	if err != nil {
		return nil, nil, err
	}
	var exact func(any, any) error
	exact = func(a, b any) error {
		switch x := a.(type) {
		case json.Number:
			y, ok := b.(json.Number)
			if !ok {
				return errors.New("numeric shape changed")
			}
			l, ok := new(big.Rat).SetString(string(x))
			if !ok {
				return errors.New("invalid decimal")
			}
			r, ok := new(big.Rat).SetString(string(y))
			if !ok || l.Cmp(r) != 0 {
				return errors.New("number would round; use exact decimal wrapper in a supported value position")
			}
		case map[string]any:
			y, ok := b.(map[string]any)
			if !ok {
				return errors.New("object changed")
			}
			for k, v := range x {
				if e := exact(v, y[k]); e != nil {
					return e
				}
			}
		case []any:
			y, ok := b.([]any)
			if !ok || len(y) != len(x) {
				return errors.New("array changed")
			}
			for i, v := range x {
				if e := exact(v, y[i]); e != nil {
					return e
				}
			}
		}
		return nil
	}
	if err = exact(v, cv); err != nil {
		return nil, nil, err
	}
	return object, c, nil
}
func Map(v any) map[string]any { m, _ := v.(map[string]any); return m }
func List(v any) []any         { a, _ := v.([]any); return a }
func String(v any) string      { s, _ := v.(string); return s }
func Encode(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("internal non-JSON value: %v", err))
	}
	return b
}
