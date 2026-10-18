// Package hermes reads a bounded, inert subset of a Hermes profile home. It
// never launches Hermes, loads environment files, or accesses runtime state.
package hermes

import (
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/nativeadapter"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/nativeparse"
)

const adapterVersion = "hermes-profile-0.21.6-v1"

var secretAssignment = regexp.MustCompile(`(?i)\b[A-Za-z0-9_]*(?:API[_-]?KEY|SECRET[_-]?ACCESS[_-]?KEY|ACCESS[_-]?TOKEN|TOKEN|SECRET|PASSWORD|PRIVATE[_-]?KEY)\s*[:=]\s*\S+`)

// New returns the profile-core adapter. Its input is a selected isolated-home
// inventory, not a profile archive or an entire live Hermes home.
func New() nativeadapter.Adapter { return adapter{} }

type adapter struct{}

func (adapter) Descriptor() nativeadapter.Descriptor {
	return nativeadapter.Descriptor{
		Target: "hermes-agent", AdapterVersion: adapterVersion, Scopes: []string{"profile"},
		// Hermes is installed, but its launcher is unsafe to probe in a temporary
		// home and no safe direct native loader has been verified.
		HostLoading: nativeadapter.HostLoadingEvidence{Status: "unavailable"},
		Fields: []nativeadapter.FieldCapability{
			{NeutralPath: "agent.identity.purpose", NativePath: "profile.yaml.description", Support: "native", EnforcementOwner: "native"},
			{NeutralPath: "executionProfile.model.selector", NativePath: "config.yaml.model.default", Support: "native", EnforcementOwner: "native"},
			{NeutralPath: "agent.instructions.sources", NativePath: "SOUL.md", Support: "native", EnforcementOwner: "native"},
		},
	}
}

func (adapter) Decode(scope string, files []nativeadapter.File) (nativeadapter.Document, []contracts.Diagnostic, error) {
	if scope != "profile" {
		return nativeadapter.Document{}, nil, errors.New("Hermes adapter supports profile scope only")
	}
	configBytes, soulBytes, varBytes, err := inventory(files)
	if err != nil {
		return nativeadapter.Document{}, nil, err
	}
	config, err := nativeparse.Decode("yaml", configBytes)
	if err != nil {
		return nativeadapter.Document{}, nil, errors.New("Hermes config.yaml is invalid")
	}
	if containsSecretKey(config) || secretAssignment.Match(configBytes) {
		return nativeadapter.Document{}, nil, errors.New("Hermes config contains a credential-bearing value")
	}
	if !safeText(soulBytes) {
		return nativeadapter.Document{}, nil, errors.New("Hermes SOUL.md contains invalid or credential-bearing text")
	}
	fields := map[string]json.RawMessage{
		"agent.instructions.sources": mustJSON([]map[string]string{{"path": "instructions.md"}}),
	}
	unknown := map[string]json.RawMessage{}
	configWithoutModel := copyMap(config)
	modelValue, modelPresent := configWithoutModel["model"]
	delete(configWithoutModel, "model")
	if len(configWithoutModel) > 0 {
		unknown["config.root"] = mustJSON(configWithoutModel)
	}
	if modelPresent {
		modelMap, isMap := modelValue.(map[string]any)
		if isMap {
			modelRemainder := copyMap(modelMap)
			defaultValue, defaultPresent := modelRemainder["default"]
			delete(modelRemainder, "default")
			if len(modelRemainder) > 0 {
				unknown["config.modelRemainder"] = mustJSON(modelRemainder)
			}
			if defaultPresent {
				if value, ok := defaultValue.(string); ok && strings.TrimSpace(value) != "" {
					fields["executionProfile.model.selector"] = mustJSON(value)
				} else {
					unknown["config.modelDefaultRaw"] = mustJSON(defaultValue)
				}
			}
			// The mapped selector and/or retained remainder already proves the
			// section existed. Keep a marker only for an otherwise-empty map.
			if !defaultPresent && len(modelRemainder) == 0 {
				unknown["config.modelPresent"] = json.RawMessage("true")
			}
		} else if value, ok := modelValue.(string); ok && strings.TrimSpace(value) != "" {
			// The installed source accepts the legacy scalar `model: <id>` form,
			// normalizing it to model.default at load time.
			fields["executionProfile.model.selector"] = mustJSON(value)
			unknown["config.modelWasScalar"] = json.RawMessage("true")
		} else {
			unknown["config.modelRaw"] = mustJSON(modelValue)
		}
	}

	if varBytes != nil {
		profileMeta, parseErr := nativeparse.Decode("yaml", varBytes)
		if parseErr != nil {
			return nativeadapter.Document{}, nil, errors.New("Hermes profile.yaml is invalid")
		}
		if containsSecretKey(profileMeta) || secretAssignment.Match(varBytes) {
			return nativeadapter.Document{}, nil, errors.New("Hermes profile.yaml contains a credential-bearing value")
		}
		profileRest := copyMap(profileMeta)
		if description, ok := profileRest["description"].(string); ok && strings.TrimSpace(description) != "" {
			fields["agent.identity.purpose"] = mustJSON(description)
			delete(profileRest, "description")
		}
		if len(profileRest) > 0 {
			unknown["profile.remainder"] = mustJSON(profileRest)
		}
		if len(profileMeta) == 0 {
			unknown["profile.present"] = json.RawMessage("true")
		}
	}
	return nativeadapter.Document{Fields: fields, Unknown: unknown, Assets: map[string][]byte{"instructions.md": append([]byte(nil), soulBytes...)}}, nil, nil
}

