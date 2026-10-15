package codex

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/nativeadapter"
)

func fixture(t *testing.T) []nativeadapter.File {
	t.Helper()
	b, err := os.ReadFile("testdata/probe.toml")
	if err != nil {
		t.Fatal(err)
	}
	return []nativeadapter.File{{Path: ".codex/agents/aok-probe.toml", Format: "toml", Data: b}}
}

func newEngine(t *testing.T) *nativeadapter.Engine {
	t.Helper()
	r := nativeadapter.NewRegistry()
	if err := r.Register(New()); err != nil {
		t.Fatal(err)
	}
	return nativeadapter.NewEngine(r)
}

func TestDecodeMapsRequiredFieldsAndRetainsUnknownTOML(t *testing.T) {
	doc, ds, err := New().Decode(projectScope, fixture(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(ds) != 0 {
		t.Fatalf("unexpected diagnostics: %+v", ds)
	}
	assertRaw(t, doc.Fields, "agent.metadata.id", `"aok-probe"`)
	assertRaw(t, doc.Fields, "agent.identity.purpose", `"Synthetic read-only discovery fixture"`)
	assertRaw(t, doc.Fields, "executionProfile.model.selector", `"gpt-6-luna"`)
	assertRaw(t, doc.Fields, "executionProfile.model.reasoning.mode", `"effort"`)
	assertRaw(t, doc.Fields, "executionProfile.model.reasoning.effort", `"medium"`)
	assertRaw(t, doc.Fields, "agent.instructions.sources", `[{"path":"instructions.md"}]`)
	assertRaw(t, doc.Unknown, "toml.features", `{"future_capability":true,"shell_tool":false}`)
	if string(doc.Assets["instructions.md"]) != "Read-only synthetic discovery fixture." {
		t.Fatalf("instruction bytes changed: %q", doc.Assets["instructions.md"])
	}
	if New().Descriptor().HostLoading.Status != "not-run" {
		t.Fatal("app-server config/read with agents=null must not imply host loading")
	}
}

func TestEngineImportExportSemanticRoundTrip(t *testing.T) {
	e := newEngine(t)
	input := fixture(t)
	b, imported := e.Import(target, projectScope, input, nil, nil)
	if imported.Status != "blocked" || b.Completeness != "draft-incomplete" {
		t.Fatalf("incomplete import boundary lost: %+v", imported)
	}
	files, next, report := e.Export(target, projectScope, input, b, nil)
	if report.Status != "blocked" {
		t.Fatalf("export should remain an incomplete draft: %+v", report)
	}
	if len(files) != 1 || files[0].Path != ".codex/agents/aok-probe.toml" || files[0].Format != "toml" {
		t.Fatalf("unexpected rendered file: %+v", files)
	}
	decoded, _, err := New().Decode(projectScope, files)
	if err != nil {
		t.Fatal(err)
	}
	if !documentsEquivalent(b.Document, decoded) {
		t.Fatalf("native roundtrip changed semantics\nwant=%s\ngot =%s", mustJSON(t, b.Document), mustJSON(t, decoded))
	}
	if next.Digest == "" {
		t.Fatal("export did not update bundle digest")
	}
}

func TestOmittedModelAndEffortDefaultsRemainOmitted(t *testing.T) {
	file := []nativeadapter.File{{Path: ".codex/agents/default.toml", Format: "toml", Data: []byte("name='default'\ndescription='Default agent'\ndeveloper_instructions='body'\n")}}
	doc, _, err := New().Decode(projectScope, file)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := doc.Fields["executionProfile.model.selector"]; ok {
		t.Fatal("missing model was synthesized")
	}
	assertRaw(t, doc.Fields, "executionProfile.model.reasoning.mode", `"provider-default"`)
	if _, ok := doc.Fields["executionProfile.model.reasoning.effort"]; ok {
		t.Fatal("missing effort was synthesized")
	}
	files, _, err := New().Encode(projectScope, doc)
	if err != nil {
		t.Fatal(err)
	}
	text := string(files[0].Data)
	for _, key := range []string{"model =", "model_reasoning_effort ="} {
		if strings.Contains(text, key) {
			t.Fatalf("encoder invented omitted default %q: %s", key, text)
		}
	}
}

func TestRequiredPermissionAndMCPControlsBlock(t *testing.T) {
	report := newEngine(t).Inspect(target, projectScope, fixture(t), []nativeadapter.Control{
		{Path: "agent.permissions.filesystem.write", Required: true},
		{Path: "agent.permissions.network.mode", Required: true},
		{Path: "agent.capabilities.mcps", Required: true},
	})
	if report.Status != "blocked" {
		t.Fatalf("required unsupported controls did not block: %+v", report)
	}
	for _, field := range []string{"agent.permissions.filesystem.write", "agent.permissions.network.mode", "agent.capabilities.mcps"} {
		found := false
		for _, d := range report.Diagnostics {
			if d.Code == "native.control.required-unsupported" && d.Field == field {
				found = true
			}
		}
		if !found {
			t.Errorf("missing unsupported control diagnostic for %s: %+v", field, report.Diagnostics)
		}
	}
}

func TestRejectsMalformedOrIllTypedTOML(t *testing.T) {
	cases := []struct{ name, path, format, source string }{
		{"duplicate", ".codex/agents/a.toml", "toml", "name='a'\nname='b'\ndescription='d'\ndeveloper_instructions='i'\n"},
		{"missing-name", ".codex/agents/a.toml", "toml", "description='d'\ndeveloper_instructions='i'\n"},
		{"wrong-name-type", ".codex/agents/a.toml", "toml", "name=1\ndescription='d'\ndeveloper_instructions='i'\n"},
		{"wrong-description-type", ".codex/agents/a.toml", "toml", "name='a'\ndescription=true\ndeveloper_instructions='i'\n"},
		{"missing-instructions", ".codex/agents/a.toml", "toml", "name='a'\ndescription='d'\n"},
		{"wrong-instructions-type", ".codex/agents/a.toml", "toml", "name='a'\ndescription='d'\ndeveloper_instructions=2\n"},
		{"wrong-model-type", ".codex/agents/a.toml", "toml", "name='a'\ndescription='d'\ndeveloper_instructions='i'\nmodel=3\n"},
		{"bad-effort", ".codex/agents/a.toml", "toml", "name='a'\ndescription='d'\ndeveloper_instructions='i'\nmodel_reasoning_effort='' \n"},
		{"wrong-path", "agents/a.toml", "toml", "name='a'\ndescription='d'\ndeveloper_instructions='i'\n"},
		{"wrong-format", ".codex/agents/a.toml", "json", `{"name":"a"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := New().Decode(projectScope, []nativeadapter.File{{Path: tc.path, Format: tc.format, Data: []byte(tc.source)}})
			if err == nil {
				t.Fatal("expected rejection")
			}
		})
	}
	if _, _, err := New().Decode(projectScope, append(fixture(t), fixture(t)...)); err == nil {
		t.Fatal("multiple input files accepted")
	}
}

func TestEncodeRejectsUnsupportedReasoningAndSourceShapes(t *testing.T) {
	doc := minimalDoc()
	doc.Assets = map[string][]byte{"instructions.md": []byte("instructions")}
	doc.Fields["executionProfile.model.reasoning.mode"] = json.RawMessage(`"budget"`)
	if _, _, err := New().Encode(projectScope, doc); err == nil {
		t.Fatal("unsupported budget reasoning mode accepted")
	}
	doc.Fields["executionProfile.model.reasoning.mode"] = json.RawMessage(`"provider-default"`)
	doc.Fields["executionProfile.model.reasoning.effort"] = json.RawMessage(`"low"`)
	if _, _, err := New().Encode(projectScope, doc); err == nil {
		t.Fatal("provider-default with explicit effort accepted")
	}
	delete(doc.Fields, "executionProfile.model.reasoning.effort")
	doc.Fields["agent.instructions.sources"] = json.RawMessage(`[{"path":"a.md"},{"path":"b.md"}]`)
	if _, _, err := New().Encode(projectScope, doc); err == nil {
		t.Fatal("multiple instruction sources accepted")
	}
	doc.Fields["agent.instructions.sources"] = json.RawMessage(`[{"path":"instructions.md"}]`)
	doc.Assets["extra.md"] = []byte("extra")
	if _, _, err := New().Encode(projectScope, doc); err == nil {
		t.Fatal("unreferenced asset accepted")
	}
}

func TestDecodeWarnsButPreservesNativeIdentityWhenFilenameDiffers(t *testing.T) {
	file := []nativeadapter.File{{Path: ".codex/agents/old-name.toml", Format: "toml", Data: []byte("name='new-name'\ndescription='Agent'\ndeveloper_instructions='Body'\n")}}
	doc, ds, err := New().Decode(projectScope, file)
	if err != nil {
		t.Fatal(err)
	}
	assertRaw(t, doc.Fields, "agent.metadata.id", `"new-name"`)
	if len(ds) != 1 || ds[0].Code != "codex.filename.name-mismatch" {
		t.Fatalf("expected filename warning: %+v", ds)
	}
}

func minimalDoc() nativeadapter.Document {
	return nativeadapter.Document{Fields: map[string]json.RawMessage{
		"agent.metadata.id":          json.RawMessage(`"minimal"`),
		"agent.identity.purpose":     json.RawMessage(`"Minimal agent"`),
		"agent.instructions.sources": json.RawMessage(`[{"path":"instructions.md"}]`),
	}, Unknown: map[string]json.RawMessage{}, Assets: map[string][]byte{}}
}

func assertRaw(t *testing.T, values map[string]json.RawMessage, key, want string) {
	t.Helper()
	if string(values[key]) != want {
		t.Fatalf("%s = %s, want %s", key, values[key], want)
	}
}
func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func documentsEquivalent(a, b nativeadapter.Document) bool {
	if len(a.Fields) != len(b.Fields) || len(a.Unknown) != len(b.Unknown) || len(a.Assets) != len(b.Assets) {
		return false
	}
	for k, v := range a.Fields {
		if string(v) != string(b.Fields[k]) {
			return false
		}
	}
	for k, v := range a.Unknown {
		if string(v) != string(b.Unknown[k]) {
			return false
		}
	}
	for k, v := range a.Assets {
		if string(v) != string(b.Assets[k]) {
			return false
		}
	}
	return true
}

func TestProviderAdvertisedEffortIsPreserved(t *testing.T) {
	files := []nativeadapter.File{{Path: ".codex/agents/a.toml", Format: "toml", Data: []byte("name='a'\ndescription='d'\ndeveloper_instructions='i'\nmodel_reasoning_effort='ultra'\n")}}
	registry := nativeadapter.NewRegistry()
	if err := registry.Register(New()); err != nil {
		t.Fatal(err)
	}
	engine := nativeadapter.NewEngine(registry)
	b, r := engine.Import("codex", "project", files, nil, nil)
	if b.Version == "" || r.Status != "blocked" {
		t.Fatalf("provider effort rejected: %+v", r)
	}
	out, _, r := engine.Export("codex", "project", files, b, nil)
	if len(out) != 1 || r.Status != "blocked" {
		t.Fatalf("roundtrip: %+v", r)
	}
}
