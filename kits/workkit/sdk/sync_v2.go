package sdk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	o "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/operationsv1"
	r "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/readv1"
	s "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/setupv1"
	y "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/syncv1"
	z "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/syncv2"
	api "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/v1alpha1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/engine"
	check "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/setup"
	"path/filepath"
	"sort"
	"time"
)

type SyncV2Request struct{ Configuration, State, Source, Destination, SourceObservation, DestinationObservation, ForwardMapping, ReverseMapping, ForwardContext, ReverseContext, SourceBinding, DestinationBinding, SourceQuery, DestinationQuery, AsOf, Out, Resolution, ResolutionBase string }
type PreparedSyncV2 struct {
	plan  z.Plan
	raw   []byte
	files map[string][]byte
}

func (p PreparedSyncV2) Plan() z.Plan              { return cloneRead(p.plan) }
func (p PreparedSyncV2) Digest() string            { return SHA256(p.raw) }
func (p PreparedSyncV2) Assets() map[string][]byte { return cloneBytes(p.files) }
func SyncV2RouteID(c z.Configuration) string       { return c.ID }
func syncV2Block(p *z.Plan, code string) {
	p.Status = "blocked"
	p.Diagnostics = append(p.Diagnostics, api.Diagnostic{Code: code, Field: "route", Message: "complete exact directional evidence, mapping and ownership required", Severity: "error"})
}
func syncV2Paths(q SyncV2Request) map[string]string {
	return map[string]string{"configuration": q.Configuration, "state": q.State, "forward-mapping": q.ForwardMapping, "reverse-mapping": q.ReverseMapping, "forward-context": q.ForwardContext, "reverse-context": q.ReverseContext, "source-binding": q.SourceBinding, "destination-binding": q.DestinationBinding, "source-query": q.SourceQuery, "destination-query": q.DestinationQuery}
}
func PreviewSyncV2(ctx context.Context, q SyncV2Request) (z.Plan, error) {
	p := z.Plan{ImplementationSHA256: NativeImplementationSHA256(), AdapterVersion: "1.0.0-directional-offline", Version: z.Version, Kind: "DirectionalSyncPlan", AsOf: q.AsOf, Status: "ready", Complete: true, Inputs: map[string]string{}, Changes: []z.Change{}, Conflicts: []z.Conflict{}, Advances: []z.Baseline{}, Diagnostics: []api.Diagnostic{}}
	now, e := nativeTime(q.AsOf)
	if e != nil {
		return p, e
	}
	if e = setupOutputOptional(q.Out, q.Source, q.Destination, q.SourceObservation, q.DestinationObservation); e != nil {
		return p, e
	}
	files := map[string][]byte{}
	for key, path := range syncV2Paths(q) {
		raw, e := ReadLocal(ctx, path)
		if e != nil {
			return p, e
		}
		files["assets/"+key+".json"] = raw
		p.Inputs[key] = SHA256(raw)
	}
	var c z.Configuration
	var state z.State
	var forward, reverse api.Mapping
	var sb, db api.WorkBinding
	for key, target := range map[string]any{"configuration": &c, "state": &state} {
		if e := DecodeRead(files["assets/"+key+".json"], target); e != nil {
			return p, e
		}
	}
	for _, v := range []struct {
		key, kind string
		target    any
	}{{"forward-mapping", "Mapping", &forward}, {"reverse-mapping", "Mapping", &reverse}, {"source-binding", "WorkBinding", &sb}, {"destination-binding", "WorkBinding", &db}} {
		if e := setupDocument(files["assets/"+v.key+".json"], v.kind, v.target); e != nil {
			return p, e
		}
	}
	if c.Version != z.Version || c.Kind != "DirectionalSyncConfiguration" || c.ID == "" || c.Revision < 1 || len(c.Rules) == 0 || len(c.Rules) > 128 || len(c.Correlations) > 10000 || len(c.Rules)*len(c.Correlations) > 10000 {
		return p, errors.New("invalid directional route contract")
	}
	p.RouteID = c.ID
	p.ConfigurationSHA256 = p.Inputs["configuration"]
	p.StateSHA256 = p.Inputs["state"]
	if state.Version != z.Version || state.Kind != "DirectionalSyncState" || state.RouteID != c.ID || state.ConfigurationSHA256 != p.ConfigurationSHA256 || state.Revision < 0 || len(state.Baselines) > 10000 || len(state.Unresolved) > 100 {
		return p, errors.New("state/configuration revision mismatch; no implicit reinitialization")
	}
	p.Candidate = cloneRead(state)
	if state.Paused || len(state.Unresolved) > 0 || state.PendingPlanSHA256 != "" {
		syncV2Block(&p, "sync.v2.pending-or-paused")
	}
	if !state.Initialized && (state.Revision != 0 || len(state.Baselines) > 0 || state.CompletedWatermark != "") {
		return p, errors.New("initial state must be empty")
	}
	for key, want := range map[string]string{"forward-mapping": c.ForwardMappingSHA256, "reverse-mapping": c.ReverseMappingSHA256, "source-binding": c.SourceBindingSHA256, "destination-binding": c.DestinationBindingSHA256, "source-query": c.SourceQuerySHA256, "destination-query": c.DestinationQuerySHA256} {
		if !nativeDigest(want) || p.Inputs[key] != want {
			return p, errors.New("directional configuration input mismatch")
		}
	}
	sources := []string{q.Source, q.Destination}
	metas := []string{q.SourceObservation, q.DestinationObservation}
	captures := make([]*ReadBundle, 2)
	metadata := make([]s.Observation, 2)
	rows := make([]map[string]r.Record, 2)
	for index, key := range []string{"source", "destination"} {
		b, e := LoadReadBundle(ctx, sources[index])
		if e != nil {
			return p, e
		}
		captures[index] = b
		p.Inputs[key] = b.Digest()
		m, mf, e := LoadSetupObservation(ctx, metas[index])
		if e != nil {
			return p, e
		}
		metadata[index] = m
		p.Inputs[key+"-observation"] = check.Hash(m)
		expected := c.SourceConfigurationSHA256
		binding := sb
		mappingRaw := files["assets/reverse-mapping.json"]
		mapping := reverse
		queryPath := q.SourceQuery
		if index == 1 {
			expected = c.DestinationConfigurationSHA256
			binding = db
			mapping = forward
			mappingRaw = files["assets/forward-mapping.json"]
			queryPath = q.DestinationQuery
		}
		if b.manifest.ConfigurationSHA256 != expected || m.ConfigurationSHA256 != expected || m.CaptureSHA256 != b.Digest() || binding.Spec.ConnectionRef != m.ConnectionRef || binding.Spec.MappingRef.Sha256 != api.Sha(SHA256(mappingRaw)) || binding.Spec.MappingRef.Id != mapping.Metadata.Id || binding.Spec.MappingRef.Revision != mapping.Metadata.Revision || binding.Spec.Scope.TenantId != m.Scope.TenantID || len(binding.Spec.Scope.ContainerIds) != 1 || binding.Spec.Scope.ContainerIds[0] != m.Scope.ContainerID {
			return p, errors.New("directional endpoint/binding mismatch")
		}
		if binding.Spec.Status != "verified" {
			syncV2Block(&p, "sync.v2.binding-unverified")
		}
		query, e := LoadReadQuery(ctx, queryPath)
		if e != nil {
			return p, e
		}
		if query.MaxAgeSeconds > 300 {
			return p, errors.New("directional query freshness too broad")
		}
		selected, e := Federate(ctx, []string{sources[index]}, query, q.AsOf)
		if e != nil {
			return p, e
		}
		if !selected.Exhaustive {
			p.Complete = false
			syncV2Block(&p, "sync.v2.incomplete")
		}
		rows[index] = map[string]r.Record{}
		for _, record := range selected.Records {
			rows[index][check.Hash(record.Identity)] = record
		}
		at, e := nativeTime(m.ObservedAt)
		if e != nil || now.Before(at) || now.Sub(at) >= 5*time.Minute {
			p.Complete = false
			syncV2Block(&p, "sync.v2.stale")
		}
		files["assets/"+key+"/bundle.json"] = b.manifestRaw
		for _, asset := range b.manifest.Assets {
			path, e := ConfinedLocal(sources[index], asset.Path)
			if e != nil {
				return p, e
			}
			raw, e := ReadLocal(ctx, path)
			if e != nil || SHA256(raw) != asset.SHA256 {
				return p, errors.New("capture drift during planning")
			}
			files["assets/"+key+"/"+asset.Path] = raw
		}
		files["assets/"+key+"-observation/bundle.json"] = engine.Encode(m)
		for name, raw := range mf {
			files["assets/"+key+"-observation/"+name] = raw
		}
	}
	fwd, e := PreviewSetupMapping(ctx, q.ForwardMapping, q.ForwardContext, q.SourceObservation, q.DestinationObservation, q.Source, q.AsOf, "")
	if e != nil {
		return p, e
	}
	rev, e := PreviewSetupMapping(ctx, q.ReverseMapping, q.ReverseContext, q.DestinationObservation, q.SourceObservation, q.Destination, q.AsOf, "")
	if e != nil {
		return p, e
	}
	if fwd.Report.Status != "ok" || rev.Report.Status != "ok" {
		syncV2Block(&p, "sync.v2.mapping")
	}
	values := func(v s.Preview) map[string]map[string]json.RawMessage {
		out := map[string]map[string]json.RawMessage{}
		for _, value := range v.Values {
			if value.State == "proposed" {
				if out[value.ItemID] == nil {
					out[value.ItemID] = map[string]json.RawMessage{}
				}
				out[value.ItemID][value.TargetFieldID] = value.Value
			}
		}
		return out
	}
	fv, rv := values(fwd), values(rev)
	rules := map[string]bool{}
	sourceFields, destinationFields := map[string]bool{}, map[string]bool{}
	for _, rule := range c.Rules {
		if rule.ID == "" || rules[rule.ID] || sourceFields[rule.SourceField] || destinationFields[rule.DestinationField] {
			return p, errors.New("duplicate directional rule/cell")
		}
		rules[rule.ID] = true
		sourceFields[rule.SourceField] = true
		destinationFields[rule.DestinationField] = true
		a, b := false, false
		for _, field := range forward.Spec.Fields {
			a = a || field.SourceFieldId == rule.SourceField && field.TargetFieldId == rule.DestinationField
		}
		for _, field := range reverse.Spec.Fields {
			b = b || field.SourceFieldId == rule.DestinationField && field.TargetFieldId == rule.SourceField
		}
		if !a || !b {
			return p, errors.New("explicit forward and reverse field mappings required")
		}
		if rule.Owner != "source" && rule.Owner != "destination" && rule.Owner != "shared" && rule.Owner != "human" && rule.Owner != "runtime" {
			return p, errors.New("unknown directional owner")
		}
		if rule.Category != "ordinary" {
			syncV2Block(&p, "sync.v2.manual-typed-or-completion-operation")
		}
	}
	bases := map[string]z.Baseline{}
	for _, b := range state.Baselines {
		key := baselineKey(b.CorrelationID, b.RuleID)
		if _, ok := bases[key]; ok {
			return p, errors.New("duplicate directional baseline")
		}
		bases[key] = b
	}
	inputsSHA := check.Hash(p.Inputs)
	seenCo, seenS, seenD := map[string]bool{}, map[string]bool{}, map[string]bool{}
	conflict := func(co zBaselineScope, rule z.Rule, base z.Baseline, sc, dc y.Cell, code string) {
		value := z.Conflict{CorrelationID: co.ID, RuleID: rule.ID, Code: code, BaselineSHA256: check.Hash(base), InputsSHA256: inputsSHA, Source: sc, Destination: dc}
		value.ID = "conflict-" + check.Hash(value)
		p.Conflicts = append(p.Conflicts, value)
	}
	for _, co := range c.Correlations {
		sk, dk := check.Hash(co.Source), check.Hash(co.Destination)
		if co.ID == "" || seenCo[co.ID] || seenS[sk] || seenD[dk] {
			return p, errors.New("ambiguous directional correlation")
		}
		seenCo[co.ID] = true
		seenS[sk] = true
		seenD[dk] = true
		sr, sok := rows[0][sk]
		dr, dok := rows[1][dk]
		if !sok || !dok {
			syncV2Block(&p, "sync.v2.unlinked")
			continue
		}
		if !sr.Data.Complete || !dr.Data.Complete || len(sr.Data.LossyFields) > 0 || len(dr.Data.LossyFields) > 0 {
			syncV2Block(&p, "sync.v2.lossy")
			continue
		}
		for _, rule := range c.Rules {
			sv, _, se := setupSnapshotValue(sr.Data, metadata[0].Scope, metadata[0].Scope.ContainerID, rule.SourceField)
			dv, _, de := setupSnapshotValue(dr.Data, metadata[1].Scope, metadata[1].Scope.ContainerID, rule.DestinationField)
			sc, dc := syncCell(sv, se), syncCell(dv, de)
			key := baselineKey(co.ID, rule.ID)
			base, has := bases[key]
			delete(bases, key)
			fw, fok := fv[sr.Identity.NativeID][rule.DestinationField]
			bw, bok := rv[dr.Identity.NativeID][rule.SourceField]
			equivalent := se && de && fok && bok && equalCell(syncCell(fw, true), dc) && equalCell(syncCell(bw, true), sc)
			if !state.Initialized {
				if !equivalent {
					conflict(zBaselineScope{co.ID}, rule, base, sc, dc, "initial-values-differ")
				} else {
					p.Advances = append(p.Advances, z.Baseline{CorrelationID: co.ID, RuleID: rule.ID, Source: sc, Destination: dc, ConfirmedOperations: []string{}})
				}
				continue
			}
			if !has {
				syncV2Block(&p, "sync.v2.baseline-missing")
				continue
			}
			sChanged, dChanged := !equalCell(sc, base.Source), !equalCell(dc, base.Destination)
			if !sChanged && !dChanged {
				continue
			}
			if !se || !de || !fok || !bok {
				conflict(zBaselineScope{co.ID}, rule, base, sc, dc, "unresolved-or-missing-value")
				continue
			}
			if equivalent {
				p.Advances = append(p.Advances, z.Baseline{CorrelationID: co.ID, RuleID: rule.ID, Source: sc, Destination: dc, ConfirmedOperations: []string{}})
				continue
			}
			direction := ""
			if sChanged && !dChanged && (rule.Owner == "source" || rule.Owner == "shared") {
				direction = "forward"
			}
			if dChanged && !sChanged && (rule.Owner == "destination" || rule.Owner == "shared") {
				direction = "reverse"
			}
			if direction == "" {
				conflict(zBaselineScope{co.ID}, rule, base, sc, dc, "divergent-or-protected-change")
				continue
			}
			if rule.Category != "ordinary" {
				continue
			}
			target := dr.Identity
			value := fw
			field := rule.DestinationField
			next := z.Baseline{CorrelationID: co.ID, RuleID: rule.ID, Source: sc, Destination: syncCell(fw, true), ConfirmedOperations: []string{}}
			if direction == "reverse" {
				target = sr.Identity
				value = bw
				field = rule.SourceField
				next.Source = syncCell(bw, true)
				next.Destination = dc
			}
			logical := "sync-v2-" + check.Hash(struct {
				Route, Configuration, Inputs, Correlation, Rule, Direction string
				Value                                                      json.RawMessage
			}{c.ID, p.ConfigurationSHA256, inputsSHA, co.ID, rule.ID, direction, value})
			desired := o.Desired{Version: o.Version, Kind: "DesiredChange", LogicalKey: logical, Operation: "update-fields", ParentID: target.ContainerID, ItemID: target.NativeID, Fields: map[string]json.RawMessage{field: value}, DependsOn: []string{}}
			p.Changes = append(p.Changes, z.Change{Direction: direction, CorrelationID: co.ID, RuleID: rule.ID, Target: target, Desired: desired, Next: next})
		}
	}
	if len(bases) > 0 {
		syncV2Block(&p, "sync.v2.baseline-orphan")
	}
	if len(seenS) != len(rows[0]) || len(seenD) != len(rows[1]) {
		syncV2Block(&p, "sync.v2.unlinked")
	}
	if len(p.Changes) > 100 {
		syncV2Block(&p, "sync.v2.operation-bound")
	}
	sort.Slice(p.Changes, func(i, j int) bool { return check.Hash(p.Changes[i]) < check.Hash(p.Changes[j]) })
	sort.Slice(p.Advances, func(i, j int) bool {
		return baselineKey(p.Advances[i].CorrelationID, p.Advances[i].RuleID) < baselineKey(p.Advances[j].CorrelationID, p.Advances[j].RuleID)
	})
	sort.Slice(p.Conflicts, func(i, j int) bool { return p.Conflicts[i].ID < p.Conflicts[j].ID })
	if len(p.Conflicts) > 0 && p.Status != "blocked" {
		p.Status = "conflict"
	}
	if !state.Initialized && p.Status == "ready" {
		p.Status = "baseline-candidate"
	}
	if q.Resolution != "" {
		raw, err := ReadLocal(ctx, q.Resolution)
		if err != nil {
			return p, err
		}
		baseRaw, err := ReadLocal(ctx, q.ResolutionBase)
		if err != nil {
			return p, err
		}
		var original z.Plan
		if DecodeRead(baseRaw, &original) != nil {
			return p, errors.New("resolution base plan required")
		}
		baseQ := q
		baseQ.Resolution = ""
		baseQ.ResolutionBase = ""
		baseQ.Out = ""
		baseQ.AsOf = original.AsOf
		rebuilt, err := PreviewSyncV2(ctx, baseQ)
		if err != nil || !bytes.Equal(engine.Encode(rebuilt), baseRaw) {
			return p, errors.New("resolution base does not reproduce from original observations")
		}
		if err = applySyncV2Resolution(&p, c, raw, baseRaw, fv, rv); err != nil {
			return p, err
		}
		files["assets/resolution.json"] = raw
		files["assets/resolution-base.json"] = baseRaw
	}
	if q.Out != "" {
		if e = writeOperationalReport(ctx, q.Out, z.Version, p, files); e != nil {
			return p, e
		}
	}
	return p, nil
}

