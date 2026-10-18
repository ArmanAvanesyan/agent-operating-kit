// Package agy projects a small, loss-aware subset to Antigravity CLI's native
// Markdown custom-agent format.
package agy

import (
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/nativeadapter"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/nativeparse"
)

const (
	target       = "agy"
	version      = "0.1.0"
	projectScope = "project"
	agentsDir    = ".agents/agents/"
	assetName    = "instructions.md"
)

var canonicalID = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)
var nativeName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

type Adapter struct{}

func New() nativeadapter.Adapter { return Adapter{} }

func (Adapter) Descriptor() nativeadapter.Descriptor {
	return nativeadapter.Descriptor{
		Target: target, AdapterVersion: version, Scopes: []string{projectScope},
		HostLoading: nativeadapter.HostLoadingEvidence{
			Status: "not-run", Scopes: []string{projectScope}, HostVersion: "1.4.2",
			Evidence: []string{
				"docs/evidence/native-adapters/agy-1.4.2-fresh-loading-2026.10.18.json",
				"docs/evidence/native-adapters/agy-linux-container-2026-10-18.json",
				"docs/evidence/native-adapters/agy-onboarding-2026-10-18/local-manager-final.json",
				"docs/evidence/native-adapters/agy-account-2026-10-18/account-manager-final.json",
			},
		},
		Fields: []nativeadapter.FieldCapability{
			{NeutralPath: "agent.metadata.id", NativePath: "frontmatter.name", Support: "native", EnforcementOwner: "native"},
			{NeutralPath: "agent.identity.purpose", NativePath: "frontmatter.description", Support: "native", EnforcementOwner: "native"},
			{NeutralPath: "executionProfile.model.selector", NativePath: "frontmatter.model", Support: "native", EnforcementOwner: "native"},
			{NeutralPath: "agent.instructions.sources", NativePath: "markdown.body", Support: "native", EnforcementOwner: "native"},
			{NeutralPath: "agent.capabilities.tools", NativePath: "unsupported.tools", Support: "unsupported", EnforcementOwner: "none"},
			{NeutralPath: "agent.permissions.filesystem.read", NativePath: "unsupported.filesystem.read", Support: "unsupported", EnforcementOwner: "none"},
			{NeutralPath: "agent.permissions.filesystem.write", NativePath: "unsupported.filesystem.write", Support: "unsupported", EnforcementOwner: "none"},
			{NeutralPath: "agent.permissions.network.mode", NativePath: "unsupported.network.mode", Support: "unsupported", EnforcementOwner: "none"},
			{NeutralPath: "agent.capabilities.mcps", NativePath: "unsupported.mcps", Support: "unsupported", EnforcementOwner: "none"},
		},
	}
}

func (Adapter) Decode(scope string, files []nativeadapter.File) (nativeadapter.Document, []contracts.Diagnostic, error) {
	if scope != projectScope || len(files) != 1 {
		return nativeadapter.Document{}, nil, errors.New("expected one project-scoped Antigravity agent Markdown file")
	}
	f := files[0]
	if f.Format != "markdown" {
		return nativeadapter.Document{}, nil, errors.New("expected Markdown custom-agent input")
	}
	pathName, ok := agentIDFromPath(f.Path)
	if !ok {
		return nativeadapter.Document{}, nil, errors.New("expected .agents/agents/<name>.md or .agents/agents/<name>/agent.md")
	}
	front, body, err := nativeparse.Frontmatter(f.Data)
	if err != nil {
		return nativeadapter.Document{}, nil, err
	}
	name, err := nonemptyString(front, "name")
	if err != nil {
		return nativeadapter.Document{}, nil, err
	}
	if name != pathName {
		return nativeadapter.Document{}, nil, errors.New("frontmatter name must match the native agent path")
	}
	description, err := nonemptyString(front, "description")
	if err != nil {
		return nativeadapter.Document{}, nil, err
	}
	fields := map[string]json.RawMessage{}
	unknown := map[string]json.RawMessage{}
	put(fields, "agent.metadata.id", name)
	put(fields, "agent.identity.purpose", description)
	if value, exists := front["model"]; exists {
		model, valid := value.(string)
		if !valid || !allowedModel(model) {
			return nativeadapter.Document{}, nil, errors.New("model must be one of inherit, flash, or pro")
		}
		put(fields, "executionProfile.model.selector", model)
	}
	for key, value := range front {
		if key == "name" || key == "description" || key == "model" {
			continue
		}
		raw, marshalErr := json.Marshal(value)
		if marshalErr != nil {
			return nativeadapter.Document{}, nil, errors.New("frontmatter value cannot be preserved")
		}
		unknown["frontmatter."+key] = raw
	}
	fields["agent.instructions.sources"] = json.RawMessage(`[ {"path":"instructions.md"} ]`)
	diagnostics := []contracts.Diagnostic{}
	if !canonicalID.MatchString(name) {
		diagnostics = append(diagnostics, contracts.Diagnostic{Code: "agy.agent-id.slug", Source: f.Path, Field: "agent.metadata.id", Severity: "warning", Message: "native name is not a canonical AOK slug; name is preserved"})
	}
	return nativeadapter.Document{Fields: fields, Unknown: unknown, Assets: map[string][]byte{assetName: append([]byte(nil), body...)}}, diagnostics, nil
}

