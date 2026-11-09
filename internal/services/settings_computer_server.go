//go:build server

package services

// The server build's Computer Use seam (add-computer-use design D9: the whole
// lane is absent in server mode — no GUI session, so no daemon, no adapter,
// no tools, and the pane presents the capability as unavailable rather than
// offering toggles that cannot work; spec computer-use "Desktop-only
// availability"). Mirrors the clipboard service's server gate
// (clipboard_server.go): the seam answers before any hook is consulted, so
// the server composition never wires ComputerHooks and nothing panics.

// computerLaneAvailable reports the lane absent: every shared method
// short-circuits to the unavailable status or ErrComputerUnavailable.
func computerLaneAvailable() bool { return false }

// computerPermissions has nothing to probe — there is no desktop session to
// hold permissions. The composition never wires it in server mode; defined so
// the seam's surface is complete and symmetric with the desktop builds.
func computerPermissions() []ComputerPermission { return nil }

// promptComputerPermissions refuses: no GUI session can host a permission
// prompt (spec computer-use "Desktop-only availability").
func promptComputerPermissions() error { return ErrComputerUnavailable }
