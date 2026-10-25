package toolchain

import (
	"context"
	"encoding/json"
	"errors"
	a "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/accessv1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/engine"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func HashTree(ctx context.Context, root string) (string, error) {
	if !filepath.IsAbs(root) {
		return "", errors.New("absolute tree required")
	}
	if _, e := engine.SafePath(root); e != nil {
		return "", e
	}
	records := []struct {
		Path, SHA256 string
		Mode         uint32
	}{}
	var total int64
	directories := map[string]os.FileInfo{}
	entries := 0
	e := filepath.WalkDir(root, func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if e = ctx.Err(); e != nil {
			return e
		}
		entries++
		if entries > 4096 {
			return errors.New("tree entry bound")
		}
		if d.Type()&os.ModeSymlink != 0 {
			return errors.New("tree symlink rejected")
		}
		if d.IsDir() {
			st, e := d.Info()
			if e != nil {
				return e
			}
			directories[p] = st
			rel, _ := filepath.Rel(root, p)
			records = append(records, struct {
				Path, SHA256 string
				Mode         uint32
			}{filepath.ToSlash(rel), "directory", uint32(st.Mode().Perm())})
			return nil
		}
		st, e := d.Info()
		if e != nil || !st.Mode().IsRegular() {
			return errors.New("unsafe tree member")
		}
		total += st.Size()
		if total > MaxBinaryBytes || len(records) >= 4096 {
			return errors.New("tree bound")
		}
		hash, e := HashFile(ctx, p, MaxBinaryBytes)
		if e != nil {
			return e
		}
		rel, _ := filepath.Rel(root, p)
		records = append(records, struct {
			Path, SHA256 string
			Mode         uint32
		}{filepath.ToSlash(rel), hash, uint32(st.Mode().Perm())})
		return nil
	})
	if e != nil {
		return "", e
	}
	for p, before := range directories {
		after, e := os.Stat(p)
		if e != nil || !os.SameFile(before, after) || !after.ModTime().Equal(before.ModTime()) || after.Mode() != before.Mode() {
			return "", errors.New("tree changed during inspection")
		}
	}
	for _, r := range records {
		if r.SHA256 == "directory" {
			continue
		}
		sha, e := HashFile(ctx, filepath.Join(root, filepath.FromSlash(r.Path)), MaxBinaryBytes)
		if e != nil || sha != r.SHA256 {
			return "", errors.New("tree file changed")
		}
	}
	raw, _ := json.Marshal(records)
	return engine.Hash(raw), nil
}
func Discover(ctx context.Context, r a.DiscoveryRequest) ([]a.DiscoveryFinding, error) {
	if r.Version != a.Version || r.Kind != "ToolDiscovery" || len(r.Roots) == 0 || len(r.Roots) > 32 || len(r.Tools) == 0 || len(r.Tools) > 4 {
		return nil, errors.New("bounded explicit discovery required")
	}
	out := []a.DiscoveryFinding{}
	var total int64
	seen := map[string]bool{}
	for _, root := range r.Roots {
		if !filepath.IsAbs(root) {
			return nil, errors.New("absolute search root required")
		}
		if _, e := engine.SafePath(root); e != nil {
			return nil, e
		}
		for _, id := range r.Tools {
			if id != "gh" && id != "az" && id != "acli" {
				return nil, errors.New("unregistered tool")
			}
			for _, name := range []string{id, id + ".exe", id + ".cmd"} {
				p := filepath.Join(root, name)
				if seen[p] {
					continue
				}
				seen[p] = true
				if _, e := os.Lstat(p); os.IsNotExist(e) {
					continue
				}
				if e := ctx.Err(); e != nil {
					return nil, e
				}
				if st, e := os.Lstat(p); e == nil && st.Mode().IsRegular() {
					total += st.Size()
					if total > MaxBinaryBytes {
						return nil, errors.New("aggregate discovery byte bound")
					}
				}
				hash, e := HashFile(ctx, p, MaxBinaryBytes)
				if e := ctx.Err(); e != nil {
					return nil, e
				}
				status := "discovered-unverified"
				if e != nil {
					hash = ""
					status = "unsafe-or-unreadable"
				}
				out = append(out, a.DiscoveryFinding{ID: id, Path: p, SHA256: hash, Platform: Platform(), Status: status})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return strings.Compare(out[i].Path, out[j].Path) < 0 })
	return out, nil
}
