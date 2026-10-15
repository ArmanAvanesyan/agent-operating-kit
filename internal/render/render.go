package render

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/kit"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/setup"
)

func resolveTargets(raw string) ([]string, error) {
	if raw == "all" {
		return append([]string{}, kit.SupportedTargets...), nil
	}
	for _, t := range kit.SupportedTargets {
		if t == raw {
			return []string{raw}, nil
		}
	}
	return nil, kit.NewErrorf("Unsupported target: %s", raw)
}

func loadJSON(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, kit.NewErrorf("Invalid JSON in %s: %v", path, err)
	}
	return out, nil
}

func loadPackManifest(root, packName string) (map[string]any, string, error) {
	packDir := filepath.Join(root, "packs", packName)
	manifestPath := filepath.Join(packDir, "pack.json")
	info, err := os.Stat(packDir)
	if err != nil || !info.IsDir() {
		return nil, "", kit.NewErrorf("Unknown pack: %s", packName)
	}
	manifest, err := loadJSON(manifestPath)
	if err != nil {
		return nil, "", kit.NewErrorf("Missing pack manifest: %s", manifestPath)
	}
	if len(manifest) == 0 {
		return nil, "", kit.NewErrorf("Invalid pack manifest: %s", manifestPath)
	}
	return manifest, packDir, nil
}

func loadPackContent(packName, packDir string, manifest map[string]any) (map[string]any, string, error) {
	contentRel, _ := manifest["neutralContent"].(string)
	if contentRel == "" {
		contentRel = "pack.content.json"
	}
	contentPath := filepath.Join(packDir, filepath.FromSlash(contentRel))
	content, err := loadJSON(contentPath)
	if err != nil {
		return nil, "", kit.NewErrorf("Missing neutral pack content file: %s", contentPath)
	}
	return content, contentPath, nil
}

