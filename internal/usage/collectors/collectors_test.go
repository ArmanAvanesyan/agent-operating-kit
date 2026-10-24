package collectors

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
)

func TestDescribeCoversAllHostsAndKeepsLiveGatePending(t *testing.T) {
	ds := Describe()
	want := []string{"codex", "claude-code", "cursor", "opencode", "gemini", "agy", "openclaw", "hermes-agent"}
	if len(ds) != len(want) {
		t.Fatalf("got %d descriptors", len(ds))
	}
	for i, d := range ds {
		if d.Host != want[i] {
			t.Fatalf("descriptor %d host %q, want %q", i, d.Host, want[i])
		}
		if d.LiveGate != "pending" {
			t.Fatalf("%s live gate %q", d.Host, d.LiveGate)
		}
	}
}

func TestCodexCumulativeCountersAndRateLimitWindows(t *testing.T) {
	data, err := os.ReadFile("testdata/codex-app-server-v2.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	p := testPolicy("codex", "protocol-v2-fixture", "chatgpt", "app-server-fixture", "input-tokens", "cached-input-tokens", "output-tokens", "reasoning-tokens", "percentage")
	b := usage.Binding{ID: "binding-codex", PoolID: "explicit-pool-a", Provider: "openai", Service: "codex-app", Host: "codex", HostVersion: "protocol-v2-fixture", AuthMode: "chatgpt", SourceID: "app-server-fixture", StreamID: "stream-fixture"}
	ex, r := Normalize("codex", data, b, p, time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC))
	if r.Status != "valid" {
		t.Fatalf("status %s diagnostics=%+v", r.Status, r.Diagnostics)
	}
	if len(ex.Events) != 4 {
		t.Fatalf("got %d events, want four deduplicated cumulative component counters", len(ex.Events))
	}
	if len(ex.Quotas) != 4 {
		t.Fatalf("got %d quotas, want three buckets/windows plus update", len(ex.Quotas))
	}
	if err := usage.ValidateExport(ex, p); err != nil {
		t.Fatalf("fixture export fails contract validation: %v; quotas=%+v bindings=%+v", err, ex.Quotas, ex.Bindings)
	}
	for _, e := range ex.Events {
		if e.Semantics != "cumulative" || !e.Billable {
			t.Fatalf("unexpected token semantics: %+v", e)
		}
		if e.Sequence != 0 {
			t.Fatalf("invented source sequence: %+v", e)
		}
	}
	var over *usage.Quota
	for i := range ex.Quotas {
		if ex.Quotas[i].Used != nil && string(*ex.Quotas[i].Used) == "105.25" {
			over = &ex.Quotas[i]
		}
	}
	if over == nil || string(*over.Remaining) != "-5.25" {
		t.Fatalf("over-limit percentage was normalized: %+v", over)
	}
	for _, q := range ex.Quotas {
		if !q.Origin.FreshnessKnown || q.Origin.ObservedAt == "" {
			t.Fatalf("Codex read/update should carry observed marker: %+v", q.Origin)
		}
		if q.Generation == "unknown" && q.Completeness != "sparse" {
			t.Fatalf("incomplete source window marked full: %+v", q)
		}
	}
	full, sparse := 0, 0
	for _, q := range ex.Quotas {
		if q.Completeness == "full" {
			full++
		} else {
			sparse++
		}
	}
	if full != 3 || sparse != 1 {
		t.Fatalf("read reply completeness was not distinguished from update: full=%d sparse=%d", full, sparse)
	}
}

