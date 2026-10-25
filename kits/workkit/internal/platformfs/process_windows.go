//go:build windows

package platformfs

import (
	"fmt"
	"golang.org/x/sys/windows"
)

func ProcessStart() (string, error) {
	var creation, exit, kernel, user windows.Filetime
	if e := windows.GetProcessTimes(windows.CurrentProcess(), &creation, &exit, &kernel, &user); e != nil {
		return "", e
	}
	return fmt.Sprintf("windows:%d:%d", creation.HighDateTime, creation.LowDateTime), nil
}
