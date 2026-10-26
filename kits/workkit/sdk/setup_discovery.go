package sdk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	s "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/setupv1"
	api "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/v1alpha1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/engine"
	check "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/setup"
	"strings"
)

// MetadataRequest contains only typed scope and categories, never URLs/commands.
// A protected embedding may supply additional metadata unavailable in P2 captures.
type MetadataRequest struct {
	Scope         s.Scope
	ConnectionRef api.Ref
	CaptureSHA256 string
	Categories    []string
}
type SetupMetadataReader interface {
	ReadSetupMetadata(context.Context, MetadataRequest) (s.Observation, map[string][]byte, error)
}

func (svc *Service) WithSetupMetadataReader(reader SetupMetadataReader) *Service {
	v := *svc
	v.setupReader = reader
	return &v
}
func nativeString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case json.Number:
		return string(x)
	}
	return ""
}
func metadataType(v string) string {
	switch strings.ToLower(v) {
	case "string", "text", "textfield":
		return "string"
	case "integer":
		return "integer"
	case "number", "double":
		return "number"
	case "boolean":
		return "boolean"
	case "single_select", "pickliststring", "enum", "projectv2singleselectfield":
		return "enum"
	case "user", "person":
		return "person"
	}
	return "unknown"
}
func (svc *Service) DiscoverSetup(ctx context.Context, capture, out string) (s.Observation, error) {
	if e := setupOutput(out, capture); e != nil {
		return s.Observation{}, e
	}
	b, e := LoadReadBundle(ctx, capture)
	if e != nil {
		return s.Observation{}, e
	}
	source := b.config.Source
	base := ""
	deployment := ""
	if source.Deployment != nil {
		base = source.Deployment.BasePath
		deployment = check.Hash(source.Deployment)
	}
	var profile api.ConnectionProfile
	if e = setupDocument(b.config.Profile, "ConnectionProfile", &profile); e != nil {
		return s.Observation{}, e
	}
	scope := s.Scope{Provider: providerName(source.Provider), Origin: profile.Spec.Origin, BasePath: base, TenantID: source.NativeTenantID, ContainerID: source.ContainerID, APIVersion: source.APIVersion, DeploymentSHA256: deployment, Principal: source.PrincipalPseudonym}
	ref := api.Ref{Kind: "ConnectionProfile", Id: profile.Metadata.Id, Revision: profile.Metadata.Revision, Sha256: api.Sha(SHA256(b.config.Profile))}
	cats := []string{"containers", "fields", "states", "transitions", "people", "views"}
	if scope.Provider == "azure-devops" {
		cats = append(cats, "processes", "teams", "boards", "types")
	}
	if strings.HasPrefix(scope.Provider, "jira-") {
		cats = append(cats, "contexts", "types")
	}
	if scope.Provider == "linear" {
		cats = append(cats, "teams", "labels")
	}
	if svc.setupReader != nil {
		o, files, e := svc.setupReader.ReadSetupMetadata(ctx, MetadataRequest{scope, ref, b.digest, cats})
		if e != nil {
			return o, e
		}
		if o.Scope != scope || o.ConnectionRef != ref || o.CaptureSHA256 != b.digest || o.ConfigurationSHA256 != b.manifest.ConfigurationSHA256 {
			return o, errors.New("setup reader scope drift")
		}
		if e = WriteSetupObservation(ctx, o, files, out); e != nil {
			return o, e
		}
		return o, nil
	}
	o := s.Observation{Version: s.Version, Kind: "SetupObservation", ConnectionRef: ref, ConfigurationSHA256: b.manifest.ConfigurationSHA256, CaptureSHA256: b.digest, Scope: scope, AdapterVersion: b.manifest.AdapterVersion, Transport: b.manifest.Transport, Revision: b.digest, RevisionKind: "observation-fingerprint", QualificationOnly: true, Coverage: []s.Coverage{}, Entities: []s.Entity{}, Assets: []api.Asset{}, Diagnostics: []api.Diagnostic{}}
	if b.manifest.OriginalObservedAt != nil {
		o.ObservedAt = *b.manifest.OriginalObservedAt
	} else {
		return o, errors.New("original observation date required")
	}
	files := map[string][]byte{"assets/profile.json": b.config.Profile, "assets/configuration.json": b.configBytes, "assets/capture-manifest.json": b.manifestRaw}
	known := map[string]bool{}
	seen := map[string]string{}
	for i, page := range b.capture.Snapshot.Pages {
		if e = ctx.Err(); e != nil {
			return o, e
		}
		raw := b.capture.Files[page.Asset.Path]
		assetPath := fmt.Sprintf("assets/pages/%04d.json", i)
		files[assetPath] = raw
		node, e := engine.JSONValue(raw)
		if e != nil {
			return o, e
		}
		// Metadata parsers dispatch by the capture's validated typed request kind.
		add := func(kind, parent, pointer, idKey, nameKey, typeKey string, obj map[string]any) {
			if kind == "field" && typeKey == "dataType" && obj[typeKey] == nil && obj["__typename"] != nil {
				typeKey = "__typename"
			}
			id := nativeString(obj[idKey])
			if id == "" {
				return
			}
			name := nativeString(obj[nameKey])
			typ := nativeString(obj[typeKey])
			typePointer := pointer + "/" + typeKey
			if kind == "field" && strings.HasPrefix(scope.Provider, "jira-") {
				schema, _ := obj["schema"].(map[string]any)
				if st := nativeString(schema["type"]); st != "" {
					typ = st
					typePointer = pointer + "/schema/type"
				}
			}
			fields := map[string]string{"id": pointer + "/" + idKey}
			if name != "" {
				fields["name"] = pointer + "/" + nameKey
			}
			if typ != "" {
				fields["nativeType"] = typePointer
			}
			ent := s.Entity{Kind: kind, ID: id, ParentID: parent, Name: name, Type: metadataType(typ), NativeType: typ, Options: []s.Option{}, RequiredFields: []string{}, Provenance: s.Provenance{Asset: api.Asset{Path: assetPath, Sha256: api.Sha(SHA256(raw))}, Pointer: pointer, Fields: fields}}
			if required, ok := obj["required"].(bool); ok {
				ent.Required = required
				ent.Provenance.Fields["required"] = pointer + "/required"
			}
			if options, ok := obj["options"].([]any); ok {
				ent.Provenance.Fields["options"] = pointer + "/options"
				for _, v := range options {
					m, _ := v.(map[string]any)
					ent.Options = append(ent.Options, s.Option{ID: nativeString(m["id"]), Name: nativeString(m["name"])})
				}
			}
			if parent != scope.ContainerID && parent != scope.TenantID && kind == "field" {
				ent.Provenance.Fields["parentId"] = "/data/node/id"
			}
			key := kind + "\x00" + id
			val := check.Hash(obj)
			if old, ok := seen[key]; ok {
				if old != val {
					o.Diagnostics = append(o.Diagnostics, api.Diagnostic{Code: "metadata.conflict", Field: kind, Message: "conflicting repeated native identity", Severity: "error"})
				}
				return
			}
			seen[key] = val
			o.Entities = append(o.Entities, ent)
		}
		arr := func(v any, kind, parent, pointer, id, name, typ string) {
			xs, _ := v.([]any)
			for j, v := range xs {
				m, _ := v.(map[string]any)
				add(kind, parent, fmt.Sprintf("%s/%d", pointer, j), id, name, typ, m)
			}
		}
		m, _ := node.(map[string]any)
		switch page.Request.Kind {
		case "github-repository", "github-repo":
			add("container", "", "", "node_id", "name", "", m)
			known["containers"] = true
		case "azure-project", "jira-project", "jira-dc-project":
			add("container", "", "", "id", "name", "", m)
			known["containers"] = true
		case "azure-fields":
			arr(m["value"], "field", scope.ContainerID, "/value", "referenceName", "name", "type")
			known["fields"] = true
		case "azure-types":
			arr(m["value"], "type", scope.ContainerID, "/value", "referenceName", "name", "")
			known["types"] = true
		case "azure-processes":
			arr(m["value"], "process", scope.TenantID, "/value", "id", "name", "type")
			known["processes"] = true
		case "jira-fields", "jira-dc-fields":
			if xs, ok := node.([]any); ok {
				arr(xs, "field", scope.ContainerID, "", "id", "name", "")
				known["fields"] = true
			} else if m["values"] != nil {
				arr(m["values"], "field", scope.ContainerID, "/values", "id", "name", "")
				known["fields"] = true
			}
		case "jira-types", "jira-dc-types":
			arr(node, "type", scope.ContainerID, "", "id", "name", "")
			known["types"] = true
		case "github-project-fields":
			data, _ := m["data"].(map[string]any)
			n, _ := data["node"].(map[string]any)
			add("container", scope.ContainerID, "/data/node", "id", "title", "", n)
			known["containers"] = true
			f, _ := n["fields"].(map[string]any)
			arr(f["nodes"], "field", nativeString(n["id"]), "/data/node/fields/nodes", "id", "name", "dataType")
			known["fields"] = true
		case "linear-metadata", "linear-states", "linear-labels":
			data, _ := m["data"].(map[string]any)
			team, _ := data["team"].(map[string]any)
			add("team", scope.TenantID, "/data/team", "id", "name", "", team)
			known["teams"] = true
			for _, v := range []struct{ key, kind string }{{"states", "state"}, {"labels", "label"}} {
				conn, _ := team[v.key].(map[string]any)
				if conn != nil {
					arr(conn["nodes"], v.kind, scope.ContainerID, "/data/team/"+v.key+"/nodes", "id", "name", "type")
					known[v.key] = true
				}
			}
		}
	}
	for _, cat := range cats {
		c := s.Coverage{Category: cat, Status: "unknown", Reason: "required metadata not captured by this adapter"}
		if known[cat] {
			c.Status = "observed"
			c.Complete = b.manifest.Complete && len(o.Diagnostics) == 0
			c.Evidence = api.Asset{Path: "assets/capture-manifest.json", Sha256: api.Sha(SHA256(b.manifestRaw))}
			c.CompletePointer = "/complete"
			c.Reason = "captured typed metadata; completeness bounded by original capture"
		}
		o.Coverage = append(o.Coverage, c)
	}
	check.Sort(&o)
	if e = WriteSetupObservation(ctx, o, files, out); e != nil {
		return o, e
	}
	return o, nil
}
