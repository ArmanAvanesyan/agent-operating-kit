package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/decision"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/planning"
)

func runDecision(args []string, stdout, stderr io.Writer) (int, error) {
	usage := func(w io.Writer) { fmt.Fprintln(w, "Usage: aok decision validate-result <contract> <result> [--json]") }
	if len(args) == 1 && (args[0] == "--help" || args[0] == "help") {
		usage(stdout)
		return 0, nil
	}
	if len(args) == 0 || args[0] != "validate-result" {
		usage(stderr)
		return 2, nil
	}
	positional := []string{}
	for _, arg := range args[1:] {
		if arg == "--help" || arg == "-h" {
			usage(stdout)
			return 0, nil
		}
		if arg == "--json" {
			continue
		}
		if strings.HasPrefix(arg, "-") {
			usage(stderr)
			return 2, nil
		}
		positional = append(positional, arg)
	}
	if len(positional) != 2 {
		usage(stderr)
		return 2, nil
	}
	report := decision.ValidateFiles(positional[0], positional[1])
	if err := writePlanningReport(stdout, report); err != nil {
		return 1, err
	}
	if report.Status != "valid" {
		return 1, nil
	}
	return 0, nil
}
func runPlanning(args []string, stdout, stderr io.Writer) (int, error) {
	usage := func(w io.Writer) {
		fmt.Fprintln(w, "Usage: aok planning validate <directory> [--json]\n       aok planning ready <plan> --evidence <file> --as-of <RFC3339> [--root <corpus>] [--json]")
	}
	if len(args) == 1 && (args[0] == "--help" || args[0] == "help") {
		usage(stdout)
		return 0, nil
	}
	if len(args) == 0 || (args[0] != "validate" && args[0] != "ready") {
		usage(stderr)
		return 2, nil
	}
	var input, evidence, root, asOf string
	seen := map[string]bool{}
	for i := 1; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--help", "-h":
			usage(stdout)
			return 0, nil
		case "--json":
		case "--evidence", "--root", "--as-of":
			if args[0] != "ready" || seen[arg] || i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
				usage(stderr)
				return 2, nil
			}
			seen[arg] = true
			i++
			switch arg {
			case "--evidence":
				evidence = args[i]
			case "--root":
				root = args[i]
			case "--as-of":
				asOf = args[i]
			}
		default:
			if input != "" || strings.HasPrefix(arg, "-") {
				usage(stderr)
				return 2, nil
			}
			input = arg
		}
	}
	if input == "" {
		usage(stderr)
		return 2, nil
	}
	if args[0] == "ready" && (evidence == "" || asOf == "") {
		usage(stderr)
		return 2, nil
	}
	if args[0] == "ready" {
		if _, err := time.Parse(time.RFC3339, asOf); err != nil {
			usage(stderr)
			return 2, nil
		}
	}
	report := planning.Report{}
	if args[0] == "validate" {
		report = planning.Validate(input)
	} else {
		report = planning.Ready(input, evidence, root, asOf)
	}
	if err := writePlanningReport(stdout, report); err != nil {
		return 1, err
	}
	if report.Status != "valid" {
		return 1, nil
	}
	return 0, nil
}
func writePlanningReport(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
