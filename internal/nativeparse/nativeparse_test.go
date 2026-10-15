package nativeparse

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDocumentFixtures(t *testing.T) {
	dir := filepath.Join("testdata")
	for _, tc := range []struct {
		file, format string
		wantError    bool
	}{
		{"positive.yaml", "yaml", false}, {"positive.jsonc", "jsonc", false},
		{"negative-duplicate.json", "json", true}, {"negative-alias.yaml", "yaml", true},
		{"negative-date.toml", "toml", true},
	} {
		data, err := os.ReadFile(filepath.Join(dir, tc.file))
		if err != nil {
			t.Fatal(err)
		}
		_, err = Decode(tc.format, data)
		if (err != nil) != tc.wantError {
			t.Errorf("%s: err=%v, wantError=%v", tc.file, err, tc.wantError)
		}
	}
}

func TestDecodeFormatsAndUnknownFields(t *testing.T) {
	cases := []struct{ format, input string }{
		{"json", `{"unknown":{"n":9007199254740993},"body":"keep"}`},
		{"jsonc", `{/* comment */ "unknown": [1, 2,], // tail
 "body": "/* literal */",}`},
		{"yaml", "unknown:\n  nested: [one, two]\nbody: keep\n"},
		{"toml", "unknown = { nested = [\"one\", \"two\"] }\nbody = \"keep\"\n"},
	}
	for _, tc := range cases {
		t.Run(tc.format, func(t *testing.T) {
			m, err := Decode(tc.format, []byte(tc.input))
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := m["unknown"]; !ok {
				t.Fatalf("unknown field lost: %#v", m)
			}
			if m["body"] != "keep" && tc.format != "jsonc" {
				t.Fatalf("body changed: %#v", m["body"])
			}
			if tc.format == "json" {
				n := m["unknown"].(map[string]any)["n"].(json.Number)
				if string(n) != "9007199254740993" {
					t.Fatalf("integer precision lost: %s", n)
				}
			}
		})
	}
}

func TestJSONCDoesNotRewriteStrings(t *testing.T) {
	m, err := Decode("jsonc", []byte(`{"url":"https://x/a//b","text":"comma, } // x /* y */","a":[1,2,],}`))
	if err != nil {
		t.Fatal(err)
	}
	if m["url"] != "https://x/a//b" || m["text"] != "comma, } // x /* y */" {
		t.Fatalf("string corrupted: %#v", m)
	}
}

func TestRejectsMalformedOrUnsafeDocuments(t *testing.T) {
	cases := []struct{ format, input string }{
		{"json", `{"a":1,"a":2}`}, {"json", `{"x":{"a":1,"a":2}}`},
		{"json", `{} {}`}, {"json", `{} trailing`}, {"json", `{"x":NaN}`},
		{"jsonc", `{"x": /* never closes`}, {"yaml", "a: 1\na: 2\n"},
		{"yaml", "outer:\n  inner: 1\n  inner: 2\n"}, {"yaml", "base: {x: 1}\nmerged:\n  <<: {y: 2}\n"},
		{"yaml", "a: &x 1\nb: *x\n"}, {"yaml", "a: !!str hi\n"},
		{"yaml", "!!str key: value\n"}, {"yaml", "&key key: value\n"},
		{"yaml", "a: !!map {b: c}\n"}, {"yaml", "a: !!seq [x, y]\n"},
		{"yaml", "value: 18446744073709551616\n"},
		{"yaml", "a: &a {x: 1}\n"}, {"yaml", "a: 1\n---\nb: 2\n"},
		{"yaml", "date: 2025-01-01\n"}, {"toml", "date = 1979-05-27\n"},
		{"toml", "x = nan\n"}, {"toml", "outer = { inner = 1, inner = 2 }\n"},
	}
	for _, tc := range cases {
		t.Run(tc.format+"/"+strings.ReplaceAll(tc.input, "\n", "|"), func(t *testing.T) {
			if _, err := Decode(tc.format, []byte(tc.input)); err == nil {
				t.Fatalf("accepted %q", tc.input)
			}
		})
	}
}

func TestEncodeRejectsDecimalPrecisionLoss(t *testing.T) {
	for _, format := range []string{"yaml", "toml"} {
		if _, err := Encode(format, map[string]any{"value": json.Number("1.0000000000000000001")}); err == nil {
			t.Errorf("%s silently rounded a high-precision decimal", format)
		}
		if _, err := Encode(format, map[string]any{"value": json.Number("0.1")}); err != nil {
			t.Errorf("%s rejected a representable decimal: %v", format, err)
		}
	}
}

func TestInputLimitsUTF8AndDepth(t *testing.T) {
	if _, err := Decode("json", append([]byte{0xef, 0xbb, 0xbf}, []byte(`{}`)...)); err == nil {
		t.Fatal("accepted BOM")
	}
	if _, err := Decode("json", []byte{0xff}); err == nil {
		t.Fatal("accepted invalid UTF-8")
	}
	if _, err := Decode("json", bytes.Repeat([]byte{' '}, MaxDocumentBytes+1)); err == nil {
		t.Fatal("accepted oversize document")
	}
	deep := `{"x":` + strings.Repeat("[", MaxDepth) + "0" + strings.Repeat("]", MaxDepth) + `}`
	if _, err := Decode("json", []byte(deep)); err == nil {
		t.Fatal("accepted excessive depth")
	}
}

func TestFrontmatterPreservesBody(t *testing.T) {
	src := []byte("---\ntitle: Hello\nunknown: 7\n---\n# body\n\nkeep\n")
	meta, body, err := Frontmatter(src)
	if err != nil {
		t.Fatal(err)
	}
	if meta["title"] != "Hello" || string(body) != "# body\n\nkeep\n" {
		t.Fatalf("metadata/body mismatch: %#v %q", meta, body)
	}
	if _, _, err := Frontmatter([]byte("# no frontmatter\n")); err == nil {
		t.Fatal("accepted absent frontmatter")
	}
	if _, _, err := Frontmatter([]byte("---\ntitle: x\n")); err == nil {
		t.Fatal("accepted unterminated frontmatter")
	}
}

func TestEncodeSemanticRoundTrip(t *testing.T) {
	original := map[string]any{"name": "basic", "enabled": true, "items": []any{"a", json.Number("2")}}
	for _, format := range []string{"json", "yaml", "toml"} {
		encoded, err := Encode(format, original)
		if err != nil {
			t.Fatalf("%s encode: %v", format, err)
		}
		decoded, err := Decode(format, encoded)
		if err != nil {
			t.Fatalf("%s decode: %v (%s)", format, err, encoded)
		}
		if decoded["name"] != "basic" || decoded["enabled"] != true {
			t.Fatalf("%s semantic mismatch: %#v", format, decoded)
		}
		items, ok := decoded["items"].([]any)
		if !ok || len(items) != 2 || items[0] != "a" || fmt.Sprint(items[1]) != "2" {
			t.Fatalf("%s changed array values: %#v", format, decoded["items"])
		}
	}
}

func TestJSONCRejectsCommaWithoutPrecedingValue(t *testing.T) {
	for _, input := range []string{`{,}`, `{"a":[,]}`, `{"a":1,,}`} {
		if _, err := Decode("jsonc", []byte(input)); err == nil {
			t.Fatalf("accepted malformed JSONC %s", input)
		}
	}
}
