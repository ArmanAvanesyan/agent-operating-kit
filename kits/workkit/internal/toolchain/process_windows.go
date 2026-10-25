//go:build windows

package toolchain

import (
	"golang.org/x/sys/windows"
	"os/exec"
	"sync"
	"unsafe"
)

func configureProcess(c *exec.Cmd) error {
	c.SysProcAttr = &windows.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.CREATE_SUSPENDED}
	return nil
}
func attachProcess(c *exec.Cmd) error {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return err
	}
	proc, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(c.Process.Pid))
	if err != nil {
		_ = windows.CloseHandle(job)
		return err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		_ = windows.TerminateJobObject(job, 1)
		_ = windows.CloseHandle(job)
		return err
	}
	err = windows.AssignProcessToJobObject(job, proc)
	if err != nil {
		_ = windows.CloseHandle(proc)
		_ = windows.CloseHandle(job)
		return err
	}
	thread, err := suspendedThread(c.Process.Pid)
	_ = windows.CloseHandle(proc)
	if err != nil {
		_ = windows.TerminateJobObject(job, 1)
		_ = windows.CloseHandle(job)
		return err
	}
	_, err = windows.ResumeThread(thread)
	_ = windows.CloseHandle(thread)
	if err != nil {
		_ = windows.TerminateJobObject(job, 1)
		_ = windows.CloseHandle(job)
		return err
	}
	processJobMu.Lock()
	processJobs[c.Process.Pid] = job
	processJobMu.Unlock()
	return nil
}

func suspendedThread(pid int) (windows.Handle, error) {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return 0, err
	}
	defer windows.CloseHandle(snapshot)
	e := windows.ThreadEntry32{Size: uint32(unsafe.Sizeof(windows.ThreadEntry32{}))}
	for err = windows.Thread32First(snapshot, &e); err == nil; err = windows.Thread32Next(snapshot, &e) {
		if e.OwnerProcessID == uint32(pid) {
			return windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, e.ThreadID)
		}
	}
	return 0, err
}

func finishProcess(c *exec.Cmd, raw uintptr) {
	if raw != 0 {
		_ = windows.CloseHandle(windows.Handle(raw))
	}
	if c.Process != nil {
		processJobMu.Lock()
		delete(processJobs, c.Process.Pid)
		processJobMu.Unlock()
	}
}

var processJobMu sync.Mutex
var processJobs = map[int]windows.Handle{}

func processHandle(c *exec.Cmd) uintptr {
	processJobMu.Lock()
	defer processJobMu.Unlock()
	return uintptr(processJobs[c.Process.Pid])
}
func stopProcess(c *exec.Cmd, raw uintptr) error {
	if raw != 0 {
		h := windows.Handle(raw)
		_ = windows.TerminateJobObject(h, 1)
	} else if c.Process != nil {
		_ = c.Process.Kill()
	}
	return nil
}
