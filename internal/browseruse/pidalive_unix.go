//go:build !windows

package browseruse

import (
	"errors"
	"os"
	"syscall"
)

// pidAlive reports whether a process with pid exists, portably: on Unix a
// signal-0 probe answers existence (EPERM means it exists and we merely lack
// permission to signal it).
func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
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

// parentDied reports whether the parent recorded at child startup is gone.
// On Unix a dead parent reparents the surviving child to init/launchd, so a
// changed getppid is the reliable death signal; the pid probe catches a
// parent that vanished without a reparent.
func parentDied(parentPID int) bool {
	return os.Getppid() != parentPID || !pidAlive(parentPID)
}
