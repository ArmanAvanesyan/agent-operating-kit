// Developer-only materializer. All identities and records are synthetic.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	r "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/readv1"
	s "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/setupv1"
	api "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/v1alpha1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/readsource"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/sdk"
	"os"
	"path/filepath"
)

func enc(v any) []byte {
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		panic(e)
	}
	return append(b, '\n')
}
func main() {
	check := flag.Bool("check", false, "")
	flag.Parse()
	tmp, e := os.MkdirTemp("", "workkit-p4-fixtures-")
	if e != nil {
		panic(e)
	}
	tmp, e = filepath.EvalSymlinks(tmp)
	if e != nil {
		panic(e)
	}
	defer os.RemoveAll(tmp)
	for _, p := range []string{"github", "azure-devops", "jira-cloud", "jira-datacenter", "linear"} {
		origin := map[string]string{"github": "https://api.github.com", "azure-devops": "https://dev.azure.com", "jira-cloud": "https://synthetic.atlassian.net", "jira-datacenter": "https://jira.synthetic.invalid", "linear": "https://api.linear.app"}[p]
		version := "synthetic-api"
		transport := "rest"
		if p == "github" {
			transport = "gh"
		}
		if p == "linear" {
			transport = "graphql"
		}
		profile := api.ConnectionProfile{APIVersion: api.Version, Kind: "ConnectionProfile", Metadata: api.Metadata{Id: p, Revision: 1}, Spec: api.ConnectionProfileSpec{Provider: p, Origin: origin, Transport: transport, Scope: api.Scope{TenantId: "tenant-synthetic", ContainerIds: []string{"container-synthetic"}}, ApiVersion: version}}
		scope := s.Scope{Provider: p, Origin: origin, TenantID: "tenant-synthetic", ContainerID: "container-synthetic", APIVersion: version, Principal: "principal-synthetic"}
		if p == "jira-datacenter" {
			scope.BasePath = "/jira"
			scope.DeploymentSHA256 = sdk.SHA256([]byte("synthetic-deployment"))
			profile.Spec.BasePath = "/jira"
			profile.Spec.Deployment = &api.ConnectionProfileSpecDeployment{ServerVersion: "synthetic", TlsTrust: api.Asset{Path: "assets/ca.pem", Sha256: api.Sha(sdk.SHA256([]byte("synthetic CA")))}}
		}
		authRef := "credentials/" + p
		profile.Spec.AuthRef = &authRef
		profileRaw, _ := json.Marshal(profile)
		if _, err := sdk.Parse(profileRaw, "json"); err != nil {
			panic(fmt.Sprintf("%s profile: %s (%s)", p, err, profileRaw))
		}
		sourceProvider := p
		if p == "jira-cloud" {
			sourceProvider = "jira"
		}
		srcVersion := readsource.Version
		var deploy *readsource.Deployment
		if p == "jira-datacenter" {
			srcVersion = readsource.Version2
			deploy = &readsource.Deployment{BasePath: scope.BasePath, ServerVersion: "synthetic", TLSTrustSHA256: string(profile.Spec.Deployment.TlsTrust.Sha256)}
			d, _ := json.Marshal(deploy)
			scope.DeploymentSHA256 = sdk.SHA256(d)
		}
		configValue := r.Configuration{Version: r.Version, ConnectionID: p, Profile: profileRaw, Source: readsource.Source{Version: srcVersion, Provider: sourceProvider, Origin: origin, APIVersion: version, NativeTenantID: scope.TenantID, ContainerID: scope.ContainerID, ContainerKey: "synthetic", PrincipalPseudonym: scope.Principal, Auth: readsource.Auth{Mode: "synthetic"}, Scope: readsource.Scope{ItemIDs: []string{}}, MappingVersion: "synthetic", Deployment: deploy}}
		configRaw, _ := json.Marshal(configValue)
		ref := api.Ref{Kind: "ConnectionProfile", Id: p, Revision: 1, Sha256: api.Sha(sdk.SHA256(profileRaw))}
		native := []map[string]any{{"id": "container-synthetic", "name": "Synthetic container", "nativeType": "", "parentId": "tenant-synthetic", "options": []s.Option{}}, {"id": "field-title", "name": "Title", "nativeType": "text", "parentId": "container-synthetic", "options": []s.Option{}}, {"id": "field-priority", "name": "Priority", "nativeType": "single_select", "parentId": "container-synthetic", "options": []s.Option{{ID: "opt-low", Name: "Low"}, {ID: "opt-high", Name: "High"}}}, {"id": "process-synthetic", "name": "Synthetic process", "nativeType": "inherited", "parentId": "container-synthetic", "options": []s.Option{}}}
		raw := enc(map[string]any{"complete": true, "entities": native})
		asset := api.Asset{Path: "assets/native.json", Sha256: api.Sha(sdk.SHA256(raw))}
		o := s.Observation{Version: s.Version, Kind: "SetupObservation", ConnectionRef: ref, ConfigurationSHA256: sdk.SHA256(configRaw), CaptureSHA256: sdk.SHA256([]byte("synthetic-capture-" + p)), Scope: scope, ObservedAt: "2026-10-26T00:00:00Z", AdapterVersion: "synthetic-fixture/v1", Transport: r.TransportIdentity{Kind: "captured-source", Version: "unverified", Principal: scope.Principal, AuthMode: "synthetic"}, Revision: "synthetic-fingerprint", RevisionKind: "observation-fingerprint", QualificationOnly: true, Entities: []s.Entity{}, Coverage: []s.Coverage{}, Assets: []api.Asset{}, Diagnostics: []api.Diagnostic{}}
		for i, v := range native {
			kind := []string{"container", "field", "field", "process"}[i]
			typ := []string{"unknown", "string", "enum", "unknown"}[i]
			ptr := fmt.Sprintf("/entities/%d", i)
			o.Entities = append(o.Entities, s.Entity{Kind: kind, ID: v["id"].(string), Name: v["name"].(string), ParentID: v["parentId"].(string), Type: typ, NativeType: v["nativeType"].(string), Options: v["options"].([]s.Option), RequiredFields: []string{}, Provenance: s.Provenance{Asset: asset, Pointer: ptr, Fields: map[string]string{"id": ptr + "/id", "name": ptr + "/name", "nativeType": ptr + "/nativeType", "parentId": ptr + "/parentId", "options": ptr + "/options"}}})
		}
		for _, c := range []string{"containers", "fields", "processes"} {
			o.Coverage = append(o.Coverage, s.Coverage{Category: c, Status: "observed", Complete: true, Reason: "synthetic fixture only", Evidence: asset, CompletePointer: "/complete"})
		}
		for _, c := range []string{"people", "states", "transitions", "views"} {
			o.Coverage = append(o.Coverage, s.Coverage{Category: c, Status: "unknown", Reason: "no synthetic assertion"})
		}
		dir := filepath.Join(tmp, p)
		if e = sdk.WriteSetupObservation(context.Background(), o, map[string][]byte{"assets/native.json": raw, "assets/profile.json": profileRaw, "assets/configuration.json": configRaw}, dir); e != nil {
			panic(e)
		}
		id := "field-title"
		desired := api.DesiredSetup{APIVersion: api.Version, Kind: "DesiredSetup", Metadata: api.Metadata{Id: "desired-" + p, Revision: 1}, Spec: api.DesiredSetupSpec{ConnectionRef: ref, ContainerId: scope.ContainerID, Fields: []api.DesiredSetupSpecFieldsItem{{LogicalId: "title", NativeId: &id, Name: "Title", Type: "text", Options: []string{}}}, Views: []api.Ref{}}}
		mapping := api.Mapping{APIVersion: api.Version, Kind: "Mapping", Metadata: api.Metadata{Id: "mapping-" + p, Revision: 1}, Spec: api.MappingSpec{Fields: []api.MappingSpecFieldsItem{{Canonical: "title", SourceFieldId: id, TargetFieldId: id, Owner: "source", ValueMap: map[string]json.RawMessage{}, LossPolicy: "reject"}}, Lifecycle: []api.MappingSpecLifecycleItem{}, PersonMapping: []api.MappingSpecPersonMappingItem{}}}
		mapRaw := enc(mapping)
		saved, _, e := sdk.LoadSetupObservation(context.Background(), dir)
		if e != nil {
			panic(e)
		}
		savedRaw, _ := json.Marshal(saved)

		context := s.MappingContext{Version: s.Version, Kind: "MappingContext", MappingSHA256: sdk.SHA256(mapRaw), SourceSHA256: sdk.SHA256(savedRaw), DestinationSHA256: sdk.SHA256(savedRaw), RequiredFields: []string{"title"}}
		for n, b := range map[string][]byte{"desired.json": enc(desired), "mapping.json": mapRaw, "mapping-context.json": enc(context)} {
			if e = os.WriteFile(filepath.Join(dir, n), b, 0644); e != nil {
				panic(e)
			}
		}
	}
	root := "kits/workkit/testdata/setup"
	e = filepath.WalkDir(tmp, func(p string, d os.DirEntry, e error) error {
		if e != nil || d.IsDir() {
			return e
		}
		rel, _ := filepath.Rel(tmp, p)
		target := filepath.Join(root, rel)
		b, e := os.ReadFile(p)
		if e != nil {
			return e
		}
		if *check {
			old, e := os.ReadFile(target)
			if e != nil || !bytes.Equal(old, b) {
				return fmt.Errorf("setup fixture drift: %s", target)
			}
			return nil
		}
		if e = os.MkdirAll(filepath.Dir(target), 0755); e != nil {
			return e
		}
		return os.WriteFile(target, b, 0644)
	})
	if e != nil {
		panic(e)
	}
	fmt.Println("WorkKit setup fixtures passed")
}
