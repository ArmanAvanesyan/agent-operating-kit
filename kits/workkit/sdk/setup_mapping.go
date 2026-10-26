package sdk

import (
	"context"
	"encoding/json"
	"errors"
	s "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/setupv1"
	api "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/v1alpha1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/engine"
	check "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/setup"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/readsource"
	"math/big"
	"sort"
	"time"
)

func EvaluateMapping(mappingRaw, contextRaw []byte, source, dest s.Observation, asOf string) (s.Report, error) {
	r := check.NewReport("MappingEvaluation", asOf)
	if _, e := time.Parse(time.RFC3339Nano, asOf); e != nil {
		return r, e
	}
	r.Inputs = map[string]string{"mapping": SHA256(mappingRaw), "context": SHA256(contextRaw), "source": check.Hash(source), "destination": check.Hash(dest)}
	if check.Validate(source) != nil || check.Validate(dest) != nil {
		return r, errors.New("invalid mapping observations")
	}
	var m api.Mapping
	var c s.MappingContext
	if e := setupDocument(mappingRaw, "Mapping", &m); e != nil {
		return r, e
	}
	if DecodeRead(contextRaw, &c) != nil || c.Version != s.Version || c.Kind != "MappingContext" || c.MappingSHA256 != SHA256(mappingRaw) || c.SourceSHA256 != check.Hash(source) || c.DestinationSHA256 != check.Hash(dest) {
		return r, errors.New("mapping context digest mismatch")
	}
	sourceParent := c.SourceContainerID
	if sourceParent == "" {
		sourceParent = source.Scope.ContainerID
	}
	destParent := c.DestinationContainerID
	if destParent == "" {
		destParent = dest.Scope.ContainerID
	}
	for _, entry := range []struct {
		o  s.Observation
		id string
	}{{source, sourceParent}, {dest, destParent}} {
		container, ok := check.Find(entry.o, "container", entry.id)
		if !ok || entry.id != entry.o.Scope.ContainerID && container.ParentID != entry.o.Scope.ContainerID {
			check.Block(&r, "mapping.scope", entry.id, "selected metadata parent outside observed connection container")
		}
	}
	if len(source.Diagnostics) > 0 || len(dest.Diagnostics) > 0 {
		check.Block(&r, "mapping.diagnostics", "metadata", "unresolved metadata diagnostics block mapping conformance")
	}
	check.Fresh(&r, source, asOf, 300)
	check.Fresh(&r, dest, asOf, 300)
	if !check.Category(source, "fields") || !check.Category(dest, "fields") {
		check.Block(&r, "mapping.catalog", "fields", "complete field catalogs required")
	}
	seen := map[string]bool{}
	targets := map[string]bool{}
	for _, f := range m.Spec.Fields {
		if seen[f.Canonical] || targets[f.TargetFieldId] {
			return r, errors.New("duplicate canonical or target field")
		}
		seen[f.Canonical] = true
		targets[f.TargetFieldId] = true
		a, aok := check.Find(source, "field", f.SourceFieldId)
		b, bok := check.Find(dest, "field", f.TargetFieldId)
		if !aok || !bok || a.ParentID != sourceParent || b.ParentID != destParent || a.Type == "unknown" || b.Type == "unknown" {
			check.Block(&r, "mapping.field", f.Canonical, "known exact source and destination fields required")
			continue
		}
		if a.Type == "enum" && len(a.Options) == 0 || b.Type == "enum" && len(b.Options) == 0 {
			check.Block(&r, "mapping.enum-domain", f.Canonical, "complete observed enum domains required")
		}
		if len(f.ValueMap) > 0 && a.Type != "string" && a.Type != "enum" && a.Type != "person" {
			check.Block(&r, "mapping.transform", f.Canonical, "non-string value maps require an unsupported typed transform; numeric identity remains exact")
		}
		if f.LossPolicy == "explicit-transform" {
			check.Block(&r, "mapping.transform", f.Canonical, "executable or unspecified transformations unsupported")
		}
		if a.Type != b.Type && len(f.ValueMap) == 0 {
			check.Block(&r, "mapping.type", f.Canonical, "incompatible identity mapping")
		}
		for key, v := range f.ValueMap {
			validSource := a.Type != "enum"
			for _, option := range a.Options {
				if option.ID == key {
					validSource = true
				}
			}
			validTarget := b.Type != "enum"
			var targetID string
			_ = json.Unmarshal(v, &targetID)
			for _, option := range b.Options {
				if option.ID == targetID {
					validTarget = true
				}
			}
			if !validSource || !validTarget || !setupValueType(b.Type, v) {
				check.Block(&r, "mapping.value", f.Canonical, "mapped value incompatible with target type")
			}
		}
		if a.Type == "enum" && b.Type == "enum" {
			for _, option := range a.Options {
				target := option.ID
				if raw, ok := f.ValueMap[option.ID]; ok {
					if json.Unmarshal(raw, &target) != nil {
						target = ""
					}
				} else if len(f.ValueMap) > 0 {
					target = ""
				}
				found := false
				for _, destOption := range b.Options {
					if target != "" && destOption.ID == target {
						found = true
					}
				}
				if !found {
					check.Block(&r, "mapping.enum-domain", f.Canonical, "complete source option domain must map to observed destination IDs")
				}
			}
		}
		check.OK(&r, "field:"+f.Canonical)
	}
	for _, v := range c.RequiredFields {
		if !seen[v] {
			check.Block(&r, "mapping.required", v, "required canonical field unmapped")
		}
	}
	people := map[string]bool{}
	personTargets := map[string]bool{}
	for _, p := range m.Spec.PersonMapping {
		sp, a := check.Find(source, "person", p.SourceId)
		dp, b := check.Find(dest, "person", p.TargetId)
		if !check.Category(source, "people") || !check.Category(dest, "people") || !a || !b || sp.ParentID != sourceParent || dp.ParentID != destParent || people[p.SourceId] || personTargets[p.TargetId] {
			check.Block(&r, "mapping.person", p.SourceId, "unique explicit native people in both scopes required")
		}
		people[p.SourceId] = true
		personTargets[p.TargetId] = true
	}
	states := map[string]bool{}
	for _, v := range m.Spec.Lifecycle {
		a, aok := check.Find(source, "state", v.SourceStateId)
		b, bok := check.Find(dest, "state", v.TargetStateId)
		t, tok := check.Find(dest, "transition", v.TargetTransitionId)
		if states[v.SourceStateId] || !check.Category(source, "states") || !check.Category(dest, "states") || !check.Category(dest, "transitions") || !aok || !bok || !tok || t.ToStateID != b.ID || a.ID == "" || a.ParentID != sourceParent || b.ParentID != destParent || t.ParentID != destParent {
			check.Block(&r, "mapping.lifecycle", v.SourceStateId, "observed native states and applicable transition required")
		}
		states[v.SourceStateId] = true
	}
	return r, nil
}
func setupValueType(t string, raw []byte) bool {
	v, e := engine.JSONValue(raw)
	if e != nil {
		return false
	}
	if v == nil {
		return true
	}
	switch t {
	case "string", "enum", "person":
		_, ok := v.(string)
		return ok
	case "integer":
		n, ok := v.(json.Number)
		if !ok {
			return false
		}
		r, ok := new(big.Rat).SetString(string(n))
		return ok && r.IsInt()
	case "number":
		_, ok := v.(json.Number)
		return ok
	case "boolean":
		_, ok := v.(bool)
		return ok
	}
	return false
}
func ValidateSetupMapping(ctx context.Context, mapping, bindings, source, dest, asOf string) (s.Report, error) {
	raw, e := ReadLocal(ctx, mapping)
	if e != nil {
		return s.Report{}, e
	}
	c, e := ReadLocal(ctx, bindings)
	if e != nil {
		return s.Report{}, e
	}
	a, _, e := LoadSetupObservation(ctx, source)
	if e != nil {
		return s.Report{}, e
	}
	b, _, e := LoadSetupObservation(ctx, dest)
	if e != nil {
		return s.Report{}, e
	}
	return EvaluateMapping(raw, c, a, b, asOf)
}

