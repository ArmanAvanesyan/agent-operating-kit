package agy

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/nativeadapter"
)

func fixture(t *testing.T, path string) []nativeadapter.File {
	t.Helper()
	b, err := os.ReadFile("testdata/probe.md")
	if err != nil {
		t.Fatal(err)
	}
	return []nativeadapter.File{{Path: path, Format: "markdown", Data: b}}
}

func engine(t *testing.T) *nativeadapter.Engine {
	t.Helper()
	r := nativeadapter.NewRegistry()
	if err := r.Register(New()); err != nil {
		t.Fatal(err)
	}
	return nativeadapter.NewEngine(r)
}

func TestDecodeNativeFrontmatterAndPreserveUnknowns(t *testing.T) {
	files := fixture(t, ".agents/agents/aok-probe/agent.md")
	doc, ds, err := New().Decode(projectScope, files)
	if err != nil {
		t.Fatal(err)
	}
	if len(ds) != 0 {
		t.Fatalf("unexpected diagnostics: %+v", ds)
	}
	assertJSON(t, doc.Fields["agent.metadata.id"], `"aok-probe"`)
	assertJSON(t, doc.Fields["agent.identity.purpose"], `"Synthetic AGY adapter discovery fixture"`)
	assertJSON(t, doc.Fields["executionProfile.model.selector"], `"flash"`)
	assertJSON(t, doc.Unknown["frontmatter.subagent"], `true`)
	assertJSON(t, doc.Unknown["frontmatter.tools"], `["view_file"]`)
	assertJSON(t, doc.Unknown["frontmatter.futurePolicy"], `{"retain":true}`)
	if !strings.Contains(string(doc.Assets[assetName]), "This synthetic body is inert test content.") {
		t.Fatal("Markdown body was not retained")
	}
}

func TestFlatAndNestedNativePaths(t *testing.T) {
	for _, path := range []string{".agents/agents/aok-probe.md", ".agents/agents/aok-probe/agent.md"} {
		if _, _, err := New().Decode(projectScope, fixture(t, path)); err != nil {
			t.Errorf("%s: %v", path, err)
		}
	}
}

func TestEngineImportExportSemanticRoundTrip(t *testing.T) {
	e := engine(t)
	input := fixture(t, ".agents/agents/aok-probe.md")
	b, report := e.Import(target, projectScope, input, nil, nil)
	if report.Status != "blocked" || b.Completeness != "draft-incomplete" {
		t.Fatalf("draft boundary not retained: %+v", report)
	}
	out, next, report := e.Export(target, projectScope, input, b, nil)
	if report.Status != "blocked" {
		t.Fatalf("export must remain blocked as a draft: %+v", report)
	}
	if len(out) != 1 || out[0].Path != ".agents/agents/aok-probe/agent.md" {
		t.Fatalf("unexpected output: %+v", out)
	}
	decoded, _, err := New().Decode(projectScope, out)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"agent.metadata.id", "agent.identity.purpose", "executionProfile.model.selector", "agent.instructions.sources"} {
		if string(decoded.Fields[key]) != string(b.Document.Fields[key]) {
			t.Errorf("mapped field %s changed: %s => %s", key, b.Document.Fields[key], decoded.Fields[key])
		}
	}
	for key, value := range b.Document.Unknown {
		if string(decoded.Unknown[key]) != string(value) {
			t.Errorf("unknown %s changed: %s => %s", key, value, decoded.Unknown[key])
		}
	}
	if string(decoded.Assets[assetName]) != string(b.Document.Assets[assetName]) {
		t.Fatal("body changed across roundtrip")
	}
	if next.Digest == "" {
		t.Fatal("export did not update bundle digest")
	}
}

func TestOmittedModelRemainsOmittedAndDefaultIsDocumented(t *testing.T) {
	input := []nativeadapter.File{{Path: ".agents/agents/basic/agent.md", Format: "markdown", Data: []byte("---\nname: basic\ndescription: Basic agent\n---\nBody\n")}}
	doc, _, err := New().Decode(projectScope, input)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := doc.Fields["executionProfile.model.selector"]; ok {
		t.Fatal("omitted model default was synthesized")
	}
	out, _, err := New().Encode(projectScope, doc)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out[0].Data), "model:") {
		t.Fatalf("encoder invented omitted model default: %s", out[0].Data)
	}
}

