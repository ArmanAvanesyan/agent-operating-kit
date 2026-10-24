package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/evidence"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/hostbridge"
	"io"
	"os"
	"os/signal"
	"time"
)

const hostHelp = `aok host list [--json]
New driver offline operations require --host HOST --as-of RFC3339 and v2 records.
aok host isolation-check --policy FILE --as-of RFC3339 --out NEW [--json]
aok host inspect --host codex|claude-code --policy FILE --out NEW [--json]
aok host prepare --assignment FILE --root DIR --policy FILE --out NEW [--json]
aok host run --prepared BUNDLE --policy FILE --out NEW [--json]
aok host resume --attempt BUNDLE --prepared BUNDLE --policy FILE --out NEW [--json]
aok host replay --manifest FILE --out NEW [--json]
aok host validate BUNDLE [--json]
aok host tasks plan --attempt BUNDLE --plan FILE --out NEW [--json]
aok host qualify --host codex|claude-code --policy FILE --out NEW [--json]
Execution is explicit and foreground. Host completion is not AOK acceptance.`

func runHost(args []string, out, errout io.Writer) (int, error) {
	code, e := hostCommand(args, out)
	if e != nil {
		fmt.Fprintln(errout, "Error:", e)
		if code == 0 {
			code = 1
		}
	}
	return code, nil
}
func hostCommand(args []string, out io.Writer) (int, error) {
	if len(args) > 0 && args[0] == "isolation-check" {
		return hostIsolationCheck(args[1:], out)
	}
	if len(args) > 0 && args[0] == "list" {
		return runHostList(args[1:], out)
	}
	newHost := false
	policyPath := ""
	for i, a := range args {
		if i+1 < len(args) {
			if a == "--host" && args[i+1] != "codex" && args[i+1] != "claude-code" {
				newHost = true
			}
			if a == "--policy" {
				policyPath = args[i+1]
			}
		}
	}
	if newHost {
		if policyPath != "" && len(args) > 0 && (args[0] == "inspect" || args[0] == "prepare") {
			raw, e := hostbridge.ReadFile(policyPath)
			if e != nil {
				return 1, e
			}
			var head struct {
				Version string          `json:"version"`
				Policy  json.RawMessage `json:"policy"`
			}
			if e = json.Unmarshal(raw, &head); e != nil {
				return 1, e
			}
			if head.Version == hostbridge.VersionV2 && len(head.Policy) == 0 {
				clean := []string{}
				asOf := ""
				for i := 0; i < len(args); i++ {
					if args[i] == "--as-of" && i+1 < len(args) {
						i++
						asOf = args[i]
						continue
					}
					if args[0] == "prepare" && args[i] == "--host" && i+1 < len(args) {
						i++
						continue
					}
					clean = append(clean, args[i])
				}
				if _, e = time.Parse(time.RFC3339Nano, asOf); e != nil {
					return 2, fmt.Errorf("new-host offline operations require --as-of RFC3339")
				}
				return hostCommandBase(clean, out)
			}
		}
		return runHostV2(args, out)
	}
	return hostCommandBase(args, out)
}
func hostCommandBase(args []string, out io.Writer) (int, error) {
	if len(args) == 0 {
		fmt.Fprintln(out, hostHelp)
		return 2, nil
	}
	if args[0] == "help" || args[0] == "--help" || len(args) > 1 && args[1] == "--help" {
		fmt.Fprintln(out, hostHelp)
		return 0, nil
	}
	op := args[0]
	args = args[1:]
	if op == "tasks" {
		if len(args) == 0 || args[0] != "plan" {
			return 2, fmt.Errorf("host tasks requires plan")
		}
		op = "tasks-plan"
		args = args[1:]
	}
	allowed := map[string][]string{"inspect": {"host", "policy", "out"}, "prepare": {"assignment", "root", "policy", "out"}, "run": {"prepared", "policy", "out"}, "resume": {"attempt", "prepared", "policy", "out"}, "replay": {"manifest", "out"}, "validate": {}, "tasks-plan": {"attempt", "plan", "out"}, "qualify": {"host", "policy", "out"}}
	keys, ok := allowed[op]
	if !ok {
		return 2, fmt.Errorf("unknown host operation")
	}
	pos := ""
	if op == "validate" {
		if len(args) < 1 {
			return 2, fmt.Errorf("bundle required")
		}
		pos = args[0]
		args = args[1:]
	}
	values, machine, e := selectionFlags(args, keys)
	if e != nil {
		return 2, e
	}
	required := map[string][]string{"inspect": {"host", "policy", "out"}, "prepare": {"assignment", "root", "policy", "out"}, "run": {"prepared", "policy", "out"}, "resume": {"attempt", "prepared", "policy", "out"}, "replay": {"manifest", "out"}, "tasks-plan": {"attempt", "plan", "out"}, "qualify": {"host", "policy", "out"}}
	for _, key := range required[op] {
		if values["--"+key] == "" {
			return 2, fmt.Errorf("missing --%s", key)
		}
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	var result any
	files := map[string][]byte{}
	status := "valid"
	protected := []string{pos}
	for k, v := range values {
		if k != "--out" && k != "--host" {
			protected = append(protected, v)
		}
	}
	var policy hostbridge.Policy
	if values["--policy"] != "" {
		policy, e = hostbridge.ReadPolicy(values["--policy"])
		if e != nil {
			return 1, e
		}
	}
	var reservation *evidence.Reservation
	if op == "run" || op == "resume" || op == "qualify" {
		protected = append(protected, policy.Workspace)
		reservation, e = evidence.Reserve(values["--out"], protected...)
		if e != nil {
			return 1, e
		}
	}
	switch op {
	case "inspect":
		if policy.Host != values["--host"] {
			return 1, fmt.Errorf("policy host mismatch")
		}
		result, e = hostbridge.Preflight(ctx, policy, hostbridge.NativeRunner{})
		if e == nil {
			raw, _ := json.MarshalIndent(result, "", "  ")
			files["capabilities.json"] = append(raw, '\n')
		}
	case "prepare":
		var b hostbridge.Bundle
		b, e = hostbridge.Prepare(values["--assignment"], values["--root"], policy)
		if e == nil {
			files = b.Files
			result = b.Prepared
		}
	case "run", "resume":
		var p hostbridge.Prepared
		p, e = hostbridge.LoadPrepared(values["--prepared"])
		if e != nil {
			return 1, e
		}
		var prev *hostbridge.Receipt
		if op == "resume" {
			r, err := hostbridge.LoadReceipt(values["--attempt"])
			if err != nil {
				return 1, err
			}
			prev = &r
		}
		var b hostbridge.Bundle
		b, e = hostbridge.Run(ctx, p, policy, hostbridge.NativeRunner{}, prev, nil)
		if e == nil {
			for _, a := range p.Sources {
				raw, err := hostbridge.ReadAsset(values["--prepared"], a.Path)
				if err != nil {
					return 1, err
				}
				b.Files[a.Path] = raw
			}
			files = b.Files
			result = b.Receipt
			status = b.Receipt.Report.Status
		}
	case "replay":
		var b hostbridge.Bundle
		b, e = hostbridge.Replay(values["--manifest"])
		if e == nil {
			files = b.Files
			result = b.Receipt
			status = b.Receipt.Report.Status
		}
	case "validate":
		r, err := hostbridge.ValidateBundle(pos)
		e = err
		result = r
		status = r.Status
	case "tasks-plan":
		r, err := hostbridge.LoadReceipt(values["--attempt"])
		if err != nil {
			return 1, err
		}
		p, err := hostbridge.PlanTasks(r, values["--plan"])
		e = err
		result = p
		status = p.Report.Status
		raw, _ := json.MarshalIndent(p, "", "  ")
		files["task-projection.json"] = append(raw, '\n')
	case "qualify":
		if policy.Host != values["--host"] {
			return 1, fmt.Errorf("policy host mismatch")
		}
		var b hostbridge.Bundle
		b, e = hostbridge.Qualify(ctx, policy, hostbridge.NativeRunner{})
		if e == nil {
			files = b.Files
			result = b.Receipt
			status = b.Receipt.Report.Status
		}
	}
	if e != nil {
		return 1, e
	}
	if len(files) > 0 {
		if reservation != nil {
			e = reservation.Write(files)
		} else {
			e = evidence.WriteFiles(values["--out"], files, protected...)
		}
		if e != nil {
			return 1, e
		}
	}
	if machine {
		raw, _ := json.MarshalIndent(result, "", "  ")
		fmt.Fprintln(out, string(raw))
	} else {
		fmt.Fprintf(out, "Host %s: %s\n", op, status)
	}
	if status != "valid" {
		return 1, nil
	}
	return 0, nil
}
