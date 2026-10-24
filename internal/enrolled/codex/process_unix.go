//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package codex

import (
	"os/exec"
	"syscall"
)

func processGroupSupported() bool { return true }
func configureProcessGroup(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return nil
}
func killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
