package modelcatalog

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
)

func Inspect(c *Capture, asOf time.Time) Inspection {
	r := baseReport()
	fresh := map[string]string{}
	if c == nil {
		r.Status = "invalid"
		r.Diagnostics = append(r.Diagnostics, diag("missing capture"))
		return Inspection{Report: r, Freshness: fresh}
	}
	for _, a := range c.Catalog.Assertions {
		status := assertionFreshness(c, a, asOf)
		if status == "invalid" {
			fresh[a.ID] = "invalid"
		} else {
			fresh[a.ID] = status
		}
	}
	for _, g := range c.Catalog.Guidance {
		fresh[g.ID] = guidanceFreshness(c, g, asOf)
	}
	for _, p := range c.Catalog.Probes {
		fresh[p.ID] = probeFreshness(c, p, asOf)
	}
	r.Counts = map[string]int{"sources": len(c.Catalog.Sources), "entries": len(c.Catalog.Entries), "assertions": len(c.Catalog.Assertions), "guidance": len(c.Catalog.Guidance), "probes": len(c.Catalog.Probes)}
	return Inspection{Report: r, AsOf: asOf.UTC().Format(time.RFC3339), Freshness: fresh, Catalog: c.Catalog}
}

func Diff(old, new *Capture, asOf time.Time) DiffReport {
	r := baseReport()
	out := DiffReport{Report: r, Changes: []Change{}}
	if old == nil || new == nil {
		out.Report.Status = "invalid"
		out.Report.Diagnostics = append(out.Report.Diagnostics, diag("diff requires two captures"))
		return out
	}
	compare := func(cat string, a, b map[string]string) {
		for id, x := range a {
			if y, ok := b[id]; !ok {
				out.Changes = append(out.Changes, Change{cat, id, "not-present-in-new-capture"})
			} else if x != y {
				out.Changes = append(out.Changes, Change{cat, id, "changed"})
			}
		}
		for id := range b {
			if _, ok := a[id]; !ok {
				out.Changes = append(out.Changes, Change{cat, id, "added"})
			}
		}
	}
	mapEntries := func(c *Capture) map[string]string {
		m := map[string]string{}
		for _, x := range c.Catalog.Entries {
			b, _ := json.Marshal(x)
			m[x.ID] = string(b)
		}
		return m
	}
	mapAssertions := func(c *Capture) map[string]string {
		m := map[string]string{}
		for _, x := range c.Catalog.Assertions {
			x.Value = json.RawMessage(canonicalJSON(x.Value))
			b, _ := json.Marshal(x)
			m[x.ID] = string(b)
		}
		return m
	}
	mapGuidance := func(c *Capture) map[string]string {
		m := map[string]string{}
		for _, x := range c.Catalog.Guidance {
			x.Content = json.RawMessage(canonicalJSON(x.Content))
			b, _ := json.Marshal(x)
			m[x.ID] = string(b)
		}
		return m
	}
	mapConfigs := func(c *Capture) map[string]string {
		m := map[string]string{}
		for _, x := range c.Catalog.Configurations {
			b, _ := json.Marshal(x)
			m[x.ID] = string(b)
		}
		return m
	}
	mapSources := func(c *Capture) map[string]string {
		m := map[string]string{}
		for _, name := range []string{"source-manifest.json", "policy.json"} {
			if raw, ok := c.Files[name]; ok {
				m["captured:"+name] = Hash(raw)
			}
		}
		if c.PolicySHA256 != "" {
			m["effective-policy"] = c.PolicySHA256
		}
		for _, x := range c.Catalog.Sources {
			b, _ := json.Marshal(x)
			m[x.ID] = string(b)
			if raw, ok := c.Files[x.Artifact.Path]; ok {
				m[x.ID+":artifact-bytes"] = Hash(raw)
			}
			for _, e := range x.Evidence {
				if raw, ok := c.Files[e.Path]; ok {
					m[x.ID+":"+e.Path] = Hash(raw)
				}
			}
		}
		return m
	}
	mapProbe := func(c *Capture) map[string]string {
		m := map[string]string{}
		for _, x := range c.Catalog.Probes {
			for i := range x.Observations {
				x.Observations[i].Value = json.RawMessage(canonicalJSON(x.Observations[i].Value))
			}
			b, _ := json.Marshal(x)
			m[x.ID] = string(b)
		}
		return m
	}
	mapFreshness := func(c *Capture) map[string]string {
		m := map[string]string{}
		for id, status := range Inspect(c, asOf).Freshness {
			m[id] = status
		}
		return m
	}
	compare("entry", mapEntries(old), mapEntries(new))
	compare("configuration", mapConfigs(old), mapConfigs(new))
	compare("assertion", mapAssertions(old), mapAssertions(new))
	compare("guidance", mapGuidance(old), mapGuidance(new))
	compare("source-provenance", mapSources(old), mapSources(new))
	compare("probe", mapProbe(old), mapProbe(new))
	if !asOf.IsZero() {
		compare("freshness", mapFreshness(old), mapFreshness(new))
	}
	sort.Slice(out.Changes, func(i, j int) bool {
		a, b := out.Changes[i], out.Changes[j]
		if a.Category != b.Category {
			return a.Category < b.Category
		}
		if a.ID != b.ID {
			return a.ID < b.ID
		}
		return a.Change < b.Change
	})
	return out
}

