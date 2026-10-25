// Package command projects the offline SDK into standalone or umbrella CLI UX.
package command

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	api "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/v1alpha1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/sdk"
)

const help = `WorkKit P1 offline commands (P2 read commands are opt-in):
  workspace validate|inspect --workspace FILE [--root DIRECTORY] [--json]
  migrate inspect-aok|plan-aok --source PATH --format FORMAT --as-of RFC3339 [--json]
  migrate import-aok --source PATH --format FORMAT --as-of RFC3339 --out NEW-DIRECTORY [--json]
  migrate verify BUNDLE [--json]
  version
P2: providers, discover, capture, catalog, browse, items, views (use GROUP --help).
P3: tools, auth, connections (use GROUP --help).
Native auth and activation remain gated; tracker mutation is not exposed.
`

func Run(ctx context.Context, args []string, stdout, stderr io.Writer, service *sdk.Service) int {
	return run(ctx, args, stdout, stderr, service, false)
}

// RunSuite preserves the full domain result and exit code in the suite envelope.
func RunSuite(ctx context.Context, args []string, stdout, stderr io.Writer, service *sdk.Service) int {
	return run(ctx, args, stdout, stderr, service, true)
}
func run(ctx context.Context, args []string, stdout, stderr io.Writer, service *sdk.Service, suite bool) int {
	if ctx == nil || stdout == nil || stderr == nil || service == nil {
		return 2
	}
	if len(args) > 0 && (args[0] == "tools" || args[0] == "auth" || args[0] == "connections") {
		return runAccess(ctx, args, stdout, stderr, service, suite)
	}
	if len(args) > 0 && isReadGroup(args[0]) {
		return runRead(ctx, args, stdout, stderr, service, suite)
	}
	if len(args) == 0 {
		fmt.Fprint(stderr, help)
		return 2
	}
	if args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprint(stdout, help)
		return 0
	}
	if args[0] == "version" {
		if len(args) != 1 {
			return 2
		}
		fmt.Fprintln(stdout, "workkit "+sdk.Version)
		return 0
	}
	if len(args) < 2 {
		fmt.Fprint(stderr, help)
		return 2
	}
	group, operation := args[0], args[1]
	if operation == "--help" || operation == "-h" {
		fmt.Fprint(stdout, help)
		return 0
	}
	command := group + " " + operation
	supported := command == "workspace validate" || command == "workspace inspect" || command == "migrate inspect-aok" || command == "migrate plan-aok" || command == "migrate import-aok" || command == "migrate verify"
	if !supported {
		fmt.Fprintln(stderr, "unsupported WorkKit P1 command")
		return 2
	}
	fs := flag.NewFlagSet("workkit "+command, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	asJSON := fs.Bool("json", false, "one domain result envelope")
	workspace := fs.String("workspace", "", "workspace file")
	root := fs.String("root", "", "configuration root")
	source := fs.String("source", "", "legacy input")
	format := fs.String("format", "", "explicit original format")
	asof := fs.String("as-of", "", "RFC3339 inspection time")
	out := fs.String("out", "", "new immutable directory")
	rest := args[2:]
	bundle := ""
	if command == "migrate verify" && len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		bundle = rest[0]
		rest = rest[1:]
	}
	if e := fs.Parse(rest); e != nil {
		if errors.Is(e, flag.ErrHelp) {
			fmt.Fprint(stdout, help)
			return 0
		}
		fmt.Fprintln(stderr, "invalid WorkKit arguments")
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(stderr, "unexpected positional arguments")
		return 2
	}
	var data any
	var e error
	code := 0
	effect := "none"
	switch group {
	case "workspace":
		if *workspace == "" || *source != "" || *format != "" || *asof != "" || *out != "" {
			return misuse(stderr)
		}
		data, e = service.Resolve(ctx, sdk.WorkspaceRequest{Workspace: *workspace, Root: *root})
	case "migrate":
		if *workspace != "" || *root != "" {
			return misuse(stderr)
		}
		if operation == "verify" {
			if bundle == "" || *source != "" || *format != "" || *asof != "" || *out != "" {
				return misuse(stderr)
			}
			data, e = service.VerifyMigration(ctx, bundle)
		} else {
			if *source == "" || *format == "" || *asof == "" || operation == "import-aok" && *out == "" || operation != "import-aok" && *out != "" {
				return misuse(stderr)
			}
			request := sdk.SourceRequest{Source: *source, Format: *format, AsOf: *asof}
			if operation == "import-aok" {
				data, e = service.Import(ctx, request, *out)
				if e == nil {
					effect = "confirmed"
				} else {
					effect = "possible"
				}
			} else {
				data, e = service.InspectMigration(ctx, request)
			}
		}
	}
	status := "ok"
	diagnostics := []api.Diagnostic{}
	if e != nil {
		var failure *sdk.Error
		if errors.As(e, &failure) {
			code = failure.ExitCode
			diagnostics = append(diagnostics, api.Diagnostic{Code: failure.Code, Field: "", Message: failure.Message, Severity: "error"})
		} else {
			code = 1
			diagnostics = append(diagnostics, api.Diagnostic{Code: "operation.failed", Field: "", Message: "offline operation failed", Severity: "error"})
		}
		data = map[string]any{"authorizationGranted": false, "executionReady": false}
		status = "error"
		if code == 3 {
			status = "blocked"
		}
		if code == 4 {
			status = "conflict"
		}
	} else if migration, ok := data.(sdk.MigrationReport); ok && len(migration.Losses) > 0 {
		code = 6
		status = "partial"
	}
	result := sdk.Result(command, status, data, diagnostics, effect)
	if *asJSON {
		encoder := json.NewEncoder(stdout)
		encoder.SetEscapeHTML(false)
		var rendered any = result
		if suite {
			rendered = sdk.SuiteResult(result, code)
		}
		if encoder.Encode(rendered) != nil {
			return 1
		}
	} else {
		fmt.Fprintf(stdout, "%s: %s\n", command, status)
		for _, d := range diagnostics {
			fmt.Fprintln(stderr, d.Code+": "+d.Message)
		}
	}
	return code
}
func misuse(w io.Writer) int { fmt.Fprintln(w, "missing or inapplicable WorkKit arguments"); return 2 }
