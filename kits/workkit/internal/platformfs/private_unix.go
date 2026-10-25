//go:build !windows

package platformfs

import (
	"errors"
	"os"
	"syscall"
)

func PrivateDirectory(path string) error {
	s, e := os.Lstat(path)
	if e != nil || !s.IsDir() || s.Mode().Perm()&0077 != 0 {
		return errors.New("private owned directory required")
	}
	st, ok := s.Sys().(*syscall.Stat_t)
	if !ok || int(st.Uid) != os.Geteuid() {
		return errors.New("directory owner mismatch")
	}
	return nil
}
func ProtectNewDirectory(path string) error { return PrivateDirectory(path) }
