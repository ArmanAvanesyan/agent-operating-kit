package tracker

import (
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/readsource"
)

func stableDiagnostics(r *Report) {
	v := compatConvert[readsource.Report](r)
	readsource.SharedStableDiagnostics(&v)
	*r = compatConvert[Report](v)
}
func requestKey(r Request) string { return readsource.SharedRequestKey(r) }
