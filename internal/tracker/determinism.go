package tracker

import (
	"sort"
)

func stableDiagnostics(report *Report) {
	sort.SliceStable(report.Diagnostics, func(i, j int) bool {
		a, b := report.Diagnostics[i], report.Diagnostics[j]
		if a.Field != b.Field {
			return a.Field < b.Field
		}
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		return a.Message < b.Message
	})
}

// Request identity is structural: JSON object key order cannot change the
// approved read plan or continuation identity. Original bytes remain assets.
func requestKey(r Request) string {
	if len(r.Body) > 0 {
		r.Body = []byte(semantic(r.Body))
	}
	return Digest(r)
}
