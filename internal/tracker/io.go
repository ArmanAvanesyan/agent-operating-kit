package tracker

import (
	"encoding/json"
	"fmt"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/modelcatalog"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
	"os"
	"path/filepath"
)

func Hash(b []byte) string { return usage.Hash(b) }
func Digest(v any) string  { return usage.Digest(v) }
func DecodeObject(b []byte) (map[string]json.RawMessage, error) {
	var m map[string]json.RawMessage
	e := usage.Decode(b, &m)
	if e == nil && m == nil {
		e = fmt.Errorf("object required")
	}
	return m, e
}
func ReadFile(path string) ([]byte, error) {
	if e := safePath(path); e != nil {
		return nil, e
	}
	return modelcatalog.ReadFile(path)
}
func ReadAsset(root, path string) ([]byte, error) {
	if e := safePath(root); e != nil {
		return nil, e
	}
	return modelcatalog.ReadAsset(root, path)
}
func safePath(path string) error {
	p, e := filepath.Abs(path)
	if e != nil {
		return e
	}
	for {
		st, e := os.Lstat(p)
		if e != nil {
			return e
		}
		if st.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink input ancestor forbidden")
		}
		if p == filepath.Dir(p) {
			return nil
		}
		p = filepath.Dir(p)
	}
}
func ReadSource(path string) (Source, error) {
	var s Source
	b, e := ReadFile(path)
	if e != nil {
		return s, e
	}
	if e = Decode(b, "source", &s); e != nil {
		return s, e
	}
	return s, ValidateSource(s)
}
func Decode(b []byte, kind string, v any) error {
	if e := ValidateJSON(b, kind); e != nil {
		return e
	}
	return usage.Decode(b, v)
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
	return Item{Identity: identity(s, id, kind), NativeTenantID: s.NativeTenantID, Fields: map[string]json.RawMessage{}, Relations: []Relation{}, Comments: []Comment{}, ProjectFields: map[string]json.RawMessage{}, Transitions: []Transition{}, Provenance: map[string][]Pointer{}, LossyFields: []string{}}
}
func identity(s Source, id, kind string) contracts.RemoteIdentity {
	return contracts.RemoteIdentity{Provider: s.Provider, TenantID: TenantScopeID(s), ContainerID: s.ContainerID, RemoteID: id, ItemKind: kind}
}
