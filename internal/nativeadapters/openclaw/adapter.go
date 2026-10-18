// Package openclaw provides a pure projection to OpenClaw's home-scoped
// openclaw.json roster and workspace bootstrap file.
package openclaw

import (
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/nativeadapter"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/nativeparse"
)

const (
	target        = "openclaw"
	version       = "0.1.0"
	scope         = "isolated-home"
	configPath    = ".openclaw/openclaw.json"
	workspacePath = ".openclaw/workspace/AGENTS.md"
	assetName     = "instructions.md"
	workspace     = "~/.openclaw/workspace"
)

type Adapter struct{}

func New() nativeadapter.Adapter { return Adapter{} }

func (Adapter) Descriptor() nativeadapter.Descriptor {
	return nativeadapter.Descriptor{
		Target: target, AdapterVersion: version, Scopes: []string{scope},
		HostLoading: nativeadapter.HostLoadingEvidence{Status: "unavailable", Scopes: []string{scope}},
		Fields: []nativeadapter.FieldCapability{
			{NeutralPath: "agent.metadata.id", NativePath: "agents.entries.<id> (key)", Support: "native", EnforcementOwner: "native"},
			{NeutralPath: "agent.identity.callsign", NativePath: "agents.entries.<id>.identity.name", Support: "native", EnforcementOwner: "native"},
			{NeutralPath: "executionProfile.model.selector", NativePath: "agents.entries.<id>.model", Support: "native", EnforcementOwner: "native"},
			{NeutralPath: "agent.instructions.sources", NativePath: "<workspace>/AGENTS.md", Support: "native", EnforcementOwner: "native"},
			{NeutralPath: "agent.identity.purpose", NativePath: "unsupported.identity-purpose", Support: "unsupported", EnforcementOwner: "none"},
			{NeutralPath: "agent.permissions.filesystem.read", NativePath: "unsupported.filesystem-read", Support: "unsupported", EnforcementOwner: "none"},
			{NeutralPath: "agent.permissions.filesystem.write", NativePath: "unsupported.filesystem-write", Support: "unsupported", EnforcementOwner: "none"},
			{NeutralPath: "agent.permissions.network.mode", NativePath: "unsupported.network-mode", Support: "unsupported", EnforcementOwner: "none"},
			{NeutralPath: "agent.capabilities.mcps", NativePath: "unsupported.mcps", Support: "unsupported", EnforcementOwner: "none"},
		},
	}
}

