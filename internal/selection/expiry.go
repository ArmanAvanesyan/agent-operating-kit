package selection

import (
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/modelcatalog"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
	"time"
)

func decisionExpiry(c *modelcatalog.Capture, e usage.Export, p usage.Policy, at time.Time) time.Time {
	until := at.Add(60 * time.Second)
	limit := func(t time.Time) {
		if t.After(at) {
			until = minTime(until, t)
		}
	}
	parse := func(s string) {
		t, err := time.Parse(time.RFC3339Nano, s)
		if err == nil {
			limit(t)
		}
	}
	for _, a := range c.Catalog.Assertions {
		parse(a.ExpiresAt)
		obs, err := time.Parse(time.RFC3339Nano, a.ObservedAt)
		if err == nil {
			hours := c.Policy.ValidityHours[a.Category]
			if hours <= 0 {
				hours = modelcatalog.DefaultValidityHours()[a.Category]
			}
			if hours > 0 {
				limit(obs.Add(time.Duration(hours) * time.Hour))
			}
		}
	}
	for _, probe := range c.Catalog.Probes {
		obs, err := time.Parse(time.RFC3339Nano, probe.ObservedAt)
		if err == nil {
			hours := 24
			if h := c.Policy.ValidityHours[modelcatalog.Invoked]; h > 0 && h < hours {
				hours = h
			}
			limit(obs.Add(time.Duration(hours) * time.Hour))
		}
	}
	for _, q := range e.Quotas {
		parse(q.ExpiresAt)
		if q.ResetAt != nil {
			parse(*q.ResetAt)
		}
		obs, err := time.Parse(time.RFC3339Nano, q.Origin.ObservedAt)
		if err == nil {
			age := 60
			for _, s := range p.Sources {
				if s.ID == q.Origin.SourceID && s.MaxAgeSeconds > 0 && s.MaxAgeSeconds < age {
					age = s.MaxAgeSeconds
				}
			}
			limit(obs.Add(time.Duration(age) * time.Second))
		}
	}
	return until
}
