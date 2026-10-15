package contracts

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseContextPolicy(t *testing.T) {
	doc := `{"apiVersion":"aok/v1alpha1","kind":"ContextPolicy","metadata":{"id":"repo-context","name":"Repo context","version":"1.2.3","description":"bounded context"},"repositoryScope":["workspace"],"requiredDocuments":["AGENTS.md"],"conversation":"assignment-only","retrievalSources":["workspace"],"untrustedContent":"data-only","limits":{"maxInputTokens":1000,"overflow":"retrieve-selectively"},"retention":"run-only","redactionRequired":true}`
	v, ds, err := Parse([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	if len(ds) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", ds)
	}
	if _, ok := v.(Resource); !ok {
		t.Fatalf("unexpected type %T", v)
	}
}

func TestParseRejectsMalformedCanonicalJSON(t *testing.T) {
	base := `{"apiVersion":"aok/v1alpha1","kind":"ContextPolicy","metadata":{"id":"repo-context","name":"Repo context","version":"1.2.3","description":"bounded context"},"repositoryScope":[],"requiredDocuments":[],"conversation":"assignment-only","retrievalSources":[],"untrustedContent":"data-only","limits":{"maxInputTokens":1,"overflow":"retrieve-selectively"},"retention":"run-only","redactionRequired":true}`
	cases := []struct {
		name string
		data []byte
	}{
		{"nested duplicate", []byte(strings.Replace(base, `"metadata":{"id":"repo-context",`, `"metadata":{"id":"repo-context","id":"again",`, 1))},
		{"bom", append([]byte{0xef, 0xbb, 0xbf}, []byte(base)...)},
		{"trailing value", []byte(base + ` {}`)},
		{"unsupported version", []byte(strings.Replace(base, "aok/v1alpha1", "aok/v9", 1))},
		{"unknown field", []byte(strings.Replace(base, `"retention":"run-only"`, `"retention":"run-only","mystery":true`, 1))},
		{"too deep", []byte(strings.Repeat("[", MaxDepth+1) + "0" + strings.Repeat("]", MaxDepth+1))},
		{"too large", append([]byte(base), bytesRepeat(' ', MaxDocumentBytes+1-len(base))...)},
		{"invalid UTF-8", append(append([]byte{}, base[:len(base)-1]...), 0xff, '}')},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, diagnostics, err := Parse(tc.data)
			if err == nil {
				t.Fatal("expected rejection")
			}
			if len(diagnostics) != 1 || diagnostics[0].Code == "" || diagnostics[0].Severity != "error" {
				t.Fatalf("missing stable error diagnostic: %+v", diagnostics)
			}
		})
	}
	if _, _, err := Parse(append([]byte(base), bytesRepeat(' ', MaxDocumentBytes-len(base))...)); err != nil {
		t.Fatalf("exactly 4 MiB should pass: %v", err)
	}
}

func TestParsePreservesLargeInputNumber(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "assignment.json"))
	if err != nil {
		t.Fatal(err)
	}
	doc := strings.Replace(string(data), `"repository": "."`, `"repository":9007199254740993`, 1)
	v, ds, err := Parse([]byte(doc))
	if err != nil {
		t.Fatalf("Parse: %v diagnostics=%+v", err, ds)
	}
	r := v.(Resource)
	if !bytes.Contains(r.Document, []byte("9007199254740993")) {
		t.Fatal("large integer lost precision")
	}
}

func TestDepthBoundary(t *testing.T) {
	depth64 := []byte(strings.Repeat("[", MaxDepth) + "0" + strings.Repeat("]", MaxDepth))
	if err := scanJSON(depth64); err != nil {
		t.Fatalf("depth %d should pass: %v", MaxDepth, err)
	}
	depth65 := []byte(strings.Repeat("[", MaxDepth+1) + "0" + strings.Repeat("]", MaxDepth+1))
	if err := scanJSON(depth65); err == nil {
		t.Fatal("depth above limit should fail")
	}
}

func bytesRepeat(b byte, n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = b
	}
	return out
}

func TestOperationalFixturesCoverEverySupportedKind(t *testing.T) {
	names := []string{"agent.json", "execution-profile.json", "assignment.json", "capability-registry.json", "invocation-contract.json", "context-policy.json", "output-contract.json", "environment-profile.json"}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", name))
			if err != nil {
				t.Fatal(err)
			}
			v, ds, err := Parse(data)
			if err != nil {
				t.Fatalf("Parse: %v diagnostics=%+v", err, ds)
			}
			r, ok := v.(Resource)
			if !ok {
				t.Fatalf("got %T", v)
			}
			if len(r.Document) == 0 {
				t.Fatal("document was not retained")
			}
		})
	}
}

func TestReferenceVersionsAreExactThreePartVersions(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "agent.json"))
	if err != nil {
		t.Fatal(err)
	}
	valid := strings.ReplaceAll(string(data), "0.1.0", "1.2.10")
	if _, ds, err := Parse([]byte(valid)); err != nil {
		t.Fatalf("1.2.10 should validate: %v diagnostics=%+v", err, ds)
	}
	invalid := strings.Replace(string(data), "0.1.0", "1.2.", 1)
	if _, ds, err := Parse([]byte(invalid)); err == nil {
		t.Fatalf("1.2. should fail, diagnostics=%+v", ds)
	}
}

func TestSchemaLoaderRejectsNetworkAndFileURLs(t *testing.T) {
	loader := denyLoader{}
	for _, url := range []string{"https://example.invalid/schema.json", "file:///etc/passwd"} {
		if _, err := loader.Load(url); err == nil {
			t.Fatalf("loader accepted %s", url)
		}
	}
}
