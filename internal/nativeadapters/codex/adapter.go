// Package codex projects a bounded subset of AOK agent/profile fields into
// Codex's project-scoped custom subagent TOML format.
package codex

import (
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/evidence"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/nativeadapter"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/nativeparse"
)

const (
	target          = "codex"
	adapterVersion  = "0.2.0"
	projectScope    = "project"
	registeredScope = "project-registered"
	agentDir        = ".codex/agents/"
	projectConfig   = ".codex/config.toml"
)

var canonicalID = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)

type Adapter struct{}

func New() nativeadapter.Adapter { return Adapter{} }

func (Adapter) Descriptor() nativeadapter.Descriptor {
	fields := []nativeadapter.FieldCapability{
		{NeutralPath: "agent.metadata.id", NativePath: "toml.name", Support: "native", EnforcementOwner: "native"},
		{NeutralPath: "agent.identity.purpose", NativePath: "toml.description", Support: "native", EnforcementOwner: "native"},
		{NeutralPath: "executionProfile.model.selector", NativePath: "toml.model", Support: "native", EnforcementOwner: "native"},
		{NeutralPath: "executionProfile.model.reasoning.mode", NativePath: "toml.model_reasoning_effort.presence", Support: "native", EnforcementOwner: "native"},
		{NeutralPath: "executionProfile.model.reasoning.effort", NativePath: "toml.model_reasoning_effort", Support: "native", EnforcementOwner: "native"},
		{NeutralPath: "agent.instructions.sources", NativePath: "toml.developer_instructions", Support: "native", EnforcementOwner: "native"},
		{NeutralPath: "executionProfile.model.reasoning.budgetTokens", NativePath: "unsupported.reasoning.budgetTokens", Support: "unsupported", EnforcementOwner: "none"},
		{NeutralPath: "agent.permissions.filesystem.read", NativePath: "unsupported.permissions.filesystem.read", Support: "unsupported", EnforcementOwner: "none"},
		{NeutralPath: "agent.permissions.filesystem.write", NativePath: "unsupported.permissions.filesystem.write", Support: "unsupported", EnforcementOwner: "none"},
		{NeutralPath: "agent.permissions.network.mode", NativePath: "unsupported.permissions.network.mode", Support: "unsupported", EnforcementOwner: "none"},
		{NeutralPath: "agent.permissions.network.hosts", NativePath: "unsupported.permissions.network.hosts", Support: "unsupported", EnforcementOwner: "none"},
		{NeutralPath: "agent.permissions.secrets.mode", NativePath: "unsupported.permissions.secrets.mode", Support: "unsupported", EnforcementOwner: "none"},
		{NeutralPath: "agent.permissions.externalMutations.mode", NativePath: "unsupported.permissions.externalMutations.mode", Support: "unsupported", EnforcementOwner: "none"},
		{NeutralPath: "agent.permissions.actions.default", NativePath: "unsupported.permissions.actions.default", Support: "unsupported", EnforcementOwner: "none"},
		{NeutralPath: "agent.permissions.actions.rules", NativePath: "unsupported.permissions.actions.rules", Support: "unsupported", EnforcementOwner: "none"},
		{NeutralPath: "agent.capabilities.tools", NativePath: "unsupported.capabilities.tools", Support: "unsupported", EnforcementOwner: "none"},
		{NeutralPath: "agent.capabilities.skills", NativePath: "unsupported.capabilities.skills", Support: "unsupported", EnforcementOwner: "none"},
		{NeutralPath: "agent.capabilities.mcps", NativePath: "unsupported.capabilities.mcps", Support: "unsupported", EnforcementOwner: "none"},
		{NeutralPath: "agent.capabilities.plugins", NativePath: "unsupported.capabilities.plugins", Support: "unsupported", EnforcementOwner: "none"},
	}
	return nativeadapter.Descriptor{Target: target, AdapterVersion: adapterVersion, Scopes: []string{projectScope, registeredScope}, HostLoading: nativeadapter.HostLoadingEvidence{Status: "verified", HostVersion: "Codex CLI 0.162.1", Evidence: []string{"docs/evidence/native-adapters/codex-project-registered-0.162.1.json"}, Scopes: []string{registeredScope}}, Fields: fields}
}

