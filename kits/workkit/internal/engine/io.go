package engine

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const MaxResources = 1024
const MaxTotalBytes = 64 << 20

func SafePath(p string) (string, error) {
	a, e := filepath.Abs(p)
	if e != nil {
		return "", e
	}
	a = filepath.Clean(a)
	for cur := a; ; cur = filepath.Dir(cur) {
		st, e := os.Lstat(cur)
		if e != nil {
			return "", e
		}
		if st.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("symlink input rejected")
		}
		if filepath.Dir(cur) == cur {
			break
		}
	}
	return a, nil
}
func Confined(root, rel string) (string, error) {
	if rel == "" || strings.ContainsAny(rel, "\\:") || filepath.IsAbs(rel) || filepath.VolumeName(rel) != "" || filepath.ToSlash(filepath.Clean(rel)) != rel || rel == ".." || strings.HasPrefix(rel, "../") {
		return "", errors.New("unsafe asset path")
	}
	return SafePath(filepath.Join(root, filepath.FromSlash(rel)))
}
func ReadFile(ctx context.Context, p string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	a, e := SafePath(p)
	if e != nil {
		return nil, e
	}
	before, e := os.Stat(a)
	if e != nil || !before.Mode().IsRegular() || before.Size() > MaxDocumentBytes {
		return nil, errors.New("unsafe or oversized input")
	}
	f, e := os.Open(a)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	opened, e := f.Stat()
	if e != nil || !os.SameFile(before, opened) {
		return nil, errors.New("input identity changed")
	}
	b, e := io.ReadAll(io.LimitReader(f, MaxDocumentBytes+1))
	if e != nil || len(b) > MaxDocumentBytes {
		return nil, errors.New("input read limit")
	}
	after, e := os.Stat(a)
	if e != nil || !os.SameFile(opened, after) || after.Size() != opened.Size() || !after.ModTime().Equal(opened.ModTime()) {
		return nil, errors.New("input changed during read")
	}
	if e = ctx.Err(); e != nil {
		return nil, e
	}
	return b, nil
}
func WriteBundle(ctx context.Context, out string, files map[string][]byte, source string) error {
	a, e := filepath.Abs(out)
	if e != nil {
		return e
	}
	if _, e = os.Lstat(a); e == nil || !os.IsNotExist(e) {
		return errors.New("output must be a new directory")
	}
	parent, e := SafePath(filepath.Dir(a))
	if e != nil {
		return e
	}
	a = filepath.Join(parent, filepath.Base(a))
	if source != "" {
		src, e := filepath.Abs(source)
		if e != nil {
			return e
		}
		r, e := filepath.Rel(src, a)
		if e == nil && (r == "." || r != ".." && !strings.HasPrefix(r, ".."+string(os.PathSeparator))) {
			return errors.New("output overlaps input root")
		}
	}
	names := make([]string, 0, len(files))
	total := 0
	for name, b := range files {
		if name == "" || filepath.IsAbs(name) || filepath.ToSlash(filepath.Clean(name)) != name || name == ".." || strings.HasPrefix(name, "../") || strings.ContainsAny(name, "\\:") || filepath.VolumeName(name) != "" {
			return errors.New("unsafe output name")
		}
		total += len(b)
		if total > MaxTotalBytes || len(files) > MaxResources {
			return errors.New("output bounds")
		}
		names = append(names, name)
	}
	sort.Strings(names)
	if e = os.Mkdir(a, 0700); e != nil {
		return e
	}
	// A bundle is complete only when its final seal is present.
	for _, name := range names {
		if name == "bundle.json" {
			continue
		}
		if e = writeOne(ctx, a, name, files[name]); e != nil {
			return e
		}
	}
	if b, ok := files["bundle.json"]; ok {
		return writeOne(ctx, a, "bundle.json", b)
	}
	return nil
}
func writeOne(ctx context.Context, root, name string, b []byte) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	p := filepath.Join(root, filepath.FromSlash(name))
	if e := os.MkdirAll(filepath.Dir(p), 0700); e != nil {
		return e
	}
	if _, e := SafePath(filepath.Dir(p)); e != nil {
		return e
	}
	f, e := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return e
	}
	_, e = f.Write(b)
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e != nil {
		return e
	}
	return ce
}
func ResourcePaths(ctx context.Context, root string) ([]string, error) {
	dir, e := Confined(root, "resources")
	if e != nil {
		return nil, e
	}
	paths := []string{}
	entries := 0
	e = filepath.WalkDir(dir, func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		entries++
		if entries > 4096 {
			return errors.New("directory entry limit")
		}
		if e := ctx.Err(); e != nil {
			return e
		}
		if d.Type()&os.ModeSymlink != 0 {
			return errors.New("resource symlink")
		}
		if d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(p))
		if ext == ".json" || ext == ".yaml" || ext == ".yml" {
			paths = append(paths, p)
		}
		if len(paths) > MaxResources {
			return errors.New("resource limit")
		}
		return nil
	})
	sort.Strings(paths)
	return paths, e
}

// BundleReservation consumes a fresh private output directory before external IO.
// Write is create-only and writes the completion manifest last.
type BundleReservation struct {
	root     string
	identity os.FileInfo
}

func ReserveBundle(ctx context.Context, out string) (*BundleReservation, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	a, e := filepath.Abs(out)
	if e != nil {
		return nil, e
	}
	parent, e := SafePath(filepath.Dir(a))
	if e != nil {
		return nil, e
	}
	a = filepath.Join(parent, filepath.Base(a))
	if e = os.Mkdir(a, 0700); e != nil {
		return nil, e
	}
	st, e := os.Stat(a)
	if e != nil {
		return nil, e
	}
	return &BundleReservation{a, st}, nil
}
func (w *BundleReservation) Write(ctx context.Context, files map[string][]byte) error {
	if w == nil {
		return errors.New("missing output reservation")
	}
	a, e := SafePath(w.root)
	if e != nil {
		return e
	}
	st, e := os.Stat(a)
	if e != nil || !os.SameFile(w.identity, st) {
		return errors.New("reserved output identity changed")
	}
	total := 0
	names := []string{}
	for name, b := range files {
		if name == "" || filepath.IsAbs(name) || strings.ContainsAny(name, "\\:") || filepath.ToSlash(filepath.Clean(name)) != name || name == ".." || strings.HasPrefix(name, "../") || len(b) > MaxDocumentBytes {
			return errors.New("unsafe reserved output")
		}
		total += len(b)
		names = append(names, name)
	}
	if total > MaxTotalBytes || len(files) > MaxResources {
		return errors.New("reserved output budget")
	}
	sort.Strings(names)
	for _, name := range names {
		if name != "bundle.json" {
			if e = writeOne(ctx, a, name, files[name]); e != nil {
				return e
			}
		}
	}
	if b, ok := files["bundle.json"]; ok {
		return writeOne(ctx, a, "bundle.json", b)
	}
	return nil
}