func TestClaudeStatuslineRedrawAndSparseQuotaAreNotFresh(t *testing.T) {
	data, err := os.ReadFile("testdata/claude-statusline-2.1.284.json")
	if err != nil {
		t.Fatal(err)
	}
	p := testPolicy("claude-code", "2.1.284", "claudeai", "statusline-fixture", "input-tokens", "output-tokens", "cache-creation-tokens", "cached-input-tokens", "currency", "percentage")
	b := usage.Binding{ID: "binding-claude", PoolID: "explicit-pool-a", Provider: "anthropic", Service: "claude-code", Host: "claude-code", HostVersion: "2.1.284", AuthMode: "claudeai", SourceID: "statusline-fixture", StreamID: "stream-fixture"}
	at := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	ex1, r1 := Normalize("claude-code", data, b, p, at)
	if r1.Status != "valid" {
		t.Fatalf("status %s diagnostics=%+v", r1.Status, r1.Diagnostics)
	}
	ex2, _ := Normalize("claude-code", data, b, p, at.Add(time.Minute))
	if len(ex1.Events) != 1 {
		t.Fatalf("got %d events, expected only estimated session cost", len(ex1.Events))
	}
	if len(ex1.Quotas) != 4 || len(ex2.Quotas) != 4 {
		t.Fatalf("unexpected sparse quotas: %d / %d", len(ex1.Quotas), len(ex2.Quotas))
	}
	for _, q := range ex2.Quotas {
		if q.Origin.FreshnessKnown || q.Origin.ObservedAt == "" || q.Origin.ReceivedAt == "" {
			t.Fatalf("redraw incorrectly renewed quota: %+v", q.Origin)
		}
	}
	if ex1.Quotas[0].ID != ex2.Quotas[0].ID {
		t.Fatal("Claude redraw changed deterministic quota identity")
	}
	if ex1.Quotas[0].ID != ex2.Quotas[0].ID {
		t.Fatal("identical redraw changed quota identity")
	}
	var estimated *usage.Quota
	for i := range ex1.Quotas {
		if ex1.Quotas[i].Precision == "estimated-gateway-spend" {
			estimated = &ex1.Quotas[i]
		}
	}
	if estimated == nil || estimated.Remaining == nil || string(*estimated.Remaining) != "185.88" || estimated.Origin.FreshnessKnown {
		t.Fatalf("gateway USD estimate was merged or marked fresh: %+v", estimated)
	}
	if err := usage.ValidateExport(ex1, p); err != nil {
		t.Fatalf("fixture export fails contract validation: %v; events=%+v", err, ex1.Events)
	}
	for _, e := range ex1.Events {
		if e.Metric == "currency" && e.Billable {
			t.Fatal("estimated client-side cost marked billable")
		}
		if (e.Metric == "input-tokens" || e.Metric == "output-tokens" || e.Metric == "cache-creation-tokens" || e.Metric == "cached-input-tokens") && e.Billable {
			t.Fatal("context snapshot marked billable")
		}
	}
}

func TestMissingAndSparseFieldsDoNotCreateOrRenewEvidence(t *testing.T) {
	p := testPolicy("codex", "protocol-v2-fixture", "chatgpt", "fixture", "percentage")
	b := usage.Binding{ID: "b", PoolID: "pool", Provider: "openai", Service: "codex", Host: "codex", HostVersion: "protocol-v2-fixture", AuthMode: "chatgpt", SourceID: "fixture", StreamID: "stream-fixture"}
	at := time.Now().UTC()
	missing, report := Normalize("codex", []byte(`{"id":3,"result":{"rateLimits":null}}`), b, p, at)
	if len(missing.Quotas) != 0 || report.Status != "unknown" {
		t.Fatalf("missing fields became evidence: %+v %+v", missing, report)
	}
	sparse, report := Normalize("codex", []byte(`{"method":"account/rateLimits/updated","params":{"rateLimits":{"limitId":"codex","primary":{"usedPercent":30,"resetsAt":1792700000},"secondary":null}}}`), b, p, at)
	if report.Status != "valid" || len(sparse.Quotas) != 1 {
		t.Fatalf("sparse update not represented accurately: %+v %+v", sparse, report)
	}
	claudePolicy := testPolicy("claude-code", "2.1.284", "claudeai", "fixture", "input-tokens", "output-tokens", "cache-creation-tokens", "cached-input-tokens", "currency", "percentage")
	claudeBinding := usage.Binding{ID: "claude-binding", PoolID: "pool", Provider: "anthropic", Service: "claude", Host: "claude-code", HostVersion: "2.1.284", AuthMode: "claudeai", SourceID: "fixture", StreamID: "claude-stream"}
	claude, claudeReport := Normalize("claude-code", []byte(`{"version":"2.1.284","session_id":"sparse-session","model":{"id":"claude-opus-5-5"},"context_window":{"current_usage":null}}`), claudeBinding, claudePolicy, at)
	if len(claude.Events) != 0 || len(claude.Quotas) != 0 || claudeReport.Status != "unknown" {
		t.Fatalf("missing Claude fields became zero-valued evidence: %+v %+v", claude, claudeReport)
	}
}

