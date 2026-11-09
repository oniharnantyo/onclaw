//go:build (!darwin || ios) && !server

package services

// The no-ceremony desktop Computer Use seam (add-computer-use design D9):
// windows and linux desktop have no OS permission gate for desktop
// automation, so the lane is available with an empty permission list — the
// pane goes straight to ready and there is nothing to prompt (spec
// computer-use "Desktop-only availability": the lane is desktop-only; the
// ceremony itself is macOS-only, design D6). The composition root wires
// these into ComputerHooks.ProbePermissions / .PromptPermissions.

// computerLaneAvailable reports the lane present: desktop windows/linux need
// no permission grant before the daemon may run.
func computerLaneAvailable() bool { return true }

// computerPermissions returns the empty permission list — no required OS
// permissions, nothing to render in the pane's permission card.
func computerPermissions() []ComputerPermission { return []ComputerPermission{} }

// promptComputerPermissions is a no-op: there is no OS prompt to raise.
func promptComputerPermissions() error { return nil }
