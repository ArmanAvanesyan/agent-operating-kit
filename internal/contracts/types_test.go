package contracts

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDecodeTypedOperationalFixturesRoundTrip(t *testing.T) {
	cases := []struct {
		file string
		want any
	}{
		{"agent.json", Agent{}},
		{"execution-profile.json", ExecutionProfile{}},
		{"assignment.json", AgentAssignment{}},
		{"capability-registry.json", CapabilityRegistry{}},
		{"invocation-contract.json", InvocationContract{}},
		{"context-policy.json", ContextPolicy{}},
		{"output-contract.json", OutputContract{}},
		{"environment-profile.json", EnvironmentProfile{}},
		{"lifecycle-policy.json", LifecyclePolicy{}},
		{"evaluation-contract.json", EvaluationContract{}},
		{"migration-contract.json", MigrationContract{}},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", tc.file))
			if err != nil {
				t.Fatal(err)
			}
			parsed, _, err := Parse(data)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			got, err := DecodeTyped(parsed.(Resource))
			if err != nil {
				t.Fatalf("DecodeTyped: %v", err)
			}
			if reflect.TypeOf(got) != reflect.TypeOf(tc.want) {
				t.Fatalf("got type %T, want %T", got, tc.want)
			}
			encoded, err := json.Marshal(got)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			if !semanticJSONEqual(t, data, encoded) {
				t.Fatalf("typed round-trip lost or changed fields\ninput: %s\noutput: %s", data, encoded)
			}
		})
	}
}

func TestDecodeTypedPreservesLargeIntegersAndNullableParameters(t *testing.T) {
	profile, err := os.ReadFile(filepath.Join("testdata", "execution-profile.json"))
	if err != nil {
		t.Fatal(err)
	}
	precise := bytes.Replace(profile, []byte(`"timeoutSeconds": 900`), []byte(`"timeoutSeconds": 900719925474099312345`), 1)
	parsed, _, err := Parse(precise)
	if err != nil {
		t.Fatalf("Parse large integer: %v", err)
	}
	got, err := DecodeTyped(parsed.(Resource))
	if err != nil {
		t.Fatalf("DecodeTyped large integer: %v", err)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(encoded, []byte(`900719925474099312345`)) {
		t.Fatalf("integer precision lost: %s", encoded)
	}

	assignment, err := os.ReadFile(filepath.Join("testdata", "assignment.json"))
	if err != nil {
		t.Fatal(err)
	}
	withNull := bytes.Replace(assignment, []byte(`"depth": "deep"`), []byte(`"depth": "deep", "nullable": null`), 1)
	parsed, _, err = Parse(withNull)
	if err != nil {
		t.Fatalf("Parse nullable parameter: %v", err)
	}
	got, err = DecodeTyped(parsed.(Resource))
	if err != nil {
		t.Fatalf("DecodeTyped nullable parameter: %v", err)
	}
	typed := got.(AgentAssignment)
	if string(typed.Inputs["nullable"]) != "null" {
		t.Fatalf("null parameter was not retained: %s", typed.Inputs["nullable"])
	}
	encoded, err = json.Marshal(typed)
	if err != nil {
		t.Fatal(err)
	}
	if !semanticJSONEqual(t, withNull, encoded) {
		t.Fatalf("nullable parameter or other field changed: %s", encoded)
	}
}

func TestDecodeTypedStrictAndEnvelopeBound(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "agent.json"))
	if err != nil {
		t.Fatal(err)
	}
	withUnknown := bytes.Replace(data, []byte(`"kind": "Agent"`), []byte(`"kind": "Agent", "unexpected": true`), 1)
	if _, err := DecodeTyped(Resource{Envelope: Envelope{Kind: "Agent"}, Document: withUnknown}); err == nil {
		t.Fatalf("expected strict unknown-field rejection, got %v", err)
	}
	if _, err := DecodeTyped(Resource{Envelope: Envelope{Kind: "Agent"}, Document: []byte(`{"apiVersion":"aok/v1alpha1","kind":"ContextPolicy"}`)}); err == nil {
		t.Fatal("accepted mismatched resource envelope")
	}
	if _, err := DecodeTyped(Resource{Envelope: Envelope{Kind: "LifecyclePolicy"}, Document: []byte(`{"apiVersion":"aok/v1alpha1","kind":"LifecyclePolicy"}`)}); err == nil {
		t.Fatal("accepted incomplete policy document")
	}
}

func semanticJSONEqual(t *testing.T, want, got []byte) bool {
	t.Helper()
	decode := func(data []byte) any {
		var value any
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.UseNumber()
		if err := dec.Decode(&value); err != nil {
			t.Fatalf("decode semantic JSON: %v", err)
		}
		return value
	}
	return reflect.DeepEqual(decode(want), decode(got))
}

func TestDecodeTypedRejectsMutatedIdentity(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "agent.json"))
	if err != nil {
		t.Fatal(err)
	}
	parsed, _, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	r := parsed.(Resource)
	r.Metadata.ID = "different-agent"
	if _, err := DecodeTyped(r); err == nil {
		t.Fatal("accepted changed identity")
	}
}