func asMapSlice(v any) []map[string]any {
	raw, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func renderSwarmdProjection(task map[string]any, roles []map[string]any) map[string]any {
	callsign, _ := task["assigneeRole"].(string)
	assignee := "implementer"
	if callsign != "" {
		for _, role := range roles {
			if role["id"] == callsign {
				if cs, ok := role["callsign"].(string); ok && cs != "" {
					assignee = cs
				} else if name, ok := role["name"].(string); ok && name != "" {
					assignee = name
				} else if id, ok := role["id"].(string); ok {
					assignee = id
				}
				break
			}
		}
	}
	title, _ := task["title"].(string)
	if title == "" {
		title, _ = task["id"].(string)
		if title == "" {
			title = "unnamed task"
		}
	}
	spec := task["spec"]
	if spec == nil {
		spec = task["description"]
	}
	if spec == nil {
		spec = ""
	}
	phase, _ := task["phase"].(string)
	if phase == "" {
		phase = "planned"
	}
	status, _ := task["status"].(string)
	if status == "" {
		status = "pending"
	}
	decision, _ := task["defaultDecision"].(string)
	if decision == "" {
		decision = fmt.Sprintf("Follow neutral AOK process for %s.", title)
	}
	key := strings.ToLower(strings.ReplaceAll(title, " ", "-")) + "-decision"
	packetID := task["id"]
	if packetID == nil {
		packetID = title
	}
	return map[string]any{
		"task.create": map[string]any{
			"title":    title,
			"spec":     spec,
			"assignee": assignee,
			"phase":    phase,
		},
		"memory.put": map[string]any{
			"namespace": "decision",
			"key":       key,
			"content":   decision,
		},
		"event.append": map[string]any{
			"type":  "aok.task.packet.generated",
			"actor": assignee,
			"payload": map[string]any{
				"taskPacket": packetID,
				"status":     status,
			},
		},
	}
}

func idsOf(items []map[string]any) []any {
	var out []any
	for _, item := range items {
		if id, ok := item["id"].(string); ok && id != "" {
			out = append(out, id)
		}
	}
	return out
}

func RenderPackPayload(root, packName string, manifest, content map[string]any, target string) map[string]any {
	roles := asMapSlice(content["roles"])
	skills := asMapSlice(content["skills"])
	hooks := asMapSlice(content["hooks"])
	templates := asMapSlice(content["templates"])
	taskPackets := asMapSlice(content["taskPackets"])

	taskProjection := make([]any, 0, len(taskPackets))
	for _, task := range taskPackets {
		cloned := map[string]any{}
		for k, v := range task {
			cloned[k] = v
		}
		cloned["swarmd"] = renderSwarmdProjection(task, roles)
		taskProjection = append(taskProjection, cloned)
	}

	version, _ := manifest["version"].(string)
	if version == "" {
		version = "0.1.0"
	}
	neutral, _ := manifest["neutralContent"].(string)
	if neutral == "" {
		neutral = "pack.content.json"
	}
	targetRender := map[string]any{}
	if tr, ok := manifest["targetRender"].(map[string]any); ok {
		if one, ok := tr[target].(map[string]any); ok {
			targetRender = one
		}
	}

	base := map[string]any{
		"kind":    "aok-pack-render",
		"version": version,
		"pack": map[string]any{
			"name":             packName,
			"sourceRepo":       manifest["sourceRepo"],
			"kind":             orString(manifest["kind"], "pack"),
			"description":      manifest["description"],
			"supportedTargets": orAny(manifest["supportedTargets"], []any{}),
			"concepts":         orAny(manifest["concepts"], []any{}),
		},
		"source": map[string]any{
			"manifest": fmt.Sprintf("packs/%s/pack.json", packName),
			"content":  fmt.Sprintf("packs/%s/%s", packName, neutral),
		},
		"target":       target,
		"renderedAt":   time.Now().UTC().Format(time.RFC3339),
		"roles":        content["roles"],
		"hooks":        content["hooks"],
		"skills":       content["skills"],
		"templates":    content["templates"],
		"taskPackets":  taskProjection,
		"targetRender": targetRender,
		"concepts": map[string]any{
			"roles":       idsOf(roles),
			"hooks":       idsOf(hooks),
			"skills":      idsOf(skills),
			"templates":   idsOf(templates),
			"taskPackets": idsOf(taskPackets),
		},
	}

	switch target {
	case "claude-code":
		pluginPath := filepath.Join(root, "packs", packName, "claude-code", "plugin.json")
		plugin := map[string]any{}
		if data, err := loadJSON(pluginPath); err == nil {
			plugin = data
		}
		var skillContracts []any
		for _, item := range skills {
			if sp := item["sourcePath"]; sp != nil {
				skillContracts = append(skillContracts, sp)
			}
		}
		base["targetContract"] = "claude-code-toolbox-compatible"
		base["pluginJson"] = plugin
		base["compatibility"] = map[string]any{
			"hookContracts":  "hook commands and plugin.json",
			"skillContracts": skillContracts,
		}
	case "codex":
		base["targetContract"] = "codex-agent"
		base["notes"] = []any{"Rendered into Codex JSON contract for local adoption."}
		base["mcpConfig"] = map[string]any{
			"mcpServers": map[string]any{
				"swarmd": map[string]any{
					"command": "swarmd",
					"args":    []any{"mcp"},
				},
			},
		}
	default:
		base["targetContract"] = target + "-agent"
		base["notes"] = []any{fmt.Sprintf("Rendered as structured JSON contract for %s.", target)}
	}
	return base
}

func orString(v any, def string) string {
	if s, ok := v.(string); ok && s != "" {
		return s
	}
	return def
}

func orAny(v any, def any) any {
	if v == nil {
		return def
	}
	return v
}

func normalizePackOutput(root, packName, target string, explicitOut string, targetCount int) string {
	if explicitOut != "" {
		if targetCount > 1 {
			return filepath.Join(explicitOut, target+".json")
		}
		return explicitOut
	}
	return filepath.Join(root, "build", "renders", packName, target+".json")
}

// Pack renders one pack for one or more targets.
func Pack(root, packName, target, out string, stdout io.Writer) error {
	targets, err := resolveTargets(target)
	if err != nil {
		return err
	}
	manifest, packDir, err := loadPackManifest(root, packName)
	if err != nil {
		return err
	}
	if name, _ := manifest["name"].(string); name != "" {
		packName = name
	}
	content, _, err := loadPackContent(packName, packDir, manifest)
	if err != nil {
		return err
	}
	for _, t := range targets {
		path := normalizePackOutput(root, packName, t, out, len(targets))
		rendered := RenderPackPayload(root, packName, manifest, content, t)
		if err := kit.WriteJSON(path, rendered); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "Wrote render: %s\n", path)
	}
	return nil
}