func (adapter) Encode(scope string, doc nativeadapter.Document) ([]nativeadapter.File, []contracts.Diagnostic, error) {
	if scope != "profile" {
		return nil, nil, errors.New("Hermes adapter supports profile scope only")
	}
	for field := range doc.Fields {
		switch field {
		case "agent.identity.purpose", "executionProfile.model.selector", "agent.instructions.sources":
		default:
			return nil, nil, fmt.Errorf("Hermes adapter cannot encode canonical field %q", field)
		}
	}
	for key := range doc.Unknown {
		switch key {
		case "config.root", "config.modelPresent", "config.modelRemainder", "config.modelDefaultRaw", "config.modelWasScalar", "config.modelRaw", "profile.present", "profile.remainder":
		default:
			return nil, nil, errors.New("Hermes adapter cannot encode an unknown projection path")
		}
	}
	if raw, ok := doc.Fields["agent.instructions.sources"]; !ok || !validSoulSource(raw) {
		return nil, nil, errors.New("Hermes export requires the supported SOUL.md source mapping")
	}
	config := map[string]any{}
	if raw, ok := doc.Unknown["config.root"]; ok {
		value, decodeErr := decodeObject(raw)
		if decodeErr != nil {
			return nil, nil, errors.New("Hermes config remainder is invalid")
		}
		config = copyMap(value)
	}
	modelRemainder := map[string]any{}
	if raw, ok := doc.Unknown["config.modelRemainder"]; ok {
		value, decodeErr := decodeObject(raw)
		if decodeErr != nil {
			return nil, nil, errors.New("Hermes model config remainder is invalid")
		}
		modelRemainder = value
	}
	_, hasRawDefault := doc.Unknown["config.modelDefaultRaw"]
	_, hasModelSelector := doc.Fields["executionProfile.model.selector"]
	if hasRawDefault && hasModelSelector {
		return nil, nil, errors.New("Hermes mapped model conflicts with retained native model default")
	}
	if raw, ok := doc.Unknown["config.modelDefaultRaw"]; ok {
		value, decodeErr := contracts.DecodeJSON(raw)
		if decodeErr != nil {
			return nil, nil, errors.New("Hermes model default is invalid")
		}
		modelRemainder["default"] = value
	}
	if raw, ok := doc.Unknown["config.modelRaw"]; ok {
		if hasModelSelector || len(modelRemainder) > 0 || truthyRaw(doc.Unknown["config.modelPresent"]) {
			return nil, nil, errors.New("Hermes model section conflicts with retained native model data")
		}
		value, decodeErr := contracts.DecodeJSON(raw)
		if decodeErr != nil {
			return nil, nil, errors.New("Hermes model section is invalid")
		}
		config["model"] = value
	} else if raw, ok := doc.Fields["executionProfile.model.selector"]; ok {
		model, modelErr := rawString(raw)
		if modelErr != nil || strings.TrimSpace(model) == "" {
			return nil, nil, errors.New("Hermes model selector must be a non-empty string")
		}
		if truthyRaw(doc.Unknown["config.modelWasScalar"]) {
			config["model"] = model
		} else {
			modelRemainder["default"] = model
			config["model"] = modelRemainder
		}
	} else if len(modelRemainder) > 0 || truthyRaw(doc.Unknown["config.modelPresent"]) {
		config["model"] = modelRemainder
	}
	if containsSecretKey(config) {
		return nil, nil, errors.New("Hermes config contains a credential-bearing value")
	}
	configBytes, err := nativeparse.Encode("yaml", config)
	if err != nil {
		return nil, nil, errors.New("Hermes config.yaml cannot be encoded")
	}
	soul, ok := doc.Assets["instructions.md"]
	if !ok || len(doc.Assets) != 1 || !safeText(soul) {
		return nil, nil, errors.New("Hermes export requires exactly one safe instructions.md asset")
	}
	out := []nativeadapter.File{
		{Path: "config.yaml", Format: "yaml", Data: configBytes},
		{Path: "SOUL.md", Format: "markdown", Data: append([]byte(nil), soul...)},
	}
	profileMeta := map[string]any{}
	if raw, ok := doc.Unknown["profile.remainder"]; ok {
		value, decodeErr := decodeObject(raw)
		if decodeErr != nil {
			return nil, nil, errors.New("Hermes profile metadata remainder is invalid")
		}
		profileMeta = value
	}
	if raw, ok := doc.Fields["agent.identity.purpose"]; ok {
		if _, conflict := profileMeta["description"]; conflict {
			return nil, nil, errors.New("Hermes profile description conflicts with retained native metadata")
		}
		description, descriptionErr := rawString(raw)
		if descriptionErr != nil {
			return nil, nil, errors.New("Hermes profile description must be a string")
		}
		profileMeta["description"] = description
	}
	if truthyRaw(doc.Unknown["profile.present"]) || len(profileMeta) > 0 {
		if containsSecretKey(profileMeta) {
			return nil, nil, errors.New("Hermes profile metadata contains a credential-bearing value")
		}
		profileBytes, encodeErr := nativeparse.Encode("yaml", profileMeta)
		if encodeErr != nil {
			return nil, nil, errors.New("Hermes profile.yaml cannot be encoded")
		}
		out = append(out, nativeadapter.File{Path: "profile.yaml", Format: "yaml", Data: profileBytes})
	}
	return out, nil, nil
}

