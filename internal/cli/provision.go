package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/evidence"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/tracker"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/worksystem/provision"
)

// ProvisionServices is installed by a protected embedding application. The
// standard binary has no provisioning credentials, grants, writer or ledger.
type ProvisionServices struct {
	Authority provision.Authority
	Boundary  provision.Boundary
	Writer    provision.Writer
}

func runProvision(args []string, out, errout io.Writer) (int, error) {
	return runProvisionServices(context.Background(), args, out, errout, ProvisionServices{})
}
func runProvisionServices(ctx context.Context, args []string, out, errout io.Writer, s ProvisionServices) (int, error) {
	if len(args) == 0 {
		return 2, fmt.Errorf("provision operation required")
	}
	op := args[0]
	keys, ok := map[string][]string{"plan": {"observation", "desired", "as-of", "out"}, "validate-plan": {"plan"}, "apply": {"plan", "change", "out"}, "reconcile": {"plan", "change", "out"}}[op]
	if !ok {
		return 2, fmt.Errorf("unknown provisioning command")
	}
	v, machine, e := selectionFlags(args[1:], keys)
	if e != nil {
		return 2, e
	}
	for _, k := range keys {
		if v["--"+k] == "" {
			return 2, fmt.Errorf("missing --%s", k)
		}
	}
	read := func(path string, target any) error {
		b, e := tracker.ReadFile(path)
		if e != nil {
			return e
		}
		return usage.Decode(b, target)
	}
	var result any
	var p provision.Plan
	status := "valid"
	protected := []string{}
	if op == "plan" {
		var o provision.Observation
		var d provision.Desired
		if e = read(v["--observation"], &o); e != nil {
			return 1, e
		}
		if e = read(v["--desired"], &d); e != nil {
			return 1, e
		}
		at, e := time.Parse(time.RFC3339Nano, v["--as-of"])
		if e != nil {
			return 2, e
		}
		p, e = provision.Build(o, d, at)
		if e != nil {
			return 1, e
		}
		result = p
		status = p.Status
		protected = []string{v["--observation"], v["--desired"]}
	} else {
		if e = read(v["--plan"], &p); e != nil {
			return 1, e
		}
		if e = provision.Validate(p); e != nil {
			return 1, e
		}
		result = p
		status = p.Status
		protected = []string{v["--plan"]}
	}
	if op == "apply" || op == "reconcile" {
		i, e := strconv.Atoi(v["--change"])
		if e != nil {
			return 2, fmt.Errorf("zero-based change index required")
		}
		if s.Authority == nil || s.Boundary == nil || s.Writer == nil {
			result = map[string]any{"version": provision.Version, "status": "blocked", "reason": "qualified provider provisioning authority, durable P7 boundary and writer are not installed", "authorizationGranted": false, "executionReady": false}
			status = "blocked"
			if e = evidence.WriteJSON(v["--out"], "report.json", result, protected...); e != nil {
				return 1, e
			}
		} else {
			reservation, e := evidence.Reserve(v["--out"], protected...)
			if e != nil {
				return 1, e
			}
			var receipt provision.Receipt
			if op == "apply" {
				receipt, e = provision.Apply(ctx, p, i, s.Authority, s.Boundary, s.Writer)
			} else {
				receipt, e = provision.Reconcile(ctx, p, i, s.Authority, s.Boundary, s.Writer)
			}
			result = receipt
			b, _ := json.MarshalIndent(receipt, "", "  ")
			if writeErr := reservation.Write(map[string][]byte{"receipt.json": append(b, '\n')}); writeErr != nil {
				return 1, writeErr
			}
			if e != nil {
				if machine {
					fmt.Fprintln(out, string(b))
				}
				return 1, e
			}
			status = receipt.Disposition
		}
	} else if op == "plan" {
		if e = evidence.WriteJSON(v["--out"], "plan.json", result, protected...); e != nil {
			return 1, e
		}
	}
	if machine {
		b, _ := json.Marshal(result)
		fmt.Fprintln(out, string(b))
	} else {
		fmt.Fprintf(out, "Work-system %s: %s\n", op, status)
	}
	if status == "blocked" {
		return 1, nil
	}
	return 0, nil
}
