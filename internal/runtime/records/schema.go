package records

import (
	"encoding/json"
	"fmt"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"reflect"
	"strings"
)

// Schema returns the closed, independently versioned wire contract. Provider field
// values intentionally remain raw JSON; structured AOK control records stay closed.
func Schema(kind string) (map[string]any, error) {
	records := map[string]any{"policy": Policy{}, "manifest": Manifest{}, "state": State{}, "checkpoint": Checkpoint{}, "report": Report{}, "recovery": RecoveryEvidence{}, "replay": Scenario{}}
	r, ok := records[kind]
	if !ok {
		return nil, fmt.Errorf("unknown runtime record")
	}
	s := schemaType(reflect.TypeOf(r))
	s["$schema"] = "https://json-schema.org/draft/2020-12/schema"
	return s, nil
}
func schemaType(t reflect.Type) map[string]any {
	if t == reflect.TypeOf(json.RawMessage{}) {
		return map[string]any{}
	}
	if t == reflect.TypeOf(json.Number("")) {
		return map[string]any{"type": "number"}
	}
	if t.Kind() == reflect.Pointer {
		return map[string]any{"anyOf": []any{schemaType(t.Elem()), map[string]any{"type": "null"}}}
	}
	switch t.Kind() {
	case reflect.Struct:
		properties := map[string]any{}
		required := []string{}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if !f.IsExported() {
				continue
			}
			tag := strings.Split(f.Tag.Get("json"), ",")
			name := tag[0]
			if name == "-" {
				continue
			}
			if name == "" {
				name = f.Name
			}
			properties[name] = schemaType(f.Type)
			optional := false
			for _, part := range tag[1:] {
				if part == "omitempty" {
					optional = true
				}
			}
			if !optional {
				required = append(required, name)
			}
		}
		if _, ok := properties["version"]; ok && t.PkgPath() == reflect.TypeOf(Policy{}).PkgPath() {
			properties["version"] = map[string]any{"const": Version}
		}
		for _, f := range []string{"authorizationGranted", "executionReady", "accepted"} {
			if _, ok := properties[f]; ok {
				properties[f] = map[string]any{"const": false}
			}
		}
		return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
	case reflect.Slice:
		return map[string]any{"type": []string{"array", "null"}, "items": schemaType(t.Elem()), "maxItems": 10000}
	case reflect.Map:
		return map[string]any{"type": []string{"object", "null"}, "additionalProperties": schemaType(t.Elem())}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int, reflect.Int64:
		return map[string]any{"type": "integer"}
	default:
		return map[string]any{"type": "string"}
	}
}
func ValidateJSON(b []byte, kind string) error {
	if e := contracts.ValidateJSON(b); e != nil {
		return e
	}
	schema, e := Schema(kind)
	if e != nil {
		return e
	}
	sb, _ := json.Marshal(schema)
	var s any
	if e = json.Unmarshal(sb, &s); e != nil {
		return e
	}
	c := jsonschema.NewCompiler()
	c.UseLoader(denyLoader{})
	if e = c.AddResource("urn:aok:runtime", s); e != nil {
		return e
	}
	compiled, e := c.Compile("urn:aok:runtime")
	if e != nil {
		return e
	}
	var v any
	decoder := json.NewDecoder(strings.NewReader(string(b)))
	decoder.UseNumber()
	if e = decoder.Decode(&v); e != nil {
		return e
	}
	return compiled.Validate(v)
}

type denyLoader struct{}

func (denyLoader) Load(u string) (any, error) {
	return nil, fmt.Errorf("external schema references forbidden")
}
