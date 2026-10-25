package sdk

import (
	"encoding/json"
	api "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/v1alpha1"
	"os"
	"path/filepath"
	"testing"
)

func TestAllTypedContractsRoundTrip(t *testing.T) {
	types := map[string]func() any{
		"AuthBinding":           func() any { return &api.AuthBinding{} },
		"CapabilityReport":      func() any { return &api.CapabilityReport{} },
		"CaptureManifest":       func() any { return &api.CaptureManifest{} },
		"CommandResult":         func() any { return &api.CommandResult{} },
		"ConnectionProfile":     func() any { return &api.ConnectionProfile{} },
		"DesiredSetup":          func() any { return &api.DesiredSetup{} },
		"InstallPlan":           func() any { return &api.InstallPlan{} },
		"Mapping":               func() any { return &api.Mapping{} },
		"MigrationManifest":     func() any { return &api.MigrationManifest{} },
		"NativeTaskBinding":     func() any { return &api.NativeTaskBinding{} },
		"NativeTaskObservation": func() any { return &api.NativeTaskObservation{} },
		"NativeTaskProfile":     func() any { return &api.NativeTaskProfile{} },
		"OperationPlan":         func() any { return &api.OperationPlan{} },
		"OperationReceipt":      func() any { return &api.OperationReceipt{} },
		"SetupPlan":             func() any { return &api.SetupPlan{} },
		"SyncCheckpoint":        func() any { return &api.SyncCheckpoint{} },
		"SyncConflict":          func() any { return &api.SyncConflict{} },
		"SyncRoute":             func() any { return &api.SyncRoute{} },
		"ToolchainLock":         func() any { return &api.ToolchainLock{} },
		"ToolchainPolicy":       func() any { return &api.ToolchainPolicy{} },
		"ViewDefinition":        func() any { return &api.ViewDefinition{} },
		"WorkBinding":           func() any { return &api.WorkBinding{} },
		"WorkItem":              func() any { return &api.WorkItem{} },
		"Workspace":             func() any { return &api.Workspace{} },
	}
	paths, _ := filepath.Glob("../testdata/design/*.json")
	seen := map[string]bool{}
	for _, p := range paths {
		b, _ := os.ReadFile(p)
		d, e := Parse(b, "json")
		if e != nil {
			t.Fatal(e)
		}
		factory := types[d.Kind()]
		if factory == nil {
			t.Fatal("missing typed resource", d.Kind())
		}
		target := factory()
		if e = d.Decode(target); e != nil {
			t.Fatal(e)
		}
		round, e := json.Marshal(target)
		if e != nil {
			t.Fatal(e)
		}
		again, e := Parse(round, "json")
		if e != nil {
			t.Fatalf("%s typed roundtrip: %v", d.Kind(), e)
		}
		if again.ContentSHA256() != d.ContentSHA256() {
			t.Fatal("typed roundtrip lost content", d.Kind())
		}
		seen[d.Kind()] = true
	}
	if len(seen) != 24 {
		t.Fatal("typed coverage", len(seen))
	}
}
