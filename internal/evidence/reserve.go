package evidence

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Reservation creates an immutable launch intent before a potentially
// side-effectful host operation. A failed launch leaves its intent inspectable.
type Reservation struct {
	root string
	info os.FileInfo
}

func Reserve(out string, protected ...string) (*Reservation, error) {
	if e := WriteFiles(out, map[string][]byte{"launch-intent.json": []byte("{\"status\":\"starting\",\"authorizationGranted\":false,\"executionReady\":false}\n")}, protected...); e != nil {
		return nil, e
	}
	abs, e := filepath.Abs(out)
	if e != nil {
		return nil, e
	}
	info, e := os.Lstat(abs)
	if e != nil {
		return nil, e
	}
	return &Reservation{abs, info}, nil
}
func (r *Reservation) Write(files map[string][]byte) error {
	st, e := os.Lstat(r.root)
	if e != nil || !os.SameFile(st, r.info) || !st.IsDir() {
		return fmt.Errorf("reserved evidence directory changed")
	}
	names := []string{}
	seen := map[string]bool{}
	for name := range files {
		if e = CheckPath(name); e != nil {
			return e
		}
		if name == "launch-intent.json" {
			return fmt.Errorf("reserved launch intent cannot be replaced")
		}
		lower := strings.ToLower(name)
		if seen[lower] {
			return fmt.Errorf("reserved evidence path collision")
		}
		seen[lower] = true
		names = append(names, name)
	}
	for name := range seen {
		for parent := filepath.ToSlash(filepath.Dir(name)); parent != "."; parent = filepath.ToSlash(filepath.Dir(parent)) {
			if seen[strings.ToLower(parent)] {
				return fmt.Errorf("reserved evidence file/directory collision")
			}
		}
	}
	sort.Strings(names)
	for _, name := range names {
		path := filepath.Join(r.root, filepath.FromSlash(name))
		parent := filepath.Dir(path)
		if e = os.MkdirAll(parent, 0700); e != nil {
			return e
		}
		for p := parent; ; p = filepath.Dir(p) {
			i, err := os.Lstat(p)
			if err != nil || !i.IsDir() || i.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("reserved evidence path changed")
			}
			if p == r.root {
				break
			}
		}
		f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return e
		}
		_, e = f.Write(files[name])
		closeErr := f.Close()
		if e != nil {
			return e
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}