// PreviewSetupMapping uses only the source snapshot and explicit catalogs. It
// never creates a send permit, acceptance receipt or writable runtime proposal.
func PreviewSetupMapping(ctx context.Context, mapping, bindings, source, dest, snapshot, asOf, out string) (s.Preview, error) {
	if e := setupOutput(out, source, dest, snapshot); e != nil {
		return s.Preview{}, e
	}
	p := s.Preview{Values: []s.ProposedValue{}}
	raw, e := ReadLocal(ctx, mapping)
	if e != nil {
		return p, e
	}
	contextRaw, e := ReadLocal(ctx, bindings)
	if e != nil {
		return p, e
	}
	a, sourceFiles, e := LoadSetupObservation(ctx, source)
	if e != nil {
		return p, e
	}
	b, destFiles, e := LoadSetupObservation(ctx, dest)
	if e != nil {
		return p, e
	}
	r, e := EvaluateMapping(raw, contextRaw, a, b, asOf)
	p.Report = r
	if e != nil {
		return p, e
	}
	var m api.Mapping
	if e = setupDocument(raw, "Mapping", &m); e != nil {
		return p, e
	}
	capture, e := LoadReadBundle(ctx, snapshot)
	if e != nil {
		return p, e
	}
	if capture.Digest() != a.CaptureSHA256 || capture.manifest.ConfigurationSHA256 != a.ConfigurationSHA256 || capture.config.Source.PrincipalPseudonym != a.Scope.Principal {
		return p, errors.New("snapshot not bound to source metadata")
	}
	p.Report.Inputs["snapshot"] = capture.Digest()
	var capturedContext s.MappingContext
	if e = DecodeRead(contextRaw, &capturedContext); e != nil {
		return p, e
	}
	sourceParent := capturedContext.SourceContainerID
	if sourceParent == "" {
		sourceParent = a.Scope.ContainerID
	}
	for _, item := range capture.records {
		for _, f := range m.Spec.Fields {
			value, pointers, exists := setupSnapshotValue(item.Data, a.Scope, sourceParent, f.SourceFieldId)
			v := s.ProposedValue{ItemID: item.Identity.NativeID, Canonical: f.Canonical, SourceFieldID: f.SourceFieldId, TargetFieldID: f.TargetFieldId, SourcePointers: pointers, State: "unresolved", Value: json.RawMessage("null"), Provenance: []api.Asset{{Path: "bundle.json", Sha256: api.Sha(capture.Digest())}}}
			if !exists {
				check.Block(&p.Report, "mapping.missing", f.Canonical, "source value missing; never imputed as null")
				p.Values = append(p.Values, v)
				continue
			}
			encoded := engine.Encode(value)
			target, ok := check.Find(b, "field", f.TargetFieldId)
			if f.Owner != "source" {
				v.State = "protected"
				check.Block(&p.Report, "mapping.ownership", f.Canonical, "only source-owned fields produce proposed target values")
			} else if r.Status != "ok" || !ok {
				check.Block(&p.Report, "mapping.invalid", f.Canonical, "invalid mapping cannot yield a resolved value")
			} else {
				var key string
				nativeValue, _ := engine.JSONValue(encoded)
				key, isString := nativeValue.(string)
				mapped, has := f.ValueMap[key]
				has = has && isString
				if len(f.ValueMap) > 0 && !has && string(encoded) != "null" {
					check.Block(&p.Report, "mapping.enum", f.Canonical, "explicit value mapping missing")
				} else {
					if has {
						encoded = append([]byte(nil), mapped...)
					}
					if target.Type == "person" && string(encoded) != "null" {
						found := false
						for _, pm := range m.Spec.PersonMapping {
							if pm.SourceId == key {
								encoded = engine.Encode(pm.TargetId)
								found = true
								break
							}
						}
						if !found {
							check.Block(&p.Report, "mapping.person", f.Canonical, "explicit person correlation missing")
							p.Values = append(p.Values, v)
							continue
						}
					}
					allowed := setupValueType(target.Type, encoded) && (!target.Required || string(encoded) != "null")
					if string(encoded) == "null" {
						if _, known := target.Provenance.Fields["required"]; !known {
							allowed = false
						}
					}
					if target.Type == "enum" && string(encoded) != "null" {
						var id string
						_ = json.Unmarshal(encoded, &id)
						allowed = false
						for _, op := range target.Options {
							if op.ID == id {
								allowed = true
							}
						}
					}
					if allowed {
						v.State = "proposed"
						v.Value = encoded
					} else {
						check.Block(&p.Report, "mapping.loss", f.Canonical, "required value is incompatible or unknown; raw source remains retained")
					}
				}
			}
			p.Values = append(p.Values, v)
		}
		if len(m.Spec.Lifecycle) > 0 {
			check.Block(&p.Report, "mapping.lifecycle-preview", item.Identity.NativeID, "lifecycle changes require item-specific current transition prerequisites and independent acceptance; no transition proposed")
		}
	}
	sort.Slice(p.Values, func(i, j int) bool {
		return p.Values[i].ItemID+"\x00"+p.Values[i].Canonical < p.Values[j].ItemID+"\x00"+p.Values[j].Canonical
	})
	if out != "" {
		files := map[string][]byte{"mapping.json": raw, "mapping-context.json": contextRaw, "source.json": engine.Encode(a), "destination.json": engine.Encode(b)}
		files["snapshot-manifest.json"] = capture.manifestRaw
		for n, b := range capture.capture.Files {
			files["snapshot-native/"+n] = b
		}
		for n, b := range sourceFiles {
			files["source-native/"+n] = b
		}
		for n, b := range destFiles {
			files["destination-native/"+n] = b
		}
		e = writeSetupReport(ctx, out, p, files)
	}
	return p, e
}

