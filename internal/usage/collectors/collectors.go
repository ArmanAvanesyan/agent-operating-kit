// Package collectors contains bounded, allow-list-only host usage normalizers.
// It never starts a host process or performs authentication.
package collectors

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
)

const AdapterVersion = "aok-usage-collector/v1"

var hosts = []string{"codex", "claude-code", "cursor", "opencode", "gemini", "agy", "openclaw", "hermes-agent"}

// Describe declares discovery state for every supported host. Only Codex and
// Claude have bounded normalizers; live collection remains gated pending host
// version/auth-mode conformance evidence.
func Describe() []usage.CollectorDescriptor {
	out := make([]usage.CollectorDescriptor, 0, len(hosts))
	for _, host := range hosts {
		d := usage.CollectorDescriptor{Host: host, AdapterVersion: AdapterVersion, Usage: "unknown", Capacity: "unknown", Streaming: "unknown", Identity: "unknown", LiveGate: "pending", Notes: "no verified supported source; discovery only"}
		switch host {
		case "codex":
			d.Usage = "supported-normalizer"
			d.Capacity = "supported-normalizer"
			d.Streaming = "supported-normalizer"
			d.Identity = "unknown"
			d.Notes = "App Server v2 tokenUsage and rateLimits frames; read-only transport may be injected; live gate pending"
		case "claude-code":
			d.Usage = "supported-normalizer"
			d.Capacity = "reported-observation-only"
			d.Notes = "status-line stdin snapshot; redraw does not prove fresh quota observation"
		}
		out = append(out, d)
	}
	return out
}

// Normalize converts a single JSON document or JSONL stream into an allowlisted
// export. It uses receivedAt only as a receipt timestamp unless the source
// protocol itself establishes an update/read observation.
func Normalize(host string, input []byte, b usage.Binding, p usage.Policy, receivedAt time.Time) (usage.Export, usage.Report) {
	return normalize(host, input, b, p, receivedAt, 0)
}

// NormalizeCapture adds a caller-supplied serialized capture ordinal to fresh
// quota observations. The ordinal is scoped to the exact bound stream and is
// not a provider revision. Callers must guarantee that captures for that
// stream are serialized; zero preserves unordered/ambiguous semantics.
func NormalizeCapture(host string, input []byte, b usage.Binding, p usage.Policy, receivedAt time.Time, streamRevision int64) (usage.Export, usage.Report) {
	if streamRevision < 0 {
		return usage.EmptyExport(), diag(usage.BaseReport(), "invalid-input", "streamRevision", "capture ordinal cannot be negative")
	}
	return normalize(host, input, b, p, receivedAt, streamRevision)
}

func normalize(host string, input []byte, b usage.Binding, p usage.Policy, receivedAt time.Time, streamRevision int64) (usage.Export, usage.Report) {
	ex := usage.EmptyExport()
	r := usage.BaseReport()
	r.Status = "unknown"
	if !knownHost(host) {
		return ex, diag(r, "unsupported-host", "host", "host has no usage collector")
	}
	if len(input) == 0 || len(input) > 4<<20 {
		return ex, diag(r, "invalid-input", "input", "input is empty or exceeds the bounded collector limit")
	}
	if b.Host != host || b.ID == "" || b.SourceID == "" {
		return ex, diag(r, "binding-required", "binding", "an explicit matching source binding is required")
	}
	if usage.ValidatePolicy(p) != nil {
		return ex, diag(r, "policy-invalid", "policy", "the trusted source policy is invalid")
	}
	if usage.ValidateBinding(b) != nil {
		return ex, diag(r, "binding-invalid", "binding", "the configured source binding is incomplete or invalid")
	}
	if b.HostVersion == "" || !policyAllows(p, b.SourceID, host, b.HostVersion, b.AuthMode) {
		return ex, diag(r, "policy-unsupported", "policy", "no trusted policy rule permits this host version and auth mode")
	}
	if receivedAt.IsZero() {
		return ex, diag(r, "receipt-time-required", "receivedAt", "a caller-supplied receipt time is required")
	}
	frames, err := decodeFrames(input)
	if err != nil {
		return ex, diag(r, "invalid-json", "input", "input could not be decoded as bounded JSON")
	}
	if host == "codex" && codexAuthModeChanged(frames, b.AuthMode) {
		return ex, diag(r, "binding-changed", "binding.authMode", "the App Server account auth mode differs from the configured binding")
	}
	ex.Bindings = []usage.Binding{b}
	ex.PolicySHA256 = usage.PolicyHash(p)
	for _, frame := range frames {
		switch host {
		case "codex":
			codexFrame(frame, b, p, receivedAt.UTC(), &ex)
		case "claude-code":
			claudeFrame(frame, b, p, receivedAt.UTC(), &ex, &r)
		default:
			return usage.EmptyExport(), diag(r, "unsupported-source", "host", "no verified normalizer is available for this host")
		}
	}
	constraintIDs := map[string]bool{}
	for _, id := range ex.Bindings[0].ConstraintIDs {
		constraintIDs[id] = true
	}
	for _, q := range ex.Quotas {
		constraintIDs[q.Constraint.ID] = true
	}
	if streamRevision > 0 {
		for i := range ex.Quotas {
			if ex.Quotas[i].Origin.FreshnessKnown && ex.Quotas[i].Origin.SourceID == b.SourceID && ex.Quotas[i].Origin.Host == b.Host && ex.Quotas[i].Origin.HostVersion == b.HostVersion && ex.Quotas[i].Origin.AuthMode == b.AuthMode {
				ex.Quotas[i].Revision = streamRevision
			}
		}
	}
	ex.Bindings[0].ConstraintIDs = ex.Bindings[0].ConstraintIDs[:0]
	for id := range constraintIDs {
		ex.Bindings[0].ConstraintIDs = append(ex.Bindings[0].ConstraintIDs, id)
	}
	sort.Strings(ex.Bindings[0].ConstraintIDs)
	ex.Events = dedupeEvents(ex.Events)
	ex.Quotas = dedupeQuotas(ex.Quotas)
	if len(ex.Events) == 0 && len(ex.Quotas) == 0 {
		r.Status = "unknown"
		if len(r.Diagnostics) == 0 {
			r.Diagnostics = append(r.Diagnostics, usage.Diagnostic{Code: "no-supported-observation", Field: "input", Message: "input contained no supported usage or capacity observation"})
		}
	} else if len(r.Diagnostics) > 0 {
		r.Status = "partial"
	} else {
		r.Status = "valid"
	}
	return ex, r
}

