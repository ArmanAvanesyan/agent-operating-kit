//go:build !windows

package store

import (
	"fmt"
	"os"
)

func secureDirectory(path string) error {
	info, e := os.Lstat(path)
	if e != nil {
		return e
	}
	if info.Mode().Perm()&0077 != 0 {
		return fmt.Errorf("accounting directory must be private (0700)")
	}
	return nil
}
func secureFile(path string) error {
	info, e := os.Lstat(path)
	if e != nil {
		return e
	}
	if info.Mode().Perm()&0077 != 0 {
		return fmt.Errorf("accounting file must be private (0600)")
	}
	return nil
}
