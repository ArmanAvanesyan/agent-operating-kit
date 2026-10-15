package validate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/kit"
)

// Kit runs the full validation suite and returns error messages (empty = pass).
func Kit(root string) []string {
	var errors []string
	validateRequiredFiles(root, &errors)
	validateJSONFiles(root, &errors)
	validateAdapters(root, &errors)
	validatePacks(root, &errors)
	validateClaudeCodeMarketplace(root, &errors)
	validateCodexLocalEnvironmentTemplates(root, &errors)
	return errors
}

func validateRequiredFiles(root string, errors *[]string) {
	required := []string{
		"README.md",
		"CHANGELOG.md",
		"LICENSE",
		".codex-plugin/plugin.json",
		"hooks.json",
		"core/model.md",
		"core/examples/skill.example.json",
		"core/examples/role.example.json",
		"core/examples/hook.example.json",
		"core/examples/task-packet.example.json",
		"docs/architecture.md",
		"docs/swarmd-integration.md",
		"docs/migration-from-claude-code-toolbox.md",
		"docs/install-and-onboarding.md",
		"docs/cli-distribution.md",
		"docs/codex-local-environments.md",
		"docs/project-init-guide.md",
		"docs/targets-support-matrix.md",
		"docs/release-checklist.md",
		"docs/uninstall-and-rollback.md",
		"adapters/claude-code/aok-plugin/plugin.json",
		"adapters/claude-code/aok-plugin/README.md",
		"adapters/claude-code/aok-plugin/skills/aok-setup.md",
		"adapters/claude-code/aok-plugin/skills/aok-doctor.md",
		"adapters/claude-code/aok-plugin/skills/aok-render.md",
		"adapters/claude-code/aok-plugin/skills/aok-migrate-toolbox.md",
		"adapters/claude-code/aok-plugin/skills/aok-pack-discovery.md",
		"schemas/skill.schema.json",
		"schemas/role.schema.json",
		"schemas/hook.schema.json",
		"schemas/task-packet.schema.json",
		"schemas/tool-adapter.schema.json",
		"schemas/pack.schema.json",
		"skills/agent-workflow/SKILL.md",
		"skills/github-project-execution/SKILL.md",
		"skills/code-review/SKILL.md",
		"skills/qa-verification/SKILL.md",
		"skills/project-bootstrap/SKILL.md",
		"skills/project-exploration/SKILL.md",
		"skills/environment-readiness/SKILL.md",
		"templates/AGENTS.md",
		"templates/.codex/config.toml",
		"templates/.codex/setup.sh",
		"templates/.codex/aok-action.sh",
		"templates/.codex/aok-local-environment.json",
		"templates/.codex/environments/environment.toml",
		"templates/agents/workflow.md",
		"scripts/aok",
		"scripts/install.sh",
		"scripts/project-init.sh",
		"scripts/github-sync.sh",
		"scripts/validate.sh",
		"scripts/release/build.sh",
		"scripts/release/sign.sh",
		"cmd/aok-release/main.go",
		"go.mod",
		".github/workflows/release.yml",
		"references/migration-from-existing-project.md",
	}
	for _, rel := range required {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
			*errors = append(*errors, rel+": missing")
		}
	}
}

func requireJSON(path string, root string, errors *[]string) map[string]any {
	data, err := os.ReadFile(path)
	if err != nil {
		*errors = append(*errors, kit.Rel(root, path)+": missing or unreadable")
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		*errors = append(*errors, kit.Rel(root, path)+": invalid JSON: "+err.Error())
		return nil
	}
	return out
}

func validateJSONFiles(root string, errors *[]string) {
	for _, rel := range []string{
		".codex-plugin/plugin.json",
		"hooks.json",
		"templates/.codex/aok-local-environment.json",
	} {
		requireJSON(filepath.Join(root, filepath.FromSlash(rel)), root, errors)
	}
	schemaDir := filepath.Join(root, "schemas")
	entries, _ := filepath.Glob(filepath.Join(schemaDir, "*.schema.json"))
	sort.Strings(entries)
	for _, path := range entries {
		data := requireJSON(path, root, errors)
		if data == nil {
			continue
		}
		for _, key := range []string{"$schema", "title", "type"} {
			if _, ok := data[key]; !ok {
				*errors = append(*errors, kit.Rel(root, path)+": missing "+key)
			}
		}
	}
	exampleDir := filepath.Join(root, "core", "examples")
	examples, _ := filepath.Glob(filepath.Join(exampleDir, "*.json"))
	sort.Strings(examples)
	for _, path := range examples {
		data := requireJSON(path, root, errors)
		if data == nil {
			continue
		}
		if data["kind"] == nil && filepath.Base(path) != "task-packet.example.json" {
			*errors = append(*errors, kit.Rel(root, path)+": missing kind")
		}
	}
	packContents, _ := filepath.Glob(filepath.Join(root, "packs", "*", "*", "pack.content.json"))
	sort.Strings(packContents)
	for _, path := range packContents {
		data := requireJSON(path, root, errors)
		if data == nil {
			continue
		}
		if data["kind"] != "pack-content" {
			*errors = append(*errors, kit.Rel(root, path)+": expected kind pack-content")
		}
	}
}

