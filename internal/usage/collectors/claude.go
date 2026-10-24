package collectors

import (
	"encoding/json"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
)

func claudeFrame(raw json.RawMessage, b usage.Binding, p usage.Policy, received time.Time, ex *usage.Export, report *usage.Report) {
	var data struct {
		Version   string `json:"version"`
		SessionID string `json:"session_id"`
		Model     struct {
			ID string `json:"id"`
		} `json:"model"`
		Cost struct {
			TotalCostUSD json.Number `json:"total_cost_usd"`
		} `json:"cost"`
		RateLimits *struct {
			FiveHour   *claudeLimit `json:"five_hour"`
			SevenDay   *claudeLimit `json:"seven_day"`
			SpendLimit *struct {
				UsedPercentage json.Number `json:"used_percentage"`
				ResetsAt       json.Number `json:"resets_at"`
				UsedUSD        json.Number `json:"used_usd"`
				LimitUSD       json.Number `json:"limit_usd"`
				Period         string      `json:"period"`
			} `json:"spend_limit"`
		} `json:"rate_limits"`
	}
	if json.Unmarshal(raw, &data) != nil {
		return
	}
	session := safeID(data.SessionID)
	if session == "" {
		return
	}
	hostVersion := data.Version
	if hostVersion == "" || hostVersion != b.HostVersion {
		report.Diagnostics = append(report.Diagnostics, usage.Diagnostic{Code: "host-version-mismatch", Field: "hostVersion", Message: "status-line version is missing or differs from the bound host version"})
		return
	}
	model := safeID(data.Model.ID)
	if data.Cost.TotalCostUSD != "" && validDecimal(data.Cost.TotalCostUSD) && metricAllowed(p, b, "currency") {
		id := eventID("claude-session-cost", session, string(data.Cost.TotalCostUSD))
		ex.Events = append(ex.Events, usage.Event{ID: id, Origin: origin(b, received, time.Time{}, false), BindingID: b.ID, SessionID: session, Attribution: usage.Attribution{Model: optString(model)}, Metric: "currency", Unit: "USD", Value: data.Cost.TotalCostUSD, Semantics: "cumulative", CounterEpoch: session, Sequence: 0, Billable: false, DedupeCoverage: "weak"})
	}
	if data.RateLimits == nil || b.PoolID == "" {
		return
	}
	if data.RateLimits.FiveHour != nil && metricAllowed(p, b, "percentage") {
		addClaudeQuota("five_hour", *data.RateLimits.FiveHour, b, p, received, ex)
	}
	if data.RateLimits.SevenDay != nil && metricAllowed(p, b, "percentage") {
		addClaudeQuota("seven_day", *data.RateLimits.SevenDay, b, p, received, ex)
	}
	if data.RateLimits.SpendLimit != nil {
		s := data.RateLimits.SpendLimit
		if versionAtLeast(hostVersion, "2.1.251") && metricAllowed(p, b, "percentage") {
			q := claudeLimit{UsedPercentage: s.UsedPercentage, ResetsAt: s.ResetsAt}
			addClaudeQuota("spend_limit", q, b, p, received, ex)
		}
		// USD values are gateway estimates with a separate, slower refresh
		// schedule; preserve them as their own sparse constraint.
		if versionAtLeast(hostVersion, "2.1.284") && metricAllowed(p, b, "currency") {
			addClaudeSpendUSD(s.UsedUSD, s.LimitUSD, s.Period, s.ResetsAt, b, p, received, ex)
		}
	}
}

type claudeLimit struct {
	UsedPercentage json.Number `json:"used_percentage"`
	ResetsAt       json.Number `json:"resets_at"`
}