type zBaselineScope struct{ ID string }

func PrepareSyncV2(ctx context.Context, root string) (PreparedSyncV2, error) {
	files, e := loadOperationalReport(ctx, root, z.Version)
	if e != nil {
		return PreparedSyncV2{}, e
	}
	var p z.Plan
	if e = DecodeRead(files["report.json"], &p); e != nil {
		return PreparedSyncV2{}, e
	}
	q := syncV2RequestFromAssets(root, p.AsOf)
	if _, ok := files["assets/resolution.json"]; ok {
		q.Resolution = filepath.Join(root, "assets/resolution.json")
		q.ResolutionBase = filepath.Join(root, "assets/resolution-base.json")
	}
	expected, e := PreviewSyncV2(ctx, q)
	if e != nil || !bytes.Equal(engine.Encode(p), engine.Encode(expected)) {
		return PreparedSyncV2{}, errors.New("directional plan does not reproduce from pinned evidence")
	}
	if p.AuthorizationGranted || p.ExecutionReady || p.MigrationActivated || p.ProductionActivated {
		return PreparedSyncV2{}, errors.New("draft cannot grant authority")
	}
	return PreparedSyncV2{p, files["report.json"], files}, nil
}
func syncV2RequestFromAssets(root, asof string) SyncV2Request {
	a := func(name string) string { return filepath.Join(root, "assets", name+".json") }
	d := func(name string) string { return filepath.Join(root, "assets", name) }
	return SyncV2Request{Configuration: a("configuration"), State: a("state"), ForwardMapping: a("forward-mapping"), ReverseMapping: a("reverse-mapping"), ForwardContext: a("forward-context"), ReverseContext: a("reverse-context"), SourceBinding: a("source-binding"), DestinationBinding: a("destination-binding"), SourceQuery: a("source-query"), DestinationQuery: a("destination-query"), Source: d("source"), Destination: d("destination"), SourceObservation: d("source-observation"), DestinationObservation: d("destination-observation"), AsOf: asof}
}

