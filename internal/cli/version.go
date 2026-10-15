package cli

import (
	"flag"
	"fmt"
	"io"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/kit"
)

func runVersion(args []string, stdout, _ io.Writer) (int, error) {
	fs := flag.NewFlagSet("version", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		return 2, nil
	}
	root, err := requireRoot()
	if err != nil {
		return 1, err
	}
	v, err := kit.Version(root)
	if err != nil {
		return 1, err
	}
	fmt.Fprintln(stdout, v)
	return 0, nil
}
