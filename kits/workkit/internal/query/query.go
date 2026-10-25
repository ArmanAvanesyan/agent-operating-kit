// Package query implements a bounded, non-executable, three-valued query DSL.
package query

import (
	"encoding/json"
	"errors"
	r "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/readv1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/engine"
	"math/big"

	"strings"
	"unicode/utf8"
)

type Truth int

const (
	Unknown Truth = iota
	False
	True
)

type Node struct {
	op, field   string
	value       any
	left, right *Node
}
type token struct {
	kind, text string
	value      any
}
type parser struct {
	tokens     []token
	pos, depth int
	fields     map[string]r.Field
}

func lex(s string) ([]token, error) {
	if len(s) > 16384 || !utf8.ValidString(s) {
		return nil, errors.New("query encoding/size")
	}
	out := []token{}
	for i := 0; i < len(s); {
		c := s[i]
		if strings.ContainsRune(" \t\r\n", rune(c)) {
			i++
			continue
		}
		if len(out) >= 512 {
			return nil, errors.New("query token limit")
		}
		if strings.ContainsRune("()[],", rune(c)) {
			out = append(out, token{kind: string(c), text: string(c)})
			i++
			continue
		}
		if i+1 < len(s) && (s[i:i+2] == "==" || s[i:i+2] == "!=") {
			out = append(out, token{kind: s[i : i+2]})
			i += 2
			continue
		}
		if c == '"' {
			j := i + 1
			for j < len(s) {
				if s[j] == '\\' {
					j += 2
					continue
				}
				if s[j] == '"' {
					break
				}
				j++
			}
			if j >= len(s) {
				return nil, errors.New("unterminated query string")
			}
			raw := []byte(s[i : j+1])
			v, e := engine.JSONValue(raw)
			if e != nil {
				return nil, e
			}
			out = append(out, token{kind: "literal", value: v})
			i = j + 1
			continue
		}
		j := i
		for j < len(s) && !strings.ContainsRune(" \t\r\n()[],=!", rune(s[j])) {
			j++
		}
		if j == i {
			return nil, errors.New("query token invalid")
		}
		word := s[i:j]
		switch word {
		case "and", "or", "not", "contains", "in":
			out = append(out, token{kind: word})
		case "true", "false", "null":
			v, _ := engine.JSONValue([]byte(word))
			out = append(out, token{kind: "literal", value: v})
		default:
			if c == '-' || c >= '0' && c <= '9' {
				v, e := engine.JSONValue([]byte(word))
				if e != nil {
					return nil, e
				}
				out = append(out, token{kind: "literal", value: v})
			} else {
				out = append(out, token{kind: "field", text: word})
			}
		}
		i = j
	}
	return out, nil
}
func Parse(q r.Query) (*Node, error) {
	if q.Version != r.Version || q.Dialect != "portable" || q.ConnectionID != "" || q.Limit < 1 || q.Limit > 10000 || q.MaxAgeSeconds < 1 || q.MaxAgeSeconds > 86400*90 || len(q.Fields) == 0 || len(q.Fields) > 128 {
		return nil, errors.New("invalid portable query contract")
	}
	for _, f := range q.Fields {
		if f.Type != "string" && f.Type != "number" && f.Type != "boolean" && f.Type != "any" {
			return nil, errors.New("unknown field type")
		}
		if len(f.ProviderFields) == 0 || len(f.ProviderFields) > 100 {
			return nil, errors.New("explicit connection field mappings required")
		}
	}
	ts, e := lex(q.Expression)
	if e != nil {
		return nil, e
	}
	p := parser{tokens: ts, fields: q.Fields}
	n, e := p.or()
	if e == nil && p.pos != len(ts) {
		e = errors.New("unexpected query tokens")
	}
	return n, e
}
func (p *parser) take(k string) bool {
	if p.pos < len(p.tokens) && p.tokens[p.pos].kind == k {
		p.pos++
		return true
	}
	return false
}
func (p *parser) or() (*Node, error) {
	a, e := p.and()
	for e == nil && p.take("or") {
		b, x := p.and()
		e = x
		a = &Node{op: "or", left: a, right: b}
	}
	return a, e
}
func (p *parser) and() (*Node, error) {
	a, e := p.unary()
	for e == nil && p.take("and") {
		b, x := p.unary()
		e = x
		a = &Node{op: "and", left: a, right: b}
	}
	return a, e
}
func (p *parser) unary() (*Node, error) {
	p.depth++
	defer func() { p.depth-- }()
	if p.depth > 32 {
		return nil, errors.New("query nesting limit")
	}
	if p.take("not") {
		n, e := p.unary()
		return &Node{op: "not", left: n}, e
	}
	if p.take("(") {
		n, e := p.or()
		if !p.take(")") {
			return nil, errors.New("missing query parenthesis")
		}
		return n, e
	}
	if p.pos >= len(p.tokens) || p.tokens[p.pos].kind != "field" {
		return nil, errors.New("query field required")
	}
	field := p.tokens[p.pos].text
	p.pos++
	f, ok := p.fields[field]
	if !ok {
		return nil, errors.New("unknown canonical field")
	}
	op := ""
	for _, k := range []string{"==", "!=", "contains", "in"} {
		if p.take(k) {
			op = k
			break
		}
	}
	if op == "" {
		return nil, errors.New("unknown query operator")
	}
	n := &Node{op: op, field: field}
	if op == "in" {
		if !p.take("[") {
			return nil, errors.New("in requires a literal array")
		}
		values := []any{}
		if !p.take("]") {
			for {
				v, e := p.literal(f)
				if e != nil {
					return nil, e
				}
				values = append(values, v)
				if len(values) > 100 {
					return nil, errors.New("query literal limit")
				}
				if p.take("]") {
					break
				}
				if !p.take(",") {
					return nil, errors.New("in literal separator required")
				}
			}
		}
		n.value = values
	} else {
		v, e := p.literal(f)
		if e != nil {
			return nil, e
		}
		if op == "contains" {
			if _, ok := v.(string); !ok || f.Type != "string" {
				return nil, errors.New("contains requires strings")
			}
		}
		n.value = v
	}
	return n, nil
}
func (p *parser) literal(f r.Field) (any, error) {
	if p.pos >= len(p.tokens) || p.tokens[p.pos].kind != "literal" {
		return nil, errors.New("typed literal required")
	}
	v := p.tokens[p.pos].value
	p.pos++
	if v != nil && f.Type != "any" && kind(v) != f.Type {
		return nil, errors.New("query literal type mismatch")
	}
	return v, nil
}
func kind(v any) string {
	switch v.(type) {
	case string:
		return "string"
	case json.Number:
		return "number"
	case bool:
		return "boolean"
	case nil:
		return "null"
	}
	return "unknown"
}
func equal(a, b any) bool {
	if kind(a) != kind(b) {
		return false
	}
	switch x := a.(type) {
	case json.Number:
		l, ok := new(big.Rat).SetString(string(x))
		y, yes := b.(json.Number)
		if !ok || !yes {
			return false
		}
		rr, ok := new(big.Rat).SetString(string(y))
		return ok && l.Cmp(rr) == 0
	case string:
		return x == b
	case bool:
		return x == b
	case nil:
		return b == nil
	}
	return false
}
func Lookup(row r.Record, fields map[string]r.Field, name string) (any, bool) {
	if !row.Data.Complete {
		return nil, false
	}
	f, ok := fields[name]
	if !ok {
		return nil, false
	}
	id, ok := f.ProviderFields[row.ConnectionID]
	if !ok {
		return nil, false
	}
	for _, loss := range row.Data.LossyFields {
		if loss == id {
			return nil, false
		}
	}
	raw, ok := row.Data.Fields[id]
	if !ok || len(row.Data.Provenance[id]) == 0 {
		return nil, false
	}
	v, e := engine.JSONValue(raw)
	if e != nil || f.Type != "any" && v != nil && kind(v) != f.Type || kind(v) == "unknown" {
		return nil, false
	}
	return v, true
}
func (n *Node) Eval(row r.Record, fields map[string]r.Field) Truth {
	switch n.op {
	case "not":
		switch n.left.Eval(row, fields) {
		case True:
			return False
		case False:
			return True
		}
		return Unknown
	case "and":
		a, b := n.left.Eval(row, fields), n.right.Eval(row, fields)
		if a == False || b == False {
			return False
		}
		if a == Unknown || b == Unknown {
			return Unknown
		}
		return True
	case "or":
		a, b := n.left.Eval(row, fields), n.right.Eval(row, fields)
		if a == True || b == True {
			return True
		}
		if a == Unknown || b == Unknown {
			return Unknown
		}
		return False
	}
	v, known := Lookup(row, fields, n.field)
	if !known {
		return Unknown
	}
	yes := false
	switch n.op {
	case "==":
		yes = equal(v, n.value)
	case "!=":
		yes = !equal(v, n.value)
	case "contains":
		s, ok := v.(string)
		if !ok {
			return Unknown
		}
		yes = strings.Contains(s, n.value.(string))
	case "in":
		for _, value := range n.value.([]any) {
			if equal(v, value) {
				yes = true
				break
			}
		}
	}
	if yes {
		return True
	}
	return False
}

// Compare orders known scalar values; callers place unavailable values last.
func Compare(a, b any) int {
	ka, kb := kind(a), kind(b)
	if ka != kb {
		return strings.Compare(ka, kb)
	}
	switch x := a.(type) {
	case json.Number:
		l, _ := new(big.Rat).SetString(string(x))
		r, _ := new(big.Rat).SetString(string(b.(json.Number)))
		return l.Cmp(r)
	case string:
		return strings.Compare(x, b.(string))
	case bool:
		if x == b {
			return 0
		}
		if x {
			return 1
		}
		return -1
	}
	return 0
}
