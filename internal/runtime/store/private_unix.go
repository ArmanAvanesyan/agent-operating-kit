//go:build !windows

package store

import (
	"fmt"
	"os"
)

func secureDirectory(path string) error {
	i, e := os.Lstat(path)
	if e != nil {
		return e
	}
	if i.Mode().Perm()&0077 != 0 {
		return fmt.Errorf("runtime directory must be private (0700)")
	}
	return nil
}
func secureFile(path string) error {
	i, e := os.Lstat(path)
	if e != nil {
		return e
	}
	if i.Mode().Perm()&0077 != 0 {
		return fmt.Errorf("runtime file must be private (0600)")
	}
	return nil
}
