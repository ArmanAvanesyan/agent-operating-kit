// Package gemini maps Gemini CLI project subagent Markdown to an incomplete,
// inert AOK projection. It does not run or install Gemini CLI.
package gemini

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

const adapterVersion = "gemini-cli-subagents-v1"

// New returns the inert project-scoped Gemini CLI subagent adapter.
func New() nativeadapter.Adapter { return adapter{} }

type adapter struct{}

func (adapter) Descriptor() nativeadapter.Descriptor {
	return nativeadapter.Descriptor{
		Target: "gemini", AdapterVersion: adapterVersion, Scopes: []string{"project"},
		// Pinned CLI 0.61.0 displayed the generated agent in its project discovery
		// UI and omitted it in an empty-project control before authentication.
		// This verifies project discovery only, not invocation or enforcement.
		HostLoading: nativeadapter.HostLoadingEvidence{
			Status: "verified", HostVersion: "gemini-cli@0.61.0", Scopes: []string{"project"},
			Evidence: []string{"official bundled loadAgentsFromDirectory accepted the exported fixture and rejected malformed YAML; actual CLI project discovery displayed repository-explorer with the expected description and omitted it in an empty control before authentication; invocation and runtime enforcement unverified"},
		},
		Fields: []nativeadapter.FieldCapability{
			{NeutralPath: "agent.metadata.id", NativePath: "frontmatter.name", Support: "native", EnforcementOwner: "native"},
			{NeutralPath: "agent.identity.purpose", NativePath: "frontmatter.description", Support: "native", EnforcementOwner: "native"},
			{NeutralPath: "executionProfile.model.selector", NativePath: "frontmatter.model", Support: "native", EnforcementOwner: "native"},
			{NeutralPath: "agent.instructions.sources", NativePath: "markdown.body", Support: "native", EnforcementOwner: "native"},
		},
	}
}

func (adapter) Decode(scope string, files []nativeadapter.File) (nativeadapter.Document, []contracts.Diagnostic, error) {
	if scope != "project" || len(files) != 1 {
		return nativeadapter.Document{}, nil, errors.New("Gemini adapter requires one project Markdown file")
	}
	f := files[0]
	if !markdownFormat(f.Format) || !validLocation(f.Path) {
		return nativeadapter.Document{}, nil, errors.New("Gemini subagent path or format is invalid")
	}
	meta, body, err := nativeparse.Frontmatter(f.Data)
	if err != nil {
		return nativeadapter.Document{}, nil, errors.New("Gemini subagent frontmatter is invalid")
	}
	name, ok := requiredString(meta, "name")
	if !ok || !validAgentName(name) {
		return nativeadapter.Document{}, nil, errors.New("Gemini subagent name is missing or invalid")
	}
	description, ok := requiredString(meta, "description")
	if !ok {
		return nativeadapter.Document{}, nil, errors.New("Gemini subagent description is missing or invalid")
	}
	fields := map[string]json.RawMessage{
		"agent.metadata.id":          mustJSON(name),
		"agent.identity.purpose":     mustJSON(description),
		"agent.instructions.sources": mustJSON([]map[string]string{{"path": "instructions.md"}}),
	}
	if model, exists := meta["model"]; exists {
		text, isString := model.(string)
		if !isString || strings.TrimSpace(text) == "" {
			return nativeadapter.Document{}, nil, errors.New("Gemini model must be a non-empty string")
		}
		fields["executionProfile.model.selector"] = mustJSON(text)
	}
	unknown := make(map[string]json.RawMessage, len(meta)+1)
	for key, value := range meta {
		if key == "name" || key == "description" || key == "model" {
			continue
		}
		raw, marshalErr := json.Marshal(value)
		if marshalErr != nil {
			return nativeadapter.Document{}, nil, errors.New("Gemini frontmatter contains an unsupported value")
		}
		unknown["frontmatter."+key] = raw
	}
	if f.Path != defaultPath(name) {
		unknown["file.name"] = mustJSON(f.Path)
	}
	return nativeadapter.Document{Fields: fields, Unknown: unknown, Assets: map[string][]byte{"instructions.md": append([]byte(nil), body...)}}, nil, nil
}

