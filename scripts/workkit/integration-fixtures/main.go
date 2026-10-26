// Developer-only deterministic two-scope Azure fixtures, never a live route.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func main() {
	check := flag.Bool("check", false, "compare fixture bytes without writing")
	flag.Parse()
	temp, e := os.MkdirTemp("", "workkit-p7-fixtures-")
	must(e)
	defer os.RemoveAll(temp)
	temp, e = filepath.EvalSymlinks(temp)
	must(e)
	out := filepath.Join(temp, "generated")
	cmd := exec.Command("go", "test", "./kits/workkit/sdk", "-run", "^TestP7ExportFixtures$", "-count=1")
	for _, v := range os.Environ() {
		if !strings.HasPrefix(v, "AOK_TEST_P7_") {
			cmd.Env = append(cmd.Env, v)
		}
	}
	cmd.Env = append(cmd.Env, "AOK_TEST_P7_EXPORT="+out)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	must(cmd.Run())
	base := "kits/workkit/testdata/integration"
	seen := map[string]bool{}
	must(filepath.WalkDir(out, func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			return nil
		}
		rel, e := filepath.Rel(out, path)
		if e != nil {
			return e
		}
		seen[rel] = true
		want, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		target := filepath.Join(base, rel)
		if *check {
			actual, e := os.ReadFile(target)
			if e != nil || !bytes.Equal(actual, want) {
				return fmt.Errorf("fixture drift: %s", rel)
			}
		} else {
			if e = os.MkdirAll(filepath.Dir(target), 0755); e != nil {
				return e
			}
			return os.WriteFile(target, want, 0644)
		}
		return nil
	}))
	if *check {
		must(filepath.WalkDir(base, func(path string, d os.DirEntry, e error) error {
			if e != nil {
				return e
			}
			rel, _ := filepath.Rel(base, path)
			if !d.IsDir() && !seen[rel] {
				return fmt.Errorf("extra fixture: %s", rel)
			}
			return nil
		}))
	}
	fmt.Println("WorkKit P7 fixture generation/check passed; fixture conformance only")
}
func must(e error) {
	if e != nil {
		panic(e)
	}
}
