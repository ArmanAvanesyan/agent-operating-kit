// Developer-only schema materializer; -check never writes repository files.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/sdk"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	check := flag.Bool("check", false, "check generated schema bytes")
	flag.Parse()
	for _, kind := range []string{"configuration", "policy", "enrollment", "event-envelope", "operational-read", "checkpoint", "command", "report", "worker-recovery", "operational-capture", "sync-configuration", "sync-state", "sync-plan", "sync-resolution", "sync-resolution-preview", "sync-invocation"} {
		s, e := sdk.IntegrationContractSchema(kind)
		if e != nil {
			panic(e)
		}
		b, e := json.MarshalIndent(s, "", "  ")
		if e != nil {
			panic(e)
		}
		b = append(b, '\n')
		dir := "integrationv1"
		if strings.HasPrefix(kind, "sync-") {
			dir = "syncv2"
		}
		base := filepath.Join("kits/workkit/api", dir, "schemas")
		if !*check {
			if e = os.MkdirAll(base, 0755); e != nil {
				panic(e)
			}
		}
		path := filepath.Join(base, kind+".schema.json")
		if *check {
			actual, e := os.ReadFile(path)
			if e != nil || !bytes.Equal(actual, b) {
				fmt.Fprintln(os.Stderr, "integration schema drift:", path)
				os.Exit(1)
			}
		} else if e = os.WriteFile(path, b, 0644); e != nil {
			panic(e)
		}
	}
	fmt.Println("WorkKit integration schema generation/check passed")
}