func (adapter) Encode(scope string, doc nativeadapter.Document) ([]nativeadapter.File, []contracts.Diagnostic, error) {
	if scope != "project" {
		return nil, nil, errors.New("Gemini adapter supports project scope only")
	}
	name, err := rawString(doc.Fields["agent.metadata.id"])
	if err != nil || !validAgentName(name) {
		return nil, nil, errors.New("Gemini export requires a valid agent.metadata.id")
	}
	description, err := rawString(doc.Fields["agent.identity.purpose"])
	if err != nil || strings.TrimSpace(description) == "" {
		return nil, nil, errors.New("Gemini export requires agent.identity.purpose")
	}
	front := map[string]any{"name": name, "description": description}
	if raw, ok := doc.Fields["executionProfile.model.selector"]; ok {
		model, modelErr := rawString(raw)
		if modelErr != nil || strings.TrimSpace(model) == "" {
			return nil, nil, errors.New("Gemini model selector must be a non-empty string")
		}
		front["model"] = model
	}
	for field := range doc.Fields {
		switch field {
		case "agent.metadata.id", "agent.identity.purpose", "executionProfile.model.selector", "agent.instructions.sources":
		default:
			return nil, nil, fmt.Errorf("Gemini adapter cannot encode canonical field %q", field)
		}
	}
	if raw, ok := doc.Fields["agent.instructions.sources"]; !ok || !validInstructionSource(raw) {
		return nil, nil, errors.New("Gemini export requires the supported instructions.md source mapping")
	}
	for key, raw := range doc.Unknown {
		if key == "file.name" {
			continue
		}
		if !strings.HasPrefix(key, "frontmatter.") || len(key) == len("frontmatter.") {
			return nil, nil, errors.New("Gemini adapter cannot encode unknown field path")
		}
		frontKey := strings.TrimPrefix(key, "frontmatter.")
		if frontKey == "name" || frontKey == "description" || frontKey == "model" {
			return nil, nil, errors.New("unknown Gemini frontmatter collides with a mapped field")
		}
		value, decodeErr := contracts.DecodeJSON(raw)
		if decodeErr != nil {
			return nil, nil, errors.New("Gemini unknown frontmatter is invalid JSON")
		}
		front[frontKey] = value
	}
	body, ok := doc.Assets["instructions.md"]
	if !ok || len(doc.Assets) != 1 {
		return nil, nil, errors.New("Gemini export requires exactly one instructions.md asset")
	}
	metadata, err := nativeparse.Encode("yaml", front)
	if err != nil {
		return nil, nil, errors.New("Gemini frontmatter cannot be encoded")
	}
	data := make([]byte, 0, len(metadata)+len(body)+8)
	data = append(data, []byte("---\n")...)
	data = append(data, metadata...)
	data = append(data, []byte("---\n")...)
	data = append(data, body...)
	filePath := defaultPath(name)
	if raw, exists := doc.Unknown["file.name"]; exists {
		retained, pathErr := rawString(raw)
		if pathErr != nil || !validLocation(retained) {
			return nil, nil, errors.New("retained Gemini filename is invalid")
		}
		filePath = retained
	}
	return []nativeadapter.File{{Path: filePath, Format: "markdown", Data: data}}, nil, nil
}

func defaultPath(name string) string { return ".gemini/agents/" + name + ".md" }

func validLocation(file string) bool {
	if path.Clean(file) != file || strings.Contains(file, "\\") || path.IsAbs(file) || !strings.HasPrefix(file, ".gemini/agents/") || !strings.HasSuffix(file, ".md") {
		return false
	}
	relative := strings.TrimPrefix(file, ".gemini/agents/")
	return relative != "" && !strings.Contains(relative, "/") && relative != "." && relative != ".."
}

func markdownFormat(format string) bool {
	f := strings.ToLower(strings.TrimSpace(format))
	return f == "markdown" || f == "md"
}

func validAgentName(name string) bool {
	if name == "" || len(name) > 128 {
		return false
	}
	for _, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

func requiredString(meta map[string]any, key string) (string, bool) {
	value, ok := meta[key].(string)
	return value, ok && strings.TrimSpace(value) != ""
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
