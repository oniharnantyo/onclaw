//go:build server

package services

// NewHostSaveDialog returns the server host's SaveDialog: the null object —
// there is no GUI run loop to open a native dialog in.
func NewHostSaveDialog() SaveDialog { return NoSaveDialog{} }

// nativeDialogsAvailable reports whether this build can open native dialogs:
// the server build cannot.
func nativeDialogsAvailable() bool { return false }
