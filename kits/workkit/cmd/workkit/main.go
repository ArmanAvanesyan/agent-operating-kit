package main

import (
	"context"
	"fmt"
	legacy "github.com/ArmanAvanesyan/agent-operating-kit/integrations/legacy-aok/workkit"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/sdk"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/sdk/command"
	"os"
	"os/signal"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	os.Exit(command.Run(ctx, os.Args[1:], os.Stdout, os.Stderr, sdk.New(&legacy.Reader{}).WithReadBackend(legacy.ReadBackend{}).WithAccessBackend(legacy.AccessBackend{Challenge: func(url string) error {
		_, e := fmt.Fprintln(os.Stderr, "Open this one-use authorization URL in your browser:", url)
		return e
	}})))
}
