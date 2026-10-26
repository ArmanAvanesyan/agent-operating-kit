package command

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	n "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/nativev1"
	o "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/operationsv1"
	api "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/v1alpha1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/sdk"
	"io"
	"time"
)

const nativeHelp = `WorkKit P6 observation core (no native mutations or model/session launches):
 native hosts [--json]
 native inspect --bundle BUNDLE --as-of RFC3339 [--json]
 native plan|bind|validate-binding --assignment FILE --profile FILE --mapping FILE --handle FILE --as-of RFC3339 [--out NEW] [--json]
 native observe|reconcile --assignment FILE --profile FILE --mapping FILE --handle FILE --evidence FILE --as-of RFC3339 --out NEW [--json]
 native observe --assignment FILE --profile FILE --mapping FILE --handle FILE --policy FILE --enrollment FILE --out NEW [--json]
 native plan --progress-from BUNDLE --link FILE --snapshot BUNDLE --observation BUNDLE --desired FILE --mapping FILE --binding FILE [--binding-root ROOT] --capability FILE --policy FILE --as-of RFC3339 --out NEW [--json]
 native apply (blocked: no native writer/admission is installed)
Projection/binding outputs require fresh --out; validate-binding and inspect write reports only.
Connected reads require protected compiled enrollment. Offline parsing is historical consistency only.
P5 live qualification pending; migration activation blocked; production activation unauthorized.
`

