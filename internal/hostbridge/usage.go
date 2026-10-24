package hostbridge

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage/collectors"
)

// ExportUsage converts only explicitly bound host observations into the
// offline usage ledger. It never promotes an unbound transcript into usage.
func ExportUsage(p Prepared, policy Policy, t Transcript, observedAt time.Time) (usage.Export, usage.Report, error) {
	ex := usage.EmptyExport()
	r := usage.BaseReport()
	r.Status = "unknown"
	unknown := func(code, field, message string) (usage.Export, usage.Report, error) {
		r.Diagnostics = append(r.Diagnostics, usage.Diagnostic{Code: code, Field: field, Message: message})
		return usage.EmptyExport(), r, nil
	}
	if policy.UsageBinding == nil || policy.UsagePolicy == nil {
		return unknown("binding-required", "usageBinding", "explicit hash-bound usage binding and policy assets are required")
	}
	bindingBytes, err := assetBytes(policy.root, *policy.UsageBinding)
	if err != nil {
		return unknown("binding-unavailable", "usageBinding", "configured usage binding is unavailable or changed")
	}
	policyBytes, err := assetBytes(policy.root, *policy.UsagePolicy)
	if err != nil {
		return unknown("policy-unavailable", "usagePolicy", "configured usage policy is unavailable or changed")
	}
	var b usage.Binding
	if err = usage.Decode(bindingBytes, &b); err != nil {
		return unknown("binding-invalid", "usageBinding", "configured usage binding is invalid")
	}
	var up usage.Policy
	if err = usage.Decode(policyBytes, &up); err != nil || usage.ValidatePolicy(up) != nil {
		return unknown("policy-invalid", "usagePolicy", "configured usage policy is invalid")
	}
	if usage.ValidateBinding(b) != nil {
		return unknown("binding-invalid", "usageBinding", "configured usage binding is incomplete")
	}
	host := p.Host
	if host == "claude" {
		host = "claude-code"
	}
	if (host != "codex" && host != "claude-code") || b.Host != host || policy.Host != host || p.HostVersion == "" || policy.HostVersion != p.HostVersion || b.HostVersion != p.HostVersion || policy.AuthMode == "" || b.AuthMode != policy.AuthMode || !usageSourceAllowed(up, b) {
		return unknown("binding-mismatch", "usageBinding", "usage binding, host version, auth mode, and trusted source policy must match the prepared host")
	}
	if observedAt.IsZero() {
		return unknown("observation-time-required", "observedAt", "a caller-supplied observation time is required")
	}
	if host == "codex" {
		var input bytes.Buffer
		for _, frame := range t.UsageFrames {
			if len(frame) > 0 {
				input.Write(frame)
				input.WriteByte('\n')
			}
		}
		ex, r = collectors.Normalize("codex", input.Bytes(), b, up, observedAt)
	} else {
		ex = usage.EmptyExport()
		ex.Bindings = []usage.Binding{b}
		ex.PolicySHA256 = Hash(policyBytes)
		ex.Evidence = []usage.Asset{{Path: policy.UsageBinding.Path, SHA256: policy.UsageBinding.SHA256}, {Path: policy.UsagePolicy.Path, SHA256: policy.UsagePolicy.SHA256}}
		for i, raw := range t.UsageFrames {
			addClaudeUsage(raw, i, p, policy, t, b, up, observedAt, &ex)
		}
		if len(ex.Events) == 0 {
			r.Diagnostics = append(r.Diagnostics, usage.Diagnostic{Code: "no-supported-observation", Field: "usageFrames", Message: "transcript contains no supported bound token usage observation"})
		}
	}
	ex.PolicySHA256 = Hash(policyBytes)
	ex.Evidence = []usage.Asset{{Path: policy.UsageBinding.Path, SHA256: policy.UsageBinding.SHA256}, {Path: policy.UsagePolicy.Path, SHA256: policy.UsagePolicy.SHA256}}
	if len(ex.Events) == 0 && len(ex.Quotas) == 0 {
		r.Status = "unknown"
	} else if len(r.Diagnostics) > 0 {
		r.Status = "partial"
	} else {
		r.Status = "valid"
	}
	return ex, r, nil
}

func usageSourceAllowed(p usage.Policy, b usage.Binding) bool {
	for _, s := range p.Sources {
		if s.ID != b.SourceID || s.Host != b.Host || s.HostVersion != b.HostVersion || s.AdapterVersion != collectors.AdapterVersion || !s.CollectionAllowed {
			continue
		}
		for _, a := range s.AuthModes {
			if a == b.AuthMode {
				return true
			}
		}
	}
	return false
}

