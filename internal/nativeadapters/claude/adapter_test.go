package claude

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/nativeadapter"
)

func TestProjectSubagentMapsNativeFieldsAndPreservesPromptAndUnknown(t *testing.T) {
	input := nativeadapter.File{Path: ".claude/agents/reviewer.md", Format: "markdown", Data: []byte("---\nname: code-reviewer\ndescription: Reviews changes\nmodel: sonnet\ncolor: blue\n---\nReview this code.\nKeep the response concise.\n")}
	doc, diagnostics, err := New().Decode("project", []nativeadapter.File{input})
	if err != nil || len(diagnostics) != 0 {
		t.Fatalf("decode: diagnostics=%+v err=%v", diagnostics, err)
	}
	if string(doc.Fields["agent.metadata.id"]) != `"code-reviewer"` || string(doc.Fields["agent.identity.purpose"]) != `"Reviews changes"` || string(doc.Fields["executionProfile.model.selector"]) != `"sonnet"` {
		t.Fatalf("mapped fields: %+v", doc.Fields)
	}
	if string(doc.Unknown["frontmatter.color"]) != `"blue"` || string(doc.Unknown["file.name"]) != `".claude/agents/reviewer.md"` {
		t.Fatalf("unknown provenance not retained: %+v", doc.Unknown)
	}
	if string(doc.Assets["instructions.md"]) != "Review this code.\nKeep the response concise.\n" {
		t.Fatalf("prompt body changed: %q", doc.Assets["instructions.md"])
	}
	outputs, _, err := New().Encode("project", doc)
	if err != nil || len(outputs) != 1 || outputs[0].Path != input.Path {
		t.Fatalf("encode: files=%+v err=%v", outputs, err)
	}
	roundtrip, _, err := New().Decode("project", outputs)
	if err != nil || !sameDoc(doc, roundtrip) {
		t.Fatalf("semantic roundtrip failed: doc=%+v roundtrip=%+v err=%v", doc, roundtrip, err)
	}
}

func TestPluginScopeWarnsAndRetainsIgnoredFields(t *testing.T) {
	input := nativeadapter.File{Path: "agents/reviewer.md", Format: "md", Data: []byte("---\nname: reviewer\ndescription: Reviews code\npermissionMode: bypassPermissions\nmcpServers: [tools]\nhooks:\n  PreToolUse: []\n---\nPrompt\n")}
	doc, diagnostics, err := New().Decode("plugin", []nativeadapter.File{input})
	if err != nil || len(diagnostics) != 3 {
		t.Fatalf("plugin decode: diagnostics=%+v err=%v", diagnostics, err)
	}
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity != "warning" || diagnostic.Code != "claude.plugin.field-ignored" {
			t.Fatalf("unexpected plugin diagnostic: %+v", diagnostic)
		}
	}
	if _, ok := doc.Unknown["frontmatter.permissionMode"]; !ok {
		t.Fatalf("ignored plugin field was lost: %+v", doc.Unknown)
	}
	d := New().Descriptor()
	for _, capability := range d.Fields {
		if capability.NeutralPath == "agent.permissionMode" && capability.Support != "unsupported" {
			t.Fatalf("plugin-restricted control must remain unsupported: %+v", capability)
		}
	}
	encoded, _, err := New().Encode("plugin", doc)
	if err != nil || len(encoded) != 1 {
		t.Fatalf("plugin encode silently lost supported inert data: %+v %v", encoded, err)
	}
	roundtrip, _, err := New().Decode("plugin", encoded)
	if err != nil || !sameDoc(doc, roundtrip) {
		t.Fatalf("plugin semantic roundtrip failed: %v", err)
	}
}

