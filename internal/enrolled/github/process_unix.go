//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package github

import (
	"context"
	"io"
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
func runBoundedProcess(ctx context.Context, cmd *exec.Cmd, limit int) ([]byte, bool, error) {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, false, err
	}
	if err = cmd.Start(); err != nil {
		return nil, false, err
	}
	finished := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			killProcessGroup(cmd)
		case <-finished:
		}
	}()
	var output limitedBuffer
	output.limit = limit
	copyDone := make(chan error, 1)
	go func() { _, e := io.Copy(&output, stdout); copyDone <- e }()
	waitErr := cmd.Wait()
	close(finished)
	killProcessGroup(cmd)
	copyErr := <-copyDone
	if waitErr == nil && copyErr != nil {
		waitErr = copyErr
	}
	return append([]byte(nil), output.Bytes()...), output.err != nil, waitErr
}
