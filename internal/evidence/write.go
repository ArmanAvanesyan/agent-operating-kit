// Package evidence writes explicit, non-overwriting report directories.
package evidence

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
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
	return WriteFiles(out, map[string][]byte{name: data}, protectedRoots...)
}

// WriteFiles writes a fully validated relative file set to a new private directory.
// It rejects portable path collisions before creating anything. On failure it
// removes only the newly created directory, never existing content.
func WriteFiles(out string, files map[string][]byte, protectedRoots ...string) error {
	if out == "" || len(files) == 0 {
		return fmt.Errorf("output directory and files are required")
	}
	names := make([]string, 0, len(files))
	seen := map[string]bool{}
	for name := range files {
		if err := CheckPath(name); err != nil {
			return err
		}
		lower := strings.ToLower(name)
		if seen[lower] {
			return fmt.Errorf("case-insensitive output collision")
		}
		seen[lower] = true
		names = append(names, name)
	}
	sort.Strings(names)
	for name := range seen {
		for p := path.Dir(name); p != "."; p = path.Dir(p) {
			if seen[p] {
				return fmt.Errorf("file/directory output collision")
			}
		}
	}
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

	complete := false
	defer func() {
		if !complete {
			_ = os.RemoveAll(abs)
		}
	}()
	for _, name := range names {
		dest := filepath.Join(abs, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
			return err
		}
		f, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		_, writeErr := f.Write(files[name])
		closeErr := f.Close()
		if writeErr != nil {
			return writeErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	complete = true
	return nil
}

// CheckPath accepts portable, relative POSIX file names only.
func CheckPath(name string) error {
	if name == "" || path.IsAbs(name) || path.Clean(name) != name || name == "." || name == ".." || strings.HasPrefix(name, "../") || strings.ContainsAny(name, "\\:\x00<>\"|?*") {
		return fmt.Errorf("invalid relative output path")
	}
	for _, part := range strings.Split(name, "/") {
		if strings.TrimRight(part, ". ") != part {
			return fmt.Errorf("nonportable file name")
		}
		for _, r := range part {
			if r < 32 || r == 127 {
				return fmt.Errorf("control character in file name")
			}
		}
		stem := strings.ToUpper(strings.SplitN(part, ".", 2)[0])
		if stem == "CON" || stem == "PRN" || stem == "AUX" || stem == "NUL" || (len(stem) == 4 && (strings.HasPrefix(stem, "COM") || strings.HasPrefix(stem, "LPT")) && stem[3] >= '1' && stem[3] <= '9') {
			return fmt.Errorf("reserved file name")
		}
	}
	return nil
}

func contains(root, child string) bool {
	relative, err := filepath.Rel(root, child)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
