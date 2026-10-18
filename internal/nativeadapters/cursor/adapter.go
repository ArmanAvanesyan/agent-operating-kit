// Package cursor maps Cursor project subagent Markdown files to an explicitly
// incomplete AOK projection. It never discovers or runs Cursor agents.
package cursor

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

const adapterVersion = "cursor-subagents-v1"

// New returns the inert project-scoped Cursor subagent Markdown adapter.
func New() nativeadapter.Adapter { return adapter{} }

type adapter struct{}

func (adapter) Descriptor() nativeadapter.Descriptor {
	return nativeadapter.Descriptor{
		Target: "cursor", AdapterVersion: adapterVersion, Scopes: []string{"project"},
		HostLoading: nativeadapter.HostLoadingEvidence{Status: "not-run"},
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
		return nativeadapter.Document{}, nil, errors.New("Cursor adapter requires one project Markdown file")
	}
	f := files[0]
	if !markdownFormat(f.Format) || !validLocation(f.Path) {
		return nativeadapter.Document{}, nil, errors.New("Cursor subagent path or format is invalid")
	}
	meta, body, err := nativeparse.Frontmatter(f.Data)
	if err != nil {
		return nativeadapter.Document{}, nil, errors.New("Cursor subagent frontmatter is invalid")
	}
	name, hasName := metaString(meta, "name")
	if !hasName {
		if _, explicitlySet := meta["name"]; explicitlySet {
			return nativeadapter.Document{}, nil, errors.New("Cursor subagent name must be a non-empty string")
		}
		name = strings.TrimSuffix(path.Base(f.Path), path.Ext(f.Path))
	}
	if !validAgentName(name) {
		return nativeadapter.Document{}, nil, errors.New("Cursor subagent name is missing or invalid")
	}
	fields := map[string]json.RawMessage{
		"agent.metadata.id":          mustJSON(name),
		"agent.instructions.sources": mustJSON([]map[string]string{{"path": "instructions.md"}}),
	}
	if description, ok := metaString(meta, "description"); ok {
		fields["agent.identity.purpose"] = mustJSON(description)
	} else if _, exists := meta["description"]; exists {
		return nativeadapter.Document{}, nil, errors.New("Cursor description must be a string")
	}
	if model, exists := meta["model"]; exists {
		text, ok := model.(string)
		if !ok || strings.TrimSpace(text) == "" {
			return nativeadapter.Document{}, nil, errors.New("Cursor model must be a non-empty string")
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
			return nativeadapter.Document{}, nil, errors.New("Cursor frontmatter contains an unsupported value")
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
		return nil, nil, errors.New("Cursor adapter supports project scope only")
	}
	name, err := rawString(doc.Fields["agent.metadata.id"])
	if err != nil || !validAgentName(name) {
		return nil, nil, errors.New("Cursor export requires a valid agent.metadata.id")
	}
	front := map[string]any{"name": name}
	if raw, ok := doc.Fields["agent.identity.purpose"]; ok {
		description, descriptionErr := rawString(raw)
		if descriptionErr != nil {
			return nil, nil, errors.New("Cursor description must be a string")
		}
		front["description"] = description
	}
	if raw, ok := doc.Fields["executionProfile.model.selector"]; ok {
		model, modelErr := rawString(raw)
		if modelErr != nil || strings.TrimSpace(model) == "" {
			return nil, nil, errors.New("Cursor model selector must be a non-empty string")
		}
		front["model"] = model
	}
	if _, ok := doc.Fields["agent.instructions.sources"]; !ok || !validInstructionSource(doc.Fields["agent.instructions.sources"]) {
		return nil, nil, errors.New("Cursor export requires the supported instructions.md source mapping")
	}
	for field := range doc.Fields {
		switch field {
		case "agent.metadata.id", "agent.identity.purpose", "executionProfile.model.selector", "agent.instructions.sources":
		default:
			return nil, nil, fmt.Errorf("Cursor adapter cannot encode canonical field %q", field)
		}
	}
	for key, raw := range doc.Unknown {
		if key == "file.name" {
			continue
		}
		if !strings.HasPrefix(key, "frontmatter.") || len(key) == len("frontmatter.") {
			return nil, nil, errors.New("Cursor adapter cannot encode unknown field path")
		}
		frontKey := strings.TrimPrefix(key, "frontmatter.")
		if frontKey == "name" || frontKey == "description" || frontKey == "model" {
			return nil, nil, errors.New("unknown Cursor frontmatter collides with a mapped field")
		}
		value, decodeErr := contracts.DecodeJSON(raw)
		if decodeErr != nil {
			return nil, nil, errors.New("Cursor unknown frontmatter is invalid JSON")
		}
		front[frontKey] = value
	}
	body, ok := doc.Assets["instructions.md"]
	if !ok || len(doc.Assets) != 1 {
		return nil, nil, errors.New("Cursor export requires exactly one instructions.md asset")
	}
	metadata, err := nativeparse.Encode("yaml", front)
	if err != nil {
		return nil, nil, errors.New("Cursor frontmatter cannot be encoded")
	}
	data := make([]byte, 0, len(metadata)+len(body)+8)
	data = append(data, []byte("---\n")...)
	data = append(data, metadata...)
	data = append(data, []byte("---\n")...)
	data = append(data, body...)
	file := defaultPath(name)
	if raw, exists := doc.Unknown["file.name"]; exists {
		retained, pathErr := rawString(raw)
		if pathErr != nil || !validLocation(retained) {
			return nil, nil, errors.New("retained Cursor filename is invalid")
		}
		file = retained
	}
	return []nativeadapter.File{{Path: file, Format: "markdown", Data: data}}, nil, nil
}

func defaultPath(name string) string { return ".cursor/agents/" + name + ".md" }

func validLocation(file string) bool {
	if path.Clean(file) != file || strings.Contains(file, "\\") || path.IsAbs(file) || !strings.HasPrefix(file, ".cursor/agents/") || !strings.HasSuffix(file, ".md") {
		return false
	}
	return len(strings.TrimPrefix(file, ".cursor/agents/")) > 3
}

func markdownFormat(format string) bool {
	f := strings.ToLower(strings.TrimSpace(format))
	return f == "markdown" || f == "md"
}

func validAgentName(name string) bool {
	return strings.TrimSpace(name) != "" && len(name) <= 256 && name != "." && name != ".." && !strings.ContainsAny(name, ":/\\")
}

func metaString(meta map[string]any, key string) (string, bool) {
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
