package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/evidence"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/modelcatalog"
)

func runModels(args []string, stdout, stderr io.Writer) (int, error) {
	if len(args) > 0 && args[0] == "select" {
		return runSelection(args[1:], stdout, stderr)
	}
	usage := func(w io.Writer) {
		fmt.Fprintln(w, `Usage:
  aok models select --task <file> --candidates <file> --catalog <bundle> --catalog-policy <file> --usage <file> --usage-policy <file> --policy <file> --evidence-policy <file> --as-of <RFC3339> --out <new-directory> [--json]
  aok models import --manifest <file> --policy <file> --out <new-directory> [--json]
  aok models validate <bundle> --policy <file> [--json]
  aok models inspect <bundle> --as-of <RFC3339> [--json]
  aok models diff <old-bundle> <new-bundle> --as-of <RFC3339> [--json]
  aok models refresh <old-bundle> --manifest <file> --policy <file> --out <new-directory> [--json]
  aok models check --catalog <bundle> --policy <file> --profile <file> --as-of <RFC3339> [--json]
Offline evidence only. No inference, quota collection or runtime authority. Use models select for offline advisory selection.`)
	}
	if len(args) == 1 && (args[0] == "--help" || args[0] == "help" || args[0] == "-h") {
		usage(stdout)
		return 0, nil
	}
	if len(args) == 0 {
		usage(stderr)
		return 2, nil
	}
	op := args[0]
	allowed := map[string][]string{
		"import":   {"--manifest", "--policy", "--out"},
		"validate": {"--policy"}, "inspect": {"--as-of"}, "diff": {"--as-of"},
		"refresh": {"--manifest", "--policy", "--out"},
		"check":   {"--catalog", "--policy", "--profile", "--as-of"},
	}
	flags, known := allowed[op]
	if !known {
		usage(stderr)
		return 2, nil
	}
	values := map[string]string{}
	pos := []string{}
	machine := false
	for i := 1; i < len(args); i++ {
		a := args[i]
		if a == "--help" || a == "-h" {
			usage(stdout)
			return 0, nil
		}
		if a == "--json" {
			if machine {
				usage(stderr)
				return 2, nil
			}
			machine = true
			continue
		}
		if strings.HasPrefix(a, "-") {
			valid := false
			for _, flag := range flags {
				if flag == a {
					valid = true
				}
			}
			if !valid || values[a] != "" || i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") || args[i+1] == "" {
				usage(stderr)
				return 2, nil
			}
			i++
			values[a] = args[i]
		} else {
			pos = append(pos, a)
		}
	}
	for _, flag := range flags {
		if values[flag] == "" {
			usage(stderr)
			return 2, nil
		}
	}
	wantPos := map[string]int{"import": 0, "validate": 1, "inspect": 1, "diff": 2, "refresh": 1, "check": 0}[op]
	if len(pos) != wantPos {
		usage(stderr)
		return 2, nil
	}
	var at time.Time
	if values["--as-of"] != "" {
		var err error
		at, err = time.Parse(time.RFC3339Nano, values["--as-of"])
		if err != nil {
			usage(stderr)
			return 2, nil
		}
	}
	invalid := func(field string, err error) modelcatalog.Report {
		return modelcatalog.Report{Status: "invalid", ProbeGate: "pending-p6", Diagnostics: []contracts.Diagnostic{{Code: "catalog." + field, Field: field, Severity: "error", Message: err.Error()}}}
	}
	write := func(value any, status string) (int, error) {
		b, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			return 1, err
		}
		if !machine {
			if _, err = fmt.Fprintf(stdout, "Model catalog %s: %s\n", op, status); err != nil {
				return 1, err
			}
		}
		if _, err = fmt.Fprintf(stdout, "%s\n", b); err != nil {
			return 1, err
		}
		if status != "valid" {
			return 1, nil
		}
		return 0, nil
	}
	load := func(bundle, policy string) (*modelcatalog.Capture, modelcatalog.Report) {
		return modelcatalog.Load(bundle, policy)
	}
	switch op {
	case "import", "refresh":
		var previous *modelcatalog.Capture
		if op == "refresh" {
			var report modelcatalog.Report
			previous, report = load(pos[0], "")
			if previous == nil || report.Status != "valid" {
				return write(report, report.Status)
			}
		}
		capture, report := modelcatalog.Import(values["--manifest"], values["--policy"])
		if capture == nil || report.Status != "valid" {
			return write(report, report.Status)
		}
		if previous != nil {
			// Refresh compares structure without inventing a wall-clock instant.
			// Explicit `models diff --as-of` additionally evaluates freshness.
			comparison := modelcatalog.Diff(previous, capture, time.Time{})
			data, err := json.MarshalIndent(comparison, "", "  ")
			if err != nil {
				return 1, err
			}
			capture.Files["refresh-report.json"] = append(data, '\n')
		}
		protected := []string{filepath.Dir(values["--manifest"]), values["--policy"]}
		if op == "refresh" {
			protected = append(protected, pos[0])
		}
		if err := evidence.WriteFiles(values["--out"], capture.Files, protected...); err != nil {
			report = invalid("output", err)
		}
		return write(report, report.Status)
	case "validate":
		_, report := load(pos[0], values["--policy"])
		return write(report, report.Status)
	case "inspect":
		capture, report := load(pos[0], "")
		if capture == nil || report.Status != "valid" {
			return write(report, report.Status)
		}
		result := modelcatalog.Inspect(capture, at)
		return write(result, result.Status)
	case "diff":
		old, report := load(pos[0], "")
		if old == nil || report.Status != "valid" {
			return write(report, report.Status)
		}
		newer, report := load(pos[1], "")
		if newer == nil || report.Status != "valid" {
			return write(report, report.Status)
		}
		result := modelcatalog.Diff(old, newer, at)
		return write(result, result.Status)
	case "check":
		capture, report := load(values["--catalog"], values["--policy"])
		if capture == nil || report.Status != "valid" {
			return write(report, report.Status)
		}
		data, err := modelcatalog.ReadFile(values["--profile"])
		if err != nil {
			report = invalid("profile", err)
			return write(report, report.Status)
		}
		parsed, ds, err := contracts.Parse(data)
		if err != nil {
			report = invalid("profile", err)
			report.Diagnostics = append(report.Diagnostics, ds...)
			return write(report, report.Status)
		}
		resource := parsed.(contracts.Resource)
		if resource.Kind != "ExecutionProfile" {
			report = invalid("profile", fmt.Errorf("expected ExecutionProfile"))
			return write(report, report.Status)
		}
		decoded, err := contracts.DecodeTyped(resource)
		if err != nil {
			report = invalid("profile", err)
			return write(report, report.Status)
		}
		result := modelcatalog.CheckWithProfileDigest(capture, decoded.(contracts.ExecutionProfile), modelcatalog.Hash(data), at)
		return write(result, result.Status)
	}
	return 2, nil
}