func (Adapter) Decode(scope string, files []nativeadapter.File) (nativeadapter.Document, []contracts.Diagnostic, error) {
	if scope == registeredScope {
		return decodeRegistered(files)
	}
	if scope != projectScope || len(files) != 1 {
		return nativeadapter.Document{}, nil, errors.New("expected one project-scoped Codex agent TOML file")
	}
	f := files[0]
	if f.Format != "toml" || !validAgentPath(f.Path) {
		return nativeadapter.Document{}, nil, errors.New("expected .codex/agents/<name>.toml TOML input")
	}
	obj, err := nativeparse.Decode("toml", f.Data)
	if err != nil {
		return nativeadapter.Document{}, nil, err
	}
	fields := map[string]json.RawMessage{}
	unknown := map[string]json.RawMessage{}
	assets := map[string][]byte{}
	name, ok := obj["name"].(string)
	if !ok || strings.TrimSpace(name) == "" || len(name) > 128 {
		return nativeadapter.Document{}, nil, errors.New("name must be a non-empty string of at most 128 bytes")
	}
	description, ok := obj["description"].(string)
	if !ok || strings.TrimSpace(description) == "" {
		return nativeadapter.Document{}, nil, errors.New("description must be a non-empty string")
	}
	instructions, ok := obj["developer_instructions"].(string)
	if !ok {
		return nativeadapter.Document{}, nil, errors.New("developer_instructions must be a string")
	}
	put(fields, "agent.metadata.id", name)
	put(fields, "agent.identity.purpose", description)
	put(fields, "agent.instructions.sources", []map[string]string{{"path": "instructions.md"}})
	assets["instructions.md"] = []byte(instructions)
	if model, exists := obj["model"]; exists {
		s, ok := model.(string)
		if !ok || strings.TrimSpace(s) == "" {
			return nativeadapter.Document{}, nil, errors.New("model must be a non-empty string")
		}
		put(fields, "executionProfile.model.selector", s)
	}
	if effort, exists := obj["model_reasoning_effort"]; exists {
		s, ok := effort.(string)
		if !ok || !validEffort(s) {
			return nativeadapter.Document{}, nil, errors.New("model_reasoning_effort is not a supported Codex effort value")
		}
		put(fields, "executionProfile.model.reasoning.mode", "effort")
		put(fields, "executionProfile.model.reasoning.effort", s)
	} else {
		put(fields, "executionProfile.model.reasoning.mode", "provider-default")
	}
	for key, value := range obj {
		switch key {
		case "name", "description", "developer_instructions", "model", "model_reasoning_effort":
			continue
		}
		if strings.Contains(key, ".") {
			return nativeadapter.Document{}, nil, errors.New("quoted dotted top-level TOML keys are unsupported")
		}
		raw, e := json.Marshal(value)
		if e != nil {
			return nativeadapter.Document{}, nil, errors.New("TOML contains an unsupported native value")
		}
		unknown["toml."+key] = raw
	}
	var ds []contracts.Diagnostic
	filename := strings.TrimSuffix(path.Base(f.Path), ".toml")
	if filename != name {
		ds = append(ds, diag("codex.filename.name-mismatch", f.Path, "agent.metadata.id", "warning", "Codex identifies this agent by its name field; export will use that name as the filename"))
	}
	if !canonicalID.MatchString(name) {
		ds = append(ds, diag("codex.agent-id.slug", f.Path, "agent.metadata.id", "warning", "Codex agent name does not match the canonical AOK id pattern; the name is preserved"))
	}
	return nativeadapter.Document{Fields: fields, Unknown: unknown, Assets: assets}, ds, nil
}

