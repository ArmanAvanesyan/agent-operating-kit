//go:build linux

package sdk

import (
	"errors"
	"golang.org/x/sys/unix"
	"net"
	"os"
	"syscall"
)

func integrationDirectoryOwner(st os.FileInfo) bool {
	s, ok := st.Sys().(*syscall.Stat_t)
	return ok && int(s.Uid) == os.Geteuid()
}
func integrationPeerOwner(c *net.UnixConn) error {
	raw, e := c.SyscallConn()
	if e != nil {
		return e
	}
	var uid uint32
	var err error
	e = raw.Control(func(fd uintptr) {
		var credential *unix.Ucred
		credential, err = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
		if err == nil {
			uid = credential.Uid
		}
	})
	if e != nil || err != nil || int(uid) != os.Geteuid() {
		return errors.New("IPC peer identity mismatch")
	}
	return nil
}

func integrationPeerPID(c *net.UnixConn) (int, error) {
	raw, e := c.SyscallConn()
	if e != nil {
		return 0, e
	}
	var pid int
	var err error
	e = raw.Control(func(fd uintptr) {
		v, readErr := unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
		err = readErr
		if err == nil {
			pid = int(v.Pid)
		}
	})
	if e != nil {
		return 0, e
	}
	return pid, err
}