func (Adapter) Decode(scopeName string, files []nativeadapter.File) (nativeadapter.Document, []contracts.Diagnostic, error) {
	if scopeName != scope || len(files) != 2 {
		return nativeadapter.Document{}, nil, errors.New("expected an isolated-home OpenClaw config and one workspace instruction file")
	}
	var configFile, instructions nativeadapter.File
	for _, f := range files {
		switch f.Path {
		case configPath:
			configFile = f
		case workspacePath:
			instructions = f
		default:
			return nativeadapter.Document{}, nil, errors.New("input contains a path outside the supported OpenClaw config/workspace files")
		}
	}
	if configFile.Path == "" || instructions.Path == "" || (configFile.Format != "json" && configFile.Format != "jsonc") || instructions.Format != "markdown" {
		return nativeadapter.Document{}, nil, errors.New("expected .openclaw/openclaw.json (JSON or JSONC) and .openclaw/workspace/AGENTS.md (Markdown)")
	}
	if !utf8.Valid(instructions.Data) || strings.HasPrefix(string(instructions.Data), "\ufeff") {
		return nativeadapter.Document{}, nil, errors.New("workspace instructions must be UTF-8 without BOM")
	}
	root, err := nativeparse.Decode(configFile.Format, configFile.Data)
	if err != nil {
		return nativeadapter.Document{}, nil, err
	}
	agents, ok := object(root["agents"])
	if !ok {
		return nativeadapter.Document{}, nil, errors.New("agents must be an object")
	}
	entries, ok := object(agents["entries"])
	if !ok || len(entries) != 1 {
		return nativeadapter.Document{}, nil, errors.New("exactly one keyed agents.entries agent is supported")
	}
	if defaults, exists := agents["defaults"]; exists {
		defaultsObject, valid := object(defaults)
		if !valid {
			return nativeadapter.Document{}, nil, errors.New("agents.defaults must be an object")
		}
		if value, configured := defaultsObject["workspace"]; configured && value != workspace {
			return nativeadapter.Document{}, nil, errors.New("custom default workspace paths are unsupported; expected the isolated-home default workspace")
		}
	}
	var id string
	var entry map[string]any
	for key, value := range entries {
		id = key
		var valid bool
		entry, valid = object(value)
		if !valid {
			return nativeadapter.Document{}, nil, errors.New("agent entry must be an object")
		}
	}
	if !safeID(id) {
		return nativeadapter.Document{}, nil, errors.New("agent entry key is not a safe canonical identifier")
	}
	if w, exists := entry["workspace"]; exists && w != workspace {
		return nativeadapter.Document{}, nil, errors.New("custom workspace paths are unsupported; expected the isolated-home default workspace")
	}
	if entry["workspace"] == nil {
		entry["workspace"] = workspace
	}
	fields := map[string]json.RawMessage{}
	unknown := map[string]json.RawMessage{}
	assets := map[string][]byte{assetName: append([]byte(nil), instructions.Data...)}
	put(fields, "agent.metadata.id", id)
	identity := map[string]any{}
	if value, exists := entry["identity"]; exists {
		var valid bool
		identity, valid = object(value)
		if !valid {
			return nativeadapter.Document{}, nil, errors.New("agent identity must be an object")
		}
	}
	if value, exists := identity["name"]; exists {
		name, valid := value.(string)
		if !valid || strings.TrimSpace(name) == "" {
			return nativeadapter.Document{}, nil, errors.New("agent identity name must be a non-empty string")
		}
		put(fields, "agent.identity.callsign", name)
	}
	if value, exists := entry["model"]; exists {
		model, valid := value.(string)
		if !valid || strings.TrimSpace(model) == "" {
			return nativeadapter.Document{}, nil, errors.New("only a non-empty string per-agent model is supported")
		}
		put(fields, "executionProfile.model.selector", model)
	}
	put(fields, "agent.instructions.sources", []map[string]string{{"path": assetName}})
	// Retain all config not represented in the AOK projection. The selected
	// entry's identity.name/model are separated so Encode can merge edits back.
	for k, v := range root {
		if k != "agents" {
			saveUnknown(unknown, "config."+k, v)
		}
	}
	for k, v := range agents {
		if k != "entries" {
			saveUnknown(unknown, "agents."+k, v)
		}
	}
	for k, v := range entry {
		if k == "identity" || k == "model" || k == "workspace" {
			continue
		}
		saveUnknown(unknown, "entry."+k, v)
	}
	for k, v := range identity {
		if k != "name" {
			saveUnknown(unknown, "identity."+k, v)
		}
	}
	return nativeadapter.Document{Fields: fields, Unknown: unknown, Assets: assets}, nil, nil
}

func (Adapter) Encode(scopeName string, doc nativeadapter.Document) ([]nativeadapter.File, []contracts.Diagnostic, error) {
	if scopeName != scope {
		return nil, nil, errors.New("unsupported OpenClaw scope")
	}
	allowed := map[string]bool{"agent.metadata.id": true, "agent.identity.callsign": true, "executionProfile.model.selector": true, "agent.instructions.sources": true}
	for k := range doc.Fields {
		if !allowed[k] {
			return nil, nil, fmt.Errorf("unsupported mapped field %q", k)
		}
	}
	id, err := requiredString(doc.Fields, "agent.metadata.id")
	if err != nil || !safeID(id) {
		return nil, nil, errors.New("agent.metadata.id must be a safe OpenClaw entry key")
	}
	body, err := instructionBody(doc)
	if err != nil {
		return nil, nil, err
	}
	entry := map[string]any{"workspace": workspace}
	if raw, ok := doc.Fields["agent.identity.callsign"]; ok {
		name, good := rawString(raw)
		if !good || strings.TrimSpace(name) == "" {
			return nil, nil, errors.New("identity.callsign must be a non-empty string")
		}
		entry["identity"] = map[string]any{"name": name}
	}
	if raw, ok := doc.Fields["executionProfile.model.selector"]; ok {
		model, good := rawString(raw)
		if !good || strings.TrimSpace(model) == "" {
			return nil, nil, errors.New("model selector must be a non-empty string")
		}
		entry["model"] = model
	}
	root := map[string]any{"agents": map[string]any{"entries": map[string]any{id: entry}}}
	for key, raw := range doc.Unknown {
		value, e := contracts.DecodeJSON(raw)
		if e != nil {
			return nil, nil, errors.New("unknown native field is invalid")
		}
		if err := mergeUnknown(root, entry, key, value); err != nil {
			return nil, nil, err
		}
	}
	data, err := nativeparse.Encode("json", root)
	if err != nil {
		return nil, nil, errors.New("OpenClaw config cannot be represented as JSON")
	}
	return []nativeadapter.File{{Path: configPath, Format: "json", Data: data}, {Path: workspacePath, Format: "markdown", Data: body}}, nil, nil
}

