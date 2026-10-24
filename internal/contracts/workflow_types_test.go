package contracts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestWorkflowFixturesParseAndTypedRoundTrip(t *testing.T) {
	for _, name := range []string{"work-item-binding.json", "host-task-binding.json", "run-record.json", "connector-capability.json", "sync-operation.json", "sync-receipt.json"} {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", name))
			if err != nil {
				t.Fatal(err)
			}
			parsed, diagnostics, err := Parse(data)
			if err != nil {
				t.Fatalf("Parse: %v (%+v)", err, diagnostics)
			}
			value, err := DecodeTyped(parsed.(Resource))
			if err != nil {
				t.Fatalf("DecodeTyped: %v", err)
			}
			encoded, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			round, ds, err := Parse(encoded)
			if err != nil {
				t.Fatalf("round-trip Parse: %v (%+v)\n%s", err, ds, encoded)
			}
			again, err := DecodeTyped(round.(Resource))
			if err != nil {
				t.Fatal(err)
			}
			a, _ := json.Marshal(value)
			b, _ := json.Marshal(again)
			if !reflect.DeepEqual(jsonValue(t, a), jsonValue(t, b)) {
				t.Fatalf("typed round trip changed document\n%s\n%s", a, b)
			}
		})
	}
}

func TestOperationDigestSortsFieldsAndPreservesNumberPrecision(t *testing.T) {
	binding := Reference{Kind: "WorkItemBinding", ID: "issue", Version: "0.1.0"}
	capability := Reference{Kind: "ConnectorCapability", ID: "github", Version: "0.1.0"}
	id := RemoteIdentity{Provider: "github", TenantID: "org", ContainerID: "repo", RemoteID: "42", ItemKind: "issue"}
	first := SyncChange{Fields: []FieldChange{{ProviderFieldID: "z", Value: json.RawMessage(`1.0000000000000000001`)}, {ProviderFieldID: "a", Value: json.RawMessage(`true`)}}}
	second := SyncChange{Fields: []FieldChange{{ProviderFieldID: "a", Value: json.RawMessage(`true`)}, {ProviderFieldID: "z", Value: json.RawMessage(`1.0000000000000000001`)}}}
	d1, err := OperationDigest(binding, id, capability, "update-fields", "rev-1", first)
	if err != nil {
		t.Fatal(err)
	}
	d2, err := OperationDigest(binding, id, capability, "update-fields", "rev-1", second)
	if err != nil {
		t.Fatal(err)
	}
	if d1 != d2 {
		t.Fatalf("field order changed digest: %s != %s", d1, d2)
	}
	preimage, err := CanonicalOperationBytes(binding, id, capability, "update-fields", "rev-1", first)
	if err != nil {
		t.Fatal(err)
	}
	wantPreimage := `{"binding":{"kind":"WorkItemBinding","id":"issue","version":"0.1.0"},"identity":{"provider":"github","tenantId":"org","containerId":"repo","remoteId":"42","itemKind":"issue"},"capability":{"kind":"ConnectorCapability","id":"github","version":"0.1.0"},"operationKind":"update-fields","expectedRevision":"rev-1","change":{"fields":[{"providerFieldId":"a","value":true},{"providerFieldId":"z","value":1.0000000000000000001}]}}`
	if string(preimage) != wantPreimage {
		t.Fatalf("canonical digest preimage changed: %s", preimage)
	}
	if d1 != "91def633bee1b2cfe4dda72cfedb3dd096842d9a759b439b2bb1190f42586c3c" {
		t.Fatalf("digest changed: %s", d1)
	}
	second.Fields[1].Value = json.RawMessage(`1.0000000000000000002`)
	d3, err := OperationDigest(binding, id, capability, "update-fields", "rev-1", second)
	if err != nil {
		t.Fatal(err)
	}
	if d1 == d3 {
		t.Fatal("changed exact number did not change digest")
	}
	second.Fields = append(second.Fields, second.Fields[0])
	if _, err := OperationDigest(binding, id, capability, "update-fields", "rev-1", second); err == nil {
		t.Fatal("duplicate provider field accepted")
	}
}

func TestSyncOperationSchemaRequiresMatchingClosedPayload(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "sync-operation.json"))
	if err != nil {
		t.Fatal(err)
	}
	var base map[string]any
	if err := json.Unmarshal(data, &base); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, kind string
		change     map[string]any
	}{
		{"empty update", "update-fields", map[string]any{}},
		{"mixed update", "update-fields", map[string]any{"fields": []any{map[string]any{"providerFieldId": "title", "value": "x"}}, "transitionId": "closed"}},
		{"mismatched kind", "transition", map[string]any{"fields": []any{map[string]any{"providerFieldId": "title", "value": "x"}}}},
		{"empty transition", "transition", map[string]any{}},
		{"mixed comment", "comment", map[string]any{"comment": map[string]any{"path": "assets/comment.md", "sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, "relationType": "blocks", "relationIdentity": base["change"]}},
		{"incomplete link", "link", map[string]any{"relationType": "blocks"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			copy := map[string]any{}
			for k, v := range base {
				copy[k] = v
			}
			copy["operationKind"], copy["change"] = tc.kind, tc.change
			bad, err := json.Marshal(copy)
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := Parse(bad); err == nil {
				t.Fatalf("accepted invalid payload: %s", bad)
			}
		})
	}
}

func TestWorkflowSchemasRejectUnsupportedProviders(t *testing.T) {
	t.Run("remote identity", func(t *testing.T) {
		data, err := os.ReadFile(filepath.Join("testdata", "work-item-binding.json"))
		if err != nil {
			t.Fatal(err)
		}
		var doc map[string]any
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Fatal(err)
		}
		doc["identity"].(map[string]any)["provider"] = "unknown-tracker"
		bad, _ := json.Marshal(doc)
		if _, _, err := Parse(bad); err == nil {
			t.Fatal("unsupported RemoteIdentity provider accepted")
		}
	})
	t.Run("connector capability", func(t *testing.T) {
		data, err := os.ReadFile(filepath.Join("testdata", "connector-capability.json"))
		if err != nil {
			t.Fatal(err)
		}
		var doc map[string]any
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Fatal(err)
		}
		doc["provider"] = "unknown-tracker"
		bad, _ := json.Marshal(doc)
		if _, _, err := Parse(bad); err == nil {
			t.Fatal("unsupported ConnectorCapability provider accepted")
		}
	})
}
