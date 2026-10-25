package toolchain

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	a "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/accessv1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/engine"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type layoutFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Mode   uint32 `json:"mode"`
}

func installLayout(ctx context.Context, p a.InstallPlan, raw []byte, check func() error) (a.ToolPin, error) {
	var pin a.ToolPin
	if p.Artifact.Layout != "tree-v1" && p.Artifact.Layout != "azure-extension" {
		return pin, errors.New("unknown archive layout")
	}

	if p.Artifact.Format == "binary" {
		return pin, errors.New("tree archive required")
	}
	// Validate every entry/count/hash before creating any installation file.
	if _, e := Extract(raw, p.Artifact); e != nil {
		return pin, e
	}
	if e := check(); e != nil {
		return pin, e
	}
	if e := os.Mkdir(p.Destination, 0700); e != nil {
		return pin, errors.New("fresh layout directory required")
	}
	records := []layoutFile{}
	write := func(name string, mode os.FileMode, r io.Reader, size int64) error {
		if e := check(); e != nil {
			return e
		}
		if name == ".workkit-install.json" {
			return errors.New("reserved installation member")
		}
		if p.Artifact.Layout == "azure-extension" && !strings.HasPrefix(name, "azure-devops/") {
			name = "azure-devops/" + name
		}
		target := filepath.Join(p.Destination, filepath.FromSlash(name))
		if e := os.MkdirAll(filepath.Dir(target), 0700); e != nil {
			return e
		}
		if mode.IsDir() {
			return os.MkdirAll(target, 0700)
		}
		perm := os.FileMode(0600)
		if mode.Perm()&0111 != 0 || name == p.Artifact.Executable {
			perm = 0700
		}
		f, e := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, perm)
		if e != nil {
			return e
		}
		b, e := io.ReadAll(io.LimitReader(r, size+1))
		if e == nil && int64(len(b)) != size {
			e = errors.New("archive member size changed")
		}
		if e == nil {
			_, e = f.Write(b)
		}
		if e == nil {
			e = f.Sync()
		}
		ce := f.Close()
		if e != nil {
			return e
		}
		if ce != nil {
			return ce
		}
		records = append(records, layoutFile{name, engine.Hash(b), uint32(perm)})
		return nil
	}
	if p.Artifact.Format == "zip" {
		z, e := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
		if e != nil {
			return pin, e
		}
		for _, f := range z.File {
			if f.FileInfo().IsDir() {
				continue
			}
			r, e := f.Open()
			if e != nil {
				return pin, e
			}
			e = write(f.Name, f.Mode(), r, int64(f.UncompressedSize64))
			r.Close()
			if e != nil {
				return pin, e
			}
		}
	} else {
		g, e := gzip.NewReader(bytes.NewReader(raw))
		if e != nil {
			return pin, e
		}
		defer g.Close()
		t := tar.NewReader(g)
		for {
			h, e := t.Next()
			if e == io.EOF {
				break
			}
			if e != nil {
				return pin, e
			}
			if h.Typeflag == tar.TypeDir {
				continue
			}
			if e = write(h.Name, os.FileMode(h.Mode), t, h.Size); e != nil {
				return pin, e
			}
		}
	}
	if p.Action == "update" {
		hash, e := HashTree(ctx, p.PreviousPath)
		if e != nil || hash != p.PreviousSHA256 {
			return pin, errors.New("previous layout drift")
		}
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Path < records[j].Path })
	manifest, _ := json.Marshal(struct {
		Artifact string       `json:"artifactSha256"`
		Files    []layoutFile `json:"files"`
	}{p.Artifact.SHA256, records})
	if e := check(); e != nil {
		return pin, e
	}
	seal, e := os.OpenFile(filepath.Join(p.Destination, ".workkit-install.json"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return pin, e
	}
	_, e = seal.Write(manifest)
	if e == nil {
		e = seal.Sync()
	}
	seal.Close()
	if e != nil {
		return pin, e
	}
	executable := p.Artifact.Executable
	if p.Artifact.Layout == "azure-extension" && !strings.HasPrefix(executable, "azure-devops/") {
		executable = "azure-devops/" + executable
	}
	pin = a.ToolPin{ID: p.Artifact.ToolID, Path: filepath.Join(p.Destination, filepath.FromSlash(executable)), SHA256: p.Artifact.ExecutableSHA256, VersionOutput: p.Artifact.VersionOutput, Platform: p.Artifact.Platform, Extensions: []a.Extension{}}
	tree, e := HashTree(ctx, p.Destination)
	if e != nil {
		return pin, e
	}
	pin.Runtime = &a.LocalPin{Path: p.Destination, SHA256: tree}
	return pin, Verify(ctx, pin)
}
