package toolchain

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"errors"
	a "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/accessv1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/engine"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

func ValidateArtifact(a a.Artifact) error {
	u, e := url.Parse(a.URL)
	if e != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Port() != "" {
		return errors.New("invalid artifact URL")
	}
	official := a.ToolID == "gh" && u.Host == "github.com" && strings.HasPrefix(u.Path, "/cli/cli/releases/download/") || a.ToolID == "az" && (u.Host == "packages.microsoft.com" || u.Host == "azurecliprod.blob.core.windows.net" || a.Layout == "azure-extension" && u.Host == "github.com" && strings.HasPrefix(u.Path, "/Azure/azure-devops-cli-extension/releases/download/")) || a.ToolID == "acli" && u.Host == "acli.atlassian.com"
	if !official || len(a.SHA256) != 64 || len(a.ExecutableSHA256) != 64 || a.ToolVersion == "" || strings.ContainsAny(a.ToolVersion, "*<>= ") || a.ToolVersion == "latest" || a.Platform != Platform() || a.MaxBytes < 1 || a.MaxBytes > MaxBinaryBytes || a.VersionOutput == "" {
		return errors.New("exact official artifact required")
	}
	if a.Layout == "azure-extension" && a.ToolID != "az" {
		return errors.New("Azure extension layout requires az tool identity")
	}
	if a.Layout != "" && a.Layout != "tree-v1" && a.Layout != "azure-extension" {
		return errors.New("unsupported layout")
	}
	if a.Format != "binary" && a.Format != "zip" && a.Format != "tar.gz" {
		return errors.New("unsupported installer format")
	}
	for _, origin := range a.RedirectOrigins {
		u, e := url.Parse(origin)
		if e != nil || u.Scheme != "https" || u.Path != "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || !(a.ToolID == "gh" && (u.Host == "release-assets.githubusercontent.com" || u.Host == "objects.githubusercontent.com") || a.ToolID == "az" && (u.Host == "azurecliprod.blob.core.windows.net" || a.Layout == "azure-extension" && (u.Host == "release-assets.githubusercontent.com" || u.Host == "objects.githubusercontent.com")) || a.ToolID == "acli" && u.Host == "acli.atlassian.com") {
			return errors.New("unapproved redirect origin")
		}
	}
	return member(a.Executable)
}
func member(s string) error {
	if s == "" || strings.ContainsAny(s, "\\:") || path.IsAbs(s) || path.Clean(s) != s || s == ".." || strings.HasPrefix(s, "../") {
		return errors.New("unsafe archive member")
	}
	return nil
}
func Extract(raw []byte, a a.Artifact) ([]byte, error) {
	if int64(len(raw)) > a.MaxBytes || engine.Hash(raw) != a.SHA256 {
		return nil, errors.New("artifact integrity mismatch")
	}
	if a.Format == "binary" {
		if engine.Hash(raw) != a.ExecutableSHA256 {
			return nil, errors.New("executable hash mismatch")
		}
		return raw, nil
	}
	var result []byte
	seen := map[string]bool{}
	var total int64
	count := 0
	take := func(name string, size int64, regular bool, r io.Reader) error {
		name = strings.TrimSuffix(name, "/")
		count++
		if count > 4096 || member(name) != nil || seen[name] || !regular || size < 0 || size > MaxBinaryBytes {
			return errors.New("unsafe or duplicate archive member")
		}
		seen[name] = true
		total += size
		if total > MaxBinaryBytes {
			return errors.New("expanded archive bound")
		}
		if name != a.Executable {
			return nil
		}
		b, e := io.ReadAll(io.LimitReader(r, size+1))
		if e != nil || int64(len(b)) != size {
			return errors.New("truncated archive")
		}
		result = b
		return nil
	}
	if a.Format == "zip" {
		if e := zipBounds(raw); e != nil {
			return nil, e
		}
		z, e := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
		if e != nil {
			return nil, errors.New("invalid zip")
		}
		for _, f := range z.File {
			if f.FileInfo().IsDir() {
				if e := take(f.Name, 0, true, bytes.NewReader(nil)); e != nil {
					return nil, e
				}
				continue
			}
			r, e := f.Open()
			if e != nil {
				return nil, errors.New("zip member")
			}
			e = take(f.Name, int64(f.UncompressedSize64), f.Mode().IsRegular(), r)
			r.Close()
			if e != nil {
				return nil, e
			}
		}
	} else if a.Format == "tar.gz" {
		g, e := gzip.NewReader(bytes.NewReader(raw))
		if e != nil {
			return nil, errors.New("invalid gzip")
		}
		defer g.Close()
		t := tar.NewReader(g)
		for {
			h, e := t.Next()
			if e == io.EOF {
				break
			}
			if e != nil {
				return nil, errors.New("invalid tar")
			}
			if h.Typeflag == tar.TypeDir {
				if e := take(h.Name, 0, true, bytes.NewReader(nil)); e != nil {
					return nil, e
				}
				continue
			}
			if e = take(h.Name, h.Size, h.Typeflag == tar.TypeReg, t); e != nil {
				return nil, e
			}
		}
	} else {
		return nil, errors.New("unsupported archive")
	}
	if len(result) == 0 || engine.Hash(result) != a.ExecutableSHA256 {
		return nil, errors.New("missing or changed executable")
	}
	return result, nil
}

