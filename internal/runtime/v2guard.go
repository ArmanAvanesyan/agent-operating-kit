package runtime

import (
	"context"
	"fmt"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/hostbridge"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/runtime/records"
)

type V2LaunchAdmission func(context.Context, records.Run, records.StepSpec, records.Step, hostbridge.Prepared, hostbridge.Policy, hostbridge.Launch) error
type v2LaunchAdmissionKey struct{}

// WithV2LaunchAdmission carries a protected v2 qualification check into the
// existing P7 launch boundary. The callback is evaluated after durable launch
// intent and immediately before the shared Runner.Start call.
func WithV2LaunchAdmission(ctx context.Context, check V2LaunchAdmission) (context.Context, error) {
	if ctx == nil || check == nil {
		return nil, fmt.Errorf("v2 launch admission callback required")
	}
	return context.WithValue(ctx, v2LaunchAdmissionKey{}, check), nil
}

func v2LaunchAdmission(ctx context.Context) V2LaunchAdmission {
	check, _ := ctx.Value(v2LaunchAdmissionKey{}).(V2LaunchAdmission)
	return check
}