// All renders every pack for the target(s).
func All(root, target, out string, stdout io.Writer) error {
	targets, err := resolveTargets(target)
	if err != nil {
		return err
	}
	packDirs := setup.FindPackDirs(root)
	if len(packDirs) == 0 {
		return kit.NewError("No packs found.")
	}
	outputRoot := out
	if outputRoot == "" {
		outputRoot = filepath.Join(root, "build", "renders")
	}
	for _, packName := range packDirs {
		manifestPath := filepath.Join(root, "packs", packName, "pack.json")
		if _, err := os.Stat(manifestPath); err != nil {
			continue
		}
		manifest, err := loadJSON(manifestPath)
		if err != nil {
			continue
		}
		content, _, err := loadPackContent(packName, filepath.Dir(manifestPath), manifest)
		if err != nil {
			return err
		}
		for _, t := range targets {
			path := filepath.Join(outputRoot, t, packName+".json")
			rendered := RenderPackPayload(root, packName, manifest, content, t)
			if err := kit.WriteJSON(path, rendered); err != nil {
				return err
			}
			fmt.Fprintf(stdout, "Wrote render: %s\n", path)
		}
	}
	return nil
}

var nonID = regexp.MustCompile(`[^a-zA-Z0-9_.-]+`)

func itemID(item map[string]any, fallback string) string {
	value := fallback
	if id, ok := item["id"].(string); ok && id != "" {
		value = id
	} else if name, ok := item["name"].(string); ok && name != "" {
		value = name
	}
	value = strings.Trim(nonID.ReplaceAllString(value, "-"), "-")
	if value == "" {
		return fallback
	}
	return value
}

func writeBundleItemFiles(bundleDir, section string, items []map[string]any) []string {
	var written []string
	if len(items) == 0 {
		return written
	}
	sectionDir := filepath.Join(bundleDir, section)
	_ = os.MkdirAll(sectionDir, 0o755)
	for idx, item := range items {
		rel := filepath.Join(section, fmt.Sprintf("%s.json", itemID(item, fmt.Sprintf("%s-%d", section, idx+1))))
		_ = kit.WriteJSON(filepath.Join(bundleDir, rel), item)
		written = append(written, filepath.ToSlash(rel))
	}
	return written
}

func markdownList(title string, items []map[string]any, fields []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", title)
	if len(items) == 0 {
		b.WriteString("No items.\n")
		return b.String()
	}
	for _, item := range items {
		heading := orString(item["name"], orString(item["title"], orString(item["id"], "item")))
		fmt.Fprintf(&b, "## %s\n\n", heading)
		for _, field := range fields {
			value := item[field]
			if value == nil {
				continue
			}
			switch v := value.(type) {
			case []any:
				parts := make([]string, 0, len(v))
				for _, p := range v {
					parts = append(parts, fmt.Sprint(p))
				}
				fmt.Fprintf(&b, "- %s: %s\n", field, strings.Join(parts, ", "))
			case map[string]any:
				encoded, _ := json.Marshal(v)
				fmt.Fprintf(&b, "- %s: `%s`\n", field, string(encoded))
			default:
				fmt.Fprintf(&b, "- %s: %v\n", field, v)
			}
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func copyBundleSource(root, packDir, rel, bundleDir string, copied *[]string) error {
	src := filepath.Join(packDir, filepath.FromSlash(rel))
	src, _ = filepath.Abs(src)
	if _, err := os.Stat(src); err != nil {
		return nil
	}
	rootAbs, _ := filepath.Abs(root)
	relToRoot, err := filepath.Rel(rootAbs, src)
	if err != nil || strings.HasPrefix(relToRoot, "..") {
		return kit.NewErrorf("Refusing to copy source outside kit root: %s", rel)
	}
	dest := filepath.Join(bundleDir, "sources", relToRoot)
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		_ = os.RemoveAll(dest)
		if err := copyTree(src, dest); err != nil {
			return err
		}
	} else {
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		data, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		if err := os.WriteFile(dest, data, info.Mode()); err != nil {
			return err
		}
	}
	*copied = append(*copied, filepath.ToSlash(filepath.Join("sources", rel)))
	return nil
}

func copyTree(src, dest string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dest, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode())
	})
}

