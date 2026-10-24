package hostbridge

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

// Validate emitted requests rather than just the existence of schema fields.
// These fixtures never invoke a native host or establish control enforcement.
func TestCodexSandboxRequestsConformToPinnedSchemas(t *testing.T) {
	for _, write := range []bool{false, true} {
		for _, resume := range []bool{false, true} {
			root := t.TempDir()
			perms := contracts.Permissions{Filesystem: contracts.FilesystemPermissions{Read: []string{root}}}
			if write {
				perms.Filesystem.Write = []string{filepath.Join(root, "pilot-output.txt")}
			}
			policy := signedCodexSandboxPolicy(t, root, perms, time.Now().UTC(), write)
			var previous *Receipt
			if resume {
				previous = &Receipt{ThreadID: "thread-x", SessionID: "session-x", HostOutcome: "interrupted"}
			}
			runner := &sandboxCaptureRunner{}
			if _, err := CodexRun(context.Background(), sandboxPrepared(root), policy, runner, previous, nil); err != nil {
				t.Fatal(err)
			}
			checked := 0
			for _, raw := range runner.sent {
				var req struct {
					Method string `json:"method"`
					Params any    `json:"params"`
				}
				if err := json.Unmarshal(raw, &req); err != nil {
					t.Fatal(err)
				}
				name := ""
				switch req.Method {
				case "thread/start":
					name = "ThreadStartParams"
				case "thread/resume":
					name = "ThreadResumeParams"
				case "turn/start":
					name = "TurnStartParams"
				}
				if name == "" {
					continue
				}
				b, err := os.ReadFile(filepath.Join("testdata", "codex-0.162.1-schema", name+".json"))
				if err != nil {
					t.Fatal(err)
				}
				var schema any
				if err = json.Unmarshal(b, &schema); err != nil {
					t.Fatal(err)
				}
				c := jsonschema.NewCompiler()
				c.UseLoader(denyLoader{})
				uri := "urn:aok:codex-pinned:" + name
				if err = c.AddResource(uri, schema); err != nil {
					t.Fatal(err)
				}
				compiled, err := c.Compile(uri)
				if err != nil {
					t.Fatal(err)
				}
				if err = compiled.Validate(req.Params); err != nil {
					t.Fatalf("write=%v resume=%v %s: %v", write, resume, name, err)
				}
				checked++
			}
			if checked != 2 {
				t.Fatalf("validated %d requests, want thread and turn", checked)
			}
		}
	}
}
