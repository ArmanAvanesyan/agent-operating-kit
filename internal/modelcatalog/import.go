package modelcatalog

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Import captures the explicitly pinned research inputs into a deterministic,
// offline catalog. It never contacts a source or executes a command.
func Import(manifestPath, policyPath string) (*Capture, Report) {
	cap := &Capture{Files: map[string][]byte{}, PolicyExternal: true}
	r := baseReport()
	mb, err := ReadFile(manifestPath)
	if err != nil {
		return cap, diagnostic(r, "manifest", err)
	}
	var m Manifest
	if err = Decode(mb, &m); err != nil {
		return cap, diagnostic(r, "manifest", err)
	}
	if err = Validate(mb, "manifest.json"); err != nil {
		return cap, diagnostic(r, "manifest", err)
	}
	if m.Version != ManifestVersion {
		return cap, diagnostic(r, "manifest.version", fmt.Errorf("unsupported manifest version %q", m.Version))
	}
	p, pb, err := ReadPolicy(policyPath)
	if err != nil {
		return cap, diagnostic(r, "policy", err)
	}
	root := filepath.Dir(manifestPath)
	cap.Policy = p
	cap.PolicySHA256 = Hash(pb)
	cap.Files["source-manifest.json"] = append([]byte(nil), mb...)
	cap.Files["policy.json"] = pb
	cat := Catalog{Version: Version, ImporterVersion: ImporterVersion, Sources: []Source{}, Entries: []Entry{}, Configurations: []ConfigurationRecord{}, Assertions: []Assertion{}, Guidance: []Guidance{}, Probes: []ProbeEvidence{}}
	files := map[string]bool{"manifest.json": true, "policy.json": true}
	seen := map[string]bool{}
	seenProbes := map[string]bool{}
	modelPointers := map[string]map[string]string{}
	declaredFiles := 5 // canonical manifest, original manifest, policy, catalog, report
	for _, source := range m.Sources {
		declaredFiles += 1 + len(source.Evidence)
	}
	if declaredFiles > 512 {
		return cap, diagnostic(r, "manifest", fmt.Errorf("capture exceeds 512 files"))
	}
	for _, si := range m.Sources {
		if si.ID == "" || seen[si.ID] || safeID(si.ID) != si.ID {
			return cap, diagnostic(r, "sources", fmt.Errorf("empty or duplicate source id %q", si.ID))
		}
		seen[si.ID] = true
		if si.Format == "probe" && len(si.Evidence) != 0 {
			return cap, diagnostic(r, "sources.probe", fmt.Errorf("probe sources use the typed artifact digest; separate evidence files are not accepted"))
		}
		rule, ok := ruleFor(p, si.ID)
		if !ok {
			r.Diagnostics = append(r.Diagnostics, warning("policy.untrusted_source", fmt.Sprintf("source %s has no policy rule; evidence retained as untrusted", si.ID)))
		} else if rule.ArtifactSHA256 != si.Artifact.SHA256 {
			r.Diagnostics = append(r.Diagnostics, warning("policy.stale_pin", fmt.Sprintf("source %s differs from its policy-pinned copy; evidence retained as untrusted", si.ID)))
		} else {
			for _, category := range requiredCategories(si.Format) {
				if !hasValue(rule.Categories, category) {
					r.Diagnostics = append(r.Diagnostics, warning("policy.untrusted_category", fmt.Sprintf("source %s policy omits category %s; evidence retained as untrusted", si.ID, category)))
				}
			}
		}
		art, err := ReadAsset(root, si.Artifact.Path)
		if err != nil {
			return cap, diagnostic(r, "sources.artifact", err)
		}
		if Hash(art) != si.Artifact.SHA256 {
			return cap, diagnostic(r, "sources.artifact", fmt.Errorf("digest mismatch for %s", si.ID))
		}
		if overCaptureBudget(cap.Files, len(art)) {
			return cap, diagnostic(r, "capture", fmt.Errorf("capture exceeds 64 MiB"))
		}
		if err = contracts.ValidateJSON(art); err != nil {
			return cap, diagnostic(r, "sources.artifact", err)
		}
		var raw any
		dec := json.NewDecoder(bytes.NewReader(art))
		dec.UseNumber()
		if err = dec.Decode(&raw); err != nil {
			return cap, diagnostic(r, "sources.artifact", err)
		}
		base := "sources/" + safeID(si.ID) + "/"
		addCaptured(cap.Files, files, base+"artifact.json", art)
		canonicalEvidence := make([]Asset, 0, len(si.Evidence))
		for i, a := range si.Evidence {
			b, e := ReadAsset(root, a.Path)
			if e != nil {
				return cap, diagnostic(r, "sources.evidence", e)
			}
			if Hash(b) != a.SHA256 {
				return cap, diagnostic(r, "sources.evidence", fmt.Errorf("digest mismatch for %s", a.Path))
			}
			if overCaptureBudget(cap.Files, len(b)) {
				return cap, diagnostic(r, "capture", fmt.Errorf("capture exceeds 64 MiB"))
			}
			dest := base + fmt.Sprintf("evidence/%02d-%s", i, filepath.Base(a.Path))
			addCaptured(cap.Files, files, dest, b)
			canonicalEvidence = append(canonicalEvidence, Asset{Path: dest, SHA256: a.SHA256})
		}
		m.Sources[sourceIndex(m.Sources, si.ID)].Artifact.Path = base + "artifact.json"
		m.Sources[sourceIndex(m.Sources, si.ID)].Evidence = canonicalEvidence
		observed := extractDate(raw)
		src := Source{ID: si.ID, Format: si.Format, ObservedAt: observed, URLs: extractURLs(raw), Artifact: Asset{Path: base + "artifact.json", SHA256: Hash(art)}, Evidence: []Asset{}}
		for i, a := range si.Evidence {
			src.Evidence = append(src.Evidence, Asset{Path: base + fmt.Sprintf("evidence/%02d-%s", i, filepath.Base(a.Path)), SHA256: a.SHA256})
		}
		cat.Sources = append(cat.Sources, src)
		if si.Format == "probe" {
			if len(si.Evidence) != 0 {
				return cap, diagnostic(r, "sources.probe", fmt.Errorf("probe source %s cannot contain separate evidence assets", si.ID))
			}
			if err = Validate(art, "probe.json"); err != nil {
				return cap, diagnostic(r, "sources.probe", err)
			}
			var probe ProbeEvidence
			if err = Decode(art, &probe); err != nil {
				return cap, diagnostic(r, "sources.probe", err)
			}
			if probe.Version != ProbeVersion {
				return cap, diagnostic(r, "sources.probe", fmt.Errorf("unsupported probe version %q", probe.Version))
			}
			if probe.ID == "" || seenProbes[probe.ID] {
				return cap, diagnostic(r, "sources.probe", fmt.Errorf("empty or duplicate probe id %q", probe.ID))
			}
			seenProbes[probe.ID] = true
			if probe.Outcome == "success" && (empty(probe.Configuration.Host) || empty(probe.Configuration.HostVersion) || empty(probe.Configuration.ModelID) || empty(probe.Configuration.Provider)) {
				return cap, diagnostic(r, "sources.probe", fmt.Errorf("successful probe requires host, host version, provider, and model id"))
			}
			cat.Probes = append(cat.Probes, probe)
			continue
		}
		if si.Format != "codex" && si.Format != "claude" && si.Format != "opencode" && si.Format != "agy" {
			return cap, diagnostic(r, "sources.format", fmt.Errorf("unsupported format %q", si.Format))
		}
		if err = validateSourceShape(si.Format, raw); err != nil {
			return cap, diagnostic(r, "sources.artifact", err)
		}
		if !hasSourceDate(raw) {
			return cap, diagnostic(r, "sources.artifact", fmt.Errorf("%s artifact is missing its capture date", si.Format))
		}
		models := extractModels(si.Format, raw)
		if len(models) == 0 {
			return cap, diagnostic(r, "sources.artifact", fmt.Errorf("%s artifact has no models in its required shape", si.Format))
		}
		if err = normalizeModelRemovals(models); err != nil {
			return cap, diagnostic(r, "sources.artifact", err)
		}
		modelPointers[si.ID] = map[string]string{}
		for _, mm := range models {
			modelPointers[si.ID][mm.id] = mm.pointer
		}
		for _, model := range models {
			cfg := Configuration{}
			cid := ConfigurationID(cfg)
			id := si.ID + ":" + model.id
			selector := (*string)(nil)
			for _, mp := range m.Mappings {
				if mp.SourceID == si.ID && mp.SourceModelID == model.id {
					if mp.Pointer != model.pointer {
						return cap, diagnostic(r, "mappings.pointer", fmt.Errorf("mapping pointer for %s does not identify its source model", mp.SourceModelID))
					}
					selector = String(mp.Selector)
					cfg.Provider = mp.Provider
					cid = ConfigurationID(cfg)
					cat.Configurations = appendConfig(cat.Configurations, ConfigurationRecord{ID: cid, Identity: cfg})
					break
				}
			}
			cid = ConfigurationID(cfg)
			cat.Configurations = appendConfig(cat.Configurations, ConfigurationRecord{ID: cid, Identity: cfg})
			cat.Entries = append(cat.Entries, Entry{ID: id, SourceID: si.ID, SourceModelID: model.id, Name: model.name, Selector: selector, Aliases: []string{}, ConfigurationID: cid, ScheduledRemoval: model.removal})
			for _, fact := range model.facts {
				category := categoryFor(si.Format, fact.dimension)
				cat.Assertions = append(cat.Assertions, Assertion{ID: id + ":" + fact.dimension, EntryID: id, SourceID: si.ID, Pointer: fact.pointer, Dimension: fact.dimension, Value: fact.value, Category: category, Scope: cfg, ObservedAt: observed, ExpiresAt: expiryFor(category, fact.dimension, observed, p.ValidityHours), Polarity: "supports"})
			}
		}
		// Repository recommendations remain guidance, with their raw source pointer.
		for _, g := range extractGuidance(si.ID, raw) {
			g.ObservedAt = observed
			g.ExpiresAt = expiryFor(g.Category, "", observed, p.ValidityHours)
			cat.Guidance = append(cat.Guidance, g)
		}
		if hasUnknownTop(si.Format, raw) || hasUnknownNested(si.Format, raw) {
			r.Diagnostics = append(r.Diagnostics, warning("source.unknown_fields", si.ID+" contains fields outside the importer allowlist; preserved raw"))
		}
	}
	seenMappings := map[mappingKey]bool{}
	for _, mp := range m.Mappings {
		if !seen[mp.SourceID] {
			return cap, diagnostic(r, "mappings", fmt.Errorf("unknown source %q", mp.SourceID))
		}
		if mp.Selector == "" || mp.SourceModelID == "" || mp.Pointer == "" {
			return cap, diagnostic(r, "mappings", fmt.Errorf("mapping requires model id and selector"))
		}
		key := mappingKey{mp.SourceID, mp.SourceModelID}
		if seenMappings[key] {
			return cap, diagnostic(r, "mappings", fmt.Errorf("duplicate mapping for %s", mp.SourceModelID))
		}
		seenMappings[key] = true
		if modelPointers[mp.SourceID][mp.SourceModelID] == "" || modelPointers[mp.SourceID][mp.SourceModelID] != mp.Pointer {
			return cap, diagnostic(r, "mappings.pointer", fmt.Errorf("mapping does not point to an exact source model"))
		}
	}
	canonicalManifest, _ := json.MarshalIndent(m, "", "  ")
	canonicalManifest = append(canonicalManifest, '\n')
	cap.Files["manifest.json"] = canonicalManifest
	sort.Slice(cat.Sources, func(i, j int) bool { return cat.Sources[i].ID < cat.Sources[j].ID })
	sort.Slice(cat.Entries, func(i, j int) bool { return cat.Entries[i].ID < cat.Entries[j].ID })
	sort.Slice(cat.Configurations, func(i, j int) bool { return cat.Configurations[i].ID < cat.Configurations[j].ID })
	sort.Slice(cat.Assertions, func(i, j int) bool { return cat.Assertions[i].ID < cat.Assertions[j].ID })
	sort.Slice(cat.Guidance, func(i, j int) bool { return cat.Guidance[i].ID < cat.Guidance[j].ID })
	cb, _ := json.MarshalIndent(cat, "", "  ")
	cb = append(cb, '\n')
	if err = Validate(cb, "catalog.json"); err != nil {
		return cap, diagnostic(r, "catalog", err)
	}
	if err = validateCatalog(cat, p); err != nil {
		return cap, diagnostic(r, "catalog", err)
	}
	cap.Catalog = cat
	cap.CatalogSHA256 = Hash(cb)
	cap.Files["catalog.json"] = cb
	cap.Files["import-report.json"], _ = json.MarshalIndent(r, "", "  ")
	cap.Files["import-report.json"] = append(cap.Files["import-report.json"], '\n')
	r.Status = "valid"
	r.Counts = map[string]int{"sources": len(cat.Sources), "entries": len(cat.Entries), "assertions": len(cat.Assertions), "guidance": len(cat.Guidance), "probes": len(cat.Probes)}
	cap.Files["import-report.json"], _ = json.MarshalIndent(r, "", "  ")
	cap.Files["import-report.json"] = append(cap.Files["import-report.json"], '\n')
	if !withinCaptureBudget(cap.Files) {
		return cap, diagnostic(r, "capture", fmt.Errorf("capture exceeds size or file-count bound"))
	}
	cap.validationSeal = captureSeal(cap)
	return cap, r
}

