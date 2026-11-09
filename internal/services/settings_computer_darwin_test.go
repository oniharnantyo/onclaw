//go:build darwin && !ios && !server

package services

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSettingsComputerDarwin_SeamProbesWithoutPrompts proves the darwin probe
// seam's shape (add-computer-use design D6): both macOS permissions are
// listed in the pane's display order, each with its Manage deep link, and
// probing is prompt-free — AXIsProcessTrusted and
// CGPreflightScreenCaptureAccess are cached-answer preflights, so calling
// them here (twice) raises nothing. The granted values themselves reflect the
// running machine's live TCC state and are deliberately not asserted.
func TestSettingsComputerDarwin_SeamProbesWithoutPrompts(t *testing.T) {
	require.True(t, computerLaneAvailable())

	perms := computerPermissions()
	require.Len(t, perms, 2)
	require.Equal(t, "Accessibility", perms[0].Name)
	require.Equal(t, "x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility", perms[0].ManageURL)
	require.Equal(t, "Screen Recording", perms[1].Name)
	require.Equal(t, "x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture", perms[1].ManageURL)

	again := computerPermissions()
	require.Equal(t, perms[0].Granted, again[0].Granted, "the preflight answer is stable within a run")
	require.Equal(t, perms[1].Granted, again[1].Granted, "the preflight answer is stable within a run")
}
