//go:build windows

package browseruse

import "os"

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

// parentDied reports whether the parent recorded at child startup is gone.
// Windows neither reparents nor signals parent death, so the pid probe is the
// best available answer; a reused pid can only delay the watchdog, never
// fire it early. The kill-on-close Job Object is the second layer there.
func parentDied(parentPID int) bool {
	return !pidAlive(parentPID)
}
