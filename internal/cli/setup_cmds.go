package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/doctor"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/install"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/kit"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/setup"
)

func runClaude(args []string, stdout, stderr io.Writer) (int, error) {
	if len(args) == 0 || args[0] != "setup" {
		return 2, kit.NewError("usage: aok claude setup [--home] [--settings] [--marketplace-path] [--enable …]")
	}
	return runClaudeSetup(args[1:], stdout, stderr)
}

func runClaudeSetup(args []string, stdout, _ io.Writer) (int, error) {
	fs := flag.NewFlagSet("claude setup", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	homeFlag := fs.String("home", "", "")
	settings := fs.String("settings", "", "")
	marketplace := fs.String("marketplace-path", "", "")
	// Collect --enable values manually after parse of known flags.
	enable := []string{"all"}
	filtered := filterEnableArgs(args, &enable)
	if err := fs.Parse(filtered); err != nil {
		return 2, nil
	}
	root, err := requireRoot()
	if err != nil {
		return 1, err
	}
	home, err := kit.UserHome(*homeFlag)
	if err != nil {
		return 1, err
	}
	if err := setup.ClaudeSetup(setup.ClaudeSetupOptions{
		Root:            root,
		Home:            home,
		Settings:        *settings,
		MarketplacePath: *marketplace,
		Enable:          enable,
	}, stdout); err != nil {
		return 1, err
	}
	return 0, nil
}

func filterEnableArgs(args []string, enable *[]string) []string {
	var out []string
	i := 0
	for i < len(args) {
		if args[i] == "--enable" {
			i++
			var vals []string
			for i < len(args) && !strings.HasPrefix(args[i], "-") {
				vals = append(vals, args[i])
				i++
			}
			if len(vals) > 0 {
				*enable = vals
			}
			continue
		}
		if strings.HasPrefix(args[i], "--enable=") {
			*enable = strings.Split(strings.TrimPrefix(args[i], "--enable="), ",")
			i++
			continue
		}
		out = append(out, args[i])
		i++
	}
	return out
}

func runProject(args []string, stdout, stderr io.Writer) (int, error) {
	if len(args) == 0 {
		return 2, kit.NewError("usage: aok project init|validate [path]")
	}
	switch args[0] {
	case "init":
		return runProjectInit(args[1:], stdout, stderr)
	case "validate":
		return runProjectValidate(args[1:], stdout, stderr)
	default:
		return 2, kit.NewErrorf("unknown project command: %s", args[0])
	}
}

func runProjectInit(args []string, stdout, _ io.Writer) (int, error) {
	fs := flag.NewFlagSet("project init", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	force := fs.Bool("force", false, "")
	if err := fs.Parse(args); err != nil {
		return 2, nil
	}
	path := "."
	if fs.NArg() > 0 {
		path = fs.Arg(0)
	}
	root, err := requireRoot()
	if err != nil {
		return 1, err
	}
	if err := setup.ProjectInit(root, path, *force, stdout); err != nil {
		return 1, err
	}
	return 0, nil
}

func runProjectValidate(args []string, stdout, _ io.Writer) (int, error) {
	fs := flag.NewFlagSet("project validate", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		return 2, nil
	}
	path := "."
	if fs.NArg() > 0 {
		path = fs.Arg(0)
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return 1, err
	}
	missing := setup.ValidateProject(path)
	if len(missing) > 0 {
		fmt.Fprintln(stdout, "Project validation failed. Missing:")
		for _, rel := range missing {
			fmt.Fprintf(stdout, "- %s\n", rel)
		}
		return 1, nil
	}
	fmt.Fprintf(stdout, "Project validation passed: %s\n", path)
	return 0, nil
}

func runCodex(args []string, stdout, stderr io.Writer) (int, error) {
	if len(args) == 0 {
		return 2, kit.NewError("usage: aok codex setup|local-env …")
	}
	switch args[0] {
	case "setup":
		return runCodexSetup(args[1:], stdout, stderr)
	case "local-env":
		return runCodexLocalEnv(args[1:], stdout, stderr)
	default:
		return 2, kit.NewErrorf("unknown codex command: %s", args[0])
	}
}

func runCodexSetup(args []string, stdout, stderr io.Writer) (int, error) {
	fs := flag.NewFlagSet("codex setup", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	homeFlag := fs.String("home", "", "")
	replace := fs.Bool("replace", false, "")
	report := fs.String("report", "", "")
	project := fs.String("project", "", "")
	forceProject := fs.Bool("force-project-init", false, "")
	claude := fs.Bool("claude", false, "")
	claudeSettings := fs.String("claude-settings", "", "")
	claudeMarketplace := fs.String("claude-marketplace-path", "", "")
	enable := []string{"all"}
	filtered := filterEnableArgs(args, &enable)
	if err := fs.Parse(filtered); err != nil {
		return 2, nil
	}

	root, err := requireRoot()
	if err != nil {
		return 1, err
	}
	home, err := kit.UserHome(*homeFlag)
	if err != nil {
		return 1, err
	}

	result, err := install.Run(install.Options{Root: root, Home: home, Replace: *replace}, stderr)
	if err != nil {
		return 1, err
	}
	_ = result

	doctorReport := doctor.Collect(root, home)
	if *report != "" {
		out := kitExpand(*report)
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return 1, err
		}
		if err := os.WriteFile(out, []byte(doctor.ReportText(doctorReport)), 0o644); err != nil {
			return 1, err
		}
		fmt.Fprintf(stdout, "Wrote setup report: %s\n", out)
	} else {
		doctor.PrintHuman(stdout, doctorReport)
	}
	doctorOK := doctorReport["requiredOk"].(bool)

	var projectPath string
	if *project != "" {
		projectPath, err = filepath.Abs(*project)
		if err != nil {
			return 1, err
		}
		if err := setup.ProjectInit(root, projectPath, *forceProject, stdout); err != nil {
			return 1, err
		}
		missing := setup.ValidateProject(projectPath)
		if len(missing) > 0 {
			fmt.Fprintln(stdout, "Project validation failed. Missing:")
			for _, rel := range missing {
				fmt.Fprintf(stdout, "- %s\n", rel)
			}
		} else {
			fmt.Fprintf(stdout, "Project validation passed: %s\n", projectPath)
		}
		local := kit.CollectCodexLocalEnv(projectPath)
		printLocalEnvHuman(stdout, local)
	}

	if *claude {
		if err := setup.ClaudeSetup(setup.ClaudeSetupOptions{
			Root:            root,
			Home:            home,
			Settings:        *claudeSettings,
			MarketplacePath: *claudeMarketplace,
			Enable:          enable,
		}, stdout); err != nil {
			return 1, err
		}
	}

	fmt.Fprintln(stdout, "Codex setup summary:")
	installState := "checked"
	if *replace {
		installState = "updated"
	}
	fmt.Fprintf(stdout, "- install: %s user registration\n", installState)
	doctorState := "needs-attention"
	if doctorOK {
		doctorState = "ok"
	}
	fmt.Fprintf(stdout, "- doctor: %s\n", doctorState)
	if projectPath != "" {
		fmt.Fprintf(stdout, "- project: initialized and validated %s\n", projectPath)
	} else {
		fmt.Fprintln(stdout, "- project: skipped")
	}
	if *claude {
		enabled, _ := setup.EnabledPackNames(root, enable)
		fmt.Fprintf(stdout, "- claude: enabled marketplace plugins (%s)\n", strings.Join(enabled, ", "))
	} else {
		fmt.Fprintln(stdout, "- claude: skipped")
	}
	if doctorOK {
		return 0, nil
	}
	return 1, nil
}

func runCodexLocalEnv(args []string, stdout, stderr io.Writer) (int, error) {
	if len(args) == 0 {
		return 2, kit.NewError("usage: aok codex local-env init|validate|actions [path]")
	}
	switch args[0] {
	case "init":
		return runCodexLocalEnvInit(args[1:], stdout, stderr)
	case "validate":
		return runCodexLocalEnvValidate(args[1:], stdout, stderr)
	case "actions":
		return runCodexLocalEnvActions(args[1:], stdout, stderr)
	default:
		return 2, kit.NewErrorf("unknown local-env command: %s", args[0])
	}
}

func runCodexLocalEnvInit(args []string, stdout, _ io.Writer) (int, error) {
	fs := flag.NewFlagSet("codex local-env init", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	force := fs.Bool("force", false, "")
	if err := fs.Parse(args); err != nil {
		return 2, nil
	}
	path := "."
	if fs.NArg() > 0 {
		path = fs.Arg(0)
	}
	root, err := requireRoot()
	if err != nil {
		return 1, err
	}
	if err := setup.CodexLocalEnvInit(root, path, *force, stdout); err != nil {
		return 1, err
	}
	return 0, nil
}

func runCodexLocalEnvValidate(args []string, stdout, _ io.Writer) (int, error) {
	fs := flag.NewFlagSet("codex local-env validate", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	jsonFlag := fs.Bool("json", false, "")
	if err := fs.Parse(args); err != nil {
		return 2, nil
	}
	path := "."
	if fs.NArg() > 0 {
		path = fs.Arg(0)
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return 1, err
	}
	report := kit.CollectCodexLocalEnv(path)
	if *jsonFlag {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			return 1, err
		}
	} else {
		printLocalEnvHuman(stdout, report)
	}
	if report["templatesOk"].(bool) {
		return 0, nil
	}
	return 1, nil
}

func printLocalEnvHuman(w io.Writer, report map[string]any) {
	for _, item := range report["files"].([]map[string]any) {
		detail := item["path"].(string)
		ok := item["ok"].(bool)
		if execv, isBool := item["executable"].(bool); isBool && !execv {
			detail = detail + " (not executable)"
			ok = false
		}
		printOK(w, item["relativePath"].(string), ok, detail)
	}
	manifest := report["manifest"].(map[string]any)
	printOK(w, "manifest", manifest["ok"].(bool), manifest["path"].(string))
	env := report["environmentToml"].(map[string]any)
	printOK(w, "environment.toml", env["ok"].(bool), env["path"].(string))
	for _, missing := range report["missingActions"].([]string) {
		printOK(w, "action "+missing, false, "")
	}
	for _, invalid := range report["invalidActions"].([]string) {
		printOK(w, "action "+invalid, false, "missing command")
	}
	cfg := report["codexGeneratedConfig"].(map[string]any)
	status, _ := cfg["status"].(string)
	if status == "" {
		status = "unknown"
	}
	printOK(w, "Codex generated config schema", true, status)
}

func printOK(w io.Writer, label string, ok bool, detail string) {
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

func runCodexLocalEnvActions(args []string, stdout, _ io.Writer) (int, error) {
	fs := flag.NewFlagSet("codex local-env actions", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		return 2, nil
	}
	path := "."
	if fs.NArg() > 0 {
		path = fs.Arg(0)
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return 1, err
	}
	report := kit.CollectCodexLocalEnv(path)
	missing := report["missingActions"].([]string)
	if len(missing) > 0 {
		return 1, kit.NewErrorf("Codex local-environment actions are incomplete: %s", strings.Join(missing, ", "))
	}
	invalid := report["invalidActions"].([]string)
	if len(invalid) > 0 {
		return 1, kit.NewErrorf("Codex local-environment actions are malformed: %s", strings.Join(invalid, ", "))
	}
	for _, raw := range report["actions"].([]any) {
		action := raw.(map[string]any)
		fmt.Fprintf(stdout, "%v: %v\n", action["name"], action["command"])
	}
	return 0, nil
}
