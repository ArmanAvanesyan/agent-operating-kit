package collectors

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"strconv"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
)

type codexFrameEnvelope struct {
	Method string          `json:"method"`
	ID     json.RawMessage `json:"id"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
}

func codexFrame(raw json.RawMessage, b usage.Binding, p usage.Policy, received time.Time, ex *usage.Export) {
	var env codexFrameEnvelope
	if json.Unmarshal(raw, &env) != nil {
		return
	}
	if env.Method == "thread/tokenUsage/updated" {
		codexTokenUsage(env.Params, b, p, received, ex)
		return
	}
	var source json.RawMessage
	fresh, fullReply := false, false
	switch env.Method {
	case "account/rateLimits/updated":
		source, fresh = env.Params, true // documented update notification is an observation marker
	case "":
		if len(env.ID) > 0 && len(env.Result) > 0 {
			source, fresh, fullReply = env.Result, true, true
		}
	}
	if len(source) == 0 {
		return
	}
	codexRateLimits(source, b, p, received, fresh, fullReply, ex)
}

func codexTokenUsage(raw json.RawMessage, b usage.Binding, p usage.Policy, received time.Time, ex *usage.Export) {
	var frame struct {
		ThreadID   string `json:"threadId"`
		TurnID     string `json:"turnId"`
		TokenUsage struct {
			Total              tokenBreakdown `json:"total"`
			Last               tokenBreakdown `json:"last"`
			ModelContextWindow json.Number    `json:"modelContextWindow"`
		} `json:"tokenUsage"`
	}
	if json.Unmarshal(raw, &frame) != nil {
		return
	}
	thread := safeID(frame.ThreadID)
	if thread == "" {
		return
	}
	// The official App Server v2 schema defines total as the cumulative thread
	// counter and last as the increment that produced it. Store only total to
	// avoid counting both views of the same consumption.
	addCounter := func(field string, n json.Number, subsetOf string) {
		if n == "" || !validNonnegativeInteger(n) || !metricAllowed(p, b, field) {
			return
		}
		id := eventID("codex-thread-token", thread, field, string(n))
		// The reported total spans the thread, so a turn ID would incorrectly
		// split one cumulative series into separately summed turn series.
		e := usage.Event{ID: id, Origin: origin(b, received, received, true), BindingID: b.ID, SessionID: thread, Attribution: usage.Attribution{}, Metric: field, Unit: "tokens", Value: n, Semantics: "cumulative", CounterEpoch: thread, Sequence: 0, Billable: true, DedupeCoverage: "weak"}
		if subsetOf != "" && metricAllowed(p, b, subsetOf) {
			base := eventID("codex-thread-token", thread, subsetOf, string(counterValue(frame.TokenUsage.Total, subsetOf)))
			e.SubsetOf = &base
		}
		ex.Events = append(ex.Events, e)
	}
	addCounter("input-tokens", frame.TokenUsage.Total.InputTokens, "")
	addCounter("cached-input-tokens", frame.TokenUsage.Total.CachedInputTokens, "input-tokens")
	addCounter("output-tokens", frame.TokenUsage.Total.OutputTokens, "")
	addCounter("reasoning-tokens", frame.TokenUsage.Total.ReasoningOutputTokens, "output-tokens")
}

type tokenBreakdown struct {
	TotalTokens           json.Number `json:"totalTokens"`
	InputTokens           json.Number `json:"inputTokens"`
	CachedInputTokens     json.Number `json:"cachedInputTokens"`
	OutputTokens          json.Number `json:"outputTokens"`
	ReasoningOutputTokens json.Number `json:"reasoningOutputTokens"`
}

func counterValue(t tokenBreakdown, field string) json.Number {
	switch field {
	case "input-tokens":
		return t.InputTokens
	case "cached-input-tokens":
		return t.CachedInputTokens
	case "output-tokens":
		return t.OutputTokens
	case "reasoning-tokens":
		return t.ReasoningOutputTokens
	}
	return ""
}
func validNonnegativeInteger(n json.Number) bool {
	v, ok := new(big.Int).SetString(string(n), 10)
	return ok && v.Sign() >= 0
}

func codexRateLimits(raw json.RawMessage, b usage.Binding, p usage.Policy, received time.Time, fresh, fullReply bool, ex *usage.Export) {
	if b.PoolID == "" || !metricAllowed(p, b, "percentage") {
		return
	}
	var root struct {
		RateLimits          json.RawMessage            `json:"rateLimits"`
		RateLimitsByLimitID map[string]json.RawMessage `json:"rateLimitsByLimitId"`
	}
	if json.Unmarshal(raw, &root) != nil {
		return
	}
	var buckets map[string]json.RawMessage
	if len(root.RateLimitsByLimitID) > 0 {
		buckets = root.RateLimitsByLimitID
	} else if len(root.RateLimits) > 0 && string(root.RateLimits) != "null" {
		buckets = map[string]json.RawMessage{"legacy": root.RateLimits}
	} else {
		return
	}
	for mapKey, bucketRaw := range buckets {
		var bucket struct {
			LimitID   string          `json:"limitId"`
			Primary   json.RawMessage `json:"primary"`
			Secondary json.RawMessage `json:"secondary"`
		}
		if json.Unmarshal(bucketRaw, &bucket) != nil {
			continue
		}
		limitID := safeID(bucket.LimitID)
		if limitID == "" && len(root.RateLimitsByLimitID) > 0 {
			limitID = safeID(mapKey)
		}
		if limitID == "" {
			continue
		}
		addCodexWindow := func(window string, windowRaw json.RawMessage) {
			if len(windowRaw) == 0 || string(windowRaw) == "null" {
				return
			}
			var w struct {
				UsedPercent        json.Number `json:"usedPercent"`
				WindowDurationMins json.Number `json:"windowDurationMins"`
				ResetsAt           json.Number `json:"resetsAt"`
			}
			if json.Unmarshal(windowRaw, &w) != nil || w.UsedPercent == "" || !validDecimal(w.UsedPercent) {
				return
			}
			constraint := usage.Constraint{PoolID: b.PoolID, Provider: b.Provider, Service: b.Service, LimitID: limitID, Metric: "percentage", Unit: "percentage-points", Scope: "account/" + window, WindowKind: "rolling"}
			constraint.ID = usage.ConstraintID(constraint)
			used := w.UsedPercent
			remaining := subtractHundred(used)
			generation := "unknown"
			var resetAt *string
			if w.ResetsAt != "" {
				if epoch, err := strconv.ParseInt(string(w.ResetsAt), 10, 64); err == nil && epoch >= 0 {
					t := time.Unix(epoch, 0).UTC().Format(time.RFC3339)
					resetAt, generation = &t, t
				}
			}
			complete := fullReply && validPositiveInteger(w.WindowDurationMins) && w.ResetsAt != "" && resetAt != nil
			id := eventID("codex-rate-limit", constraint.ID, window, generation, string(used), timestamp(received))
			observed := time.Time{}
			if fresh {
				observed = received
			}
			completeness := "sparse"
			if complete {
				completeness = "full"
			}
			q := usage.Quota{ID: id, Origin: origin(b, received, observed, fresh), Constraint: constraint, Generation: generation, Revision: 0, Completeness: completeness, Remaining: &remaining, Used: &used, Limit: number(json.Number("100")), ResetAt: resetAt, ExpiresAt: quotaExpiry(p, b, "percentage", received), Precision: "reported-percent"}
			ex.Quotas = append(ex.Quotas, q)
		}
		addCodexWindow("primary", bucket.Primary)
		addCodexWindow("secondary", bucket.Secondary)
	}
}

func validPositiveInteger(n json.Number) bool {
	v, ok := new(big.Int).SetString(string(n), 10)
	return ok && v.Sign() > 0
}

func validDecimal(n json.Number) bool {
	r, err := usage.NumberRat(n)
	return err == nil && r.Sign() >= 0
}
func subtractHundred(n json.Number) json.Number {
	r, err := usage.NumberRat(n)
	if err != nil {
		return ""
	}
	r.Sub(big.NewRat(100, 1), r)
	return usage.Decimal(r)
}
func eventID(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte{0})
		h.Write([]byte(p))
	}
	return hex.EncodeToString(h.Sum(nil))
}
func origin(b usage.Binding, received time.Time, observed time.Time, known bool) usage.Origin {
	o := usage.Origin{SourceID: b.SourceID, Host: b.Host, HostVersion: b.HostVersion, AdapterVersion: AdapterVersion, AuthMode: b.AuthMode, ReceivedAt: timestamp(received), FreshnessKnown: known}
	if observed.IsZero() {
		// Receipt time satisfies the snapshot contract, but it never promotes
		// a source without an observation marker to fresh.
		observed = received
	}
	o.ObservedAt = timestamp(observed)
	return o
}
