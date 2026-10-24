package modelcatalog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestDecodeRejectsDuplicateUnknownAndKeepsNumbersExact(t *testing.T) {
	var p Policy
	for _, b := range [][]byte{[]byte(`{"version":"aok-model-policy/v1","version":"x","sources":[],"validityHours":{}}`), []byte(`{"version":"aok-model-policy/v1","sources":[],"validityHours":{},"extra":1}`)} {
		if Decode(b, &p) == nil {
			t.Fatalf("accepted invalid JSON: %s", b)
		}
	}
	var v struct {
		N json.Number `json:"n"`
	}
	if err := Decode([]byte(`{"n":9007199254740993123456789}`), &v); err != nil {
		t.Fatal(err)
	}
	if string(v.N) != "9007199254740993123456789" {
		t.Fatalf("number rounded: %s", v.N)
	}
}

func TestProbeSchemaRestrictsObservationsAndValues(t *testing.T) {
	probe := func(dimension, value string) []byte {
		return []byte(fmt.Sprintf(`{"version":"aok-model-probe/v1","id":"p","configuration":{"host":"codex","hostVersion":"1.2.3","provider":"acme","modelId":"m","modelRevision":null,"reasoningMode":"effort","reasoningBudgetTokens":null,"generation":null,"effort":"medium","serviceTier":null,"authMode":null,"deployment":null,"toolProfile":null},"profileSha256":"%s","observedAt":"2026-10-23T10:00:00Z","accountScope":"opaque","outcome":"success","observations":[{"dimension":%q,"value":%s}],"evidenceSha256":"%s"}`, Hash([]byte("p")), dimension, value, Hash([]byte("e"))))
	}
	for _, tc := range []struct{ dimension, value string }{
		{"selector", `"model-x"`},
		{"reasoning.budgetTokens", `2048`},
		{"structuredOutput", `true`},
		{"toolUse", `false`},
		{"modalities", `["text","image"]`},
	} {
		if err := Validate(probe(tc.dimension, tc.value), "probe.json"); err != nil {
			t.Errorf("valid observation %s rejected: %v", tc.dimension, err)
		}
	}
	for _, tc := range []struct{ dimension, value string }{
		{"raw-response", `"secret"`},
		{"response.body", `{"apiKey":"secret"}`},
		{"selector", `{"response":"secret"}`},
		{"toolUse", `"yes"`},
		{"modalities", `["text",7]`},
	} {
		if err := Validate(probe(tc.dimension, tc.value), "probe.json"); err == nil {
			t.Errorf("accepted unsafe observation dimension=%s value=%s", tc.dimension, tc.value)
		}
	}
}

func TestEmbeddedSchemasAreClosedForEveryCatalogRecord(t *testing.T) {
	good := []byte(`{"version":"aok-model-policy/v1","sources":[],"validityHours":{}}`)
	if err := Validate(good, "policy.json"); err != nil {
		t.Fatal(err)
	}
	bad := []byte(`{"version":"aok-model-policy/v1","sources":[],"validityHours":{},"unknown":true}`)
	if err := Validate(bad, "policy.json"); err == nil {
		t.Fatal("accepted unknown policy property")
	}
	if err := Validate([]byte(`{"version":"aok-model-catalog/v1","importerVersion":"1","sources":[],"entries":[],"configurations":[],"assertions":[{"id":"a","oops":1}],"guidance":[],"probes":[]}`), "catalog.json"); err == nil {
		t.Fatal("accepted open assertion record")
	}
}

func TestReadAssetRejectsTraversalAndSymlink(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "safe"), []byte("ok"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadAsset(dir, "../outside"); err == nil {
		t.Fatal("accepted traversal")
	}
	if err := os.Symlink(filepath.Join(dir, "safe"), filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadAsset(dir, "link"); err == nil {
		t.Fatal("accepted symlink")
	}
	if b, err := ReadAsset(dir, "safe"); err != nil || string(b) != "ok" {
		t.Fatalf("read safe asset: %q %v", b, err)
	}
}
