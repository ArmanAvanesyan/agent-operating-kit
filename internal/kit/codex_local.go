package kit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

const CodexLocalEnvDocsURL = "https://developers.openai.com/codex/app/local-environments"

var CodexLocalEnvRequired = []string{
	".codex/config.toml",
	".codex/setup.sh",
	".codex/aok-action.sh",
	".codex/aok-local-environment.json",
	".codex/environments/environment.toml",
}

var CodexLocalEnvActions = []string{
	"AOK Doctor",
	"AOK Validate",
	"AOK Render Bundles",
	"AOK Project Init",
	"AOK Claude Setup",
	"AOK Release Build",
}

// CollectCodexLocalEnv mirrors Python collect_codex_local_env.
func CollectCodexLocalEnv(project string) map[string]any {
	project, _ = filepath.Abs(project)
	files := make([]map[string]any, 0, len(CodexLocalEnvRequired))
	for _, rel := range CodexLocalEnvRequired {
		item := filepath.Join(project, filepath.FromSlash(rel))
		info, err := os.Stat(item)
		ok := err == nil
		entry := map[string]any{
			"path":         item,
			"relativePath": rel,
			"ok":           ok,
			"executable":   nil,
		}
		if strings.HasSuffix(rel, ".sh") {
			if ok {
				entry["executable"] = info.Mode()&0o111 != 0
			} else {
				entry["executable"] = nil
			}
		}
		files = append(files, entry)
	}

	manifestPath := filepath.Join(project, ".codex", "aok-local-environment.json")
	manifest := map[string]any{}
	manifestOK := false
	var manifestError any
	if data, err := os.ReadFile(manifestPath); err == nil {
		if err := json.Unmarshal(data, &manifest); err != nil {
			manifestError = err.Error()
			manifest = map[string]any{}
		} else {
			kind, _ := manifest["kind"].(string)
			manifestOK = kind == "aok-codex-local-environment"
		}
	}

	actions, _ := manifest["actions"].([]any)
	actionNames := map[string]bool{}
	var invalidActions []string
	for _, raw := range actions {
		action, ok := raw.(map[string]any)
		if !ok {
			invalidActions = append(invalidActions, "<non-object>")
			continue
		}
		name, _ := action["name"].(string)
		if name != "" {
			actionNames[name] = true
		}
		if cmd, _ := action["command"].(string); strings.TrimSpace(cmd) == "" {
			if name == "" {
				name = "<unnamed>"
			}
			invalidActions = append(invalidActions, name)
		}
	}
	var missingActions []string
	for _, name := range CodexLocalEnvActions {
		if !actionNames[name] {
			missingActions = append(missingActions, name)
		}
	}

	environmentPath := filepath.Join(project, ".codex", "environments", "environment.toml")
	environmentOK := false
	var environmentError any
	var environmentActions []string
	var environmentSetup any
	if _, err := os.Stat(environmentPath); err == nil {
		envData, err := LoadSimpleEnvironmentTOML(environmentPath)
		if err != nil {
			environmentError = err.Error()
		} else {
			if setup, ok := envData["setup"].(map[string]any); ok {
				environmentSetup = setup["script"]
			}
			if rawActions, ok := envData["actions"].([]any); ok {
				for _, raw := range rawActions {
					action, ok := raw.(map[string]any)
					if !ok {
						continue
					}
					name, _ := action["name"].(string)
					cmd, _ := action["command"].(string)
					if name != "" && cmd != "" {
						environmentActions = append(environmentActions, name)
					}
				}
			}
			versionOK := false
			switch v := envData["version"].(type) {
			case float64:
				versionOK = int(v) == 1
			case int:
				versionOK = v == 1
			case int64:
				versionOK = v == 1
			}
			setupOK := environmentSetup == ".codex/setup.sh"
			envActionSet := map[string]bool{}
			for _, n := range environmentActions {
				envActionSet[n] = true
			}
			missingEnv := false
			for _, name := range CodexLocalEnvActions {
				if !envActionSet[name] {
					missingEnv = true
					break
				}
			}
			environmentOK = versionOK && setupOK && !missingEnv
		}
	}

	var setupDefault any
	if setupScripts, ok := manifest["setupScripts"].(map[string]any); ok {
		setupDefault = setupScripts["default"]
	}
	codexGeneratedConfig, _ := manifest["codexGeneratedConfig"].(map[string]any)
	if codexGeneratedConfig == nil {
		codexGeneratedConfig = map[string]any{
			"status": "unknown",
			"reason": "No AOK Codex local-environment manifest was found.",
		}
	}

	allFilesOK := true
	for _, item := range files {
		if !item["ok"].(bool) {
			allFilesOK = false
		}
		if execv, ok := item["executable"].(bool); ok && !execv {
			allFilesOK = false
		}
	}
	templatesOK := allFilesOK &&
		manifestOK &&
		environmentOK &&
		len(missingActions) == 0 &&
		len(invalidActions) == 0 &&
		setupDefault == ".codex/setup.sh"

	if missingActions == nil {
		missingActions = []string{}
	}
	if invalidActions == nil {
		invalidActions = []string{}
	}
	if environmentActions == nil {
		environmentActions = []string{}
	}
	if actions == nil {
		actions = []any{}
	}

	var manifestVersion any
	if manifestOK || len(manifest) > 0 {
		manifestVersion = manifest["version"]
	}

	return map[string]any{
		"kind":        "aok-codex-local-environment-report",
		"projectPath": project,
		"docs":        CodexLocalEnvDocsURL,
		"templatesOk": templatesOK,
		"files":       files,
		"manifest": map[string]any{
			"path":    manifestPath,
			"ok":      manifestOK,
			"error":   manifestError,
			"version": manifestVersion,
		},
		"setupScript": setupDefault,
		"environmentToml": map[string]any{
			"path":        environmentPath,
			"ok":          environmentOK,
			"error":       environmentError,
			"setupScript": environmentSetup,
			"actions":     environmentActions,
		},
		"actions":              actions,
		"missingActions":       missingActions,
		"invalidActions":       invalidActions,
		"codexGeneratedConfig": codexGeneratedConfig,
	}
}

// LoadSimpleEnvironmentTOML parses the small environment.toml used by Codex local-env.
func LoadSimpleEnvironmentTOML(path string) (map[string]any, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	data := map[string]any{"actions": []any{}}
	var currentAction map[string]any
	inSetup := false
	for _, rawLine := range strings.Split(string(raw), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if line == "[setup]" {
			if _, ok := data["setup"]; !ok {
				data["setup"] = map[string]any{}
			}
			inSetup = true
			currentAction = nil
			continue
		}
		if line == "[[actions]]" {
			currentAction = map[string]any{}
			actions := data["actions"].([]any)
			data["actions"] = append(actions, currentAction)
			inSetup = false
			continue
		}
		if !strings.Contains(line, "=") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		var parsed any = value
		if strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`) && len(value) >= 2 {
			parsed = value[1 : len(value)-1]
		} else if isDigits(value) {
			var n int
			for _, c := range value {
				n = n*10 + int(c-'0')
			}
			parsed = n
		}
		if currentAction != nil {
			currentAction[key] = parsed
		} else if inSetup {
			setup := data["setup"].(map[string]any)
			setup[key] = parsed
		} else {
			data[key] = parsed
		}
	}
	return data, nil
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
