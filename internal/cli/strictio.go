package cli

import (
	"encoding/json"
	"errors"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/tracker"
)

func strictReadFile(path string) ([]byte, error) { return tracker.ReadFile(path) }
func strictDecodeJSON(b []byte, v any) error {
	if e := contracts.ValidateJSON(b); e != nil {
		return e
	}
	return json.Unmarshal(b, v)
}
func strictReadJSON(path string) (map[string]any, error) {
	b, e := strictReadFile(path)
	if e != nil {
		return nil, e
	}
	m := map[string]any{}
	if e = strictDecodeJSON(b, &m); e != nil {
		return nil, e
	}
	if m == nil {
		return nil, errors.New("JSON object required")
	}
	return m, nil
}
