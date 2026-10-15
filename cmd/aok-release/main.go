// Command aok-release builds GitHub Release artifacts (kit zips, cross-compiled
// aok binaries, installers, bundles, SHA256SUMS, index.json).
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/releasebuild"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	fs := flag.NewFlagSet("aok-release", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	distFlag := fs.String("dist", "", "release output directory (default: <root>/dist/release)")
	versionFlag := fs.String("version", "", "override manifest version")
	rootFlag := fs.String("root", "", "kit root (default: walk up from cwd)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	root := *rootFlag
	if root == "" {
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return 1
		}
		root = cwd
	}
	root, err := filepath.Abs(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}

	dist := *distFlag
	if dist == "" {
		dist = filepath.Join(root, "dist", "release")
	}
	dist, err = filepath.Abs(dist)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}

	version := *versionFlag
	if version == "" {
		version, err = releasebuild.LoadVersion(root)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return 1
		}
	}

	artifacts, err := releasebuild.Build(root, dist, version)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	for _, a := range artifacts {
		fmt.Printf("%s  %s\n", a.SHA256, a.Name)
	}
	return 0
}
