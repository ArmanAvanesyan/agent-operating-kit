//go:build !darwin && !linux

package sdk

import (
	"context"
	i "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/integrationv1"
)

func (s *Service) ServeIntegration(context.Context, string, string, string, string) (i.Report, error) {
	return i.Report{}, operationFailure("integration.ipc-unsupported", "native private IPC is not qualified on this platform", 3, nil)
}
func ForwardIntegration(context.Context, string, i.Command) (i.Report, error) {
	return i.Report{}, operationFailure("integration.ipc-unsupported", "native private IPC is not qualified on this platform", 3, nil)
}
