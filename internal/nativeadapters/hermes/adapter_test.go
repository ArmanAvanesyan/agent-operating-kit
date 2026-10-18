package hermes

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/nativeadapter"
)

func TestProfileInventoryMapsOnlyVerifiedFieldsAndRoundTrips(t *testing.T) {
	files := fixtureInventory(t)
	doc, diagnostics, err := New().Decode("profile", files)
	if err != nil || len(diagnostics) != 0 {
		t.Fatalf("decode: diagnostics=%+v err=%v", diagnostics, err)
	}
	if _, hasID := doc.Fields["agent.metadata.id"]; hasID {
		t.Fatal("adapter invented an ID not present in profile-native files")
	}
	if string(doc.Fields["agent.identity.purpose"]) != `"Focused software engineering assistant."` || string(doc.Fields["executionProfile.model.selector"]) != `"anthropic/claude-sonnet-4"` {
		t.Fatalf("mapped values: %+v", doc.Fields)
	}
	if string(doc.Fields["agent.instructions.sources"]) != `[{"path":"instructions.md"}]` || string(doc.Assets["instructions.md"]) == "" {
		t.Fatalf("persona was not retained: fields=%+v assets=%+v", doc.Fields, doc.Assets)
	}
	for key := range map[string]bool{"config.root": true, "config.modelRemainder": true, "profile.remainder": true} {
		if _, ok := doc.Unknown[key]; !ok {
			t.Fatalf("unknown native state %q was lost: %+v", key, doc.Unknown)
		}
	}
	outputs, _, err := New().Encode("profile", doc)
	if err != nil || len(outputs) != 3 {
		t.Fatalf("encode: output count=%d err=%v", len(outputs), err)
	}
	for _, file := range outputs {
		if file.Path == ".env" || file.Path == "auth.json" || file.Path == "state.db" {
			t.Fatalf("export included forbidden dependency %q", file.Path)
		}
	}
	roundtrip, _, err := New().Decode("profile", outputs)
	if err != nil || !sameDocument(doc, roundtrip) {
		t.Fatalf("semantic roundtrip failed: err=%v\noriginal=%+v\nroundtrip=%+v", err, doc, roundtrip)
	}
}

func TestEngineRenderAcceptsCanonicalInstructionAssetAndRoundTrips(t *testing.T) {
	registry := nativeadapter.NewRegistry()
	if err := registry.Register(New()); err != nil {
		t.Fatal(err)
	}
	doc := nativeadapter.Document{
		Fields: map[string]json.RawMessage{
			"agent.identity.purpose":          json.RawMessage(`"A concise profile description."`),
			"executionProfile.model.selector": json.RawMessage(`"provider/model"`),
			"agent.instructions.sources":      json.RawMessage(`[{"path":"instructions.md"}]`),
		},
		Assets: map[string][]byte{"instructions.md": []byte("Follow the project instructions.\n")},
	}
	files, bundle, report := nativeadapter.NewEngine(registry).Render("hermes-agent", "profile", doc, nil, nil)
	if len(files) != 3 || report.Status != "blocked" || bundle.Digest == "" {
		t.Fatalf("render did not return blocked candidate output: files=%+v report=%+v bundle=%+v", files, report, bundle)
	}
	foundSoul := false
	for _, file := range files {
		if file.Path == "SOUL.md" && string(file.Data) == string(doc.Assets["instructions.md"]) {
			foundSoul = true
		}
	}
	if !foundSoul {
		t.Fatalf("canonical asset was not emitted as native SOUL.md: %+v", files)
	}
	if !sameDocument(doc, bundle.Document) {
		t.Fatalf("render changed canonical-like projection: doc=%+v bundle=%+v report=%+v", doc, bundle.Document, report)
	}
}

func TestPresenceMarkersOnlyPreserveOtherwiseEmptyNativeContainers(t *testing.T) {
	soul, err := os.ReadFile("testdata/SOUL.md")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, config, profile              string
		wantModelMarker, wantProfileMarker bool
	}{
		{"mapped model and description", "model:\n  default: m\n", "description: d\n", false, false},
		{"empty containers", "model: {}\n", "{}\n", true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc, _, err := New().Decode("profile", []nativeadapter.File{
				{Path: "config.yaml", Format: "yaml", Data: []byte(tc.config)},
				{Path: "SOUL.md", Format: "markdown", Data: soul},
				{Path: "profile.yaml", Format: "yaml", Data: []byte(tc.profile)},
			})
			if err != nil {
				t.Fatal(err)
			}
			_, gotModel := doc.Unknown["config.modelPresent"]
			_, gotProfile := doc.Unknown["profile.present"]
			if gotModel != tc.wantModelMarker || gotProfile != tc.wantProfileMarker {
				t.Fatalf("presence markers model=%v profile=%v; want %v %v; unknown=%+v", gotModel, gotProfile, tc.wantModelMarker, tc.wantProfileMarker, doc.Unknown)
			}
		})
	}
}

