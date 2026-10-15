package setup

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/kit"
)

// FindPackDirs returns sorted pack directory names under root/packs.
func FindPackDirs(root string) []string {
	entries, err := os.ReadDir(filepath.Join(root, "packs"))
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	// ReadDir is sorted
	return names
}

// EnabledPackNames mirrors Python enabled_pack_names.
func EnabledPackNames(root string, raw []string) ([]string, error) {
	if len(raw) == 0 || (len(raw) == 1 && raw[0] == "all") {
		return append([]string{kit.ClaudeRootPlugin}, FindPackDirs(root)...), nil
	}
	allowed := map[string]bool{kit.ClaudeRootPlugin: true}
	for _, name := range FindPackDirs(root) {
		allowed[name] = true
	}
	var unknown []string
	for _, name := range raw {
		if !allowed[name] {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) > 0 {
		return nil, kit.NewErrorf("Unknown Claude plugin or pack(s): %s", strings.Join(unknown, ", "))
	}
	return raw, nil
}

// ClaudeSetupOptions configure Claude Code marketplace registration.
type ClaudeSetupOptions struct {
	Root            string
	Home            string
	Settings        string
	MarketplacePath string
	Enable          []string
}

// ClaudeSetup updates ~/.claude/settings.json with marketplace key aok-claude-code.
func ClaudeSetup(opts ClaudeSetupOptions, stdout io.Writer) error {
	home := opts.Home
	if home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return err
		}
	}
	settingsPath := opts.Settings
	if settingsPath == "" {
		settingsPath = filepath.Join(home, ".claude", "settings.json")
	} else {
		settingsPath = expandPath(settingsPath)
	}
	marketplacePath := opts.MarketplacePath
	if marketplacePath == "" {
		marketplacePath = filepath.Join(opts.Root, "adapters", "claude-code", "marketplace")
	} else {
		marketplacePath = expandPath(marketplacePath)
	}
	marketplacePath, _ = filepath.Abs(marketplacePath)
	if _, err := os.Stat(marketplacePath); err != nil {
		return kit.NewErrorf("Claude Code marketplace path does not exist: %s", marketplacePath)
	}

	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		return err
	}
	var backup string
	if _, err := os.Stat(settingsPath); err == nil {
		backup = settingsPath + ".aok-backup-" + time.Now().UTC().Format("20060102150405")
		if err := copyFile(settingsPath, backup); err != nil {
			return err
		}
	}

	data := map[string]any{}
	if raw, err := os.ReadFile(settingsPath); err == nil {
		if err := json.Unmarshal(raw, &data); err != nil {
			return kit.NewErrorf("Claude Code settings must be a JSON object: %s", settingsPath)
		}
	}
	marketplaces, _ := data["extraKnownMarketplaces"].(map[string]any)
	if marketplaces == nil {
		marketplaces = map[string]any{}
		data["extraKnownMarketplaces"] = marketplaces
	}
	marketplaces[kit.ClaudeMarketplaceName] = map[string]any{
		"source": map[string]any{
			"source": "directory",
			"path":   marketplacePath,
		},
	}
	enabled, _ := data["enabledPlugins"].(map[string]any)
	if enabled == nil {
		enabled = map[string]any{}
		data["enabledPlugins"] = enabled
	}
	packs, err := EnabledPackNames(opts.Root, opts.Enable)
	if err != nil {
		return err
	}
	for _, pack := range packs {
		enabled[pack+"@"+kit.ClaudeMarketplaceName] = true
	}
	if err := kit.WriteJSON(settingsPath, data); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Updated Claude Code settings: %s\n", settingsPath)
	if backup != "" {
		fmt.Fprintf(stdout, "Backup: %s\n", backup)
	}
	fmt.Fprintf(stdout, "Marketplace: %s -> %s\n", kit.ClaudeMarketplaceName, marketplacePath)
	return nil
}

func expandPath(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") || strings.HasPrefix(path, "~\\") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		if path == "~" {
			return home
		}
		return filepath.Join(home, path[2:])
	}
	return path
}

func copyFile(src, dest string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dest, data, 0o644)
}

func detectWorkspaceKind(target string) string {
	if _, err := os.Stat(filepath.Join(target, ".git")); err == nil {
		return "single-repo"
	}
	entries, err := os.ReadDir(target)
	if err != nil {
		return "plain-directory"
	}
	for _, e := range entries {
		if e.IsDir() {
			if _, err := os.Stat(filepath.Join(target, e.Name(), ".git")); err == nil {
				return "multi-repo"
			}
		}
	}
	return "plain-directory"
}

