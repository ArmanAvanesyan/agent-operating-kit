// Package evidence writes explicit, non-overwriting report directories.
package evidence

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// WriteJSON creates a new output directory containing one report. Existing
// output directories, symlink ancestors and overlap with protected roots fail.
// It never writes a caller's existing files. The parent directory must exist.
func WriteJSON(out, name string, value any, protectedRoots ...string) error {
	if out == "" || name == "" || filepath.Base(name) != name || strings.ContainsAny(name, `/\:`) || name == "." || name == ".." {
		return fmt.Errorf("invalid output directory or report name")
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	abs, err := filepath.Abs(out)
	if err != nil {
		return err
	}
	parent := filepath.Dir(abs)
	for p := parent; ; p = filepath.Dir(p) {
		info, err := os.Lstat(p)
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("output parent must be a real directory: %s", p)
		}
		if filepath.Dir(p) == p {
			break
		}
	}
	for _, root := range protectedRoots {
		if root == "" {
			continue
		}
		canonical, err := filepath.EvalSymlinks(root)
		if err != nil {
			return fmt.Errorf("cannot establish protected root: %w", err)
		}
		canonical, err = filepath.Abs(canonical)
		if err != nil {
			return err
		}
		if contains(canonical, abs) || contains(abs, canonical) {
			return fmt.Errorf("output overlaps protected input root")
		}
	}
	if err := os.Mkdir(abs, 0700); err != nil {
		return fmt.Errorf("output must be a new directory: %w", err)
	}
	path := filepath.Join(abs, name)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		_ = os.Remove(abs)
		return err
	}
	_, writeErr := f.Write(data)
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		_ = os.Remove(path)
		_ = os.Remove(abs)
		if writeErr != nil {
			return writeErr
		}
		return closeErr
	}
	return nil
}

func contains(root, child string) bool {
	relative, err := filepath.Rel(root, child)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