func TestCodexAuthModeChangeInvalidatesBinding(t *testing.T) {
	p := testPolicy("codex", "protocol-v2-fixture", "chatgpt", "fixture", "percentage")
	b := usage.Binding{ID: "b", PoolID: "pool", Provider: "openai", Service: "codex", Host: "codex", HostVersion: "protocol-v2-fixture", AuthMode: "chatgpt", SourceID: "fixture", StreamID: "stream-fixture"}
	input := []byte(`{"method":"account/updated","params":{"authMode":"apiKey","planType":null}}
{"method":"account/rateLimits/updated","params":{"rateLimits":{"limitId":"codex","primary":{"usedPercent":20,"resetsAt":1792700000}}}}`)
	ex, report := Normalize("codex", input, b, p, time.Now().UTC())
	if len(ex.Quotas) != 0 || report.Status != "conflict" || len(report.Diagnostics) == 0 || report.Diagnostics[0].Code != "binding-changed" {
		t.Fatalf("auth mode change did not invalidate collection: %+v %+v", ex, report)
	}
}

func TestPoolSwitchChangesQuotaConstraintAndNoRawInputIsExported(t *testing.T) {
	p := testPolicy("codex", "protocol-v2-fixture", "chatgpt", "fixture", "percentage")
	input := []byte(`{"method":"account/rateLimits/updated","params":{"rateLimits":{"limitId":"codex","primary":{"usedPercent":20,"resetsAt":1792700000}}},"prompt":"CANARY_PROMPT_7941","cwd":"/secret/CANARY_PATH_2857","credential":"CANARY_SECRET_1836"}`)
	b := usage.Binding{ID: "b", PoolID: "pool-a", Provider: "openai", Service: "codex", Host: "codex", HostVersion: "protocol-v2-fixture", AuthMode: "chatgpt", SourceID: "fixture", StreamID: "stream-fixture"}
	first, _ := Normalize("codex", input, b, p, time.Now().UTC())
	b.PoolID = "pool-b"
	second, _ := Normalize("codex", input, b, p, time.Now().UTC())
	if first.Quotas[0].Constraint.ID == second.Quotas[0].Constraint.ID {
		t.Fatal("account pool switch reused quota constraint identity")
	}
	resetInput := []byte(`{"method":"account/rateLimits/updated","params":{"rateLimits":{"limitId":"codex","primary":{"usedPercent":20,"resetsAt":1793000000}}}}`)
	reset, _ := Normalize("codex", resetInput, b, p, time.Now().UTC())
	if second.Quotas[0].Generation == reset.Quotas[0].Generation || second.Quotas[0].Constraint.ID != reset.Quotas[0].Constraint.ID {
		t.Fatal("window reset did not advance only the generation")
	}
	out := string(mustJSON(t, first))
	for _, canary := range []string{"CANARY_PROMPT_7941", "CANARY_PATH_2857", "CANARY_SECRET_1836", "/secret"} {
		if strings.Contains(out, canary) {
			t.Fatalf("raw input leaked into normalized export: %s", canary)
		}
	}
}

func TestReadCodexRateLimitsUsesOnlyAllowedMethodAndBoundsContext(t *testing.T) {
	tr := &recordingTransport{}
	b := usage.Binding{ID: "b", PoolID: "pool", Provider: "openai", Service: "codex", Host: "codex", HostVersion: "protocol-v2-fixture", AuthMode: "chatgpt", SourceID: "fixture", StreamID: "stream-fixture"}
	p := testPolicy("codex", "protocol-v2-fixture", "chatgpt", "fixture", "percentage")
	ex, report := ReadCodexRateLimits(context.Background(), tr, b, p, time.Now().UTC())
	if tr.method != "account/rateLimits/read" {
		t.Fatalf("unexpected method %q", tr.method)
	}
	if report.Status != "valid" || len(ex.Quotas) != 1 || !ex.Quotas[0].Origin.FreshnessKnown || ex.Quotas[0].Revision != 7 {
		t.Fatalf("read reply was not normalized as observed capacity: %+v %+v", ex, report)
	}
}

