package services

// The Computer Use permission seam's exported surface for the composition
// root (add-computer-use design D6). The platform probes live behind the
// build-tag split (settings_computer_darwin.go / _stub.go / _server.go) and
// are unexported there, while the composition root (internal/cli) is what
// wires ComputerHooks.ProbePermissions / .PromptPermissions — the seam's own
// documented contract — and reads the probe for the spawn-boundary hold.
// Mirrors the savedialog seam's exported-constructor pattern
// (NewHostSaveDialog). Server builds answer the seam's own unavailable
// results; the server composition never wires them.

// ComputerPermissionProbe prefights the platform's automation permissions
// WITHOUT raising a prompt (spec computer-use "Permission warm-up gate":
// probing never prompts; the app owns prompting).
func ComputerPermissionProbe() []ComputerPermission {
	return computerPermissions()
}

// ComputerPermissionPrompt raises the OS permission prompts — wired only
// into the pane's enable action path, never the probe path (design D6: the
// prompt is raised host-side only; the driver never prompts).
func ComputerPermissionPrompt() error {
	return promptComputerPermissions()
}