func bundleReadme(packName, target, supportLevel string) string {
	return fmt.Sprintf(`# %s for %s

This bundle was generated by Agent Operating Kit.

- Pack: `+"`%s`"+`
- Target: `+"`%s`"+`
- Support level: `+"`%s`"+`
- Contract: `+"`aok-bundle.json`"+`

Use this directory as the importable, self-contained representation of the AOK
pack for the target. Native target installers may consume the same contract and
included source files.
`, packName, target, packName, target, supportLevel)
}

func RenderTargetBundle(root, packName, target, outDir string) (string, error) {
	manifest, packDir, err := loadPackManifest(root, packName)
	if err != nil {
		return "", err
	}
	if name, _ := manifest["name"].(string); name != "" {
		packName = name
	}
	content, _, err := loadPackContent(packName, packDir, manifest)
	if err != nil {
		return "", err
	}
	bundleDir := filepath.Join(outDir, target, packName)
	_ = os.RemoveAll(bundleDir)
	if err := os.MkdirAll(bundleDir, 0o755); err != nil {
		return "", err
	}

	rendered := RenderPackPayload(root, packName, manifest, content, target)
	targetRender := map[string]any{}
	if tr, ok := manifest["targetRender"].(map[string]any); ok {
		if one, ok := tr[target].(map[string]any); ok {
			targetRender = one
		}
	}
	supportLevel := orString(targetRender["supportLevel"], orString(targetRender["status"], "generated"))

	sourceRefs := map[string]bool{}
	if refsObj, ok := manifest["sourceRefs"].(map[string]any); ok {
		for _, refs := range refsObj {
			if list, ok := refs.([]any); ok {
				for _, ref := range list {
					if s, ok := ref.(string); ok {
						sourceRefs[s] = true
					}
				}
			}
		}
	}
	if list, ok := targetRender["sourceRefs"].([]any); ok {
		for _, ref := range list {
			if s, ok := ref.(string); ok {
				sourceRefs[s] = true
			}
		}
	}
	var keys []string
	for k := range sourceRefs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sourceFiles []string
	for _, ref := range keys {
		if err := copyBundleSource(root, packDir, ref, bundleDir, &sourceFiles); err != nil {
			return "", err
		}
	}

	sectionOrder := []struct {
		name  string
		items []map[string]any
	}{
		{"roles", asMapSlice(content["roles"])},
		{"hooks", asMapSlice(content["hooks"])},
		{"skills", asMapSlice(content["skills"])},
		{"templates", asMapSlice(content["templates"])},
		{"task-packets", asMapSlice(content["taskPackets"])},
	}
	var itemFiles []string
	for _, section := range sectionOrder {
		itemFiles = append(itemFiles, writeBundleItemFiles(bundleDir, section.name, section.items)...)
	}

	_ = os.WriteFile(filepath.Join(bundleDir, "roles.md"), []byte(markdownList("Roles", asMapSlice(content["roles"]), []string{"callsign", "responsibilities", "escalation"})), 0o644)
	_ = os.WriteFile(filepath.Join(bundleDir, "hooks.md"), []byte(markdownList("Hooks", asMapSlice(content["hooks"]), []string{"event", "matcher", "command", "severity", "description"})), 0o644)
	_ = os.WriteFile(filepath.Join(bundleDir, "skills.md"), []byte(markdownList("Skills", asMapSlice(content["skills"]), []string{"description", "sourcePath", "inputs"})), 0o644)
	_ = os.WriteFile(filepath.Join(bundleDir, "tasks.md"), []byte(markdownList("Task Packets", asMapSlice(content["taskPackets"]), []string{"title", "phase", "assigneeRole", "verification"})), 0o644)

	switch target {
	case "codex":
		_ = kit.WriteJSON(filepath.Join(bundleDir, ".codex-plugin", "plugin.json"), map[string]any{
			"name":        "aok-" + packName,
			"version":     orString(manifest["version"], "0.1.0"),
			"description": orString(manifest["description"], ""),
			"aokPack":     packName,
		})
	case "claude-code":
		claudeSrc := filepath.Join(packDir, "claude-code")
		if info, err := os.Stat(claudeSrc); err == nil && info.IsDir() {
			if err := copyTree(claudeSrc, filepath.Join(bundleDir, "claude-code")); err != nil {
				return "", err
			}
		}
	default:
		_ = kit.WriteJSON(filepath.Join(bundleDir, "import.json"), map[string]any{
			"kind":     "aok-target-import",
			"target":   target,
			"pack":     packName,
			"contract": "aok-bundle.json",
			"readme":   "README.md",
		})
	}

	files := append([]string{"README.md", "roles.md", "hooks.md", "skills.md", "tasks.md"}, itemFiles...)
	files = append(files, sourceFiles...)
	sort.Strings(files)
	bundle := map[string]any{
		"kind":                 "aok-bundle",
		"version":              orString(manifest["version"], "0.1.0"),
		"pack":                 packName,
		"target":               target,
		"supportLevel":         supportLevel,
		"generatedAt":          time.Now().UTC().Format(time.RFC3339),
		"contract":             rendered,
		"files":                files,
		"sourceContentIncluded": true,
	}
	if err := kit.WriteJSON(filepath.Join(bundleDir, "aok-bundle.json"), bundle); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(bundleDir, "README.md"), []byte(bundleReadme(packName, target, supportLevel)), 0o644); err != nil {
		return "", err
	}
	return bundleDir, nil
}

