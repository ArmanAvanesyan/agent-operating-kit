// Developer-only synthetic fixtures; no native session, account or transport.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	n "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/nativev1"
	api "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/v1alpha1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/sdk"
	"os"
	"path/filepath"
)

func encode(v any) []byte {
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		panic(e)
	}
	return append(b, '\n')
}
func main() {
	check := flag.Bool("check", false, "")
	flag.Parse()
	for _, host := range []string{"codex", "claude-code", "opencode", "cursor", "antigravity"} {
		mapping := api.Mapping{APIVersion: api.Version, Kind: "Mapping", Metadata: api.Metadata{Id: "native-fixture", Revision: 1}, Spec: api.MappingSpec{Fields: []api.MappingSpecFieldsItem{}, Lifecycle: []api.MappingSpecLifecycleItem{}, PersonMapping: []api.MappingSpecPersonMappingItem{}}}
		m := encode(mapping)
		a := encode(n.Assignment{Version: n.Version, Kind: "Assignment", Steps: []n.Step{{Key: "step-1", Description: "Synthetic observation fixture only", DependsOn: []string{}}}})
		operation := map[string]string{"codex": "plans.read", "claude-code": "events.observe", "opencode": "todos.read", "cursor": "projection.export", "antigravity": "projection.export"}[host]
		p := encode(api.NativeTaskProfile{APIVersion: api.Version, Kind: "NativeTaskProfile", Metadata: api.Metadata{Id: "native-profile", Revision: 1}, Spec: api.NativeTaskProfileSpec{Host: host, HostVersion: "fixture-only-1", Protocol: "fixture-" + host + "/v1", Mode: "observe", Operations: []string{operation}, MappingRef: api.Ref{Kind: "Mapping", Id: "native-fixture", Revision: 1, Sha256: api.Sha(sdk.SHA256(m))}}})
		h := n.Handle{Version: n.Version, Kind: "SessionHandle", Scope: n.Scope{Host: host, HostVersion: "fixture-only-1", Protocol: "fixture-" + host + "/v1", Deployment: "synthetic-disposable", Origin: "http://127.0.0.1:4096", Principal: "fixture-only-principal", Workspace: "/synthetic/workspace", SessionID: "synthetic-session", Owner: "fixture-only-owner", AssignmentSHA256: sdk.SHA256(a), ProfileSHA256: sdk.SHA256(p), MappingSHA256: sdk.SHA256(m), Mode: "native-only"}, ObservedAt: "2026-10-26T12:00:00Z", ExpiresAt: "2026-10-26T12:05:00Z", SchemaSHA256: sdk.NativeOpenCodeSchemaSHA256()}
		events := []n.Event{}
		event := n.Event{ID: "synthetic-event", Sequence: 0, ObservedAt: "2026-10-26T12:00:01Z", Scope: h.Scope}
		switch host {
		case "opencode":
			event.Type = "session.todo.snapshot"
			event.Data = json.RawMessage(`[{"content":"synthetic todo","status":"pending","priority":"high"}]`)
			events = append(events, event)
		case "codex":
			event.Type = "turn/plan/updated"
			event.Data = json.RawMessage(`{"threadId":"synthetic-session","turnId":"synthetic-turn","plan":[{"step":"synthetic step","status":"inProgress"}]}`)
			events = append(events, event)
		case "claude-code":
			event.Type = "tool_use"
			event.CallID = "synthetic-call"
			event.Data = json.RawMessage(`{"name":"TaskCreate","input":{"subject":"synthetic task"}}`)
			events = append(events, event)
			event.ID = "synthetic-result"
			event.Sequence = 1
			event.Type = "tool_result"
			event.Data = json.RawMessage(`{"is_error":false,"result":{"task":{"id":"synthetic-task","subject":"synthetic task"}}}`)
			events = append(events, event)
		}
		ev := encode(n.Evidence{Version: n.Version, Kind: "CapturedNativeEvidence", Scope: h.Scope, ObservedAt: "2026-10-26T12:00:01Z", Complete: len(events) > 0, Events: events})
		files := map[string][]byte{"assignment.json": a, "profile.json": p, "mapping.json": m, "handle.json": encode(h), "evidence.json": ev}
		dir := filepath.Join("kits/workkit/testdata/native", host)
		if !*check {
			if e := os.MkdirAll(dir, 0755); e != nil {
				panic(e)
			}
		}
		for name, b := range files {
			path := filepath.Join(dir, name)
			if *check {
				actual, e := os.ReadFile(path)
				if e != nil || !bytes.Equal(actual, b) {
					panic("native fixture drift: " + path)
				}
			} else {
				if e := os.WriteFile(path, b, 0644); e != nil {
					panic(e)
				}
			}
		}
	}
	fmt.Println("WorkKit native synthetic fixture generation/check passed")
}
