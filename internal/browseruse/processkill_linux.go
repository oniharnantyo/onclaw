//go:build linux

package browseruse

import (
	"errors"
	"os/exec"
	"syscall"
	"time"
)

// groupHandle identifies the launched browser's process group: on Linux it is
// the pgid Chromium was started with (it leads its own group; see
// configureChildProc).
type groupHandle = int

// configureChildProc puts the Chromium child into its own process group, so
// the leakless replacement (design D3) can signal the whole browser tree —
// main process, renderers, and helpers — with one kill. Pdeathsig is the
// kernel-level second layer: Linux kills Chromium the moment this child
// process dies, before any watchdog can tick.
func configureChildProc(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = new(syscall.SysProcAttr)
	}
	cmd.SysProcAttr.Setpgid = true
	cmd.SysProcAttr.Pdeathsig = syscall.SIGKILL
}

// startProcGroup starts cmd and reports its process group. The group is the
// handle every kill primitive below works on.
func startProcGroup(cmd *exec.Cmd) (groupHandle, error) {
	configureChildProc(cmd)
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	return cmd.Process.Pid, nil
}

// groupAlive reports whether any process remains in the browser's group. The
// main pid needs no probe on Unix: the group scan covers it.
func groupAlive(group groupHandle, _ int) bool {
	if group <= 0 {
		return false
	}
	err := syscall.Kill(-group, 0)
	switch {
	case err == nil:
		return true
	case errors.Is(err, syscall.ESRCH):
		return false
	case errors.Is(err, syscall.EPERM):
		return true
	default:
		return false
	}
}

// terminateGroup is the graceful teardown path (design D3): SIGTERM lets
// Chromium exit cleanly and flush its profile store; any process still alive
// after grace is SIGKILLed. A group that is already gone is not an error.
func terminateGroup(group groupHandle, _ int, grace time.Duration) error {
	if !groupAlive(group, 0) {
		return nil
	}
	_ = syscall.Kill(-group, syscall.SIGTERM)
	deadline := time.Now().Add(grace)
	for time.Now().Before(deadline) {
		if !groupAlive(group, 0) {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	_ = syscall.Kill(-group, syscall.SIGKILL)
	return nil
}

// forceKillGroup is the crash path (design D3): SIGKILL the whole browser
// group immediately, no grace. A group that is already gone is not an error.
func forceKillGroup(group groupHandle, _ int) error {
	if !groupAlive(group, 0) {
		return nil
	}
	_ = syscall.Kill(-group, syscall.SIGKILL)
	return nil
}
