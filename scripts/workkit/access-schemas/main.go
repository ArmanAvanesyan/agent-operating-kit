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
)

func main() {
	check := flag.Bool("check", false, "check generated schema bytes")
	flag.Parse()
	for _, kind := range []string{"configuration", "policy", "grant", "receipt", "install-plan", "artifact", "tool-pin", "tool-observation", "binding", "broker-config", "registry", "discovery", "native-enrollment", "access-state"} {
		s, e := sdk.AccessContractSchema(kind)
		if e != nil {
			panic(e)
		}
		b, e := json.MarshalIndent(s, "", "  ")
		if e != nil {
			panic(e)
		}
		b = append(b, '\n')
		path := filepath.Join("kits/workkit/api/accessv1/schemas", kind+".schema.json")
		if *check {
			actual, e := os.ReadFile(path)
			if e != nil || !bytes.Equal(actual, b) {
				fmt.Fprintln(os.Stderr, "access schema drift:", path)
				os.Exit(1)
			}
		} else if e = os.WriteFile(path, b, 0644); e != nil {
			panic(e)
		}
	}
	fmt.Println("WorkKit access schema generation/check passed")
}
