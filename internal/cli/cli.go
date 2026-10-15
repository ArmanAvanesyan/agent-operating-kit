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
	case "doctor":
		code, err = runDoctor(args, stdout, stderr)
	case "validate":
		code, err = runValidate(args, stdout, stderr)
	case "install":
		code, err = runInstall(args, stdout, stderr)
	case "claude":
		code, err = runClaude(args, stdout, stderr)
	case "codex":
		code, err = runCodex(args, stdout, stderr)
	case "project":
		code, err = runProject(args, stdout, stderr)
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
  doctor               Check install and required kit files
  validate             Validate kit content
  install              Register marketplace plugin and install PATH binary
  claude setup         Configure Claude Code marketplace (aok-claude-code)
  codex setup          Install + doctor (+ optional project / claude)
  codex local-env      init|validate|actions
  project init|validate

Environment:
  AOK_ROOT             Override kit root discovery`)
}

func requireRoot() (string, error) {
	return kit.FindRoot("")
}
