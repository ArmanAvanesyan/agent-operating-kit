package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/agentvalidate"
)

func runAgent(args []string, stdout, stderr io.Writer) (int, error) {
	usage := func(w io.Writer) {
		fmt.Fprintln(w, "Usage: aok agent validate <directory> [--json]\nValidate local agent contracts without executing work.")
	}
	if len(args) == 1 && (args[0] == "--help" || args[0] == "help") {
		usage(stdout)
		return 0, nil
	}
	if len(args) == 0 || args[0] != "validate" {
		usage(stderr)
		return 2, nil
	}
	var directory string
	machine := false
	for _, arg := range args[1:] {
		switch {
		case arg == "--help" || arg == "-h":
			usage(stdout)
			return 0, nil
		case arg == "--json":
			machine = true
		case strings.HasPrefix(arg, "-") || directory != "":
			usage(stderr)
			return 2, nil
		default:
			directory = arg
		}
	}
	if directory == "" {
		usage(stderr)
		return 2, nil
	}
	report := agentvalidate.Validate(directory)
	if machine {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			return 1, err
		}
	} else {
		fmt.Fprintf(stdout, "Agent validation: %s\n", report.Status)
		for _, d := range report.Diagnostics {
			fmt.Fprintf(stdout, "%s %s %s %s: %s\n", d.Severity, d.Code, d.Source, d.Field, d.Message)
		}
	}
	if report.Status != "valid" {
		return 1, nil
	}
	return 0, nil
}