func validateAdapters(root string, errors *[]string) {
	seen := map[string]bool{}
	for _, target := range kit.SupportedTargets {
		base := filepath.Join(root, "adapters", target)
		readme := filepath.Join(base, "README.md")
		manifest := filepath.Join(base, "adapter.json")
		if _, err := os.Stat(readme); err != nil {
			*errors = append(*errors, kit.Rel(root, readme)+": missing")
		}
		data := requireJSON(manifest, root, errors)
		if data == nil {
			continue
		}
		if name, _ := data["name"].(string); name != "" {
			seen[name] = true
		}
		for _, key := range []string{"name", "kind", "displayName", "status", "supportedTargets", "renders"} {
			if _, ok := data[key]; !ok {
				*errors = append(*errors, kit.Rel(root, manifest)+": missing "+key)
			}
		}
		if data["status"] == "active" {
			for _, key := range []string{"supportLevel", "bundle"} {
				if _, ok := data[key]; !ok {
					*errors = append(*errors, kit.Rel(root, manifest)+": active adapter missing "+key)
				}
			}
			if bundle, ok := data["bundle"].(map[string]any); ok {
				for _, key := range []string{"artifactName", "format", "importInstructions"} {
					if _, ok := bundle[key]; !ok {
						*errors = append(*errors, kit.Rel(root, manifest)+": bundle missing "+key)
					}
				}
			}
		}
		if data["name"] != target {
			*errors = append(*errors, kit.Rel(root, manifest)+": name must be "+target)
		}
		if data["kind"] != "tool-adapter" {
			*errors = append(*errors, kit.Rel(root, manifest)+": kind must be tool-adapter")
		}
	}
	var missing []string
	for _, target := range kit.SupportedTargets {
		if !seen[target] {
			missing = append(missing, target)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		*errors = append(*errors, "adapters: missing manifests for "+strings.Join(missing, ", "))
	}
}

func validatePacks(root string, errors *[]string) {
	expected := map[string]bool{
		"engineering-guardrails":    true,
		"ship-pipeline":             true,
		"phased-rollout-template":   true,
		"batch-resolve-conflicts":   true,
	}
	packRoot := filepath.Join(root, "packs")
	found := map[string]bool{}
	if entries, err := os.ReadDir(packRoot); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				found[e.Name()] = true
			}
		}
	}
	var missingPacks []string
	for name := range expected {
		if !found[name] {
			missingPacks = append(missingPacks, name)
		}
	}
	sort.Strings(missingPacks)
	for _, name := range missingPacks {
		*errors = append(*errors, "packs/"+name+": missing")
	}

	var names []string
	for name := range expected {
		if found[name] {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	expectedTargets := map[string]bool{}
	for _, t := range kit.SupportedTargets {
		expectedTargets[t] = true
	}
	expectedTargetList := append([]string{}, kit.SupportedTargets...)
	sort.Strings(expectedTargetList)

	for _, name := range names {
		base := filepath.Join(packRoot, name)
		if _, err := os.Stat(filepath.Join(base, "README.md")); err != nil {
			*errors = append(*errors, "packs/"+name+"/README.md: missing")
		}
		data := requireJSON(filepath.Join(base, "pack.json"), root, errors)
		if data == nil {
			continue
		}
		for _, key := range []string{
			"name", "kind", "description", "sourceRepo", "neutralContent",
			"supportedTargets", "concepts", "adapters", "sourceRefs", "targetRender", "conceptGraph",
		} {
			if _, ok := data[key]; !ok {
				*errors = append(*errors, "packs/"+name+"/pack.json: missing "+key)
			}
		}
		if data["name"] != name {
			*errors = append(*errors, "packs/"+name+"/pack.json: name must be "+name)
		}
		if data["kind"] != "pack" {
			*errors = append(*errors, "packs/"+name+"/pack.json: kind must be pack")
		}
		if targets, ok := data["supportedTargets"].([]any); ok {
			present := map[string]bool{}
			for _, t := range targets {
				if s, ok := t.(string); ok {
					present[s] = true
				}
			}
			if !sameStringSet(present, expectedTargets) {
				*errors = append(*errors, "packs/"+name+"/pack.json: supportedTargets must include exactly "+strings.Join(expectedTargetList, ", "))
			}
		}

		neutral, _ := data["neutralContent"].(string)
		if neutral == "" {
			neutral = "pack.content.json"
		}
		contentPath := filepath.Join(base, filepath.FromSlash(neutral))
		content := requireJSON(contentPath, root, errors)
		if content == nil {
			*errors = append(*errors, "packs/"+name+"/pack.json: missing neutral content "+neutral)
			continue
		}
		if content["name"] != name {
			*errors = append(*errors, "packs/"+name+"/pack.content.json: name must be "+name)
		}

		targetRender, ok := data["targetRender"].(map[string]any)
		if !ok {
			*errors = append(*errors, "packs/"+name+"/pack.json: targetRender must be an object")
		} else {
			for _, target := range kit.SupportedTargets {
				renderRaw, exists := targetRender[target]
				if !exists {
					*errors = append(*errors, "packs/"+name+"/pack.json: missing targetRender["+target+"]")
					continue
				}
				render, ok := renderRaw.(map[string]any)
				if !ok {
					*errors = append(*errors, "packs/"+name+"/pack.json: targetRender["+target+"] must be object")
					continue
				}
				for _, key := range []string{"status", "mode"} {
					if _, ok := render[key]; !ok {
						*errors = append(*errors, "packs/"+name+"/pack.json: targetRender["+target+"] missing "+key)
					}
				}
				if render["status"] == "active" {
					for _, key := range []string{"supportLevel", "artifactName", "packageFormat", "importInstructions", "bundleRefs"} {
						if _, ok := render[key]; !ok {
							*errors = append(*errors, "packs/"+name+"/pack.json: targetRender["+target+"] active render missing "+key)
						}
					}
				}
				mode, _ := render["mode"].(string)
				sources, _ := render["sourceRefs"].([]any)
				payload := render["payload"]
				switch mode {
				case "source":
					if len(sources) == 0 {
						*errors = append(*errors, "packs/"+name+"/pack.json: targetRender["+target+"] mode=source needs sourceRefs")
					} else {
						for _, rel := range sources {
							s, ok := rel.(string)
							if !ok || !existsFile(filepath.Join(base, filepath.FromSlash(s))) {
								*errors = append(*errors, fmt.Sprintf("packs/%s/pack.json: targetRender[%s] sourceRefs missing %v", name, target, rel))
							}
						}
					}
				case "inline":
					if payload == nil {
						*errors = append(*errors, "packs/"+name+"/pack.json: targetRender["+target+"] mode=inline needs payload")
					}
				case "generated", "manual", "unsupported":
					// ok
				default:
					*errors = append(*errors, fmt.Sprintf("packs/%s/pack.json: targetRender[%s] has unsupported mode %v", name, target, mode))
				}
			}
		}

		sourceRefs, ok := data["sourceRefs"].(map[string]any)
		if !ok {
			*errors = append(*errors, "packs/"+name+"/pack.json: sourceRefs must be an object")
		} else {
			for _, rel := range sourceRefs {
				list, ok := rel.([]any)
				if !ok {
					continue
				}
				for _, source := range list {
					s, ok := source.(string)
					if !ok {
						continue
					}
					if !existsFile(filepath.Join(base, filepath.FromSlash(s))) {
						*errors = append(*errors, "packs/"+name+"/pack.json: sourceRef "+s+" missing")
					}
				}
			}
		}

		conceptGraph, _ := data["conceptGraph"].(map[string]any)
		if data["conceptGraph"] != nil && conceptGraph == nil {
			*errors = append(*errors, "packs/"+name+"/pack.json: conceptGraph must be object")
		} else if conceptGraph != nil {
			for _, section := range []string{"roles", "hooks", "skills", "templates", "taskPackets"} {
				if raw, ok := conceptGraph[section]; ok {
					if _, ok := raw.([]any); !ok {
						*errors = append(*errors, "packs/"+name+"/pack.json: conceptGraph."+section+" must be array")
					}
				}
			}
			for _, target := range []string{"roles", "hooks", "skills", "templates", "taskPackets"} {
				declared, _ := conceptGraph[target].([]any)
				contentItems, _ := content[target].([]any)
				contentIDs := map[string]bool{}
				for _, item := range contentItems {
					if m, ok := item.(map[string]any); ok {
						if id, ok := m["id"].(string); ok && id != "" {
							contentIDs[id] = true
						}
					}
				}
				var missingIDs []string
				for _, item := range declared {
					s, _ := item.(string)
					if s != "" && !contentIDs[s] {
						missingIDs = append(missingIDs, s)
					}
				}
				if len(missingIDs) > 0 {
					*errors = append(*errors, "packs/"+name+"/pack.json: conceptGraph."+target+" references missing ids: "+strings.Join(missingIDs, ", "))
				}
			}
		}

		adapters, _ := data["adapters"].(map[string]any)
		for adapterName, raw := range adapters {
			adapter, _ := raw.(map[string]any)
			for _, key := range []string{"pluginPath", "readmePath"} {
				if rel, _ := adapter[key].(string); rel != "" {
					if !existsFile(filepath.Join(base, filepath.FromSlash(rel))) {
						*errors = append(*errors, "packs/"+name+"/pack.json: missing adapter asset "+rel)
					}
				}
			}
			if assets, ok := adapter["assets"].([]any); ok {
				for _, rel := range assets {
					s, _ := rel.(string)
					if s != "" && !existsFile(filepath.Join(base, filepath.FromSlash(s))) {
						*errors = append(*errors, "packs/"+name+"/pack.json: missing adapter asset "+s)
					}
				}
			}
			if !expectedTargets[adapterName] {
				*errors = append(*errors, "packs/"+name+"/pack.json: unknown adapter "+adapterName)
			}
		}
	}
}

func validateClaudeCodeMarketplace(root string, errors *[]string) {
	marketplaceRoot := filepath.Join(root, "adapters", "claude-code", "marketplace")
	marketplace := filepath.Join(marketplaceRoot, ".claude-plugin", "marketplace.json")
	data := requireJSON(marketplace, root, errors)
	if data == nil {
		return
	}
	plugins, ok := data["plugins"].([]any)
	if !ok || len(plugins) == 0 {
		*errors = append(*errors, kit.Rel(root, marketplace)+": plugins must be a non-empty list")
		return
	}
	names := map[string]bool{}
	for _, raw := range plugins {
		plugin, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		name, _ := plugin["name"].(string)
		names[name] = true
		source, _ := plugin["source"].(string)
		if source == "" {
			if name == "" {
				name = "<unknown>"
			}
			*errors = append(*errors, kit.Rel(root, marketplace)+": plugin "+name+" missing source")
			continue
		}
		resolved := filepath.Join(marketplaceRoot, filepath.FromSlash(source))
		resolved, _ = filepath.Abs(resolved)
		if !existsFile(resolved) && !existsDir(resolved) {
			*errors = append(*errors, kit.Rel(root, marketplace)+": plugin "+name+" source does not exist: "+source)
		}
		rootAbs, _ := filepath.Abs(root)
		rel, err := filepath.Rel(rootAbs, resolved)
		if err != nil || strings.HasPrefix(rel, "..") {
			*errors = append(*errors, kit.Rel(root, marketplace)+": plugin "+name+" source escapes kit root: "+source)
		}
	}
	if !names[kit.ClaudeRootPlugin] {
		*errors = append(*errors, kit.Rel(root, marketplace)+": missing "+kit.ClaudeRootPlugin+" plugin")
	}
}

func validateCodexLocalEnvironmentTemplates(root string, errors *[]string) {
	report := kit.CollectCodexLocalEnv(filepath.Join(root, "templates"))
	if !report["templatesOk"].(bool) {
		for _, raw := range report["files"].([]map[string]any) {
			if !raw["ok"].(bool) {
				*errors = append(*errors, "templates/"+raw["relativePath"].(string)+": missing")
			}
			if execv, ok := raw["executable"].(bool); ok && !execv {
				*errors = append(*errors, "templates/"+raw["relativePath"].(string)+": must be executable")
			}
		}
		manifest := report["manifest"].(map[string]any)
		if !manifest["ok"].(bool) {
			detail := ""
			if errv := manifest["error"]; errv != nil {
				detail = ": " + fmt.Sprint(errv)
			}
			*errors = append(*errors, "templates/.codex/aok-local-environment.json: invalid manifest"+detail)
		}
		env := report["environmentToml"].(map[string]any)
		if !env["ok"].(bool) {
			detail := ""
			if errv := env["error"]; errv != nil {
				detail = ": " + fmt.Sprint(errv)
			}
			*errors = append(*errors, "templates/.codex/environments/environment.toml: invalid local environment config"+detail)
		}
		for _, action := range report["missingActions"].([]string) {
			*errors = append(*errors, "templates/.codex/aok-local-environment.json: missing action "+action)
		}
		for _, action := range report["invalidActions"].([]string) {
			*errors = append(*errors, "templates/.codex/aok-local-environment.json: invalid action "+action)
		}
	}
	cfg := report["codexGeneratedConfig"].(map[string]any)
	if cfg["status"] != "locally-observed" {
		*errors = append(*errors, "templates/.codex/aok-local-environment.json: codexGeneratedConfig.status must be locally-observed")
	}
	if cfg["path"] != ".codex/environments/environment.toml" {
		*errors = append(*errors, "templates/.codex/aok-local-environment.json: codexGeneratedConfig.path must be .codex/environments/environment.toml")
	}
}

func existsFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func existsDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func sameStringSet(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}
