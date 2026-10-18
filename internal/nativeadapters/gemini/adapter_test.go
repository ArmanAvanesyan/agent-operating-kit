package gemini

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/nativeadapter"
)

func TestGeminiAdapterMapsSupportedFieldsAndPreservesUnknowns(t *testing.T) {
	data, err := os.ReadFile("testdata/security-auditor.md")
	if err != nil {
		t.Fatal(err)
	}
	files := []nativeadapter.File{{Path: ".gemini/agents/reviewer.md", Format: "markdown", Data: data}}
	doc, diagnostics, err := New().Decode("project", files)
	if err != nil || len(diagnostics) != 0 {
		t.Fatalf("decode: diagnostics=%+v err=%v", diagnostics, err)
	}
	if string(doc.Fields["agent.metadata.id"]) != `"security-auditor"` || string(doc.Fields["agent.identity.purpose"]) != `"Specialized in finding security vulnerabilities in code."` || string(doc.Fields["executionProfile.model.selector"]) != `"gemini-3-flash-preview"` {
		t.Fatalf("mapped projection: %+v", doc.Fields)
	}
	for key, want := range map[string]string{
		"frontmatter.kind":        `"local"`,
		"frontmatter.tools":       `["read_file","grep_search"]`,
		"frontmatter.temperature": "0.2",
		"frontmatter.max_turns":   "10",
	} {
		if string(doc.Unknown[key]) != want {
			t.Fatalf("unknown %s=%s; want %s", key, doc.Unknown[key], want)
		}
	}
	if string(doc.Assets["instructions.md"]) != "You are a security auditor. Identify vulnerabilities and report concrete evidence.\n" {
		t.Fatalf("prompt body changed: %q", doc.Assets["instructions.md"])
	}
	if string(doc.Unknown["file.name"]) != `".gemini/agents/reviewer.md"` {
		t.Fatalf("mismatched source filename was not retained: %+v", doc.Unknown)
	}
	outputs, _, err := New().Encode("project", doc)
	if err != nil || len(outputs) != 1 || outputs[0].Path != files[0].Path {
		t.Fatalf("encode: outputs=%+v err=%v", outputs, err)
	}
	roundtrip, _, err := New().Decode("project", outputs)
	if err != nil || !sameDocument(doc, roundtrip) {
		t.Fatalf("semantic roundtrip failed: err=%v\noriginal=%+v\nroundtrip=%+v", err, doc, roundtrip)
	}
}

func TestEngineImportExportCompareAndHostUnavailability(t *testing.T) {
	registry := nativeadapter.NewRegistry()
	if err := registry.Register(New()); err != nil {
		t.Fatal(err)
	}
	engine := nativeadapter.NewEngine(registry)
	data, err := os.ReadFile("testdata/security-auditor.md")
	if err != nil {
		t.Fatal(err)
	}
	input := []nativeadapter.File{{Path: ".gemini/agents/security-auditor.md", Format: "md", Data: data}}
	unsupported := []nativeadapter.Control{{Path: "agent.permissions.network.mode", Required: true}}
	bundle, imported := engine.Import("gemini", "project", input, nil, unsupported)
	if imported.Status != "blocked" || bundle.Digest == "" || imported.HostLoading.Status != "unavailable" || imported.RuntimeEnforcement != "unverified" {
		t.Fatalf("import claimed availability or gate completion: %+v", imported)
	}
	assertDiagnostic(t, imported, "native.control.required-unsupported")
	outputs, renderedBundle, exported := engine.Export("gemini", "project", input, bundle, nil)
	if exported.Status != "blocked" || len(outputs) != 1 || renderedBundle.Digest == "" || exported.HostLoading.Status != "unavailable" || exported.RuntimeEnforcement != "unverified" {
		t.Fatalf("export candidate/host state: files=%d report=%+v", len(outputs), exported)
	}
	compared := engine.Compare("gemini", "project", outputs, renderedBundle, nil)
	if compared.Status != "valid" || compared.MappingStatus != "compatible" || len(compared.Differences) != 0 || compared.HostLoading.Status != "unavailable" || compared.RuntimeEnforcement != "unverified" {
		t.Fatalf("semantic compare or unavailable host state: %+v", compared)
	}
}

