package command

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	o "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/operationsv1"
	y "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/syncv1"
	api "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/v1alpha1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/sdk"
	"io"
	"strings"
	"time"
)

const operationsHelp = `WorkKit controlled operations (no default writer enrollment):
 items plan-create|plan-update|plan-transition|plan-comment|plan-relate --snapshot BUNDLE --observation BUNDLE --desired FILE --mapping FILE --binding FILE --capability FILE --policy FILE --as-of RFC3339 --out NEW
 operations list|inspect --store FILE [--operation ID]
 operations apply|reconcile|cancel --plan BUNDLE --policy FILE --enrollment FILE --out NEW
 setup apply|reconcile --plan P4-BUNDLE --operation CHANGE-ID --policy FILE --enrollment FILE --out NEW
 sync validate|preview --configuration FILE --state FILE --source BUNDLE --destination BUNDLE --source-observation BUNDLE --observation BUNDLE --mapping FILE --mapping-context FILE --source-binding FILE --binding FILE --query FILE --as-of RFC3339 [--out NEW]
 sync status --store FILE --route SHA256
 sync conflicts --plan BUNDLE
 sync run --once [--initialize] --plan BUNDLE --invocation FILE --policy FILE --enrollment FILE --out NEW
 sync pause|resume|reconcile --plan BUNDLE --policy FILE --enrollment FILE --out NEW [--invocation FILE]
All commands accept --json. Planning never resolves credentials. Migration activation blocked; live qualification pending; production activation unauthorized.
`

