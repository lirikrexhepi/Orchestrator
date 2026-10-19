//go:build windows

package process

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

type jobObject struct {
	handle windows.Handle
}

func attachJob(p *Process, ownProcessOnly bool) {
	if p.cmd.Process == nil {
		return
	}

	handle, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return
	}

	flags := uint32(windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE)
	if ownProcessOnly {
		flags |= windows.JOB_OBJECT_LIMIT_SILENT_BREAKAWAY_OK
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: flags,
		},
	}
	if _, err := windows.SetInformationJobObject(
		handle,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	); err != nil {
		_ = windows.CloseHandle(handle)
		return
	}

	target, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE,
		false,
		uint32(p.cmd.Process.Pid),
	)
	if err != nil {
		_ = windows.CloseHandle(handle)
		return
	}
	defer windows.CloseHandle(target)

	if err := windows.AssignProcessToJobObject(handle, target); err != nil {
		_ = windows.CloseHandle(handle)
		return
	}

	p.mu.Lock()
	p.job = &jobObject{handle: handle}
	p.mu.Unlock()
}

func closeJob(p *Process) bool {
	p.mu.Lock()
	job, _ := p.job.(*jobObject)
	p.job = nil
	p.mu.Unlock()

	if job == nil {
		return false
	}
	_ = windows.CloseHandle(job.handle)
	return true
}
