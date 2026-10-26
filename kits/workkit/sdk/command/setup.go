package command

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	s "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/setupv1"
	api "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/v1alpha1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/sdk"
	"io"
	"os"
	"strings"
	"time"
)

const setupHelp = `WorkKit P4 (opt-in; no provider writes, migration or production activation):
 setup discover --capture BUNDLE --out NEW
 setup discover --connection-file FILE --policy FILE --enrollment FILE --capture-out NEW --out NEW
 setup plan --observation BUNDLE --desired FILE --as-of RFC3339 --out NEW
 setup validate-plan --plan BUNDLE --as-of RFC3339
 setup verify --observation BUNDLE [--source-observation BUNDLE] --desired FILE --mapping FILE --mapping-context FILE --as-of RFC3339 --out NEW
 setup verify ... --policy FILE --verification FILE --out NEW [--write-binding --workspace FILE --root DIR --expected-workspace-sha256 HASH]
 mappings validate|inspect --mapping FILE --mapping-context FILE --source-observation BUNDLE --observation BUNDLE --as-of RFC3339
 mappings preview ... --snapshot BUNDLE --out NEW
 views inspect --view FILE --observation BUNDLE --id NATIVE-ID --as-of RFC3339
 views plan-create|plan-update --observation BUNDLE --view FILE --parent-id ID [--id EXISTING] --as-of RFC3339 --out NEW
All commands accept --json. Verification requires separately issued operational evidence; standard bootstrap cannot grant binding selection. setup apply/reconcile and views open remain unavailable.
`

func isSetupCommand(args []string) bool {
	if len(args) == 0 {
		return false
	}
	if args[0] == "setup" || args[0] == "mappings" {
		return true
	}
	if args[0] == "views" && len(args) > 1 {
		if args[1] == "plan-create" || args[1] == "plan-update" {
			return true
		}
		if args[1] == "inspect" {
			for _, arg := range args[2:] {
				if arg == "--observation" || strings.HasPrefix(arg, "--observation=") {
					return true
				}
			}
		}
	}
	return false
}

