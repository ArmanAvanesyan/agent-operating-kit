package runtime

import (
	"context"
	"fmt"
	"time"
)

// MethodologyAuthority is protected host configuration. Implementations bind
// run/step/input revisions to a current P7 lifecycle projection, protected roles
// and fresh independently verified artifacts. Task configuration grants nothing.
type MethodologyAuthority interface {
	MethodologyBindingSHA256() string
	CheckMethodology(context.Context, string, string, string, time.Time) error
}

func (c *Controller) ConfigureMethodologyAuthority(a MethodologyAuthority) error {
	if a == nil || len(a.MethodologyBindingSHA256()) != 64 {
		return fmt.Errorf("protected methodology authority required")
	}
	c.methodologyAuthority = a
	return nil
}
func (c *Controller) HasMethodologyAuthority() bool { return c.methodologyAuthority != nil }
func (c *Controller) CheckMethodology(ctx context.Context, runID, stepID, milestone string) error {
	if c.methodologyAuthority == nil {
		return nil
	}
	return c.methodologyAuthority.CheckMethodology(ctx, runID, stepID, milestone, c.now())
}

func (c *Controller) HasMethodologyBinding(digest string) bool {
	return c.methodologyAuthority != nil && c.methodologyAuthority.MethodologyBindingSHA256() == digest
}
