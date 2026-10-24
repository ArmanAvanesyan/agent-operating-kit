package modelcatalog

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
)

func captureSeal(c *Capture) string {
	if c == nil {
		return ""
	}
	files := map[string]string{}
	for name, b := range c.Files {
		files[name] = Hash(b)
	}
	b, _ := json.Marshal(struct {
		Catalog    Catalog           `json:"catalog"`
		Policy     Policy            `json:"policy"`
		CatalogSHA string            `json:"catalogSha"`
		PolicySHA  string            `json:"policySha"`
		External   bool              `json:"external"`
		Files      map[string]string `json:"files"`
	}{c.Catalog, c.Policy, c.CatalogSHA256, c.PolicySHA256, c.PolicyExternal, files})
	return Hash(b)
}

func ReadPolicy(path string) (Policy, []byte, error) {
	b, err := ReadFile(path)
	if err != nil {
		return Policy{}, nil, err
	}
	p, err := decodePolicy(b)
	if err != nil {
		return Policy{}, b, err
	}
	return p, b, nil
}

func decodePolicy(b []byte) (Policy, error) {
	if err := Validate(b, "policy.json"); err != nil {
		return Policy{}, err
	}
	var p Policy
	if err := Decode(b, &p); err != nil {
		return Policy{}, err
	}
	if p.Version != PolicyVersion {
		return p, fmt.Errorf("unsupported policy version %q", p.Version)
	}
	seen := map[string]bool{}
	for _, s := range p.Sources {
		if s.ID == "" || seen[s.ID] {
			return Policy{}, fmt.Errorf("empty or duplicate policy source %q", s.ID)
		}
		seen[s.ID] = true
	}
	return p, nil
}

func Load(bundle, policyPath string) (*Capture, Report) {
	r := baseReport()
	bad := func(err error) (*Capture, Report) {
		r.Status = "invalid"
		r.Diagnostics = append(r.Diagnostics, diag(err.Error()))
		return nil, r
	}
	capturedPolicyBytes, err := ReadAsset(bundle, "policy.json")
	if err != nil {
		return bad(err)
	}
	capturedPolicy, err := decodePolicy(capturedPolicyBytes)
	if err != nil {
		return bad(err)
	}
	policy, effectivePolicyBytes := capturedPolicy, capturedPolicyBytes
	if policyPath != "" {
		policy, effectivePolicyBytes, err = ReadPolicy(policyPath)
	}
	if err != nil {
		return bad(err)
	}
	files := map[string][]byte{}
	var total int64
	put := func(name string, b []byte) error {
		if _, ok := files[name]; ok {
			return fmt.Errorf("duplicate captured path %q", name)
		}
		if len(files) >= 512 {
			return errors.New("capture exceeds 512 files")
		}
		total += int64(len(b))
		if total > 64<<20 {
			return errors.New("capture exceeds 64 MiB")
		}
		files[name] = append([]byte(nil), b...)
		return nil
	}
	for _, name := range []string{"manifest.json", "catalog.json", "source-manifest.json"} {
		b, e := ReadAsset(bundle, name)
		if e != nil {
			return bad(e)
		}
		if e = put(name, b); e != nil {
			return bad(e)
		}
	}
	if err = Validate(files["manifest.json"], "manifest.json"); err != nil {
		return bad(err)
	}
	if err = Validate(files["catalog.json"], "catalog.json"); err != nil {
		return bad(err)
	}
	var m Manifest
	var c Catalog
	if err = Decode(files["manifest.json"], &m); err != nil {
		return bad(err)
	}
	if err = Decode(files["catalog.json"], &c); err != nil {
		return bad(err)
	}
	if m.Version != ManifestVersion || c.Version != Version {
		return bad(errors.New("unsupported manifest or catalog version"))
	}
	var original Manifest
	if err = Validate(files["source-manifest.json"], "manifest.json"); err != nil {
		return bad(fmt.Errorf("source manifest: %w", err))
	}
	if err = Decode(files["source-manifest.json"], &original); err != nil {
		return bad(err)
	}
	if original.Version != m.Version || !sameManifestSources(original, m) {
		return bad(errors.New("source manifest provenance does not match canonical manifest"))
	}
	if err = put("policy.json", capturedPolicyBytes); err != nil {
		return bad(err)
	}
	for _, s := range m.Sources {
		for _, a := range append([]Asset{s.Artifact}, s.Evidence...) {
			if _, exists := files[a.Path]; exists {
				return bad(fmt.Errorf("duplicate source asset path %q", a.Path))
			}
			b, e := ReadAsset(bundle, a.Path)
			if e != nil {
				return bad(e)
			}
			if Hash(b) != a.SHA256 {
				return bad(fmt.Errorf("source asset hash mismatch: %s", a.Path))
			}
			if e = put(a.Path, b); e != nil {
				return bad(e)
			}
		}
		if s.Format == "probe" {
			if err = Validate(files[s.Artifact.Path], "probe.json"); err != nil {
				return bad(fmt.Errorf("probe source %s: %w", s.ID, err))
			}
		}
	}
	trusted, err := validatePins(policy, m, c)
	if err != nil {
		return bad(err)
	}
	projection, e := BuildCatalog(m, capturedPolicy, files)
	if e != nil {
		return bad(fmt.Errorf("catalog replay: %w", e))
	}
	got, _ := json.Marshal(projection)
	want, _ := json.Marshal(c)
	if !bytes.Equal(got, want) {
		return bad(errors.New("catalog projection does not match captured source evidence"))
	}
	cap := &Capture{Catalog: c, Policy: policy, Files: files, CatalogSHA256: Hash(files["catalog.json"]), PolicySHA256: Hash(effectivePolicyBytes), PolicyExternal: policyPath != ""}
	cap.validationSeal = captureSeal(cap)
	if !trusted {
		r.Diagnostics = append(r.Diagnostics, contracts.Diagnostic{Code: "modelcatalog.untrusted-policy", Severity: "warning", Message: "source category or artifact is not approved by the supplied policy"})
	}
	return cap, r
}

