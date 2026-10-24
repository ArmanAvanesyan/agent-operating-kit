package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/doctor"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/kit"
)

const deliveryLauncher = "scripts/repo-delivery"

func runDelivery(args []string, stdout, stderr io.Writer) (int, error) {
	if len(args) > 0 && args[0] == "windows-preservation-evaluate" {
		return runWindowsPreservationEvaluate(args[1:], stdout, stderr)
	}
	if len(args) > 0 && (args[0] == "handoff" || args[0] == "validate-handoff") {
		return runDeliveryBridge(args, stdout, stderr)
	}
	if len(args) > 0 && args[0] == "doctor" {
		if len(args) == 1 || (len(args) == 2 && args[1] == "--json") {
			return runDeliveryDoctor(stdout)
		}
		return 2, fmt.Errorf("Usage: aok delivery doctor --json")
	}
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		fmt.Fprintln(stdout, "Usage: aok delivery <repository-delivery arguments>\nRuns the repository-delivery pack using Python 3.11 or newer.\nLocal Go receipt bridge: handoff, validate-handoff.\nCommands: doctor, inspect, compare, checkpoint, snapshot, delivery, gates, review, workspace, port, cleanup, issue, pr, backlog, integration, aok, runtime, hook.\nUse aok delivery <command> --help for arguments.")
		return 0, nil
	}
	root, err := requireRoot()
	if err != nil {
		return 1, capabilityError("AOK kit root unavailable", err)
	}
	launcher := filepath.Join(root, "packs", "repository-delivery", deliveryLauncher)
	if info, err := os.Stat(launcher); err != nil || info.IsDir() {
		return 1, fmt.Errorf("repository-delivery payload is missing at %s; install or update the AOK pack", launcher)
	}
	python, _, err := findDeliveryPython()
	if err != nil {
		return 1, err
	}
	cmd := exec.Command(python, append([]string{"-I", launcher}, args...)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, stdout, stderr
	cmd.Dir, _ = os.Getwd()
	cmd.Env = os.Environ()
	err = cmd.Run()
	if err == nil {
		return 0, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), nil
	}
	return 1, fmt.Errorf("could not start repository-delivery: %w", err)
}

func capabilityError(label string, err error) error { return fmt.Errorf("%s: %w", label, err) }

func findDeliveryPython() (string, string, error) {
	candidates := []string{}
	if explicit := strings.TrimSpace(os.Getenv("AOK_DELIVERY_PYTHON")); explicit != "" {
		candidates = append(candidates, explicit)
	} else {
		candidates = []string{"python3", "python3.11", "python"}
	}
	for _, candidate := range candidates {
		path, err := exec.LookPath(candidate)
		if err != nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		cmd := exec.CommandContext(ctx, path, "-I", "-c", "import sys; print('%d.%d.%d' % sys.version_info[:3]); raise SystemExit(0 if sys.version_info >= (3,11) else 1)")
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = io.Discard
		runErr := cmd.Run()
		cancel()
		if runErr == nil {
			return path, strings.TrimSpace(out.String()), nil
		}
	}
	return "", "", fmt.Errorf("Python 3.11 or newer is required for repository delivery; set AOK_DELIVERY_PYTHON to an executable Python 3.11+ interpreter")
}

type deliveryDoctor struct {
	Payload           string   `json:"payload"`
	Python            string   `json:"python"`
	PythonVersion     string   `json:"pythonVersion"`
	Git               bool     `json:"git"`
	GH                bool     `json:"gh"`
	JSONSchema        bool     `json:"jsonschema"`
	JSONSchemaVersion string   `json:"jsonschemaVersion,omitempty"`
	PlatformSupported bool     `json:"platformSupported"`
	FeatureLimits     []string `json:"featureLimits"`
}

func runDeliveryDoctor(out io.Writer) (int, error) {
	root, err := requireRoot()
	if err != nil {
		return 1, err
	}
	home, err := kit.UserHome("")
	if err != nil {
		return 1, err
	}
	baseReport := doctor.Collect(root, home)
	payload := filepath.Join(root, "packs", "repository-delivery")
	report := deliveryDoctor{Payload: payload, PlatformSupported: runtime.GOOS == "darwin" || runtime.GOOS == "linux", FeatureLimits: []string{"Go doctor reports local capability only; it does not check authentication or invoke MCP."}}
	if _, err := os.Stat(filepath.Join(payload, deliveryLauncher)); err != nil {
		report.FeatureLimits = append(report.FeatureLimits, "repository-delivery payload is unavailable")
	}
	if p, v, err := findDeliveryPython(); err == nil {
		report.Python, report.PythonVersion = p, v
	} else {
		report.FeatureLimits = append(report.FeatureLimits, err.Error())
	}
	_, err = exec.LookPath("git")
	report.Git = err == nil
	_, err = exec.LookPath("gh")
	report.GH = err == nil
	// jsonschema is checked in the isolated interpreter, without importing project files.
	if report.Python != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, report.Python, "-I", "-c", "import importlib.metadata as m; import sys; v=m.version('jsonschema'); print(v); p=v.split('.'); sys.exit(0 if int(p[0]) == 4 and int(p[1]) >= 26 else 1)")
		if version, err := cmd.Output(); err == nil {
			report.JSONSchema = true
			report.JSONSchemaVersion = strings.TrimSpace(string(version))
		}
	}
	if !report.PlatformSupported {
		report.FeatureLimits = append(report.FeatureLimits, "repository-delivery helpers require macOS or Linux")
	}
	if !report.Git {
		report.FeatureLimits = append(report.FeatureLimits, "git-dependent delivery features unavailable")
	}
	if !report.GH {
		report.FeatureLimits = append(report.FeatureLimits, "GitHub delivery features unavailable without gh")
	}
	if !report.JSONSchema {
		report.FeatureLimits = append(report.FeatureLimits, "JSON Schema validation requires jsonschema>=4.26,<5 in the interpreter selected by AOK_DELIVERY_PYTHON")
	}
	baseReport["delivery"] = report
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(baseReport); err != nil {
		return 1, err
	}
	if ok, _ := baseReport["requiredOk"].(bool); !ok {
		return 1, nil
	}
	return 0, nil
}