func (Adapter) Encode(scope string, doc nativeadapter.Document) ([]nativeadapter.File, []contracts.Diagnostic, error) {
	if scope != projectScope {
		return nil, nil, errors.New("unsupported Antigravity scope")
	}
	allowed := map[string]bool{"agent.metadata.id": true, "agent.identity.purpose": true, "executionProfile.model.selector": true, "agent.instructions.sources": true}
	for key := range doc.Fields {
		if !allowed[key] {
			return nil, nil, fmt.Errorf("unsupported mapped field %q", key)
		}
	}
	name, err := requiredString(doc.Fields, "agent.metadata.id")
	if err != nil || !safeName(name) {
		return nil, nil, errors.New("agent.metadata.id must be a safe native name")
	}
	description, err := requiredString(doc.Fields, "agent.identity.purpose")
	if err != nil || strings.TrimSpace(description) == "" {
		return nil, nil, errors.New("agent.identity.purpose must be a non-empty string")
	}
	body, err := instructionBody(doc)
	if err != nil {
		return nil, nil, err
	}
	front := map[string]any{"name": name, "description": description}
	if raw, exists := doc.Fields["executionProfile.model.selector"]; exists {
		model, valid := rawString(raw)
		if !valid || !allowedModel(model) {
			return nil, nil, errors.New("model must be one of inherit, flash, or pro")
		}
		front["model"] = model
	}
	for key, raw := range doc.Unknown {
		if !strings.HasPrefix(key, "frontmatter.") {
			return nil, nil, fmt.Errorf("unsupported unknown native path %q", key)
		}
		name := strings.TrimPrefix(key, "frontmatter.")
		if name == "" || name == "name" || name == "description" || name == "model" {
			return nil, nil, errors.New("unknown frontmatter path must name one unmapped top-level key")
		}
		if _, exists := front[name]; exists {
			return nil, nil, fmt.Errorf("unknown frontmatter key collides with mapped key %q", name)
		}
		v, e := contracts.DecodeJSON(raw)
		if e != nil {
			return nil, nil, errors.New("unknown frontmatter value is invalid")
		}
		front[name] = v
	}
	yaml, err := nativeparse.Encode("yaml", front)
	if err != nil {
		return nil, nil, errors.New("frontmatter cannot be represented as YAML")
	}
	data := append([]byte("---\n"), yaml...)
	data = append(data, []byte("---\n")...)
	data = append(data, body...)
	diagnostics := []contracts.Diagnostic{}
	if !canonicalID.MatchString(name) {
		diagnostics = append(diagnostics, contracts.Diagnostic{Code: "agy.agent-id.slug", Field: "agent.metadata.id", Severity: "warning", Message: "native name is not a canonical AOK slug; name is preserved"})
	}
	return []nativeadapter.File{{Path: agentsDir + name + "/agent.md", Format: "markdown", Data: data}}, diagnostics, nil
}

func agentIDFromPath(file string) (string, bool) {
	if path.Clean(file) != file || strings.Contains(file, "\\") || !strings.HasPrefix(file, agentsDir) {
		return "", false
	}
	trimmed := strings.TrimPrefix(file, agentsDir)
	parts := strings.Split(trimmed, "/")
	var name string
	switch len(parts) {
	case 1:
		if !strings.HasSuffix(parts[0], ".md") {
			return "", false
		}
		name = strings.TrimSuffix(parts[0], ".md")
	case 2:
		if parts[1] != "agent.md" {
			return "", false
		}
		name = parts[0]
	default:
		return "", false
	}
	return name, safeName(name)
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
	list, ok := v.([]any)
	if !ok || len(list) != 1 {
		return nil, errors.New("exactly one instruction source is supported")
	}
	item, ok := list[0].(map[string]any)
	if !ok || len(item) != 1 || item["path"] != assetName {
		return nil, errors.New("instruction source must point to the materialized instructions asset")
	}
	if len(doc.Assets) != 1 {
		return nil, errors.New("exactly one referenced instruction asset is supported")
	}
	body, ok := doc.Assets[assetName]
	if !ok {
		return nil, errors.New("instruction asset is missing")
	}
	return append([]byte(nil), body...), nil
}

func nonemptyString(values map[string]any, key string) (string, error) {
	value, ok := values[key]
	if !ok {
		return "", fmt.Errorf("required frontmatter %q is missing", key)
	}
	s, ok := value.(string)
	if !ok || strings.TrimSpace(s) == "" {
		return "", fmt.Errorf("frontmatter %q must be a non-empty string", key)
	}
	return s, nil
}
func safeName(name string) bool {
	return nativeName.MatchString(name) && name != "." && name != ".." && !strings.ContainsAny(name, "/\\") && path.Clean(name) == name
}
func allowedModel(model string) bool { return model == "inherit" || model == "flash" || model == "pro" }
func put(dst map[string]json.RawMessage, key string, value any) {
	b, _ := json.Marshal(value)
	dst[key] = b
}
func requiredString(fields map[string]json.RawMessage, key string) (string, error) {
	raw, ok := fields[key]
	if !ok {
		return "", fmt.Errorf("%s is required", key)
	}
	value, ok := rawString(raw)
	if !ok {
		return "", fmt.Errorf("%s must be a string", key)
	}
	return value, nil
}
func rawString(raw json.RawMessage) (string, bool) {
	var value string
	err := json.Unmarshal(raw, &value)
	return value, err == nil
}
