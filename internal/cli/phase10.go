package cli

import (
	"context"
	"fmt"
	readinessv2 "github.com/ArmanAvanesyan/agent-operating-kit/internal/readiness/v2"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/worksystem"
	"io"
)

// Phase10Services are installed only by a protected embedding application.
// The shipped binary supplies none; records cannot populate service interfaces.
type Phase10Services struct {
	ReadinessAuthority  readinessv2.PolicyAuthority
	ReadinessValidators map[string]readinessv2.Validator
	Phase9              Phase9Services
	Methodology         *MethodologyLifecycleServices
	Provision           ProvisionServices
	WorkSystemReader    worksystem.Reader
}

func RunWithPhase10Services(ctx context.Context, args []string, out, errout io.Writer, s Phase10Services) int {
	if out == nil {
		out = io.Discard
	}
	if errout == nil {
		errout = io.Discard
	}
	if ctx == nil {
		fmt.Fprintln(errout, "cancellable context required")
		return 1
	}
	if len(args) == 0 {
		return Run(args, out, errout)
	}
	var code int
	var err error
	switch args[0] {
	case "readiness":
		if handled, c, e := readinessV2(args[1:], out, errout, s.ReadinessAuthority, s.ReadinessValidators); handled {
			code, err = c, e
		} else {
			return RunWithPhase9Services(args, out, errout, s.Phase9)
		}
	case "methodology":
		code, err = runMethodologyWithServices(ctx, args[1:], s.Methodology, out, errout)
	case "work-system":
		if len(args) > 1 && (args[1] == "plan" || args[1] == "validate-plan" || args[1] == "apply" || args[1] == "reconcile") {
			code, err = runProvisionServices(ctx, args[1:], out, errout, s.Provision)
		} else {
			code, err = runWorkSystemWithReader(ctx, args[1:], s.WorkSystemReader, out, errout)
		}
	default:
		return RunWithPhase9Services(args, out, errout, s.Phase9)
	}
	if err != nil {
		fmt.Fprintln(errout, "Error:", err)
		if code == 0 {
			code = 1
		}
	}
	return code
}
