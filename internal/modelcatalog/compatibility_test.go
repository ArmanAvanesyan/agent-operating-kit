package modelcatalog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
)

func TestCheckDoesNotPromoteListingsOrUnknownHostVersion(t *testing.T) {
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	c := &Capture{Catalog: Catalog{Assertions: []Assertion{{ID: "listed", EntryID: "e", SourceID: "s", Dimension: "listed", Value: json.RawMessage(`true`), Category: Listed, Scope: Configuration{}, ObservedAt: now.Format(time.RFC3339), ExpiresAt: now.Add(time.Hour).Format(time.RFC3339), Polarity: "supports"}}}}
	p := contracts.ExecutionProfile{Client: contracts.ClientSettings{Adapter: "codex"}, Model: contracts.ModelSettings{Selector: "model-x", Reasoning: contracts.ReasoningSettings{Mode: "enabled"}}}
	r := Check(c, p, now)
	if r.AuthorizationGranted || r.ExecutionReady || r.Compatibility != "unknown" {
		t.Fatalf("check granted authority or inferred support: %#v", r)
	}
	for _, f := range r.Findings {
		if f.Field == "selector" && f.Status != "unknown" {
			t.Fatalf("listing became selector support: %#v", f)
		}
	}
}

func TestCheckRejectsCaptureMutationAfterValidation(t *testing.T) {
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	newCapture := func() *Capture {
		return trustedCapture(t, Catalog{Assertions: []Assertion{{ID: "a", SourceID: "s", Dimension: "host", Value: json.RawMessage(`"codex"`), Category: Published, Polarity: "supports"}}})
	}
	mutators := []struct {
		name string
		fn   func(*Capture)
	}{
		{"catalog", func(c *Capture) { c.Catalog.Assertions[0].Value = json.RawMessage(`"other"`) }},
		{"policy", func(c *Capture) { c.Policy.Sources[0].Categories = nil }},
		{"captured-file", func(c *Capture) { c.Files["artifact.json"][0] = 'R' }},
		{"external-policy-flag", func(c *Capture) { c.PolicyExternal = false }},
	}
	profile := contracts.ExecutionProfile{Client: contracts.ClientSettings{Adapter: "codex"}}
	for _, tc := range mutators {
		t.Run(tc.name, func(t *testing.T) {
			c := newCapture()
			tc.fn(c)
			if got := Check(c, profile, now).Report.Status; got != "invalid" {
				t.Fatalf("mutated capture status = %q, want invalid", got)
			}
		})
	}
}

func TestCheckRequiresFreshExactScopedAssertion(t *testing.T) {
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	val := `"codex"`
	effort := String("medium")
	version := String("1.2.3")
	provider := String("acme")
	scope := Configuration{Host: String("codex"), HostVersion: version, Provider: provider, ModelID: String("x"), ReasoningMode: String("effort"), Effort: effort}
	c := trustedCapture(t, Catalog{Sources: []Source{{ID: "s", Artifact: Asset{Path: "artifact.json", SHA256: Hash([]byte("raw"))}}}, Assertions: []Assertion{{ID: "host", SourceID: "s", Dimension: "host", Value: json.RawMessage(val), Category: Published, Scope: scope, ObservedAt: now.Add(-time.Minute).Format(time.RFC3339), ExpiresAt: now.Add(time.Hour).Format(time.RFC3339), Polarity: "supports"}, {ID: "old", SourceID: "s", Dimension: "selector", Value: json.RawMessage(`"x"`), Category: Published, Scope: Configuration{}, ObservedAt: now.Add(-time.Hour).Format(time.RFC3339), ExpiresAt: now.Add(-time.Hour).Format(time.RFC3339), Polarity: "supports"}}})
	p := contracts.ExecutionProfile{Client: contracts.ClientSettings{Adapter: "codex", VersionConstraint: version}, Model: contracts.ModelSettings{Selector: "x", Provider: provider, Reasoning: contracts.ReasoningSettings{Mode: "effort", Effort: effort}}}
	r := Check(c, p, now)
	if r.AuthorizationGranted || r.ExecutionReady {
		t.Fatal("compatibility check granted execution")
	}
	for _, f := range r.Findings {
		if f.Field == "host" && f.Status != "supported" {
			t.Fatalf("exact scoped host assertion ignored: %#v", f)
		}
		if f.Field == "selector" && f.Status != "unknown" {
			t.Fatalf("expired assertion accepted: %#v", f)
		}
	}
}

