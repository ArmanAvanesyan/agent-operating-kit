//go:build linux

package platformfs

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

func ProcessStart() (string, error) { return ProcessStartFor(os.Getpid()) }
func ProcessStartFor(pid int) (string, error) {
	raw, e := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if e != nil || len(raw) > 8192 {
		return "", errors.New("process start unavailable")
	}
	end := strings.LastIndex(string(raw), ")")
	if end < 0 {
		return "", errors.New("invalid process stat")
	}
	fields := strings.Fields(string(raw)[end+1:])
	if len(fields) < 20 {
		return "", errors.New("process stat truncated")
	}
	boot, e := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if e != nil || len(boot) > 128 {
		return "", errors.New("boot identity unavailable")
	}
	return "linux:" + strings.TrimSpace(string(boot)) + ":" + fields[19], nil
}
