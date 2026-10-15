// Package cli implements the aok command router and shared UX helpers.
package cli

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/kit"
)

// Run parses argv and dispatches to a subcommand. Returns process exit code.
func Run(argv []string, stdout, stderr io.Writer) int {
	if stdout == nil {
		stdout = os.Stdout
	}
	if stderr == nil {
		stderr = os.Stderr
	}
	if len(argv) == 0 {
		printUsage(stderr)
		return 2
	}

	cmd := argv[0]
	args := argv[1:]

	var err error
	var code int
	switch cmd {
	case "help", "-h", "--help":
		printUsage(stdout)
		return 0
	case "version":
		code, err = runVersion(args, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown command: %s\n", cmd)
		printUsage(stderr)
		return 2
	}

	if err != nil {
		var aokErr *kit.Error
		if errors.As(err, &aokErr) {
			fmt.Fprintf(stderr, "Error: %s\n", aokErr.Msg)
			return 1
		}
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	return code
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, `Agent Operating Kit CLI

Usage:
  aok <command> [flags]

Commands:
  version              Print kit version from .codex-plugin/plugin.json

Environment:
  AOK_ROOT             Override kit root discovery

More commands land in later Go CLI slices (doctor, validate, install, …).`)
}

func requireRoot() (string, error) {
	return kit.FindRoot("")
}
