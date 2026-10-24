//go:build windows

package github

import (
	"context"
	"errors"
	"os/exec"
)

func processGroupSupported() bool { return false }
func configureProcessGroup(*exec.Cmd) error {
	return errors.New("process group cleanup unavailable on Windows")
}
func killProcessGroup(*exec.Cmd) {}
func runBoundedProcess(context.Context, *exec.Cmd, int) ([]byte, bool, error) {
	return nil, false, errors.New("GitHub collector requires process group cleanup")
}