func codexAuthModeChanged(frames []json.RawMessage, boundMode string) bool {
	for _, raw := range frames {
		var frame struct {
			Method string `json:"method"`
			Params struct {
				AuthMode json.RawMessage `json:"authMode"`
			} `json:"params"`
		}
		if json.Unmarshal(raw, &frame) != nil || frame.Method != "account/updated" || len(frame.Params.AuthMode) == 0 {
			continue
		}
		var mode string
		if json.Unmarshal(frame.Params.AuthMode, &mode) != nil || mode == "" || mode != boundMode {
			return true
		}
	}
	return false
}

func dedupeEvents(in []usage.Event) []usage.Event {
	seen := map[string]struct{}{}
	out := make([]usage.Event, 0, len(in))
	for _, e := range in {
		if _, ok := seen[e.ID]; ok {
			continue
		}
		seen[e.ID] = struct{}{}
		out = append(out, e)
	}
	return out
}
func dedupeQuotas(in []usage.Quota) []usage.Quota {
	seen := map[string]struct{}{}
	out := make([]usage.Quota, 0, len(in))
	for _, q := range in {
		if _, ok := seen[q.ID]; ok {
			continue
		}
		seen[q.ID] = struct{}{}
		out = append(out, q)
	}
	return out
}

// ReadOnlyTransport is deliberately narrower than a general app-server
// client. Implementations supply an already-authorized local transport and
// return the raw JSON-RPC response frame for the requested method.
type ReadOnlyTransport interface {
	Request(context.Context, string) ([]byte, error)
}

// OrderedReadOnlyTransport may expose an ordinal assigned by a serialized,
// pinned runtime capture stream. Implementations must not synthesize it from
// request completion time or process-local arrival races.
type OrderedReadOnlyTransport interface {
	ReadOnlyTransport
	CaptureRevision() int64
}

