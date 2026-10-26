package command

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	i "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/integrationv1"
	z "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/syncv2"
	api "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/v1alpha1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/sdk"
	"io"
	"time"
)

const integrationHelp = `WorkKit P7 (opt-in; review-first, no native mutations or activation):
 sync validate|preview --v2 --configuration FILE --state FILE --source BUNDLE --destination BUNDLE --source-observation BUNDLE --observation BUNDLE --forward-mapping FILE --reverse-mapping FILE --forward-context FILE --reverse-context FILE --source-binding FILE --binding FILE --source-query FILE --destination-query FILE --as-of RFC3339 [--out NEW]
 sync resolve --plan V2-BUNDLE --resolution FILE --as-of RFC3339 --out NEW
 sync ingest --event-envelope FILE --policy FILE --enrollment FILE --out NEW
 sync run|pause|resume|reconcile --v2 --plan BUNDLE --policy FILE --enrollment FILE --out NEW [--invocation FILE] [--once] [--initialize]
 sync status|conflicts|run|pause|resume|reconcile --v2 --command FILE --listen SOCKET
 serve --workspace FILE --configuration FILE --listen SOCKET --policy FILE --enrollment FILE
All accept --json, --timeout, --non-interactive. Offline evaluation requires --as-of.
Standard binaries install no qualifying operational bootstrap. Windows serve is unsupported.
Native mutations blocked; P5 live pending; migration blocked; production unauthorized.
`