func trustedCapture(t *testing.T, catalog Catalog) *Capture {
	t.Helper()
	hash := Hash([]byte("raw"))
	m := Manifest{Version: ManifestVersion, Sources: []SourceInput{{ID: "s", Format: "x", Artifact: Asset{Path: "artifact.json", SHA256: hash}, Evidence: []Asset{}}}, Mappings: []Mapping{}}
	mb, _ := json.Marshal(m)
	c := &Capture{Catalog: catalog, Policy: Policy{Version: PolicyVersion, Sources: []SourceRule{{ID: "s", ArtifactSHA256: hash, Categories: []string{Published, Invoked}}}}, Files: map[string][]byte{"manifest.json": mb, "artifact.json": []byte("raw")}, PolicyExternal: true}
	c.validationSeal = captureSeal(c)
	return c
}

func TestInspectNeverGrantsExecutionAndDiffIsAdditive(t *testing.T) {
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	a := &Capture{Catalog: Catalog{Entries: []Entry{{ID: "one"}}}}
	b := &Capture{Catalog: Catalog{Entries: []Entry{{ID: "one"}, {ID: "two"}}}}
	i := Inspect(b, now)
	if i.AuthorizationGranted || i.ExecutionReady || i.ProbeGate != "pending-p6" {
		t.Fatal("inspection promoted authority")
	}
	d := Diff(a, b, now)
	if len(d.Changes) != 1 || d.Changes[0].ID != "two" || d.Changes[0].Change != "added" {
		t.Fatalf("unexpected semantic diff %#v", d.Changes)
	}
}

func TestCheckConflictAndUnparsedVersionStayUnknown(t *testing.T) {
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	effort := String("medium")
	version := String("1.2.3")
	provider := String("acme")
	scope := Configuration{Host: String("codex"), HostVersion: version, Provider: provider, ModelID: String("x"), ReasoningMode: String("effort"), Effort: effort}
	base := Assertion{EntryID: "e", SourceID: "s", Dimension: "host", Value: json.RawMessage(`"codex"`), Category: Published, Scope: scope, ObservedAt: now.Add(-time.Minute).Format(time.RFC3339), ExpiresAt: now.Add(time.Hour).Format(time.RFC3339)}
	positive := base
	positive.ID = "p"
	positive.Polarity = "supports"
	negative := base
	negative.ID = "n"
	negative.Polarity = "contradicts"
	c := trustedCapture(t, Catalog{Sources: []Source{{ID: "s", Artifact: Asset{Path: "artifact.json", SHA256: Hash([]byte("raw"))}}}, Assertions: []Assertion{positive, negative}})
	p := contracts.ExecutionProfile{Client: contracts.ClientSettings{Adapter: "codex", VersionConstraint: version}, Model: contracts.ModelSettings{Selector: "x", Provider: provider, Reasoning: contracts.ReasoningSettings{Mode: "effort", Effort: effort}}}
	r := Check(c, p, now)
	if r.Report.Status != "valid" {
		t.Fatalf("conflict blocked check: %s", r.Report.Status)
	}
	for _, f := range r.Findings {
		if f.Field == "host" && f.Status != "unknown" {
			t.Fatalf("conflict resolved by order: %#v", f)
		}
	}
	p.Client.VersionConstraint = String(">=1.0")
	r = Check(c, p, now)
	for _, f := range r.Findings {
		if f.Field == "hostVersion" && f.Status != "unknown" {
			t.Fatalf("constraint was treated as exact version: %#v", f)
		}
	}
}

func TestExternalValidityCanExpireButNotRefreshAnAssertion(t *testing.T) {
	observed := time.Date(2026, 10, 23, 10, 0, 0, 0, time.UTC)
	expiry := observed.Add(20 * time.Hour)
	c := trustedCapture(t, Catalog{Sources: []Source{{ID: "s", Artifact: Asset{Path: "artifact.json", SHA256: Hash([]byte("raw"))}}}, Assertions: []Assertion{{ID: "a", SourceID: "s", EntryID: "e", Dimension: "host", Category: Published, Value: json.RawMessage(`"codex"`), ObservedAt: observed.Format(time.RFC3339), ExpiresAt: expiry.Format(time.RFC3339)}}})
	c.Policy.ValidityHours = map[string]int{}
	c.Policy.ValidityHours[Published] = 1
	c.validationSeal = captureSeal(c)
	if got := assertionFreshness(c, c.Catalog.Assertions[0], observed.Add(2*time.Hour)); got != "expired" {
		t.Fatalf("effective policy refreshed old evidence: %s", got)
	}
}

