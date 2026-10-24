package store

import (
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
	"sort"
)

func sortExport(e *usage.Export) {
	sort.Slice(e.Quotas, func(i, j int) bool { return e.Quotas[i].ID < e.Quotas[j].ID })
}
