//go:build windows

package browseruse

import (
	"os/exec"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// groupHandle identifies the launched browser's process group: on Windows it
// is the Job Object handle the whole Chromium tree is assigned to. Kill-on-
// close is the Windows leakless layer: when this child process dies, its job
// handle dies with it and the kernel terminates the browser.
type groupHandle = windows.Handle

// startProcGroup starts cmd and assigns the process to a fresh Job Object
// with kill-on-close, so every Chromium helper the job inherits dies with it
// (design D3). Helpers spawned in the microseconds between process start and
// assignment are the only ones outside the job.
func startProcGroup(cmd *exec.Cmd) (groupHandle, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, err
	}
	limit := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limit.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&limit)), uint32(unsafe.Sizeof(limit))); err != nil {
		_ = windows.CloseHandle(job)
		return 0, err
	}
	if err := cmd.Start(); err != nil {
		_ = windows.CloseHandle(job)
		return 0, err
	}
	var assignErr error
	if err := cmd.Process.WithHandle(func(h uintptr) {
		assignErr = windows.AssignProcessToJobObject(job, windows.Handle(h))
	}); err != nil {
		assignErr = err
	}
	if assignErr != nil {
		_ = forceKillGroup(job, cmd.Process.Pid)
		_ = windows.CloseHandle(job)
		return 0, assignErr
	}
	return job, nil
}

// groupAlive reports whether the browser's main process still runs. The job
// itself cannot be probed cheaply; a dead main process brings its helpers
// down with it, so the pid is the practical proxy.
func groupAlive(group groupHandle, mainPID int) bool {
	return group != 0 && pidAlive(mainPID)
}

// terminateGroup tears the browser down (design D3). Windows has no graceful
// signal, so the job is terminated directly: the cookie jar is already
// exported over CDP at this point and the Preferences seeding preserves the
// session state, so a hard kill does not cost the profile its logins.
func terminateGroup(group groupHandle, mainPID int, _ time.Duration) error {
	return forceKillGroup(group, mainPID)
}

// forceKillGroup terminates the whole browser job. A closed job is not an
// error.
func forceKillGroup(group groupHandle, _ int) error {
	if group == 0 {
		return nil
	}
	if err := windows.TerminateJobObject(group, 1); err != nil {
		return err
	}
	return windows.CloseHandle(group)
}
