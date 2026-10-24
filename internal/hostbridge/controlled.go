package hostbridge

import (
	"context"
	"fmt"
	"time"
)

// AttemptOptions is supplied by the runtime, never decoded from task content.
type AttemptOptions struct {
	AttemptID    string
	Guard        AttemptGuard
	BeforeStart  func(context.Context, string) error
	BeforeLaunch func(context.Context, string, Launch) error
	Started      func(context.Context, string) error
}
type controlledRunner struct {
	inner   Runner
	options AttemptOptions
}

func (r controlledRunner) Start(ctx context.Context, l Launch) (Process, error) {
	if err := r.options.Guard.Check(ctx, r.options.AttemptID); err != nil {
		return nil, err
	}
	if err := r.options.BeforeStart(ctx, r.options.AttemptID); err != nil {
		return nil, err
	}
	if err := r.options.Guard.Check(ctx, r.options.AttemptID); err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if r.options.BeforeLaunch != nil {
		if err := r.options.BeforeLaunch(ctx, r.options.AttemptID, l); err != nil {
			return nil, err
		}
	}
	process, err := r.inner.Start(ctx, l)
	if err != nil {
		return nil, err
	}
	if r.options.Started != nil {
		if err = r.options.Started(ctx, r.options.AttemptID); err != nil {
			_ = process.Stop()
			_ = process.Close()
			return nil, fmt.Errorf("runtime could not durably observe host start")
		}
	}
	return process, nil
}

// VerifyControls only verifies externally issued proof scope/signatures. It does
// not create enforcement or authenticate the operator policy's trust roots.
func VerifyControls(policy Policy, at time.Time) error { return verifyControls(policy, at) }
