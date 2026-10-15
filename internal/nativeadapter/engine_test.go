package nativeadapter

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/nativeparse"
)

type fakeAdapter struct{ desc Descriptor }

func (f *fakeAdapter) Descriptor() Descriptor { return f.desc }
func (f *fakeAdapter) Decode(scope string, files []File) (Document, []contracts.Diagnostic, error) {
	if scope != "project" || len(files) != 1 {
		return Document{}, nil, errFake
	}
	obj, err := nativeparse.Decode(files[0].Format, files[0].Data)
	if err != nil {
		return Document{}, []contracts.Diagnostic{{Code: "fixture.parse", Severity: "error", Message: "bad secret value: super-secret"}}, err
	}
	fields := map[string]json.RawMessage{}
	unknown := map[string]json.RawMessage{}
	for key, value := range obj {
		raw, e := json.Marshal(value)
		if e != nil {
			return Document{}, nil, e
		}
		switch key {
		case "name":
			fields["identity.purpose"] = raw
		case "model":
			fields["model.selector"] = raw
		default:
			nativePath := key
			if key != "api_key" && key != "password" && key != "accessToken" && key != "privateKey" && key != "secretRef" && key != "token-limit" {
				nativePath = "frontmatter." + key
			}
			unknown[nativePath] = raw
		}
	}
	return Document{Fields: fields, Unknown: unknown}, nil, nil
}
func (f *fakeAdapter) Encode(scope string, doc Document) ([]File, []contracts.Diagnostic, error) {
	obj := map[string]json.RawMessage{}
	if value, ok := doc.Fields["identity.purpose"]; ok {
		obj["name"] = value
	}
	if value, ok := doc.Fields["model.selector"]; ok {
		obj["model"] = value
	}
	for p, value := range doc.Unknown {
		key := strings.TrimPrefix(p, "frontmatter.")
		if strings.Contains(p, ".") && !strings.HasPrefix(p, "frontmatter.") {
			continue
		}
		if _, exists := obj[key]; exists {
			return nil, nil, errFake
		}
		obj[key] = value
	}
	data, err := json.Marshal(obj)
	if err != nil {
		return nil, nil, err
	}
	return []File{{Path: "agent.json", Format: "json", Data: data}}, nil, nil
}

var errFake = &fakeError{}

type fakeError struct{}

func (*fakeError) Error() string { return "fixture decode failed with secret super-secret" }

func testEngine(t *testing.T) *Engine {
	t.Helper()
	d := Descriptor{Target: "opencode", AdapterVersion: "test-1", Scopes: []string{"project"}, Fields: []FieldCapability{
		{NeutralPath: "identity.purpose", NativePath: "frontmatter.name", Support: "native", EnforcementOwner: "native"},
		{NeutralPath: "model.selector", NativePath: "frontmatter.model", Support: "native", EnforcementOwner: "native"},
		{NeutralPath: "permissions.network.mode", NativePath: "frontmatter.network", Support: "unsupported", EnforcementOwner: "none"},
	}, HostLoading: HostLoadingEvidence{Status: "not-run"}}
	r := NewRegistry()
	if err := r.Register(&fakeAdapter{desc: d}); err != nil {
		t.Fatal(err)
	}
	return NewEngine(r)
}
func testNative(data string, format string) []File {
	return []File{{Path: "agent.jsonc", Format: format, Data: []byte(data)}}
}
func assertDiag(t *testing.T, r Report, code string) {
	t.Helper()
	for _, d := range r.Diagnostics {
		if d.Code == code {
			return
		}
	}
	t.Fatalf("missing %s in %+v", code, r.Diagnostics)
}

func TestRegistryExplicitAndDescriptorsDoNotClaimRuntimeEvidence(t *testing.T) {
	engine := NewEngine(NewRegistry())
	r := engine.Inspect("opencode", "project", testNative(`{"name":"x"}`, "json"), nil)
	if r.Status != "blocked" {
		t.Fatalf("unregistered target status=%s", r.Status)
	}
	if _, ok := engine.registry.Lookup("opencode"); ok {
		t.Fatal("registry should be empty until explicit registration")
	}
	adapter, _ := testEngine(t).registry.Lookup("opencode")
	if adapter.Descriptor().HostLoading.Status != "not-run" {
		t.Fatal("fake descriptor unexpectedly claims host loading")
	}
	registeredReport := testEngine(t).Inspect("opencode", "project", testNative(`{"name":"x"}`, "json"), nil)
	if registeredReport.RuntimeEnforcement != "unverified" || registeredReport.HostLoading.Status != "not-run" {
		t.Fatalf("report implied runtime proof: %+v", registeredReport)
	}
}

