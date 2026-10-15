// Package opencode projects a deliberately small, loss-aware subset of AOK
// agent/profile fields into OpenCode's Markdown agent format.
package opencode

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"path"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/nativeadapter"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/nativeparse"
)

const (
	target         = "opencode"
	adapterVersion = "0.1.0"
	projectScope   = "project"
	agentDir       = ".opencode/agents/"
)

var agentID = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)

type Adapter struct{}

func New() nativeadapter.Adapter { return Adapter{} }

func (Adapter) Descriptor() nativeadapter.Descriptor {
	return nativeadapter.Descriptor{
		Target: target, AdapterVersion: adapterVersion,
		Scopes:      []string{projectScope},
		HostLoading: nativeadapter.HostLoadingEvidence{Status: "verified", HostVersion: "1.18.35", Scopes: []string{"project"}, Evidence: []string{"docs/evidence/native-adapters/opencode-1.18.35.json"}},
		Fields: []nativeadapter.FieldCapability{
			{NeutralPath: "agent.metadata.id", NativePath: "file.name", Support: "native", EnforcementOwner: "native"},
			{NeutralPath: "agent.identity.purpose", NativePath: "frontmatter.description", Support: "native", EnforcementOwner: "native"},
			{NeutralPath: "executionProfile.model.selector", NativePath: "frontmatter.model", Support: "native", EnforcementOwner: "native"},
			{NeutralPath: "executionProfile.model.generation.temperature", NativePath: "frontmatter.temperature", Support: "native", EnforcementOwner: "native"},
			{NeutralPath: "executionProfile.model.generation.topP", NativePath: "frontmatter.top_p", Support: "native", EnforcementOwner: "native"},
			{NeutralPath: "agent.instructions.sources", NativePath: "markdown.body", Support: "native", EnforcementOwner: "native"},
			{NeutralPath: "agent.permissions.filesystem.read", NativePath: "unsupported.filesystem.read", Support: "unsupported", EnforcementOwner: "none"},
			{NeutralPath: "agent.permissions.filesystem.write", NativePath: "unsupported.filesystem.write", Support: "unsupported", EnforcementOwner: "none"},
			{NeutralPath: "agent.permissions.network.mode", NativePath: "unsupported.network.mode", Support: "unsupported", EnforcementOwner: "none"},
			{NeutralPath: "agent.permissions.network.hosts", NativePath: "unsupported.network.hosts", Support: "unsupported", EnforcementOwner: "none"},
			{NeutralPath: "agent.capabilities.mcps", NativePath: "unsupported.capabilities.mcps", Support: "unsupported", EnforcementOwner: "none"},
		},
	}
}

func (Adapter) Decode(scope string, files []nativeadapter.File) (nativeadapter.Document, []contracts.Diagnostic, error) {
	if scope != projectScope || len(files) != 1 {
		return nativeadapter.Document{}, nil, errors.New("expected one project agent Markdown file")
	}
	f := files[0]
	if f.Format != "markdown" || !validAgentPath(f.Path) {
		return nativeadapter.Document{}, nil, errors.New("expected .opencode/agents/<name>.md Markdown input")
	}
	front, body, err := nativeparse.Frontmatter(f.Data)
	if err != nil {
		return nativeadapter.Document{}, nil, err
	}
	fields := map[string]json.RawMessage{}
	unknown := map[string]json.RawMessage{}
	ds := []contracts.Diagnostic{}
	id := strings.TrimSuffix(path.Base(f.Path), ".md")
	if !agentID.MatchString(id) {
		ds = append(ds, diagnostic("opencode.agent-id.slug", f.Path, "agent.metadata.id", "warning", "native filename does not match the canonical AOK id pattern; filename is preserved"))
	}
	put(fields, "agent.metadata.id", id)
	for key, value := range front {
		raw, e := json.Marshal(value)
		if e != nil {
			return nativeadapter.Document{}, ds, errors.New("frontmatter contains an unsupported value")
		}
		switch key {
		case "description":
			if !isNonemptyString(value) {
				return nativeadapter.Document{}, ds, errors.New("description must be a non-empty string")
			}
			fields["agent.identity.purpose"] = raw
		case "model":
			if !isNonemptyString(value) {
				return nativeadapter.Document{}, ds, errors.New("model must be a non-empty string")
			}
			fields["executionProfile.model.selector"] = raw
		case "temperature":
			if !boundedNumber(value, 0, 2) {
				return nativeadapter.Document{}, ds, errors.New("temperature must be a number from 0 through 2")
			}
			fields["executionProfile.model.generation.temperature"] = raw
		case "top_p":
			if !boundedNumber(value, 0, 1) {
				return nativeadapter.Document{}, ds, errors.New("top_p must be a number from 0 through 1")
			}
			fields["executionProfile.model.generation.topP"] = raw
		default:
			unknown["frontmatter."+key] = raw
		}
	}
	if _, ok := fields["agent.identity.purpose"]; !ok {
		return nativeadapter.Document{}, ds, errors.New("required OpenCode description is missing")
	}
	fields["agent.instructions.sources"] = json.RawMessage(`[{"path":"instructions.md"}]`)
	return nativeadapter.Document{Fields: fields, Unknown: unknown, Assets: map[string][]byte{"instructions.md": append([]byte(nil), body...)}}, ds, nil
}

