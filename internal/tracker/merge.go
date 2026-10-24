package tracker

import (
	"encoding/json"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
	"sort"
	"strconv"
	"strings"
)

func mergeItem(a, b Item) (Item, bool) {
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
			merged, bad := mergeLabelNodes(old, v)
			a.Fields[k] = merged
			if bad {
				conflict = true
				a.Complete = false
			}
			continue
		}
		if old, ok := a.Fields[k]; ok && semantic(old) != semantic(v) {
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
		if old, ok := a.ProjectFields[k]; ok && semantic(old) != semantic(v) {
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
func semantic(raw []byte) string {
	var v any
	if usage.Decode(raw, &v) != nil {
		return string(raw)
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// Field provenance points to a native field with the captured value. Converted
// descriptionText explicitly points to its original rich-text description.
func fieldPointer(s Source, r Request, item Item, field string, body []byte) string {
	var root any
	if usage.Decode(body, &root) != nil {
		return ""
	}
	matches := map[string]bool{}
	var walk func(any, string)
	walk = func(v any, path string) {
		switch node := v.(type) {
		case map[string]any:
			nativeKey := "id"
			if s.Provider == "github" {
				nativeKey = "node_id"
			}
			id := ""
			switch raw := node[nativeKey].(type) {
			case string:
				id = raw
			case json.Number:
				id = string(raw)
			}
			if id == item.Identity.RemoteID {
				fields := node
				prefix := path
				if inner, ok := node["fields"].(map[string]any); ok {
					fields = inner
					prefix = path + "/fields"
				}
				key := field
				derived := false
				if field == "descriptionText" {
					key = "description"
					derived = true
				}
				if field == "labelsConnection" {
					key = "labels"
				}
				value, ok := fields[key]
				target := prefix + "/" + escapePointer(key)
				if field == "labels" && s.Provider == "linear" {
					if connection, yes := value.(map[string]any); yes {
						value, ok = connection["nodes"]
						target += "/nodes"
					}
				}
				if ok {
					raw, _ := json.Marshal(value)
					if derived || semantic(raw) == semantic(item.Fields[field]) {
						matches[target] = true
					}
				}
			}
			for key, value := range node {
				walk(value, path+"/"+escapePointer(key))
			}
		case []any:
			for i, value := range node {
				walk(value, path+"/"+strconv.Itoa(i))
			}
		}
	}
	walk(root, "")
	if len(matches) != 1 {
		return ""
	}
	for path := range matches {
		return path
	}
	return ""
}
func escapePointer(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "~", "~0"), "/", "~1")
}

func mergeLabelNodes(a, b json.RawMessage) (json.RawMessage, bool) {
	var left, right []map[string]json.RawMessage
	if usage.Decode(a, &left) != nil || usage.Decode(b, &right) != nil {
		return a, true
	}
	nodes := map[string]map[string]json.RawMessage{}
	for _, node := range append(left, right...) {
		var id string
		if usage.Decode(node["id"], &id) != nil || id == "" {
			return a, true
		}
		if old, ok := nodes[id]; ok && semantic(marshalRaw(old)) != semantic(marshalRaw(node)) {
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
	return marshalRaw(out), false
}
func marshalRaw(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