func runNative(ctx context.Context, args []string, stdout, stderr io.Writer, svc *sdk.Service, suite bool) int {
	if len(args) < 2 {
		fmt.Fprint(stderr, nativeHelp)
		return 2
	}
	op := args[1]
	if op == "--help" || op == "help" || op == "-h" {
		fmt.Fprint(stdout, nativeHelp)
		return 0
	}
	f := flag.NewFlagSet("native "+op, flag.ContinueOnError)
	f.SetOutput(io.Discard)
	v := map[string]*string{}
	for _, key := range []string{"assignment", "profile", "mapping", "handle", "evidence", "as-of", "out", "bundle", "policy", "enrollment", "progress-from", "link", "snapshot", "observation", "desired", "binding", "binding-root", "capability", "operation-request"} {
		v[key] = f.String(key, "", key)
	}
	jsonOut := f.Bool("json", false, "")
	timeout := f.Duration("timeout", 5*time.Minute, "")
	f.Bool("non-interactive", false, "")
	if e := f.Parse(args[2:]); e != nil {
		if errors.Is(e, flag.ErrHelp) {
			fmt.Fprint(stdout, nativeHelp)
			return 0
		}
		return misuse(stderr)
	}
	if f.NArg() != 0 || *timeout <= 0 || *timeout > 5*time.Minute {
		return misuse(stderr)
	}
	rules := map[string][]string{"hosts": {}, "inspect": {"bundle", "as-of"}, "apply": {}, "plan": {"assignment", "profile", "mapping", "handle", "as-of", "out", "operation-request"}, "bind": {"assignment", "profile", "mapping", "handle", "as-of", "out"}, "validate-binding": {"assignment", "profile", "mapping", "handle", "as-of"}, "observe": {"assignment", "profile", "mapping", "handle", "evidence", "as-of", "policy", "enrollment", "out"}, "reconcile": {"assignment", "profile", "mapping", "handle", "evidence", "as-of", "out"}}
	if op == "plan" && *v["progress-from"] != "" {
		rules[op] = []string{"progress-from", "link", "snapshot", "observation", "desired", "mapping", "binding", "binding-root", "capability", "policy", "as-of", "out"}
	}
	allowed, ok := rules[op]
	if !ok {
		return misuse(stderr)
	}
	used := map[string]bool{}
	invalid := false
	f.Visit(func(x *flag.Flag) {
		used[x.Name] = true
		if x.Name == "json" || x.Name == "timeout" || x.Name == "non-interactive" {
			return
		}
		found := false
		for _, a := range allowed {
			found = found || a == x.Name
		}
		invalid = invalid || !found
	})
	if invalid {
		return misuse(stderr)
	}
	get := func(key string) string { return *v[key] }
	connected := op == "observe" && get("evidence") == ""
	for _, key := range allowed {
		optional := key == "binding-root" || key == "operation-request" || op == "observe" && (connected && (key == "evidence" || key == "as-of") || !connected && (key == "policy" || key == "enrollment"))
		if !optional && get(key) == "" {
			return misuse(stderr)
		}
	}
	if op == "observe" && (connected && (used["as-of"] || used["evidence"]) || !connected && (used["policy"] || used["enrollment"])) {
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
	effect := "none"
	code := 0
	status := "ok"
	q := sdk.NativeRequest{Assignment: get("assignment"), Profile: get("profile"), Mapping: get("mapping"), Handle: get("handle"), Evidence: get("evidence"), Operation: get("operation-request"), AsOf: get("as-of"), Out: get("out")}
	switch op {
	case "hosts":
		data = sdk.NativeHosts()
	case "apply":
		err = sdk.NativeMutationUnavailable()
	case "inspect":
		data, err = sdk.InspectNative(ctx, get("bundle"), get("as-of"))
	case "observe":
		if connected {
			data, err = svc.ObserveNative(ctx, q, get("policy"), get("enrollment"))
		} else {
			data, err = sdk.NativeEvaluate(ctx, "NativeObservation", q)
		}
		effect = "possible"
	case "reconcile":
		data, err = sdk.NativeEvaluate(ctx, "NativeReconciliation", q)
		effect = "possible"
	case "plan":
		if get("progress-from") != "" {
			data, err = sdk.PlanNativeProgress(ctx, get("progress-from"), get("link"), sdk.PlanItemRequest{Snapshot: get("snapshot"), Observation: get("observation"), Desired: get("desired"), Mapping: get("mapping"), Binding: get("binding"), BindingRoot: get("binding-root"), Capability: get("capability"), Policy: get("policy"), AsOf: get("as-of"), Out: get("out")})
		} else {
			data, err = sdk.NativeEvaluate(ctx, "NativeProjection", q)
		}
		effect = "possible"
	default:
		data, err = sdk.NativeEvaluate(ctx, "NativeBindingCandidate", q)
		if q.Out != "" {
			effect = "possible"
		}
	}
	diagnostics := []api.Diagnostic{}
	if r, ok := data.(n.Report); ok {
		code = sdk.NativeReportExit(r)
		status = r.Status
		diagnostics = r.Diagnostics
	}
	if draft, ok := data.(o.Draft); ok {
		status = "ok"
		diagnostics = draft.Diagnostics
		if draft.Status == "blocked" {
			code = 3
			status = "blocked"
		}
	}
	if err != nil {
		code = 2
		status = "error"
		var fail *sdk.Error
		if errors.As(err, &fail) {
			code = fail.ExitCode
			diagnostics = append(diagnostics, api.Diagnostic{Code: fail.Code, Message: fail.Message, Severity: "error"})
		} else {
			diagnostics = append(diagnostics, api.Diagnostic{Code: "native.invalid", Message: "native evidence failed safe validation", Severity: "error"})
		}
		if code == 3 {
			status = "blocked"
		}
		data = map[string]any{"authorizationGranted": false, "executionReady": false, "migrationActivated": false, "productionActivated": false}
	}
	result := sdk.Result("native "+op, status, data, diagnostics, effect)
	if draft, ok := data.(o.Draft); ok {
		result.Spec.OperationIds = []string{draft.ID}
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
		fmt.Fprintf(stdout, "native %s: %s\n", op, status)
		for _, d := range diagnostics {
			fmt.Fprintln(stderr, d.Code+": "+d.Message)
		}
	}
	return code
}