func TestRejectsUnsafeOrMalformedFrontmatterAndScope(t *testing.T) {
	base := ".claude/agents/worker.md"
	cases := []struct {
		name  string
		path  string
		data  string
		scope string
	}{
		{"duplicate", base, "---\nname: x\nname: y\ndescription: z\n---\nbody", "project"},
		{"yaml anchor", base, "---\nname: x\ndescription: z\nmore: &v thing\nother: *v\n---\nbody", "project"},
		{"explicit tag", base, "---\nname: x\ndescription: z\nextra: !custom value\n---\nbody", "project"},
		{"missing name", base, "---\ndescription: z\n---\nbody", "project"},
		{"invalid model type", base, "---\nname: x\ndescription: z\nmodel: [one]\n---\nbody", "project"},
		{"plugin path in project", "agents/x.md", "---\nname: x\ndescription: z\n---\nbody", "project"},
		{"wrong format", base, "---\nname: x\ndescription: z\n---\nbody", "project"},
		{"multiple files", base, "---\nname: x\ndescription: z\n---\nbody", "project"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			files := []nativeadapter.File{{Path: tc.path, Format: "markdown", Data: []byte(tc.data)}}
			if tc.name == "wrong format" {
				files[0].Format = "yaml"
			}
			if tc.name == "multiple files" {
				files = append(files, files[0])
			}
			if _, _, err := New().Decode(tc.scope, files); err == nil {
				t.Fatal("invalid Claude document was accepted")
			}
		})
	}
}

func TestEncodeRejectsUnsupportedOrMismatchedContent(t *testing.T) {
	doc, _, err := New().Decode("project", []nativeadapter.File{{Path: ".claude/agents/x.md", Format: "md", Data: []byte("---\nname: x\ndescription: y\n---\nbody")}})
	if err != nil {
		t.Fatal(err)
	}
	bad := cloneDoc(doc)
	bad.Fields["agent.instructions.sources"] = json.RawMessage(`[{"path":"other.md"}]`)
	if _, _, err := New().Encode("project", bad); err == nil {
		t.Fatal("unsupported instruction source was silently dropped")
	}
	bad = cloneDoc(doc)
	bad.Unknown["file.name"] = json.RawMessage(`"../../escape.md"`)
	if _, _, err := New().Encode("project", bad); err == nil {
		t.Fatal("path traversal filename was accepted")
	}
	bad = cloneDoc(doc)
	bad.Assets = nil
	if _, _, err := New().Encode("project", bad); err == nil {
		t.Fatal("missing prompt body was accepted")
	}
}

func sameDoc(a, b nativeadapter.Document) bool {
	if len(a.Fields) != len(b.Fields) || len(a.Unknown) != len(b.Unknown) || len(a.Assets) != len(b.Assets) {
		return false
	}
	for key, raw := range a.Fields {
		if !jsonEquivalent(raw, b.Fields[key]) {
			return false
		}
	}
	for key, raw := range a.Unknown {
		if !jsonEquivalent(raw, b.Unknown[key]) {
			return false
		}
	}
	for key, content := range a.Assets {
		if string(content) != string(b.Assets[key]) {
			return false
		}
	}
	return true
}

func jsonEquivalent(a, b []byte) bool {
	x, errX := contracts.DecodeJSON(a)
	y, errY := contracts.DecodeJSON(b)
	return errX == nil && errY == nil && reflect.DeepEqual(x, y)
}

func cloneDoc(doc nativeadapter.Document) nativeadapter.Document {
	out := nativeadapter.Document{Fields: map[string]json.RawMessage{}, Unknown: map[string]json.RawMessage{}, Assets: map[string][]byte{}}
	for k, v := range doc.Fields {
		out.Fields[k] = append([]byte(nil), v...)
	}
	for k, v := range doc.Unknown {
		out.Unknown[k] = append([]byte(nil), v...)
	}
	for k, v := range doc.Assets {
		out.Assets[k] = append([]byte(nil), v...)
	}
	return out
}

func TestEnginePreservesUnsupportedNativeControls(t *testing.T) {
	r := nativeadapter.NewRegistry()
	if err := r.Register(New()); err != nil {
		t.Fatal(err)
	}
	e := nativeadapter.NewEngine(r)
	files := []nativeadapter.File{{Path: ".claude/agents/probe.md", Format: "markdown", Data: []byte("---\nname: probe\ndescription: Probe\ntools: Read\npermissionMode: plan\n---\nPrompt\n")}}
	b, report := e.Import("claude-code", "project", files, nil, []nativeadapter.Control{{Path: "agent.capabilities.tools", Required: true}})
	if report.Status != "blocked" || b.Version == "" {
		t.Fatalf("unsupported control should be preserved and blocked: %+v", report)
	}
	out, _, report := e.Export("claude-code", "project", files, b, nil)
	if len(out) != 1 || report.Status != "blocked" {
		t.Fatalf("export: %+v", report)
	}
}
