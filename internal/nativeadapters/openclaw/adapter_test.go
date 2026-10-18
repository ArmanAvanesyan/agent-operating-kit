package openclaw

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/nativeadapter"
)

func TestNativeJSONCWorkspaceRoundTrip(t *testing.T) {
	adapter := New()
	doc, _, err := adapter.Decode(scope, []nativeadapter.File{
		{Path: configPath, Format: "jsonc", Data: []byte(`{
          // Native config permits JSONC, but not other JSON5 extensions.
          "agents": { "entries": { "researcher": {
            "identity": { "name": "Researcher", "theme": "preserve this" },
            "model": "openai/gpt-5.4-mini", "tools": {"deny": ["exec"]}
          } } }, "update": {"checkOnStart":false}
        }`)},
		{Path: workspacePath, Format: "markdown", Data: []byte("# Instructions\n\nStay within the supplied task.\n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(doc.Fields["agent.metadata.id"]); got != `"researcher"` {
		t.Fatalf("id=%s", got)
	}
	if got := string(doc.Fields["executionProfile.model.selector"]); got != `"openai/gpt-5.4-mini"` {
		t.Fatalf("model=%s", got)
	}
	if !bytes.Equal(doc.Assets[assetName], []byte("# Instructions\n\nStay within the supplied task.\n")) {
		t.Fatal("workspace instructions were not preserved")
	}
	if _, ok := doc.Unknown["identity.theme"]; !ok {
		t.Fatal("unmapped identity.theme was not preserved")
	}
	if _, ok := doc.Unknown["entry.tools"]; !ok {
		t.Fatal("unmapped tools config was not preserved")
	}
	if _, ok := doc.Unknown["config.update"]; !ok {
		t.Fatal("valid unmapped top-level config was not preserved")
	}
	files, _, err := adapter.Encode(scope, doc)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || files[0].Path != configPath || files[1].Path != workspacePath {
		t.Fatalf("unexpected outputs: %#v", files)
	}
	var got map[string]any
	if err := json.Unmarshal(files[0].Data, &got); err != nil {
		t.Fatal(err)
	}
	agents := got["agents"].(map[string]any)
	entries := agents["entries"].(map[string]any)
	entry := entries["researcher"].(map[string]any)
	if entry["workspace"] != workspace {
		t.Fatalf("workspace=%v", entry["workspace"])
	}
	if entry["model"] != "openai/gpt-5.4-mini" {
		t.Fatalf("model=%v", entry["model"])
	}
	if got["update"] == nil || entry["tools"] == nil || entry["identity"].(map[string]any)["theme"] != "preserve this" {
		t.Fatalf("unknown fields relocated: %#v", got)
	}
	round, _, err := adapter.Decode(scope, files)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(doc)
	after, _ := json.Marshal(round)
	if !bytes.Equal(before, after) {
		t.Fatal("unknown semantics changed")
	}

	if string(files[1].Data) != "# Instructions\n\nStay within the supplied task.\n" {
		t.Fatal("instruction bytes changed")
	}
}

func TestEngineRoundTripKeepsVerifiedHostAndDraftBlocked(t *testing.T) {
	descriptor := New().Descriptor()
	if descriptor.HostLoading.HostVersion != "2026.9.4 (Linux; Node 26.7.0)" || len(descriptor.HostLoading.Scopes) != 1 || descriptor.HostLoading.Scopes[0] != scope || len(descriptor.HostLoading.Evidence) == 0 {
		t.Fatalf("native loading evidence must stay version/scope bound: %+v", descriptor.HostLoading)
	}
	registry := nativeadapter.NewRegistry()
	if err := registry.Register(New()); err != nil {
		t.Fatal(err)
	}
	engine := nativeadapter.NewEngine(registry)
	files := []nativeadapter.File{
		{Path: configPath, Format: "json", Data: []byte(`{"agents":{"entries":{"main":{"identity":{"name":"Main"}}}}}`)},
		{Path: workspacePath, Format: "markdown", Data: []byte("Use the project context.\n")},
	}
	bundle, imported := engine.Import(target, scope, files, nil, nil)
	if imported.Status != "blocked" || imported.HostLoading.Status != "verified" || imported.RuntimeEnforcement != "unverified" {
		t.Fatalf("host loading or draft readiness was misreported: %+v", imported)
	}
	rendered, _, exported := engine.Export(target, scope, files, bundle, nil)
	if exported.Status != "blocked" || exported.HostLoading.Status != "verified" || exported.RuntimeEnforcement != "unverified" {
		t.Fatalf("host loading or draft readiness was misreported: %+v", exported)
	}
	if len(rendered) != 2 {
		t.Fatalf("rendered files=%d", len(rendered))
	}
	inspected := engine.Inspect(target, scope, rendered, []nativeadapter.Control{{Path: "agent.permissions.filesystem.write", Required: true}})
	if inspected.HostLoading.Status != "verified" {
		t.Fatalf("host loading=%q", inspected.HostLoading.Status)
	}
	if inspected.Status != "blocked" {
		t.Fatalf("required unsupported control should remain blocked, got %s", inspected.Status)
	}
}

func TestRejectUnsupportedOrUnsafeNativeInput(t *testing.T) {
	tests := []struct {
		name  string
		files []nativeadapter.File
	}{
		{"arbitrary path", []nativeadapter.File{{Path: "/tmp/outside.json", Format: "json", Data: []byte(`{}`)}, {Path: workspacePath, Format: "markdown", Data: []byte("x")}}},
		{"traversal", []nativeadapter.File{{Path: ".openclaw/../openclaw.json", Format: "json", Data: []byte(`{}`)}, {Path: workspacePath, Format: "markdown", Data: []byte("x")}}},
		{"JSON5 unquoted key", []nativeadapter.File{{Path: configPath, Format: "jsonc", Data: []byte(`{agents:{entries:{main:{}}}}`)}, {Path: workspacePath, Format: "markdown", Data: []byte("x")}}},
		{"multiple entries", []nativeadapter.File{{Path: configPath, Format: "json", Data: []byte(`{"agents":{"entries":{"one":{},"two":{}}}}`)}, {Path: workspacePath, Format: "markdown", Data: []byte("x")}}},
		{"custom workspace", []nativeadapter.File{{Path: configPath, Format: "json", Data: []byte(`{"agents":{"entries":{"main":{"workspace":"/tmp/arbitrary"}}}}`)}, {Path: workspacePath, Format: "markdown", Data: []byte("x")}}},
		{"custom default workspace", []nativeadapter.File{{Path: configPath, Format: "json", Data: []byte(`{"agents":{"defaults":{"workspace":"/tmp/arbitrary"},"entries":{"main":{}}}}`)}, {Path: workspacePath, Format: "markdown", Data: []byte("x")}}},
		{"unsupported model form", []nativeadapter.File{{Path: configPath, Format: "json", Data: []byte(`{"agents":{"entries":{"main":{"model":{"primary":"x"}}}}}`)}, {Path: workspacePath, Format: "markdown", Data: []byte("x")}}},
		{"invalid identity type", []nativeadapter.File{{Path: configPath, Format: "json", Data: []byte(`{"agents":{"entries":{"main":{"identity":"name"}}}}`)}, {Path: workspacePath, Format: "markdown", Data: []byte("x")}}},
		{"duplicate key", []nativeadapter.File{{Path: configPath, Format: "json", Data: []byte(`{"agents":{"entries":{"main":{},"main":{}}}}`)}, {Path: workspacePath, Format: "markdown", Data: []byte("x")}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := New().Decode(scope, tc.files); err == nil {
				t.Fatal("invalid input accepted")
			}
		})
	}
}

func TestRejectMissingInstructionsAsset(t *testing.T) {
	doc := nativeadapter.Document{Fields: map[string]json.RawMessage{"agent.metadata.id": json.RawMessage(`"main"`), "agent.instructions.sources": json.RawMessage(`[ {"path":"instructions.md"} ]`)}}
	if _, _, err := New().Encode(scope, doc); err == nil {
		t.Fatal("missing instruction asset accepted")
	}
}

func TestDottedUnknownKeysStayLiteral(t *testing.T) {
	registry := nativeadapter.NewRegistry()
	if err := registry.Register(New()); err != nil {
		t.Fatal(err)
	}
	engine := nativeadapter.NewEngine(registry)
	files := []nativeadapter.File{{Path: configPath, Format: "json", Data: []byte(`{"agents":{"entries":{"probe":{"identity":{"custom.key":true},"tools.extra":1}}},"foo":1,"foo.bar":2}`)}, {Path: workspacePath, Format: "markdown", Data: []byte("Prompt")}}
	b, r := engine.Import(target, scope, files, nil, nil)
	if b.Version == "" {
		t.Fatalf("import: %+v", r)
	}
	out, exported, r := engine.Export(target, scope, files, b, nil)
	if len(out) != 2 {
		t.Fatalf("export: %+v", r)
	}
	if r = engine.Compare(target, scope, out, exported, nil); r.Status != "valid" {
		t.Fatalf("roundtrip: %+v", r)
	}
	var config map[string]any
	_ = json.Unmarshal(out[0].Data, &config)
	if config["foo"] != float64(1) || config["foo.bar"] != float64(2) {
		t.Fatalf("literal key lost: %+v", config)
	}
}

func TestRejectsNonTextWorkspace(t *testing.T) {
	files := []nativeadapter.File{{Path: configPath, Format: "json", Data: []byte(`{"agents":{"entries":{"probe":{}}}}`)}, {Path: workspacePath, Format: "markdown", Data: []byte{255}}}
	if _, _, err := New().Decode(scope, files); err == nil {
		t.Fatal("invalid UTF-8 accepted")
	}
}
