package cli

import (
	"flag"
	"fmt"
	"io"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/install"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/kit"
)

func runInstall(args []string, stdout, stderr io.Writer) (int, error) {
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	homeFlag := fs.String("home", "", "override home directory")
	replace := fs.Bool("replace", false, "replace existing plugin path / PATH binary")
	if err := fs.Parse(args); err != nil {
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

	result, err := install.Run(install.Options{
		Root:    root,
		Home:    home,
		Replace: *replace,
	}, stderr)
	if err != nil {
		return 1, err
	}

	fmt.Fprintf(stdout, "Registered plugin in %s\n", result.Marketplace)
	fmt.Fprintf(stdout, "Plugin path (%s): %s -> %s\n", result.PluginMode, result.PluginPath, root)
	fmt.Fprintf(stdout, "CLI path (%s): %s\n", result.CLIMode, result.CLIPath)
	return 0, nil
}
