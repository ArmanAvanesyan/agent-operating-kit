package modelcatalog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestResearchImportsAreDeterministicAndKeepSourceBoundaries(t *testing.T) {
	manifest := filepath.Join("..", "..", "docs", "research", "model-catalog-manifest.json")
	policy := filepath.Join("..", "..", "docs", "research", "model-catalog-policy.json")
	a, ar := Import(manifest, policy)
	b, br := Import(manifest, policy)
	if ar.Status != "valid" || br.Status != "valid" {
		t.Fatalf("import status: %s / %s: %#v", ar.Status, br.Status, ar.Diagnostics)
	}
	if a.CatalogSHA256 != b.CatalogSHA256 {
		t.Fatal("catalog digest changed between identical imports")
	}
	for _, name := range []string{"catalog.json", "manifest.json", "policy.json", "import-report.json", "sources/codex/artifact.json", "sources/claude/artifact.json", "sources/opencode/artifact.json", "sources/agy/artifact.json"} {
		if _, ok := a.Files[name]; !ok {
			t.Errorf("capture missing %s", name)
		}
	}
	entries := map[string]Entry{}
	for _, e := range a.Catalog.Entries {
		entries[e.ID] = e
	}
	if entries["codex:sol61"].Selector != nil {
		t.Fatal("Codex symbolic key was promoted to selector")
	}
	for _, e := range a.Catalog.Entries {
		if e.SourceID == "opencode" && e.Selector != nil {
			t.Fatal("OpenCode research selector was promoted without explicit mapping")
		}
	}
	for _, x := range a.Catalog.Assertions {
		if x.SourceID == "opencode" && (x.Dimension == "effort" || x.Dimension == "reasoning.effort") {
			t.Fatalf("OpenCode listing inferred reasoning support: %#v", x)
		}
		if x.SourceID == "agy" && (x.Dimension == "thinkingMode" || x.Dimension == "reasoning.mode" || x.Dimension == "reasoning.effort") {
			t.Fatalf("agy listing implied reasoning support: %#v", x)
		}
	}
	if len(a.Catalog.Guidance) == 0 {
		t.Fatal("repository guidance was not preserved")
	}
	benchmarkRef := false
	for _, g := range a.Catalog.Guidance {
		if g.SourceID == "codex" && g.Pointer == "/benchmarkSource" && g.Category == Published {
			benchmarkRef = true
		}
	}
	if !benchmarkRef {
		t.Fatal("Codex benchmark reference metadata was not retained as published guidance")
	}
	var replayManifest Manifest
	if err := Decode(a.Files["manifest.json"], &replayManifest); err != nil {
		t.Fatal(err)
	}
	projection, err := BuildCatalog(replayManifest, a.Policy, a.Files)
	if err != nil {
		t.Fatal(err)
	}
	projBytes, _ := json.Marshal(projection)
	catBytes, _ := json.Marshal(a.Catalog)
	if string(projBytes) != string(catBytes) {
		t.Fatalf("captured bundle did not replay to the imported catalog: replay=%s catalog=%s", projBytes, catBytes)
	}
	bundle := t.TempDir()
	writeCapturedFiles(t, bundle, a.Files)
	loaded, lr := Load(bundle, "")
	if loaded == nil || lr.Status != "valid" {
		t.Fatalf("successful import did not load: %#v", lr.Diagnostics)
	}
}

