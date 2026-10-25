package tracker

import (
	"encoding/json"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/readsource"
)

func mergeItem(a, b Item) (Item, bool) {
	v, c := readsource.SharedMergeItem(compatConvert[readsource.Item](a), compatConvert[readsource.Item](b))
	return compatConvert[Item](v), c
}
func semantic(b json.RawMessage) string { return readsource.SharedSemantic(b) }
func fieldPointer(s Source, r Request, i Item, f string, b []byte) string {
	return readsource.SharedFieldPointer(s, r, compatConvert[readsource.Item](i), f, b)
}