// BuildCatalog deterministically replays the normalized manifest against
// captured bundle bytes. It performs no filesystem access.
func BuildCatalog(m Manifest, p Policy, files map[string][]byte) (Catalog, error) {
	cat := Catalog{Version: Version, ImporterVersion: ImporterVersion, Sources: []Source{}, Entries: []Entry{}, Configurations: []ConfigurationRecord{}, Assertions: []Assertion{}, Guidance: []Guidance{}, Probes: []ProbeEvidence{}}
	knownSources := map[string]bool{}
	knownModels := map[string]map[string]string{}
	for _, si := range m.Sources {
		if si.ID == "" || safeID(si.ID) != si.ID || knownSources[si.ID] {
			return cat, fmt.Errorf("invalid or duplicate source id %q", si.ID)
		}
		knownSources[si.ID] = true
		art, ok := files[si.Artifact.Path]
		if !ok {
			return cat, fmt.Errorf("missing captured artifact %s", si.Artifact.Path)
		}
		if Hash(art) != si.Artifact.SHA256 {
			return cat, fmt.Errorf("artifact digest mismatch: %s", si.ID)
		}
		if err := contracts.ValidateJSON(art); err != nil {
			return cat, err
		}
		var raw any
		d := json.NewDecoder(bytes.NewReader(art))
		d.UseNumber()
		if e := d.Decode(&raw); e != nil {
			return cat, e
		}
		observed := extractDate(raw)
		src := Source{ID: si.ID, Format: si.Format, ObservedAt: observed, URLs: extractURLs(raw), Artifact: si.Artifact, Evidence: append([]Asset{}, si.Evidence...)}
		cat.Sources = append(cat.Sources, src)
		if si.Format == "probe" {
			if len(si.Evidence) != 0 {
				return cat, fmt.Errorf("probe source %s cannot contain separate evidence assets", si.ID)
			}
			if err := Validate(art, "probe.json"); err != nil {
				return cat, err
			}
			var probe ProbeEvidence
			if err := Decode(art, &probe); err != nil {
				return cat, err
			}
			if probe.Version != ProbeVersion {
				return cat, fmt.Errorf("unsupported probe version %q", probe.Version)
			}
			if probe.Outcome == "success" && (empty(probe.Configuration.Host) || empty(probe.Configuration.HostVersion) || empty(probe.Configuration.ModelID) || empty(probe.Configuration.Provider)) {
				return cat, fmt.Errorf("successful probe has incomplete configuration identity")
			}
			cat.Probes = append(cat.Probes, probe)
			continue
		}
		if !hasSourceDate(raw) {
			return cat, fmt.Errorf("source %s is missing its capture date", si.ID)
		}
		if si.Format != "codex" && si.Format != "claude" && si.Format != "opencode" && si.Format != "agy" {
			return cat, fmt.Errorf("unsupported source format %q", si.Format)
		}
		if err := validateSourceShape(si.Format, raw); err != nil {
			return cat, err
		}
		models := extractModels(si.Format, raw)
		if len(models) == 0 {
			return cat, fmt.Errorf("source %s has no models in its required shape", si.ID)
		}
		if err := normalizeModelRemovals(models); err != nil {
			return cat, err
		}
		pointers := map[string]string{}
		for _, model := range models {
			pointers[model.id] = model.pointer
		}
		knownModels[si.ID] = pointers
		for _, model := range models {
			id := si.ID + ":" + model.id
			cfg := Configuration{}
			selector := (*string)(nil)
			for _, mp := range m.Mappings {
				if mp.SourceID == si.ID && mp.SourceModelID == model.id {
					if mp.Pointer != model.pointer {
						return cat, fmt.Errorf("mapping pointer does not identify source model %s", mp.SourceModelID)
					}
					selector = String(mp.Selector)
					cfg.Provider = mp.Provider
					break
				}
			}
			cid := ConfigurationID(cfg)
			cat.Configurations = appendConfig(cat.Configurations, ConfigurationRecord{ID: cid, Identity: cfg})
			cat.Entries = append(cat.Entries, Entry{ID: id, SourceID: si.ID, SourceModelID: model.id, Name: model.name, Selector: selector, Aliases: []string{}, ConfigurationID: cid, ScheduledRemoval: model.removal})
			for _, f := range model.facts {
				category := categoryFor(si.Format, f.dimension)
				cat.Assertions = append(cat.Assertions, Assertion{ID: id + ":" + f.dimension, EntryID: id, SourceID: si.ID, Pointer: f.pointer, Dimension: f.dimension, Value: f.value, Category: category, Scope: cfg, ObservedAt: observed, ExpiresAt: expiryFor(category, f.dimension, observed, p.ValidityHours), Polarity: "supports"})
			}
		}
		for _, mp := range m.Mappings {
			if mp.SourceID == si.ID && pointers[mp.SourceModelID] == "" {
				return cat, fmt.Errorf("mapping references unknown source model %s", mp.SourceModelID)
			}
		}
		for _, g := range extractGuidance(si.ID, raw) {
			g.ObservedAt = observed
			g.ExpiresAt = expiryFor(g.Category, "", observed, p.ValidityHours)
			cat.Guidance = append(cat.Guidance, g)
		}
	}
	seenMapping := map[mappingKey]bool{}
	for _, mp := range m.Mappings {
		key := mappingKey{mp.SourceID, mp.SourceModelID}
		if !knownSources[mp.SourceID] || mp.Selector == "" || mp.SourceModelID == "" || mp.Pointer == "" || knownModels[mp.SourceID][mp.SourceModelID] != mp.Pointer || seenMapping[key] {
			return cat, fmt.Errorf("invalid mapping for %s/%s", mp.SourceID, mp.SourceModelID)
		}
		seenMapping[key] = true
	}
	sort.Slice(cat.Sources, func(i, j int) bool { return cat.Sources[i].ID < cat.Sources[j].ID })
	sort.Slice(cat.Entries, func(i, j int) bool { return cat.Entries[i].ID < cat.Entries[j].ID })
	sort.Slice(cat.Configurations, func(i, j int) bool { return cat.Configurations[i].ID < cat.Configurations[j].ID })
	sort.Slice(cat.Assertions, func(i, j int) bool { return cat.Assertions[i].ID < cat.Assertions[j].ID })
	sort.Slice(cat.Guidance, func(i, j int) bool { return cat.Guidance[i].ID < cat.Guidance[j].ID })
	return cat, nil
}

