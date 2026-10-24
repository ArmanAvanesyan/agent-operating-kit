package usage

import (
	"sort"
	"time"
)

// CurrentConstraints projects the same policy-authorized observations as Capacity.
// Unknown/stale balances must never be used to invent a generation or future fit.
func CurrentConstraints(e Export, p Policy, configurationID string, asOf time.Time) CapacitySnapshot {
	return Capacity(e, p, configurationID, nil, asOf)
}

// WindowFamily binds attribution to a particular generation-neutral limit and
// scope, not merely the broad fixed/rolling window kind.
func WindowFamily(c Constraint) string {
	return Digest(struct{ PoolID, Provider, Service, LimitID, Metric, Unit, Scope, WindowKind string }{c.PoolID, c.Provider, c.Service, c.LimitID, c.Metric, c.Unit, c.Scope, c.WindowKind})
}

// ConfigurationBindingsDigest binds an account-scope observation to the exact
// pool/source/stream/constraint coverage used for this configuration.
func ConfigurationBindingsDigest(e Export, configurationID string) string {
	bindings := []Binding{}
	for _, b := range e.Bindings {
		for _, id := range b.ConfigurationIDs {
			if id == configurationID {
				bindings = append(bindings, b)
				break
			}
		}
	}
	sort.Slice(bindings, func(i, j int) bool { return bindings[i].ID < bindings[j].ID })
	return Digest(bindings)
}
