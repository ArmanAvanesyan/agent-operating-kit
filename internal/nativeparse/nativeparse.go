// Package nativeparse reads bounded, inert native configuration data into JSON-compatible maps.
package nativeparse

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"reflect"
	"strconv"
	"strings"
	"unicode/utf8"

	toml "github.com/pelletier/go-toml/v2"
	"gopkg.in/yaml.v3"
)

const (
	MaxDocumentBytes = 4 << 20
	MaxDepth         = 64
)

var ErrUnsupported = errors.New("unsupported native value")

// Decode parses JSON, JSONC, YAML, or TOML into an inert object map. Unknown
// fields are retained. JSON numbers and native integers use json.Number to avoid loss
// of integer precision; native fractional values use finite float64.
func Decode(format string, data []byte) (map[string]any, error) {
	if err := checkInput(data); err != nil {
		return nil, err
	}
	format = strings.ToLower(strings.TrimSpace(format))
	var v any
	var err error
	switch format {
	case "json":
		v, err = decodeJSON(data)
	case "jsonc":
		var clean []byte
		clean, err = stripJSONC(data)
		if err == nil {
			v, err = decodeJSON(clean)
		}
	case "yaml", "yml":
		v, err = decodeYAML(data)
	case "toml":
		v, err = decodeTOML(data)
	default:
		return nil, fmt.Errorf("unsupported format %q", format)
	}
	if err != nil {
		return nil, err
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, errors.New("document root must be an object")
	}
	if err := checkValue(m, 1); err != nil {
		return nil, err
	}
	return m, nil
}

// Frontmatter parses a leading YAML --- block and returns its metadata plus
// the untouched Markdown body. The body is copied so callers may safely retain it.
func Frontmatter(data []byte) (map[string]any, []byte, error) {
	if err := checkInput(data); err != nil {
		return nil, nil, err
	}
	if !utf8.Valid(data) {
		return nil, nil, errors.New("input is not valid UTF-8")
	}
	if bytes.HasPrefix(data, []byte{0xef, 0xbb, 0xbf}) {
		return nil, nil, errors.New("UTF-8 BOM is not permitted")
	}
	lines := bytes.SplitAfter(data, []byte("\n"))
	if len(lines) == 0 || strings.TrimSpace(string(lines[0])) != "---" {
		return nil, nil, errors.New("Markdown must begin with a YAML frontmatter delimiter")
	}
	offset := len(lines[0])
	for i := 1; i < len(lines); i++ {
		line := strings.TrimSpace(string(lines[i]))
		if line == "---" || line == "..." {
			meta, err := Decode("yaml", data[len(lines[0]):offset])
			if err != nil {
				return nil, nil, err
			}
			body := append([]byte(nil), data[offset+len(lines[i]):]...)
			return meta, body, nil
		}
		offset += len(lines[i])
	}
	return nil, nil, errors.New("unterminated YAML frontmatter")
}

// Encode serializes a JSON-compatible object using JSON, YAML, or TOML.
func Encode(format string, value map[string]any) ([]byte, error) {
	if value == nil {
		return nil, errors.New("document object must not be nil")
	}
	if err := checkValue(value, 1); err != nil {
		return nil, err
	}
	format = strings.ToLower(strings.TrimSpace(format))
	switch format {
	case "json", "jsonc":
		return boundedEncode(json.MarshalIndent(value, "", "  "))
	case "yaml", "yml":
		normalized, err := normalizeForFormat(value, false)
		if err != nil {
			return nil, err
		}
		return boundedEncode(yaml.Marshal(normalized))
	case "toml":
		normalized, err := normalizeForFormat(value, true)
		if err != nil {
			return nil, err
		}
		return boundedEncode(toml.Marshal(normalized))
	default:
		return nil, fmt.Errorf("unsupported format %q", format)
	}
}

func boundedEncode(data []byte, err error) ([]byte, error) {
	if err != nil {
		return nil, err
	}
	if len(data) > MaxDocumentBytes {
		return nil, fmt.Errorf("encoded document exceeds %d bytes", MaxDocumentBytes)
	}
	return data, nil
}

