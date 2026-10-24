package provision

import (
	"context"
	"fmt"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/operations"
)

// ActivatedBoundary adds P9's external anti-rollback witness to the P7 journal.
// A crash between the two commits consumes the send rather than retrying it.
type ActivatedBoundary struct {
	Journal     Boundary
	Activation  *operations.Activation
	RouteSHA256 string
}

func (b ActivatedBoundary) Consume(ctx context.Context, id, digest string) error {
	if b.Journal == nil || b.Activation == nil || b.RouteSHA256 == "" || b.Activation.ScopeSHA256() != b.RouteSHA256 {
		return fmt.Errorf("qualified exact-route provisioning activation required")
	}
	if e := b.Activation.Consume(ctx, id, "provisioning-send", digest); e != nil {
		return e
	}
	return b.Journal.Consume(ctx, id, digest)
}
func (b ActivatedBoundary) Record(ctx context.Context, id, state string) error {
	if b.Journal == nil {
		return fmt.Errorf("journal required")
	}
	return b.Journal.Record(ctx, id, state)
}