func sameManifestSources(original, canonical Manifest) bool {
	if len(original.Sources) != len(canonical.Sources) || len(original.Mappings) != len(canonical.Mappings) {
		return false
	}
	a, _ := json.Marshal(original.Mappings)
	b, _ := json.Marshal(canonical.Mappings)
	if !bytes.Equal(a, b) {
		return false
	}
	by := map[string]SourceInput{}
	for _, s := range original.Sources {
		if _, ok := by[s.ID]; ok {
			return false
		}
		by[s.ID] = s
	}
	for _, s := range canonical.Sources {
		o, ok := by[s.ID]
		if !ok || o.Format != s.Format || o.Artifact.SHA256 != s.Artifact.SHA256 || len(o.Evidence) != len(s.Evidence) {
			return false
		}
		for i := range o.Evidence {
			if o.Evidence[i].SHA256 != s.Evidence[i].SHA256 {
				return false
			}
		}
	}
	return true
}

func validatePins(p Policy, m Manifest, c Catalog) (bool, error) {
	trusted := true
	rules := map[string]SourceRule{}
	for _, x := range p.Sources {
		if _, ok := rules[x.ID]; ok {
			return false, fmt.Errorf("duplicate policy source %q", x.ID)
		}
		rules[x.ID] = x
	}
	sources := map[string]Source{}
	for _, s := range c.Sources {
		if _, ok := sources[s.ID]; ok {
			return false, fmt.Errorf("duplicate source %q", s.ID)
		}
		sources[s.ID] = s
	}
	seen := map[string]bool{}
	for _, x := range m.Sources {
		rule, ok := rules[x.ID]
		if !ok || rule.ArtifactSHA256 != x.Artifact.SHA256 {
			trusted = false
		}
		s, ok := sources[x.ID]
		if !ok {
			return false, fmt.Errorf("catalog omits source %q", x.ID)
		}
		if s.Artifact != x.Artifact || s.Evidence == nil && len(x.Evidence) > 0 || len(s.Evidence) != len(x.Evidence) {
			return false, fmt.Errorf("catalog source provenance mismatch for %q", x.ID)
		}
		for i := range x.Evidence {
			if s.Evidence[i] != x.Evidence[i] {
				return false, fmt.Errorf("catalog evidence provenance mismatch for %q", x.ID)
			}
		}
		seen[x.ID] = true
	}
	for id := range sources {
		if !seen[id] {
			return false, fmt.Errorf("unmanifested catalog source %q", id)
		}
	}
	if err := validateCatalog(c, p); err != nil {
		return false, err
	}
	for _, a := range c.Assertions {
		r, ok := rules[a.SourceID]
		if !ok || !contains(r.Categories, a.Category) {
			trusted = false
		}
	}
	for _, g := range c.Guidance {
		r, ok := rules[g.SourceID]
		if !ok || !contains(r.Categories, g.Category) {
			trusted = false
		}
	}
	return trusted, nil
}

func contains(xs []string, v string) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