func TestImportRejectsDriftedPinnedArtifact(t *testing.T) {
	dir := t.TempDir()
	b, err := os.ReadFile(filepath.Join("..", "..", "docs", "research", "model-catalog-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m Manifest
	if err = Decode(b, &m); err != nil {
		t.Fatal(err)
	}
	// Keep only the first source and give it a portable local artifact path.
	m.Sources = m.Sources[:1]
	m.Sources[0].Artifact.Path = "artifact.json"
	m.Sources[0].Evidence = nil
	encoded, _ := json.Marshal(m)
	if err = os.WriteFile(filepath.Join(dir, "manifest.json"), encoded, 0600); err != nil {
		t.Fatal(err)
	}
	policyData, _ := os.ReadFile(filepath.Join("..", "..", "docs", "research", "model-catalog-policy.json"))
	var p Policy
	if err = Decode(policyData, &p); err != nil {
		t.Fatal(err)
	}
	p.Sources = p.Sources[:1]
	pe, _ := json.Marshal(p)
	if err = os.WriteFile(filepath.Join(dir, "policy.json"), pe, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "artifact.json"), []byte(`{"models":{}}`), 0600); err != nil {
		t.Fatal(err)
	}
	_, r := Import(filepath.Join(dir, "manifest.json"), filepath.Join(dir, "policy.json"))
	if r.Status != "invalid" {
		t.Fatalf("expected pinned drift error, got %s", r.Status)
	}
}

func TestImportWarnsOnUnknownSourceFieldsAndRequiresExplicitMapping(t *testing.T) {
	dir := t.TempDir()
	artifact := []byte(`{"extractedOn":"2026-10-15","models":{"sym":"Friendly name"},"surprise":{"x":1}}`)
	if err := os.WriteFile(filepath.Join(dir, "raw.json"), artifact, 0600); err != nil {
		t.Fatal(err)
	}
	h := Hash(artifact)
	m := Manifest{Version: ManifestVersion, Sources: []SourceInput{{ID: "codex", Format: "codex", Artifact: Asset{Path: "raw.json", SHA256: h}, Evidence: []Asset{}}}, Mappings: []Mapping{}}
	p := Policy{Version: PolicyVersion, Sources: []SourceRule{{ID: "codex", ArtifactSHA256: "0000000000000000000000000000000000000000000000000000000000000000", Categories: []string{Listed}}}, ValidityHours: DefaultValidityHours()}
	mb, _ := json.Marshal(m)
	pb, _ := json.Marshal(p)
	mp := filepath.Join(dir, "manifest.json")
	pp := filepath.Join(dir, "policy.json")
	_ = os.WriteFile(mp, mb, 0600)
	_ = os.WriteFile(pp, pb, 0600)
	c, r := Import(mp, pp)
	if r.Status != "valid" {
		t.Fatalf("import failed: %#v", r.Diagnostics)
	}
	if len(r.Diagnostics) == 0 || r.Diagnostics[0].Severity != "warning" {
		t.Fatal("unknown source fields did not produce a warning")
	}
	if _, ok := c.Files["sources/codex/artifact.json"]; !ok {
		t.Fatal("policy mismatch discarded source evidence")
	}
	if c.Catalog.Entries[0].Selector != nil {
		t.Fatal("source model id was promoted without a manifest mapping")
	}
	if string(c.Catalog.Entries[0].Name) != "Friendly name" {
		t.Fatal("source model label was not retained")
	}
	bundle := t.TempDir()
	writeCapturedFiles(t, bundle, c.Files)
	loaded, lr := Load(bundle, "")
	if loaded == nil || lr.Status != "valid" || len(lr.Diagnostics) == 0 {
		t.Fatalf("untrusted import should load as inspectable warning evidence: capture=%v status=%s", loaded != nil, lr.Status)
	}
}

func TestImportRejectsInvalidCaptureAndRemovalDates(t *testing.T) {
	tests := []struct {
		name, format string
		raw          []byte
	}{{"capture", "codex", []byte(`{"extractedOn":"2026-not-a-date","models":{"x":"X"}}`)}, {"missing-capture", "codex", []byte(`{"models":{"x":"X"}}`)}, {"removal", "agy", []byte(`{"checkedOn":"2026-10-15","models":[{"label":"X","selector":"x","scheduledRemoval":"whenever"}]}`)}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			h := Hash(tt.raw)
			_ = os.WriteFile(filepath.Join(dir, "source.json"), tt.raw, 0600)
			m := Manifest{Version: ManifestVersion, Sources: []SourceInput{{ID: tt.name, Format: tt.format, Artifact: Asset{Path: "source.json", SHA256: h}, Evidence: []Asset{}}}, Mappings: []Mapping{}}
			cats := requiredCategories(tt.format)
			p := Policy{Version: PolicyVersion, Sources: []SourceRule{{ID: tt.name, ArtifactSHA256: h, Categories: cats}}, ValidityHours: DefaultValidityHours()}
			mb, _ := json.Marshal(m)
			pb, _ := json.Marshal(p)
			_ = os.WriteFile(filepath.Join(dir, "manifest.json"), mb, 0600)
			_ = os.WriteFile(filepath.Join(dir, "policy.json"), pb, 0600)
			_, r := Import(filepath.Join(dir, "manifest.json"), filepath.Join(dir, "policy.json"))
			if r.Status != "invalid" {
				t.Fatalf("malformed date accepted: %s", r.Status)
			}
		})
	}
}