type fact struct {
	dimension, pointer string
	value              json.RawMessage
}
type mappingKey struct{ sourceID, modelID string }
type model struct {
	id, name             string
	pointer, basePointer string
	removal              *string
	malformedRemoval     bool
	facts                []fact
}

func extractModels(format string, raw any) []model {
	root, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	out := []model{}
	add := func(id string, v any, pointer string) {
		basePointer := strings.TrimSuffix(pointer, "/selector")
		m := model{id: id, name: id, pointer: pointer, basePointer: basePointer}
		if s, ok := v.(string); ok {
			m.name = s
		}
		obj, _ := v.(map[string]any)
		if s, ok := obj["name"].(string); ok {
			m.name = s
		}
		if s, ok := obj["label"].(string); ok {
			m.name = s
		}
		fieldDims := map[string]string{"positioning": "positioning", "latency": "latency", "priceInPerMTok": "price.inputPerMillionTokens", "priceOutPerMTok": "price.outputPerMillionTokens", "priceNote": "price.note", "defaultEffort": "defaultEffort", "defaultEffortClaudeCode": "defaultEffortClaudeCode", "contextTokens": "contextTokens", "maxOutputTokens": "maxOutputTokens", "effort": "listedEffort", "thinking": "thinkingMode", "thinkingMode": "thinkingMode", "selector": "listedSelector", "offer": "offer"}
		for k, d := range fieldDims {
			if val, ok := obj[k]; ok && val != nil {
				if format == "agy" && k == "thinking" {
					d = "listedThinking"
				}
				m.facts = append(m.facts, fact{d, basePointer + "/" + k, rawJSON(val)})
			}
		}
		if val, exists := obj["scheduledRemoval"]; exists && val != nil {
			if s, ok := val.(string); ok {
				m.removal = String(s)
			} else {
				m.malformedRemoval = true
			}
		}
		if format == "opencode" || format == "agy" {
			m.facts = append(m.facts, fact{"listed", basePointer, rawJSON(true)})
		}
		out = append(out, m)
	}
	switch format {
	case "codex":
		if mm, ok := root["models"].(map[string]any); ok {
			keys := sortedKeys(mm)
			for _, k := range keys {
				add(k, mm[k], "/models/"+escapePointer(k))
			}
		}
	case "claude":
		o, _ := root["official"].(map[string]any)
		mm, _ := o["models"].(map[string]any)
		for _, k := range sortedKeys(mm) {
			add(k, mm[k], "/official/models/"+escapePointer(k))
		}
	default:
		arr, _ := root["models"].([]any)
		for i, v := range arr {
			o, _ := v.(map[string]any)
			id, _ := o["selector"].(string)
			if id == "" {
				id = fmt.Sprint(i)
			}
			ptr := fmt.Sprintf("/models/%d", i)
			if _, ok := o["selector"].(string); ok {
				ptr += "/selector"
			}
			add(id, v, ptr)
		}
	}
	return out
}
func extractGuidance(source string, raw any) []Guidance {
	root, _ := raw.(map[string]any)
	out := []Guidance{}
	for _, k := range []string{"derivedMapping", "policy", "recommendationsStatus", "aokIntegration", "categories", "guidanceLadder"} {
		if v, ok := root[k]; ok {
			b, _ := json.Marshal(v)
			out = append(out, Guidance{ID: source + ":" + k, SourceID: source, Pointer: "/" + k, Category: Derived, Content: b})
		}
	}
	if v, ok := root["benchmarkSource"]; ok {
		b, _ := json.Marshal(v)
		out = append(out, Guidance{ID: source + ":benchmarkSource", SourceID: source, Pointer: "/benchmarkSource", Category: Published, Content: b})
	}
	if source == "claude" {
		official, _ := root["official"].(map[string]any)
		for _, k := range sortedKeys(official) {
			if k == "models" {
				continue
			}
			b, _ := json.Marshal(official[k])
			out = append(out, Guidance{ID: source + ":official:" + k, SourceID: source, Pointer: "/official/" + escapePointer(k), Category: Published, Content: b})
		}
	}
	return out
}
func extractDate(v any) string {
	m, _ := v.(map[string]any)
	if s, ok := m["observedAt"].(string); ok {
		if t, e := time.Parse(time.RFC3339, s); e == nil {
			return t.UTC().Format(time.RFC3339)
		}
		return ""
	}
	for _, k := range []string{"extractedOn", "checkedOn"} {
		if s, ok := m[k].(string); ok {
			if t, e := time.Parse(time.DateOnly, s); e == nil {
				return t.UTC().Format(time.RFC3339)
			}
			if t, e := time.Parse(time.RFC3339, s); e == nil {
				return t.UTC().Format(time.RFC3339)
			}
			return ""
		}
	}
	return ""
}
func hasSourceDate(v any) bool { return extractDate(v) != "" }
func normalizeModelRemovals(models []model) error {
	for i := range models {
		if models[i].malformedRemoval {
			return fmt.Errorf("model %s has non-string scheduledRemoval", models[i].id)
		}
		if models[i].removal == nil {
			continue
		}
		s := *models[i].removal
		if t, e := time.Parse(time.DateOnly, s); e == nil {
			models[i].removal = String(t.UTC().Format(time.RFC3339))
			continue
		}
		if t, e := time.Parse(time.RFC3339, s); e == nil {
			models[i].removal = String(t.UTC().Format(time.RFC3339))
			continue
		}
		return fmt.Errorf("model %s has invalid scheduledRemoval %q", models[i].id, s)
	}
	return nil
}
func extractURLs(v any) []string {
	m, _ := v.(map[string]any)
	u := []string{}
	if s, ok := m["source"].(string); ok && strings.HasPrefix(s, "http") {
		u = append(u, s)
	}
	if x, ok := m["sources"].(map[string]any); ok {
		for _, v := range x {
			if s, ok := v.(string); ok && strings.HasPrefix(s, "http") {
				u = append(u, s)
			}
		}
	}
	if a, ok := m["sources"].([]any); ok {
		for _, v := range a {
			if s, ok := v.(string); ok && strings.HasPrefix(s, "http") {
				u = append(u, s)
			}
		}
	}
	sort.Strings(u)
	return u
}
func hasUnknownTop(format string, v any) bool {
	m, _ := v.(map[string]any)
	allowed := map[string]bool{}
	keys := map[string][]string{"codex": {"source", "extractedOn", "benchmarkSource", "policy", "models", "efforts", "priorities", "questions", "guidanceLadder", "categories", "verification"}, "claude": {"kind", "extractedOn", "interactiveSelector", "note", "sources", "official", "derivedMapping"}, "opencode": {"kind", "checkedOn", "scope", "evidence", "sources", "models", "recommendationsStatus", "aokIntegration"}, "agy": {"kind", "checkedOn", "scope", "evidence", "sources", "models", "recommendationsStatus", "aokIntegration"}}[format]
	for _, k := range keys {
		allowed[k] = true
	}
	for k := range m {
		if !allowed[k] {
			return true
		}
	}
	return false
}
func hasUnknownNested(format string, raw any) bool {
	root, _ := raw.(map[string]any)
	allowedModel := map[string]bool{}
	for _, k := range []string{"name", "label", "selector", "positioning", "latency", "priceInPerMTok", "priceOutPerMTok", "priceNote", "defaultEffort", "defaultEffortClaudeCode", "contextTokens", "maxOutputTokens", "thinking", "scheduledRemoval", "benchmarkReproduced", "verification", "source", "offer", "effort", "effortNote", "priceGuarantee"} {
		allowedModel[k] = true
	}
	checkRows := func(x any) bool {
		rows, ok := x.([]any)
		if !ok {
			return false
		}
		for _, row := range rows {
			obj, ok := row.(map[string]any)
			if !ok {
				continue
			}
			for k := range obj {
				if !allowedModel[k] {
					return true
				}
			}
		}
		return false
	}
	switch format {
	case "claude":
		official, _ := root["official"].(map[string]any)
		allowedSections := map[string]bool{"models": true, "selectionMatrix": true, "effortLevels": true, "effortPersistence": true, "escalateToFableWhen": true, "startWith": true, "blogRules": true, "claudeCodeAliases": true, "claudeCodeEffortGuidance": true}
		for key, value := range official {
			if !allowedSections[key] {
				return true
			}
			if key == "models" {
				models, _ := value.(map[string]any)
				for _, item := range models {
					obj, ok := item.(map[string]any)
					if !ok {
						continue
					}
					for field := range obj {
						if !allowedModel[field] {
							return true
						}
					}
				}
			}
		}
	case "opencode", "agy":
		return checkRows(root["models"])
	}
	return false
}
func validateSourceShape(format string, raw any) error {
	root, ok := raw.(map[string]any)
	if !ok {
		return fmt.Errorf("%s artifact must be an object", format)
	}
	switch format {
	case "codex":
		models, ok := root["models"].(map[string]any)
		if !ok || len(models) == 0 {
			return fmt.Errorf("Codex models must be a nonempty object")
		}
		for id, name := range models {
			label, ok := name.(string)
			if id == "" || !ok || strings.TrimSpace(label) == "" {
				return fmt.Errorf("Codex model %q must have a nonempty name", id)
			}
		}
	case "claude":
		official, ok := root["official"].(map[string]any)
		if !ok {
			return fmt.Errorf("Claude artifact is missing official section")
		}
		models, ok := official["models"].(map[string]any)
		if !ok || len(models) == 0 {
			return fmt.Errorf("Claude official models must be a nonempty object")
		}
		for id, value := range models {
			obj, ok := value.(map[string]any)
			if !ok {
				return fmt.Errorf("Claude model %s must be an object", id)
			}
			label, ok := obj["name"].(string)
			if !ok || strings.TrimSpace(label) == "" {
				return fmt.Errorf("Claude model %s must have a nonempty name", id)
			}
		}
	case "opencode", "agy":
		rows, ok := root["models"].([]any)
		if !ok || len(rows) == 0 {
			return fmt.Errorf("%s models must be a nonempty array", format)
		}
		for i, row := range rows {
			obj, ok := row.(map[string]any)
			if !ok {
				return fmt.Errorf("%s model %d must be an object", format, i)
			}
			selector, sok := obj["selector"].(string)
			label, lok := obj["label"].(string)
			if !sok || strings.TrimSpace(selector) == "" {
				return fmt.Errorf("%s model %d requires a selector", format, i)
			}
			if !lok || strings.TrimSpace(label) == "" {
				return fmt.Errorf("%s model %d requires a label", format, i)
			}
		}
	}
	return nil
}
func withinCaptureBudget(files map[string][]byte) bool {
	if len(files) > 512 {
		return false
	}
	n := 0
	for _, b := range files {
		n += len(b)
		if n > 64*1024*1024 {
			return false
		}
	}
	return true
}
func categoryFor(format, dimension string) string {
	if dimension == "listed" || dimension == "listedSelector" || dimension == "listedEffort" || dimension == "listedThinking" {
		return Listed
	}
	if format == "codex" {
		return Derived
	}
	return Published
}
func expiryFor(category, dimension, date string, validity map[string]int) string {
	t, e := time.Parse(time.RFC3339, date)
	if e != nil {
		return ""
	}
	hours := DefaultValidityHours()
	for k, v := range validity {
		hours[k] = v
	}
	ttlKey := category
	if strings.HasPrefix(dimension, "price.") || dimension == "offer" {
		ttlKey = "pricing"
	}
	h := hours[ttlKey]
	if h <= 0 {
		h = DefaultValidityHours()[ttlKey]
	}
	return t.Add(time.Duration(h) * time.Hour).Format(time.RFC3339)
}
func rawJSON(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
func sortedKeys(m map[string]any) []string {
	k := make([]string, 0, len(m))
	for x := range m {
		k = append(k, x)
	}
	sort.Strings(k)
	return k
}
func escapePointer(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "~", "~0"), "/", "~1")
}
func appendConfig(xs []ConfigurationRecord, x ConfigurationRecord) []ConfigurationRecord {
	for _, a := range xs {
		if a.ID == x.ID {
			return xs
		}
	}
	return append(xs, x)
}
func ruleFor(p Policy, id string) (SourceRule, bool) {
	for _, x := range p.Sources {
		if x.ID == id {
			return x, true
		}
	}
	return SourceRule{}, false
}
func hasValue(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}
func empty(s *string) bool { return s == nil || strings.TrimSpace(*s) == "" }
func requiredCategories(format string) []string {
	switch format {
	case "codex":
		return []string{Derived}
	case "claude":
		return []string{Published, Derived}
	case "opencode", "agy":
		return []string{Published, Listed, Derived}
	case "probe":
		return []string{Invoked}
	default:
		return []string{"unsupported-format"}
	}
}
func sourceIndex(xs []SourceInput, id string) int {
	for i, x := range xs {
		if x.ID == id {
			return i
		}
	}
	return 0
}
func safeID(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, s)
}
func addCaptured(files map[string][]byte, seen map[string]bool, path string, b []byte) {
	if !seen[path] {
		files[path] = append([]byte(nil), b...)
		seen[path] = true
	}
}
func overCaptureBudget(files map[string][]byte, additional int) bool {
	n := additional
	for _, b := range files {
		n += len(b)
	}
	return n > 64*1024*1024
}
func diagnostic(r Report, field string, e error) Report {
	r.Status = "invalid"
	r.Diagnostics = append(r.Diagnostics, contracts.Diagnostic{Code: "modelcatalog.invalid", Severity: "error", Message: field + ": " + e.Error()})
	return r
}

func warning(code, message string) contracts.Diagnostic {
	return contracts.Diagnostic{Code: code, Severity: "warning", Message: message}
}