func unifiedDiff(oldText, newText, oldLabel, newLabel string) string {
	// Minimal unified-diff-like output sufficient for review files.
	var b strings.Builder
	fmt.Fprintf(&b, "--- %s\n+++ %s\n", oldLabel, newLabel)
	oldLines := strings.SplitAfter(oldText, "\n")
	newLines := strings.SplitAfter(newText, "\n")
	// Simple full-file replace view when contents differ.
	for _, line := range oldLines {
		if line == "" {
			continue
		}
		b.WriteString("-")
		b.WriteString(line)
		if !strings.HasSuffix(line, "\n") {
			b.WriteByte('\n')
		}
	}
	for _, line := range newLines {
		if line == "" {
			continue
		}
		b.WriteString("+")
		b.WriteString(line)
		if !strings.HasSuffix(line, "\n") {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.Mode()&0o111 != 0
}

// ProjectInit copies templates into target (Python copy_template_tree + cmd_project_init).
func ProjectInit(root, target string, force bool, stdout io.Writer) error {
	target, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	info, err := os.Stat(target)
	if err != nil || !info.IsDir() {
		return kit.NewErrorf("Target is not a directory: %s", target)
	}
	changes, err := copyTemplateTree(root, target, force)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Initialized %s\n", target)
	fmt.Fprintf(stdout, "Workspace kind: %s\n", detectWorkspaceKind(target))
	for _, change := range changes {
		fmt.Fprintf(stdout, "- %s\n", change)
	}
	return nil
}

func copyTemplateTree(root, target string, force bool) ([]string, error) {
	templateRoot := filepath.Join(root, "templates")
	timestamp := time.Now().UTC().Format("20060102150405")
	backupRoot := filepath.Join(target, ".aok-backups", timestamp)
	var changes []string

	err := filepath.Walk(templateRoot, func(src string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(templateRoot, src)
		if err != nil {
			return err
		}
		dest := filepath.Join(target, rel)
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		newBytes, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		newText := string(newBytes)
		if _, err := os.Stat(dest); err != nil {
			mode := os.FileMode(0o644)
			if isExecutable(src) {
				mode = 0o755
			}
			if err := os.WriteFile(dest, newBytes, mode); err != nil {
				return err
			}
			changes = append(changes, "created "+filepath.ToSlash(rel))
			return nil
		}
		oldBytes, err := os.ReadFile(dest)
		if err != nil {
			return err
		}
		oldText := string(oldBytes)
		if oldText == newText {
			changes = append(changes, "unchanged "+filepath.ToSlash(rel))
			return nil
		}
		if filepath.ToSlash(rel) == "AGENTS.md" && !force {
			proposed := filepath.Join(target, "AGENTS.aok-proposed.md")
			if err := os.WriteFile(proposed, newBytes, 0o644); err != nil {
				return err
			}
			diffPath := filepath.Join(target, "AGENTS.aok.diff")
			if err := os.WriteFile(diffPath, []byte(unifiedDiff(oldText, newText, "AGENTS.md", "AGENTS.aok-proposed.md")), 0o644); err != nil {
				return err
			}
			changes = append(changes, "preserved AGENTS.md; wrote AGENTS.aok-proposed.md and AGENTS.aok.diff")
			return nil
		}
		backupRel := filepath.ToSlash(rel)
		backupDest := filepath.Join(backupRoot, rel)
		if err := os.MkdirAll(filepath.Dir(backupDest), 0o755); err != nil {
			return err
		}
		if err := copyFile(dest, backupDest); err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if isExecutable(src) {
			mode = 0o755
		}
		if err := os.WriteFile(dest, newBytes, mode); err != nil {
			return err
		}
		backupShown, _ := filepath.Rel(target, backupDest)
		changes = append(changes, fmt.Sprintf("updated %s; backup %s", filepath.ToSlash(rel), filepath.ToSlash(backupShown)))
		_ = backupRel
		return nil
	})
	if err != nil {
		return nil, err
	}

	meta := filepath.Join(target, ".aok")
	if err := os.MkdirAll(meta, 0o755); err != nil {
		return nil, err
	}
	projectJSON := map[string]any{
		"initializedAt": time.Now().UTC().Format(time.RFC3339),
		"workspaceKind": detectWorkspaceKind(target),
		"kitVersion":    kit.MustVersion(root),
	}
	if err := kit.WriteJSON(filepath.Join(meta, "project.json"), projectJSON); err != nil {
		return nil, err
	}
	changes = append(changes, "wrote .aok/project.json")
	return changes, nil
}

// ValidateProject returns missing/invalid items for a project tree.
func ValidateProject(path string) []string {
	path, _ = filepath.Abs(path)
	required := []string{
		"AGENTS.md",
		"agents/workflow.md",
		"agents/github-project.md",
		"agents/hooks.md",
		"agents/tasks/task-template.md",
		"agents/roles/orchestrator.md",
		"agents/roles/implementer.md",
		"agents/roles/qa-reality-checker.md",
		"agents/roles/code-reviewer.md",
		".codex/config.toml",
		".codex/setup.sh",
		".codex/aok-action.sh",
		".codex/aok-local-environment.json",
		".codex/environments/environment.toml",
	}
	var missing []string
	for _, rel := range required {
		if _, err := os.Stat(filepath.Join(path, filepath.FromSlash(rel))); err != nil {
			missing = append(missing, rel)
		}
	}
	localEnv := kit.CollectCodexLocalEnv(path)
	for _, item := range localEnv["files"].([]map[string]any) {
		if item["ok"].(bool) {
			if execv, ok := item["executable"].(bool); ok && !execv {
				missing = append(missing, item["relativePath"].(string)+" must be executable")
			}
		}
	}
	manifest := localEnv["manifest"].(map[string]any)
	if _, err := os.Stat(filepath.Join(path, ".codex", "aok-local-environment.json")); err == nil && !manifest["ok"].(bool) {
		missing = append(missing, ".codex/aok-local-environment.json must be a valid AOK Codex local-environment manifest")
	}
	for _, action := range localEnv["missingActions"].([]string) {
		missing = append(missing, ".codex/aok-local-environment.json missing action "+action)
	}
	for _, action := range localEnv["invalidActions"].([]string) {
		missing = append(missing, ".codex/aok-local-environment.json invalid action "+action)
	}
	return missing
}

// CodexLocalEnvInit copies required .codex templates into target.
func CodexLocalEnvInit(root, target string, force bool, stdout io.Writer) error {
	target, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	info, err := os.Stat(target)
	if err != nil || !info.IsDir() {
		return kit.NewErrorf("Target is not a directory: %s", target)
	}
	changes, err := copyCodexLocalEnvTemplates(root, target, force)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Initialized Codex local environment: %s\n", target)
	for _, change := range changes {
		fmt.Fprintf(stdout, "- %s\n", change)
	}
	fmt.Fprintln(stdout, "Register .codex/setup.sh and .codex/aok-action.sh actions in Codex App settings.")
	return nil
}

func copyCodexLocalEnvTemplates(root, target string, force bool) ([]string, error) {
	templateRoot := filepath.Join(root, "templates")
	timestamp := time.Now().UTC().Format("20060102150405")
	backupRoot := filepath.Join(target, ".aok-backups", timestamp)
	var changes []string
	for _, relRaw := range kit.CodexLocalEnvRequired {
		rel := filepath.FromSlash(relRaw)
		src := filepath.Join(templateRoot, rel)
		dest := filepath.Join(target, rel)
		if _, err := os.Stat(src); err != nil {
			return nil, kit.NewErrorf("Missing Codex local-environment template: %s", src)
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return nil, err
		}
		newBytes, err := os.ReadFile(src)
		if err != nil {
			return nil, err
		}
		newText := string(newBytes)
		if _, err := os.Stat(dest); err != nil {
			mode := os.FileMode(0o644)
			if isExecutable(src) {
				mode = 0o755
			}
			if err := os.WriteFile(dest, newBytes, mode); err != nil {
				return nil, err
			}
			changes = append(changes, "created "+relRaw)
			continue
		}
		oldBytes, err := os.ReadFile(dest)
		if err != nil {
			return nil, err
		}
		if string(oldBytes) == newText {
			changes = append(changes, "unchanged "+relRaw)
			continue
		}
		if !force {
			proposed := dest + ".aok-proposed"
			if err := os.WriteFile(proposed, newBytes, 0o644); err != nil {
				return nil, err
			}
			diffPath := dest + ".aok.diff"
			proposedRel, _ := filepath.Rel(target, proposed)
			if err := os.WriteFile(diffPath, []byte(unifiedDiff(string(oldBytes), newText, relRaw, filepath.ToSlash(proposedRel))), 0o644); err != nil {
				return nil, err
			}
			diffRel, _ := filepath.Rel(target, diffPath)
			changes = append(changes, fmt.Sprintf("preserved %s; wrote %s and %s", relRaw, filepath.ToSlash(proposedRel), filepath.ToSlash(diffRel)))
			continue
		}
		backupDest := filepath.Join(backupRoot, rel)
		if err := os.MkdirAll(filepath.Dir(backupDest), 0o755); err != nil {
			return nil, err
		}
		if err := copyFile(dest, backupDest); err != nil {
			return nil, err
		}
		mode := os.FileMode(0o644)
		if isExecutable(src) {
			mode = 0o755
		}
		if err := os.WriteFile(dest, newBytes, mode); err != nil {
			return nil, err
		}
		backupShown, _ := filepath.Rel(target, backupDest)
		changes = append(changes, fmt.Sprintf("updated %s; backup %s", relRaw, filepath.ToSlash(backupShown)))
	}
	return changes, nil
}