func writeBundleIndex(kitRoot, outputRoot string, bundles []string) (string, error) {
	sort.Strings(bundles)
	var entries []any
	for _, bundle := range bundles {
		contract := filepath.Join(bundle, "aok-bundle.json")
		data, err := loadJSON(contract)
		if err != nil {
			continue
		}
		rel, _ := filepath.Rel(outputRoot, bundle)
		contractRel, _ := filepath.Rel(outputRoot, contract)
		entries = append(entries, map[string]any{
			"pack":         data["pack"],
			"target":       data["target"],
			"supportLevel": data["supportLevel"],
			"path":         filepath.ToSlash(rel),
			"contract":     filepath.ToSlash(contractRel),
		})
	}
	index := map[string]any{
		"kind":        "aok-bundle-index",
		"version":     kit.MustVersion(kitRoot),
		"generatedAt": time.Now().UTC().Format(time.RFC3339),
		"bundles":     entries,
	}
	out := filepath.Join(outputRoot, "index.json")
	if err := kit.WriteJSON(out, index); err != nil {
		return "", err
	}
	return out, nil
}

// Bundle renders one pack bundle for target(s).
func Bundle(root, packName, target, out string, stdout io.Writer) error {
	targets, err := resolveTargets(target)
	if err != nil {
		return err
	}
	outputRoot := out
	if outputRoot == "" {
		outputRoot = filepath.Join(root, "build", "bundles")
	}
	var bundles []string
	for _, t := range targets {
		bundle, err := RenderTargetBundle(root, packName, t, outputRoot)
		if err != nil {
			return err
		}
		bundles = append(bundles, bundle)
		fmt.Fprintf(stdout, "Wrote bundle: %s\n", bundle)
	}
	index, err := writeBundleIndex(root, outputRoot, bundles)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Wrote bundle index: %s\n", index)
	return nil
}

// Bundles renders all packs for target(s).
func Bundles(root, target, out string, stdout io.Writer) error {
	targets, err := resolveTargets(target)
	if err != nil {
		return err
	}
	outputRoot := out
	if outputRoot == "" {
		outputRoot = filepath.Join(root, "build", "bundles")
	}
	var bundles []string
	for _, packName := range setup.FindPackDirs(root) {
		if _, err := os.Stat(filepath.Join(root, "packs", packName, "pack.json")); err != nil {
			continue
		}
		for _, t := range targets {
			bundle, err := RenderTargetBundle(root, packName, t, outputRoot)
			if err != nil {
				return err
			}
			bundles = append(bundles, bundle)
			fmt.Fprintf(stdout, "Wrote bundle: %s\n", bundle)
		}
	}
	index, err := writeBundleIndex(root, outputRoot, bundles)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Wrote bundle index: %s\n", index)
	return nil
}