func isOperationCommand(a []string) bool {
	if len(a) == 0 {
		return false
	}
	if a[0] == "operations" || a[0] == "sync" {
		return true
	}
	if len(a) > 1 && a[0] == "items" && strings.HasPrefix(a[1], "plan-") {
		return true
	}
	return len(a) > 1 && a[0] == "setup" && (a[1] == "apply" || a[1] == "reconcile")
}
func runOperations(ctx context.Context, args []string, out, errout io.Writer, svc *sdk.Service, suite bool) int {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	if len(args) < 2 {
		return misuse(errout)
	}
	group, op := args[0], args[1]
	if op == "--help" || op == "help" {
		fmt.Fprint(out, operationsHelp)
		return 0
	}
	f := flag.NewFlagSet("operations", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	v := map[string]*string{}
	for _, key := range []string{"snapshot", "observation", "desired", "mapping", "binding", "binding-root", "capability", "policy", "as-of", "out", "plan", "enrollment", "store", "operation", "configuration", "state", "source", "destination", "source-observation", "mapping-context", "source-binding", "query", "route", "invocation"} {
		v[key] = f.String(key, "", key)
	}
	asJSON := f.Bool("json", false, "")
	once := f.Bool("once", false, "")
	initialize := f.Bool("initialize", false, "")
	if e := f.Parse(args[2:]); e != nil {
		if errors.Is(e, flag.ErrHelp) {
			fmt.Fprint(out, operationsHelp)
			return 0
		}
		return misuse(errout)
	}
	if f.NArg() != 0 {
		return misuse(errout)
	}
	allowed := map[string]bool{"json": true}
	permit := func(names string) {
		for _, n := range strings.Fields(names) {
			allowed[n] = true
		}
	}
	switch group {
	case "items":
		permit("snapshot observation desired mapping binding binding-root capability policy as-of out")
	case "operations":
		if op == "list" || op == "inspect" {
			permit("store operation")
		} else {
			permit("plan policy enrollment out")
		}
	case "setup":
		permit("plan operation policy enrollment out")
	case "sync":
		switch op {
		case "validate", "preview":
			permit("configuration state source destination source-observation observation mapping mapping-context source-binding binding query as-of")
			if op == "preview" {
				permit("out")
			}
		case "status":
			permit("store route")
		case "conflicts":
			permit("plan")
		default:
			permit("plan policy enrollment out invocation")
			if op == "run" {
				permit("once initialize")
			}
		}
	}
	bad := false
	f.Visit(func(a *flag.Flag) {
		if !allowed[a.Name] {
			bad = true
		}
	})
	if bad {
		return misuse(errout)
	}

	get := func(k string) string { return *v[k] }
	required := func(keys ...string) bool {
		for _, key := range keys {
			if get(key) == "" {
				return false
			}
		}
		return true
	}
	cmd := group + " " + op
	var data any
	var err error
	code := 0
	status := "ok"
	effect := "none"
	switch group {
	case "items":
		kinds := map[string]string{"plan-create": "create-item", "plan-update": "update-fields", "plan-transition": "transition", "plan-comment": "comment", "plan-relate": "relate"}
		kind, ok := kinds[op]
		if !ok || !required("snapshot", "observation", "desired", "mapping", "binding", "capability", "policy", "as-of", "out") {
			return misuse(errout)
		}
		var d o.Draft
		d, err = sdk.PlanItemOperation(ctx, sdk.PlanItemRequest{Snapshot: get("snapshot"), Observation: get("observation"), Desired: get("desired"), Mapping: get("mapping"), Binding: get("binding"), BindingRoot: get("binding-root"), Capability: get("capability"), Policy: get("policy"), AsOf: get("as-of"), Out: get("out"), Operation: kind})
		data = d
		status = d.Status
		effect = "possible"
		if d.Status == "blocked" {
			code = 3
		}
	case "operations":
		switch op {
		case "list", "inspect":
			if !required("store") || op == "inspect" && !required("operation") {
				return misuse(errout)
			}
			raw, e := sdk.ReadLocal(ctx, get("store"))
			var store o.Store
			if e != nil || sdk.DecodeRead(raw, &store) != nil {
				return misuse(errout)
			}
			data, err = svc.InspectOperations(ctx, store, get("operation"))
		case "apply", "reconcile", "cancel":
			if !required("plan", "policy", "enrollment", "out") {
				return misuse(errout)
			}
			data, err = svc.ExecuteOperation(ctx, sdk.ExecuteOperationRequest{Plan: get("plan"), Policy: get("policy"), Enrollment: get("enrollment"), Action: op, Out: get("out")})
			effect = "possible"
		default:
			return misuse(errout)
		}
	case "setup":
		if !required("plan", "operation", "policy", "enrollment", "out") {
			return misuse(errout)
		}
		data, err = svc.ExecuteSetupOperation(ctx, sdk.ExecuteSetupOperationRequest{Plan: get("plan"), Operation: get("operation"), Policy: get("policy"), Enrollment: get("enrollment"), Out: get("out"), Action: op})
		effect = "possible"
	case "sync":
		switch op {
		case "validate", "preview":
			if !required("configuration", "state", "source", "destination", "source-observation", "observation", "mapping", "mapping-context", "source-binding", "binding", "query", "as-of") || op == "preview" && !required("out") {
				return misuse(errout)
			}
			var p y.Plan
			p, err = sdk.PreviewSync(ctx, sdk.SyncRequest{Configuration: get("configuration"), State: get("state"), Source: get("source"), Destination: get("destination"), SourceObservation: get("source-observation"), DestinationObservation: get("observation"), Mapping: get("mapping"), MappingContext: get("mapping-context"), SourceBinding: get("source-binding"), DestinationBinding: get("binding"), Query: get("query"), AsOf: get("as-of"), Out: get("out")})
			data = p
			status = p.Status
			if p.Status == "blocked" || p.Status == "baseline-candidate" {
				code = 3
			}
			if p.Status == "conflict" {
				code = 4
			}
			if get("out") != "" {
				effect = "possible"
			}
		case "conflicts":
			if !required("plan") {
				return misuse(errout)
			}
			var p y.Plan
			p, err = sdk.LoadSyncPlan(ctx, get("plan"))
			data = p.Conflicts
		case "status":
			if !required("store", "route") {
				return misuse(errout)
			}
			raw, e := sdk.ReadLocal(ctx, get("store"))
			var store o.Store
			if e != nil || sdk.DecodeRead(raw, &store) != nil {
				return misuse(errout)
			}
			data, err = svc.InspectSync(ctx, store, get("route"))
		case "run", "pause", "resume", "reconcile":
			if !required("plan", "policy", "enrollment", "out") || op == "run" && !*once {
				return misuse(errout)
			}
			action := "sync-" + op
			if *initialize {
				if op != "run" {
					return misuse(errout)
				}
				action = "sync-initialize"
			}
			data, err = svc.ExecuteSync(ctx, sdk.ExecuteSyncRequest{Plan: get("plan"), Invocation: get("invocation"), Policy: get("policy"), Enrollment: get("enrollment"), Out: get("out"), Action: action, Once: *once})
			effect = "possible"
		default:
			return misuse(errout)
		}
	}
	diagnostics := []api.Diagnostic{}
	if err != nil {
		code = 2
		var typed *sdk.Error
		if errors.As(err, &typed) {
			code = typed.ExitCode
		} else if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			code = 1
		}
		status = "error"
		if code == 3 {
			status = "blocked"
		}
		if code == 4 {
			status = "conflict"
		}
		if code == 5 {
			status = "unknown"
		}
		diagnostics = append(diagnostics, api.Diagnostic{Code: "operations.failed", Field: cmd, Message: "operation failed validation or protected prerequisites; inspect immutable evidence", Severity: "error"})
	} else if effect == "possible" {
		effect = "confirmed"
	}
	if status == "draft" || status == "ready" {
		status = "ok"
	}
	if status == "baseline-candidate" {
		status = "blocked"
	}
	if code == 6 {
		status = "partial"
	}
	result := sdk.Result(cmd, status, data, diagnostics, effect)
	switch value := data.(type) {
	case o.Draft:
		result.Spec.OperationIds = []string{value.ID}
	case o.Observation:
		if value.OperationID != "" {
			result.Spec.OperationIds = []string{value.OperationID}
		}
	case y.Run:
		for _, op := range value.Operations {
			if op.OperationID != "" {
				result.Spec.OperationIds = append(result.Spec.OperationIds, op.OperationID)
			}
		}
	}
	if cmd == "operations apply" || cmd == "setup apply" || cmd == "sync run" {
		possible := code == 5
		switch value := data.(type) {
		case o.Observation:
			possible = possible || value.Attempts > 0 || value.State == "readback-observed"
		case y.Run:
			for _, obs := range value.Operations {
				possible = possible || obs.Attempts > 0
			}
		}
		if possible {
			result.Spec.Effects.Provider = "possible"
		}
	}
	if *asJSON {
		var value any = result
		if suite {
			value = sdk.SuiteResult(result, code)
		}
		_ = json.NewEncoder(out).Encode(value)
	} else {
		fmt.Fprintf(out, "%s: %s (migration blocked; live qualification pending; production unauthorized)\n", cmd, status)
	}
	return code
}
