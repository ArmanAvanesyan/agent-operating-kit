package contracts

import (
	"encoding/json"
	"testing"
)

func TestConnectorV2DoesNotChangeV1Schema(t *testing.T) {
	// Validation must reach provider validation without opening other kinds.
	s := map[string]any{"apiVersion": "aok/v1alpha2", "kind": "Agent"}
	b, _ := json.Marshal(s)
	if _, d, e := Parse(b); e == nil || d[0].Code != "document.api-version" {
		t.Fatal("unrelated kind enabled")
	}
	for _, kind := range []string{"WorkItemBinding", "ConnectorCapability", "SyncOperation", "SyncReceipt", "HostTaskBinding", "RunRecord"} {
		if !connectorV2Kind(kind) {
			t.Fatal(kind)
		}
	}
	var schema any = map[string]any{"const": "aok/v1alpha1", "enum": []any{"github", "jira"}}
	extendConnectorSchema(schema)
	m := schema.(map[string]any)
	if m["const"] != "aok/v1alpha2" || len(m["enum"].([]any)) != 3 {
		t.Fatal(m)
	}
}
