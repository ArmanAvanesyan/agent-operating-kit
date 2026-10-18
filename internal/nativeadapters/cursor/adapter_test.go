package cursor

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/nativeadapter"
)

func TestProjectSubagentRoundTripsMappedFieldsUnknownAndBody(t *testing.T) {
	data, err := os.ReadFile("testdata/reviewer.md")
	if err != nil {
		t.Fatal(err)
	}
	input := []nativeadapter.File{{Path: ".cursor/agents/reviewer.md", Format: "markdown", Data: data}}
	doc, diagnostics, err := New().Decode("project", input)
	if err != nil || len(diagnostics) != 0 {
		t.Fatalf("decode: diagnostics=%+v err=%v", diagnostics, err)
	}
	if string(doc.Fields["agent.metadata.id"]) != `"reviewer"` || string(doc.Fields["agent.identity.purpose"]) != `"Reviews changes for correctness and security."` || string(doc.Fields["executionProfile.model.selector"]) != `"inherit"` {
		t.Fatalf("mapped fields: %+v", doc.Fields)
	}
	if string(doc.Unknown["frontmatter.readonly"]) != "true" || string(doc.Unknown["frontmatter.is_background"]) != "false" {
		t.Fatalf("unknown Cursor controls were not retained: %+v", doc.Unknown)
	}
	if string(doc.Assets["instructions.md"]) != "Review the requested changes. Report concrete findings with file and line references.\n" {
		t.Fatalf("prompt body changed: %q", doc.Assets["instructions.md"])
	}
	outputs, _, err := New().Encode("project", doc)
	if err != nil || len(outputs) != 1 || outputs[0].Path != input[0].Path {
		t.Fatalf("encode: outputs=%+v err=%v", outputs, err)
	}
	roundtrip, _, err := New().Decode("project", outputs)
	if err != nil || !sameDocument(doc, roundtrip) {
		t.Fatalf("semantic roundtrip failed: err=%v\noriginal=%+v\nroundtrip=%+v", err, doc, roundtrip)
	}
}

func TestOptionalFrontmatterAndNameDerivedFromFilename(t *testing.T) {
	file := nativeadapter.File{Path: ".cursor/agents/from-file.md", Format: "md", Data: []byte("---\ndescription: Filename supplies identifier.\n---\nPrompt body\n")}
	doc, _, err := New().Decode("project", []nativeadapter.File{file})
	if err != nil {
		t.Fatal(err)
	}
	if string(doc.Fields["agent.metadata.id"]) != `"from-file"` || len(doc.Fields["executionProfile.model.selector"]) != 0 {
		t.Fatalf("optional fields mapped incorrectly: %+v", doc.Fields)
	}
	files, _, err := New().Encode("project", doc)
	if err != nil || len(files) != 1 {
		t.Fatalf("encode derived name: %+v %v", files, err)
	}
	roundtrip, _, err := New().Decode("project", files)
	if err != nil || !sameDocument(doc, roundtrip) {
		t.Fatalf("optional roundtrip mismatch: %v %+v", err, roundtrip)
	}
}

func TestRequiredUnsupportedCursorControlsStayUnsupported(t *testing.T) {
	registry := nativeadapter.NewRegistry()
	if err := registry.Register(New()); err != nil {
		t.Fatal(err)
	}
	engine := nativeadapter.NewEngine(registry)
	data, err := os.ReadFile("testdata/reviewer.md")
	if err != nil {
		t.Fatal(err)
	}
	files := []nativeadapter.File{{Path: ".cursor/agents/reviewer.md", Format: "markdown", Data: data}}
	controls := []nativeadapter.Control{{Path: "agent.permissions.filesystem.write", Required: true}}
	bundle, report := engine.Import("cursor", "project", files, nil, controls)
	if bundle.Version == "" || report.Status != "blocked" {
		t.Fatalf("import: %+v", report)
	}
	out, exported, report := engine.Export("cursor", "project", files, bundle, controls)
	if len(out) != 1 || report.Status != "blocked" {
		t.Fatalf("export: %+v", report)
	}
	if report = engine.Compare("cursor", "project", out, exported, controls); report.Status != "blocked" {
		t.Fatalf("required control accepted: %+v", report)
	}
	if report = engine.Compare("cursor", "project", out, exported, nil); report.Status != "valid" {
		t.Fatalf("semantic roundtrip failed: %+v", report)
	}
}

