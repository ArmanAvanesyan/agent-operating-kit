package command

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	a "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/accessv1"
	api "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/v1alpha1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/sdk"
	"io"
	"strings"
	"time"
)

const accessHelp = `WorkKit P3 access commands (opt-in; no activation):
 tools doctor (--tool PIN | --discovery REQUEST) --as-of RFC3339
 tools plan --artifact FILE --policy FILE --destination ABSOLUTE --as-of RFC3339 --out NEW [--action update --previous-path ABSOLUTE --previous-sha256 HASH]
 tools verify --tool FILE --policy FILE --grant FILE --out NEW
 tools install|update --plan FILE --policy FILE --grant FILE --out NEW
 auth status [--configuration-file FILE --receipt FILE --as-of RFC3339]
 auth login|refresh|logout CONNECTION --configuration-file FILE --policy FILE --grant FILE --out NEW
 connections add|remove ID [--configuration-file FILE] [--registry FILE --expected-sha256 HASH] --as-of RFC3339 --out NEW
 connections list --workspace FILE
 connections inspect CONNECTION --configuration-file FILE
 connections test CONNECTION --connection-file FILE --policy FILE --enrollment FILE --out NEW
 connections test CONNECTION --configuration-file AUTH-WITH-READ.json --policy ACCESS-POLICY --grant GRANT --out NEW
All accept --json. Native CLI authentication requires fresh external control enrollment.
Status is offline metadata, never current provider access. No implicit refresh/install/login.
`