func TestOfflineImportDoesNotRequireLiveCollectionPermissionAndPinsSource(t *testing.T) {
	p := testPolicy("codex", "protocol-v2-fixture", "chatgpt", "fixture", "percentage")
	p.Sources[0].CollectionAllowed = false
	b := usage.Binding{ID: "b", PoolID: "pool", Provider: "openai", Service: "codex", Host: "codex", HostVersion: "protocol-v2-fixture", AuthMode: "chatgpt", SourceID: "fixture", StreamID: "stream-fixture"}
	input := []byte(`{"id":1,"result":{"rateLimits":{"limitId":"codex","primary":{"usedPercent":10,"windowDurationMins":300,"resetsAt":1792700000}}}}`)
	ex, report := Normalize("codex", input, b, p, time.Now().UTC())
	if report.Status != "valid" || len(ex.Quotas) != 1 || ex.Quotas[0].Completeness != "full" {
		t.Fatalf("offline normalization incorrectly required live permission: %+v %+v", ex, report)
	}
	b.SourceID = "different-rule"
	_, report = Normalize("codex", input, b, p, time.Now().UTC())
	if report.Status != "unknown" || len(report.Diagnostics) == 0 || report.Diagnostics[0].Code != "policy-unsupported" {
		t.Fatalf("mismatched source rule authorized the binding: %+v", report)
	}
	tr := &recordingTransport{}
	_, report = ReadCodexRateLimits(context.Background(), tr, b, p, time.Now().UTC())
	if tr.method != "" || report.Status != "unknown" {
		t.Fatalf("live read bypassed exact configured source gate: method=%q report=%+v", tr.method, report)
	}
}

func TestOrderedCapturesResolveQuotaRevisionButUnorderedReadsConflict(t *testing.T) {
	p := testPolicy("codex", "protocol-v2-fixture", "chatgpt", "fixture", "percentage")
	p.Sources[0].Authority = "openai"
	p.Sources[0].MaxAgeSeconds = 60
	b := usage.Binding{ID: "b", PoolID: "pool", Provider: "openai", Service: "codex", Host: "codex", HostVersion: "protocol-v2-fixture", AuthMode: "chatgpt", SourceID: "fixture", StreamID: "pinned-stream", ConfigurationIDs: []string{"cfg"}, ConstraintsComplete: true}
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	read := func(used string, ordinal int64) usage.Export {
		input := []byte(`{"id":1,"result":{"rateLimits":{"limitId":"codex","primary":{"usedPercent":` + used + `,"windowDurationMins":300,"resetsAt":1793000000}}}}`)
		ex, report := NormalizeCapture("codex", input, b, p, now.Add(time.Duration(ordinal)*time.Second), ordinal)
		if report.Status != "valid" || len(ex.Quotas) != 1 || ex.Quotas[0].Revision != ordinal {
			t.Fatalf("capture ordinal was not preserved: %+v %+v", ex, report)
		}
		return ex
	}
	first, second := read("10", 1), read("20", 2)
	merged := first
	merged.Quotas = append(merged.Quotas, second.Quotas...)
	d := &usage.Demand{Version: usage.Version, ConfigurationID: "cfg", ProfileSHA256: strings.Repeat("a", 64), Source: "fixture-demand", ValidUntil: now.Add(time.Minute).Format(time.RFC3339Nano), Amounts: []usage.Amount{{ConstraintID: second.Quotas[0].Constraint.ID, Generation: second.Quotas[0].Generation, Unit: "percentage-points", Amount: "5"}}}
	ordered := usage.Capacity(merged, p, "cfg", d, now.Add(3*time.Second))
	if ordered.Disposition != "observed-fit" || len(ordered.Balances) != 1 || ordered.Balances[0].QuotaID != second.Quotas[0].ID {
		t.Fatalf("higher serialized capture should be selected: %+v", ordered)
	}
	first.Quotas[0].Revision, second.Quotas[0].Revision = 0, 0
	unorderedExport := first
	unorderedExport.Quotas = append(unorderedExport.Quotas, second.Quotas...)
	unordered := usage.Capacity(unorderedExport, p, "cfg", d, now.Add(3*time.Second))
	if unordered.Disposition != "unknown" {
		t.Fatalf("unsequenced conflicting observations should remain ambiguous: %+v", unordered)
	}
}

type recordingTransport struct{ method string }

func (r *recordingTransport) CaptureRevision() int64 { return 7 }

func (r *recordingTransport) Request(ctx context.Context, method string) ([]byte, error) {
	r.method = method
	if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > 5*time.Second {
		return nil, fmt.Errorf("missing bounded context")
	}
	return []byte(`{"id":1,"result":{"rateLimits":{"limitId":"codex","primary":{"usedPercent":10,"windowDurationMins":300,"resetsAt":1792700000}}}}`), nil
}
func testPolicy(host, version, auth, sourceID string, metrics ...string) usage.Policy {
	return usage.Policy{Version: usage.Version, Sources: []usage.SourceRule{{ID: sourceID, Host: host, HostVersion: version, AdapterVersion: AdapterVersion, AuthModes: []string{auth}, Metrics: metrics, Authority: "fixture-only", MaxAgeSeconds: 300, CollectionAllowed: true}}}
}
func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