func (Adapter) Encode(scope string, doc nativeadapter.Document) ([]nativeadapter.File, []contracts.Diagnostic, error) {
	if scope == registeredScope {
		return encodeRegistered(doc)
	}
	if scope != projectScope {
		return nil, nil, errors.New("unsupported scope")
	}
	allowed := map[string]bool{
		"agent.metadata.id": true, "agent.identity.purpose": true, "agent.instructions.sources": true,
		"executionProfile.model.selector": true, "executionProfile.model.reasoning.mode": true, "executionProfile.model.reasoning.effort": true,
	}
	for key := range doc.Fields {
		if !allowed[key] {
			return nil, nil, fmt.Errorf("unsupported mapped field %q", key)
		}
	}
	name, err := requiredString(doc.Fields, "agent.metadata.id")
	if err != nil {
		return nil, nil, err
	}
	outputPath := agentDir + name + ".toml"
	if len(name) > 128 || strings.ContainsAny(name, `/\\`) || name == "." || name == ".." || evidence.CheckPath(outputPath) != nil {
		return nil, nil, errors.New("agent name is not a valid Codex filename")
	}
	description, err := requiredString(doc.Fields, "agent.identity.purpose")
	if err != nil {
		return nil, nil, err
	}
	obj := map[string]any{"name": name, "description": description}
	if raw, exists := doc.Fields["executionProfile.model.selector"]; exists {
		model, ok := rawString(raw)
		if !ok || strings.TrimSpace(model) == "" {
			return nil, nil, errors.New("model selector must be a non-empty string")
		}
		obj["model"] = model
	}
	mode, exists := doc.Fields["executionProfile.model.reasoning.mode"]
	if exists {
		m, ok := rawString(mode)
		if !ok {
			return nil, nil, errors.New("reasoning mode must be a string")
		}
		switch m {
		case "provider-default":
			if _, hasEffort := doc.Fields["executionProfile.model.reasoning.effort"]; hasEffort {
				return nil, nil, errors.New("provider-default reasoning mode cannot include an explicit effort")
			}
		case "effort":
			effort, e := requiredString(doc.Fields, "executionProfile.model.reasoning.effort")
			if e != nil {
				return nil, nil, e
			}
			if !validEffort(effort) {
				return nil, nil, errors.New("reasoning effort is not supported by Codex")
			}
			obj["model_reasoning_effort"] = effort
		default:
			return nil, nil, fmt.Errorf("reasoning mode %q has no supported Codex equivalent", m)
		}
	} else if _, hasEffort := doc.Fields["executionProfile.model.reasoning.effort"]; hasEffort {
		return nil, nil, errors.New("reasoning effort requires mode=effort")
	}
	instructions, err := getInstructions(doc)
	if err != nil {
		return nil, nil, err
	}
	obj["developer_instructions"] = string(instructions)
	for key, raw := range doc.Unknown {
		if !strings.HasPrefix(key, "toml.") {
			return nil, nil, fmt.Errorf("unsupported unknown native path %q", key)
		}
		top := strings.TrimPrefix(key, "toml.")
		if top == "" || strings.Contains(top, ".") {
			return nil, nil, errors.New("unknown TOML path must name one top-level key")
		}
		if _, collision := obj[top]; collision {
			return nil, nil, fmt.Errorf("unknown TOML key collides with mapped key %q", top)
		}
		value, e := contracts.DecodeJSON(raw)
		if e != nil {
			return nil, nil, errors.New("unknown TOML field contains invalid JSON")
		}
		obj[top] = value
	}
	tomlBytes, err := nativeparse.Encode("toml", obj)
	if err != nil {
		return nil, nil, errors.New("agent settings cannot be represented in Codex TOML")
	}
	var ds []contracts.Diagnostic
	if !canonicalID.MatchString(name) {
		ds = append(ds, diag("codex.agent-id.slug", "", "agent.metadata.id", "warning", "canonical id is not a canonical slug; Codex name is preserved"))
	}
	return []nativeadapter.File{{Path: outputPath, Format: "toml", Data: tomlBytes}}, ds, nil
}