func writeCapturedFiles(t *testing.T, root string, files map[string][]byte) {
	t.Helper()
	for name, b := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, b, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestExpiryUsesCategoryPricingAndOverrides(t *testing.T) {
	date := "2026-10-15T00:00:00Z"
	defaults := DefaultValidityHours()
	cases := []struct{ category, dimension, want string }{{Listed, "listed", "2026-10-22T00:00:00Z"}, {Published, "contextTokens", "2026-11-14T00:00:00Z"}, {Published, "price.inputPerMillionTokens", "2026-10-16T00:00:00Z"}, {Published, "offer", "2026-10-16T00:00:00Z"}, {Derived, "", "2027-01-13T00:00:00Z"}}
	for _, tc := range cases {
		if got := expiryFor(tc.category, tc.dimension, date, defaults); got != tc.want {
			t.Errorf("%s/%s expiry=%s want %s", tc.category, tc.dimension, got, tc.want)
		}
	}
	overrides := map[string]int{Listed: 12, "pricing": 9, Derived: 48}
	if got := expiryFor(Listed, "listed", date, overrides); got != "2026-10-15T12:00:00Z" {
		t.Fatalf("listed override=%s", got)
	}
	if got := expiryFor(Published, "offer", date, overrides); got != "2026-10-15T09:00:00Z" {
		t.Fatalf("pricing override=%s", got)
	}
	if got := expiryFor(Derived, "", date, overrides); got != "2026-10-17T00:00:00Z" {
		t.Fatalf("derived override=%s", got)
	}
}

func TestReimportChangesExpiryButPreservesSourceObservedAt(t *testing.T) {
	dir := t.TempDir()
	raw := []byte(`{"checkedOn":"2026-10-15","models":[{"label":"Model","selector":"p/model","offer":"Free"}]}`)
	h := Hash(raw)
	_ = os.WriteFile(filepath.Join(dir, "source.json"), raw, 0600)
	m := Manifest{Version: ManifestVersion, Sources: []SourceInput{{ID: "opencode", Format: "opencode", Artifact: Asset{Path: "source.json", SHA256: h}, Evidence: []Asset{}}}, Mappings: []Mapping{}}
	p := Policy{Version: PolicyVersion, Sources: []SourceRule{{ID: "opencode", ArtifactSHA256: h, Categories: []string{Published, Listed, Derived}}}, ValidityHours: DefaultValidityHours()}
	mb, _ := json.Marshal(m)
	_ = os.WriteFile(filepath.Join(dir, "manifest.json"), mb, 0600)
	p.ValidityHours[Listed] = 1
	pb, _ := json.Marshal(p)
	_ = os.WriteFile(filepath.Join(dir, "policy.json"), pb, 0600)
	first, fr := Import(filepath.Join(dir, "manifest.json"), filepath.Join(dir, "policy.json"))
	if fr.Status != "valid" {
		t.Fatal(fr.Diagnostics)
	}
	p.ValidityHours[Listed] = 2
	pb, _ = json.Marshal(p)
	_ = os.WriteFile(filepath.Join(dir, "policy.json"), pb, 0600)
	second, sr := Import(filepath.Join(dir, "manifest.json"), filepath.Join(dir, "policy.json"))
	if sr.Status != "valid" {
		t.Fatal(sr.Diagnostics)
	}
	if first.Catalog.Sources[0].ObservedAt != "2026-10-15T00:00:00Z" || second.Catalog.Sources[0].ObservedAt != first.Catalog.Sources[0].ObservedAt {
		t.Fatal("reimport changed the captured source observation time")
	}
	var expiry1, expiry2 string
	for _, a := range first.Catalog.Assertions {
		if a.Dimension == "listed" {
			expiry1 = a.ExpiresAt
		}
	}
	for _, a := range second.Catalog.Assertions {
		if a.Dimension == "listed" {
			expiry2 = a.ExpiresAt
		}
	}
	if expiry1 != "2026-10-15T01:00:00Z" || expiry2 != "2026-10-15T02:00:00Z" {
		t.Fatalf("reimport did not apply overridden listed TTL: %s / %s", expiry1, expiry2)
	}
}

func TestClaudeNestedUnknownAndOfficialGuidanceProvenance(t *testing.T) {
	dir := t.TempDir()
	raw := []byte(`{"extractedOn":"2026-10-15","official":{"models":{"m":{"name":"Model","futureCapability":"raw only"}},"selectionMatrix":{"task":"model"},"futureSection":{"note":"preserved"}},"derivedMapping":{"rows":[]}}`)
	h := Hash(raw)
	_ = os.WriteFile(filepath.Join(dir, "source.json"), raw, 0600)
	m := Manifest{Version: ManifestVersion, Sources: []SourceInput{{ID: "claude", Format: "claude", Artifact: Asset{Path: "source.json", SHA256: h}, Evidence: []Asset{}}}, Mappings: []Mapping{}}
	p := Policy{Version: PolicyVersion, Sources: []SourceRule{{ID: "claude", ArtifactSHA256: h, Categories: []string{Published, Derived}}}, ValidityHours: DefaultValidityHours()}
	mb, _ := json.Marshal(m)
	pb, _ := json.Marshal(p)
	_ = os.WriteFile(filepath.Join(dir, "manifest.json"), mb, 0600)
	_ = os.WriteFile(filepath.Join(dir, "policy.json"), pb, 0600)
	c, r := Import(filepath.Join(dir, "manifest.json"), filepath.Join(dir, "policy.json"))
	if r.Status != "valid" {
		t.Fatalf("import failed: %#v", r.Diagnostics)
	}
	warned := false
	for _, d := range r.Diagnostics {
		if d.Code == "source.unknown_fields" {
			warned = true
		}
	}
	if !warned {
		t.Fatal("unknown nested model/official fields did not warn")
	}
	for _, a := range c.Catalog.Assertions {
		if a.Dimension == "futureCapability" {
			t.Fatal("unknown nested field was promoted")
		}
	}
	want := map[string]string{"/official/selectionMatrix": Published, "/official/futureSection": Published, "/derivedMapping": Derived}
	for _, g := range c.Catalog.Guidance {
		if category, ok := want[g.Pointer]; ok {
			if g.Category != category {
				t.Errorf("guidance %s category=%s want %s", g.Pointer, g.Category, category)
			}
			delete(want, g.Pointer)
		}
	}
	if len(want) != 0 {
		t.Fatalf("official/derived guidance provenance missing: %#v", want)
	}
	if c.validationSeal == "" || c.validationSeal != captureSeal(c) {
		t.Fatal("successful import was not sealed after capture completion")
	}
}

func TestOpenCodeAndAgyRequireSelectorAndLabel(t *testing.T) {
	for _, format := range []string{"opencode", "agy"} {
		for _, row := range []string{`{"label":"Model"}`, `{"selector":"p/model"}`} {
			t.Run(format+row, func(t *testing.T) {
				dir := t.TempDir()
				raw := []byte(`{"checkedOn":"2026-10-15","models":[` + row + `]}`)
				h := Hash(raw)
				_ = os.WriteFile(filepath.Join(dir, "source.json"), raw, 0600)
				m := Manifest{Version: ManifestVersion, Sources: []SourceInput{{ID: format, Format: format, Artifact: Asset{Path: "source.json", SHA256: h}, Evidence: []Asset{}}}, Mappings: []Mapping{}}
				p := Policy{Version: PolicyVersion, Sources: []SourceRule{{ID: format, ArtifactSHA256: h, Categories: requiredCategories(format)}}, ValidityHours: DefaultValidityHours()}
				mb, _ := json.Marshal(m)
				pb, _ := json.Marshal(p)
				_ = os.WriteFile(filepath.Join(dir, "manifest.json"), mb, 0600)
				_ = os.WriteFile(filepath.Join(dir, "policy.json"), pb, 0600)
				_, report := Import(filepath.Join(dir, "manifest.json"), filepath.Join(dir, "policy.json"))
				if report.Status != "invalid" {
					t.Fatal("malformed source model accepted")
				}
			})
		}
	}
}

func TestProbeImportPreservesTypedOutcomesWithoutFailureAssertions(t *testing.T) {
	outcomes := []string{"success", "authentication-failure", "unsupported-model", "timeout", "cancelled", "malformed-output"}
	for _, outcome := range outcomes {
		t.Run(outcome, func(t *testing.T) {
			dir := t.TempDir()
			cfg := Configuration{}
			if outcome == "success" {
				cfg.Host = String("codex")
				cfg.HostVersion = String("1.0")
				cfg.Provider = String("openai")
				cfg.ModelID = String("gpt-6")
			}
			probe := ProbeEvidence{Version: ProbeVersion, ID: "probe-" + outcome, Configuration: cfg, ProfileSHA256: Hash([]byte("profile")), ObservedAt: "2026-10-15T00:00:00Z", AccountScope: "test-account", Outcome: outcome, Observations: []ProbeObservation{}, EvidenceSHA256: Hash([]byte("redacted evidence"))}
			if outcome == "authentication-failure" {
				probe.Observations = []ProbeObservation{{Dimension: "selector", Value: json.RawMessage(`"gpt-6"`)}}
			}
			artifact, _ := json.Marshal(probe)
			h := Hash(artifact)
			if err := os.WriteFile(filepath.Join(dir, "probe.json"), artifact, 0600); err != nil {
				t.Fatal(err)
			}
			m := Manifest{Version: ManifestVersion, Sources: []SourceInput{{ID: "probe-" + outcome, Format: "probe", Artifact: Asset{Path: "probe.json", SHA256: h}, Evidence: []Asset{}}}, Mappings: []Mapping{}}
			p := Policy{Version: PolicyVersion, Sources: []SourceRule{{ID: "probe-" + outcome, ArtifactSHA256: h, Categories: []string{Invoked}}}, ValidityHours: DefaultValidityHours()}
			mb, _ := json.Marshal(m)
			pb, _ := json.Marshal(p)
			mp := filepath.Join(dir, "manifest.json")
			pp := filepath.Join(dir, "policy.json")
			_ = os.WriteFile(mp, mb, 0600)
			_ = os.WriteFile(pp, pb, 0600)
			c, r := Import(mp, pp)
			if r.Status != "valid" {
				t.Fatalf("import failed: %#v", r.Diagnostics)
			}
			if len(c.Catalog.Probes) != 1 || c.Catalog.Probes[0].Outcome != outcome {
				t.Fatal("typed probe outcome was not preserved")
			}
			if len(c.Catalog.Assertions) != 0 {
				t.Fatal("probe synthesized unbound catalog assertions")
			}
		})
	}
}
