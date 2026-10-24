package selection

import (
	"encoding/json"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
	"os"
	"path/filepath"
	"testing"
)

func TestExplicitRouteAssignmentPreservesTaskAndRestrictions(t *testing.T) {
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile("../contracts/testdata/assignment.json")
	if e != nil {
		t.Fatal(e)
	}
	r, _, e := contracts.Parse(b)
	if e != nil {
		t.Fatal(e)
	}
	typed, e := contracts.DecodeTyped(r.(contracts.Resource))
	if e != nil {
		t.Fatal(e)
	}
	base := typed.(contracts.AgentAssignment)
	packet := []byte(`{"id":"same-task"}`)
	if e = os.WriteFile(filepath.Join(root, "packet.json"), packet, 0600); e != nil {
		t.Fatal(e)
	}
	task := TaskProfile{Packet: Asset{SHA256: usage.Hash(packet)}}
	route := base
	route.TaskPacket = "packet.json"
	route.ExecutionProfile.ID = "alternative-profile"
	route.Metadata.ID = "alternative-assignment"
	save := func() Candidate {
		t.Helper()
		data, _ := json.Marshal(route)
		if e = os.WriteFile(filepath.Join(root, "assignment.json"), data, 0600); e != nil {
			t.Fatal(e)
		}
		return Candidate{Assignment: &Asset{Path: "assignment.json", SHA256: usage.Hash(data)}}
	}
	c := save()
	bound, _, e := routeAssignment(root, c, task, base)
	if e != nil || bound.ExecutionProfile.ID != "alternative-profile" {
		t.Fatalf("explicit boundroute failed: %v", e)
	}
	route.Agent.ID = "different-agent"
	c = save()
	if _, _, e = routeAssignment(root, c, task, base); e == nil {
		t.Fatal("route replaced baseline agent")
	}
	route = base
	route.TaskPacket = "packet.json"
	route.Inputs = contracts.ParameterValues{"new": json.RawMessage(`true`)}
	c = save()
	if _, _, e = routeAssignment(root, c, task, base); e == nil {
		t.Fatal("route changed task inputs")
	}
}