func TestEngineImportExportCompareStaysHostUnavailableAndBlocksRequiredControls(t *testing.T) {
	registry := nativeadapter.NewRegistry()
	if err := registry.Register(New()); err != nil {
		t.Fatal(err)
	}
	engine := nativeadapter.NewEngine(registry)
	files := fixtureInventory(t)
	required := []nativeadapter.Control{{Path: "agent.permissions.network.mode", Required: true}}
	bundle, imported := engine.Import("hermes-agent", "profile", files, nil, required)
	if imported.Status != "blocked" || bundle.Digest == "" || imported.HostLoading.Status != "unavailable" || imported.RuntimeEnforcement != "unverified" {
		t.Fatalf("import overstated readiness: report=%+v bundle=%+v", imported, bundle)
	}
	assertDiagnostic(t, imported, "native.control.required-unsupported")
	outputs, rendered, exported := engine.Export("hermes-agent", "profile", files, bundle, required)
	if exported.Status != "blocked" || len(outputs) != 3 || rendered.Digest == "" || exported.HostLoading.Status != "unavailable" || exported.RuntimeEnforcement != "unverified" {
		t.Fatalf("export overstated readiness: files=%d report=%+v", len(outputs), exported)
	}
	comparison := engine.Compare("hermes-agent", "profile", outputs, rendered, nil)
	if comparison.Status != "valid" || comparison.MappingStatus != "compatible" || len(comparison.Differences) != 0 || comparison.HostLoading.Status != "unavailable" || comparison.RuntimeEnforcement != "unverified" {
		t.Fatalf("comparison implied loading/runtime proof or semantic drift: %+v", comparison)
	}
}

func TestInventoryRequiresCoreAndRejectsSecretsAndRuntimeOrArchiveFiles(t *testing.T) {
	config, _ := os.ReadFile("testdata/config.yaml")
	soul, _ := os.ReadFile("testdata/SOUL.md")
	cases := []struct {
		name  string
		files []nativeadapter.File
	}{
		{"missing soul", []nativeadapter.File{{Path: "config.yaml", Format: "yaml", Data: config}}},
		{"environment file", []nativeadapter.File{{Path: "config.yaml", Format: "yaml", Data: config}, {Path: "SOUL.md", Format: "markdown", Data: soul}, {Path: ".env", Format: "text", Data: []byte("KEY=value")}}},
		{"auth store", []nativeadapter.File{{Path: "config.yaml", Format: "yaml", Data: config}, {Path: "SOUL.md", Format: "markdown", Data: soul}, {Path: "auth.json", Format: "json", Data: []byte(`{"providers":{}}`)}}},
		{"runtime database", []nativeadapter.File{{Path: "config.yaml", Format: "yaml", Data: config}, {Path: "SOUL.md", Format: "markdown", Data: soul}, {Path: "state.db", Format: "sqlite", Data: []byte("runtime")}}},
		{"runtime transcript", []nativeadapter.File{{Path: "config.yaml", Format: "yaml", Data: config}, {Path: "SOUL.md", Format: "markdown", Data: soul}, {Path: "sessions/active.jsonl", Format: "jsonl", Data: []byte("{}")}}},
		{"archive", []nativeadapter.File{{Path: "config.yaml", Format: "yaml", Data: config}, {Path: "SOUL.md", Format: "markdown", Data: soul}, {Path: "profile.tar.gz", Format: "tar.gz", Data: []byte("archive")}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := New().Decode("profile", tc.files); err == nil {
				t.Fatal("unsafe or incomplete profile inventory was accepted")
			}
		})
	}
	secretConfig := []byte("model:\n  default: local-model\nprovider:\n  api_key: do-not-import\n")
	if _, _, err := New().Decode("profile", []nativeadapter.File{{Path: "config.yaml", Format: "yaml", Data: secretConfig}, {Path: "SOUL.md", Format: "markdown", Data: soul}}); err == nil {
		t.Fatal("credential-bearing config was accepted")
	}
	secretSoul := append(append([]byte(nil), soul...), []byte("\nAPI_KEY: do-not-import\n")...)
	if _, _, err := New().Decode("profile", []nativeadapter.File{{Path: "config.yaml", Format: "yaml", Data: config}, {Path: "SOUL.md", Format: "markdown", Data: secretSoul}}); err == nil {
		t.Fatal("credential assignment in persona was accepted")
	}
}