func runSetup(ctx context.Context, args []string, out, errout io.Writer, svc *sdk.Service, suite bool) int {
	if len(args) < 2 {
		fmt.Fprint(errout, setupHelp)
		return 2
	}
	group, op := args[0], args[1]
	if op == "--help" || op == "help" || op == "-h" {
		fmt.Fprint(out, setupHelp)
		return 0
	}
	f := flag.NewFlagSet("setup", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	values := map[string]*string{}
	for _, name := range []string{"capture", "connection-file", "policy", "enrollment", "capture-out", "observation", "source-observation", "desired", "as-of", "out", "mapping", "mapping-context", "snapshot", "plan", "verification", "workspace", "root", "expected-workspace-sha256", "view", "parent-id", "id"} {
		values[name] = f.String(name, "", name)
	}
	jsonOut := f.Bool("json", false, "")
	write := f.Bool("write-binding", false, "")
	timeout := f.Duration("timeout", 5*time.Minute, "")
	f.Bool("non-interactive", false, "")
	if e := f.Parse(args[2:]); e != nil {
		if errors.Is(e, flag.ErrHelp) {
			fmt.Fprint(out, setupHelp)
			return 0
		}
		return misuse(errout)
	}
	if f.NArg() != 0 || *timeout <= 0 || *timeout > 5*time.Minute {
		return misuse(errout)
	}
	cmd := group + " " + op
	rules := map[string][]string{
		"setup discover": {"capture", "connection-file", "policy", "enrollment", "capture-out", "out"}, "setup plan": {"observation", "desired", "as-of", "out"}, "setup validate-plan": {"plan", "as-of"}, "setup verify": {"observation", "source-observation", "desired", "mapping", "mapping-context", "as-of", "policy", "verification", "out", "workspace", "root", "expected-workspace-sha256", "write-binding"},
		"mappings validate": {"mapping", "mapping-context", "source-observation", "observation", "as-of"}, "mappings inspect": {"mapping", "mapping-context", "source-observation", "observation", "as-of"}, "mappings preview": {"mapping", "mapping-context", "source-observation", "observation", "as-of", "snapshot", "out"}, "views inspect": {"observation", "view", "id", "as-of"}, "views plan-create": {"observation", "view", "parent-id", "as-of", "out"}, "views plan-update": {"observation", "view", "parent-id", "id", "as-of", "out"}}
	allowed, ok := rules[cmd]
	if !ok {
		fmt.Fprintln(errout, "unsupported P4 operation; mutation dispatch remains pending P5")
		return 3
	}
	used := map[string]bool{}
	invalid := false
	f.Visit(func(v *flag.Flag) {
		used[v.Name] = true
		if v.Name == "json" || v.Name == "timeout" || v.Name == "non-interactive" {
			return
		}
		ok := false
		for _, a := range allowed {
			if a == v.Name {
				ok = true
			}
		}
		if !ok {
			invalid = true
		}
	})
	if invalid {
		return misuse(errout)
	}
	get := func(n string) string { return *values[n] }
	required := []string{}
	switch cmd {
	case "setup discover":
		required = []string{"out"}
		if get("capture") == "" {
			required = append(required, "connection-file", "policy", "enrollment", "capture-out")
		} else if used["connection-file"] || used["policy"] || used["enrollment"] || used["capture-out"] {
			return misuse(errout)
		}
	case "setup verify":
		required = []string{"observation", "desired", "mapping", "mapping-context", "out"}
		if get("policy") != "" || get("verification") != "" || *write {
			required = append(required, "policy", "verification")
			if used["as-of"] {
				return misuse(errout)
			}
		} else {
			required = append(required, "as-of")
		}
		if *write {
			required = append(required, "workspace", "root", "expected-workspace-sha256")
		} else if used["workspace"] || used["root"] || used["expected-workspace-sha256"] {
			return misuse(errout)
		}
	default:
		required = allowed
	}
	for _, name := range required {
		if name == "write-binding" {
			continue
		}
		if get(name) == "" {
			return misuse(errout)
		}
	}
	if used["as-of"] {
		if _, e := time.Parse(time.RFC3339Nano, get("as-of")); e != nil {
			return misuse(errout)
		}
	}
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	var data any
	var e error
	code := 0
	status := "ok"
	effect := "none"
	switch cmd {
	case "setup discover":
		capture := get("capture")
		if capture == "" {
			_, e = svc.CaptureRead(ctx, get("connection-file"), get("policy"), get("enrollment"), "discover", "", get("capture-out"))
			capture = get("capture-out")
		}
		if e == nil {
			var o s.Observation
			o, e = svc.DiscoverSetup(ctx, capture, get("out"))
			data = o
			for _, c := range o.Coverage {
				if !c.Complete {
					code = 6
					status = "partial"
				}
			}
		}
		effect = "possible"
	case "setup plan":
		var r s.Report
		r, e = sdk.PlanSetup(ctx, get("observation"), get("desired"), get("as-of"), get("out"))
		data = r
		code = setupExit(r)
		status = r.Status
		effect = "possible"
	case "setup validate-plan":
		var r s.Report
		r, e = sdk.ValidateSetupPlan(ctx, get("plan"), get("as-of"))
		data = r
		code = setupExit(r)
		status = r.Status
	case "mappings validate", "mappings inspect":
		var r s.Report
		r, e = sdk.ValidateSetupMapping(ctx, get("mapping"), get("mapping-context"), get("source-observation"), get("observation"), get("as-of"))
		data = r
		code = setupExit(r)
		status = r.Status
	case "mappings preview":
		var p s.Preview
		p, e = sdk.PreviewSetupMapping(ctx, get("mapping"), get("mapping-context"), get("source-observation"), get("observation"), get("snapshot"), get("as-of"), get("out"))
		data = p
		code = setupExit(p.Report)
		status = p.Report.Status
		effect = "possible"
	case "setup verify":
		var receipt s.BindingReceipt
		receipt, e = svc.VerifySetup(ctx, sdk.VerifySetupRequest{Observation: get("observation"), SourceObservation: get("source-observation"), Desired: get("desired"), Mapping: get("mapping"), MappingContext: get("mapping-context"), AsOf: get("as-of"), Policy: get("policy"), Verification: get("verification"), Out: get("out"), Workspace: get("workspace"), Root: get("root"), ExpectedWorkspaceSHA256: get("expected-workspace-sha256"), WriteBinding: *write})
		data = receipt
		code = setupExit(receipt.Report)
		status = receipt.Report.Status
		effect = "possible"
	case "views inspect":
		var result s.ViewInspection
		result, e = sdk.InspectSetupProviderView(ctx, get("observation"), get("view"), get("id"), get("as-of"))
		data = result
		code = setupExit(result.Report)
		status = result.Report.Status
	case "views plan-create", "views plan-update":
		var d s.ViewDraft
		d, e = sdk.PlanSetupView(ctx, get("observation"), get("view"), get("parent-id"), get("id"), op, get("as-of"), get("out"))
		data = d
		code = setupExit(d.Report)
		status = d.Report.Status
		effect = "possible"
	}
	diagnostics := []api.Diagnostic{}
	switch v := data.(type) {
	case s.Report:
		diagnostics = append(diagnostics, v.Diagnostics...)
	case s.Preview:
		diagnostics = append(diagnostics, v.Report.Diagnostics...)
	case s.ViewInspection:
		diagnostics = append(diagnostics, v.Report.Diagnostics...)
	case s.ViewDraft:
		diagnostics = append(diagnostics, v.Report.Diagnostics...)
	case s.BindingReceipt:
		diagnostics = append(diagnostics, v.Report.Diagnostics...)
	}
	if e != nil {
		code = 1
		var typed *sdk.Error
		if errors.As(e, &typed) {
			code = typed.ExitCode
		} else {
			var pathError *os.PathError
			if os.IsExist(e) {
				code = 4
			} else if !errors.As(e, &pathError) && !errors.Is(e, context.Canceled) && !errors.Is(e, context.DeadlineExceeded) {
				code = 2
			}
		}
		status = "error"
		diagnostics = append(diagnostics, api.Diagnostic{Code: "setup.failed", Field: cmd, Message: "setup operation failed; inputs or protected prerequisites could not be validated", Severity: "error"})
	} else if effect == "possible" {
		effect = "confirmed"
	}
	result := sdk.Result(cmd, status, data, diagnostics, effect)
	if *jsonOut {
		var v any = result
		if suite {
			v = sdk.SuiteResult(result, code)
		}
		_ = json.NewEncoder(out).Encode(v)
	} else {
		fmt.Fprintf(out, "%s: %s (migration blocked; live qualification pending; production unauthorized)\n", cmd, status)
	}
	return code
}
func setupExit(r s.Report) int {
	if r.Status == "blocked" {
		for _, d := range r.Diagnostics {
			if d.Code == "refresh-needed" {
				return 4
			}
		}
		return 3
	}
	if r.Status == "partial" {
		return 6
	}
	return 0
}
