package hostbridge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage/collectors"
)

func usageFixture(t *testing.T, host, version, auth string) (Policy, usage.Policy) {
	t.Helper()
	root := t.TempDir()
	b := usage.Binding{ID: "binding-a", PoolID: "pool-a", Provider: "anthropic", Service: host, Host: host, HostVersion: version, AuthMode: auth, SourceID: "source-a", StreamID: "stream-a", ConfigurationIDs: []string{}, ConstraintIDs: []string{}}
	up := usage.Policy{Version: usage.Version, Sources: []usage.SourceRule{{ID: "source-a", Host: host, HostVersion: version, AdapterVersion: collectors.AdapterVersion, AuthModes: []string{auth}, Metrics: []string{"input-tokens", "output-tokens", "cached-input-tokens", "cache-creation-tokens", "reasoning-tokens", "currency"}, Authority: "fixture", MaxAgeSeconds: 300, CollectionAllowed: true}}, SafetyMargins: map[string]json.Number{}}
	bb, _ := json.Marshal(b)
	pb, _ := json.Marshal(up)
	if err := os.WriteFile(filepath.Join(root, "binding.json"), bb, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "usage-policy.json"), pb, 0600); err != nil {
		t.Fatal(err)
	}
	return Policy{Host: host, HostVersion: version, AuthMode: auth, UsageBinding: &Asset{Path: "binding.json", SHA256: Hash(bb)}, UsagePolicy: &Asset{Path: "usage-policy.json", SHA256: Hash(pb)}, root: root}, up
}

func TestExportUsageRequiresExplicitBindings(t *testing.T) {
	e, r, err := ExportUsage(Prepared{Host: "claude-code"}, Policy{}, Transcript{}, time.Now())
	if err != nil || r.Status != "unknown" || len(e.Events) != 0 || len(e.Bindings) != 0 {
		t.Fatalf("unexpected unbound usage result: %+v %+v %v", e, r, err)
	}
}

func TestExportUsageClaudeExactFramesAndCumulativeResumeCost(t *testing.T) {
	version, auth := "2.1.303", "native-login"
	policy, _ := usageFixture(t, "claude-code", version, auth)
	p := Prepared{Host: "claude-code", HostVersion: version, ConfigurationID: "config-a", Model: "claude-opus", Workspace: "/tmp"}
	at := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	frame := json.RawMessage(`{"input_tokens":12,"output_tokens":7,"cache_read_input_tokens":3,"cache_creation_input_tokens":2,"total_cost_usd":0.25}`)
	tx := Transcript{SessionID: "session-a", TurnID: "turn-a", UsageFrames: []json.RawMessage{frame}}
	e, r, err := ExportUsage(p, policy, tx, at)
	if err != nil || r.Status != "valid" || len(e.Events) != 5 {
		t.Fatalf("unexpected usage export: events=%d report=%+v err=%v", len(e.Events), r, err)
	}
	if err := usage.ValidateExport(e, func() usage.Policy {
		var x usage.Policy
		raw, _ := os.ReadFile(filepath.Join(policy.root, policy.UsagePolicy.Path))
		_ = usage.Decode(raw, &x)
		return x
	}()); err != nil {
		t.Fatalf("export violates usage contract: %v", err)
	}
	for _, event := range e.Events {
		if event.Metric == "currency" {
			if event.Semantics != "cumulative" || event.CounterEpoch != "session-a" || event.Unit != "USD" {
				t.Fatalf("session cost is not cumulative: %+v", event)
			}
			continue
		}
		if event.Semantics != "snapshot" || event.Unit != "tokens" || event.Billable {
			t.Fatalf("token frame scope was overstated: %+v", event)
		}
	}
	resumed := tx
	resumed.TurnID = "turn-b"
	resumed.UsageFrames = []json.RawMessage{json.RawMessage(`{"total_cost_usd":0.40}`)}
	e2, _, err := ExportUsage(p, policy, resumed, at.Add(time.Minute))
	if err != nil || len(e2.Events) != 1 {
		t.Fatalf("unexpected resumed cost export: %+v %v", e2, err)
	}
	if e2.Events[0].Semantics != "cumulative" || e2.Events[0].CounterEpoch != e.Events[4].CounterEpoch {
		t.Fatalf("resume cost would be summed as a new increment: %+v", e2.Events[0])
	}
}

func TestExportUsageCodexKeepsCumulativeThreadTotals(t *testing.T) {
	version, auth := "protocol-v2-fixture", "chatgpt"
	policy, _ := usageFixture(t, "codex", version, auth)
	p := Prepared{Host: "codex", HostVersion: version}
	tx := Transcript{UsageFrames: []json.RawMessage{json.RawMessage(`{"method":"thread/tokenUsage/updated","params":{"threadId":"thread-a","turnId":"turn-a","tokenUsage":{"total":{"inputTokens":20,"cachedInputTokens":4,"outputTokens":6,"reasoningOutputTokens":2},"last":{"inputTokens":8,"outputTokens":3}}}}`)}}
	e, r, err := ExportUsage(p, policy, tx, time.Now().UTC())
	if err != nil || r.Status != "valid" || len(e.Events) != 4 {
		t.Fatalf("unexpected Codex usage result: %+v %+v %v", e, r, err)
	}
	for _, event := range e.Events {
		if event.Semantics != "cumulative" || event.Sequence != 0 {
			t.Fatalf("Codex thread total semantics changed: %+v", event)
		}
	}
}
