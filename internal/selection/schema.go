package selection

import (
	"encoding/json"
	"fmt"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"reflect"
	"strings"
)

// Schema returns a closed schema for the separately versioned selection records.
func Schema(kind string) (map[string]any, error) {
	types := map[string]any{"task": TaskProfile{}, "candidates": CandidateManifest{}, "policy": SelectionPolicy{}, "decision": SelectionDecision{}}
	v, ok := types[kind]
	if !ok {
		return nil, fmt.Errorf("unknown selection schema")
	}
	s := schemaType(reflect.TypeOf(v))
	s["$schema"] = "https://json-schema.org/draft/2020-12/schema"
	return s, nil
}
func schemaType(t reflect.Type) map[string]any {
	if t == reflect.TypeOf(json.Number("")) {
		return map[string]any{"type": "number"}
	}
	if t == reflect.TypeOf(json.RawMessage{}) {
		return map[string]any{}
	}
	if t.Kind() == reflect.Pointer {
		return map[string]any{"anyOf": []any{schemaType(t.Elem()), map[string]any{"type": "null"}}}
	}
	switch t.Kind() {
	case reflect.Struct:
		props := map[string]any{}
		required := []string{}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if !f.IsExported() {
				continue
			}
			if f.Anonymous {
				nested := schemaType(f.Type)
				if pp, ok := nested["properties"].(map[string]any); ok {
					for k, v := range pp {
						props[k] = v
					}
					if rr, ok := nested["required"].([]string); ok {
						required = append(required, rr...)
					}
				}
				continue
			}
			tag := strings.Split(f.Tag.Get("json"), ",")
			if tag[0] == "-" {
				continue
			}
			name := tag[0]
			if name == "" {
				name = f.Name
			}
			props[name] = schemaType(f.Type)
			optional := false
			for _, part := range tag[1:] {
				if part == "omitempty" {
					optional = true
				}
			}
			if f.Type.Kind() != reflect.Pointer && !optional {
				required = append(required, name)
			}
		}
		if t == reflect.TypeOf(TaskProfile{}) || t == reflect.TypeOf(CandidateManifest{}) || t == reflect.TypeOf(SelectionPolicy{}) || t == reflect.TypeOf(SelectionDecision{}) {
			props["version"] = map[string]any{"const": Version}
		}
		for _, key := range []string{"authorizationGranted", "executionReady"} {
			if _, ok := props[key]; ok {
				props[key] = map[string]any{"const": false}
			}
		}
		return map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false}
	case reflect.Slice:
		return map[string]any{"type": []string{"array", "null"}, "items": schemaType(t.Elem()), "maxItems": 10000}
	case reflect.Map:
		return map[string]any{"type": "object", "additionalProperties": schemaType(t.Elem())}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int, reflect.Int64:
		return map[string]any{"type": "integer"}
	default:
		return map[string]any{"type": "string"}
	}
}
func ValidateJSON(data []byte, kind string) error {
	schema, e := Schema(kind)
	if e != nil {
		return e
	}
	c := jsonschema.NewCompiler()
	encoded, _ := json.Marshal(schema)
	var resource any
	if e = json.Unmarshal(encoded, &resource); e != nil {
		return e
	}
	if e = c.AddResource("urn:aok:selection", resource); e != nil {
		return e
	}
	compiled, e := c.Compile("urn:aok:selection")
	if e != nil {
		return e
	}
	var v any
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.UseNumber()
	if e = decoder.Decode(&v); e != nil {
		return e
	}
	return compiled.Validate(v)
}
