//go:build !windows

package hostbridge

import (
	"os/exec"
	"syscall"
)

func configureProcess(c *exec.Cmd) error {
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return nil
}
func attachProcess(*exec.Cmd) error    { return nil }
func processHandle(*exec.Cmd) uintptr  { return 0 }
func finishProcess(*exec.Cmd, uintptr) {}
func stopProcess(c *exec.Cmd, _ uintptr) error {
	if c.Process != nil {
		_ = syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
	}
	return nil
}
