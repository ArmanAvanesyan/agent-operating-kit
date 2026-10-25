package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"path"
	"strings"
	"sync"

	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/schemas"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

type Document struct {
	Raw, Canonical []byte
	Value          map[string]any
	Kind, ID       string
	Revision       json.Number
}

var schemaOnce sync.Once
var registry map[string]*jsonschema.Schema
var schemaErr error

type denyLoader struct{}

func (denyLoader) Load(s string) (any, error) {
	return nil, fmt.Errorf("external schema reference rejected: %s", s)
}
func loadSchemas() {
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft2020)
	c.AssertFormat()
	// Every reference is preloaded. Unknown URLs cannot trigger a network loader.
	c.UseLoader(denyLoader{})
	registry = map[string]*jsonschema.Schema{}
	resourceSchema = map[string]map[string]any{}
	ids := map[string]string{}
	entries, e := schemas.Files.ReadDir(".")
	if e != nil {
		schemaErr = e
		return
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		b, e := schemas.Files.ReadFile(entry.Name())
		if e != nil {
			schemaErr = e
			return
		}
		v, e := JSONValue(b)
		if e != nil {
			schemaErr = e
			return
		}
		m := Map(v)
		if entry.Name() == "common.schema.json" {
			commonDefs = Map(m["$defs"])
		}
		if k := String(m["title"]); k != "" {
			resourceSchema[k] = m
		}
		id := String(m["$id"])
		if e = c.AddResource(id, v); e != nil {
			schemaErr = e
			return
		}
		if k := String(m["title"]); k != "" {
			ids[k] = id
		}
	}
	for k, id := range ids {
		s, e := c.Compile(id)
		if e != nil {
			schemaErr = e
			return
		}
		registry[k] = s
	}
}
func Parse(raw []byte, format string) (*Document, error) {
	value, canonical, err := Canonical(raw, format)
	if err != nil {
		return nil, err
	}
	if value["apiVersion"] != "workkit/v1alpha1" {
		return nil, errors.New("unsupported apiVersion")
	}
	schemaOnce.Do(loadSchemas)
	if schemaErr != nil {
		return nil, schemaErr
	}
	kind := String(value["kind"])
	schema, ok := registry[kind]
	if !ok {
		return nil, errors.New("unsupported resource kind")
	}
	// Validate json.Number through the schema library's exact decimal support.
	if err = schema.Validate(value); err != nil {
		return nil, fmt.Errorf("resource schema rejected: %w", err)
	}
	if err = semantics(kind, Map(value["spec"])); err != nil {
		return nil, err
	}
	metadata := Map(value["metadata"])
	revision, _ := metadata["revision"].(json.Number)
	normalized, err := AuthorRevision(revision)
	if err != nil {
		return nil, err
	}
	revision = json.Number(normalized)
	document := &Document{append([]byte(nil), raw...), canonical, value, kind, String(metadata["id"]), revision}
	refs, _ := References(document)
	for _, ref := range refs {
		n, _ := ref["revision"].(json.Number)
		if _, e := AuthorRevision(n); e != nil {
			return nil, e
		}
	}
	return document, nil
}
func semantics(kind string, s map[string]any) error {
	expected := map[string]string{"connectionRef": "ConnectionProfile", "mappingRef": "Mapping", "profileRef": "NativeTaskProfile", "toolchainPolicyRef": "ToolchainPolicy", "routeRef": "SyncRoute", "capabilityRef": "CapabilityReport", "policyRef": "OperatorPolicy"}
	for field, want := range expected {
		if v, ok := s[field]; ok && Map(v)["kind"] != want {
			return errors.New("wrong-kind typed reference")
		}
	}
	for _, field := range []string{"source", "destination"} {
		if endpoint := Map(s[field]); endpoint != nil {
			if ref, ok := endpoint["connectionRef"]; ok && Map(ref)["kind"] != "ConnectionProfile" {
				return errors.New("wrong-kind endpoint reference")
			}
		}
	}

	switch kind {
	case "Workspace":
		expected := map[string]string{"connections": "ConnectionProfile", "bindings": "WorkBinding", "syncRoutes": "SyncRoute", "nativeProfiles": "NativeTaskProfile"}
		for field, target := range expected {
			seen := map[string]bool{}
			for _, r := range List(s[field]) {
				ref := Map(r)
				id := String(ref["id"])
				if seen[id] || ref["kind"] != target {
					return errors.New("duplicate or wrong-kind workspace reference")
				}
				seen[id] = true
			}
		}
	case "Mapping":
		seen := map[string]bool{}
		for _, f := range List(s["fields"]) {
			m := Map(f)
			name := String(m["canonical"])
			if seen[name] {
				return errors.New("duplicate canonical mapping field")
			}
			seen[name] = true
		}
	case "ConnectionProfile":
		u, e := url.Parse(String(s["origin"]))
		if e != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || u.Host == "" {
			return errors.New("origin must contain scheme/host/port only")
		}
		provider := String(s["provider"])
		if provider != "local" && u.Scheme != "https" {
			return errors.New("remote origin requires HTTPS")
		}
		base := String(s["basePath"])
		if base != "" && (path.Clean(base) != base || strings.ContainsAny(base, "?#\\") || strings.Contains(base, "%")) {
			return errors.New("basePath must be normalized")
		}
		allowed := map[string]string{"github": "gh rest", "azure-devops": "az rest", "jira-cloud": "acli rest", "jira-datacenter": "rest", "linear": "graphql community-cli", "local": "local"}
		if !strings.Contains(" "+allowed[provider]+" ", " "+String(s["transport"])+" ") {
			return errors.New("provider/transport mismatch")
		}
	case "WorkBinding":
		if s["status"] == "verified" {
			if len(List(s["checks"])) == 0 || len(List(s["evidence"])) == 0 {
				return errors.New("verified binding lacks supplied conformance evidence")
			}
			for _, c := range List(s["checks"]) {
				if Map(c)["status"] != "satisfied" {
					return errors.New("incomplete verified binding")
				}
			}
		}
	case "OperationPlan":
		pre := Map(s["precondition"])
		r := Map(pre["revision"])
		if pre["requiredConcurrency"] == "provider-cas" && r["kind"] != "etag" && r["kind"] != "native-revision" {
			return errors.New("fingerprint cannot establish CAS")
		}
		for _, dep := range List(s["dependsOn"]) {
			if dep == s["operationId"] {
				return errors.New("operation self-dependency")
			}
		}
		if s["operation"] == "native-update" && String(Map(s["nativeTarget"])["nativeTaskId"]) == "" {
			return errors.New("native-update requires returned native identity")
		}
	case "ViewDefinition":
		if s["location"] == "provider" && s["connectionRef"] == nil {
			return errors.New("provider view needs connection")
		}
	case "SyncRoute":
		if string(Encode(s["source"])) == string(Encode(s["destination"])) {
			return errors.New("identical sync endpoints")
		}
	}
	return nil
}

func AuthorRevision(n json.Number) (string, error) {
	r, ok := new(big.Rat).SetString(string(n))
	if !ok || !r.IsInt() || r.Sign() <= 0 || r.Num().Cmp(big.NewInt(9007199254740991)) > 0 {
		return "", errors.New("author revision exceeds safe integer range")
	}
	return r.Num().String(), nil
}
