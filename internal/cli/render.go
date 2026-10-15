package cli

import (
	"io"
	"strings"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/kit"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/render"
)

func runRender(args []string, stdout, stderr io.Writer) (int, error) {
	if len(args) == 0 {
		return 2, kit.NewError("usage: aok render pack|all|bundle|bundles …")
	}
	switch args[0] {
	case "pack":
		return runRenderPack(args[1:], stdout, stderr)
	case "all":
		return runRenderAll(args[1:], stdout, stderr)
	case "bundle":
		return runRenderBundle(args[1:], stdout, stderr)
	case "bundles":
		return runRenderBundles(args[1:], stdout, stderr)
	default:
		return 2, kit.NewErrorf("unknown render command: %s", args[0])
	}
}

// parseKVFlags accepts flags before or after positionals (Python argparse style).
func parseKVFlags(args []string, keys ...string) (flags map[string]string, positionals []string, err error) {
	flags = map[string]string{}
	allowed := map[string]bool{}
	for _, k := range keys {
		allowed[k] = true
	}
	i := 0
	for i < len(args) {
		arg := args[i]
		if arg == "--" {
			positionals = append(positionals, args[i+1:]...)
			break
		}
		if strings.HasPrefix(arg, "--") {
			key := strings.TrimPrefix(arg, "--")
			val := ""
			if strings.Contains(key, "=") {
				parts := strings.SplitN(key, "=", 2)
				key, val = parts[0], parts[1]
			} else {
				i++
				if i >= len(args) {
					return nil, nil, kit.NewErrorf("flag --%s requires a value", key)
				}
				val = args[i]
			}
			if !allowed[key] {
				return nil, nil, kit.NewErrorf("unknown flag: --%s", key)
			}
			flags[key] = val
			i++
			continue
		}
		positionals = append(positionals, arg)
		i++
	}
	return flags, positionals, nil
}

func runRenderPack(args []string, stdout, _ io.Writer) (int, error) {
	flags, positionals, err := parseKVFlags(args, "target", "out")
	if err != nil {
		return 2, err
	}
	if len(positionals) < 1 || flags["target"] == "" {
		return 2, kit.NewError("usage: aok render pack <pack> --target <target|all> [--out PATH]")
	}
	root, err := requireRoot()
	if err != nil {
		return 1, err
	}
	if err := render.Pack(root, positionals[0], flags["target"], flags["out"], stdout); err != nil {
		return 1, err
	}
	return 0, nil
}

func runRenderAll(args []string, stdout, _ io.Writer) (int, error) {
	flags, _, err := parseKVFlags(args, "target", "out")
	if err != nil {
		return 2, err
	}
	if flags["target"] == "" {
		return 2, kit.NewError("usage: aok render all --target <target|all> [--out PATH]")
	}
	root, err := requireRoot()
	if err != nil {
		return 1, err
	}
	if err := render.All(root, flags["target"], flags["out"], stdout); err != nil {
		return 1, err
	}
	return 0, nil
}

func runRenderBundle(args []string, stdout, _ io.Writer) (int, error) {
	flags, positionals, err := parseKVFlags(args, "target", "out")
	if err != nil {
		return 2, err
	}
	if len(positionals) < 1 || flags["target"] == "" {
		return 2, kit.NewError("usage: aok render bundle <pack> --target <target|all> [--out PATH]")
	}
	root, err := requireRoot()
	if err != nil {
		return 1, err
	}
	if err := render.Bundle(root, positionals[0], flags["target"], flags["out"], stdout); err != nil {
		return 1, err
	}
	return 0, nil
}

func runRenderBundles(args []string, stdout, _ io.Writer) (int, error) {
	flags, _, err := parseKVFlags(args, "target", "out")
	if err != nil {
		return 2, err
	}
	if flags["target"] == "" {
		return 2, kit.NewError("usage: aok render bundles --target <target|all> [--out PATH]")
	}
	root, err := requireRoot()
	if err != nil {
		return 1, err
	}
	if err := render.Bundles(root, flags["target"], flags["out"], stdout); err != nil {
		return 1, err
	}
	return 0, nil
}
