package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/agentresolve"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/evidence"
)

func runAgentResolve(args []string, stdout, stderr io.Writer) (int, error) {
	usage := func(w io.Writer) {
		fmt.Fprintln(w, "Usage: aok agent resolve <assignment> --out <new-directory> [--root <corpus>] [--json]")
	}
	var assignment, root, out string
	machine := false
	seen := map[string]bool{}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--help", "-h":
			usage(stdout)
			return 0, nil
		case "--json":
			machine = true
		case "--root", "--out":
			if seen[arg] || i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
				usage(stderr)
				return 2, nil
			}
			seen[arg] = true
			i++
			if arg == "--root" {
				root = args[i]
			} else {
				out = args[i]
			}
		default:
			if strings.HasPrefix(arg, "-") || assignment != "" {
				usage(stderr)
				return 2, nil
			}
			assignment = arg
		}
	}
	if assignment == "" || out == "" {
		usage(stderr)
		return 2, nil
	}
	report := agentresolve.Resolve(assignment, root)
	protected := root
	if protected == "" {
		absolute, err := filepath.Abs(assignment)
		if err != nil {
			return 1, err
		}
		protected = filepath.Dir(absolute)
	}
	if err := evidence.WriteJSON(out, "resolution.json", report, protected); err != nil {
		report.Status = "invalid"
		report.Diagnostics = append(report.Diagnostics, contracts.Diagnostic{Code: "resolution.output", Source: out, Field: "out", Severity: "error", Message: err.Error()})
	}
	if machine {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			return 1, err
		}
	} else {
		fmt.Fprintf(stdout, "Agent resolution: %s\n", report.Status)
		for _, d := range report.Diagnostics {
			fmt.Fprintf(stdout, "%s %s %s %s: %s\n", d.Severity, d.Code, d.Source, d.Field, d.Message)
		}
	}
	if report.Status != "valid" {
		return 1, nil
	}
	return 0, nil
}
