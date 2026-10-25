package command

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	r "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/readv1"
	api "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/v1alpha1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/sdk"
	"io"
	"strings"
	"time"
)

const readHelp = `WorkKit P2 read commands (opt-in; no writes or activation):
  providers list|inspect NAME|capabilities --capture BUNDLE --as-of RFC3339
  capture import --connection-file FILE --manifest FILE --as-of RFC3339 --out NEW
  capture run|resume --connection-file FILE --policy FILE --enrollment FILE --out NEW [--capture PRIOR]
  capture validate|inspect --capture BUNDLE [--as-of RFC3339]
  capture diff --old BUNDLE --new BUNDLE --as-of RFC3339
  discover --connection-file FILE --policy FILE --enrollment FILE --out NEW
  items list|show --capture BUNDLE --as-of RFC3339 [--id NATIVE-ID]
  items capture --connection-file FILE --policy FILE --enrollment FILE --out NEW
  catalog containers|teams|types|fields|states|transitions|iterations --capture BUNDLE --as-of RFC3339
  browse --captures BUNDLE[,BUNDLE] --query FILE --as-of RFC3339
  views list --workspace FILE | views inspect --view FILE
  views evaluate --captures BUNDLE[,BUNDLE] --query FILE --view FILE --as-of RFC3339
All commands accept --json. Live reads require externally signed qualification-only enrollment.
`

