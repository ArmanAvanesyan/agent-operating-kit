package engine

import (
	"bytes"
	"context"
	"encoding/json"
	jcs "github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func canonicalDir(t *testing.T) string {
	t.Helper()
	d, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	return d
}
func testDoc(t *testing.T) map[string]any {
	t.Helper()
	b, e := os.ReadFile("../../testdata/design/workspace.json")
	if e != nil {
		t.Fatal(e)
	}
	v, e := JSONValue(b)
	if e != nil {
		t.Fatal(e)
	}
	return Map(v)
}
func TestAllStructuralExamplesAndTypedResourceParity(t *testing.T) {
	paths, e := filepath.Glob("../../testdata/design/*.json")
	if e != nil {
		t.Fatal(e)
	}
	seen := map[string]bool{}
	for _, p := range paths {
		b, e := os.ReadFile(p)
		if e != nil {
			t.Fatal(e)
		}
		d, e := Parse(b, "json")
		if e != nil {
			t.Fatalf("%s: %v", p, e)
		}
		seen[d.Kind] = true
	}
	if len(seen) != 24 {
		t.Fatalf("resource coverage %d", len(seen))
	}
}
func TestNegativeDesignFixtures(t *testing.T) {
	paths, _ := filepath.Glob("../../testdata/negative/*.json")
	for _, p := range paths {
		b, _ := os.ReadFile(p)
		if _, e := Parse(b, "json"); e == nil {
			t.Fatalf("accepted %s", p)
		}
	}
}
func TestStrictJSONAndYAML(t *testing.T) {
	for _, b := range []string{`{"a":1,"a":2}`, `{} {}`, `{"a":"\ud800"}`, `{"a":"\udc00"}`, `{"a":"\ud800\ud800"}`, `{"a":01}`, `{"a":NaN}`, `{"a":1e999999}`, "{\"a\":\"\xff\"}"} {
		if _, e := JSONValue([]byte(b)); e == nil {
			t.Errorf("accepted malformed JSON")
		}
	}
	if _, e := JSONValue([]byte(`{"a":"\ud83d\ude00"}`)); e != nil {
		t.Fatal(e)
	}
	for _, b := range []string{"x: 1\nx: 2", "x: &a hello\ny: *a", "x: !!str hello", "x: 2026-10-25", "x: 0x12", "x: 012", "x: .inf", "x: ~", "x:", "1: value", "x: 1\n---\ny: 2", "x: {<<: {a: b}}"} {
		if _, e := yamlValue([]byte(b)); e == nil {
			t.Errorf("accepted restricted YAML %q", b)
		}
	}
	for _, b := range []string{"x: null", "x: true", "x: \"2026-10-25\"", "x: 0.1", "x: yes"} {
		if _, e := yamlValue([]byte(b)); e != nil {
			t.Errorf("valid YAML %q: %v", b, e)
		}
	}
	if _, e := JSONValue([]byte(strings.Repeat("[", 65) + "0" + strings.Repeat("]", 65))); e == nil {
		t.Fatal("depth accepted")
	}
	if _, e := JSONValue(make([]byte, MaxDocumentBytes+1)); e == nil {
		t.Fatal("size accepted")
	}
}
func TestJCSExactNumbersAndUnicodeOrdering(t *testing.T) {
	_, c, e := Canonical([]byte(`{"b":1.00,"a":0.1,"z":"<&","\ud83d\ude00":1,"\ue000":2}`), "json")
	if e != nil {
		t.Fatal(e)
	}
	want := `{"a":0.1,"b":1,"z":"<&","😀":1,"":2}`
	if string(c) != want {
		t.Fatalf("canonical=%s", c)
	}
	for _, b := range []string{`{"n":9007199254740993}`, `{"n":0.123456789012345678901}`, `{"n":1e400}`} {
		if _, _, e := Canonical([]byte(b), "json"); e == nil {
			t.Fatal("lossy number accepted")
		}
	}
	if _, _, e := Canonical([]byte(`{"n":{"decimal":"0.123456789012345678901"}}`), "json"); e != nil {
		t.Fatal(e)
	}
}
func TestRawAndCanonicalAndUnknownFields(t *testing.T) {
	v := testDoc(t)
	raw := Encode(v)
	d, e := Parse(raw, "json")
	if e != nil {
		t.Fatal(e)
	}
	pretty, _ := json.MarshalIndent(v, "", "  ")
	other, e := Parse(pretty, "json")
	if e != nil {
		t.Fatal(e)
	}
	if Hash(d.Raw) == Hash(other.Raw) || Hash(d.Canonical) != Hash(other.Canonical) {
		t.Fatal("raw/content identity conflated")
	}
	v["unexpected"] = true
	if _, e := Parse(Encode(v), "json"); e == nil {
		t.Fatal("unknown field")
	}
	delete(v, "unexpected")
	v["apiVersion"] = "workkit/v9"
	if _, e := Parse(Encode(v), "json"); e == nil {
		t.Fatal("unknown version")
	}
}
func TestCorporaAndNoCapabilityPromotion(t *testing.T) {
	for _, name := range []string{"offline", "one-provider", "multi-provider", "native-only"} {
		root := filepath.Join("../../testdata/corpora", name)
		if _, e := Resolve(context.Background(), filepath.Join(root, "workspace.json"), root); e != nil {
			t.Fatalf("%s: %v", name, e)
		}
	}
}
func copyCorpus(t *testing.T, name string) string {
	root := canonicalDir(t)
	e := filepath.WalkDir(filepath.Join("../../testdata/corpora", name), func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		r, _ := filepath.Rel(filepath.Join("../../testdata/corpora", name), p)
		to := filepath.Join(root, r)
		if d.IsDir() {
			return os.MkdirAll(to, 0700)
		}
		b, e := os.ReadFile(p)
		if e != nil {
			return e
		}
		return os.WriteFile(to, b, 0600)
	})
	if e != nil {
		t.Fatal(e)
	}
	return root
}
func TestResolverHashConflictMissingAndExtensionOpacity(t *testing.T) {
	for _, mode := range []string{"tamper", "missing", "duplicate", "symlink"} {
		t.Run(mode, func(t *testing.T) {
			root := copyCorpus(t, "one-provider")
			p := filepath.Join(root, "resources/github.json")
			switch mode {
			case "tamper":
				f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
				f.WriteString(" ")
				f.Close()
			case "missing":
				os.Remove(p)
			case "duplicate":
				b, _ := os.ReadFile(p)
				os.WriteFile(filepath.Join(root, "resources/duplicate.json"), b, 0600)
			case "symlink":
				os.Remove(p)
				os.Symlink("/unavailable", p)
			}
			if _, e := Resolve(context.Background(), filepath.Join(root, "workspace.json"), root); e == nil {
				t.Fatal("invalid graph accepted")
			}
		})
	}
	root := copyCorpus(t, "offline")
	p := filepath.Join(root, "workspace.json")
	raw, _ := os.ReadFile(p)
	v, _ := JSONValue(raw)
	Map(v)["extensions"] = map[string]any{"provider/raw": map[string]any{"kind": "ConnectionProfile", "id": "unresolved", "revision": 1, "sha256": strings.Repeat("a", 64)}}
	os.WriteFile(p, Encode(v), 0600)
	if _, e := Resolve(context.Background(), p, root); e != nil {
		t.Fatal("extension treated as executable ref", e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := Resolve(ctx, p, root); e == nil {
		t.Fatal("cancellation ignored")
	}
}
func TestImmutableWriteAndUnsafePaths(t *testing.T) {
	root := canonicalDir(t)
	out := filepath.Join(root, "bundle")
	files := map[string][]byte{"assets/a": []byte("a"), "bundle.json": []byte("{}")}
	if e := WriteBundle(context.Background(), out, files, ""); e != nil {
		t.Fatal(e)
	}
	if e := WriteBundle(context.Background(), out, files, ""); e == nil {
		t.Fatal("overwrite")
	}
	for _, p := range []string{"../escape", "/absolute", "a/../b", "a\\b"} {
		if _, e := Confined(root, p); e == nil {
			t.Fatal("unsafe asset")
		}
	}
	if e := WriteBundle(context.Background(), filepath.Join(root, "invalid"), map[string][]byte{"../escape": nil}, ""); e == nil {
		t.Fatal("unsafe output")
	}
}

func TestUpstreamJCSVectors(t *testing.T) {
	paths, _ := filepath.Glob("../../testdata/jcs/input/*.json")
	if len(paths) != 6 {
		t.Fatalf("upstream vector coverage %d", len(paths))
	}
	for _, p := range paths {
		raw, _ := os.ReadFile(p)
		want, _ := os.ReadFile(filepath.Join("../../testdata/jcs/output", filepath.Base(p)))
		got, e := jcs.Transform(raw)
		if e != nil || !bytes.Equal(got, bytes.TrimSpace(want)) {
			t.Errorf("upstream %s: %v", p, e)
		}
	}
}
func TestOperationCyclesAndMissingDependencies(t *testing.T) {
	for _, graph := range []map[string][]string{{"one": {"two"}, "two": {"one"}}, {"one": {"missing"}}} {
		if CheckOperationGraph(graph) == nil {
			t.Fatal("cycle or missing dependency accepted")
		}
	}
	if e := CheckOperationGraph(map[string][]string{"one": {}, "two": {"one"}}); e != nil {
		t.Fatal(e)
	}
}

func TestRevisionIdentityNormalizesExactIntegerSpellings(t *testing.T) {
	root := copyCorpus(t, "one-provider")
	p := filepath.Join(root, "resources/github.json")
	b, _ := os.ReadFile(p)
	v, _ := JSONValue(b)
	Map(Map(v)["metadata"])["revision"] = json.Number("1.0")
	raw := Encode(v)
	os.WriteFile(p, raw, 0600)
	p = filepath.Join(root, "workspace.json")
	b, _ = os.ReadFile(p)
	v, _ = JSONValue(b)
	ref := Map(List(Map(Map(v)["spec"])["connections"])[0])
	ref["sha256"] = Hash(raw)
	ref["revision"] = json.Number("1e0")
	os.WriteFile(p, Encode(v), 0600)
	if _, e := Resolve(context.Background(), p, root); e != nil {
		t.Fatal("integer identity coerced by lexeme", e)
	}
	Map(Map(v)["metadata"])["revision"] = json.Number("1e20")
	if _, e := Parse(Encode(v), "json"); e == nil {
		t.Fatal("unsafe author revision")
	}
}
