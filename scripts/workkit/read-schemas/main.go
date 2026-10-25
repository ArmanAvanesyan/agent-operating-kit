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
	for _, kind := range []string{"configuration", "manifest", "query", "enrollment", "access-policy", "item-batch", "catalog-batch"} {
		s, e := sdk.ReadContractSchema(kind)
		if e != nil {
			panic(e)
		}
		b, e := json.MarshalIndent(s, "", "  ")
		if e != nil {
			panic(e)
		}
		b = append(b, '\n')
		path := filepath.Join("kits/workkit/api/readv1/schemas", kind+".schema.json")
		if *check {
			actual, e := os.ReadFile(path)
			if e != nil || !bytes.Equal(actual, b) {
				fmt.Fprintln(os.Stderr, "read schema drift:", path)
				os.Exit(1)
			}
		} else if e = os.WriteFile(path, b, 0644); e != nil {
			panic(e)
		}
	}
	fmt.Println("WorkKit read schema generation/check passed")
}
