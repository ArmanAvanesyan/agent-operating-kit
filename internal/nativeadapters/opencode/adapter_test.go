package opencode

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/nativeadapter"
)

func fixture(t *testing.T) []nativeadapter.File {
	t.Helper()
	b, err := os.ReadFile("testdata/probe.md")
	if err != nil {
		t.Fatal(err)
	}
	return []nativeadapter.File{{Path: ".opencode/agents/aok-probe.md", Format: "markdown", Data: b}}
}

func engine(t *testing.T) *nativeadapter.Engine {
	t.Helper()
	r := nativeadapter.NewRegistry()
	if err := r.Register(New()); err != nil {
		t.Fatal(err)
	}
	return nativeadapter.NewEngine(r)
}

func TestDecodePreservesBodyModelAndUnmappedFrontmatter(t *testing.T) {
	doc, ds, err := New().Decode(projectScope, fixture(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(ds) != 0 {
		t.Fatalf("unexpected diagnostics: %+v", ds)
	}
	assertJSON(t, doc.Fields["agent.metadata.id"], `"aok-probe"`)
	assertJSON(t, doc.Fields["agent.identity.purpose"], `"Synthetic read-only discovery fixture"`)
	assertJSON(t, doc.Fields["executionProfile.model.selector"], `"openai/gpt-6-luna"`)
	assertJSON(t, doc.Unknown["frontmatter.mode"], `"subagent"`)
	assertJSON(t, doc.Unknown["frontmatter.permission"], `{"bash":"deny","edit":"deny"}`)
	assertJSON(t, doc.Unknown["frontmatter.futureField"], `{"enabled":true}`)
	if string(doc.Assets["instructions.md"]) != "Read-only fixture body; this is not an executable instruction.\n" {
		t.Fatalf("body was changed: %q", doc.Assets["instructions.md"])
	}
}

func TestEngineImportExportSemanticRoundTrip(t *testing.T) {
	e := engine(t)
	input := fixture(t)
	b, report := e.Import(target, projectScope, input, nil, nil)
	if report.Status != "blocked" || b.Completeness != "draft-incomplete" {
		t.Fatalf("draft boundary not retained: %+v", report)
	}
	output, next, report := e.Export(target, projectScope, input, b, nil)
	if report.Status != "blocked" {
		t.Fatalf("export must remain a draft: %+v", report)
	}
	if len(output) != 1 || output[0].Path != ".opencode/agents/aok-probe.md" {
		t.Fatalf("unexpected output files: %+v", output)
	}
	decoded, _, err := New().Decode(projectScope, output)
	if err != nil {
		t.Fatal(err)
	}
	if string(decoded.Fields["agent.metadata.id"]) != string(b.Document.Fields["agent.metadata.id"]) || string(decoded.Fields["agent.identity.purpose"]) != string(b.Document.Fields["agent.identity.purpose"]) || string(decoded.Fields["executionProfile.model.selector"]) != string(b.Document.Fields["executionProfile.model.selector"]) {
		t.Fatalf("mapped semantics changed across roundtrip: %+v", decoded.Fields)
	}
	for k, v := range b.Document.Unknown {
		if string(decoded.Unknown[k]) != string(v) {
			t.Fatalf("unknown field %s changed: %s => %s", k, v, decoded.Unknown[k])
		}
	}
	if string(decoded.Assets["instructions.md"]) != string(b.Document.Assets["instructions.md"]) {
		t.Fatal("body changed across roundtrip")
	}
	if next.Digest == "" {
		t.Fatal("export did not update bundle digest")
	}
}

func TestOmittedModelDefaultsStayOmitted(t *testing.T) {
	file := []nativeadapter.File{{Path: ".opencode/agents/basic.md", Format: "markdown", Data: []byte("---\ndescription: Basic agent\n---\nBody\n")}}
	doc, _, err := New().Decode(projectScope, file)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"executionProfile.model.selector", "executionProfile.model.generation.temperature", "executionProfile.model.generation.topP"} {
		if _, ok := doc.Fields[key]; ok {
			t.Fatalf("omitted native default was synthesized at %s", key)
		}
	}
	out, _, err := New().Encode(projectScope, doc)
	if err != nil {
		t.Fatal(err)
	}
	front := string(out[0].Data)
	for _, key := range []string{"model:", "temperature:", "top_p:"} {
		if strings.Contains(front, key) {
			t.Fatalf("encoder invented omitted default %q: %s", key, front)
		}
	}
}

func TestRequiredFilesystemAndMCPControlsBlockWithoutInference(t *testing.T) {
	report := engine(t).Inspect(target, projectScope, fixture(t), []nativeadapter.Control{
		{Path: "agent.permissions.filesystem.write", Required: true},
		{Path: "agent.capabilities.mcps", Required: true},
	})
	if report.Status != "blocked" {
		t.Fatalf("required unsupported controls did not block: %+v", report)
	}
	for _, path := range []string{"agent.permissions.filesystem.write", "agent.capabilities.mcps"} {
		found := false
		for _, d := range report.Diagnostics {
			if d.Field == path && d.Code == "native.control.required-unsupported" {
				found = true
			}
		}
		if !found {
			t.Errorf("missing unsupported-control diagnostic for %s: %+v", path, report.Diagnostics)
		}
	}
}

func TestRejectsUnsafeOrAmbiguousMarkdown(t *testing.T) {
	cases := []struct{ name, path, format, body string }{
		{"duplicate", ".opencode/agents/a.md", "markdown", "---\ndescription: one\ndescription: two\n---\n"},
		{"tag", ".opencode/agents/a.md", "markdown", "---\ndescription: !!str hello\n---\n"},
		{"alias", ".opencode/agents/a.md", "markdown", "---\nbase: &x hello\ndescription: *x\n---\n"},
		{"wrong-description-type", ".opencode/agents/a.md", "markdown", "---\ndescription: 12\n---\n"},
		{"missing-description", ".opencode/agents/a.md", "markdown", "---\nmodel: x/y\n---\n"},
		{"wrong-path", "agents/a.md", "markdown", "---\ndescription: hi\n---\n"},
		{"wrong-format", ".opencode/agents/a.md", "yaml", "description: hi"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := New().Decode(projectScope, []nativeadapter.File{{Path: tc.path, Format: tc.format, Data: []byte(tc.body)}})
			if err == nil {
				t.Fatal("expected rejection")
			}
		})
	}
	if _, _, err := New().Decode(projectScope, append(fixture(t), fixture(t)...)); err == nil {
		t.Fatal("multiple native files accepted")
	}
}