func addClaudeQuota(window string, source claudeLimit, b usage.Binding, p usage.Policy, received time.Time, ex *usage.Export) {
	if source.UsedPercentage == "" || !validDecimal(source.UsedPercentage) {
		return
	}
	constraint := usage.Constraint{PoolID: b.PoolID, Provider: b.Provider, Service: b.Service, LimitID: "statusline/rate_limits/" + window, Metric: "percentage", Unit: "percentage-points", Scope: "bound-pool", WindowKind: "rolling"}
	constraint.ID = usage.ConstraintID(constraint)
	used := source.UsedPercentage
	remaining := subtractHundred(used)
	var resetAt *string
	generation := "unknown"
	if source.ResetsAt != "" {
		if epoch, err := parseEpoch(source.ResetsAt); err == nil {
			t := time.Unix(epoch, 0).UTC().Format(time.RFC3339)
			resetAt = &t
			generation = t
		}
	}
	id := eventID("claude-rate-limit", constraint.ID, generation, string(used))
	ex.Quotas = append(ex.Quotas, usage.Quota{ID: id, Origin: origin(b, received, time.Time{}, false), Constraint: constraint, Generation: generation, Revision: 0, Completeness: "sparse", Remaining: &remaining, Used: &used, Limit: number(json.Number("100")), ResetAt: resetAt, ExpiresAt: quotaExpiry(p, b, "percentage", received), Precision: "reported-percent"})
}

func addClaudeSpendUSD(usedValue, limitValue json.Number, period string, resetsAt json.Number, b usage.Binding, p usage.Policy, received time.Time, ex *usage.Export) {
	if usedValue == "" && limitValue == "" {
		return
	}
	if usedValue != "" && !validDecimal(usedValue) || limitValue != "" && !validDecimal(limitValue) {
		return
	}
	window := "unknown"
	switch period {
	case "daily", "weekly", "monthly":
		window = period
	}
	scope := "bound-pool"
	if window != "unknown" {
		scope += "/" + window
	}
	c := usage.Constraint{PoolID: b.PoolID, Provider: b.Provider, Service: b.Service, LimitID: "statusline/rate_limits/spend_limit/usd", Metric: "currency", Unit: "USD", Scope: scope, WindowKind: "unknown"}
	c.ID = usage.ConstraintID(c)
	generation := "unknown"
	var resetAt *string
	if resetsAt != "" {
		if epoch, err := parseEpoch(resetsAt); err == nil {
			t := time.Unix(epoch, 0).UTC().Format(time.RFC3339)
			generation, resetAt = t, &t
		}
	}
	var remaining *json.Number
	if usedValue != "" && limitValue != "" {
		u, _ := new(big.Rat).SetString(string(usedValue))
		l, _ := new(big.Rat).SetString(string(limitValue))
		l.Sub(l, u)
		n := usage.Decimal(l)
		remaining = &n
	}
	id := eventID("claude-gateway-spend-usd", c.ID, generation, string(usedValue), string(limitValue))
	ex.Quotas = append(ex.Quotas, usage.Quota{ID: id, Origin: origin(b, received, time.Time{}, false), Constraint: c, Generation: generation, Completeness: "sparse", Remaining: remaining, Used: number(usedValue), Limit: number(limitValue), ResetAt: resetAt, ExpiresAt: quotaExpiry(p, b, "currency", received), Precision: "estimated-gateway-spend"})
}

func parseEpoch(n json.Number) (int64, error) { return strconv.ParseInt(string(n), 10, 64) }
func versionAtLeast(version, minimum string) bool {
	parse := func(v string) ([3]int, bool) {
		var parts [3]int
		pieces := strings.Split(v, ".")
		if len(pieces) != len(parts) {
			return parts, false
		}
		for i, piece := range pieces {
			n, err := strconv.Atoi(piece)
			if err != nil || n < 0 {
				return parts, false
			}
			parts[i] = n
		}
		return parts, true
	}
	got, ok := parse(version)
	need, valid := parse(minimum)
	if !ok || !valid {
		return false
	}
	for i := range got {
		if got[i] != need[i] {
			return got[i] > need[i]
		}
	}
	return true
}
func optString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