func addClaudeUsage(raw json.RawMessage, sequence int, prepared Prepared, policy Policy, transcript Transcript, b usage.Binding, up usage.Policy, at time.Time, ex *usage.Export) {
	var fields map[string]json.RawMessage
	if usage.Decode(raw, &fields) != nil {
		return
	}
	metricMap := []struct{ field, metric string }{{"input_tokens", "input-tokens"}, {"output_tokens", "output-tokens"}, {"cache_read_input_tokens", "cached-input-tokens"}, {"cache_creation_input_tokens", "cache-creation-tokens"}}
	session := safeUsageID(transcript.SessionID)
	if session == "" {
		session = safeUsageID(transcript.ThreadID)
	}
	if session == "" {
		return
	}
	turn := safeUsageID(transcript.TurnID)
	for _, item := range metricMap {
		vraw := fields[item.field]
		if len(vraw) == 0 || !usageMetricAllowed(up, b, item.metric) {
			continue
		}
		var n json.Number
		d := json.NewDecoder(bytes.NewReader(vraw))
		d.UseNumber()
		if d.Decode(&n) != nil || !validUsageInteger(n) {
			continue
		}
		call := rawUsageID(raw, sequence)
		id := rawUsageID(json.RawMessage(item.metric+":"+string(vraw)), sequence)
		origin := usage.Origin{SourceID: b.SourceID, Host: b.Host, HostVersion: b.HostVersion, AdapterVersion: collectors.AdapterVersion, AuthMode: b.AuthMode, ObservedAt: at.UTC().Format(time.RFC3339Nano), ReceivedAt: at.UTC().Format(time.RFC3339Nano), FreshnessKnown: false}
		ex.Events = append(ex.Events, usage.Event{ID: id, Origin: origin, BindingID: b.ID, SessionID: session, TurnID: turn, CallID: call, Attribution: usage.Attribution{ConfigurationID: stringPtr(prepared.ConfigurationID), Provider: stringPtr(b.Provider), Model: stringPtr(prepared.Model)}, Metric: item.metric, Unit: "tokens", Value: n, Semantics: "snapshot", CounterEpoch: session, Sequence: int64(sequence), Billable: false, DedupeCoverage: "weak"})
	}
	// A session-level cost observation is cumulative. Keeping it in one session
	// epoch prevents resumed sessions from being summed as separate increments.
	if rawCost := fields["total_cost_usd"]; len(rawCost) > 0 && usageMetricAllowed(up, b, "currency") {
		var n json.Number
		d := json.NewDecoder(bytes.NewReader(rawCost))
		d.UseNumber()
		if d.Decode(&n) == nil && validUsageDecimal(n) {
			origin := usage.Origin{SourceID: b.SourceID, Host: b.Host, HostVersion: b.HostVersion, AdapterVersion: collectors.AdapterVersion, AuthMode: b.AuthMode, ObservedAt: at.UTC().Format(time.RFC3339Nano), ReceivedAt: at.UTC().Format(time.RFC3339Nano)}
			ex.Events = append(ex.Events, usage.Event{ID: rawUsageID(json.RawMessage("session-cost:"+session+":"+string(n)), 0), Origin: origin, BindingID: b.ID, SessionID: session, CallID: "session-cost", Attribution: usage.Attribution{ConfigurationID: stringPtr(prepared.ConfigurationID), Provider: stringPtr(b.Provider), Model: stringPtr(prepared.Model)}, Metric: "currency", Unit: "USD", Value: n, Semantics: "cumulative", CounterEpoch: session, Sequence: 0, Billable: false, DedupeCoverage: "weak"})
		}
	}
}

func usageMetricAllowed(p usage.Policy, b usage.Binding, m string) bool {
	for _, s := range p.Sources {
		if s.ID == b.SourceID && s.Host == b.Host && s.HostVersion == b.HostVersion && s.AdapterVersion == collectors.AdapterVersion {
			for _, v := range s.Metrics {
				if v == m {
					return true
				}
			}
		}
	}
	return false
}
func validUsageInteger(n json.Number) bool {
	v, ok := new(big.Int).SetString(string(n), 10)
	return ok && v.Sign() >= 0
}
func validUsageDecimal(n json.Number) bool {
	r, ok := new(big.Rat).SetString(string(n))
	return ok && r.Sign() >= 0
}
func rawUsageID(raw json.RawMessage, sequence int) string {
	h := sha256.Sum256(append(append([]byte(nil), raw...), []byte(fmt.Sprintf(":%d", sequence))...))
	return "host-usage-" + hex.EncodeToString(h[:])
}
func safeUsageID(s string) string {
	if len(s) > 256 {
		return ""
	}
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && !strings.ContainsRune("._:/@+ -", r) {
			return ""
		}
	}
	return strings.TrimSpace(s)
}
func stringPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
