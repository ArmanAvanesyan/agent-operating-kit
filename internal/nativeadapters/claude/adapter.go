// Package claude maps Claude Code custom subagent Markdown files to an
// explicitly incomplete AOK projection. It never loads or executes a subagent.
package claude

import (
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/nativeadapter"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/nativeparse"
)

const adapterVersion = "claude-subagents-v1"

// New returns the inert Claude Code subagent Markdown adapter.
func New() nativeadapter.Adapter { return adapter{} }

type adapter struct{}

func (adapter) Descriptor() nativeadapter.Descriptor {
	return nativeadapter.Descriptor{
		Target: "claude-code", AdapterVersion: adapterVersion,
		Scopes: []string{"project", "plugin"}, HostLoading: nativeadapter.HostLoadingEvidence{Status: "verified", HostVersion: "2.1.281", Scopes: []string{"project"}, Evidence: []string{"docs/evidence/native-adapters/claude-2.1.281.json"}},
		Fields: []nativeadapter.FieldCapability{
			{NeutralPath: "agent.metadata.id", NativePath: "frontmatter.name", Support: "native", EnforcementOwner: "native"},
			{NeutralPath: "agent.identity.purpose", NativePath: "frontmatter.description", Support: "native", EnforcementOwner: "native"},
			{NeutralPath: "executionProfile.model.selector", NativePath: "frontmatter.model", Support: "native", EnforcementOwner: "native"},
			{NeutralPath: "agent.instructions.sources", NativePath: "markdown.body", Support: "native", EnforcementOwner: "native"},
			// These recognized Claude controls are intentionally not mapped to
			// canonical AOK capability references. Required requests stay blocked.
			{NeutralPath: "agent.capabilities.tools", NativePath: "frontmatter.tools", Support: "unsupported", EnforcementOwner: "none"},
			{NeutralPath: "agent.permissions.process", NativePath: "frontmatter.disallowedTools", Support: "unsupported", EnforcementOwner: "none"},
			{NeutralPath: "agent.permissions", NativePath: "frontmatter.permissionMode", Support: "unsupported", EnforcementOwner: "none"},
			{NeutralPath: "agent.capabilities.mcps", NativePath: "frontmatter.mcpServers", Support: "unsupported", EnforcementOwner: "none"},
			{NeutralPath: "agent.lifecyclePolicy", NativePath: "frontmatter.hooks", Support: "unsupported", EnforcementOwner: "none"},
			{NeutralPath: "agent.capabilities.skills", NativePath: "frontmatter.skills", Support: "unsupported", EnforcementOwner: "none"},
		},
	}
}

func (adapter) Decode(scope string, files []nativeadapter.File) (nativeadapter.Document, []contracts.Diagnostic, error) {
	if !validScope(scope) || len(files) != 1 {
		return nativeadapter.Document{}, nil, errors.New("Claude adapter requires one Markdown file and a supported scope")
	}
	f := files[0]
	if !markdownFormat(f.Format) || !validLocation(scope, f.Path) {
		return nativeadapter.Document{}, nil, errors.New("Claude subagent file path or format does not match scope")
	}
	meta, body, err := nativeparse.Frontmatter(f.Data)
	if err != nil {
		return nativeadapter.Document{}, nil, errors.New("Claude subagent frontmatter is invalid")
	}
	name, err := requiredString(meta, "name")
	if err != nil || !validAgentName(name) {
		return nativeadapter.Document{}, nil, errors.New("Claude subagent name is missing or invalid")
	}
	description, err := requiredString(meta, "description")
	if err != nil {
		return nativeadapter.Document{}, nil, errors.New("Claude subagent description is missing or invalid")
	}
	fields := map[string]json.RawMessage{
		"agent.metadata.id":          mustJSON(name),
		"agent.identity.purpose":     mustJSON(description),
		"agent.instructions.sources": mustJSON([]map[string]string{{"path": "instructions.md"}}),
	}
	unknown := make(map[string]json.RawMessage, len(meta)+1)
	for key, value := range meta {
		switch key {
		case "name", "description":
			continue
		case "model":
			model, ok := value.(string)
			if !ok || strings.TrimSpace(model) == "" {
				return nativeadapter.Document{}, nil, errors.New("Claude model field must be a non-empty string")
			}
			fields["executionProfile.model.selector"] = mustJSON(model)
			continue
		}
		raw, encodeErr := json.Marshal(value)
		if encodeErr != nil {
			return nativeadapter.Document{}, nil, errors.New("Claude frontmatter contains an unsupported value")
		}
		unknown["frontmatter."+key] = raw
	}
	if strings.TrimSpace(f.Path) != defaultPath(scope, name) {
		unknown["file.name"] = mustJSON(f.Path)
	}
	diagnostics := []contracts.Diagnostic{}
	if scope == "plugin" {
		for _, key := range []string{"hooks", "mcpServers", "permissionMode"} {
			if _, found := meta[key]; found {
				diagnostics = append(diagnostics, contracts.Diagnostic{Code: "claude.plugin.field-ignored", Source: "claude-code", Field: "frontmatter." + key, Severity: "warning", Message: "Claude Code ignores this field for plugin subagents; it is retained as inert native data"})
			}
		}
	}
	return nativeadapter.Document{Fields: fields, Unknown: unknown, Assets: map[string][]byte{"instructions.md": append([]byte(nil), body...)}}, diagnostics, nil
}