// ReadCodexRateLimits asks only for the documented read method and bounds the
// caller-provided transport to five seconds. It neither starts app-server nor
// obtains credentials.
func ReadCodexRateLimits(ctx context.Context, transport ReadOnlyTransport, b usage.Binding, p usage.Policy, receivedAt time.Time) (usage.Export, usage.Report) {
	if transport == nil {
		return usage.EmptyExport(), diag(usage.BaseReport(), "transport-unavailable", "transport", "no configured read-only App Server transport")
	}
	if b.Host != "codex" || usage.ValidateBinding(b) != nil || usage.ValidatePolicy(p) != nil || !liveCollectionAllowed(p, b) {
		return usage.EmptyExport(), diag(usage.BaseReport(), "collection-not-authorized", "policy", "the exact bound source is not configured for live collection")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	data, err := transport.Request(ctx, "account/rateLimits/read")
	if err != nil {
		return usage.EmptyExport(), diag(usage.BaseReport(), "read-failed", "transport", "the bounded read-only quota request failed")
	}
	if ordered, ok := transport.(OrderedReadOnlyTransport); ok {
		return NormalizeCapture("codex", data, b, p, receivedAt, ordered.CaptureRevision())
	}
	return Normalize("codex", data, b, p, receivedAt)
}

func decodeFrames(input []byte) ([]json.RawMessage, error) {
	trimmed := bytes.TrimSpace(input)
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("empty")
	}
	if json.Valid(trimmed) && (trimmed[0] == '{' || trimmed[0] == '[') {
		if trimmed[0] == '[' {
			var items []json.RawMessage
			if err := json.Unmarshal(trimmed, &items); err != nil {
				return nil, err
			}
			return items, nil
		}
		return []json.RawMessage{append(json.RawMessage(nil), trimmed...)}, nil
	}
	lines := bytes.Split(trimmed, []byte("\n"))
	if len(lines) > 1024 {
		return nil, fmt.Errorf("too many frames")
	}
	frames := make([]json.RawMessage, 0, len(lines))
	for _, line := range lines {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		if !json.Valid(line) {
			return nil, fmt.Errorf("invalid frame")
		}
		frames = append(frames, append(json.RawMessage(nil), line...))
	}
	return frames, nil
}

func policyAllows(p usage.Policy, sourceID, host, version, auth string) bool {
	for _, rule := range p.Sources {
		if rule.ID != sourceID || rule.Host != host || rule.HostVersion != version || rule.AdapterVersion != AdapterVersion {
			continue
		}
		if len(rule.AuthModes) == 0 {
			return true
		}
		for _, mode := range rule.AuthModes {
			if mode == auth && auth != "" {
				return true
			}
		}
	}
	return false
}
func liveCollectionAllowed(p usage.Policy, b usage.Binding) bool {
	for _, rule := range p.Sources {
		if rule.ID == b.SourceID && rule.Host == b.Host && rule.HostVersion == b.HostVersion && rule.AdapterVersion == AdapterVersion && authAllowed(rule, b.AuthMode) && rule.CollectionAllowed {
			return true
		}
	}
	return false
}
func metricAllowed(p usage.Policy, b usage.Binding, metric string) bool {
	for _, rule := range p.Sources {
		if rule.ID != b.SourceID || rule.Host != b.Host || rule.HostVersion != b.HostVersion || rule.AdapterVersion != AdapterVersion {
			continue
		}
		if !authAllowed(rule, b.AuthMode) {
			continue
		}
		for _, m := range rule.Metrics {
			if m == metric {
				return true
			}
		}
	}
	return false
}
func authAllowed(rule usage.SourceRule, auth string) bool {
	for _, mode := range rule.AuthModes {
		if mode == auth {
			return true
		}
	}
	return false
}
func quotaExpiry(p usage.Policy, b usage.Binding, metric string, received time.Time) string {
	ttl := 60 * time.Second
	for _, rule := range p.Sources {
		if rule.ID != b.SourceID || rule.Host != b.Host || rule.HostVersion != b.HostVersion || rule.AdapterVersion != AdapterVersion || !authAllowed(rule, b.AuthMode) {
			continue
		}
		for _, allowed := range rule.Metrics {
			if allowed == metric && time.Duration(rule.MaxAgeSeconds)*time.Second < ttl {
				ttl = time.Duration(rule.MaxAgeSeconds) * time.Second
			}
		}
	}
	return timestamp(received.Add(ttl))
}
func knownHost(host string) bool {
	for _, h := range hosts {
		if h == host {
			return true
		}
	}
	return false
}
func diag(r usage.Report, code, field, message string) usage.Report {
	r.Status = "unknown"
	switch code {
	case "invalid-input", "invalid-json", "receipt-time-required", "policy-invalid", "binding-invalid":
		r.Status = "invalid"
	case "binding-changed":
		r.Status = "conflict"
	}
	r.Diagnostics = append(r.Diagnostics, usage.Diagnostic{Code: code, Field: field, Message: message})
	return r
}
func timestamp(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }
func ptr(s string) *string         { return &s }
func number(v json.Number) *json.Number {
	if v == "" {
		return nil
	}
	n := v
	return &n
}
func safeID(s string) string {
	// Protocol identifiers are admitted only from explicitly named ID fields.
	// Restrict their alphabet so a path, prompt fragment, or credential cannot
	// be smuggled through an identifier field.
	if len(s) == 0 || len(s) > 256 || strings.IndexFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9') && r != '_' && r != '-'
	}) >= 0 {
		return ""
	}
	return s
}
