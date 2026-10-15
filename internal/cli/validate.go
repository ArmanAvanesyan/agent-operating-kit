package cli

import (
	"flag"
	"fmt"
	"io"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/validate"
)

func runValidate(args []string, stdout, _ io.Writer) (int, error) {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		return 2, nil
	}
	root, err := requireRoot()
	if err != nil {
		return 1, err
	}
	missing := validate.Kit(root)
	if len(missing) > 0 {
		fmt.Fprintln(stdout, "Kit validation failed:")
		for _, item := range missing {
			fmt.Fprintf(stdout, "- %s\n", item)
		}
		return 1, nil
	}
	fmt.Fprintf(stdout, "Kit validation passed: %s\n", root)
	return 0, nil
}
