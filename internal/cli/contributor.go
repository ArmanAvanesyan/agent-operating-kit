package cli

import (
	"io"
	"strings"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/hooks"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/importhistory"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/kit"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/task"
)

func runTask(args []string, stdout, stderr io.Writer) (int, error) {
	if len(args) == 0 {
		return 2, kit.NewError("usage: aok task sync|packet <ref> [-o PATH]")
	}
	switch args[0] {
	case "sync":
		return runTaskSync(args[1:], stdout, stderr)
	case "packet":
		return runTaskPacket(args[1:], stdout, stderr)
	default:
		return 2, kit.NewErrorf("unknown task command: %s", args[0])
	}
}

func runTaskSync(args []string, stdout, _ io.Writer) (int, error) {
	_, positionals, err := parseOutputFlags(args)
	if err != nil {
		return 2, err
	}
	if len(positionals) < 1 {
		return 2, kit.NewError("usage: aok task sync <ref>")
	}
	return task.Sync(positionals[0], stdout)
}

func runTaskPacket(args []string, stdout, _ io.Writer) (int, error) {
	output, positionals, err := parseOutputFlags(args)
	if err != nil {
		return 2, err
	}
	if len(positionals) < 1 {
		return 2, kit.NewError("usage: aok task packet <ref> [-o PATH]")
	}
	return task.Packet(positionals[0], output, stdout)
}

func runHooks(args []string, stdout, stderr io.Writer) (int, error) {
	if len(args) == 0 {
		return 2, kit.NewError("usage: aok hooks pre|after <packet>")
	}
	switch args[0] {
	case "pre":
		if len(args) < 2 {
			return 2, kit.NewError("usage: aok hooks pre <packet>")
		}
		return hooks.Pre(args[1], stdout)
	case "after":
		if len(args) < 2 {
			return 2, kit.NewError("usage: aok hooks after <packet>")
		}
		return hooks.After(args[1], stdout, stderr)
	default:
		return 2, kit.NewErrorf("unknown hooks command: %s", args[0])
	}
}

func runImport(args []string, stdout, stderr io.Writer) (int, error) {
	if len(args) == 0 {
		return 2, kit.NewError("usage: aok import history [path] [-o PATH]")
	}
	switch args[0] {
	case "history":
		return runImportHistory(args[1:], stdout, stderr)
	default:
		return 2, kit.NewErrorf("unknown import command: %s", args[0])
	}
}

func runImportHistory(args []string, stdout, _ io.Writer) (int, error) {
	output, positionals, err := parseOutputFlags(args)
	if err != nil {
		return 2, err
	}
	path := "."
	if len(positionals) > 0 {
		path = positionals[0]
	}
	return importhistory.History(path, output, stdout)
}

// parseOutputFlags accepts -o/--output before or after positionals.
func parseOutputFlags(args []string) (output string, positionals []string, err error) {
	i := 0
	for i < len(args) {
		arg := args[i]
		if arg == "--" {
			positionals = append(positionals, args[i+1:]...)
			break
		}
		if arg == "-o" || arg == "--output" {
			i++
			if i >= len(args) {
				return "", nil, kit.NewError("flag -o/--output requires a value")
			}
			output = args[i]
			i++
			continue
		}
		if strings.HasPrefix(arg, "--output=") {
			output = strings.TrimPrefix(arg, "--output=")
			i++
			continue
		}
		if strings.HasPrefix(arg, "-o=") {
			output = strings.TrimPrefix(arg, "-o=")
			i++
			continue
		}
		if strings.HasPrefix(arg, "-") {
			return "", nil, kit.NewErrorf("unknown flag: %s", arg)
		}
		positionals = append(positionals, arg)
		i++
	}
	return output, positionals, nil
}
