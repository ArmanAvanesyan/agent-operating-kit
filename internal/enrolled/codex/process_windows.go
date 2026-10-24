//go:build windows

package codex

import (
	"errors"
	"os/exec"
)

func processGroupSupported() bool { return false }
func configureProcessGroup(*exec.Cmd) error {
	return errors.New("process group cleanup unavailable on Windows")
}
func killProcessGroup(*exec.Cmd) {}