func isReadGroup(s string) bool {
	switch s {
	case "providers", "capture", "discover", "browse", "items", "catalog", "views":
		return true
	}
	return false
}
func runRead(ctx context.Context, args []string, out, errout io.Writer, service *sdk.Service, suite bool) int {
	if len(args) == 0 {
		return 2
	}
	group := args[0]
	op := ""
	rest := args[1:]
	if group != "discover" && group != "browse" {
		if len(rest) == 0 {
			fmt.Fprint(errout, readHelp)
			return 2
		}
		op = rest[0]
		rest = rest[1:]
	}
	if op == "help" || op == "--help" || len(rest) > 0 && (rest[0] == "--help" || rest[0] == "-h") {
		fmt.Fprint(out, readHelp)
		return 0
	}
	provider := ""
	if group == "providers" && op == "inspect" && len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		provider = rest[0]
		rest = rest[1:]
	}
	flags := flag.NewFlagSet("read", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	asJSON := flags.Bool("json", false, "")
	capture := flags.String("capture", "", "")
	captures := flags.String("captures", "", "")
	asof := flags.String("as-of", "", "")
	config := flags.String("connection-file", "", "")
	manifest := flags.String("manifest", "", "")
	policy := flags.String("policy", "", "")
	enrollment := flags.String("enrollment", "", "")
	destination := flags.String("out", "", "")
	queryPath := flags.String("query", "", "")
	view := flags.String("view", "", "")
	workspace := flags.String("workspace", "", "")
	old := flags.String("old", "", "")
	newer := flags.String("new", "", "")
	id := flags.String("id", "", "")
	if e := flags.Parse(rest); e != nil || flags.NArg() != 0 {
		fmt.Fprintln(errout, "invalid read arguments")
		return 2
	}
	used := map[string]bool{}
	flags.Visit(func(f *flag.Flag) { used[f.Name] = true })
	used["json"] = false
	command := strings.TrimSpace(group + " " + op)
	allowed := map[string]bool{}
	allow := func(names ...string) {
		for _, name := range names {
			allowed[name] = true
		}
	}
	var data any
	var err error
	code := 0
	status := "ok"
	effect := "none"
	needTime := false
	switch {
	case group == "providers" && op == "list":
		data = sdk.ReadProviders()
	case group == "providers" && op == "inspect":
		if provider == "" {
			return misuse(errout)
		}
		data, err = sdk.InspectReadProvider(provider)
	case group == "providers" && op == "capabilities":
		allow("capture", "as-of")
		needTime = true
	case group == "capture" && op == "import":
		allow("connection-file", "manifest", "as-of", "out")
		needTime = true
	case group == "capture" && (op == "run" || op == "resume") || group == "discover" || group == "items" && op == "capture":
		allow("connection-file", "policy", "enrollment", "out")
		if op == "resume" {
			allow("capture")
		}
	case group == "capture" && (op == "validate" || op == "inspect"):
		allow("capture")
		if op == "inspect" {
			allow("as-of")
			needTime = true
		}
	case group == "capture" && op == "diff":
		allow("old", "new", "as-of")
		needTime = true
	case group == "browse":
		allow("captures", "query", "as-of")
		needTime = true
	case group == "items" && (op == "list" || op == "show"):
		allow("capture", "as-of")
		if op == "show" {
			allow("id")
		}
		needTime = true
	case group == "catalog":
		allow("capture", "as-of")
		needTime = true
	case group == "views" && op == "list":
		allow("workspace")
	case group == "views" && op == "inspect":
		allow("view")
	case group == "views" && op == "evaluate":
		allow("captures", "query", "view", "as-of")
		needTime = true
	default:
		fmt.Fprintln(errout, "unsupported WorkKit read command")
		return 2
	}
	for flag, yes := range used {
		if yes && !allowed[flag] {
			return misuse(errout)
		}
	}
	if needTime {
		if _, e := time.Parse(time.RFC3339Nano, *asof); e != nil {
			return misuse(errout)
		}
	}
	for name := range allowed {
		required := name != "id"
		if required && !used[name] {
			return misuse(errout)
		}
	}
	if err == nil && data == nil {
		switch {
		case group == "providers":
			data, err = sdk.ReadCapabilities(ctx, *capture, *asof)
		case group == "capture" && op == "import":
			var m r.Manifest
			m, err = sdk.ImportRead(ctx, *config, *manifest, *destination)
			data = map[string]any{"manifest": m, "inspectionAsOf": *asof, "originalObservationRetained": true}
			effect = "possible"
			if err == nil {
				effect = "confirmed"
				if !m.Complete {
					code = 6
					status = "partial"
				}
			}
		case group == "capture" && (op == "run" || op == "resume") || group == "discover" || group == "items" && op == "capture":
			if group == "items" {
				c, _, e := sdk.LoadReadConfiguration(ctx, *config)
				if e != nil || len(c.Source.Scope.ItemIDs) == 0 {
					return misuse(errout)
				}
			}
			mode := "capture"
			if group == "discover" {
				mode = "discover"
			}
			var m r.Manifest
			m, err = service.CaptureRead(ctx, *config, *policy, *enrollment, mode, *capture, *destination)
			data = m
			effect = "possible"
			if err == nil {
				effect = "confirmed"
				if !m.Complete {
					code = 6
					status = "partial"
				}
			}
		case group == "capture" && op == "validate":
			var b *sdk.ReadBundle
			b, err = sdk.LoadReadBundle(ctx, *capture)
			if err == nil {
				data = b.Manifest()
				if !b.Manifest().Complete {
					code = 6
					status = "partial"
				}
			}
		case group == "capture" && op == "inspect":
			data, err = sdk.InspectRead(ctx, *capture, *asof)
		case group == "capture" && op == "diff":
			if _, err = sdk.InspectRead(ctx, *old, *asof); err == nil {
				if _, err = sdk.InspectRead(ctx, *newer, *asof); err == nil {
					data, err = sdk.DiffRead(ctx, *old, *newer)
				}
			}
		case group == "browse" || group == "views" && op == "evaluate":
			var q r.Query
			q, err = sdk.LoadReadQuery(ctx, *queryPath)
			if err == nil {
				roots := strings.Split(*captures, ",")
				if group == "browse" {
					var result r.QueryResult
					result, err = sdk.Federate(ctx, roots, q, *asof)
					data = result
					if err == nil && !result.Exhaustive {
						code = 6
						status = "partial"
					}
				} else {
					var raw []byte
					raw, err = sdk.ReadLocal(ctx, *view)
					if err == nil {
						data, err = sdk.EvaluateReadView(ctx, roots, q, raw, *asof)
						if err == nil {
							if report, ok := data.(map[string]any); ok {
								if result, ok := report["result"].(r.QueryResult); ok && (!result.Exhaustive || report["complete"] == false) {
									code = 6
									status = "partial"
								}
							}
						}
					}
				}
			}
		case group == "items":
			var b *sdk.ReadBundle
			b, err = sdk.LoadReadBundle(ctx, *capture)
			if err == nil {
				_, err = sdk.InspectRead(ctx, *capture, *asof)
			}
			if err == nil {
				rows := b.Records()
				if op == "show" {
					if *id == "" {
						return misuse(errout)
					}
					filtered := []r.Record{}
					for _, row := range rows {
						if row.Identity.NativeID == *id {
							filtered = append(filtered, row)
						}
					}
					rows = filtered
					if len(rows) != 1 {
						err = &sdk.Error{Code: "read.identity", Message: "native item not found or ambiguous in capture", ExitCode: 3}
					}
				}
				data = map[string]any{"records": rows, "complete": b.Manifest().Complete, "authorizationGranted": false, "executionReady": false}
				if !b.Manifest().Complete {
					code = 6
					status = "partial"
				}
			}
		case group == "catalog":
			var report map[string]any
			report, err = sdk.ReadCatalog(ctx, *capture, op, *asof)
			data = report
			if err == nil && report["status"] != "unknown" && report["complete"] == false {
				code = 6
				status = "partial"
			}
			if err == nil && report["status"] == "unknown" {
				code = 3
				status = "unknown"
			}
		case group == "views" && op == "list":
			data, err = sdk.LocalReadViews(ctx, *workspace)
		case group == "views" && op == "inspect":
			var raw []byte
			raw, err = sdk.ReadLocal(ctx, *view)
			if err == nil {
				var d sdk.Document
				d, err = sdk.Parse(raw, "json")
				if err == nil && d.Kind() != "ViewDefinition" {
					err = errors.New("wrong resource kind")
				}
				if err == nil {
					data = json.RawMessage(d.CanonicalBytes())
				}
			}
		}
	}
	diagnostics := []struct{ Code, Message string }{}
	var resultDiagnostics = []map[string]string{}
	if err != nil {
		code = 2
		status = "error"
		message := "read inputs or evidence failed validation"
		key := "read.invalid"
		var e *sdk.Error
		if errors.As(err, &e) {
			code = e.ExitCode
			message = e.Message
			key = e.Code
		}
		if code == 3 {
			status = "blocked"
		}
		if code == 4 {
			status = "conflict"
		}
		data = map[string]any{"authorizationGranted": false, "executionReady": false, "migrationActivated": false, "liveQualification": "not-established", "productionActivated": false}
		resultDiagnostics = append(resultDiagnostics, map[string]string{"code": key, "field": "", "severity": "error", "message": message})
	}
	raw, _ := json.Marshal(resultDiagnostics)
	var ds []api.Diagnostic
	_ = json.Unmarshal(raw, &ds)
	result := sdk.Result(command, status, data, ds, effect)
	if *asJSON {
		var v any = result
		if suite {
			v = sdk.SuiteResult(result, code)
		}
		if json.NewEncoder(out).Encode(v) != nil {
			return 1
		}
	} else {
		fmt.Fprintf(out, "%s: %s\n", command, status)
		if err != nil {
			for _, d := range ds {
				fmt.Fprintln(errout, d.Code+": "+d.Message)
			}
		}
	}
	_ = diagnostics
	return code
}