func (Adapter) Encode(scope string, doc nativeadapter.Document) ([]nativeadapter.File, []contracts.Diagnostic, error) {
	if scope != projectScope {
		return nil, nil, errors.New("unsupported scope")
	}
	allowed := map[string]bool{
		"agent.metadata.id": true, "agent.identity.purpose": true,
		"executionProfile.model.selector":               true,
		"executionProfile.model.generation.temperature": true,
		"executionProfile.model.generation.topP":        true,
		"agent.instructions.sources":                    true,
	}
	for k := range doc.Fields {
		if !allowed[k] {
			return nil, nil, fmt.Errorf("unsupported mapped field %q", k)
		}
	}
	id, err := requiredString(doc.Fields, "agent.metadata.id")
	if err != nil {
		return nil, nil, err
	}
	if strings.ContainsAny(id, `/\\`) || id == "." || id == ".." {
		return nil, nil, errors.New("agent id is not a valid native filename")
	}
	ds := []contracts.Diagnostic{}
	if !agentID.MatchString(id) {
		ds = append(ds, diagnostic("opencode.agent-id.slug", "", "agent.metadata.id", "warning", "canonical id is not a canonical slug; native filename is preserved"))
	}
	description, err := requiredString(doc.Fields, "agent.identity.purpose")
	if err != nil {
		return nil, ds, err
	}
	front := map[string]any{"description": description}
	if raw, ok := doc.Fields["executionProfile.model.selector"]; ok {
		v, good := rawString(raw)
		if !good || v == "" {
			return nil, ds, errors.New("model selector must be a non-empty string")
		}
		front["model"] = v
	}
	if raw, ok := doc.Fields["executionProfile.model.generation.temperature"]; ok {
		v, good := rawNumber(raw)
		if !good || v < 0 || v > 2 {
			return nil, ds, errors.New("temperature must be a number from 0 through 2")
		}
		front["temperature"] = json.Number(string(raw))
	}
	if raw, ok := doc.Fields["executionProfile.model.generation.topP"]; ok {
		v, good := rawNumber(raw)
		if !good || v < 0 || v > 1 {
			return nil, ds, errors.New("topP must be a number from 0 through 1")
		}
		front["top_p"] = json.Number(string(raw))
	}
	body, err := instructionBody(doc)
	if err != nil {
		return nil, ds, err
	}
	for key, raw := range doc.Unknown {
		if !strings.HasPrefix(key, "frontmatter.") {
			return nil, ds, fmt.Errorf("unsupported unknown native path %q", key)
		}
		name := strings.TrimPrefix(key, "frontmatter.")
		if name == "" {
			return nil, ds, errors.New("unknown frontmatter path must name one top-level key")
		}
		if _, collision := front[name]; collision {
			return nil, ds, fmt.Errorf("unknown frontmatter key collides with mapped key %q", name)
		}
		v, e := contracts.DecodeJSON(raw)
		if e != nil {
			return nil, ds, errors.New("unknown frontmatter value is invalid")
		}
		front[name] = v
	}
	if _, ok := front["description"]; !ok {
		return nil, ds, errors.New("description is required")
	}
	yamlData, err := nativeparse.Encode("yaml", front)
	if err != nil {
		return nil, ds, errors.New("frontmatter cannot be represented in OpenCode YAML")
	}
	data := append([]byte("---\n"), yamlData...)
	data = append(data, []byte("---\n")...)
	data = append(data, body...)
	return []nativeadapter.File{{Path: agentDir + id + ".md", Format: "markdown", Data: data}}, ds, nil
}