func instructionBody(doc nativeadapter.Document) ([]byte, error) {
	raw, ok := doc.Fields["agent.instructions.sources"]
	if !ok {
		return nil, errors.New("one workspace instruction source is required")
	}
	v, err := contracts.DecodeJSON(raw)
	if err != nil {
		return nil, errors.New("instruction source is invalid")
	}
	a, ok := v.([]any)
	if !ok || len(a) != 1 {
		return nil, errors.New("exactly one workspace instruction source is supported")
	}
	m, ok := a[0].(map[string]any)
	if !ok || len(m) != 1 || m["path"] != assetName {
		return nil, errors.New("instruction source must point to the materialized instructions asset")
	}
	b, ok := doc.Assets[assetName]
	if !ok || len(doc.Assets) != 1 || !utf8.Valid(b) || strings.HasPrefix(string(b), "\ufeff") {
		return nil, errors.New("one valid UTF-8 instruction asset is required")
	}
	return append([]byte(nil), b...), nil
}

func mergeUnknown(root, entry map[string]any, key string, value any) error {
	namespace, name, ok := strings.Cut(key, ".")
	if !ok || name == "" {
		return errors.New("unknown native path is invalid")
	}
	var at map[string]any
	switch namespace {
	case "config":
		if name == "agents" {
			return errors.New("unknown config collides with agent roster")
		}
		at = root
	case "agents":
		if name == "entries" {
			return errors.New("unknown agents field collides with entries")
		}
		at = root["agents"].(map[string]any)
	case "entry":
		if name == "identity" || name == "model" || name == "workspace" {
			return errors.New("unknown entry field collides with mapped configuration")
		}
		at = entry
	case "identity":
		if name == "name" {
			return errors.New("unknown identity field collides with name")
		}
		var exists bool
		at, exists = object(entry["identity"])
		if !exists {
			at = map[string]any{}
			entry["identity"] = at
		}
	default:
		return errors.New("unsupported unknown namespace")
	}
	if _, exists := at[name]; exists {
		return errors.New("unknown native field collision")
	}
	at[name] = value
	return nil
}

func safeID(s string) bool {
	if s == "" || s == "." || s == ".." || strings.ContainsAny(s, "/\\") || path.Clean(s) != s {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}
func object(v any) (map[string]any, bool)                   { m, ok := v.(map[string]any); return m, ok }
func put(dst map[string]json.RawMessage, key string, v any) { b, _ := json.Marshal(v); dst[key] = b }
func saveUnknown(dst map[string]json.RawMessage, key string, v any) {
	b, _ := json.Marshal(v)
	dst[key] = b
}
func requiredString(fields map[string]json.RawMessage, key string) (string, error) {
	raw, ok := fields[key]
	if !ok {
		return "", fmt.Errorf("%s is required", key)
	}
	s, ok := rawString(raw)
	if !ok {
		return "", fmt.Errorf("%s must be a string", key)
	}
	return s, nil
}
func rawString(raw json.RawMessage) (string, bool) {
	var s string
	err := json.Unmarshal(raw, &s)
	return s, err == nil
}