func (adapter) Encode(scope string, doc nativeadapter.Document) ([]nativeadapter.File, []contracts.Diagnostic, error) {
	if !validScope(scope) {
		return nil, nil, errors.New("unsupported Claude subagent scope")
	}
	name, err := rawString(doc.Fields["agent.metadata.id"])
	if err != nil || !validAgentName(name) {
		return nil, nil, errors.New("Claude export requires a valid agent.metadata.id")
	}
	description, err := rawString(doc.Fields["agent.identity.purpose"])
	if err != nil {
		return nil, nil, errors.New("Claude export requires agent.identity.purpose")
	}
	front := map[string]any{"name": name, "description": description}
	if raw, ok := doc.Fields["executionProfile.model.selector"]; ok {
		model, modelErr := rawString(raw)
		if modelErr != nil || strings.TrimSpace(model) == "" {
			return nil, nil, errors.New("Claude model selector must be a non-empty string")
		}
		front["model"] = model
	}
	for neutral := range doc.Fields {
		switch neutral {
		case "agent.metadata.id", "agent.identity.purpose", "agent.instructions.sources", "executionProfile.model.selector":
		default:
			return nil, nil, fmt.Errorf("Claude adapter cannot encode canonical field %q", neutral)
		}
	}
	for key, raw := range doc.Unknown {
		if key == "file.name" {
			continue
		}
		if !strings.HasPrefix(key, "frontmatter.") || len(key) == len("frontmatter.") {
			return nil, nil, errors.New("Claude adapter cannot encode unknown field path")
		}
		value, decodeErr := contracts.DecodeJSON(raw)
		if decodeErr != nil {
			return nil, nil, errors.New("Claude unknown frontmatter is invalid JSON")
		}
		frontKey := strings.TrimPrefix(key, "frontmatter.")
		if frontKey == "name" || frontKey == "description" || frontKey == "model" {
			return nil, nil, errors.New("unknown frontmatter collides with a mapped field")
		}
		front[frontKey] = value
	}
	if raw, ok := doc.Fields["agent.instructions.sources"]; !ok || !validInstructionSource(raw) {
		return nil, nil, errors.New("Claude export requires the supported instructions.md source mapping")
	}
	asset, ok := doc.Assets["instructions.md"]
	if !ok {
		return nil, nil, errors.New("Claude export requires the instructions.md asset")
	}
	if len(doc.Assets) != 1 {
		return nil, nil, errors.New("Claude adapter cannot encode additional instruction assets")
	}
	metadata, err := nativeparse.Encode("yaml", front)
	if err != nil {
		return nil, nil, errors.New("Claude frontmatter cannot be encoded")
	}
	data := make([]byte, 0, len(metadata)+len(asset)+9)
	data = append(data, []byte("---\n")...)
	data = append(data, metadata...)
	data = append(data, []byte("---\n")...)
	data = append(data, asset...)
	filePath := defaultPath(scope, name)
	if raw, ok := doc.Unknown["file.name"]; ok {
		retained, pathErr := rawString(raw)
		if pathErr != nil || !validLocation(scope, retained) {
			return nil, nil, errors.New("retained Claude subagent filename is invalid for scope")
		}
		filePath = retained
	}
	return []nativeadapter.File{{Path: filePath, Format: "markdown", Data: data}}, nil, nil
}

func defaultPath(scope, name string) string {
	if scope == "plugin" {
		return "agents/" + name + ".md"
	}
	return ".claude/agents/" + name + ".md"
}

func validLocation(scope, file string) bool {
	if !validScope(scope) || path.Clean(file) != file || strings.Contains(file, "\\") || path.IsAbs(file) {
		return false
	}
	prefix := ".claude/agents/"
	if scope == "plugin" {
		prefix = "agents/"
	}
	return strings.HasPrefix(file, prefix) && strings.HasSuffix(file, ".md") && len(strings.TrimPrefix(file, prefix)) > 3
}

func validScope(scope string) bool { return scope == "project" || scope == "plugin" }
func markdownFormat(format string) bool {
	f := strings.ToLower(strings.TrimSpace(format))
	return f == "markdown" || f == "md"
}
func validAgentName(name string) bool {
	if strings.TrimSpace(name) == "" || len(name) > 256 || strings.ContainsAny(name, ":/\\") || name == "." || name == ".." {
		return false
	}
	return true
}
func requiredString(meta map[string]any, key string) (string, error) {
	value, ok := meta[key].(string)
	if !ok || strings.TrimSpace(value) == "" {
		return "", errors.New("required string field is missing")
	}
	return value, nil
}
func rawString(raw json.RawMessage) (string, error) {
	var value string
	if len(raw) == 0 || json.Unmarshal(raw, &value) != nil {
		return "", errors.New("value is not a string")
	}
	return value, nil
}
func validInstructionSource(raw json.RawMessage) bool {
	var value []map[string]string
	return json.Unmarshal(raw, &value) == nil && len(value) == 1 && len(value[0]) == 1 && value[0]["path"] == "instructions.md"
}
func mustJSON(value any) json.RawMessage {
	b, _ := json.Marshal(value)
	return b
}
