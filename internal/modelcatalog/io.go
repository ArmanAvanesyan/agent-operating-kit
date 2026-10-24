package modelcatalog

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
)

// Decode parses bounded, duplicate-free JSON with exact number lexemes.
func Decode(data []byte, target any) error {
	if err := contracts.ValidateJSON(data); err != nil {
		return err
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		return err
	}
	return nil
}

func ReadFile(path string) ([]byte, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, errors.New("document must be a regular file")
	}
	if before.Mode()&fs.ModeSymlink != 0 {
		return nil, errors.New("symlink documents are forbidden")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	after, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(before, after) || !after.Mode().IsRegular() {
		return nil, errors.New("document changed while opening")
	}
	b, err := ioReadBounded(f, contracts.MaxDocumentBytes)
	if err != nil {
		return nil, err
	}
	return b, nil
}

func ioReadBounded(r interface{ Read([]byte) (int, error) }, limit int) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, int64(limit+1)))
	if err != nil {
		return nil, err
	}
	if len(b) > limit {
		return nil, errors.New("document exceeds 4 MiB")
	}
	return b, nil
}

// ReadAsset confines a portable relative path to root and rejects symlinks in
// every existing component, including the final file.
func ReadAsset(root, rel string) ([]byte, error) {
	if rel == "" || filepath.IsAbs(rel) || strings.Contains(rel, "\\") || filepath.VolumeName(rel) != "" || strings.Contains(rel, ":") {
		return nil, errors.New("asset path must be portable and relative")
	}
	for _, part := range strings.Split(rel, "/") {
		if part == "" || part == "." || part == ".." {
			return nil, errors.New("asset path must use canonical relative components")
		}
	}
	clean := filepath.Clean(filepath.FromSlash(rel))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return nil, errors.New("asset path escapes bundle")
	}
	r, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	rootInfo, err := os.Lstat(r)
	if err != nil {
		return nil, err
	}
	if rootInfo.Mode()&fs.ModeSymlink != 0 || !rootInfo.IsDir() {
		return nil, errors.New("bundle root must be a real directory")
	}
	p := filepath.Join(r, clean)
	cur := r
	parts := strings.Split(clean, string(filepath.Separator))
	for i, part := range parts {
		cur = filepath.Join(cur, part)
		info, e := os.Lstat(cur)
		if e != nil {
			return nil, e
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			return nil, errors.New("symlink asset paths are forbidden")
		}
		if i < len(parts)-1 && !info.IsDir() {
			return nil, errors.New("asset parent is not a directory")
		}
	}
	resolved, err := filepath.Abs(p)
	if err != nil {
		return nil, err
	}
	relpath, err := filepath.Rel(r, resolved)
	if err != nil || relpath == ".." || strings.HasPrefix(relpath, ".."+string(filepath.Separator)) {
		return nil, errors.New("asset path escapes bundle")
	}
	return ReadFile(p)
}
