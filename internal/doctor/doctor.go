package doctor

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/kit"
)

// Collect builds the aok-doctor-report structure matching Python collect_doctor.
func Collect(root, home string) map[string]any {
	required := []string{
		".codex-plugin/plugin.json",
		"hooks.json",
		"templates/AGENTS.md",
		"templates/agents/workflow.md",
		"templates/.codex/setup.sh",
		"templates/.codex/aok-action.sh",
		"templates/.codex/aok-local-environment.json",
		"templates/.codex/environments/environment.toml",
		"docs/codex-local-environments.md",
		"scripts/aok",
	}
	marketplace, linkPath := kit.MarketplacePaths(home)
	cliPath := kit.CLIShimPath(home)

	pluginOK := false
	if _, err := os.Lstat(linkPath); err == nil {
		resolved, err := filepath.EvalSymlinks(linkPath)
		if err == nil {
			resolved, _ = filepath.Abs(resolved)
			rootAbs, _ := filepath.Abs(root)
			if resolved == rootAbs {
				pluginOK = true
			}
		}
		if !pluginOK && runtime.GOOS == "windows" && pluginDirLooksValid(linkPath) {
			pluginOK = true
		}
	}

	commands := map[string]any{}
	for _, name := range []string{"git", "gh", "codex"} {
		path, err := exec.LookPath(name)
		ok := err == nil
		var pathVal any
		if ok {
			pathVal = path
		} else {
			pathVal = nil
		}
		commands[name] = map[string]any{
			"ok":       ok,
			"path":     pathVal,
			"required": false,
		}
	}

	files := make([]map[string]any, 0, len(required))
	allFilesOK := true
	for _, rel := range required {
		path := filepath.Join(root, filepath.FromSlash(rel))
		_, err := os.Stat(path)
		ok := err == nil
		if !ok {
			allFilesOK = false
		}
		files = append(files, map[string]any{
			"path":         path,
			"relativePath": rel,
			"ok":           ok,
		})
	}

	_, marketErr := os.Stat(marketplace)
	marketOK := marketErr == nil
	_, shimErr := os.Stat(cliPath)
	shimOK := shimErr == nil

	install := map[string]any{
		"marketplace": map[string]any{
			"path": marketplace,
			"ok":   marketOK,
		},
		"pluginPath": map[string]any{
			"path":   linkPath,
			"ok":     pluginOK,
			"target": root,
		},
		"cliShim": map[string]any{
			"path": cliPath,
			"ok":   shimOK,
		},
	}

	codexLocal := kit.CollectCodexLocalEnv(filepath.Join(root, "templates"))
	templatesOK, _ := codexLocal["templatesOk"].(bool)
	requiredOK := allFilesOK && marketOK && pluginOK && templatesOK

	next := []any{}
	if !requiredOK {
		next = append(next, "Run the AOK installer again or check the marketplace/plugin paths.")
	}

	status := "needs-attention"
	if requiredOK {
		status = "ok"
	}

	return map[string]any{
		"kind":                  "aok-doctor-report",
		"version":               kit.MustVersion(root),
		"kitRoot":               root,
		"home":                  home,
		"status":                status,
		"requiredOk":            requiredOK,
		"commands":              commands,
		"files":                 files,
		"install":               install,
		"codexLocalEnvironment": codexLocal,
		"nextActions":           next,
	}
}

func pluginDirLooksValid(path string) bool {
	return kit.IsKitRoot(path)
}

// ReportText renders the markdown setup report.
func ReportText(report map[string]any) string {
	var b strings.Builder
	b.WriteString("# Agent Operating Kit Setup Report\n\n")
	fmt.Fprintf(&b, "Status: %v\n", report["status"])
	fmt.Fprintf(&b, "Kit root: %v\n", report["kitRoot"])
	fmt.Fprintf(&b, "Home: %v\n\n", report["home"])
	b.WriteString("## Required Files\n")
	for _, raw := range report["files"].([]map[string]any) {
		mark := "FAIL"
		if raw["ok"].(bool) {
			mark = "OK"
		}
		fmt.Fprintf(&b, "- %s %v\n", mark, raw["relativePath"])
	}
	b.WriteString("\n## Installation\n")
	install := report["install"].(map[string]any)
	for _, label := range []string{"marketplace", "pluginPath", "cliShim"} {
		item := install[label].(map[string]any)
		mark := "FAIL"
		if item["ok"].(bool) {
			mark = "OK"
		}
		fmt.Fprintf(&b, "- %s %s: %v\n", mark, label, item["path"])
	}
	b.WriteString("\n## Codex Local Environment\n")
	local := report["codexLocalEnvironment"].(map[string]any)
	mark := "FAIL"
	if local["templatesOk"].(bool) {
		mark = "OK"
	}
	fmt.Fprintf(&b, "- %s templates: %v\n", mark, local["projectPath"])
	cfg := local["codexGeneratedConfig"].(map[string]any)
	fmt.Fprintf(&b, "- config schema status: %v\n", cfg["status"])
	for _, raw := range local["files"].([]map[string]any) {
		m := "FAIL"
		if raw["ok"].(bool) {
			m = "OK"
		}
		fmt.Fprintf(&b, "- %s %v\n", m, raw["relativePath"])
	}
	b.WriteString("\n## Optional Commands\n")
	commands := report["commands"].(map[string]any)
	for _, name := range []string{"git", "gh", "codex"} {
		item := commands[name].(map[string]any)
		detail := "not found"
		if p, ok := item["path"].(string); ok && p != "" {
			detail = p
		}
		m := "MISSING"
		if item["ok"].(bool) {
			m = "OK"
		}
		fmt.Fprintf(&b, "- %s %s: %s\n", m, name, detail)
	}
	if next, ok := report["nextActions"].([]any); ok && len(next) > 0 {
		b.WriteString("\n## Next Actions\n")
		for _, action := range next {
			fmt.Fprintf(&b, "- %v\n", action)
		}
	}
	return b.String()
}

// PrintHuman writes OK/FAIL lines like Python print_result.
func PrintHuman(w io.Writer, report map[string]any) {
	commands := report["commands"].(map[string]any)
	for _, name := range []string{"git", "gh", "codex"} {
		item := commands[name].(map[string]any)
		detail := "not found"
		if p, ok := item["path"].(string); ok && p != "" {
			detail = p
		}
		printResult(w, "optional command "+name, item["ok"].(bool), detail)
	}
	for _, item := range report["files"].([]map[string]any) {
		printResult(w, item["relativePath"].(string), item["ok"].(bool), "")
	}
	install := report["install"].(map[string]any)
	m := install["marketplace"].(map[string]any)
	printResult(w, "marketplace", m["ok"].(bool), m["path"].(string))
	p := install["pluginPath"].(map[string]any)
	printResult(w, "plugin path", p["ok"].(bool), p["path"].(string))
	c := install["cliShim"].(map[string]any)
	printResult(w, "CLI shim", c["ok"].(bool), c["path"].(string))
	local := report["codexLocalEnvironment"].(map[string]any)
	printResult(w, "Codex local environment templates", local["templatesOk"].(bool), local["projectPath"].(string))
}

func printResult(w io.Writer, label string, ok bool, detail string) {
	mark := "FAIL"
	if ok {
		mark = "OK"
	}
	if detail != "" {
		fmt.Fprintf(w, "%s %s - %s\n", mark, label, detail)
	} else {
		fmt.Fprintf(w, "%s %s\n", mark, label)
	}
}

// WriteJSON dumps the report with indent 2.
func WriteJSON(w io.Writer, report map[string]any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}
