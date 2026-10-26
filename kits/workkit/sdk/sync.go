package sdk

import (
	"context"
	"encoding/json"
	"errors"
	o "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/operationsv1"
	r "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/readv1"
	y "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/syncv1"
	api "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/v1alpha1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/engine"
	check "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/setup"
	"path/filepath"
	"sort"
	"time"
)

type SyncRequest struct{ Configuration, State, Source, Destination, SourceObservation, DestinationObservation, Mapping, MappingContext, SourceBinding, DestinationBinding, Query, AsOf, Out string }

func syncCell(v json.RawMessage, exists bool) y.Cell {
	if !exists {
		return y.Cell{Present: false, Value: json.RawMessage("null")}
	}
	return y.Cell{Present: true, Value: append(json.RawMessage{}, v...)}
}
func equalCell(a, b y.Cell) bool {
	return a.Present == b.Present && (!a.Present || sourceSemantic(a.Value) == sourceSemantic(b.Value))
}
func baselineKey(id, field string) string { return check.Hash(struct{ ID, Field string }{id, field}) }

// PreviewSync compares exact identities and a paired baseline. It performs no
// credential lookup, baseline initialization or provider requests.
func PreviewSync(ctx context.Context, q SyncRequest) (y.Plan, error) {
	p := y.Plan{Version: y.Version, Kind: "SyncPlan", AsOf: q.AsOf, Inputs: map[string]string{}, Operations: []o.Draft{}, Proposals: []o.Desired{}, Updates: []y.Baseline{}, Conflicts: []y.Conflict{}, Diagnostics: []api.Diagnostic{}, Status: "ready"}
	if _, err := time.Parse(time.RFC3339Nano, q.AsOf); err != nil {
		return p, err
	}
	if err := setupOutputOptional(q.Out, q.Source, q.Destination, q.SourceObservation, q.DestinationObservation); err != nil {
		return p, err
	}
	files := map[string][]byte{}
	for name, path := range map[string]string{"configuration": q.Configuration, "state": q.State, "mapping": q.Mapping, "mapping-context": q.MappingContext, "source-binding": q.SourceBinding, "destination-binding": q.DestinationBinding, "query": q.Query} {
		b, err := ReadLocal(ctx, path)
		if err != nil {
			return p, err
		}
		files["assets/"+name+".json"] = b
		p.Inputs[name] = SHA256(b)
	}
	var c y.Configuration
	var state y.State
	var m api.Mapping
	var sb, db api.WorkBinding
	if DecodeRead(files["assets/configuration.json"], &c) != nil || DecodeRead(files["assets/state.json"], &state) != nil || setupDocument(files["assets/mapping.json"], "Mapping", &m) != nil || setupDocument(files["assets/source-binding.json"], "WorkBinding", &sb) != nil || setupDocument(files["assets/destination-binding.json"], "WorkBinding", &db) != nil {
		return p, errors.New("invalid closed sync inputs")
	}
	if len(m.Spec.Fields) > 128 || len(c.Correlations)*len(m.Spec.Fields) > 10000 {
		return p, errors.New("sync mapping/correlation cell bound")
	}
	if c.Version != y.Version || c.Kind != "SyncConfiguration" || c.Route.APIVersion != api.Version || c.Route.Kind != "SyncRoute" || c.MaxOperations < 1 || c.MaxOperations > 100 || len(c.Correlations) > 10000 {
		return p, errors.New("invalid route contract/bounds")
	}
	// Preserve domain schema validation as well as typed closed parsing.
	if _, err := Parse(engine.Encode(c.Route), "json"); err != nil {
		return p, err
	}
	p.RouteSHA256 = SyncRouteSHA256(c)
	if state.Version != y.Version || state.Kind != "SyncState" || state.RouteSHA256 != p.RouteSHA256 || state.Revision < 0 || len(state.Baselines) > 10000 || len(state.Unresolved) > 100 {
		return p, errors.New("state/route identity mismatch")
	}
	p.Candidate = cloneRead(state)
	p.Initialize = !state.Initialized
	block := func(code string) {
		p.Status = "blocked"
		p.Diagnostics = append(p.Diagnostics, api.Diagnostic{Code: code, Message: "route requires reviewed evidence or conflict resolution", Field: "route", Severity: "error"})
	}
	if c.Route.Spec.Mode != "one-way" || c.Route.Spec.Trigger != "manual" || c.Route.Spec.ConflictPolicy != "manual" || c.Route.Spec.DeletePolicy != "ignore" || !c.Route.Spec.Enabled {
		block("sync.route-policy")
	}
	if state.Paused {
		block("sync.paused")
	}
	if len(state.Unresolved) > 0 {
		block("sync.unresolved")
	}
	a, af, err := LoadSetupObservation(ctx, q.SourceObservation)
	if err != nil {
		return p, err
	}
	b, bf, err := LoadSetupObservation(ctx, q.DestinationObservation)
	if err != nil {
		return p, err
	}
	source, err := LoadReadBundle(ctx, q.Source)
	if err != nil {
		return p, err
	}
	dest, err := LoadReadBundle(ctx, q.Destination)
	if err != nil {
		return p, err
	}
	if c.SourceObservationSHA256 != check.Hash(a) || c.DestinationObservationSHA256 != check.Hash(b) || c.MappingContextSHA256 != p.Inputs["mapping-context"] || c.SourceBindingSHA256 != p.Inputs["source-binding"] || c.DestinationBindingSHA256 != p.Inputs["destination-binding"] || c.QuerySHA256 != p.Inputs["query"] {
		return p, errors.New("route inputs changed")
	}
	if source.Digest() != a.CaptureSHA256 || dest.Digest() != b.CaptureSHA256 || c.Route.Spec.Source.ConnectionRef != a.ConnectionRef || c.Route.Spec.Destination.ConnectionRef != b.ConnectionRef || c.Route.Spec.MappingRef.Sha256 != api.Sha(p.Inputs["mapping"]) || c.Route.Spec.MappingRef.Id != m.Metadata.Id || c.Route.Spec.MappingRef.Revision != m.Metadata.Revision {
		return p, errors.New("route endpoint/mapping mismatch")
	}
	scopeCheck := func(scope api.Scope, binding api.WorkBinding, obTenant, obContainer string, ref api.Ref) bool {
		has := false
		for _, id := range scope.ContainerIds {
			if id == obContainer {
				has = true
			}
		}
		bh := false
		for _, id := range binding.Spec.Scope.ContainerIds {
			if id == obContainer {
				bh = true
			}
		}
		return has && bh && scope.TenantId == obTenant && binding.Spec.Scope.TenantId == obTenant && binding.Spec.ConnectionRef == ref && binding.Spec.Status == "verified" && binding.Spec.MappingRef == c.Route.Spec.MappingRef
	}
	if !scopeCheck(c.Route.Spec.Source.Scope, sb, a.Scope.TenantID, a.Scope.ContainerID, a.ConnectionRef) || !scopeCheck(c.Route.Spec.Destination.Scope, db, b.Scope.TenantID, b.Scope.ContainerID, b.ConnectionRef) {
		block("sync.binding")
	}
	query, err := LoadReadQuery(ctx, q.Query)
	if err != nil {
		return p, err
	}
	if query.ConnectionID != "" || query.Dialect != c.Route.Spec.Selector.Dialect || query.Expression != c.Route.Spec.Selector.Expression || query.MaxAgeSeconds > 300 || c.Route.Spec.Selector.ConnectionRef != nil && *c.Route.Spec.Selector.ConnectionRef != a.ConnectionRef {
		return p, errors.New("selector mismatch or freshness too broad")
	}
	selected, err := Federate(ctx, []string{q.Source}, query, q.AsOf)
	if err != nil {
		return p, err
	}
	p.Complete = selected.Exhaustive && dest.manifest.Complete
	for _, capture := range []*ReadBundle{source, dest} {
		if capture.manifest.OriginalObservedAt == nil {
			p.Complete = false
			continue
		}
		observed, e := time.Parse(time.RFC3339Nano, *capture.manifest.OriginalObservedAt)
		at, _ := time.Parse(time.RFC3339Nano, q.AsOf)
		if e != nil || at.Before(observed) || at.Sub(observed) >= 5*time.Minute {
			p.Complete = false
		}
	}
	if !p.Complete {
		block("sync.incomplete")
	}
	fresh := check.NewReport("SyncFreshness", q.AsOf)
	check.Fresh(&fresh, b, q.AsOf, 300)
	if fresh.Status != "ok" {
		block("sync.stale")
	}
	if len(source.records)*len(m.Spec.Fields) > 10000 {
		return p, errors.New("sync projection cell bound")
	}
	preview, err := PreviewSetupMapping(ctx, q.Mapping, q.MappingContext, q.SourceObservation, q.DestinationObservation, q.Source, q.AsOf, "")
	if err != nil {
		return p, err
	}
	if preview.Report.Status != "ok" {
		block("sync.mapping")
	}
	sourceRows := map[string]r.Record{}
	destRows := map[string]r.Record{}
	for _, v := range selected.Records {
		sourceRows[check.Hash(v.Identity)] = v
	}
	for _, v := range dest.records {
		destRows[check.Hash(v.Identity)] = v
	}
	baselines := map[string]y.Baseline{}
	for _, v := range state.Baselines {
		key := baselineKey(v.CorrelationID, v.Field)
		if _, ok := baselines[key]; ok {
			return p, errors.New("duplicate baseline")
		}
		baselines[key] = v
	}
	proposed := map[string]map[string]json.RawMessage{}
	for _, v := range preview.Values {
		if v.State == "proposed" {
			if proposed[v.ItemID] == nil {
				proposed[v.ItemID] = map[string]json.RawMessage{}
			}
			proposed[v.ItemID][v.TargetFieldID] = v.Value
		}
	}
	seenIDs, seenSrc, seenDst := map[string]bool{}, map[string]bool{}, map[string]bool{}
	next := []y.Baseline{}
	correlated := map[string]bool{}
	for _, co := range c.Correlations {
		sk, dk := check.Hash(co.Source), check.Hash(co.Destination)
		if co.ID == "" || seenIDs[co.ID] || seenSrc[sk] || seenDst[dk] {
			return p, errors.New("duplicate/ambiguous correlation")
		}
		seenIDs[co.ID] = true
		seenSrc[sk] = true
		seenDst[dk] = true
		sr, sok := sourceRows[sk]
		dr, dok := destRows[dk]
		if !sok || !dok {
			block("sync.correlation-scope")
			continue
		}
		correlated[sk] = true
		fields := map[string]json.RawMessage{}
		for _, f := range m.Spec.Fields {
			sv, _, sexists := setupSnapshotValue(sr.Data, a.Scope, a.Scope.ContainerID, f.SourceFieldId)
			dv, _, dexists := setupSnapshotValue(dr.Data, b.Scope, b.Scope.ContainerID, f.TargetFieldId)
			sc, dc := syncCell(sv, sexists), syncCell(dv, dexists)
			key := baselineKey(co.ID, f.TargetFieldId)
			old, has := baselines[key]
			if !sr.Data.Complete || !dr.Data.Complete || len(sr.Data.LossyFields) > 0 || len(dr.Data.LossyFields) > 0 {
				block("sync.lossy-item")
				continue
			}
			if p.Initialize {
				mapped, ok := proposed[sr.Identity.NativeID][f.TargetFieldId]
				if !ok || !equalCell(syncCell(mapped, true), dc) {
					p.Conflicts = append(p.Conflicts, y.Conflict{CorrelationID: co.ID, Field: f.TargetFieldId, Code: "initial-values-differ", Source: sc, Destination: dc})
					continue
				}
				next = append(next, y.Baseline{CorrelationID: co.ID, Field: f.TargetFieldId, Source: sc, Destination: dc})
				continue
			}
			if !has {
				block("sync.baseline-missing")
				continue
			}
			delete(baselines, key)
			next = append(next, old)
			if !equalCell(dc, old.Destination) {
				p.Conflicts = append(p.Conflicts, y.Conflict{CorrelationID: co.ID, Field: f.TargetFieldId, Code: "destination-divergence", Source: sc, Destination: dc})
				continue
			}
			if equalCell(sc, old.Source) {
				continue
			}
			value, ok := proposed[sr.Identity.NativeID][f.TargetFieldId]
			if !sexists || !ok || f.Owner != "source" {
				block("sync.unresolved-value")
				continue
			}
			fields[f.TargetFieldId] = value
		}
		if len(fields) > 0 {
			want := o.Desired{Version: o.Version, Kind: "DesiredChange", LogicalKey: "sync-" + check.Hash(struct {
				Route, Correlation string
				Revision           int64
				Fields             map[string]json.RawMessage
			}{p.RouteSHA256, co.ID, state.Revision, fields}), Operation: "update-fields", ParentID: b.Scope.ContainerID, ItemID: dr.Identity.NativeID, Fields: fields, DependsOn: []string{}}
			p.Proposals = append(p.Proposals, want)
			for _, f := range m.Spec.Fields {
				if value, ok := fields[f.TargetFieldId]; ok {
					sv, _, exists := setupSnapshotValue(sr.Data, a.Scope, a.Scope.ContainerID, f.SourceFieldId)
					p.Updates = append(p.Updates, y.Baseline{CorrelationID: co.ID, Field: f.TargetFieldId, Source: syncCell(sv, exists), Destination: syncCell(value, true), ConfirmedOperationID: operationID(want.LogicalKey, dr.Identity, want.Operation)})
				}
			}
		}

	}
	if state.Initialized && len(baselines) > 0 {
		block("sync.baseline-orphan")
	}
	for key := range sourceRows {
		if !correlated[key] {
			block("sync.unlinked")
			if c.AllowCreate {
				row := sourceRows[key]
				fields := cloneRead(proposed[row.Identity.NativeID])
				if fields == nil {
					fields = map[string]json.RawMessage{}
				}
				p.Proposals = append(p.Proposals, o.Desired{Version: o.Version, Kind: "DesiredChange", LogicalKey: "sync-create-" + check.Hash(struct {
					Route  string
					Source r.Identity
				}{p.RouteSHA256, row.Identity}), Operation: "create-item", ParentID: b.Scope.ContainerID, Fields: fields, DependsOn: []string{}, AllowCreate: true})
				p.Diagnostics = append(p.Diagnostics, api.Diagnostic{Code: "sync.create-manual", Field: "correlations", Message: "reviewable creation intent requires an explicit qualified type/schema and confirmed native identity before correlation; dispatch blocked", Severity: "warning"})
			}
		}
	}
	if len(p.Conflicts) > 0 {
		p.Status = "conflict"
	}
	if len(p.Proposals) > c.MaxOperations {
		block("sync.operation-bound")
	}
	sort.Slice(p.Proposals, func(i, j int) bool { return p.Proposals[i].LogicalKey < p.Proposals[j].LogicalKey })
	sort.Slice(next, func(i, j int) bool {
		return baselineKey(next[i].CorrelationID, next[i].Field) < baselineKey(next[j].CorrelationID, next[j].Field)
	})
	sort.Slice(p.Conflicts, func(i, j int) bool {
		return baselineKey(p.Conflicts[i].CorrelationID, p.Conflicts[i].Field) < baselineKey(p.Conflicts[j].CorrelationID, p.Conflicts[j].Field)
	})
	if p.Initialize {
		p.Status = "baseline-candidate"
		if len(p.Diagnostics) > 0 || len(p.Conflicts) > 0 {
			p.Status = "blocked"
		}
	}
	p.Candidate.Baselines = next
	p.Inputs["source"] = source.Digest()
	p.Inputs["destination"] = dest.Digest()
	p.Inputs["source-observation"] = check.Hash(a)
	p.Inputs["destination-observation"] = check.Hash(b)
	if q.Out != "" {
		files["assets/source-observation/bundle.json"] = engine.Encode(a)
		for n, v := range af {
			files["assets/source-observation/"+n] = v
		}
		files["assets/destination-observation/bundle.json"] = engine.Encode(b)
		for n, v := range bf {
			files["assets/destination-observation/"+n] = v
		}
		for name, root := range map[string]string{"source": q.Source, "destination": q.Destination} {
			bb := source
			if name == "destination" {
				bb = dest
			}
			files["assets/"+name+"/bundle.json"] = bb.manifestRaw
			for _, asset := range bb.manifest.Assets {
				path, e := ConfinedLocal(root, asset.Path)
				if e != nil {
					return p, e
				}
				raw, e := ReadLocal(ctx, path)
				if e != nil || SHA256(raw) != asset.SHA256 {
					return p, errors.New("capture changed")
				}
				files["assets/"+name+"/"+asset.Path] = raw
			}
		}
		if err = writeOperationalReport(ctx, q.Out, y.Version, p, files); err != nil {
			return p, err
		}
	}
	return p, nil
}

