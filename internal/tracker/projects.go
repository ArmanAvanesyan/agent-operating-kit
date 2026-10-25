package tracker

import (
	"encoding/json"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/readsource"
)

func joinGitHubProjects(s *Snapshot, f map[string][]byte) error {
	v := compatConvert[readsource.Snapshot](s)
	e := readsource.SharedJoinGitHubProjects(&v, f)
	*s = compatConvert[Snapshot](v)
	return e
}
func mappedNativeField(i Item, f string) (json.RawMessage, bool) {
	return readsource.SharedMappedNativeField(compatConvert[readsource.Item](i), f)
}