func isIntegrationCommand(args []string) bool {
	if len(args) == 0 {
		return false
	}
	if args[0] == "serve" {
		return true
	}
	if args[0] != "sync" || len(args) < 2 {
		return false
	}
	if args[1] == "resolve" || args[1] == "ingest" {
		return true
	}
	for _, a := range args[2:] {
		if a == "--v2" {
			return true
		}
	}
	return false
}
func runIntegration(ctx context.Context, args []string, stdout, stderr io.Writer, svc *sdk.Service, suite bool) int {
	group := args[0]
	op := "serve"
	rest := args[1:]
	if group == "sync" {
		if len(args) < 2 {
			return misuse(stderr)
		}
		op = args[1]
		rest = args[2:]
	}
	if op == "--help" || op == "help" || len(rest) == 1 && rest[0] == "--help" {
		fmt.Fprint(stdout, integrationHelp)
		return 0
	}
	f := flag.NewFlagSet("integration", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	v := map[string]*string{}
	for _, name := range []string{"configuration", "state", "source", "destination", "source-observation", "observation", "forward-mapping", "reverse-mapping", "forward-context", "reverse-context", "source-binding", "binding", "source-query", "destination-query", "as-of", "out", "plan", "resolution", "event-envelope", "policy", "enrollment", "invocation", "command", "listen", "workspace"} {
		v[name] = f.String(name, "", name)
	}
	jsonOut := f.Bool("json", false, "")
	f.Bool("v2", false, "")
	once := f.Bool("once", false, "")
	initialize := f.Bool("initialize", false, "")
	f.Bool("non-interactive", false, "")
	maximum := 5 * time.Minute
	if group == "serve" {
		maximum = 15 * time.Minute
	}
	timeout := f.Duration("timeout", maximum, "")
	if e := f.Parse(rest); e != nil {
		if errors.Is(e, flag.ErrHelp) {
			fmt.Fprint(stdout, integrationHelp)
			return 0
		}
		return misuse(stderr)
	}
	if f.NArg() != 0 || *timeout <= 0 || *timeout > maximum {
		return misuse(stderr)
	}
	common := []string{"configuration", "state", "source", "destination", "source-observation", "observation", "forward-mapping", "reverse-mapping", "forward-context", "reverse-context", "source-binding", "binding", "source-query", "destination-query", "as-of"}
	rules := map[string][]string{"validate": common, "preview": append(append([]string{}, common...), "out"), "resolve": {"plan", "resolution", "as-of", "out"}, "ingest": {"event-envelope", "policy", "enrollment", "out"}, "run": {"plan", "policy", "enrollment", "out", "invocation", "once", "initialize"}, "pause": {"plan", "policy", "enrollment", "out"}, "resume": {"plan", "policy", "enrollment", "out"}, "reconcile": {"plan", "policy", "enrollment", "out", "invocation"}, "status": {"command", "listen"}, "conflicts": {"command", "listen"}, "serve": {"workspace", "configuration", "listen", "policy", "enrollment"}}
	get := func(key string) string { return *v[key] }
	if get("command") != "" {
		if op != "status" && op != "conflicts" && op != "run" && op != "pause" && op != "resume" && op != "reconcile" {
			return misuse(stderr)
		}
		rules[op] = []string{"command", "listen"}
	}
	allowed, ok := rules[op]
	if !ok {
		return misuse(stderr)
	}
	used := map[string]bool{}
	invalid := false
	f.Visit(func(a *flag.Flag) {
		used[a.Name] = true
		if a.Name == "json" || a.Name == "v2" || a.Name == "timeout" || a.Name == "non-interactive" {
			return
		}
		found := false
		for _, key := range allowed {
			found = found || key == a.Name
		}
		invalid = invalid || !found
	})
	if invalid {
		return misuse(stderr)
	}
	for _, key := range allowed {
		if key == "once" || key == "initialize" || key == "invocation" {
			continue
		}
		if get(key) == "" {
			return misuse(stderr)
		}
	}
	if op == "run" && get("command") == "" && !*once {
		return misuse(stderr)
	}
	if op == "run" && !*initialize && get("command") == "" && get("invocation") == "" || op == "reconcile" && get("command") == "" && get("invocation") == "" {
		return misuse(stderr)
	}
	if used["as-of"] {
		if _, e := time.Parse(time.RFC3339Nano, get("as-of")); e != nil {
			return misuse(stderr)
		}
	}
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	var data any
	var err error
	code, status, effect := 0, "ok", "none"
	if get("command") != "" {
		raw, e := sdk.ReadLocal(ctx, get("command"))
		if e != nil {
			err = e
		} else {
			var c i.Command
			if e = sdk.DecodeRead(raw, &c); e != nil {
				err = e
			} else if !integrationCommandMatches(op, c) {
				err = errors.New("CLI action does not match signed IPC command")
			} else {
				data, err = sdk.ForwardIntegration(ctx, get("listen"), c)
			}
		}
	} else {
		switch op {
		case "validate", "preview":
			data, err = sdk.PreviewSyncV2(ctx, sdk.SyncV2Request{Configuration: get("configuration"), State: get("state"), Source: get("source"), Destination: get("destination"), SourceObservation: get("source-observation"), DestinationObservation: get("observation"), ForwardMapping: get("forward-mapping"), ReverseMapping: get("reverse-mapping"), ForwardContext: get("forward-context"), ReverseContext: get("reverse-context"), SourceBinding: get("source-binding"), DestinationBinding: get("binding"), SourceQuery: get("source-query"), DestinationQuery: get("destination-query"), AsOf: get("as-of"), Out: get("out")})
			if op == "preview" {
				effect = "possible"
			}
		case "resolve":
			data, err = sdk.ResolveSyncV2(ctx, get("plan"), get("resolution"), get("as-of"), get("out"))
			effect = "possible"
		case "ingest":
			data, err = svc.IngestIntegration(ctx, get("event-envelope"), get("policy"), get("enrollment"), get("out"))
			effect = "possible"
		case "serve":
			var c i.Configuration
			raw, e := sdk.ReadLocal(ctx, get("configuration"))
			if e != nil {
				err = e
			} else if sdk.DecodeRead(raw, &c) != nil || c.Listen != get("listen") {
				err = errors.New("listener configuration mismatch")
			} else {
				data, err = svc.ServeIntegration(ctx, get("workspace"), get("configuration"), get("policy"), get("enrollment"))
			}
			effect = "possible"
		default:
			action := "sync-" + op
			if *initialize {
				action = "sync-initialize"
			}
			data, err = svc.ExecuteSyncV2(ctx, sdk.ExecuteSyncV2Request{Plan: get("plan"), Invocation: get("invocation"), Policy: get("policy"), Enrollment: get("enrollment"), Action: action, Out: get("out"), Once: *once})
			effect = "possible"
		}
	}
	diagnostics := []api.Diagnostic{}
	switch r := data.(type) {
	case z.Plan:
		code = sdk.SyncV2Exit(r)
		status = r.Status
		diagnostics = r.Diagnostics
	case z.ResolutionPreview:
		code = sdk.SyncV2Exit(r.Plan)
		status = r.Plan.Status
		diagnostics = r.Plan.Diagnostics
	case i.Report:
		status = r.Status
		diagnostics = r.Diagnostics
		switch r.Status {
		case "blocked":
			code = 3
		case "conflict":
			code = 4
		case "unknown":
			code = 5
		case "partial":
			code = 6
		}
	}
	if err != nil {
		code = 2
		status = "error"
		var e *sdk.Error
		if errors.As(err, &e) {
			code = e.ExitCode
			diagnostics = append(diagnostics, api.Diagnostic{Code: e.Code, Message: e.Message, Severity: "error"})
		} else {
			diagnostics = append(diagnostics, api.Diagnostic{Code: "integration.invalid", Message: "integration evidence or current authority failed validation", Severity: "error"})
		}
		if code == 3 {
			status = "blocked"
		}
		if code == 5 {
			status = "unknown"
		}
	}
	if status == "ready" {
		status = "ok"
	}
	if status == "baseline-candidate" {
		status = "blocked"
	}
	result := sdk.Result(group+" "+op, status, data, diagnostics, effect)
	if r, ok := data.(i.Report); ok {
		possible := code == 5
		for _, observation := range r.Operations {
			if observation.OperationID != "" {
				result.Spec.OperationIds = append(result.Spec.OperationIds, observation.OperationID)
			}
			possible = possible || observation.Attempts > 0 || observation.State == "readback-observed"
		}
		if possible {
			result.Spec.Effects.Provider = "possible"
		}
	}
	if *jsonOut {
		var rendered any = result
		if suite {
			rendered = sdk.SuiteResult(result, code)
		}
		if json.NewEncoder(stdout).Encode(rendered) != nil {
			return 1
		}
	} else {
		fmt.Fprintf(stdout, "%s %s: %s\n", group, op, status)
		for _, d := range diagnostics {
			fmt.Fprintln(stderr, d.Code+": "+d.Message)
		}
	}
	return code
}

// Read-only verbs cannot forward effectful signed envelopes.
func integrationCommandMatches(op string, c i.Command) bool {
	if op == "status" || op == "conflicts" {
		return c.Action == "status" && c.Batch == nil
	}
	if c.Action != "submit" || c.Batch == nil {
		return false
	}
	want := "sync-" + op
	if op == "run" && c.Batch.Action == "sync-initialize" {
		return true
	}
	return c.Batch.Action == want
}