func validAgentPath(p string) bool {
	return strings.HasSuffix(p, ".md") && (path.Dir(p) == "." || path.Dir(p) == strings.TrimSuffix(agentDir, "/")) && len(strings.TrimSuffix(path.Base(p), ".md")) > 0
}

func instructionBody(doc nativeadapter.Document) ([]byte, error) {
	raw, ok := doc.Fields["agent.instructions.sources"]
	if !ok {
		return nil, errors.New("one materialized instruction source is required")
	}
	v, err := contracts.DecodeJSON(raw)
	if err != nil {
		return nil, errors.New("instruction sources are invalid")
	}
	items, ok := v.([]any)
	if !ok || len(items) != 1 {
		return nil, errors.New("exactly one instruction source is supported")
	}
	item, ok := items[0].(map[string]any)
	if !ok || len(item) != 1 {
		return nil, errors.New("instruction source must contain only a path")
	}
	name, ok := item["path"].(string)
	if !ok || name == "" {
		return nil, errors.New("instruction source path must be a string")
	}
	body, ok := doc.Assets[name]
	if !ok {
		return nil, errors.New("instruction source has no materialized asset")
	}
	if len(doc.Assets) != 1 {
		return nil, errors.New("unreferenced materialized assets are unsupported")
	}
	if !utf8.Valid(body) {
		return nil, errors.New("instruction body must be valid UTF-8")
	}
	return append([]byte(nil), body...), nil
}

func requiredString(fields map[string]json.RawMessage, key string) (string, error) {
	raw, ok := fields[key]
	if !ok {
		return "", fmt.Errorf("required field %s is missing", key)
	}
	v, ok := rawString(raw)
	if !ok || strings.TrimSpace(v) == "" {
		return "", fmt.Errorf("field %s must be a non-empty string", key)
	}
	return v, nil
}
func rawString(raw json.RawMessage) (string, bool) {
	v, err := contracts.DecodeJSON(raw)
	s, ok := v.(string)
	return s, err == nil && ok
}
func rawNumber(raw json.RawMessage) (float64, bool) {
	v, err := contracts.DecodeJSON(raw)
	n, ok := v.(json.Number)
	if err != nil || !ok {
		return 0, false
	}
	f, err := n.Float64()
	return f, err == nil && !math.IsInf(f, 0) && !math.IsNaN(f)
}
func isNonemptyString(v any) bool { s, ok := v.(string); return ok && strings.TrimSpace(s) != "" }
func boundedNumber(v any, min, max float64) bool {
	var f float64
	switch n := v.(type) {
	case json.Number:
		var err error
		f, err = n.Float64()
		if err != nil {
			return false
		}
	case float64:
		f = n
	default:
		return false
	}
	return !math.IsInf(f, 0) && !math.IsNaN(f) && f >= min && f <= max
}
func put(m map[string]json.RawMessage, key string, value any) {
	raw, _ := json.Marshal(value)
	m[key] = raw
}
func diagnostic(code, source, field, severity, message string) contracts.Diagnostic {
	return contracts.Diagnostic{Code: code, Source: source, Field: field, Severity: severity, Message: message}
}
