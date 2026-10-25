// Read-side extraction from AOK; legacy formats intentionally retain their identities.
package readsource

import (
	"encoding/json"

	"sort"
	"strconv"
	"strings"
)

func SharedMergeItem(a, b Item) (Item, bool) {
	conflict := false
	if a.Revision != "" && b.Revision != "" && a.Revision != b.Revision {
		a.Complete = false
		return a, true
	}
	if a.Revision == "" && b.Revision != "" {
		a, b = b, a
	}
	if a.Fields == nil {
		a.Fields = map[string]json.RawMessage{}
	}
	for k, v := range b.Fields {
		if old, ok := a.Fields[k]; ok && k == "labels" && a.Identity.Provider == "linear" {
			merged, bad := SharedMergeLabelNodes(old, v)
			a.Fields[k] = merged
			if bad {
				conflict = true
				a.Complete = false
			}
			continue
		}
		if old, ok := a.Fields[k]; ok && SharedSemantic(old) != SharedSemantic(v) {
			conflict = true
			a.Complete = false
		} else {
			a.Fields[k] = v
		}
	}
	if a.ProjectFields == nil {
		a.ProjectFields = map[string]json.RawMessage{}
	}
	for k, v := range b.ProjectFields {
		if old, ok := a.ProjectFields[k]; ok && SharedSemantic(old) != SharedSemantic(v) {
			conflict = true
			a.Complete = false
		} else {
			a.ProjectFields[k] = v
		}
	}
	if a.Provenance == nil {
		a.Provenance = map[string][]Pointer{}
	}
	for k, v := range b.Provenance {
		a.Provenance[k] = append(a.Provenance[k], v...)
	}
	comments := map[string]Comment{}
	for _, v := range a.Comments {
		comments[v.ID] = v
	}
	for _, v := range b.Comments {
		if old, ok := comments[v.ID]; ok && Digest(old) != Digest(v) {
			conflict = true
			a.Complete = false
		} else {
			comments[v.ID] = v
		}
	}
	a.Comments = []Comment{}
	for _, v := range comments {
		a.Comments = append(a.Comments, v)
	}
	sort.Slice(a.Comments, func(i, j int) bool { return a.Comments[i].ID < a.Comments[j].ID })
	rel := map[string]Relation{}
	for _, v := range append(a.Relations, b.Relations...) {
		rel[Digest(v)] = v
	}
	a.Relations = []Relation{}
	for _, v := range rel {
		a.Relations = append(a.Relations, v)
	}
	sort.Slice(a.Relations, func(i, j int) bool { return Digest(a.Relations[i]) < Digest(a.Relations[j]) })
	transitions := map[string]Transition{}
	for _, v := range append(a.Transitions, b.Transitions...) {
		if old, ok := transitions[v.ID]; ok && Digest(old) != Digest(v) {
			conflict = true
			a.Complete = false
		} else {
			transitions[v.ID] = v
		}
	}
	a.Transitions = []Transition{}
	for _, v := range transitions {
		a.Transitions = append(a.Transitions, v)
	}
	sort.Slice(a.Transitions, func(i, j int) bool { return a.Transitions[i].ID < a.Transitions[j].ID })
	loss := map[string]bool{}
	for _, v := range append(a.LossyFields, b.LossyFields...) {
		loss[v] = true
	}
	a.LossyFields = []string{}
	for v := range loss {
		a.LossyFields = append(a.LossyFields, v)
	}
	sort.Strings(a.LossyFields)
	if !b.Complete {
		a.Complete = false
	}
	return a, conflict
}
func SharedSemantic(raw []byte) string {
	var v any
	if decodeRaw(raw, &v) != nil {
		return string(raw)
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// Field provenance points to a native field with the captured value. Converted
// descriptionText explicitly points to its original rich-text description.
func SharedFieldPointer(s Source, r Request, item Item, field string, body []byte) string {
	return pageFieldPointers(s, body)(item, field)
}
func SharedEscapePointer(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "~", "~0"), "/", "~1")
}

func SharedMergeLabelNodes(a, b json.RawMessage) (json.RawMessage, bool) {
	var left, right []map[string]json.RawMessage
	if decodeRaw(a, &left) != nil || decodeRaw(b, &right) != nil {
		return a, true
	}
	nodes := map[string]map[string]json.RawMessage{}
	for _, node := range append(left, right...) {
		var id string
		if decodeRaw(node["id"], &id) != nil || id == "" {
			return a, true
		}
		if old, ok := nodes[id]; ok && SharedSemantic(SharedMarshalRaw(old)) != SharedSemantic(SharedMarshalRaw(node)) {
			return a, true
		}
		nodes[id] = node
	}
	ids := []string{}
	for id := range nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := []map[string]json.RawMessage{}
	for _, id := range ids {
		out = append(out, nodes[id])
	}
	return SharedMarshalRaw(out), false
}
func SharedMarshalRaw(v any) json.RawMessage { b, _ := json.Marshal(v); return b }

// Index native nodes once per page; provenance is otherwise quadratic in the
// number of items because every field would reparse and rescan the full page.
func pageFieldPointers(s Source, body []byte) func(Item, string) string {
	type nodeRef struct {
		path string
		node map[string]any
	}
	index := map[string][]nodeRef{}
	var root any
	if decodeRaw(body, &root) != nil {
		return func(Item, string) string { return "" }
	}
	nativeKey := "id"
	if s.Provider == "github" {
		nativeKey = "node_id"
	}
	var walk func(any, string)
	walk = func(v any, path string) {
		switch n := v.(type) {
		case map[string]any:
			id := ""
			switch x := n[nativeKey].(type) {
			case string:
				id = x
			case json.Number:
				id = string(x)
			}
			if id != "" {
				index[id] = append(index[id], nodeRef{path, n})
			}
			for key, child := range n {
				walk(child, path+"/"+SharedEscapePointer(key))
			}
		case []any:
			for i, child := range n {
				walk(child, path+"/"+strconv.Itoa(i))
			}
		}
	}
	walk(root, "")
	return func(item Item, field string) string {
		matches := map[string]bool{}
		for _, ref := range index[item.Identity.RemoteID] {
			fields := ref.node
			prefix := ref.path
			if inner, ok := fields["fields"].(map[string]any); ok {
				fields = inner
				prefix += "/fields"
			}
			key := field
			derived := field == "descriptionText"
			if derived {
				key = "description"
			}
			if field == "labelsConnection" {
				key = "labels"
			}
			value, ok := fields[key]
			target := prefix + "/" + SharedEscapePointer(key)
			if field == "labels" && s.Provider == "linear" {
				if connection, yes := value.(map[string]any); yes {
					value, ok = connection["nodes"]
					target += "/nodes"
				}
			}
			if ok {
				raw, _ := json.Marshal(value)
				if derived || SharedSemantic(raw) == SharedSemantic(item.Fields[field]) {
					matches[target] = true
				}
			}
		}
		if len(matches) == 1 {
			for p := range matches {
				return p
			}
		}
		return ""
	}
}