// Install uses one artifact request and an adjacent create-only lock. A failed
// or interrupted update retains its lock/backup for operator reconciliation.
func Install(ctx context.Context, p a.InstallPlan, check func() error) (a.ToolPin, error) {
	var pin a.ToolPin
	if e := ValidateArtifact(p.Artifact); e != nil {
		return pin, e
	}
	if p.Action != "install" && p.Action != "update" {
		return pin, errors.New("unknown install action")
	}
	if !filepath.IsAbs(p.Destination) || filepath.Clean(p.Destination) != p.Destination {
		return pin, errors.New("exact installation path required")
	}
	if _, e := engine.SafePath(filepath.Dir(p.Destination)); e != nil {
		return pin, e
	}
	if e := check(); e != nil {
		return pin, e
	}
	lock, e := os.OpenFile(p.Destination+".workkit-lock", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return pin, errors.New("installation locked; reconcile")
	}
	lock.Close()
	success := false
	defer func() {
		if success {
			os.Remove(p.Destination + ".workkit-lock")
		}
	}()
	if _, e = os.Lstat(p.Destination); !os.IsNotExist(e) {
		return pin, errors.New("new side-by-side destination required")
	}
	if p.Action == "install" {
		if p.PreviousSHA256 != "" || p.PreviousPath != "" {
			return pin, errors.New("unexpected prior installation")
		}
	} else {
		if p.PreviousPath == p.Destination {
			return pin, errors.New("in-place updates prohibited")
		}
		var h string
		var e error
		if p.Artifact.Layout != "" {
			h, e = HashTree(ctx, p.PreviousPath)
		} else {
			h, e = HashFile(ctx, p.PreviousPath, MaxBinaryBytes)
		}
		if e != nil || h != p.PreviousSHA256 {
			return pin, errors.New("update baseline drift")
		}
	}

	u, _ := url.Parse(p.Artifact.URL)
	req, e := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if e != nil {
		return pin, e
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	client := &http.Client{Transport: transport, Timeout: 30 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > 3 || req.URL.Scheme != "https" || req.URL.User != nil {
			return errors.New("redirect rejected")
		}
		origin := "https://" + req.URL.Host
		for _, allowed := range p.Artifact.RedirectOrigins {
			if origin == allowed {
				return check()
			}
		}
		return errors.New("redirect origin rejected")
	}}
	defer transport.CloseIdleConnections()
	if e = check(); e != nil {
		return pin, e
	}
	resp, e := client.Do(req)
	if e != nil {
		return pin, errors.New("artifact unavailable; no fallback")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return pin, errors.New("artifact fetch rejected")
	}
	raw, e := io.ReadAll(io.LimitReader(resp.Body, p.Artifact.MaxBytes+1))
	if e != nil {
		return pin, errors.New("artifact read failed")
	}
	if p.Artifact.Layout != "" {
		result, e := installLayout(ctx, p, raw, check)
		if e == nil {
			success = true
		}
		return result, e
	}
	executable, e := Extract(raw, p.Artifact)
	if e != nil {
		return pin, e
	}
	temp, e := os.OpenFile(p.Destination+".workkit-new", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
	if e != nil {
		return pin, errors.New("staged target exists")
	}
	_, e = temp.Write(executable)
	if e == nil {
		e = temp.Sync()
	}
	closeErr := temp.Close()
	if e != nil || closeErr != nil {
		return pin, errors.New("stage failed")
	}
	if e = check(); e != nil {
		return pin, e
	}
	if p.Action == "update" {
		var h string
		var e error
		if p.Artifact.Layout != "" {
			h, e = HashTree(ctx, p.PreviousPath)
		} else {
			h, e = HashFile(ctx, p.PreviousPath, MaxBinaryBytes)
		}
		if e != nil || h != p.PreviousSHA256 {
			return pin, errors.New("update baseline changed")
		}
	}

	// Hard link is create-only; a competing replacement never gets overwritten.
	if e = os.Link(p.Destination+".workkit-new", p.Destination); e != nil {
		return pin, errors.New("target changed; reconcile staged file and backup")
	}
	os.Remove(p.Destination + ".workkit-new")
	pin = a.ToolPin{ID: p.Artifact.ToolID, Path: p.Destination, SHA256: p.Artifact.ExecutableSHA256, VersionOutput: p.Artifact.VersionOutput, Platform: p.Artifact.Platform, Extensions: []a.Extension{}}
	if e = Verify(ctx, pin); e != nil {
		return pin, e
	}
	success = true
	return pin, nil
}

// Inspect the complete bounded central directory before archive/zip allocates
// its entries. Reject ZIP64/multi-disk inputs rather than guessing limits.
func zipBounds(raw []byte) error {
	start := len(raw) - 65557
	if start < 0 {
		start = 0
	}
	end := -1
	for i := len(raw) - 22; i >= start; i-- {
		if binary.LittleEndian.Uint32(raw[i:i+4]) == 0x06054b50 && i+22+int(binary.LittleEndian.Uint16(raw[i+20:i+22])) == len(raw) {
			end = i
			break
		}
	}
	if end < 0 {
		return errors.New("zip end record missing")
	}
	if binary.LittleEndian.Uint16(raw[end+4:end+6]) != 0 || binary.LittleEndian.Uint16(raw[end+6:end+8]) != 0 {
		return errors.New("multi-disk zip unsupported")
	}
	count := int(binary.LittleEndian.Uint16(raw[end+10 : end+12]))
	size := int64(binary.LittleEndian.Uint32(raw[end+12 : end+16]))
	offset := int64(binary.LittleEndian.Uint32(raw[end+16 : end+20]))
	if count > 4096 || int(binary.LittleEndian.Uint16(raw[end+8:end+10])) != count || size > 8<<20 || offset+size != int64(end) || offset < 0 {
		return errors.New("zip directory bounds")
	}
	cursor := offset
	actual := 0
	for cursor < int64(end) {
		if cursor+46 > int64(end) || binary.LittleEndian.Uint32(raw[cursor:cursor+4]) != 0x02014b50 {
			return errors.New("invalid zip directory")
		}
		extra := int64(binary.LittleEndian.Uint16(raw[cursor+28:cursor+30])) + int64(binary.LittleEndian.Uint16(raw[cursor+30:cursor+32])) + int64(binary.LittleEndian.Uint16(raw[cursor+32:cursor+34]))
		cursor += 46 + extra
		actual++
		if actual > 4096 || cursor > int64(end) {
			return errors.New("zip member bound")
		}
	}
	if actual != count {
		return errors.New("zip member count mismatch")
	}
	return nil
}