func TestRequiredUnsupportedControlsBlock(t *testing.T) {
	r := engine(t).Inspect(target, projectScope, fixture(t, ".agents/agents/aok-probe.md"), []nativeadapter.Control{
		{Path: "agent.capabilities.tools", Required: true},
		{Path: "agent.permissions.filesystem.write", Required: true},
		{Path: "agent.permissions.network.mode", Required: true},
	})
	if r.HostLoading.Status != "verified" || r.RuntimeEnforcement != "unverified" {
		t.Fatalf("discovery must remain separate from runtime enforcement: %+v", r)
	}
	if r.Status != "blocked" {
		t.Fatalf("required unsupported controls did not block: %+v", r)
	}
	for _, field := range []string{"agent.capabilities.tools", "agent.permissions.filesystem.write", "agent.permissions.network.mode"} {
		found := false
		for _, d := range r.Diagnostics {
			if d.Field == field && d.Code == "native.control.required-unsupported" {
				found = true
			}
		}
		if !found {
			t.Errorf("missing block diagnostic for %s: %+v", field, r.Diagnostics)
		}
	}
}

func TestRejectsInvalidOrAmbiguousInputs(t *testing.T) {
	cases := []struct{ name, path, format, body string }{
		{"duplicate frontmatter key", ".agents/agents/x/agent.md", "markdown", "---\nname: x\nname: y\ndescription: hi\n---\n"},
		{"tag", ".agents/agents/x/agent.md", "markdown", "---\nname: x\ndescription: !!str hi\n---\n"},
		{"alias", ".agents/agents/x/agent.md", "markdown", "---\nname: x\nbase: &b hi\ndescription: *b\n---\n"},
		{"wrong name type", ".agents/agents/x/agent.md", "markdown", "---\nname: 3\ndescription: hi\n---\n"},
		{"missing description", ".agents/agents/x/agent.md", "markdown", "---\nname: x\n---\n"},
		{"bad model", ".agents/agents/x/agent.md", "markdown", "---\nname: x\ndescription: hi\nmodel: gemini-2.5-pro\n---\n"},
		{"wrong path", ".agents/other/x.md", "markdown", "---\nname: x\ndescription: hi\n---\n"},
		{"path mismatch", ".agents/agents/x/agent.md", "markdown", "---\nname: y\ndescription: hi\n---\n"},
		{"wrong format", ".agents/agents/x/agent.md", "yaml", "name: x\ndescription: hi\n"},
		{"traversal", ".agents/agents/../x.md", "markdown", "---\nname: x\ndescription: hi\n---\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := New().Decode(projectScope, []nativeadapter.File{{Path: tc.path, Format: tc.format, Data: []byte(tc.body)}}); err == nil {
				t.Fatal("invalid input accepted")
			}
		})
	}
	if _, _, err := New().Decode(projectScope, append(fixture(t, ".agents/agents/aok-probe.md"), fixture(t, ".agents/agents/aok-probe.md")...)); err == nil {
		t.Fatal("multiple native files accepted")
	}
}

func TestEncodeRejectsUnsafeOrUnmaterializedDocument(t *testing.T) {
	doc := nativeadapter.Document{Fields: map[string]json.RawMessage{"agent.metadata.id": json.RawMessage(`"../escape"`), "agent.identity.purpose": json.RawMessage(`"purpose"`), "agent.instructions.sources": json.RawMessage(`[ {"path":"instructions.md"} ]`)}}
	if _, _, err := New().Encode(projectScope, doc); err == nil {
		t.Fatal("unsafe id accepted")
	}
	doc.Fields["agent.metadata.id"] = json.RawMessage(`"agent"`)
	if _, _, err := New().Encode(projectScope, doc); err == nil {
		t.Fatal("missing instruction asset accepted")
	}
	doc.Assets = map[string][]byte{assetName: []byte("body"), "extra.md": []byte("extra")}
	if _, _, err := New().Encode(projectScope, doc); err == nil {
		t.Fatal("unreferenced asset accepted")
	}
}

func assertJSON(t *testing.T, got json.RawMessage, want string) {
	t.Helper()
	if string(got) != want {
		t.Fatalf("got %s want %s", got, want)
	}
}
