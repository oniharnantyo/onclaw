//go:build windows

package computeruse

import "os"

// Parent-liveness probes for the driver daemon's watchdog on Windows,
// mirrored from internal/browseruse (add-computer-use design D1).

// pidAlive reports whether a process with pid exists. On Windows
// os.FindProcess opens a handle to the process, which fails exactly when no
// such process is running — no signal probe needed (and signaling would be
// destructive, since only Kill is supported).
func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	_, err := os.FindProcess(pid)
	return err == nil
}

// parentDied reports whether the parent recorded at daemon startup is gone.
// Windows neither reparents nor signals parent death, so the pid probe is
// the best available answer; a reused pid can only delay the watchdog, never
// fire it early. The kill-on-close Job Object is the second layer there.
func parentDied(parentPID int) bool {
	return !pidAlive(parentPID)
}