// A resolution remains a proposal. It cannot waive route ownership or provider
// concurrency/acceptance requirements. Changed observations invalidate its IDs.
func ResolveSyncV2(ctx context.Context, root, resolution, asof, out string) (z.ResolutionPreview, error) {
	p, e := PrepareSyncV2(ctx, root)
	if e != nil {
		return z.ResolutionPreview{}, e
	}
	raw, e := ReadLocal(ctx, resolution)
	if e != nil {
		return z.ResolutionPreview{}, e
	}
	var choices z.Resolution
	if DecodeRead(raw, &choices) != nil || choices.PlanSHA256 != p.Digest() {
		return z.ResolutionPreview{}, errors.New("exact original conflict plan required")
	}
	q := syncV2RequestFromAssets(root, asof)
	q.Resolution = resolution
	q.ResolutionBase = filepath.Join(root, "report.json")
	q.Out = out
	resolved, e := PreviewSyncV2(ctx, q)
	return z.ResolutionPreview{Version: z.Version, Kind: "ResolutionPreview", OriginalPlanSHA256: p.Digest(), ResolutionSHA256: SHA256(raw), Plan: resolved}, e
}
func applySyncV2Resolution(p *z.Plan, c z.Configuration, raw, baseRaw []byte, fv, rv map[string]map[string]json.RawMessage) error {
	var resolution z.Resolution
	if DecodeRead(raw, &resolution) != nil || resolution.Version != z.Version || resolution.Kind != "ConflictResolution" || resolution.PlanSHA256 != SHA256(baseRaw) || len(resolution.Choices) > 10000 {
		return errors.New("invalid exact conflict resolution")
	}
	choices := map[string]string{}
	for _, choice := range resolution.Choices {
		if _, ok := choices[choice.ConflictID]; ok || choice.Choice != "source" && choice.Choice != "destination" && choice.Choice != "unresolved" {
			return errors.New("duplicate or unsupported resolution choice")
		}
		choices[choice.ConflictID] = choice.Choice
	}
	p.Inputs["resolution"] = SHA256(raw)
	p.Inputs["resolution-base"] = SHA256(baseRaw)
	remaining := []z.Conflict{}
	for _, conflict := range p.Conflicts {
		choice, ok := choices[conflict.ID]
		delete(choices, conflict.ID)
		if !ok || choice == "unresolved" {
			remaining = append(remaining, conflict)
			continue
		}
		var rule z.Rule
		var co y.Correlation
		for _, r := range c.Rules {
			if r.ID == conflict.RuleID {
				rule = r
			}
		}
		for _, r := range c.Correlations {
			if r.ID == conflict.CorrelationID {
				co = r
			}
		}
		allowed := rule.Owner == "shared" || choice == "source" && rule.Owner == "source" || choice == "destination" && rule.Owner == "destination"
		if !p.Candidate.Initialized || !allowed || rule.Category != "ordinary" || !conflict.Source.Present || !conflict.Destination.Present {
			remaining = append(remaining, conflict)
			syncV2Block(p, "sync.v2.resolution-hard-gate")
			continue
		}
		direction, target, field, value := "forward", co.Destination, rule.DestinationField, fv[co.Source.NativeID][rule.DestinationField]
		next := z.Baseline{CorrelationID: co.ID, RuleID: rule.ID, Source: conflict.Source, Destination: syncCell(value, value != nil), ConfirmedOperations: []string{}}
		if choice == "destination" {
			direction, target, field, value = "reverse", co.Source, rule.SourceField, rv[co.Destination.NativeID][rule.SourceField]
			next.Source = syncCell(value, value != nil)
			next.Destination = conflict.Destination
		}
		if value == nil {
			remaining = append(remaining, conflict)
			syncV2Block(p, "sync.v2.resolution-unmapped")
			continue
		}
		logical := "sync-v2-resolution-" + check.Hash(struct{ Route, Resolution, Conflict, Direction string }{c.ID, SHA256(raw), conflict.ID, direction})
		want := o.Desired{Version: o.Version, Kind: "DesiredChange", LogicalKey: logical, Operation: "update-fields", ParentID: target.ContainerID, ItemID: target.NativeID, Fields: map[string]json.RawMessage{field: value}, DependsOn: []string{}}
		p.Changes = append(p.Changes, z.Change{Direction: direction, CorrelationID: co.ID, RuleID: rule.ID, Target: target, Desired: want, Next: next})
	}
	if len(choices) > 0 {
		return errors.New("unknown or stale conflict choice")
	}
	p.Conflicts = remaining
	if p.Status != "blocked" {
		p.Status = "ready"
		if len(remaining) > 0 {
			p.Status = "conflict"
		}
	}
	sort.Slice(p.Changes, func(a, b int) bool { return check.Hash(p.Changes[a]) < check.Hash(p.Changes[b]) })
	return nil
}
func SyncV2Exit(p z.Plan) int {
	switch p.Status {
	case "blocked", "baseline-candidate":
		return 3
	case "conflict":
		return 4
	}
	return 0
}