func TestPricingUsesShortDefaultExpiryAndExpiresAtBoundary(t *testing.T) {
	observed := time.Date(2026, 10, 23, 10, 0, 0, 0, time.UTC)
	c := trustedCapture(t, Catalog{Sources: []Source{{ID: "s", Artifact: Asset{Path: "artifact.json", SHA256: Hash([]byte("raw"))}}}, Assertions: []Assertion{{ID: "price", SourceID: "s", EntryID: "e", Dimension: "price.inputPerMillionTokens", Category: "pricing", Value: json.RawMessage(`1.25`), ObservedAt: observed.Format(time.RFC3339), ExpiresAt: observed.Add(720 * time.Hour).Format(time.RFC3339)}}})
	c.Policy.Sources[0].Categories = append(c.Policy.Sources[0].Categories, "pricing")
	c.validationSeal = captureSeal(c)
	if got := assertionFreshness(c, c.Catalog.Assertions[0], observed.Add(24*time.Hour-time.Second)); got != "fresh" {
		t.Fatalf("pricing expired too early: %s", got)
	}
	if got := assertionFreshness(c, c.Catalog.Assertions[0], observed.Add(24*time.Hour)); got != "expired" {
		t.Fatalf("pricing boundary must be expired: %s", got)
	}
}

func TestExactSuccessfulProbeSupportsOnlyExplicitObservations(t *testing.T) {
	version, provider, effort := String("1.2.3"), String("acme"), String("medium")
	p := contracts.ExecutionProfile{Client: contracts.ClientSettings{Adapter: "codex", VersionConstraint: version}, Model: contracts.ModelSettings{Selector: "x", Provider: provider, Reasoning: contracts.ReasoningSettings{Mode: "effort", Effort: effort}}}
	profileBytes, _ := json.Marshal(p)
	rawEvidence := []byte("redacted observation")
	probe := ProbeEvidence{Version: ProbeVersion, ID: "p1", Configuration: Configuration{Host: String("codex"), HostVersion: version, Provider: provider, ModelID: String("x"), ReasoningMode: String("effort"), Effort: effort}, ProfileSHA256: Hash(profileBytes), ObservedAt: "2026-10-23T11:00:00Z", AccountScope: "account-hash", Outcome: "success", Observations: []ProbeObservation{{Dimension: "selector", Value: json.RawMessage(`"x"`)}}, EvidenceSHA256: Hash(rawEvidence)}
	artifact, _ := json.Marshal(probe)
	artifactHash := Hash(artifact)
	evidenceHash := Hash(rawEvidence)
	m := Manifest{Version: ManifestVersion, Sources: []SourceInput{{ID: "probe-source", Format: "probe", Artifact: Asset{Path: "probe.json", SHA256: artifactHash}, Evidence: []Asset{{Path: "evidence.json", SHA256: evidenceHash}}}}, Mappings: []Mapping{}}
	mb, _ := json.Marshal(m)
	c := &Capture{Catalog: Catalog{Sources: []Source{{ID: "probe-source", Format: "probe", ObservedAt: probe.ObservedAt, Artifact: Asset{Path: "probe.json", SHA256: artifactHash}, Evidence: []Asset{{Path: "evidence.json", SHA256: evidenceHash}}}}, Probes: []ProbeEvidence{probe}}, Policy: Policy{Sources: []SourceRule{{ID: "probe-source", ArtifactSHA256: artifactHash, Categories: []string{Invoked}}}}, Files: map[string][]byte{"manifest.json": mb, "probe.json": artifact, "evidence.json": rawEvidence}, PolicyExternal: true}
	c.validationSeal = captureSeal(c)
	r := CheckWithProfileDigest(c, p, Hash(profileBytes), time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC))
	if r.Report.Status != "valid" {
		t.Fatalf("successful probe blocked: %#v", r.Report)
	}
	selector, invocation := false, false
	for _, f := range r.Findings {
		if f.Field == "selector" && f.Status == "supported" {
			selector = true
		}
		if f.Field == "invocation" && f.Status == "supported" {
			invocation = true
		}
		if f.Field == "tools" && f.Status == "supported" {
			t.Fatal("successful invocation inferred tool support")
		}
	}
	if !selector || !invocation {
		t.Fatalf("explicit probe observations missing: %#v", r.Findings)
	}
	p.Model.Provider = String("other")
	different, _ := json.Marshal(p)
	r = CheckWithProfileDigest(c, p, Hash(different), time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC))
	for _, f := range r.Findings {
		if (f.Field == "selector" || f.Field == "invocation") && f.Status == "supported" {
			t.Fatalf("profile mismatch trusted probe: %#v", f)
		}
	}
}