func TestGeminiRequiredMetadataAndProjectScopeAreEnforced(t *testing.T) {
	cases := []struct{ path, data, scope string }{
		{".gemini/agents/x.md", "---\ndescription: d\n---\nprompt", "project"},
		{".gemini/agents/x.md", "---\nname: x\n---\nprompt", "project"},
		{".gemini/agents/x.md", "---\nname: Invalid Name\ndescription: d\n---\nprompt", "project"},
		{".gemini/agents/x.md", "---\nname: x\ndescription: d\nmodel: [bad]\n---\nprompt", "project"},
		{".gemini/agents/nested/x.md", "---\nname: x\ndescription: d\n---\nprompt", "project"},
		{"agents/x.md", "---\nname: x\ndescription: d\n---\nprompt", "project"},
		{".gemini/agents/x.md", "---\nname: x\ndescription: d\n---\nprompt", "user"},
	}
	for _, tc := range cases {
		if _, _, err := New().Decode(tc.scope, []nativeadapter.File{{Path: tc.path, Format: "md", Data: []byte(tc.data)}}); err == nil {
			t.Fatalf("accepted invalid document path=%q scope=%q", tc.path, tc.scope)
		}
	}
}

func TestGeminiFrontmatterRejectsDuplicateAndUnsafeYAML(t *testing.T) {
	for name, frontmatter := range map[string]string{
		"duplicate": "name: x\nname: y\ndescription: d\n",
		"anchor":    "name: x\ndescription: d\nbase: &v text\nalias: *v\n",
		"tag":       "name: x\ndescription: d\nextra: !custom value\n",
		"merge":     "name: x\ndescription: d\nbase: &v {extra: true}\n<<: *v\n",
		"bad model": "name: x\ndescription: d\nmodel: [bad]\n",
	} {
		t.Run(name, func(t *testing.T) {
			data := []byte("---\n" + frontmatter + "---\nprompt")
			if _, _, err := New().Decode("project", []nativeadapter.File{{Path: ".gemini/agents/x.md", Format: "md", Data: data}}); err == nil {
				t.Fatal("unsafe Gemini frontmatter was accepted")
			}
		})
	}
}

func TestEncodeRejectsUnknownCanonicalFieldAndLossyAssetSet(t *testing.T) {
	doc, _, err := New().Decode("project", []nativeadapter.File{{Path: ".gemini/agents/x.md", Format: "md", Data: []byte("---\nname: x\ndescription: y\n---\nbody")}})
	if err != nil {
		t.Fatal(err)
	}
	bad := cloneDocument(doc)
	bad.Fields["agent.permissions.network.mode"] = json.RawMessage(`"deny"`)
	if _, _, err := New().Encode("project", bad); err == nil {
		t.Fatal("unsupported canonical control was silently dropped")
	}
	bad = cloneDocument(doc)
	bad.Assets["other.md"] = []byte("extra")
	if _, _, err := New().Encode("project", bad); err == nil {
		t.Fatal("unmapped asset was silently dropped")
	}
	bad = cloneDocument(doc)
	bad.Fields["agent.instructions.sources"] = json.RawMessage(`[{"path":"different.md"}]`)
	if _, _, err := New().Encode("project", bad); err == nil {
		t.Fatal("unmapped instruction source was silently dropped")
	}
}

func assertDiagnostic(t *testing.T, report nativeadapter.Report, code string) {
	t.Helper()
	for _, diagnostic := range report.Diagnostics {
		if diagnostic.Code == code {
			return
		}
	}
	t.Fatalf("missing diagnostic %s: %+v", code, report.Diagnostics)
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

func TestNestedMCPSecretCannotBeBundled(t *testing.T) {
	registry := nativeadapter.NewRegistry()
	if err := registry.Register(New()); err != nil {
		t.Fatal(err)
	}
	files := []nativeadapter.File{{Path: ".gemini/agents/probe.md", Format: "markdown", Data: []byte("---\nname: probe\ndescription: Probe\nmcpServers:\n  local:\n    env:\n      MCP_ACCESS_SECRET: private-fixture-value\n---\nPrompt\n")}}
	bundle, report := nativeadapter.NewEngine(registry).Import("gemini", "project", files, nil, nil)
	if bundle.Version != "" || report.Status != "blocked" {
		t.Fatalf("credential accepted: %+v", report)
	}
	data, _ := json.Marshal(report)
	if bytes.Contains(data, []byte("private-fixture-value")) {
		t.Fatal("secret echoed")
	}
}
