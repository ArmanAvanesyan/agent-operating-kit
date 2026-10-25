package cli

import (
	"context"
	"fmt"
	legacy "github.com/ArmanAvanesyan/agent-operating-kit/integrations/legacy-aok/workkit"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/sdk"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/sdk/command"
	"io"
	"os"
	"os/signal"
)

func runWorkkit(args []string, stdout, stderr io.Writer) int {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	return command.RunSuite(ctx, args, stdout, stderr, sdk.New(&legacy.Reader{}).WithReadBackend(legacy.ReadBackend{}).WithAccessBackend(legacy.AccessBackend{Challenge: func(url string) error {
		_, e := fmt.Fprintln(stderr, "Open this one-use authorization URL in your browser:", url)
		return e
	}}))
}