// decodeRegistered requires an explicit project config registration and the
// selected role file. It never reads paths from either document.
func decodeRegistered(files []nativeadapter.File) (nativeadapter.Document, []contracts.Diagnostic, error) {
	if len(files) != 2 {
		return nativeadapter.Document{}, nil, errors.New("project-registered Codex scope requires exactly .codex/config.toml and one .codex/agents/<name>.toml")
	}
	var configFile, roleFile *nativeadapter.File
	for i := range files {
		f := &files[i]
		if f.Format != "toml" {
			return nativeadapter.Document{}, nil, errors.New("project-registered Codex inputs must both be TOML")
		}
		switch f.Path {
		case projectConfig:
			if configFile != nil {
				return nativeadapter.Document{}, nil, errors.New("duplicate Codex project config")
			}
			configFile = f
		default:
			if !validAgentPath(f.Path) || roleFile != nil {
				return nativeadapter.Document{}, nil, errors.New("expected exactly one .codex/agents/<name>.toml role file")
			}
			roleFile = f
		}
	}
	if configFile == nil || roleFile == nil {
		return nativeadapter.Document{}, nil, errors.New("project-registered Codex scope requires .codex/config.toml and one role TOML")
	}
	role, diagnostics, err := (Adapter{}).Decode(projectScope, []nativeadapter.File{*roleFile})
	if err != nil {
		return nativeadapter.Document{}, nil, err
	}
	name, _ := rawString(role.Fields["agent.metadata.id"])
	filename := strings.TrimSuffix(path.Base(roleFile.Path), ".toml")
	if name != filename {
		return nativeadapter.Document{}, nil, errors.New("registered Codex role name must match its filename")
	}
	description, _ := rawString(role.Fields["agent.identity.purpose"])
	config, err := nativeparse.Decode("toml", configFile.Data)
	if err != nil {
		return nativeadapter.Document{}, nil, fmt.Errorf("invalid Codex project config: %w", err)
	}
	agents, ok := config["agents"].(map[string]any)
	if !ok {
		return nativeadapter.Document{}, nil, errors.New("Codex project config must register the selected role under [agents.<name>]")
	}
	selected, ok := agents[name].(map[string]any)
	if !ok {
		return nativeadapter.Document{}, nil, fmt.Errorf("Codex project config is missing [agents.%s]", name)
	}
	configDescription, ok := selected["description"].(string)
	if !ok || configDescription != description {
		return nativeadapter.Document{}, nil, errors.New("registered role description must exactly match the role file description")
	}
	configFileRef, ok := selected["config_file"].(string)
	wantRef := "agents/" + filename + ".toml"
	if !ok || configFileRef != wantRef || !safeRegistrationPath(configFileRef) {
		return nativeadapter.Document{}, nil, fmt.Errorf("registered config_file must be exactly %q relative to .codex/config.toml", wantRef)
	}
	delete(agents, name)
	if len(agents) == 0 {
		delete(config, "agents")
	}
	rootRaw, err := json.Marshal(config)
	if err != nil {
		return nativeadapter.Document{}, nil, errors.New("Codex project config cannot be preserved as JSON-compatible unknown data")
	}
	role.Unknown = cloneRawMap(role.Unknown)
	role.Unknown["codex.config"] = rootRaw
	if len(config) > 0 {
		diagnostics = append(diagnostics, diag("codex.project-config.preserved", projectConfig, "codex.config", "warning", "unmapped project config settings are preserved as inert data and are not validated by this adapter"))
	}
	siblingNames := make([]string, 0, len(agents))
	for sibling := range agents {
		siblingNames = append(siblingNames, sibling)
	}
	sort.Strings(siblingNames)
	for _, sibling := range siblingNames {
		value := agents[sibling]
		entry, ok := value.(map[string]any)
		if !ok {
			continue
		}
		if ref, ok := entry["config_file"].(string); ok && ref != "" {
			diagnostics = append(diagnostics, diag("codex.sibling-registration.uninspected", projectConfig, "agents."+sibling+".config_file", "warning", "sibling role registration is preserved but its referenced file is outside this explicit inventory and was not opened"))
		}
	}
	for key, value := range selected {
		if key == "description" || key == "config_file" {
			continue
		}
		if strings.Contains(key, ".") {
			return nativeadapter.Document{}, nil, errors.New("quoted dotted keys in selected Codex registration are unsupported")
		}
		raw, e := json.Marshal(value)
		if e != nil {
			return nativeadapter.Document{}, nil, errors.New("selected registration contains an unsupported value")
		}
		role.Unknown["codex.registered.role."+key] = raw
	}
	return role, diagnostics, nil
}

