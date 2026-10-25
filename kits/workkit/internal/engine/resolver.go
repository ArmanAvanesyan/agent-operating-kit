package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

type Snapshot struct {
	Documents []*Document
	Paths     []string
	Assets    map[string][]byte
	Workspace *Document
}
type key struct{ Kind, ID, Revision string }

func Key(d *Document) key { return key{d.Kind, d.ID, string(d.Revision)} }

// schemaWalk follows only structurally declared object/array properties. Maps
// carrying arbitrary provider values and namespaced extensions are opaque.
func References(d *Document) ([]map[string]any, []map[string]any) {
	var refs, assets []map[string]any
	var walk func(any, map[string]any)
	walk = func(v any, s map[string]any) {
		if ref := String(s["$ref"]); ref != "" {
			def := ref[strings.LastIndex(ref, "/")+1:]
			if def == "ref" {
				refs = append(refs, Map(v))
				return
			}
			if def == "asset" {
				assets = append(assets, Map(v))
				return
			}
			if def == "extensions" {
				return
			}
			if c, ok := commonDefs[def]; ok {
				walk(v, Map(c))
			}
			return
		}
		if Map(s["properties"]) != nil {
			for k, child := range Map(s["properties"]) {
				if value, ok := Map(v)[k]; ok {
					walk(value, Map(child))
				}
			}
		}
		if item, ok := s["items"]; ok {
			for _, value := range List(v) {
				walk(value, Map(item))
			}
		}
	}
	walk(d.Value, resourceSchema[d.Kind])
	return refs, assets
}

var commonDefs map[string]any
var resourceSchema map[string]map[string]any

func Resolve(ctx context.Context, workspace, root string) (*Snapshot, error) {
	if root == "" {
		root = filepath.Dir(workspace)
	}
	r, e := SafePath(root)
	if e != nil {
		return nil, e
	}
	wp, e := SafePath(workspace)
	if e != nil {
		return nil, e
	}
	rel, e := filepath.Rel(r, wp)
	if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, errors.New("workspace outside root")
	}
	paths, e := ResourcePaths(ctx, r)
	if e != nil {
		return nil, e
	}
	if !containsPath(paths, wp) {
		paths = append(paths, wp)
	}
	sort.Strings(paths)
	if len(paths) > MaxResources {
		return nil, errors.New("resource limit")
	}
	snapshot := &Snapshot{Assets: map[string][]byte{}}
	index := map[key]*Document{}
	total := 0
	for _, p := range paths {
		b, e := ReadFile(ctx, p)
		if e != nil {
			return nil, e
		}
		total += len(b)
		if total > MaxTotalBytes {
			return nil, errors.New("configuration byte limit")
		}
		format := strings.TrimPrefix(strings.ToLower(filepath.Ext(p)), ".")
		d, e := Parse(b, format)
		if e != nil {
			return nil, e
		}
		k := Key(d)
		if _, ok := index[k]; ok {
			return nil, errors.New("duplicate resource identity")
		}
		index[k] = d
		snapshot.Documents = append(snapshot.Documents, d)
		snapshot.Paths = append(snapshot.Paths, p)
		if p == wp {
			snapshot.Workspace = d
		}
	}
	if snapshot.Workspace == nil || snapshot.Workspace.Kind != "Workspace" {
		return nil, errors.New("workspace file is not Workspace")
	}
	operations := map[string][]string{}
	identities := map[string]string{}
	for _, d := range snapshot.Documents {
		if d.Kind == "WorkItem" {
			identity := string(Encode(Map(d.Value["spec"])["identity"]))
			if previous, ok := identities[identity]; ok && previous != d.ID {
				return nil, errors.New("duplicate native item identity")
			}
			identities[identity] = d.ID
		}
		refs, assets := References(d)
		for _, ref := range refs {
			revision, _ := ref["revision"].(json.Number)
			normalized, e := AuthorRevision(revision)
			if e != nil {
				return nil, e
			}
			target, ok := index[key{String(ref["kind"]), String(ref["id"]), normalized}]
			if !ok {
				return nil, errors.New("missing referenced resource")
			}
			if Hash(target.Raw) != String(ref["sha256"]) {
				return nil, errors.New("reference byte hash mismatch")
			}
		}
		for _, asset := range assets {
			name := String(asset["path"])
			if name != "assets" && !strings.HasPrefix(name, "assets/") {
				return nil, errors.New("evidence must be beneath assets/")
			}
			p, e := Confined(r, name)
			if e != nil {
				return nil, e
			}
			b, ok := snapshot.Assets[name]
			if !ok {
				b, e = ReadFile(ctx, p)
				if e != nil {
					return nil, e
				}
				total += len(b)
				if total > MaxTotalBytes {
					return nil, errors.New("asset byte limit")
				}
				snapshot.Assets[name] = b
			}
			if Hash(b) != String(asset["sha256"]) {
				return nil, errors.New("asset hash mismatch")
			}
		}
		if d.Kind == "OperationPlan" {
			s := Map(d.Value["spec"])
			id := String(s["operationId"])
			if _, ok := operations[id]; ok {
				return nil, errors.New("duplicate operation ID")
			}
			operations[id] = []string{}
			for _, v := range List(s["dependsOn"]) {
				operations[id] = append(operations[id], String(v))
			}
		}
	}
	if e := CheckOperationGraph(operations); e != nil {
		return nil, e
	}
	// Re-read immutable inputs once to reject a mixed snapshot.
	for i, p := range snapshot.Paths {
		b, e := ReadFile(ctx, p)
		if e != nil || Hash(b) != Hash(snapshot.Documents[i].Raw) {
			return nil, fmt.Errorf("resource changed during resolution")
		}
	}
	for name, b := range snapshot.Assets {
		p, e := Confined(r, name)
		if e != nil {
			return nil, e
		}
		again, e := ReadFile(ctx, p)
		if e != nil || Hash(again) != Hash(b) {
			return nil, errors.New("asset changed during resolution")
		}
	}
	return snapshot, nil
}
func containsPath(paths []string, p string) bool {
	for _, x := range paths {
		if x == p {
			return true
		}
	}
	return false
}

func CheckOperationGraph(operations map[string][]string) error {
	done, active := map[string]bool{}, map[string]bool{}
	var visit func(string) error
	visit = func(id string) error {
		if active[id] {
			return errors.New("operation dependency cycle")
		}
		if done[id] {
			return nil
		}
		deps, ok := operations[id]
		if !ok {
			return errors.New("missing operation dependency")
		}
		active[id] = true
		for _, dep := range deps {
			if e := visit(dep); e != nil {
				return e
			}
		}
		delete(active, id)
		done[id] = true
		return nil
	}
	for id := range operations {
		if e := visit(id); e != nil {
			return e
		}
	}

	return nil
}
