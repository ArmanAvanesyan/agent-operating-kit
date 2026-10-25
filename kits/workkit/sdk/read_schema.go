package sdk

import (
	"encoding/json"
	"errors"
	r "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/readv1"
	"reflect"
	"strings"
)

// ReadContractSchema returns structural contracts; semantic binding, trust and
// qualification are independently checked by the read application services.
func ReadContractSchema(kind string) (map[string]any, error) {
	records := map[string]any{"configuration": r.Configuration{}, "manifest": r.Manifest{}, "query": r.Query{}, "enrollment": r.ReadEnrollment{}, "access-policy": r.AccessPolicy{}, "item-batch": []r.Record{}, "catalog-batch": []r.CatalogEntry{}}
	v, ok := records[kind]
	if !ok {
		return nil, errors.New("unknown read contract")
	}
	schema := readSchemaType(reflect.TypeOf(v))
	schema["$schema"] = "https://json-schema.org/draft/2020-12/schema"
	schema["$id"] = "https://workkit.example/spec/read/v1/" + kind + ".schema.json"
	schema["title"] = "WorkKit read " + kind
	if props, ok := schema["properties"].(map[string]any); ok {
		props["version"] = map[string]any{"const": r.Version}
		for _, name := range []string{"authorizationGranted", "executionReady", "migrationActivated", "productionActivated"} {
			if _, ok := props[name]; ok {
				props[name] = map[string]any{"const": false}
			}
		}
		if kind == "manifest" {
			props["liveQualification"] = map[string]any{"const": "not-established"}
		}
		if kind == "enrollment" {
			props["qualificationOnly"] = map[string]any{"const": true}
		}
	}
	return schema, nil
}
func readSchemaType(t reflect.Type) map[string]any {
	if t == reflect.TypeOf(json.RawMessage{}) {
		return map[string]any{}
	}
	if t == reflect.TypeOf(json.Number("")) {
		return map[string]any{"type": "number"}
	}
	if t.Kind() == reflect.Pointer {
		return map[string]any{"anyOf": []any{readSchemaType(t.Elem()), map[string]any{"type": "null"}}}
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
			tag := strings.Split(f.Tag.Get("json"), ",")
			if tag[0] == "-" {
				continue
			}
			name := tag[0]
			if name == "" {
				name = f.Name
			}
			props[name] = readSchemaType(f.Type)
			if len(tag) == 1 || tag[1] != "omitempty" {
				required = append(required, name)
			}
		}
		return map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false}
	case reflect.Slice:
		return map[string]any{"type": []string{"array", "null"}, "items": readSchemaType(t.Elem()), "maxItems": 10000}
	case reflect.Map:
		return map[string]any{"type": []string{"object", "null"}, "additionalProperties": readSchemaType(t.Elem())}
	case reflect.Int, reflect.Int32, reflect.Int64:
		return map[string]any{"type": "integer"}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Interface:
		return map[string]any{}
	default:
		return map[string]any{"type": "string"}
	}
}
