//go:build !linux && !windows

package computeruse

import (
	"errors"
	"os/exec"
	"syscall"
	"time"
)

// Process-group hygiene for the driver daemon child, mirrored from
// internal/browseruse's leakless replacement (add-computer-use design D1,
// spec computer-use "Driver daemon lifecycle": the daemon SHALL NOT outlive
// the app). On darwin and the other non-Linux Unix systems the group handle
// is the pgid the daemon was started with (it leads its own group; see
// configureChildProc).

// groupHandle identifies the launched daemon's process group.
type groupHandle = int

// configureChildProc puts the daemon child into its own process group, so
// the teardown and crash paths can signal the whole driver tree with one
// kill.
func configureChildProc(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = new(syscall.SysProcAttr)
	}
	cmd.SysProcAttr.Setpgid = true
}

// startProcGroup starts cmd and reports its process group — the handle every
// kill primitive below works on.
func startProcGroup(cmd *exec.Cmd) (groupHandle, error) {
	configureChildProc(cmd)
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	return cmd.Process.Pid, nil
}

// groupAlive reports whether any process remains in the daemon's group.
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

// terminateGroup is the graceful teardown path: SIGTERM lets the daemon exit
// cleanly and drop its socket, SIGKILL for whatever outlives the grace. A
// group that is already gone is not an error.
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

// forceKillGroup is the crash path: SIGKILL the whole group immediately, no
// grace. A group that is already gone is not an error.
func forceKillGroup(group groupHandle, _ int) error {
	if !groupAlive(group, 0) {
		return nil
	}
	_ = syscall.Kill(-group, syscall.SIGKILL)
	return nil
}
