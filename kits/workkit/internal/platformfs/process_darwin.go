//go:build darwin

package platformfs

import (
	"fmt"
	"golang.org/x/sys/unix"
	"os"
)

func ProcessStart() (string, error) { return ProcessStartFor(os.Getpid()) }
func ProcessStartFor(pid int) (string, error) {
	info, e := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if e != nil {
		return "", e
	}
	return fmt.Sprintf("darwin:%d:%d", info.Proc.P_starttime.Sec, info.Proc.P_starttime.Usec), nil
}