// inventory accepts only the selected source/config/persona subset. Environment
// files, auth stores, runtime state, caches, skills, and archives are explicitly
// outside this adapter's profile projection.
func inventory(files []nativeadapter.File) (config, soul, profile []byte, err error) {
	seen := map[string]bool{}
	for _, file := range files {
		if seen[file.Path] {
			return nil, nil, nil, errors.New("Hermes profile inventory contains duplicate paths")
		}
		seen[file.Path] = true
		if path.Clean(file.Path) != file.Path || strings.Contains(file.Path, "\\") || path.IsAbs(file.Path) {
			return nil, nil, nil, errors.New("Hermes profile inventory contains an unsafe path")
		}
		switch file.Path {
		case "config.yaml":
			if !isYAML(file.Format) {
				return nil, nil, nil, errors.New("Hermes config.yaml has an unsupported format")
			}
			config = append([]byte{}, file.Data...)
		case "SOUL.md":
			if !isMarkdown(file.Format) {
				return nil, nil, nil, errors.New("Hermes SOUL.md has an unsupported format")
			}
			soul = append([]byte{}, file.Data...)
		case "profile.yaml":
			if !isYAML(file.Format) {
				return nil, nil, nil, errors.New("Hermes profile.yaml has an unsupported format")
			}
			profile = append([]byte{}, file.Data...)
		default:
			return nil, nil, nil, errors.New("Hermes inventory includes a secret, runtime, archive, or unsupported dependency path")
		}
	}
	if config == nil || soul == nil {
		return nil, nil, nil, errors.New("Hermes profile inventory requires config.yaml and SOUL.md")
	}
	if len(soul) == 0 {
		return nil, nil, nil, errors.New("Hermes SOUL.md must be non-empty for an explicit persona projection")
	}
	return config, soul, profile, nil
}

func safeText(data []byte) bool {
	if len(data) == 0 || len(data) > nativeparse.MaxDocumentBytes || !utf8.Valid(data) || strings.HasPrefix(string(data), "\xef\xbb\xbf") {
		return false
	}
	return !secretAssignment.Match(data)
}

func containsSecretKey(value any) bool {
	switch item := value.(type) {
	case map[string]any:
		for key, child := range item {
			if secretKey(key) || containsSecretKey(child) {
				return true
			}
		}
	case []any:
		for _, child := range item {
			if containsSecretKey(child) {
				return true
			}
		}
	}
	return false
}

func secretKey(key string) bool {
	n := strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(key))
	if n == "apikey" || n == "password" || n == "privatekey" || n == "secret" || n == "clientsecret" || n == "accesstoken" || n == "refreshtoken" || n == "authtoken" {
		return true
	}
	if strings.HasSuffix(n, "secretaccesskey") || strings.HasSuffix(n, "apikey") || strings.HasSuffix(n, "password") || strings.HasSuffix(n, "privatekey") || strings.HasSuffix(n, "secret") || strings.HasSuffix(n, "token") {
		return !(strings.Contains(n, "maxtoken") || strings.Contains(n, "tokenlimit") || strings.Contains(n, "tokencount"))
	}
	return false
}

func copyMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func decodeObject(raw json.RawMessage) (map[string]any, error) {
	value, err := contracts.DecodeJSON(raw)
	if err != nil {
		return nil, err
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("value is not an object")
	}
	return object, nil
}

func isYAML(format string) bool {
	f := strings.ToLower(strings.TrimSpace(format))
	return f == "yaml" || f == "yml"
}
func isMarkdown(format string) bool {
	f := strings.ToLower(strings.TrimSpace(format))
	return f == "markdown" || f == "md" || f == "text"
}
func validSoulSource(raw json.RawMessage) bool {
	var source []map[string]string
	return json.Unmarshal(raw, &source) == nil && len(source) == 1 && len(source[0]) == 1 && source[0]["path"] == "instructions.md"
}
func rawString(raw json.RawMessage) (string, error) {
	var value string
	if len(raw) == 0 || json.Unmarshal(raw, &value) != nil {
		return "", errors.New("value is not a string")
	}
	return value, nil
}
func truthyRaw(raw json.RawMessage) bool { return string(raw) == "true" }
func mustJSON(value any) json.RawMessage {
	b, _ := json.Marshal(value)
	return b
}
