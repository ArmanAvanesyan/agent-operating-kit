package selection

import (
	"fmt"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
	"path/filepath"
)

// A route can supply its own already-authored assignment. Only its explicit
// execution profile and metadata may differ; the selector never edits either.
func routeAssignment(root string, c Candidate, task TaskProfile, base contracts.AgentAssignment) (contracts.AgentAssignment, string, error) {
	if c.Assignment == nil {
		return base, task.Assignment.SHA256, nil
	}
	b, e := asset(root, *c.Assignment)
	if e != nil {
		return base, "", e
	}
	r, _, e := contracts.Parse(b)
	if e != nil {
		return base, "", e
	}
	resource, ok := r.(contracts.Resource)
	if !ok {
		return base, "", fmt.Errorf("route assignment required")
	}
	typed, e := contracts.DecodeTyped(resource)
	if e != nil {
		return base, "", e
	}
	route, ok := typed.(contracts.AgentAssignment)
	if !ok {
		return base, "", fmt.Errorf("route assignment kind required")
	}
	packet, e := asset(filepath.Dir(filepath.Join(root, c.Assignment.Path)), Asset{Path: route.TaskPacket, SHA256: task.Packet.SHA256})
	if e != nil || usage.Hash(packet) != task.Packet.SHA256 {
		return base, "", fmt.Errorf("route assignment packet differs from task")
	}
	if usage.Digest(route.Agent) != usage.Digest(base.Agent) || usage.Digest(route.Inputs) != usage.Digest(base.Inputs) || usage.Digest(route.Outputs) != usage.Digest(base.Outputs) || usage.Digest(route.PermissionRestrictions) != usage.Digest(base.PermissionRestrictions) {
		return base, "", fmt.Errorf("route assignment changes task inputs, agent, outputs or permission restrictions")
	}
	return route, c.Assignment.SHA256, nil
}