func TestImportedProbeSurvivesLoadAndSupportsOnlyPinnedObservation(t *testing.T) {
	root, err := os.MkdirTemp("/private/tmp", "aok-probe-roundtrip-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	version, provider, effort := String("1.2.3"), String("acme"), String("medium")
	p := contracts.ExecutionProfile{Client: contracts.ClientSettings{Adapter: "codex", VersionConstraint: version}, Model: contracts.ModelSettings{Selector: "x", Provider: provider, Reasoning: contracts.ReasoningSettings{Mode: "effort", Effort: effort}}}
	profileBytes, _ := json.Marshal(p)
	probe := ProbeEvidence{Version: ProbeVersion, ID: "imported-probe", Configuration: Configuration{Host: String("codex"), HostVersion: version, Provider: provider, ModelID: String("x"), ReasoningMode: String("effort"), Effort: effort}, ProfileSHA256: Hash(profileBytes), ObservedAt: "2026-10-23T11:00:00Z", AccountScope: "account-hash", Outcome: "success", Observations: []ProbeObservation{{Dimension: "selector", Value: json.RawMessage(`"x"`)}}, EvidenceSHA256: Hash([]byte("redacted external evidence reference"))}
	artifact, _ := json.Marshal(probe)
	artifactHash := Hash(artifact)
	manifest := Manifest{Version: ManifestVersion, Sources: []SourceInput{{ID: "probe-source", Format: "probe", Artifact: Asset{Path: "probe.json", SHA256: artifactHash}, Evidence: []Asset{}}}, Mappings: []Mapping{}}
	manifestBytes, _ := json.Marshal(manifest)
	policy := Policy{Version: PolicyVersion, Sources: []SourceRule{{ID: "probe-source", ArtifactSHA256: artifactHash, Categories: []string{Invoked}}}, ValidityHours: DefaultValidityHours()}
	policyBytes, _ := json.Marshal(policy)
	input := filepath.Join(root, "input")
	if err = os.MkdirAll(input, 0700); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{"probe.json": artifact, "manifest.json": manifestBytes, "policy.json": policyBytes} {
		if err = os.WriteFile(filepath.Join(input, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	imported, report := Import(filepath.Join(input, "manifest.json"), filepath.Join(input, "policy.json"))
	if report.Status != "valid" {
		t.Fatalf("import failed: %#v", report.Diagnostics)
	}
	bundle := filepath.Join(root, "bundle")
	for name, data := range imported.Files {
		path := filepath.Join(bundle, filepath.FromSlash(name))
		if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	loaded, loadReport := Load(bundle, filepath.Join(input, "policy.json"))
	if loadReport.Status != "valid" {
		t.Fatalf("load failed: %#v", loadReport.Diagnostics)
	}
	r := CheckWithProfileDigest(loaded, p, Hash(profileBytes), time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC))
	if r.Report.Status != "valid" {
		t.Fatalf("check failed: %#v", r.Report)
	}
	selector, invocation := false, false
	for _, f := range r.Findings {
		if f.Field == "selector" && f.Status == "supported" {
			selector = true
		}
		if f.Field == "invocation" && f.Status == "supported" {
			invocation = true
		}
		if f.Field == "tools" && f.Status == "supported" {
			t.Fatal("probe invocation inferred tool support")
		}
	}
	if !selector || !invocation {
		t.Fatalf("loaded typed probe evidence not applied: %#v", r.Findings)
	}
}