func TestEncodeRefusesUnmaterializedOrInvalidInstructions(t *testing.T) {
	doc := minimalDocument()
	if _, _, err := New().Encode(projectScope, doc); err == nil {
		t.Fatal("missing instruction asset accepted")
	}
	doc.Assets = map[string][]byte{"instructions.md": []byte("body"), "extra.md": []byte("extra")}
	if _, _, err := New().Encode(projectScope, doc); err == nil {
		t.Fatal("unreferenced asset accepted")
	}
	doc.Assets = map[string][]byte{"instructions.md": []byte("body")}
	doc.Fields["agent.instructions.sources"] = json.RawMessage(`[{"path":"a.md"},{"path":"b.md"}]`)
	if _, _, err := New().Encode(projectScope, doc); err == nil {
		t.Fatal("multiple instruction sources accepted")
	}
}

func TestBoundedMappedNativeValues(t *testing.T) {
	cases := []string{
		"---\ndescription: x\ntemperature: 2.1\n---\n",
		"---\ndescription: x\ntop_p: -0.1\n---\n",
		"---\ndescription: x\nmodel: 1\n---\n",
	}
	for i, src := range cases {
		if _, _, err := New().Decode(projectScope, []nativeadapter.File{{Path: ".opencode/agents/a.md", Format: "markdown", Data: []byte(src)}}); err == nil {
			t.Errorf("case %d unexpectedly accepted", i)
		}
	}
}

func minimalDocument() nativeadapter.Document {
	return nativeadapter.Document{Fields: map[string]json.RawMessage{
		"agent.metadata.id":          json.RawMessage(`"minimal"`),
		"agent.identity.purpose":     json.RawMessage(`"Minimal agent"`),
		"agent.instructions.sources": json.RawMessage(`[{"path":"instructions.md"}]`),
	}, Unknown: map[string]json.RawMessage{}, Assets: map[string][]byte{}}
}

func assertJSON(t *testing.T, got json.RawMessage, want string) {
	t.Helper()
	if string(got) != want {
		t.Fatalf("got %s want %s", got, want)
	}
}

func TestFractionalSamplingAndSingleFileInput(t *testing.T) {
	registry := nativeadapter.NewRegistry()
	if err := registry.Register(New()); err != nil {
		t.Fatal(err)
	}
	engine := nativeadapter.NewEngine(registry)
	files := []nativeadapter.File{{Path: "sampling.md", Format: "markdown", Data: []byte("---\ndescription: sample\ntemperature: 0.3\ntop_p: 0.9\nfuture.key: retained\n---\nLiteral body\n")}}
	b, r := engine.Import("opencode", "project", files, nil, nil)
	if b.Version == "" {
		t.Fatalf("fractional fields rejected: %+v", r)
	}
	rendered, _, r := engine.Export("opencode", "project", files, b, nil)
	if len(rendered) != 1 {
		t.Fatalf("roundtrip failed: %+v", r)
	}
}