func setupSnapshotValue(item readsource.Item, scope s.Scope, parent, field string) (json.RawMessage, []readsource.Pointer, bool) {
	if scope.Provider == "github" && parent != scope.ContainerID {
		key := readsource.Digest(struct {
			ProjectID string
			FieldID   string
		}{parent, field})
		raw, ok := item.ProjectFields[key]
		if !ok {
			return nil, nil, false
		}
		var wrapper readsource.ProjectFieldValue
		if DecodeRead(raw, &wrapper) != nil || wrapper.ProjectID != parent || wrapper.FieldID != field {
			return nil, nil, false
		}
		native, e := engine.JSONValue(wrapper.Value)
		if e != nil {
			return nil, nil, false
		}
		m, ok := native.(map[string]any)
		if !ok {
			return nil, nil, false
		}
		var prop string
		switch m["__typename"] {
		case "ProjectV2ItemFieldSingleSelectValue":
			prop = "optionId"
		case "ProjectV2ItemFieldTextValue":
			prop = "text"
		case "ProjectV2ItemFieldNumberValue":
			prop = "number"
		default:
			return nil, nil, false
		}
		value, present := m[prop]
		if !present {
			return nil, nil, false
		}
		return engine.Encode(value), append([]readsource.Pointer{}, item.Provenance["projectFields."+key]...), true
	}
	if parent != scope.ContainerID {
		return nil, nil, false
	}
	raw, ok := item.Fields[field]
	return append(json.RawMessage(nil), raw...), append([]readsource.Pointer{}, item.Provenance[field]...), ok
}
