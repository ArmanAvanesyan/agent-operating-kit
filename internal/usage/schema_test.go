package usage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

func schemaOnlyValidate(t *testing.T, name string, raw []byte) error {
	t.Helper()
	schemaBytes, e := schemaFS.ReadFile("schemas/" + name)
	if e != nil {
		t.Fatal(e)
	}
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft2020)
	c.UseLoader(usageDenyLoader{})
	var doc any
	if e = json.Unmarshal(schemaBytes, &doc); e != nil {
		t.Fatal(e)
	}
	uri := "https://aok.invalid/usage/" + name
	if e = c.AddResource(uri, doc); e != nil {
		t.Fatal(e)
	}
	s, e := c.Compile(uri)
	if e != nil {
		t.Fatal(e)
	}
	value, e := contracts.DecodeJSON(raw)
	if e != nil {
		return e
	}
	return s.Validate(value)
}
func readObject(t *testing.T, path string) map[string]any {
	t.Helper()
	b, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	var m map[string]any
	if e = json.Unmarshal(b, &m); e != nil {
		t.Fatal(e)
	}
	return m
}
func asMap(t *testing.T, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("expected object, got %T", v)
	}
	return m
}
func asSlice(t *testing.T, v any) []any {
	t.Helper()
	a, ok := v.([]any)
	if !ok {
		t.Fatalf("expected array, got %T", v)
	}
	return a
}
func putUnknown(t *testing.T, root map[string]any, path ...any) {
	t.Helper()
	var cur any = root
	for _, p := range path[:len(path)-1] {
		switch x := p.(type) {
		case string:
			cur = asMap(t, cur)[x]
		case int:
			cur = asSlice(t, cur)[x]
		default:
			t.Fatalf("bad path token %T", p)
		}
	}
	last := path[len(path)-1]
	switch x := last.(type) {
	case string:
		asMap(t, cur)[x] = "unexpected"
	case int:
		asSlice(t, cur)[x] = map[string]any{"unexpected": true}
	default:
		t.Fatalf("bad final path token %T", last)
	}
}
func rawObject(t *testing.T, v any) []byte {
	t.Helper()
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	return b
}

func TestEmbeddedSchemasCloseEveryNestedUsageRecord(t *testing.T) {
	base := filepath.Join("testdata", "export.json")
	e, p, err := Load(base, filepath.Join("testdata", "policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		name string
		data []byte
		path []any
	}
	// Each case passes directly to the embedded JSON Schema engine, without calling Decode.
	event := Event{ID: "evt", Origin: e.Quotas[0].Origin, BindingID: e.Bindings[0].ID, SessionID: "session", Metric: "requests", Unit: "requests", Value: json.Number("1"), Semantics: "incremental", DedupeCoverage: "strong"}
	charge := Charge{ID: "charge", ConstraintID: e.Bindings[0].ConstraintIDs[0], Generation: "window", Unit: "requests", Amount: json.Number("1")}
	reservation := Reservation{ID: "reservation", Authority: "authority", AuthorityEpoch: "epoch", Owner: "owner", AttemptID: "attempt", ConfigurationID: "config", PolicySHA256: e.PolicySHA256, SnapshotSHA256: fmt.Sprintf("%064x", 1), ProfileSHA256: fmt.Sprintf("%064x", 2), DemandSHA256: fmt.Sprintf("%064x", 3), DemandValidUntil: "2026-10-23T12:01:00Z", Fence: 1, LeaseUntil: "2026-10-23T12:01:00Z", State: "held", Amounts: []Amount{{ConstraintID: e.Bindings[0].ConstraintIDs[0], Generation: "window", Unit: "requests", Amount: json.Number("1")}}}
	e.Events = []Event{event}
	e.Charges = []Charge{charge}
	e.Reservations = []Reservation{reservation}
	e.Evidence = []Asset{{Path: "safe/evidence.json", SHA256: fmt.Sprintf("%064x", 4)}}
	exp := rawObject(t, e)
	for _, path := range [][]any{{"bindings", 0}, {"events", 0}, {"quotas", 0}, {"charges", 0}, {"reservations", 0}, {"evidence", 0}} {
		cases = append(cases, struct {
			name string
			data []byte
			path []any
		}{"export.json", exp, path})
	}
	policy, _, err := ReadPolicy(filepath.Join("testdata", "policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	policyRaw := rawObject(t, policy)
	cases = append(cases, struct {
		name string
		data []byte
		path []any
	}{"policy.json", policyRaw, []any{"sources", 0}})
	bindingBytes, err := os.ReadFile(filepath.Join("testdata", "binding.json"))
	if err != nil {
		t.Fatal(err)
	}
	cases = append(cases, struct {
		name string
		data []byte
		path []any
	}{"binding.json", bindingBytes, []any{}})
	demandBytes, err := os.ReadFile(filepath.Join("testdata", "demand.json"))
	if err != nil {
		t.Fatal(err)
	}
	cases = append(cases, struct {
		name string
		data []byte
		path []any
	}{"demand.json", demandBytes, []any{"amounts", 0}})
	for i, tc := range cases {
		t.Run(fmt.Sprintf("%s-%d", tc.name, i), func(t *testing.T) {
			m := readGenericObject(t, tc.data)
			putUnknown(t, m, append(tc.path, "unrecognizedMember")...)
			if err := schemaOnlyValidate(t, tc.name, mustMarshal(t, m)); err == nil {
				t.Fatalf("embedded %s schema accepted nested unknown property", tc.name)
			}
		})
	}
	// Sanity: the same fully populated export is accepted by schema and semantics.
	if err := schemaOnlyValidate(t, "export.json", exp); err != nil {
		t.Fatalf("schema rejected realistic export including reservation: %v", err)
	}
	if err := ValidateExport(e, p); err != nil {
		t.Fatalf("semantic validator rejected realistic reservation: %v", err)
	}
}
func readGenericObject(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if e := json.Unmarshal(b, &m); e != nil {
		t.Fatal(e)
	}
	return m
}
func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