func TestStrictYamlAndUnsupportedPathsRejectWithoutLeakingValues(t *testing.T) {
	soul, _ := os.ReadFile("testdata/SOUL.md")
	for name, badConfig := range map[string]string{
		"duplicate": "model:\n  default: m\n  default: n\n",
		"tag":       "model: !custom secret\n",
		"anchor":    "model: &a {default: m}\ncopy: *a\n",
		"date":      "run_at: 2025-01-01\n",
	} {
		t.Run(name, func(t *testing.T) {
			files := []nativeadapter.File{{Path: "config.yaml", Format: "yaml", Data: []byte(badConfig)}, {Path: "SOUL.md", Format: "markdown", Data: soul}}
			if _, _, err := New().Decode("profile", files); err == nil {
				t.Fatal("unsupported YAML was accepted")
			}
		})
	}
	if _, _, err := New().Decode("home", fixtureInventory(t)); err == nil {
		t.Fatal("unsupported scope was accepted")
	}
}

func TestLegacyScalarModelAndProfileWithoutOptionalMetadata(t *testing.T) {
	soul, _ := os.ReadFile("testdata/SOUL.md")
	files := []nativeadapter.File{{Path: "config.yaml", Format: "yaml", Data: []byte("model: legacy-model\n")}, {Path: "SOUL.md", Format: "md", Data: soul}}
	doc, _, err := New().Decode("profile", files)
	if err != nil || string(doc.Fields["executionProfile.model.selector"]) != `"legacy-model"` {
		t.Fatalf("legacy model form: %+v %v", doc, err)
	}
	outputs, _, err := New().Encode("profile", doc)
	if err != nil || len(outputs) != 2 {
		t.Fatalf("optional profile metadata changed inventory: %d %v", len(outputs), err)
	}
	roundtrip, _, err := New().Decode("profile", outputs)
	if err != nil || !sameDocument(doc, roundtrip) {
		t.Fatalf("legacy roundtrip: %v", err)
	}
}

func fixtureInventory(t *testing.T) []nativeadapter.File {
	t.Helper()
	config, err := os.ReadFile("testdata/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	soul, err := os.ReadFile("testdata/SOUL.md")
	if err != nil {
		t.Fatal(err)
	}
	profile, err := os.ReadFile("testdata/profile.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return []nativeadapter.File{{Path: "config.yaml", Format: "yaml", Data: config}, {Path: "SOUL.md", Format: "markdown", Data: soul}, {Path: "profile.yaml", Format: "yaml", Data: profile}}
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

func TestCredentialAssignmentsRejectedWithoutEcho(t *testing.T) {
	registry := nativeadapter.NewRegistry()
	if err := registry.Register(New()); err != nil {
		t.Fatal(err)
	}
	for _, f := range []nativeadapter.File{{Path: "config.yaml", Format: "yaml", Data: []byte("model:\n  default: provider/model\n  api: private-value-fixture\n")}, {Path: "config.yaml", Format: "yaml", Data: []byte("model: test\nextra: \"OPENAI_API_KEY=private-value-fixture\"\n")}, {Path: "SOUL.md", Format: "markdown", Data: []byte("- OPENAI_API_KEY=private-value-fixture\n")}, {Path: "config.yaml", Format: "yaml", Data: []byte("model: test\naws_secret_access_key: private-value-fixture\n")}, {Path: "SOUL.md", Format: "markdown", Data: []byte("export OPENAI_API_KEY=private-value-fixture\n")}} {
		files := []nativeadapter.File{{Path: "config.yaml", Format: "yaml", Data: []byte("model: test\n")}, {Path: "SOUL.md", Format: "markdown", Data: []byte("Prompt")}}
		for i := range files {
			if files[i].Path == f.Path {
				files[i] = f
			}
		}
		if _, _, err := New().Decode("profile", files); err == nil {
			t.Fatal("credential accepted by adapter")
		}
		b, r := nativeadapter.NewEngine(registry).Import("hermes-agent", "profile", files, nil, nil)
		if b.Version != "" {
			t.Fatal("credential bundled")
		}
		data, _ := json.Marshal(r)
		if strings.Contains(string(data), "private-value-fixture") {
			t.Fatal("credential echoed")
		}
	}
}

func TestProviderAPIURLPreserved(t *testing.T) {
	files := []nativeadapter.File{{Path: "config.yaml", Format: "yaml", Data: []byte("model: test\nproviders:\n  gateway:\n    api: https://llm.internal.example.com/v1\n")}, {Path: "SOUL.md", Format: "markdown", Data: []byte("Prompt")}}
	doc, _, err := New().Decode("profile", files)
	if err != nil {
		t.Fatal(err)
	}
	out, _, err := New().Encode("profile", doc)
	if err != nil {
		t.Fatal(err)
	}
	got, _, err := New().Decode("profile", out)
	if err != nil || !sameDocument(doc, got) {
		t.Fatalf("provider URL roundtrip: %v", err)
	}
}