func TestImportPreservesUnknownAndMarksIncompleteDraft(t *testing.T) {
	engine := testEngine(t)
	input := testNative(`{/* retained source comment */"name":"explorer","model":"m1","future":{"x":2},"count":1.0}`, "jsonc")
	bundle, report := engine.Import("opencode", "project", input, nil, nil)
	if report.Status != "blocked" || report.MappingStatus != "draft" || bundle.Completeness != "draft-incomplete" {
		t.Fatalf("import status/bundle=%s/%s %+v", report.Status, report.MappingStatus, bundle)
	}
	if string(bundle.OriginalFiles[0].Data) != string(input[0].Data) {
		t.Fatal("bundle did not retain original source bytes")
	}
	if string(bundle.Document.Fields["identity.purpose"]) != "\"explorer\"" || string(bundle.Document.Unknown["frontmatter.future"]) != "{\"x\":2}" {
		t.Fatalf("projection/unknown not retained: %+v", bundle.Document)
	}
	if strings.Contains(string(mustJSON(t, report)), "retained source comment") || strings.Contains(string(mustJSON(t, report)), "explorer") {
		t.Fatal("report leaked native bytes or values")
	}
	encoded, err := EncodeBundle(bundle)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeBundle(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Digest != bundle.Digest {
		t.Fatal("sidecar digest changed")
	}
}

func TestExportRoundTripsUnknownFieldsAndCanReimportRenderedFiles(t *testing.T) {
	engine := testEngine(t)
	input := testNative(`{"name":"explorer","model":"m1","future":{"x":2},"count":1.0}`, "json")
	b, ir := engine.Import("opencode", "project", input, nil, nil)
	if ir.Status != "blocked" {
		t.Fatalf("import should stay blocked as incomplete: %+v", ir)
	}
	outputs, next, report := engine.Export("opencode", "project", input, b, nil)
	if report.Status != "blocked" || report.MappingStatus != "draft" || len(outputs) != 1 {
		t.Fatalf("export result: files=%d report=%+v", len(outputs), report)
	}
	if len(next.RenderedFiles) != 1 || next.RenderedFiles[0].SHA256 == "" {
		t.Fatalf("rendered file lineage missing: %+v", next)
	}
	comparison := engine.Compare("opencode", "project", outputs, next, nil)
	if comparison.Status != "valid" || comparison.MappingStatus != "compatible" || len(comparison.Differences) != 0 {
		t.Fatalf("rendered reimport failed: %+v", comparison)
	}
	imported, again := engine.Import("opencode", "project", outputs, &next, nil)
	if again.Status != "blocked" || imported.Digest == "" || len(imported.OriginalFiles) != 1 || len(imported.RenderedFiles) != 1 {
		t.Fatalf("rendered sidecar reimport: %+v %+v", imported, again)
	}
	if imported.OriginalFiles[0].SHA256 != b.OriginalFiles[0].SHA256 {
		t.Fatal("original source lineage was discarded")
	}
}

func TestCompareUsesSemanticJSONNumbersAndReportsStableFieldDiffs(t *testing.T) {
	engine := testEngine(t)
	input := testNative(`{"name":"explorer","model":"m1","count":1.0}`, "json")
	b, _ := engine.Import("opencode", "project", input, nil, nil)
	changed := testNative(`{"name":"explorer","model":"m1","count":1e0}`, "json")
	r := engine.Compare("opencode", "project", changed, b, nil)
	if r.Status != "invalid" {
		t.Fatalf("changed bytes should conflict even when semantics equal, status=%s", r.Status)
	}
	assertDiag(t, r, "native.bundle.conflict")
	// Compare in a bundle captured from this exact source: equivalent number
	// spellings should not appear as semantic differences.
	b2, _ := engine.Import("opencode", "project", changed, nil, nil)
	r = engine.Compare("opencode", "project", changed, b2, nil)
	if r.MappingStatus != "compatible" || len(r.Differences) != 0 {
		t.Fatalf("equal numeric semantics differ: %+v", r)
	}
	changed[0].Data = []byte(`{"name":"other","model":"m1","count":2}`)
	r = engine.Compare("opencode", "project", changed, b2, nil)
	if r.Status != "invalid" {
		t.Fatalf("native drift should be invalid: %+v", r)
	}
}

func TestDriftTamperingAndTargetScopeMismatchRefuseExport(t *testing.T) {
	engine := testEngine(t)
	input := testNative(`{"name":"x","model":"m"}`, "json")
	b, _ := engine.Import("opencode", "project", input, nil, nil)
	changed := testNative(`{"name":"tampered","model":"m"}`, "json")
	if out, _, r := engine.Export("opencode", "project", changed, b, nil); len(out) != 0 || r.Status != "invalid" {
		t.Fatalf("source drift was not refused: %d %+v", len(out), r)
	}
	assertDiag(t, engine.Compare("opencode", "project", input, Bundle{Version: b.Version, Target: "claude", Scope: b.Scope, AdapterVersion: b.AdapterVersion, Completeness: b.Completeness, OriginalFiles: b.OriginalFiles, Document: b.Document, Digest: b.Digest}, nil), "native.bundle.integrity")
	tampered := cloneBundle(b)
	tampered.OriginalFiles[0].Data[0] ^= 1
	if out, _, r := engine.Export("opencode", "project", input, tampered, nil); len(out) != 0 || r.Status != "invalid" {
		t.Fatalf("tampered bundle was not refused: %d %+v", len(out), r)
	}
	serialized, _ := EncodeBundle(b)
	serialized = bytes.Replace(serialized, []byte(`"opencode"`), []byte(`"openc0de"`), 1)
	if _, err := DecodeBundle(serialized); err == nil {
		t.Fatal("tampered serialized sidecar decoded")
	}
	if out, _, r := engine.Export("opencode", "user", input, b, nil); len(out) != 0 || r.Status != "blocked" {
		t.Fatalf("scope drift was not blocked: %d %+v", len(out), r)
	}
}

func TestRequiredUnsupportedBlocksButCandidateCanBeRendered(t *testing.T) {
	engine := testEngine(t)
	input := testNative(`{"name":"x","model":"m"}`, "json")
	b, _ := engine.Import("opencode", "project", input, nil, nil)
	required := []Control{{Path: "permissions.network.mode", Required: true}}
	out, next, r := engine.Export("opencode", "project", input, b, required)
	if len(out) != 1 || next.Digest == "" || r.Status != "blocked" {
		t.Fatalf("expected blocked candidate: files=%d report=%+v", len(out), r)
	}
	assertDiag(t, r, "native.control.required-unsupported")
	optional := engine.Inspect("opencode", "project", input, []Control{{Path: "permissions.network.mode"}})
	if optional.Status != "valid" || optional.MappingStatus != "lossy" {
		t.Fatalf("optional unsupported should warn: %+v", optional)
	}
}

func TestExecutorBackedControlBlocksWithoutRuntimeExecutor(t *testing.T) {
	engine := testEngine(t)
	input := testNative(`{"name":"x","model":"m"}`, "json")
	adapter, _ := engine.registry.Lookup("opencode")
	d := adapter.(*fakeAdapter).desc
	d.Fields = append(d.Fields, FieldCapability{NeutralPath: "permissions.filesystem.mode", NativePath: "frontmatter.fs", Support: "executor-backed", EnforcementOwner: "executor"})
	reg := NewRegistry()
	if err := reg.Register(&fakeAdapter{desc: d}); err != nil {
		t.Fatal(err)
	}
	r := NewEngine(reg).Inspect("opencode", "project", input, []Control{{Path: "permissions.filesystem.mode", Required: true}})
	if r.Status != "blocked" || r.RuntimeEnforcement != "unverified" {
		t.Fatalf("executor control claimed support: %+v", r)
	}
	assertDiag(t, r, "native.control.required-unsupported")
}

func TestVerifiedHostLoadingEvidenceMustBeScopeBound(t *testing.T) {
	adapter := &fakeAdapter{desc: Descriptor{
		Target: "opencode", AdapterVersion: "test-1", Scopes: []string{"project"},
		Fields:      []FieldCapability{{NeutralPath: "identity.purpose", NativePath: "frontmatter.name", Support: "native", EnforcementOwner: "native"}},
		HostLoading: HostLoadingEvidence{Status: "verified", HostVersion: "1.2", Evidence: []string{"fixture"}},
	}}
	reg := NewRegistry()
	if err := reg.Register(adapter); err == nil {
		t.Fatal("accepted unscoped host loading evidence")
	}
	adapter.desc.HostLoading.Scopes = []string{"project"}
	if err := reg.Register(adapter); err != nil {
		t.Fatal(err)
	}
	r := NewEngine(reg).Inspect("opencode", "project", testNative(`{"name":"x"}`, "json"), nil)
	if r.Status != "valid" || r.HostLoading.Status != "verified" || r.RuntimeEnforcement != "unverified" {
		t.Fatalf("report lost distinction between loading and enforcement: %+v", r)
	}
}

func TestSensitiveUnknownFieldsBlockAndSecretReferencesRemainAllowed(t *testing.T) {
	engine := testEngine(t)
	secret := testNative(`{"name":"x","model":"m","api_key":"super-secret"}`, "json")
	b, r := engine.Import("opencode", "project", secret, nil, nil)
	if r.Status != "blocked" || b.Digest != "" {
		t.Fatalf("secret-bearing input should not create a bundle: %+v", r)
	}
	assertDiag(t, r, "native.credential-field")
	if strings.Contains(string(mustJSON(t, r)), "super-secret") {
		t.Fatal("secret value leaked to report")
	}
	allowed := testNative(`{"name":"x","model":"m","secretRef":"vault/item","token-limit":20}`, "json")
	if b, r = engine.Import("opencode", "project", allowed, nil, nil); b.Digest == "" || r.Status != "blocked" {
		t.Fatalf("secret reference/token limit was misclassified: %+v", r)
	}
	b.Document.Unknown["frontmatter.password"] = json.RawMessage(`"credential"`)
	b.Digest, _ = bundleDigest(b)
	if files, _, r := engine.Export("opencode", "project", allowed, b, nil); len(files) != 0 || r.Status != "blocked" {
		t.Fatalf("secret-bearing bundle exported: %d %+v", len(files), r)
	}
}

func TestUnknownMappedPathCollisionAndAdapterErrorsAreSafe(t *testing.T) {
	engine := testEngine(t)
	collision := testNative(`{"name":"x","model":"m","future":1}`, "json")
	adapter, _ := engine.registry.Lookup("opencode")
	fake := adapter.(*fakeAdapter)
	badDesc := fake.desc
	badDesc.Fields = append(badDesc.Fields, FieldCapability{NeutralPath: "future.value", NativePath: "frontmatter.future", Support: "native", EnforcementOwner: "native"})
	reg := NewRegistry()
	if err := reg.Register(&fakeAdapter{desc: badDesc}); err != nil {
		t.Fatal(err)
	}
	b, r := NewEngine(reg).Import("opencode", "project", collision, nil, nil)
	if b.Digest != "" || r.Status != "invalid" {
		t.Fatalf("mapped unknown collision accepted: %+v", r)
	}
	bad := testNative(`{"bad":`, "json")
	inspect := engine.Inspect("opencode", "project", bad, nil)
	if inspect.Status != "invalid" || strings.Contains(string(mustJSON(t, inspect)), "super-secret") || strings.Contains(string(mustJSON(t, inspect)), "bad\":") {
		t.Fatalf("adapter diagnostic leaked data: %+v", inspect)
	}
}

func TestEngineEnforcesFileSetBounds(t *testing.T) {
	engine := testEngine(t)
	tooMany := make([]File, maxBundleFiles+1)
	for i := range tooMany {
		tooMany[i] = File{Path: "f" + json.Number(strings.Repeat("1", i+1)).String() + ".json", Format: "json", Data: []byte(`{}`)}
	}
	if r := engine.Inspect("opencode", "project", tooMany, nil); r.Status != "invalid" {
		t.Fatalf("accepted too many files: %+v", r)
	}
	large := []File{{Path: "large.json", Format: "json", Data: bytes.Repeat([]byte{' '}, maxBundleBytes+1)}}
	if r := engine.Inspect("opencode", "project", large, nil); r.Status != "invalid" {
		t.Fatalf("accepted oversized file set: %+v", r)
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

func TestPortableFileCollisionsAndScopeSpecificEvidence(t *testing.T) {
	engine := testEngine(t)
	for _, paths := range [][]string{{"a", "A"}, {"a", "a/b"}, {"NUL.json"}, {"a:stream"}, {"../escape"}} {
		files := []File{}
		for _, p := range paths {
			files = append(files, File{Path: p, Format: "json", Data: []byte("{}")})
		}
		if r := engine.Inspect("opencode", "project", files, nil); r.Status != "invalid" {
			t.Fatalf("accepted %v: %+v", paths, r)
		}
	}
	a, _ := engine.registry.Lookup("opencode")
	fake := a.(*fakeAdapter)
	fake.desc.Scopes = []string{"project", "plugin"}
	fake.desc.HostLoading = HostLoadingEvidence{Status: "verified", HostVersion: "test-only", Evidence: []string{"synthetic"}, Scopes: []string{"project"}}
	r := engine.Inspect("opencode", "plugin", testNative(`{"name":"x","model":"m"}`, "json"), nil)
	if r.HostLoading.Status == "verified" {
		t.Fatal("project proof applied to plugin scope")
	}
}

func TestCommonCredentialKeysNeverProduceBundles(t *testing.T) {
	engine := testEngine(t)
	for _, key := range []string{"token", "clientSecret", "refresh_token", "Authorization", "api-secret", "secret_key", "credentials", "clientCredential"} {
		data, _ := json.Marshal(map[string]any{"name": "x", "model": "m", key: "fixture-sensitive-value"})
		b, r := engine.Import("opencode", "project", testNative(string(data), "json"), nil, nil)
		if b.Version != "" || r.Status != "blocked" {
			t.Fatalf("credential accepted: %s", key)
		}
		encoded, _ := json.Marshal(r)
		if bytes.Contains(encoded, []byte("fixture-sensitive-value")) {
			t.Fatal("credential echoed")
		}
	}
}
