package sdk

import (
	"context"
	"encoding/json"
	"errors"
	r "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/readv1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/engine"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/query"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/readsource"
	"math/big"
	"sort"
	"time"
)

func LoadReadQuery(ctx context.Context, path string) (r.Query, error) {
	var q r.Query
	b, e := ReadLocal(ctx, path)
	if e != nil {
		return q, e
	}
	e = DecodeRead(b, &q)
	if e == nil && q.Dialect != "portable" {
		return q, failure("query.unsupported", "provider-native query execution is not implemented", 3, nil)
	}
	if e == nil {
		_, e = query.Parse(q)
	}
	return q, e
}
func Federate(ctx context.Context, roots []string, q r.Query, asOf string) (r.QueryResult, error) {
	result := r.QueryResult{Records: []r.Record{}, Coverage: []r.Coverage{}, Exhaustive: true, Filtering: "bounded-local", LiveQualification: "not-established"}
	at, e := time.Parse(time.RFC3339Nano, asOf)
	if e != nil {
		return result, readFailure(e)
	}
	predicate, e := query.Parse(q)
	if e != nil {
		return result, readFailure(e)
	}
	if len(roots) == 0 || len(roots) > 16 {
		return result, readFailure(errors.New("federation input bound"))
	}
	seen := map[string]string{}
	bytesRead, items := 0, 0
	for _, root := range roots {
		if e = ctx.Err(); e != nil {
			return result, e
		}
		b, e := LoadReadBundle(ctx, root)
		if e != nil {
			return result, e
		}
		if digest, ok := seen[b.manifest.ConnectionID]; ok {
			if digest != b.digest {
				return result, failure("read.conflict", "multiple snapshots for one connection require explicit resolution", 4, nil)
			}
			continue
		}
		seen[b.manifest.ConnectionID] = b.digest
		bytesRead += b.bytes
		items += len(b.records)
		if bytesRead > engine.MaxTotalBytes || items > 10000 {
			return result, readFailure(errors.New("federation aggregate budget"))
		}
		coverage := r.Coverage{ConnectionID: b.manifest.ConnectionID, BundleSHA256: b.digest, Principal: b.config.Source.PrincipalPseudonym, Complete: b.manifest.Complete, ObservedAt: b.manifest.OriginalObservedAt, Status: "observed", Evaluated: len(b.records)}
		if coverage.ObservedAt != nil {
			observed, e := time.Parse(time.RFC3339Nano, *coverage.ObservedAt)
			coverage.Fresh = e == nil && !observed.After(at) && at.Sub(observed) <= time.Duration(q.MaxAgeSeconds)*time.Second
		}
		if !coverage.Fresh {
			coverage.Status = "stale-or-unknown"
		}
		if !coverage.Complete {
			coverage.Status = "partial"
		}
		for _, row := range b.records {
			switch predicate.Eval(row, q.Fields) {
			case query.True:
				result.Records = append(result.Records, row)
			case query.Unknown:
				coverage.Unknown++
				result.Unknown++
			}
		}
		if !coverage.Complete || !coverage.Fresh || coverage.Unknown > 0 {
			result.Exhaustive = false
		}
		result.Coverage = append(result.Coverage, coverage)
	}
	sort.Slice(result.Coverage, func(i, j int) bool { return result.Coverage[i].ConnectionID < result.Coverage[j].ConnectionID })
	sort.Slice(result.Records, func(i, j int) bool {
		a, b := result.Records[i], result.Records[j]
		if a.ConnectionID != b.ConnectionID {
			return a.ConnectionID < b.ConnectionID
		}
		return string(encodedRead(a.Identity)) < string(encodedRead(b.Identity))
	})
	result.Matched = len(result.Records)
	if len(result.Records) > q.Limit {
		result.Records = result.Records[:q.Limit]
		result.Exhaustive = false
	}
	result.Returned = len(result.Records)
	return result, nil
}
func EvaluateReadView(ctx context.Context, roots []string, q r.Query, viewRaw []byte, asOf string) (map[string]any, error) {
	d, e := Parse(viewRaw, "json")
	if e != nil || d.Kind() != "ViewDefinition" {
		return nil, readFailure(errors.New("ViewDefinition required"))
	}
	var v map[string]any
	if e = DecodeRead(d.CanonicalBytes(), &v); e != nil {
		return nil, e
	}
	spec := v["spec"].(map[string]any)
	vq := spec["query"].(map[string]any)
	if spec["location"] != "local" || vq["dialect"] != "portable" || vq["expression"] != q.Expression {
		return nil, failure("views.unsupported", "only matching explicit local portable views are implemented", 3, nil)
	}
	for _, name := range []string{"columns", "groupBy", "sortBy"} {
		for _, raw := range spec[name].([]any) {
			if _, ok := q.Fields[raw.(string)]; !ok {
				return nil, readFailure(errors.New("view field not cataloged"))
			}
		}
	}
	all := q
	all.Limit = 10000
	result, e := Federate(ctx, roots, all, asOf)
	if e != nil {
		return nil, e
	}
	if spec["layout"] == "timeline" {
		return nil, failure("views.unsupported", "timeline rendering is not implemented", 3, nil)
	}
	if spec["layout"] == "board" && len(spec["groupBy"].([]any)) == 0 {
		return nil, readFailure(errors.New("board requires explicit grouping"))
	}
	sort.SliceStable(result.Records, func(i, j int) bool {
		for _, raw := range spec["sortBy"].([]any) {
			name := raw.(string)
			a, ak := query.Lookup(result.Records[i], q.Fields, name)
			b, bk := query.Lookup(result.Records[j], q.Fields, name)
			if ak != bk {
				return ak
			}
			if ak {
				if c := query.Compare(a, b); c != 0 {
					return c < 0
				}
			}
		}
		return false
	})
	if len(result.Records) > q.Limit {
		result.Records = result.Records[:q.Limit]
		result.Exhaustive = false
	}
	result.Returned = len(result.Records)
	rows := []any{}
	unavailable := map[string]bool{}
	for _, record := range result.Records {
		values := map[string]any{}
		for _, c := range spec["columns"].([]any) {
			name := c.(string)
			v, known := query.Lookup(record, q.Fields, name)
			values[name] = map[string]any{"known": known, "value": v}
			if !known {
				unavailable[name] = true
			}
		}
		rows = append(rows, map[string]any{"identity": record.Identity, "connectionId": record.ConnectionID, "values": values})
	}
	columns := []string{}
	for name := range unavailable {
		columns = append(columns, name)
	}
	sort.Strings(columns)
	groups := []map[string]any{}
	groupIndex := map[string]int{}
	for i, record := range result.Records {
		key := []any{}
		for _, raw := range spec["groupBy"].([]any) {
			name := raw.(string)
			value, known := query.Lookup(record, q.Fields, name)
			key = append(key, map[string]any{"field": name, "known": known, "value": value})
		}
		if len(key) > 0 {
			k := sourceSemantic(key)
			index, ok := groupIndex[k]
			if !ok {
				index = len(groups)
				groupIndex[k] = index
				groups = append(groups, map[string]any{"key": key, "rows": []any{}})
			}
			groups[index]["rows"] = append(groups[index]["rows"].([]any), rows[i])
		}
	}
	// Provider rendering is not implied by a local data projection.
	return map[string]any{"result": result, "layout": spec["layout"], "columns": spec["columns"], "groupBy": spec["groupBy"], "sortBy": spec["sortBy"], "rows": rows, "groups": groups, "complete": result.Exhaustive && len(columns) == 0, "unavailableColumns": columns, "rendering": "data-projection", "providerViewCreated": false, "authorizationGranted": false, "executionReady": false}, nil
}
func InspectRead(ctx context.Context, root, asOf string) (map[string]any, error) {
	at, e := time.Parse(time.RFC3339Nano, asOf)
	if e != nil {
		return nil, readFailure(e)
	}
	b, e := LoadReadBundle(ctx, root)
	if e != nil {
		return nil, e
	}
	age := any(nil)
	if b.manifest.OriginalObservedAt != nil {
		obs, _ := time.Parse(time.RFC3339Nano, *b.manifest.OriginalObservedAt)
		if obs.After(at) {
			return nil, readFailure(errors.New("observation is in the future"))
		}
		age = at.Sub(obs).Seconds()
	}
	return map[string]any{"manifest": b.Manifest(), "bundleSha256": b.digest, "items": len(b.records), "catalogEntries": len(b.catalog), "ageSeconds": age, "freshness": "requires-operation-policy", "authorizationGranted": false, "executionReady": false}, nil
}
func DiffRead(ctx context.Context, a, b string) (map[string]any, error) {
	left, e := LoadReadBundle(ctx, a)
	if e != nil {
		return nil, e
	}
	right, e := LoadReadBundle(ctx, b)
	if e != nil {
		return nil, e
	}
	if readIdentityKey(left.config) != readIdentityKey(right.config) {
		return nil, failure("read.conflict", "captures have different connection or principal scopes", 4, nil)
	}
	old, newer := map[string]string{}, map[string]string{}
	for _, row := range left.records {
		old[string(encodedRead(row.Identity))] = semanticReadItem(row.Data)
	}
	for _, row := range right.records {
		newer[string(encodedRead(row.Identity))] = semanticReadItem(row.Data)
	}
	added, changed, absent := []string{}, []string{}, []string{}
	for id, hash := range newer {
		if before, ok := old[id]; !ok {
			added = append(added, id)
		} else if before != hash {
			changed = append(changed, id)
		}
	}
	for id := range old {
		if _, ok := newer[id]; !ok {
			absent = append(absent, id)
		}
	}
	sort.Strings(added)
	sort.Strings(changed)
	sort.Strings(absent)
	return map[string]any{"added": added, "changed": changed, "absentFromSnapshot": absent, "deletionEstablished": false, "completeComparison": left.manifest.Complete && right.manifest.Complete && readsource.Digest(left.config.Source.Scope) == readsource.Digest(right.config.Source.Scope), "sourceBytesChanged": left.manifest.SourceSHA256 != right.manifest.SourceSHA256, "authorizationGranted": false, "executionReady": false}, nil
}
func sourceSemantic(v any) string {
	raw, _ := json.Marshal(v)
	obj, e := engine.JSONValue(raw)
	if e != nil {
		return "unknown"
	}
	return SHA256(encodedRead(semanticReadValue(obj)))
}
func semanticReadValue(v any) any {
	switch x := v.(type) {
	case json.Number:
		r, ok := new(big.Rat).SetString(string(x))
		if !ok {
			return []any{"invalid-number"}
		}
		return []any{"number", r.RatString()}
	case map[string]any:
		out := map[string]any{}
		for k, child := range x {
			out[k] = semanticReadValue(child)
		}
		return []any{"object", out}
	case []any:
		out := []any{}
		for _, child := range x {
			out = append(out, semanticReadValue(child))
		}
		return []any{"array", out}
	case string:
		return []any{"string", x}
	case bool:
		return []any{"boolean", x}
	default:
		return []any{"null"}
	}
}
func readIdentityKey(c r.Configuration) string {
	return string(encodedRead(struct {
		ID, Provider, Origin, Tenant, Container, Principal, ContainerKey string
		Deployment                                                       *readsource.Deployment
		Auth                                                             readsource.Auth
	}{c.ConnectionID, c.Source.Provider, c.Source.Origin, c.Source.NativeTenantID, c.Source.ContainerID, c.Source.PrincipalPseudonym, c.Source.ContainerKey, c.Source.Deployment, c.Source.Auth}))
}
func semanticReadItem(i readsource.Item) string { i.Provenance = nil; return sourceSemantic(i) }
