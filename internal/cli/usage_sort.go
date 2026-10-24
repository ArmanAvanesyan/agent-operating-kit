package cli

import (
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
	"sort"
)

func sortUsageExport(e *usage.Export) {
	sort.Slice(e.Bindings, func(i, j int) bool { return e.Bindings[i].ID < e.Bindings[j].ID })
	sort.Slice(e.Quotas, func(i, j int) bool { return e.Quotas[i].ID < e.Quotas[j].ID })
	sort.Slice(e.Charges, func(i, j int) bool { return e.Charges[i].ID < e.Charges[j].ID })
	sort.Slice(e.Reservations, func(i, j int) bool { return e.Reservations[i].ID < e.Reservations[j].ID })
}