func normalizeForFormat(v any, tomlFormat bool) (any, error) {
	switch x := v.(type) {
	case map[string]any:
		m := make(map[string]any, len(x))
		for k, v := range x {
			y, err := normalizeForFormat(v, tomlFormat)
			if err != nil {
				return nil, err
			}
			m[k] = y
		}
		return m, nil
	case []any:
		a := make([]any, len(x))
		for i, v := range x {
			y, err := normalizeForFormat(v, tomlFormat)
			if err != nil {
				return nil, err
			}
			a[i] = y
		}
		return a, nil
	case json.Number:
		if i, err := strconv.ParseInt(string(x), 10, 64); err == nil {
			return i, nil
		}
		integerLexeme := !strings.ContainsAny(string(x), ".eE")
		if !tomlFormat {
			if u, err := strconv.ParseUint(string(x), 10, 64); err == nil {
				return u, nil
			}
		} else if integerLexeme {
			return nil, fmt.Errorf("%w: TOML integers must fit signed 64 bits", ErrUnsupported)
		}
		if integerLexeme {
			return nil, fmt.Errorf("%w: integer exceeds supported range", ErrUnsupported)
		}
		f, err := strconv.ParseFloat(string(x), 64)
		if err != nil || math.IsInf(f, 0) || math.IsNaN(f) {
			return nil, fmt.Errorf("%w: number %q cannot be emitted as %s", ErrUnsupported, x, map[bool]string{true: "TOML", false: "YAML"}[tomlFormat])
		}
		shortest := strconv.FormatFloat(f, 'g', -1, 64)
		originalRat, ok := new(big.Rat).SetString(string(x))
		if !ok {
			return nil, fmt.Errorf("%w: decimal number %q", ErrUnsupported, x)
		}
		shortestRat, ok := new(big.Rat).SetString(shortest)
		if !ok || originalRat.Cmp(shortestRat) != 0 {
			return nil, fmt.Errorf("%w: decimal number %q cannot be represented without precision loss", ErrUnsupported, x)
		}
		return f, nil
	case nil:
		if tomlFormat {
			return nil, fmt.Errorf("%w: TOML has no null value", ErrUnsupported)
		}
		return nil, nil
	case string, bool, int, int64, uint64, float64:
		return x, nil
	default:
		return nil, fmt.Errorf("%w: %T", ErrUnsupported, v)
	}
}

func checkInput(data []byte) error {
	if len(data) > MaxDocumentBytes {
		return fmt.Errorf("document exceeds %d bytes", MaxDocumentBytes)
	}
	if !utf8.Valid(data) {
		return errors.New("input is not valid UTF-8")
	}
	if bytes.HasPrefix(data, []byte{0xef, 0xbb, 0xbf}) {
		return errors.New("UTF-8 BOM is not permitted")
	}
	return nil
}

func decodeJSON(data []byte) (any, error) {
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	v, err := jsonValue(d, 1)
	if err != nil {
		return nil, err
	}
	var extra any
	if err = d.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, errors.New("multiple JSON values")
		}
		return nil, fmt.Errorf("trailing JSON data: %w", err)
	}
	return v, nil
}

func jsonValue(d *json.Decoder, depth int) (any, error) {
	if depth > MaxDepth {
		return nil, fmt.Errorf("nesting exceeds %d levels", MaxDepth)
	}
	t, err := d.Token()
	if err != nil {
		return nil, err
	}
	switch x := t.(type) {
	case json.Delim:
		switch x {
		case '{':
			m := map[string]any{}
			for d.More() {
				kt, err := d.Token()
				if err != nil {
					return nil, err
				}
				k, ok := kt.(string)
				if !ok {
					return nil, errors.New("object key must be a string")
				}
				if _, exists := m[k]; exists {
					return nil, fmt.Errorf("duplicate object key %q", k)
				}
				v, err := jsonValue(d, depth+1)
				if err != nil {
					return nil, err
				}
				m[k] = v
			}
			end, err := d.Token()
			if err != nil || end != json.Delim('}') {
				return nil, errors.New("unterminated JSON object")
			}
			return m, nil
		case '[':
			a := []any{}
			for d.More() {
				v, err := jsonValue(d, depth+1)
				if err != nil {
					return nil, err
				}
				a = append(a, v)
			}
			end, err := d.Token()
			if err != nil || end != json.Delim(']') {
				return nil, errors.New("unterminated JSON array")
			}
			return a, nil
		default:
			return nil, errors.New("unexpected JSON delimiter")
		}
	case json.Number:
		return x, nil
	case string:
		if !utf8.ValidString(x) {
			return nil, errors.New("invalid UTF-8 string")
		}
		return x, nil
	case bool, nil:
		return x, nil
	default:
		return nil, fmt.Errorf("unsupported JSON token %T", t)
	}
}

func decodeYAML(data []byte) (any, error) {
	d := yaml.NewDecoder(bytes.NewReader(data))
	var root yaml.Node
	if err := d.Decode(&root); err != nil {
		return nil, err
	}
	if len(root.Content) == 0 {
		return map[string]any{}, nil
	}
	var extra yaml.Node
	if err := d.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, errors.New("multiple YAML documents")
		}
		return nil, err
	}
	return yamlNode(root.Content[0], 1)
}