func validateCatalog(c Catalog, p Policy) error {
	sources := map[string]bool{}
	configurations := map[string]Configuration{}
	for _, cfg := range c.Configurations {
		if _, ok := configurations[cfg.ID]; ok {
			return fmt.Errorf("duplicate configuration %s", cfg.ID)
		}
		if ConfigurationID(cfg.Identity) != cfg.ID {
			return fmt.Errorf("configuration identity hash mismatch %s", cfg.ID)
		}
		configurations[cfg.ID] = cfg.Identity
	}
	for _, s := range c.Sources {
		if s.ID == "" || sources[s.ID] {
			return fmt.Errorf("empty or duplicate source %q", s.ID)
		}
		sources[s.ID] = true
		if _, e := time.Parse(time.RFC3339, s.ObservedAt); e != nil {
			return fmt.Errorf("bad observedAt for %s", s.ID)
		}
	}
	entries := map[string]bool{}
	for _, e := range c.Entries {
		if entries[e.ID] {
			return fmt.Errorf("duplicate entry %s", e.ID)
		}
		entries[e.ID] = true
		if !sources[e.SourceID] {
			return fmt.Errorf("entry %s references missing source", e.ID)
		}
		if _, ok := configurations[e.ConfigurationID]; !ok {
			return fmt.Errorf("entry %s has invalid configuration reference", e.ID)
		}
	}
	ids := map[string]bool{}
	for _, a := range c.Assertions {
		if a.ID == "" || ids[a.ID] {
			return fmt.Errorf("empty or duplicate assertion %q", a.ID)
		}
		ids[a.ID] = true
		if !entries[a.EntryID] || !sources[a.SourceID] {
			return fmt.Errorf("assertion %s has missing reference", a.ID)
		}
		if _, e := time.Parse(time.RFC3339, a.ObservedAt); e != nil {
			return fmt.Errorf("bad assertion observation time %s", a.ID)
		}
		if _, e := time.Parse(time.RFC3339, a.ExpiresAt); e != nil {
			return fmt.Errorf("bad assertion expiry %s", a.ID)
		}
		if len(a.Value) == 0 || !json.Valid(a.Value) {
			return fmt.Errorf("invalid assertion value %s", a.ID)
		}
	}
	for _, g := range c.Guidance {
		if g.ID == "" || ids[g.ID] {
			return fmt.Errorf("empty or duplicate guidance %q", g.ID)
		}
		ids[g.ID] = true
		if !sources[g.SourceID] {
			return fmt.Errorf("guidance %s has invalid source", g.ID)
		}
		if len(g.Content) == 0 || !json.Valid(g.Content) {
			return fmt.Errorf("invalid guidance content %s", g.ID)
		}
	}
	probeIDs := map[string]bool{}
	for _, p := range c.Probes {
		if p.ID == "" || probeIDs[p.ID] {
			return fmt.Errorf("empty or duplicate probe %q", p.ID)
		}
		probeIDs[p.ID] = true
		if p.Version != ProbeVersion || p.AccountScope == "" {
			return fmt.Errorf("probe %s has invalid version or account scope", p.ID)
		}
		if _, e := time.Parse(time.RFC3339, p.ObservedAt); e != nil {
			return fmt.Errorf("probe %s has invalid observation time", p.ID)
		}
		switch p.Outcome {
		case "success", "authentication-failure", "unsupported-model", "timeout", "cancelled", "malformed-output":
		default:
			return fmt.Errorf("probe %s has invalid outcome", p.ID)
		}
		if !validSHA256(p.ProfileSHA256) || !validSHA256(p.EvidenceSHA256) {
			return fmt.Errorf("probe %s has invalid digest", p.ID)
		}
		for _, o := range p.Observations {
			if !probeDimension(o.Dimension) || !probeValueValid(o.Dimension, o.Value) {
				return fmt.Errorf("probe %s has invalid observation", p.ID)
			}
		}
	}
	return nil
}

func validSHA256(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func probeDimension(s string) bool {
	switch s {
	case "selector", "provider", "host", "hostVersion", "reasoning.mode", "reasoning.effort", "reasoning.budgetTokens", "generation.maxOutputTokens", "generation.temperature", "generation.topP", "contextTokens", "structuredOutput", "toolUse", "modalities":
		return true
	}
	return false
}

func probeValueValid(dimension string, raw json.RawMessage) bool {
	if len(raw) == 0 || !json.Valid(raw) {
		return false
	}
	var v any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if d.Decode(&v) != nil {
		return false
	}
	switch dimension {
	case "selector", "provider", "host", "hostVersion", "reasoning.mode", "reasoning.effort":
		s, ok := v.(string)
		return ok && strings.TrimSpace(s) != ""
	case "reasoning.budgetTokens", "generation.maxOutputTokens", "generation.temperature", "generation.topP", "contextTokens":
		_, ok := v.(json.Number)
		return ok
	case "structuredOutput", "toolUse":
		_, ok := v.(bool)
		return ok
	case "modalities":
		items, ok := v.([]any)
		if !ok {
			return false
		}
		for _, item := range items {
			s, ok := item.(string)
			if !ok || strings.TrimSpace(s) == "" {
				return false
			}
		}
		return true
	}
	return false
}

func baseReport() Report {
	return Report{Status: "valid", Diagnostics: []contracts.Diagnostic{}, Counts: map[string]int{}, AuthorizationGranted: false, ExecutionReady: false, ProbeGate: "pending-p6"}
}
func diag(s string) contracts.Diagnostic {
	return contracts.Diagnostic{Code: "modelcatalog.invalid", Severity: "error", Message: s}
}
func ReadBundleFile(root, rel string) ([]byte, error) { return ReadAsset(root, rel) }
