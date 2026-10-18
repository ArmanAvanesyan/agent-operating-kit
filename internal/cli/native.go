package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/evidence"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/nativeadapter"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/nativeadapters/claude"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/nativeadapters/codex"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/nativeadapters/cursor"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/nativeadapters/gemini"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/nativeadapters/opencode"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/nativebridge"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/nativeio"
)

// Production adapters register here in their own reviewed implementation PRs.
func nativeEngine() *nativeadapter.Engine {
	registry := nativeadapter.NewRegistry()
	_ = registry.Register(opencode.New())
	_ = registry.Register(claude.New())
	_ = registry.Register(codex.New())
	_ = registry.Register(cursor.New())
	_ = registry.Register(gemini.New())
	return nativeadapter.NewEngine(registry)
}
func runNative(args []string, stdout, stderr io.Writer) (int, error) {
	return runNativeWithEngine(args, stdout, stderr, nativeEngine())
}
func runNativeWithEngine(args []string, stdout, stderr io.Writer, engine *nativeadapter.Engine) (int, error) {
	usage := func(w io.Writer) {
		fmt.Fprintln(w, "Usage: aok agent native inspect|import|export|compare --target <target> --scope <scope> --input <path> [--out <new-directory>] [--source <native-input> | --root <canonical-corpus>] [--against <bundle>] [--sidecar <bundle>] [--require <canonical-field>] [--json]")
	}
	if len(args) == 1 && (args[0] == "--help" || args[0] == "help") {
		usage(stdout)
		return 0, nil
	}
	if len(args) == 0 {
		usage(stderr)
		return 2, nil
	}
	op := args[0]
	if op != "inspect" && op != "import" && op != "export" && op != "compare" {
		usage(stderr)
		return 2, nil
	}
	flags := map[string]string{}
	controls := []nativeadapter.Control{}
	for i := 1; i < len(args); i++ {
		a := args[i]
		if a == "--help" || a == "-h" {
			usage(stdout)
			return 0, nil
		}
		if a == "--json" {
			continue
		}
		switch a {
		case "--target", "--scope", "--input", "--out", "--source", "--against", "--sidecar", "--require", "--root":
		default:
			usage(stderr)
			return 2, nil
		}
		if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") || args[i+1] == "" {
			usage(stderr)
			return 2, nil
		}
		i++
		if a == "--require" {
			controls = append(controls, nativeadapter.Control{Path: args[i], Required: true})
			continue
		}
		if _, exists := flags[a]; exists {
			usage(stderr)
			return 2, nil
		}
		flags[a] = args[i]
	}
	target, scope, input := flags["--target"], flags["--scope"], flags["--input"]
	known := false
	for _, v := range []string{"codex", "claude-code", "cursor", "opencode", "gemini", "openclaw", "hermes-agent", "agy"} {
		if target == v {
			known = true
		}
	}
	if !known || scope == "" || input == "" || (op == "import" || op == "export") && flags["--out"] == "" || op == "export" && flags["--source"] == "" && flags["--root"] == "" || op == "compare" && flags["--against"] == "" {
		usage(stderr)
		return 2, nil
	}
	if (flags["--root"] != "" && (op != "export" || flags["--source"] != "")) || (flags["--out"] != "" && op != "import" && op != "export") || (flags["--source"] != "" && op != "export") || (flags["--against"] != "" && op != "compare") || (flags["--sidecar"] != "" && op != "import") {
		usage(stderr)
		return 2, nil
	}
	report := nativeadapter.Report{Operation: op, Target: target, Scope: scope, Status: "invalid", Diagnostics: []contracts.Diagnostic{}}
	fail := func(code, source, message string) {
		report.Status = "invalid"
		report.Diagnostics = append(report.Diagnostics, contracts.Diagnostic{Code: code, Source: source, Severity: "error", Message: message})
	}
	loadBundle := func(path string) (nativeadapter.Bundle, error) {
		data, err := nativeio.ReadFile(path)
		if err != nil {
			return nativeadapter.Bundle{}, err
		}
		return nativeadapter.DecodeBundle(data)
	}
	var bundle nativeadapter.Bundle
	var outputFiles []nativeadapter.File
	bundlePresent := false
	if op == "export" && flags["--root"] != "" {
		outputFiles, bundle, report = nativebridge.Export(engine, target, scope, input, flags["--root"], controls)
		bundlePresent = bundle.Version != ""
	} else if op == "export" {
		b, err := loadBundle(input)
		if err != nil {
			fail("native.input.bundle", input, "bundle is missing, malformed or exceeds limits")
		} else {
			current, err := nativeio.Load(flags["--source"])
			if err != nil {
				fail("native.input.files", flags["--source"], err.Error())
			} else {
				outputFiles, bundle, report = engine.Export(target, scope, current, b, controls)
				bundlePresent = bundle.Version != ""
			}
		}
	} else {
		files, err := nativeio.Load(input)
		if err != nil {
			fail("native.input.files", input, err.Error())
		} else {
			switch op {
			case "inspect":
				report = engine.Inspect(target, scope, files, controls)
			case "import":
				var prior *nativeadapter.Bundle
				if path := flags["--sidecar"]; path != "" {
					b, err := loadBundle(path)
					if err != nil {
						fail("native.input.bundle", path, "sidecar is missing, malformed or exceeds limits")
						break
					}
					prior = &b
				}
				bundle, report = engine.Import(target, scope, files, prior, controls)
				bundlePresent = bundle.Version != ""
			case "compare":
				b, err := loadBundle(flags["--against"])
				if err != nil {
					fail("native.input.bundle", flags["--against"], "comparison bundle is missing, malformed or exceeds limits")
				} else {
					report = engine.Compare(target, scope, files, b, controls)
				}
			}
		}
	}
	if out := flags["--out"]; out != "" {
		toWrite := map[string][]byte{}
		if bundlePresent {
			data, err := nativeadapter.EncodeBundle(bundle)
			if err != nil || len(data) > contracts.MaxDocumentBytes {
				fail("native.output.bundle-size", "", "serialized bundle exceeds the supported limit")
				outputFiles = nil
			} else {
				toWrite["aok-native.json"] = append(data, '\n')
			}
		}
		for _, f := range outputFiles {
			lower := strings.ToLower(f.Path)
			if lower == "report.json" || lower == "aok-native.json" || lower == nativeio.ManifestName {
				fail("native.output.collision", f.Path, "native output conflicts with a reserved report path")
				break
			}
			toWrite[f.Path] = f.Data
		}
		if len(outputFiles) > 0 {
			manifest := nativeio.Manifest{Version: "aok-native-input/v1", Files: []nativeio.Entry{}}
			for _, file := range outputFiles {
				manifest.Files = append(manifest.Files, nativeio.Entry{Path: file.Path, Format: file.Format})
			}
			b, err := json.MarshalIndent(manifest, "", "  ")
			if err != nil {
				return 1, err
			}
			toWrite[nativeio.ManifestName] = append(b, '\n')
		}
		// Invalid input/integrity results write diagnostics only.
		if report.Status == "invalid" {
			toWrite = map[string][]byte{}
		}
		b, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return 1, err
		}
		toWrite["report.json"] = append(b, '\n')
		if err := evidence.WriteFiles(out, toWrite, input, flags["--source"], flags["--against"], flags["--sidecar"], flags["--root"]); err != nil {
			fail("native.output.path", out, "output must be a new safe directory outside the explicit inputs")
		}
	}
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		return 1, err
	}
	if report.Status != "valid" {
		return 1, nil
	}
	return 0, nil
}