func encodeRegistered(doc nativeadapter.Document) ([]nativeadapter.File, []contracts.Diagnostic, error) {
	name, err := requiredString(doc.Fields, "agent.metadata.id")
	if err != nil {
		return nil, nil, err
	}
	if len(name) > 128 || strings.ContainsAny(name, `/\\`) || name == "." || name == ".." || evidence.CheckPath(agentDir+name+".toml") != nil {
		return nil, nil, errors.New("agent name is not a valid Codex filename")
	}
	roleDoc := doc
	roleDoc.Unknown = map[string]json.RawMessage{}
	config := map[string]any{}
	selectedExtras := map[string]any{}
	for key, raw := range doc.Unknown {
		switch {
		case key == "codex.config":
			v, e := contracts.DecodeJSON(raw)
			if e != nil {
				return nil, nil, errors.New("preserved Codex config is invalid JSON")
			}
			var ok bool
			config, ok = v.(map[string]any)
			if !ok {
				return nil, nil, errors.New("preserved Codex config must be an object")
			}
		case strings.HasPrefix(key, "codex.registered.role."):
			field := strings.TrimPrefix(key, "codex.registered.role.")
			if field == "" || strings.Contains(field, ".") || field == "description" || field == "config_file" {
				return nil, nil, fmt.Errorf("invalid preserved registration field %q", field)
			}
			v, e := contracts.DecodeJSON(raw)
			if e != nil {
				return nil, nil, errors.New("preserved registration value is invalid JSON")
			}
			selectedExtras[field] = v
		default:
			roleDoc.Unknown[key] = raw
		}
	}
	if config == nil {
		config = map[string]any{}
	}
	agents, ok := config["agents"].(map[string]any)
	if !ok {
		if _, exists := config["agents"]; exists {
			return nil, nil, errors.New("preserved Codex agents config must be a table")
		}
		agents = map[string]any{}
	}
	if _, collision := agents[name]; collision {
		return nil, nil, fmt.Errorf("registered Codex role %q collides with a preserved sibling; rename requires explicit sibling removal", name)
	}
	description, err := requiredString(doc.Fields, "agent.identity.purpose")
	if err != nil {
		return nil, nil, err
	}
	registration := map[string]any{"description": description, "config_file": "agents/" + name + ".toml"}
	for k, v := range selectedExtras {
		registration[k] = v
	}
	agents[name] = registration
	config["agents"] = agents
	roleFiles, diagnostics, err := (Adapter{}).Encode(projectScope, roleDoc)
	if err != nil {
		return nil, nil, err
	}
	configBytes, err := nativeparse.Encode("toml", config)
	if err != nil {
		return nil, nil, errors.New("preserved project config cannot be represented in Codex TOML")
	}
	return []nativeadapter.File{
		{Path: projectConfig, Format: "toml", Data: configBytes},
		roleFiles[0],
	}, diagnostics, nil
}

func safeRegistrationPath(s string) bool {
	return s != "" && !strings.ContainsAny(s, "\\:\x00") && !strings.HasPrefix(s, "/") && path.Clean(s) == s && !strings.HasPrefix(s, "../")
}

func cloneRawMap(in map[string]json.RawMessage) map[string]json.RawMessage {
	out := make(map[string]json.RawMessage, len(in))
	for k, v := range in {
		out[k] = append(json.RawMessage(nil), v...)
	}
	return out
}

func validAgentPath(p string) bool {
	return path.Dir(p) == strings.TrimSuffix(agentDir, "/") && strings.HasSuffix(p, ".toml") && len(strings.TrimSuffix(path.Base(p), ".toml")) > 0
}

func getInstructions(doc nativeadapter.Document) ([]byte, error) {
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
		return nil, errors.New("Codex developer_instructions requires exactly one materialized AOK source")
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
		return nil, errors.New("unreferenced instruction assets are unsupported")
	}
	return append([]byte(nil), body...), nil
}

// Installed Codex protocol models effort as a nonempty provider-advertised string.
func validEffort(s string) bool { return strings.TrimSpace(s) != "" }

func requiredString(fields map[string]json.RawMessage, key string) (string, error) {
	raw, ok := fields[key]
	if !ok {
		return "", fmt.Errorf("required field %s is missing", key)
	}
	s, ok := rawString(raw)
	if !ok || strings.TrimSpace(s) == "" {
		return "", fmt.Errorf("field %s must be a non-empty string", key)
	}
	return s, nil
}
func rawString(raw json.RawMessage) (string, bool) {
	v, err := contracts.DecodeJSON(raw)
	s, ok := v.(string)
	return s, err == nil && ok
}
func put(fields map[string]json.RawMessage, key string, value any) {
	raw, _ := json.Marshal(value)
	fields[key] = raw
}
func diag(code, source, field, severity, message string) contracts.Diagnostic {
	return contracts.Diagnostic{Code: code, Source: source, Field: field, Severity: severity, Message: message}
}
