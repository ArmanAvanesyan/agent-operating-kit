package readsource

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/engine"
)

func Hash(b []byte) string { return engine.Hash(b) }

// Digest retains the historical AOK JSON-marshal algorithm, not JCS.
func Digest(v any) string        { b, _ := json.Marshal(v); return Hash(b) }
func validateRaw(b []byte) error { _, e := engine.JSONValue(b); return e }
func decodeRaw(b []byte, v any) error {
	if e := validateRaw(b); e != nil {
		return e
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	d.DisallowUnknownFields()
	return d.Decode(v)
}
func DecodeObject(b []byte) (map[string]json.RawMessage, error) {
	var m map[string]json.RawMessage
	e := decodeRaw(b, &m)
	if e == nil && m == nil {
		e = fmt.Errorf("object required")
	}
	return m, e
}
func ReadFile(p string) ([]byte, error) { return engine.ReadFile(context.Background(), p) }
func ReadAsset(root, p string) ([]byte, error) {
	a, e := engine.Confined(root, p)
	if e != nil {
		return nil, e
	}
	return ReadFile(a)
}
func ReadSource(p string) (Source, error) {
	var s Source
	b, e := ReadFile(p)
	if e != nil {
		return s, e
	}
	if e = Decode(b, "source", &s); e != nil {
		return s, e
	}
	return s, ValidateSource(s)
}
func Decode(b []byte, k string, v any) error {
	if e := ValidateJSON(b, k); e != nil {
		return e
	}
	return decodeRaw(b, v)
}
func BaseReport() Report {
	return Report{Version: Version, Status: "valid", Diagnostics: []Diagnostic{}, LiveReadGate: "pending-provider-conformance", WritebackGate: "pending-p7-p8"}
}
func TenantScopeID(s Source) string {
	if s.Provider == "jira-datacenter" && s.Deployment != nil {
		return Digest(struct{ Provider, Origin, NativeTenantID, BasePath string }{s.Provider, s.Origin, s.NativeTenantID, s.Deployment.BasePath})
	}
	return Digest(struct{ Provider, Origin, NativeTenantID string }{s.Provider, s.Origin, s.NativeTenantID})
}
func NewItem(s Source, id, kind string) Item {
	return Item{Identity: RemoteIdentity{Provider: s.Provider, TenantID: TenantScopeID(s), ContainerID: s.ContainerID, RemoteID: id, ItemKind: kind}, NativeTenantID: s.NativeTenantID, Fields: map[string]json.RawMessage{}, Relations: []Relation{}, Comments: []Comment{}, ProjectFields: map[string]json.RawMessage{}, Transitions: []Transition{}, Provenance: map[string][]Pointer{}, LossyFields: []string{}}
}