func Check(c *Capture, profile contracts.ExecutionProfile, asOf time.Time) CheckReport {
	b, _ := json.Marshal(profile)
	h := sha256.Sum256(b)
	return CheckWithProfileDigest(c, profile, hex.EncodeToString(h[:]), asOf)
}

func CheckWithProfileDigest(c *Capture, profile contracts.ExecutionProfile, profileSHA256 string, asOf time.Time) CheckReport {
	r := baseReport()
	out := CheckReport{Report: r, Compatibility: "unknown", Findings: []Finding{}}
	if c == nil {
		out.Report.Status = "invalid"
		out.Report.Diagnostics = append(out.Report.Diagnostics, diag("missing capture"))
		return out
	}
	if c.validationSeal == "" || c.validationSeal != captureSeal(c) {
		out.Report.Status = "invalid"
		out.Report.Diagnostics = append(out.Report.Diagnostics, contracts.Diagnostic{Code: "modelcatalog.capture.integrity", Severity: "error", Message: "capture was not validated or has changed since validation"})
		return out
	}
	if !c.PolicyExternal {
		out.Report.Status = "blocked"
		out.Report.Diagnostics = append(out.Report.Diagnostics, contracts.Diagnostic{Code: "modelcatalog.policy.required", Severity: "error", Message: "compatibility checks require an explicitly supplied external policy"})
		out.Findings = append(out.Findings, Finding{Field: "policy", Status: "unknown", Message: "external policy is required", AssertionIDs: []string{}})
		return out
	}
	known := map[string]string{"selector": profile.Model.Selector, "reasoning.mode": profile.Model.Reasoning.Mode}
	if profile.Model.Provider != nil {
		known["provider"] = *profile.Model.Provider
	}
	if profile.Model.Reasoning.Effort != nil {
		known["reasoning.effort"] = *profile.Model.Reasoning.Effort
	}
	if profile.Model.Reasoning.BudgetTokens != nil {
		known["reasoning.budgetTokens"] = string(*profile.Model.Reasoning.BudgetTokens)
	}
	if profile.Model.Generation != nil {
		g := profile.Model.Generation
		for k, v := range map[string]*json.Number{"generation.maxOutputTokens": g.MaxOutputTokens, "generation.temperature": g.Temperature, "generation.topP": g.TopP} {
			if v != nil {
				known[k] = string(*v)
			}
		}
	}
	if profile.Client.VersionConstraint != nil {
		known["hostVersion"] = *profile.Client.VersionConstraint
	}
	// A host selector is a declared adapter identity. Constraint strings are
	// retained as unresolved when they are not a concrete exact version.
	known["host"] = profile.Client.Adapter
	state := map[string][]Assertion{}
	for _, a := range c.Catalog.Assertions {
		if !assertionTrusted(c, a) {
			out.Report.Diagnostics = append(out.Report.Diagnostics, contracts.Diagnostic{Code: "modelcatalog.untrusted-assertion", Severity: "warning", Source: a.SourceID, Field: a.Dimension, Message: "assertion is retained but its source or category is not approved by the effective policy"})
			continue
		}
		state[a.Dimension] = append(state[a.Dimension], a)
	}
	fields := make([]string, 0, len(known))
	for field := range known {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	for _, field := range fields {
		value := known[field]
		f := Finding{Field: field, Status: "unknown", Message: "no fresh approved scoped assertion establishes this value", AssertionIDs: []string{}}
		if field == "hostVersion" && !exactVersion(value) {
			out.Findings = append(out.Findings, f)
			continue
		}
		positive, negative := false, false
		probeValues := map[string]bool{}
		probeMismatch := false
		for _, a := range state[field] {
			if !fresh(c, a, asOf) || !scopeMatchesField(a.Scope, profile, field) {
				continue
			}
			f.AssertionIDs = append(f.AssertionIDs, a.ID)
			f.Category = a.Category
			if assertionMatches(field, a.Value, value) {
				if a.Polarity == "contradicts" {
					negative = true
				} else if a.Polarity == "supports" {
					positive = true
				}
			}
		}
		for _, probe := range c.Catalog.Probes {
			if !probeTrusted(c, probe) || probe.Outcome != "success" || probe.ProfileSHA256 != profileSHA256 || !probeMatchesProfile(probe.Configuration, profile) || probeFreshness(c, probe, asOf) != "fresh" {
				continue
			}
			for _, observation := range probe.Observations {
				if observation.Dimension != field {
					continue
				}
				candidate := canonicalJSON(observation.Value)
				probeValues[candidate] = true
				if assertionMatches(field, observation.Value, value) {
					positive = true
					f.AssertionIDs = append(f.AssertionIDs, probe.ID)
					f.Category = Invoked
				} else {
					probeMismatch = true
				}
			}
		}
		switch {
		case len(probeValues) > 1 || probeMismatch && positive:
			f.Status = "unknown"
			f.Message = "fresh exact-profile observations conflict"
			out.Report.Diagnostics = append(out.Report.Diagnostics, contracts.Diagnostic{Code: "modelcatalog.conflict", Severity: "warning", Field: field, Message: "probe observations disagree for the same exact profile"})
		case positive && negative:
			f.Status = "unknown"
			f.Message = "fresh scoped evidence conflicts"
			out.Report.Diagnostics = append(out.Report.Diagnostics, contracts.Diagnostic{Code: "modelcatalog.conflict", Severity: "warning", Field: field, Message: "supporting and contradicting evidence apply to the same configuration"})
		case negative:
			f.Status = "unsupported"
			f.Message = "fresh scoped evidence rejects requested value"
		case positive:
			f.Status = "supported"
			f.Message = "fresh scoped evidence matches requested value"
		}
		out.Findings = append(out.Findings, f)
	}
	invocation := Finding{Field: "invocation", Status: "unknown", Message: "no fresh successful invocation is bound to this exact profile and configuration", AssertionIDs: []string{}}
	for _, probe := range c.Catalog.Probes {
		observed, err := time.Parse(time.RFC3339, probe.ObservedAt)
		if err != nil || probeFreshness(c, probe, asOf) != "fresh" || observed.After(asOf) || probe.Outcome != "success" || probe.AccountScope == "" || probe.ProfileSHA256 != profileSHA256 || !probeMatchesProfile(probe.Configuration, profile) || !probeTrusted(c, probe) {
			continue
		}
		invocation.Status = "supported"
		invocation.Message = "a fresh redacted observation records a successful invocation for this exact profile and configuration"
		invocation.AssertionIDs = append(invocation.AssertionIDs, probe.ID)
	}
	out.Findings = append(out.Findings, invocation)
	for _, e := range c.Catalog.Entries {
		if e.ScheduledRemoval != nil {
			d, err := time.Parse(time.RFC3339, *e.ScheduledRemoval)
			if err != nil {
				d, err = time.Parse("2006-01-02", *e.ScheduledRemoval)
			}
			cfg := Configuration{}
			for _, x := range c.Catalog.Configurations {
				if x.ID == e.ConfigurationID {
					cfg = x.Identity
					break
				}
			}
			selectorMatch := e.Selector != nil && *e.Selector == profile.Model.Selector
			sourceApproved := false
			for _, s := range c.Catalog.Sources {
				if s.ID == e.SourceID {
					sourceApproved = sourcePolicy(c, e.SourceID, Published, s.Artifact.SHA256)
				}
			}
			providerMatch := profile.Model.Provider != nil && cfg.Provider != nil && *cfg.Provider == *profile.Model.Provider
			if err == nil && !asOf.Before(d) && selectorMatch && sourceApproved && providerMatch {
				out.Report.Status = "blocked"
				out.Compatibility = "unsupported"
				out.Findings = append(out.Findings, Finding{Field: "scheduledRemoval", Status: "unsupported", Message: fmt.Sprintf("entry %s is past scheduled removal", e.ID), AssertionIDs: []string{}})
			}
		}
	}
	allSupported := true
	anyUnsupported := false
	requested := 0
	for _, f := range out.Findings {
		if f.Field == "invocation" || f.Field == "scheduledRemoval" {
			if f.Status == "unsupported" {
				anyUnsupported = true
			}
			continue
		}
		requested++
		if f.Status == "unsupported" {
			anyUnsupported = true
		}
		if f.Status != "supported" {
			allSupported = false
		}
	}
	if anyUnsupported {
		out.Compatibility = "unsupported"
		out.Report.Status = "blocked"
	} else if requested > 0 && allSupported {
		out.Compatibility = "supported"
	} else {
		out.Compatibility = "unknown"
	}
	sort.Slice(out.Findings, func(i, j int) bool { return out.Findings[i].Field < out.Findings[j].Field })
	return out
}

func probeMatchesProfile(c Configuration, p contracts.ExecutionProfile) bool {
	if p.Client.VersionConstraint == nil || !exactVersion(*p.Client.VersionConstraint) || p.Model.Provider == nil {
		return false
	}
	expected := Configuration{Host: String(p.Client.Adapter), HostVersion: p.Client.VersionConstraint, Provider: p.Model.Provider, ModelID: String(p.Model.Selector), ReasoningMode: String(p.Model.Reasoning.Mode), Effort: p.Model.Reasoning.Effort, ReasoningBudgetTokens: p.Model.Reasoning.BudgetTokens, Generation: p.Model.Generation}
	if !jsonEqual(c, expected) {
		return false
	}
	return true
}

func scopeMatchesField(s Configuration, p contracts.ExecutionProfile, field string) bool {
	if s.Host == nil || *s.Host != p.Client.Adapter {
		return false
	}
	if p.Client.VersionConstraint == nil || !exactVersion(*p.Client.VersionConstraint) || s.HostVersion == nil || *s.HostVersion != *p.Client.VersionConstraint {
		return false
	}
	if p.Model.Provider == nil || s.Provider == nil || *s.Provider != *p.Model.Provider {
		return false
	}
	if s.ModelID == nil || *s.ModelID != p.Model.Selector {
		return false
	}
	if s.ReasoningMode == nil || *s.ReasoningMode != p.Model.Reasoning.Mode {
		return false
	}
	if (p.Model.Reasoning.Effort == nil) != (s.Effort == nil) || p.Model.Reasoning.Effort != nil && *s.Effort != *p.Model.Reasoning.Effort {
		return false
	}
	if (p.Model.Reasoning.BudgetTokens == nil) != (s.ReasoningBudgetTokens == nil) || p.Model.Reasoning.BudgetTokens != nil && string(*s.ReasoningBudgetTokens) != string(*p.Model.Reasoning.BudgetTokens) {
		return false
	}
	if (p.Model.Generation == nil) != (s.Generation == nil) || p.Model.Generation != nil && !jsonEqual(*s.Generation, *p.Model.Generation) {
		return false
	}
	if s.ModelRevision != nil || s.ServiceTier != nil || s.AuthMode != nil || s.Deployment != nil || s.ToolProfile != nil {
		return false
	}
	if field == "hostVersion" && (s.HostVersion == nil || !exactVersion(*s.HostVersion)) {
		return false
	}
	return true
}

func assertionMatches(field string, raw []byte, wanted string) bool {
	if field == "generation.maxOutputTokens" || field == "generation.temperature" || field == "generation.topP" || field == "reasoning.budgetTokens" {
		var got json.Number
		d := json.NewDecoder(strings.NewReader(string(raw)))
		d.UseNumber()
		if d.Decode(&got) != nil {
			return false
		}
		a, ok := new(big.Rat).SetString(string(got))
		if !ok {
			return false
		}
		b, ok := new(big.Rat).SetString(wanted)
		if !ok {
			return false
		}
		if field == "generation.maxOutputTokens" {
			return a.Cmp(b) >= 0
		}
		return a.Cmp(b) == 0
	}
	w, _ := json.Marshal(wanted)
	return canonicalJSON(raw) == string(w)
}

func sourcePolicy(c *Capture, sourceID, category, hash string) bool {
	if c == nil {
		return false
	}
	var m Manifest
	if Decode(c.Files["manifest.json"], &m) != nil {
		return false
	}
	var input *SourceInput
	for i := range m.Sources {
		if m.Sources[i].ID == sourceID {
			input = &m.Sources[i]
			break
		}
	}
	if input == nil || input.Artifact.SHA256 != hash {
		return false
	}
	var src *Source
	for i := range c.Catalog.Sources {
		if c.Catalog.Sources[i].ID == sourceID {
			src = &c.Catalog.Sources[i]
			break
		}
	}
	if src == nil || src.Artifact.SHA256 != hash {
		return false
	}
	for _, r := range c.Policy.Sources {
		if r.ID == sourceID && r.ArtifactSHA256 == hash && contains(r.Categories, category) {
			return true
		}
	}
	return false
}

func assertionTrusted(c *Capture, a Assertion) bool {
	var src *Source
	for i := range c.Catalog.Sources {
		if c.Catalog.Sources[i].ID == a.SourceID {
			src = &c.Catalog.Sources[i]
			break
		}
	}
	return src != nil && sourcePolicy(c, a.SourceID, a.Category, src.Artifact.SHA256)
}

func probeTrusted(c *Capture, p ProbeEvidence) bool {
	var m Manifest
	if Decode(c.Files["manifest.json"], &m) != nil {
		return false
	}
	for _, src := range m.Sources {
		if src.Format != "probe" {
			continue
		}
		raw, ok := c.Files[src.Artifact.Path]
		if !ok || Hash(raw) != src.Artifact.SHA256 {
			continue
		}
		var observed ProbeEvidence
		if Decode(raw, &observed) != nil || !sameProbe(observed, p) {
			continue
		}
		// EvidenceSHA256 is a declared digest reference to externally retained,
		// redacted evidence. Probe bundles intentionally do not embed response
		// bodies or other free-form evidence files.
		return sourcePolicy(c, src.ID, Invoked, src.Artifact.SHA256)
	}
	return false
}

func sameProbe(a, b ProbeEvidence) bool {
	if a.Version != b.Version || a.ID != b.ID || !jsonEqual(a.Configuration, b.Configuration) || a.ProfileSHA256 != b.ProfileSHA256 || a.ObservedAt != b.ObservedAt || a.AccountScope != b.AccountScope || a.Outcome != b.Outcome || a.EvidenceSHA256 != b.EvidenceSHA256 || len(a.Observations) != len(b.Observations) {
		return false
	}
	for i := range a.Observations {
		if a.Observations[i].Dimension != b.Observations[i].Dimension || canonicalJSON(a.Observations[i].Value) != canonicalJSON(b.Observations[i].Value) {
			return false
		}
	}
	return true
}
func jsonEqual(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

func canonicalJSON(raw []byte) string {
	var value any
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.UseNumber()
	if d.Decode(&value) != nil {
		return string(raw)
	}
	b, err := json.Marshal(value)
	if err != nil {
		return string(raw)
	}
	return string(b)
}

func fresh(c *Capture, a Assertion, t time.Time) bool { return assertionFreshness(c, a, t) == "fresh" }

func assertionFreshness(c *Capture, a Assertion, t time.Time) string {
	if !assertionTrusted(c, a) {
		return "untrusted"
	}
	expiry, e := time.Parse(time.RFC3339, a.ExpiresAt)
	observed, oe := time.Parse(time.RFC3339, a.ObservedAt)
	if e != nil || oe != nil {
		return "invalid"
	}
	if observed.After(t) {
		return "future"
	}
	hours := validityFor(c, a.Category)
	if strings.HasPrefix(a.Dimension, "price.") || a.Dimension == "offer" {
		if pricing := validityFor(c, "pricing"); pricing > 0 && (hours == 0 || pricing < hours) {
			hours = pricing
		}
	}
	if h := hours; h > 0 {
		limit := observed.Add(time.Duration(h) * time.Hour)
		if limit.Before(expiry) {
			expiry = limit
		}
	}
	if !t.Before(expiry) {
		return "expired"
	}
	return "fresh"
}
func guidanceFreshness(c *Capture, g Guidance, t time.Time) string {
	if !sourcePolicy(c, g.SourceID, g.Category, sourceHash(c, g.SourceID)) {
		return "untrusted"
	}
	expiry, e := time.Parse(time.RFC3339, g.ExpiresAt)
	observed, oe := time.Parse(time.RFC3339, g.ObservedAt)
	if e != nil || oe != nil {
		return "invalid"
	}
	if observed.After(t) {
		return "future"
	}
	if h := validityFor(c, g.Category); h > 0 {
		limit := observed.Add(time.Duration(h) * time.Hour)
		if limit.Before(expiry) {
			expiry = limit
		}
	}
	if !t.Before(expiry) {
		return "expired"
	}
	return "fresh"
}
func sourceHash(c *Capture, id string) string {
	for _, s := range c.Catalog.Sources {
		if s.ID == id {
			return s.Artifact.SHA256
		}
	}
	return ""
}
func probeFreshness(c *Capture, p ProbeEvidence, t time.Time) string {
	if !probeTrusted(c, p) {
		return "untrusted"
	}
	o, e := time.Parse(time.RFC3339, p.ObservedAt)
	if e != nil {
		return "invalid"
	}
	if o.After(t) {
		return "future"
	}
	hours := validityFor(c, Invoked)
	if !t.Before(o.Add(time.Duration(hours) * time.Hour)) {
		return "expired"
	}
	return "fresh"
}

func validityFor(c *Capture, category string) int {
	h := DefaultValidityHours()[category]
	if c != nil && c.Policy.ValidityHours[category] > 0 {
		h = c.Policy.ValidityHours[category]
	}
	return h
}
func exactVersion(v string) bool {
	return regexp.MustCompile(`^v?\d+\.\d+(?:\.\d+)?(?:[-+][A-Za-z0-9.-]+)?$`).MatchString(v)
}
