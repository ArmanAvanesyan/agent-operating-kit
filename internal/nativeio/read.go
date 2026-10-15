// Package nativeio captures only explicitly supplied native files. Directory
// inputs require a local inventory manifest; no ambient configuration discovery
// or imported command execution is performed.
package nativeio

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/evidence"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/nativeadapter"
)

const ManifestName = "aok-native-input.json"

type Manifest struct {
	Version string  `json:"version"`
	Files   []Entry `json:"files"`
}
type Entry struct {
	Path   string `json:"path"`
	Format string `json:"format"`
}

// ReadFile rejects symlinks and special files and bounds every read. The
// explicit top-level input's parent may be a filesystem alias such as /tmp.
func ReadFile(path string) ([]byte, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("input cannot be inspected")
	}
	if !before.Mode().IsRegular() || before.Size() > contracts.MaxDocumentBytes {
		return nil, fmt.Errorf("input must be a bounded regular non-symlink file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("input cannot be opened")
	}
	defer f.Close()
	actual, err := f.Stat()
	if err != nil || !actual.Mode().IsRegular() || !os.SameFile(before, actual) {
		return nil, fmt.Errorf("input changed while opening")
	}
	data, err := io.ReadAll(io.LimitReader(f, contracts.MaxDocumentBytes+1))
	if err != nil || len(data) > contracts.MaxDocumentBytes {
		return nil, fmt.Errorf("input exceeds read limit or cannot be read")
	}
	return data, nil
}

func Load(input string) ([]nativeadapter.File, error) {
	info, err := os.Lstat(input)
	if err != nil {
		return nil, fmt.Errorf("explicit input does not exist")
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("explicit input must not be a symlink")
	}
	if !info.IsDir() {
		data, err := ReadFile(input)
		if err != nil {
			return nil, err
		}
		name := filepath.Base(input)
		if err := evidence.CheckPath(name); err != nil {
			return nil, err
		}
		return []nativeadapter.File{{Path: name, Format: format(name), Data: data}}, nil
	}
	data, err := ReadFile(filepath.Join(input, ManifestName))
	if err != nil {
		return nil, fmt.Errorf("directory input requires a bounded " + ManifestName)
	}
	if _, err = contracts.DecodeJSON(data); err != nil {
		return nil, fmt.Errorf("invalid native input manifest JSON")
	}
	var manifest Manifest
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err = dec.Decode(&manifest); err != nil || manifest.Version != "aok-native-input/v1" || len(manifest.Files) == 0 || len(manifest.Files) > 128 {
		return nil, fmt.Errorf("invalid native input manifest")
	}
	seen := map[string]bool{}
	files := []nativeadapter.File{}
	total := 0
	for _, entry := range manifest.Files {
		if err := evidence.CheckPath(entry.Path); err != nil {
			return nil, err
		}
		key := strings.ToLower(entry.Path)
		if seen[key] {
			return nil, fmt.Errorf("duplicate input path")
		}
		seen[key] = true
		if entry.Path == ManifestName {
			return nil, fmt.Errorf("manifest cannot include itself")
		}
		p := input
		for _, part := range strings.Split(entry.Path, "/") {
			p = filepath.Join(p, part)
			info, err := os.Lstat(p)
			if err != nil || info.Mode()&os.ModeSymlink != 0 {
				return nil, fmt.Errorf("native input path is missing or contains a symlink")
			}
		}
		b, err := ReadFile(p)
		if err != nil {
			return nil, err
		}
		total += len(b)
		if total > contracts.MaxDocumentBytes {
			return nil, fmt.Errorf("native input set exceeds 4 MiB")
		}
		f := entry.Format
		if f == "" {
			f = format(entry.Path)
		}
		files = append(files, nativeadapter.File{Path: entry.Path, Format: f, Data: b})
	}
	return files, nil
}
func format(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".md":
		return "markdown"
	case ".json":
		return "json"
	case ".jsonc":
		return "jsonc"
	case ".toml":
		return "toml"
	case ".yaml", ".yml":
		return "yaml"
	default:
		return "unknown"
	}
}
