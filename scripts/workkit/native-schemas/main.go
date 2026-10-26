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
	for _, kind := range []string{"assignment", "handle", "evidence", "report", "policy", "enrollment", "progress-link", "operation-request"} {
		s, e := sdk.NativeContractSchema(kind)
		if e != nil {
			panic(e)
		}
		b, e := json.MarshalIndent(s, "", "  ")
		if e != nil {
			panic(e)
		}
		b = append(b, '\n')
		dir := "nativev1"
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
				fmt.Fprintln(os.Stderr, "native schema drift:", path)
				os.Exit(1)
			}
		} else if e = os.WriteFile(path, b, 0644); e != nil {
			panic(e)
		}
	}
	fmt.Println("WorkKit native schema generation/check passed")
}