func runAccess(ctx context.Context, args []string, out, errout io.Writer, s *sdk.Service, suite bool) int {
	if len(args) < 2 {
		fmt.Fprint(errout, accessHelp)
		return 2
	}
	group, op := args[0], args[1]
	rest := args[2:]
	if op == "--help" || op == "help" || op == "-h" {
		fmt.Fprint(out, accessHelp)
		return 0
	}
	id := ""
	if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		id = rest[0]
		rest = rest[1:]
	}
	f := flag.NewFlagSet("access", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	js := f.Bool("json", false, "")
	noninteractive := f.Bool("non-interactive", false, "")
	timeout := f.Duration("timeout", 5*time.Minute, "")
	tool := f.String("tool", "", "")
	discovery := f.String("discovery", "", "")
	registry := f.String("registry", "", "")
	expected := f.String("expected-sha256", "", "")
	policy := f.String("policy", "", "")
	grant := f.String("grant", "", "")
	plan := f.String("plan", "", "")
	artifact := f.String("artifact", "", "")
	dest := f.String("destination", "", "")
	action := f.String("action", "install", "")
	prev := f.String("previous-path", "", "")
	prevHash := f.String("previous-sha256", "", "")
	asof := f.String("as-of", "", "")
	output := f.String("out", "", "")
	config := f.String("configuration-file", "", "")
	receipt := f.String("receipt", "", "")
	workspace := f.String("workspace", "", "")
	readConfig := f.String("connection-file", "", "")
	enrollment := f.String("enrollment", "", "")
	if e := f.Parse(rest); e != nil || f.NArg() != 0 {
		return misuse(errout)
	}
	used := map[string]bool{}
	f.Visit(func(v *flag.Flag) { used[v.Name] = true })
	delete(used, "json")
	delete(used, "non-interactive")
	delete(used, "timeout")
	if *timeout <= 0 || *timeout > 5*time.Minute {
		return misuse(errout)
	}
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	allowed := map[string]bool{}
	allow := func(names ...string) {
		for _, x := range names {
			allowed[x] = true
		}
	}
	cmd := group + " " + op
	// Reject every irrelevant flag before reading inputs or consuming authority.
	flagSets := map[string]string{
		"tools doctor": "tool discovery as-of", "tools plan": "artifact policy destination as-of out action previous-path previous-sha256", "tools install": "plan policy grant out", "tools update": "plan policy grant out", "tools verify": "tool policy grant out", "auth status": "configuration-file receipt as-of", "auth login": "configuration-file policy grant out", "auth refresh": "configuration-file policy grant out", "auth logout": "configuration-file policy grant out", "connections inspect": "configuration-file registry", "connections list": "workspace registry", "connections test": "connection-file configuration-file policy enrollment grant out", "connections add": "configuration-file registry expected-sha256 as-of out", "connections remove": "registry expected-sha256 as-of out"}
	names, known := flagSets[cmd]
	if !known {
		return misuse(errout)
	}
	for name := range used {
		ok := false
		for _, x := range strings.Fields(names) {
			if x == name {
				ok = true
			}
		}
		if !ok {
			return misuse(errout)
		}
	}

	var data any
	var err error
	effect := "none"
	providerEffect := "none"
	code := 0
	status := "ok"
	read := func(path string) []byte {
		if err != nil {
			return nil
		}
		var b []byte
		b, err = sdk.ReadLocal(ctx, path)
		return b
	}
	at := func() time.Time {
		t, e := time.Parse(time.RFC3339Nano, *asof)
		if e != nil {
			err = &sdk.Error{Code: "time.invalid", Message: "explicit RFC3339 as-of required", ExitCode: 2}
		}
		return t
	}
	switch cmd {
	case "tools doctor":
		allow("tool", "discovery", "as-of")
		if *discovery != "" {
			if *tool != "" || *asof == "" || id != "" {
				return misuse(errout)
			}
			raw := read(*discovery)
			_ = at()
			if err == nil {
				data, err = sdk.DiscoverTools(ctx, raw)
			}
			break
		}
		if *tool == "" || *asof == "" || id != "" {
			return misuse(errout)
		}
		b := read(*tool)
		t := at()
		if err == nil {
			data, err = sdk.InspectTool(ctx, b, t)
		}
	case "tools plan":
		allow("artifact", "policy", "destination", "as-of", "out", "action", "previous-path", "previous-sha256")
		if *artifact == "" || *policy == "" || *dest == "" || *asof == "" || *output == "" || id != "" {
			return misuse(errout)
		}
		p, b := read(*policy), read(*artifact)
		t := at()
		if err == nil {
			var result a.InstallPlan
			result, err = sdk.PlanTool(p, b, *dest, *action, *prev, *prevHash, t)
			data = result
			if err == nil {
				err = sdk.WriteAccessPlan(ctx, result, p, *output)
				effect = "possible"
				if err == nil {
					effect = "confirmed"
				}
			}
		}
	case "tools install", "tools update", "tools verify", "auth login", "auth refresh", "auth logout":
		allow("policy", "grant", "out")
		var path string
		if group == "tools" {
			if id != "" {
				return misuse(errout)
			}
			if op == "verify" {
				allow("tool")
				path = *tool
			} else {
				allow("plan")
				path = *plan
			}
		} else {
			allow("configuration-file")
			path = *config
			if id == "" {
				return misuse(errout)
			}
		}
		if path == "" || *policy == "" || *grant == "" || *output == "" {
			return misuse(errout)
		}
		request, p, g := read(path), read(*policy), read(*grant)
		if err == nil && group == "auth" {
			var c a.Configuration
			c, err = sdk.ValidateAuthConfiguration(request)
			if err == nil && *noninteractive && op == "login" && c.Mode == "oauth2" {
				err = &sdk.Error{Code: "auth.challenge-required", Message: "interactive OAuth challenge required; no login was started", ExitCode: 3}
			}
			if err == nil && id != c.ConnectionID {
				return misuse(errout)
			}
		}
		if err == nil {
			var r a.Receipt
			r, err = s.RunAccess(ctx, cmd, request, p, g, *output)
			data = r
			if r.OperationID != "" {
				effect = r.LocalEffect
				providerEffect = r.ProviderEffect
			}
		}
	case "auth status":
		allow("configuration-file", "receipt", "as-of")
		if id != "" {
			return misuse(errout)
		}
		if *config == "" && *receipt == "" && *asof == "" {
			data = sdk.AccessRoutes()
		} else {
			if *config == "" || *receipt == "" || *asof == "" {
				return misuse(errout)
			}
			c, r := read(*config), read(*receipt)
			t := at()
			if err == nil {
				data, err = sdk.InspectAuth(c, r, t)
			}
		}
	case "connections inspect":
		allow("configuration-file", "registry")
		if *registry != "" {
			if *config != "" || id == "" {
				return misuse(errout)
			}
			raw := read(*registry)
			if err == nil {
				var records a.ConnectionRegistry
				records, err = sdk.InspectConnections(raw)
				if err == nil {
					found := false
					for _, entry := range records.Connections {
						if entry.ID == id {
							found = true
							bytes, _ := base64.StdEncoding.DecodeString(entry.ConfigurationBase64)
							data, err = sdk.ValidateAuthConfiguration(bytes)
						}
					}
					if !found {
						err = &sdk.Error{Code: "connection.absent", Message: "connection absent", ExitCode: 3}
					}
				}
			}
			break
		}
		if *config == "" || id == "" {
			return misuse(errout)
		}
		raw := read(*config)
		if err == nil {
			var c a.Configuration
			c, err = sdk.ValidateAuthConfiguration(raw)
			if err == nil && c.ConnectionID != id {
				return misuse(errout)
			}
			data = c
		}
	case "connections list":
		allow("workspace", "registry")
		if *registry != "" {
			if *workspace != "" || id != "" {
				return misuse(errout)
			}
			raw := read(*registry)
			if err == nil {
				data, err = sdk.InspectConnections(raw)
			}
			break
		}
		if *workspace == "" || id != "" {
			return misuse(errout)
		}
		var report sdk.WorkspaceReport
		report, err = s.Resolve(ctx, sdk.WorkspaceRequest{Workspace: *workspace})
		if err == nil {
			rows := []sdk.ResourceSummary{}
			for _, r := range report.Resources {
				if r.Kind == "ConnectionProfile" {
					rows = append(rows, r)
				}
			}
			data = rows
		}
	case "connections add", "connections remove":
		allow("registry", "configuration-file", "expected-sha256", "as-of", "out")
		if id == "" || *asof == "" || *output == "" || op == "add" && *config == "" || op == "remove" && (*config != "" || *registry == "") {
			return misuse(errout)
		}
		var prior, configuration []byte
		if *registry != "" {
			prior = read(*registry)
		}
		if *config != "" {
			configuration = read(*config)
		}
		if err == nil {
			data, err = sdk.ChangeConnections(ctx, prior, configuration, *expected, id, op, *asof, *output)
			effect = "possible"
			if err == nil {
				effect = "confirmed"
			}
		}
	case "connections test":
		allow("connection-file", "configuration-file", "policy", "enrollment", "grant", "out")
		if *config != "" {
			if *readConfig != "" || *enrollment != "" || id == "" || *grant == "" || *policy == "" || *output == "" {
				return misuse(errout)
			}
			request, p, g := read(*config), read(*policy), read(*grant)
			if err == nil {
				var c a.Configuration
				c, err = sdk.ValidateAuthConfiguration(request)
				if err == nil && c.ConnectionID != id {
					return misuse(errout)
				}
				if err == nil {
					var receipt a.Receipt
					receipt, err = s.RunAccess(ctx, cmd, request, p, g, *output)
					data = receipt
					effect = receipt.LocalEffect
					providerEffect = receipt.ProviderEffect
				}
			}
			break
		}
		if *readConfig == "" || *policy == "" || *enrollment == "" || *output == "" || id == "" {
			return misuse(errout)
		}
		c, _, e := sdk.LoadReadConfiguration(ctx, *readConfig)
		if e != nil {
			err = e
		} else if c.ConnectionID != id {
			return misuse(errout)
		} else {
			data, err = s.CaptureRead(ctx, *readConfig, *policy, *enrollment, "capture", "", *output)
			effect = "possible"
			if err == nil {
				effect = "confirmed"
			}
		}
	default:
		return misuse(errout)
	}
	for name := range used {
		if !allowed[name] {
			return misuse(errout)
		}
	}
	diagnostics := []api.Diagnostic{}
	if err != nil {
		code = 1
		var e *sdk.Error
		if errors.As(err, &e) {
			code = e.ExitCode
			diagnostics = append(diagnostics, api.Diagnostic{Code: e.Code, Message: e.Message, Severity: "error"})
		} else {
			diagnostics = append(diagnostics, api.Diagnostic{Code: "access.failed", Message: "access inputs or operation rejected", Severity: "error"})
		}
		status = "error"
		if code == 3 {
			status = "blocked"
		}
		if code == 4 {
			status = "conflict"
		}
		if code == 6 {
			status = "partial"
		}
		if code == 5 {
			status = "unknown"
		}
	}
	result := sdk.Result(cmd, status, data, diagnostics, effect)
	result.Spec.Effects.Provider = providerEffect
	if r, ok := data.(a.Receipt); ok && r.OperationID != "" {
		result.Spec.OperationIds = []string{r.OperationID}
		if code == 5 {
			result.Spec.NextActions = []string{"inspect preserved receipt and protected consumption; reconcile without resend"}
		}
	}
	if *js {
		var rendered any = result
		if suite {
			rendered = sdk.SuiteResult(result, code)
		}
		if json.NewEncoder(out).Encode(rendered) != nil {
			return 1
		}
	} else {
		fmt.Fprintf(out, "%s: %s\n", cmd, status)
	}
	return code
}
