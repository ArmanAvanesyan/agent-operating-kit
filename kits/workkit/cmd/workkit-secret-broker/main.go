package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	a "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/accessv1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/credentialbroker"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/sdk"
	"io"
	"os"
	"time"
)

func main() { os.Exit(run()) }
func run() int {
	if len(os.Args) == 2 && os.Args[1] == "version" {
		fmt.Print("workkit-secret-broker/v1")
		return 0
	}
	if len(os.Args) < 2 || os.Args[1] != "exchange" {
		return 2
	}
	f := flag.NewFlagSet("broker", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	path := f.String("config", "", "")
	pin := f.String("config-sha256", "", "")
	if f.Parse(os.Args[2:]) != nil || f.NArg() != 0 || *path == "" || len(*pin) != 64 {
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	b, e := sdk.ReadLocal(ctx, *path)
	if e != nil || sdk.SHA256(b) != *pin {
		return 3
	}
	var c a.BrokerConfig
	if sdk.DecodeRead(b, &c) != nil {
		return 2
	}
	raw, e := io.ReadAll(io.LimitReader(os.Stdin, 32769))
	if e != nil || len(raw) > 32768 {
		return 2
	}
	var request a.BrokerRequest
	if sdk.DecodeRead(raw, &request) != nil {
		return 2
	}
	store, e := credentialbroker.Open(c)
	if e != nil {
		return 3
	}
	response, e := credentialbroker.Exchange(ctx, c, store, request)
	if e != nil {
		return 5
	}
	if json.NewEncoder(os.Stdout).Encode(response) != nil {
		return 5
	}
	return 0
}
