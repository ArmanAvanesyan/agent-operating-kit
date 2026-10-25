package sdk

import (
	"encoding/json"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/engine"
)

// DecodeProviderIdentity rejects duplicate keys and malformed JSON but permits
// unknown provider fields. Raw identity/credential responses are never assets.
func DecodeProviderIdentity(raw []byte, v *map[string]json.RawMessage) error {
	if _, e := engine.JSONValue(raw); e != nil {
		return e
	}
	return json.Unmarshal(raw, v)
}

func DecodeProviderArray(raw []byte, v any) error {
	if _, e := engine.JSONValue(raw); e != nil {
		return e
	}
	return json.Unmarshal(raw, v)
}