func TestRejectsUnsafeOrMalformedNativeFiles(t *testing.T) {
	base := ".cursor/agents/worker.md"
	cases := []struct {
		name string
		path string
		data string
	}{
		{"duplicate keys", base, "---\nname: x\nname: y\ndescription: z\n---\nbody"},
		{"anchor alias", base, "---\nname: x\ndescription: z\ncopy: &a text\nalias: *a\n---\nbody"},
		{"explicit tag", base, "---\nname: x\ndescription: z\nextra: !custom value\n---\nbody"},
		{"wrong explicit name type", base, "---\nname: 42\ndescription: z\n---\nbody"},
		{"wrong description type", base, "---\nname: x\ndescription: [bad]\n---\nbody"},
		{"wrong model type", base, "---\nname: x\ndescription: z\nmodel: [one]\n---\nbody"},
		{"missing opening delimiter", base, "name: x\ndescription: z\n---\nbody"},
		{"path traversal", ".cursor/agents/../bad.md", "---\nname: x\ndescription: z\n---\nbody"},
		{"not project path", "agents/x.md", "---\nname: x\ndescription: z\n---\nbody"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			file := nativeadapter.File{Path: tc.path, Format: "markdown", Data: []byte(tc.data)}
			if _, _, err := New().Decode("project", []nativeadapter.File{file}); err == nil {
				t.Fatal("malformed native file was accepted")
			}
		})
	}
	if _, _, err := New().Decode("user", []nativeadapter.File{{Path: base, Format: "md", Data: []byte("---\nname: x\ndescription: z\n---\nbody")}}); err == nil {
		t.Fatal("unsupported user scope was accepted")
	}
	files := []nativeadapter.File{{Path: base, Format: "md", Data: []byte("---\nname: x\ndescription: z\n---\nbody")}, {Path: base + ".other", Format: "md", Data: []byte("---\nname: y\ndescription: z\n---\nbody")}}
	if _, _, err := New().Decode("project", files); err == nil {
		t.Fatal("multiple native files were accepted")
	}
}

func TestEncodeRejectsLossyProjectionAndBadRetainedPath(t *testing.T) {
	doc, _, err := New().Decode("project", []nativeadapter.File{{Path: ".cursor/agents/x.md", Format: "md", Data: []byte("---\nname: x\ndescription: y\n---\nbody")}})
	if err != nil {
		t.Fatal(err)
	}
	bad := cloneDocument(doc)
	bad.Fields["agent.instructions.sources"] = json.RawMessage(`[{"path":"other.md"}]`)
	if _, _, err := New().Encode("project", bad); err == nil {
		t.Fatal("unsupported instruction source was silently dropped")
	}
	bad = cloneDocument(doc)
	bad.Fields["agent.permissions.network"] = json.RawMessage(`"denied"`)
	if _, _, err := New().Encode("project", bad); err == nil {
		t.Fatal("unsupported canonical control was silently dropped")
	}
	bad = cloneDocument(doc)
	bad.Unknown["file.name"] = json.RawMessage(`".cursor/agents/../../escape.md"`)
	if _, _, err := New().Encode("project", bad); err == nil {
		t.Fatal("traversal in retained native path was accepted")
	}
	bad = cloneDocument(doc)
	bad.Assets["extra.md"] = []byte("extra")
	if _, _, err := New().Encode("project", bad); err == nil {
		t.Fatal("additional asset was silently dropped")
	}
}

func sameDocument(a, b nativeadapter.Document) bool {
	if len(a.Fields) != len(b.Fields) || len(a.Unknown) != len(b.Unknown) || len(a.Assets) != len(b.Assets) {
		return false
	}
	for key, value := range a.Fields {
		if !jsonEqual(value, b.Fields[key]) {
			return false
		}
	}
	for key, value := range a.Unknown {
		if !jsonEqual(value, b.Unknown[key]) {
			return false
		}
	}
	for key, value := range a.Assets {
		if !reflect.DeepEqual(value, b.Assets[key]) {
			return false
		}
	}
	return true
}

func jsonEqual(a, b []byte) bool {
	x, errX := contracts.DecodeJSON(a)
	y, errY := contracts.DecodeJSON(b)
	return errX == nil && errY == nil && reflect.DeepEqual(x, y)
}

func cloneDocument(doc nativeadapter.Document) nativeadapter.Document {
	out := nativeadapter.Document{Fields: map[string]json.RawMessage{}, Unknown: map[string]json.RawMessage{}, Assets: map[string][]byte{}}
	for key, value := range doc.Fields {
		out.Fields[key] = append(json.RawMessage(nil), value...)
	}
	for key, value := range doc.Unknown {
		out.Unknown[key] = append(json.RawMessage(nil), value...)
	}
	for key, value := range doc.Assets {
		out.Assets[key] = append([]byte(nil), value...)
	}
	return out
}
