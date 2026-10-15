package agentvalidate

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
)

// Validate loads and statically checks an operational corpus rooted at dir.
func Validate(dir string) Report {
	corpus, report := Load(dir)
	if corpus != nil {
		report.Diagnostics = append(report.Diagnostics, semanticChecks(corpus)...)
	}
	sortDiagnostics(report.Diagnostics)
	report.finish()
	return report
}

// Load reads canonical documents and records every in-root regular file for
// later reference resolution. The assets subtree is supporting data, not a
// resource namespace.
func Load(dir string) (*Corpus, Report) {
	report := Report{Diagnostics: []contracts.Diagnostic{}}
	abs, err := filepath.Abs(dir)
	if err != nil {
		report.add("corpus.root", dir, "", "error", err.Error())
		report.finish()
		return nil, report
	}
	rootInfo, err := os.Lstat(abs)
	if err != nil {
		report.add("corpus.root", filepath.ToSlash(abs), "", "error", err.Error())
		report.finish()
		return nil, report
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		report.add("corpus.root", filepath.ToSlash(abs), "", "error", "corpus root must be a real directory")
		report.finish()
		return nil, report
	}
	rootReal, err := filepath.EvalSymlinks(abs)
	if err != nil {
		report.add("corpus.root", filepath.ToSlash(abs), "", "error", err.Error())
		report.finish()
		return nil, report
	}
	c := &Corpus{Root: rootReal, Files: map[string]struct{}{}}
	paths := []string{}
	err = filepath.WalkDir(rootReal, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			report.add("corpus.read", rel(rootReal, path), "", "error", walkErr.Error())
			return nil
		}
		if path != rootReal && d.Type()&os.ModeSymlink != 0 {
			report.add("corpus.symlink", rel(rootReal, path), "", "error", "symbolic links are forbidden in the corpus")
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		info, e := d.Info()
		if e != nil {
			report.add("corpus.stat", rel(rootReal, path), "", "error", e.Error())
			return nil
		}
		if info.Mode().IsRegular() {
			rp := rel(rootReal, path)
			c.Files[filepath.ToSlash(rp)] = struct{}{}
			if strings.EqualFold(filepath.Ext(path), ".json") && !inAssets(rp) {
				paths = append(paths, path)
			}
		}
		return nil
	})
	if err != nil {
		report.add("corpus.walk", "", "", "error", err.Error())
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		report.add("corpus.empty", filepath.ToSlash(rootReal), "", "error", "corpus contains no operational JSON documents")
	}
	for _, path := range paths {
		rp := filepath.ToSlash(rel(rootReal, path))
		info, e := os.Stat(path)
		if e != nil {
			report.add("document.stat", rp, "", "error", e.Error())
			continue
		}
		if info.Size() > contracts.MaxDocumentBytes {
			report.add("document.size", rp, "", "error", "document exceeds 4 MiB")
			continue
		}
		b, e := readBounded(path)
		if e != nil {
			report.add("document.read", rp, "", "error", e.Error())
			continue
		}
		if len(b) > contracts.MaxDocumentBytes {
			report.add("document.size", rp, "", "error", "document exceeds 4 MiB")
			continue
		}
		parsed, ds, e := contracts.Parse(b)
		if e != nil {
			if len(ds) == 0 {
				report.add("document.parse", rp, "", "error", e.Error())
			} else {
				for _, d := range ds {
					d.Source = rp
					report.Diagnostics = append(report.Diagnostics, d)
				}
			}
			continue
		}
		r, ok := parsed.(contracts.Resource)
		if !ok {
			report.add("document.type", rp, "", "error", fmt.Sprintf("contracts.Parse returned %T", parsed))
			continue
		}
		var obj map[string]any
		dec := json.NewDecoder(strings.NewReader(string(b)))
		dec.UseNumber()
		if e = dec.Decode(&obj); e != nil {
			report.add("document.decode", rp, "", "error", e.Error())
			continue
		}
		c.Resources = append(c.Resources, LoadedResource{Resource: r, Path: rp, Object: obj})
	}
	sortDiagnostics(report.Diagnostics)
	report.finish()
	return c, report
}

func rel(root, path string) string {
	r, e := filepath.Rel(root, path)
	if e != nil {
		return path
	}
	return r
}
func readBounded(path string) ([]byte, error) {
	info, e := os.Lstat(path)
	if e != nil {
		return nil, e
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("input must be a regular non-symlink file")
	}
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	opened, e := f.Stat()
	if e != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return nil, fmt.Errorf("input changed while opening")
	}
	b, e := io.ReadAll(io.LimitReader(f, contracts.MaxDocumentBytes+1))
	if e != nil {
		return nil, e
	}
	if len(b) > contracts.MaxDocumentBytes {
		return nil, fmt.Errorf("file exceeds 4 MiB")
	}
	return b, nil
}
func inAssets(path string) bool {
	p := filepath.ToSlash(path)
	return p == "assets" || strings.HasPrefix(p, "assets/")
}

// ResolveFile ensures a POSIX relative reference stays within the corpus and
// resolves to a regular non-symlink file. Paths are anchored at the owner.
func (c *Corpus) ResolveFile(owner, ref string) (string, error) {
	if ref == "" || strings.Contains(ref, "\\") || filepath.IsAbs(ref) || strings.HasPrefix(ref, "/") {
		return "", fmt.Errorf("file reference must be a relative POSIX path")
	}
	for _, p := range strings.Split(ref, "/") {
		if p == ".." || p == "." || p == "" {
			return "", fmt.Errorf("file reference contains a non-canonical path component")
		}
	}
	base := filepath.Dir(filepath.Join(c.Root, filepath.FromSlash(owner)))
	joined := filepath.Clean(filepath.Join(base, filepath.FromSlash(ref)))
	relpath, e := filepath.Rel(c.Root, joined)
	if e != nil || relpath == ".." || strings.HasPrefix(relpath, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("file reference escapes the corpus root")
	}
	cur := c.Root
	for _, p := range strings.Split(relpath, string(filepath.Separator)) {
		cur = filepath.Join(cur, p)
		info, e := os.Lstat(cur)
		if e != nil {
			return "", e
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("file reference traverses a symbolic link")
		}
		if cur != joined && !info.IsDir() {
			return "", fmt.Errorf("file reference parent is not a directory")
		}
	}
	info, e := os.Lstat(joined)
	if e != nil {
		return "", e
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("file reference is not a regular file")
	}
	if info.Size() > contracts.MaxDocumentBytes {
		return "", fmt.Errorf("referenced file exceeds 4 MiB")
	}
	return joined, nil
}

func sortDiagnostics(ds []contracts.Diagnostic) {
	sort.Slice(ds, func(i, j int) bool {
		a, b := ds[i], ds[j]
		if a.Source != b.Source {
			return a.Source < b.Source
		}
		if a.Field != b.Field {
			return a.Field < b.Field
		}
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		if a.Severity != b.Severity {
			return a.Severity < b.Severity
		}
		return a.Message < b.Message
	})
}