func LoadSyncPlan(ctx context.Context, root string) (y.Plan, error) {
	p, _, err := loadCapturedSyncPlan(ctx, root)
	return p, err
}
func loadCapturedSyncPlan(ctx context.Context, root string) (y.Plan, map[string][]byte, error) {
	files, err := loadOperationalReport(ctx, root, y.Version)
	if err != nil {
		return y.Plan{}, nil, err
	}
	var p y.Plan
	if DecodeRead(files["report.json"], &p) != nil || p.Version != y.Version || p.Kind != "SyncPlan" {
		return p, nil, errors.New("invalid sync plan")
	}
	path := func(n string) string { return filepath.Join(root, "assets", n) }
	rebuilt, err := PreviewSync(ctx, SyncRequest{Configuration: path("configuration.json"), State: path("state.json"), Source: path("source"), Destination: path("destination"), SourceObservation: path("source-observation"), DestinationObservation: path("destination-observation"), Mapping: path("mapping.json"), MappingContext: path("mapping-context.json"), SourceBinding: path("source-binding.json"), DestinationBinding: path("destination-binding.json"), Query: path("query.json"), AsOf: p.AsOf})
	if err != nil || check.Hash(rebuilt) != check.Hash(p) {
		return p, nil, errors.New("sync plan differs from recomputed pinned inputs")
	}
	again, e := loadOperationalReport(ctx, root, y.Version)
	if e != nil || check.Hash(again) != check.Hash(files) {
		return p, nil, errors.New("sync inputs changed during preparation")
	}
	return p, files, nil
}

// SyncRouteSHA256 excludes observation generations, which never rename a route.
func SyncRouteSHA256(c y.Configuration) string {
	return check.Hash(struct {
		Route                                    api.SyncRoute
		SourceBinding, DestinationBinding, Query string
		AllowCreate                              bool
		MaxOperations                            int
	}{c.Route, c.SourceBindingSHA256, c.DestinationBindingSHA256, c.QuerySHA256, c.AllowCreate, c.MaxOperations})
}
