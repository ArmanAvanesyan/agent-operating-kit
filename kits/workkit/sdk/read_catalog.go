package sdk

import (
	"context"
	"encoding/json"
	"errors"
	r "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/readv1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/engine"
	"strings"
)

func ReadProviders() []r.ProviderDescriptor {
	out := []r.ProviderDescriptor{}
	for _, p := range []string{"github", "azure-devops", "linear", "jira-cloud", "jira-datacenter"} {
		transport := "http"
		if p == "github" {
			transport = "gh"
		}
		operations := []r.OperationSupport{{Name: "items.read", Support: "supported", Qualification: "fixture-tested"}, {Name: "containers.read", Support: "supported", Qualification: "fixture-tested"}, {Name: "views.read", Support: "unknown", Qualification: "declared"}, {Name: "items.update", Support: "unsupported", Qualification: "declared"}, {Name: "views.create", Support: "unsupported", Qualification: "declared"}}
		out = append(out, r.ProviderDescriptor{Provider: p, ImplementationSHA256: engine.ImplementationSHA256, AdapterVersion: r.AdapterVersion, Transports: []string{transport}, Operations: operations})
	}
	return out
}
func InspectReadProvider(name string) (r.ProviderDescriptor, error) {
	for _, p := range ReadProviders() {
		if p.Provider == name {
			return p, nil
		}
	}
	return r.ProviderDescriptor{}, failure("read.unsupported", "provider is not registered", 3, nil)
}
func ReadCapabilities(ctx context.Context, root, asOf string) (map[string]any, error) {
	report, e := InspectRead(ctx, root, asOf)
	if e != nil {
		return nil, e
	}
	b, e := LoadReadBundle(ctx, root)
	if e != nil {
		return nil, e
	}
	p, e := InspectReadProvider(providerName(b.config.Source.Provider))
	if e != nil {
		return nil, e
	}
	return map[string]any{"provider": p, "capture": report, "scope": b.config.Source, "qualification": "fixture-tested", "liveQualification": "not-established", "authorizationGranted": false, "executionReady": false, "productionActivated": false}, nil
}
func ReadCatalog(ctx context.Context, root, category, asOf string) (map[string]any, error) {
	if _, e := InspectRead(ctx, root, asOf); e != nil {
		return nil, e
	}
	b, e := LoadReadBundle(ctx, root)
	if e != nil {
		return nil, e
	}
	keys := map[string][]string{"containers": {"repository", "project", "linear-metadata"}, "teams": {"linear-metadata"}, "types": {"types"}, "fields": {"fields", "github-project-fields"}, "states": {"states", "linear-metadata", "linear-states"}, "transitions": {}, "iterations": {}}
	needles, ok := keys[category]
	if !ok {
		return nil, readFailure(errors.New("unknown catalog category"))
	}
	entries := []r.CatalogEntry{}
	for _, entry := range b.catalog {
		for _, key := range needles {
			if entry.Name == key || strings.HasPrefix(entry.Name, key+":") {
				entries = append(entries, entry)
				break
			}
		}
	}
	if category == "transitions" {
		for _, page := range b.capture.Snapshot.Pages {
			if page.Request.Kind != "jira-transitions" && page.Request.Kind != "jira-dc-transitions" {
				continue
			}
			raw := b.capture.Files[page.Asset.Path]
			var node map[string]json.RawMessage
			if DecodeRead(raw, &node) == nil && len(node["transitions"]) > 0 {
				entries = append(entries, r.CatalogEntry{Name: "transitions:" + page.Request.ItemID, Data: node["transitions"], AssetSHA256: page.Asset.SHA256, JSONPointer: "/transitions", ProvenanceKnown: true})
			}
		}
	}
	status := "unknown"
	if len(entries) > 0 {
		status = "observed"
	}
	return map[string]any{"connectionId": b.config.ConnectionID, "category": category, "status": status, "entries": entries, "complete": b.manifest.Complete && len(entries) > 0, "scope": "captured-native-metadata", "liveQualification": "not-established", "authorizationGranted": false, "executionReady": false}, nil
}
func LocalReadViews(ctx context.Context, workspace string) ([]any, error) {
	snapshot, e := engine.Resolve(ctx, workspace, "")
	if e != nil {
		return nil, readFailure(e)
	}
	out := []any{}
	for _, doc := range snapshot.Documents {
		if doc.Kind == "ViewDefinition" {
			out = append(out, cloneRead[map[string]any](doc.Value))
		}
	}
	return out, nil
}