func yamlNode(n *yaml.Node, depth int) (any, error) {
	if depth > MaxDepth {
		return nil, fmt.Errorf("nesting exceeds %d levels", MaxDepth)
	}
	if n.Style&yaml.TaggedStyle != 0 {
		return nil, fmt.Errorf("%w: explicit YAML tags", ErrUnsupported)
	}
	if n.Anchor != "" || n.Kind == yaml.AliasNode || n.Alias != nil {
		return nil, errors.New("YAML anchors and aliases are not permitted")
	}
	if n.Kind == yaml.MappingNode {
		m := map[string]any{}
		for i := 0; i < len(n.Content); i += 2 {
			k := n.Content[i]
			if k.Anchor != "" || k.Kind == yaml.AliasNode || k.Alias != nil {
				return nil, errors.New("YAML anchors and aliases are not permitted")
			}
			if k.Style&yaml.TaggedStyle != 0 {
				return nil, fmt.Errorf("%w: explicit YAML tags", ErrUnsupported)
			}
			if k.Kind != yaml.ScalarNode || k.Tag != "!!str" {
				return nil, errors.New("YAML mapping keys must be strings")
			}
			if k.Value == "<<" || k.Tag == "!!merge" {
				return nil, errors.New("YAML merge keys are not permitted")
			}
			if _, exists := m[k.Value]; exists {
				return nil, fmt.Errorf("duplicate YAML key %q", k.Value)
			}
			v, err := yamlNode(n.Content[i+1], depth+1)
			if err != nil {
				return nil, err
			}
			m[k.Value] = v
		}
		return m, nil
	}
	if n.Kind == yaml.SequenceNode {
		a := make([]any, 0, len(n.Content))
		for _, c := range n.Content {
			v, err := yamlNode(c, depth+1)
			if err != nil {
				return nil, err
			}
			a = append(a, v)
		}
		return a, nil
	}
	if n.Kind != yaml.ScalarNode {
		return nil, errors.New("unsupported YAML node")
	}
	if n.Style&yaml.TaggedStyle != 0 {
		return nil, fmt.Errorf("%w: explicit YAML tags", ErrUnsupported)
	}
	switch n.Tag {
	case "!!str":
		return n.Value, nil
	case "!!null":
		return nil, nil
	case "!!bool":
		var b bool
		if err := n.Decode(&b); err != nil {
			return nil, err
		}
		return b, nil
	case "!!int":
		var v any
		if err := n.Decode(&v); err != nil {
			return nil, err
		}
		switch v.(type) {
		case int, int64, uint64:
			return json.Number(fmt.Sprint(v)), nil
		default:
			return nil, fmt.Errorf("%w: YAML integer is outside supported integer range", ErrUnsupported)
		}
	case "!!float":
		// yaml.v3 can resolve an oversized integer-looking scalar as a float;
		// reject that coercion instead of silently losing integer precision.
		if !strings.ContainsAny(n.Value, ".eE") {
			if _, err := strconv.ParseInt(n.Value, 0, 64); err != nil {
				if _, uintErr := strconv.ParseUint(n.Value, 0, 64); uintErr != nil {
					return nil, fmt.Errorf("%w: YAML integer is outside supported integer range", ErrUnsupported)
				}
			}
		}
		var f float64
		if err := n.Decode(&f); err != nil {
			return nil, err
		}
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return nil, errors.New("non-finite numbers are not permitted")
		}
		return f, nil
	case "!!timestamp":
		return nil, fmt.Errorf("%w: YAML timestamps", ErrUnsupported)
	default:
		return nil, fmt.Errorf("%w: YAML tag %q", ErrUnsupported, n.Tag)
	}
}

func decodeTOML(data []byte) (any, error) {
	var m map[string]any
	if err := toml.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return normalizeTOML(m, 1)
}

func normalizeTOML(v any, depth int) (any, error) {
	if depth > MaxDepth {
		return nil, fmt.Errorf("nesting exceeds %d levels", MaxDepth)
	}
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, v := range x {
			z, err := normalizeTOML(v, depth+1)
			if err != nil {
				return nil, err
			}
			out[k] = z
		}
		return out, nil
	case []any:
		out := make([]any, len(x))
		for i, v := range x {
			z, err := normalizeTOML(v, depth+1)
			if err != nil {
				return nil, err
			}
			out[i] = z
		}
		return out, nil
	case string, bool:
		return x, nil
	case int64:
		return json.Number(strconv.FormatInt(x, 10)), nil
	case uint64:
		return json.Number(strconv.FormatUint(x, 10)), nil
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return nil, errors.New("non-finite numbers are not permitted")
		}
		return x, nil
	default:
		if t := reflect.TypeOf(v); t != nil && (strings.Contains(strings.ToLower(t.String()), "datetime") || strings.Contains(strings.ToLower(t.String()), "date")) {
			return nil, fmt.Errorf("%w: TOML date/time values", ErrUnsupported)
		}
		return nil, fmt.Errorf("%w: TOML value %T", ErrUnsupported, v)
	}
}

func checkValue(v any, depth int) error {
	if depth > MaxDepth {
		return fmt.Errorf("nesting exceeds %d levels", MaxDepth)
	}
	switch x := v.(type) {
	case map[string]any:
		for k, v := range x {
			if !utf8.ValidString(k) {
				return errors.New("invalid UTF-8 object key")
			}
			if err := checkValue(v, depth+1); err != nil {
				return err
			}
		}
	case []any:
		for _, v := range x {
			if err := checkValue(v, depth+1); err != nil {
				return err
			}
		}
	case json.Number:
		if _, err := strconv.ParseFloat(string(x), 64); err != nil {
			return fmt.Errorf("invalid number %q", x)
		}
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return errors.New("non-finite numbers are not permitted")
		}
	case string:
		if !utf8.ValidString(x) {
			return errors.New("invalid UTF-8 string")
		}
	case bool, nil:
	default:
		return fmt.Errorf("%w: %T", ErrUnsupported, v)
	}
	return nil
}
