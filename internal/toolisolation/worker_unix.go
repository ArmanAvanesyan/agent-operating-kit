//go:build linux || darwin

package toolisolation

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

func executePlatform(p Policy, q Request) (Result, error) {
	var out Result
	if err := ValidatePolicy(p); err != nil {
		return out, errors.New("request denied")
	}
	resolved, err := filepath.EvalSymlinks(p.Workspace)
	if err != nil || resolved != p.Workspace {
		return out, errors.New("request denied")
	}
	root, err := openWorkspace(p.Workspace)
	if err != nil {
		return out, errors.New("request denied")
	}
	defer unix.Close(root)
	return executeAtRoot(p, q, root)
}

func executePlatformFD(p Policy, q Request, root int) (Result, error) {
	var out Result
	var st unix.Stat_t
	if unix.Fstat(root, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFDIR || uint64(st.Dev) != p.WorkspaceDevice || st.Ino != p.WorkspaceInode {
		return out, errors.New("request denied")
	}
	return executeAtRoot(p, q, root)
}

func executeAtRoot(p Policy, q Request, root int) (Result, error) {
	var out Result
	name := q.Path
	if q.Operation == "create-target" {
		name = p.Target
	}
	parts := strings.Split(name, "/")
	dir := root
	for _, part := range parts[:len(parts)-1] {
		next, e := unix.Openat(dir, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if e != nil {
			if dir != root {
				unix.Close(dir)
			}
			return out, errors.New("request denied")
		}
		if dir != root {
			unix.Close(dir)
		}
		dir = next
	}
	if dir != root {
		defer unix.Close(dir)
	}
	base := parts[len(parts)-1]
	if q.Operation == "create-target" {
		fd, e := unix.Openat(dir, base, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0644)
		if e != nil {
			return out, errors.New("request denied")
		}
		defer unix.Close(fd)
		data := []byte(createFixture)
		if e = unix.Fchmod(fd, 0644); e != nil || writeAll(fd, data) != nil || unix.Fsync(fd) != nil {
			return out, errors.New("request denied")
		}
		var st unix.Stat_t
		if unix.Fstat(fd, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Nlink != 1 || st.Mode&0777 != 0644 || st.Size != int64(len(data)) {
			return out, errors.New("request denied")
		}
		h := sha256.Sum256(data)
		return Result{Operation: q.Operation, Path: p.Target, Size: len(data), SHA256: hex.EncodeToString(h[:])}, nil
	}
	fd, e := unix.Openat(dir, base, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if e != nil {
		return out, errors.New("request denied")
	}
	f := os.NewFile(uintptr(fd), "input")
	defer f.Close()
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Nlink != 1 || st.Size < 0 || st.Size > maxReadBytes {
		return out, errors.New("request denied")
	}
	b, e := io.ReadAll(io.LimitReader(f, maxReadBytes+1))
	if e != nil || len(b) > maxReadBytes {
		return out, errors.New("request denied")
	}
	h := sha256.Sum256(b)
	return Result{Operation: q.Operation, Path: q.Path, Size: len(b), SHA256: hex.EncodeToString(h[:]), Data: b}, nil
}

// Open every workspace path component from the filesystem root without following
// symlinks, so a path-component swap cannot redirect the worker outside it.
func openWorkspace(workspace string) (int, error) {
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, err
	}
	for _, part := range strings.Split(strings.TrimPrefix(workspace, "/"), "/") {
		if part == "" {
			continue
		}
		next, e := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		unix.Close(fd)
		if e != nil {
			return -1, e
		}
		fd = next
	}
	return fd, nil
}

func writeAll(fd int, b []byte) error {
	for len(b) > 0 {
		n, err := unix.Write(fd, b)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		b = b[n:]
	}
	return nil
}
