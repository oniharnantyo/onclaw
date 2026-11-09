//go:build darwin && !ios && !server

package services

/*
#cgo LDFLAGS: -framework ApplicationServices -framework CoreGraphics
#include <ApplicationServices/ApplicationServices.h>
#include <CoreGraphics/CoreGraphics.h>

// buddiesAXTrustedWithPrompt wraps AXIsProcessTrustedWithOptions with the
// prompt option set (kAXTrustedCheckOptionPrompt: true): the app owns raising
// the Accessibility prompt — add-computer-use design D6 ("The prompt is
// raised host-side only ... the driver never prompts"). Only the pane's
// enable action reaches it; the preflight path never calls it.
static Boolean buddiesAXTrustedWithPrompt(void) {
	const void *keys[] = { kAXTrustedCheckOptionPrompt };
	const void *vals[] = { kCFBooleanTrue };
	CFDictionaryRef options = CFDictionaryCreate(
		NULL, keys, vals, 1,
		&kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
	if (options == NULL) {
		return AXIsProcessTrusted();
	}
	Boolean trusted = AXIsProcessTrustedWithOptions(options);
	CFRelease(options);
	return trusted;
}
*/
import "C"

// The darwin desktop Computer Use probe seam (add-computer-use design D6,
// the clipboard pattern's build-tag split): Accessibility and Screen
// Recording preflight through ApplicationServices without raising a prompt —
// the prompt-raising variants live beside them and are wired only into the
// pane's enable action. The composition root adapts these functions into
// ComputerHooks.ProbePermissions / .PromptPermissions; the shared service
// never links this file's cgo surface directly. macOS needs both permissions
// for the driver to observe and act; until every one is granted the daemon
// does not run and zero tools are offered (spec computer-use "Permission
// warm-up gate").

// The Manage deep links opening the matching System Settings pane (spec
// settings "Computer Use pane": a Manage action opening the matching system
// settings pane).
const (
	computerManageAccessibilityURL   = "x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility"
	computerManageScreenRecordingURL = "x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture"
)

// computerLaneAvailable reports the lane present: desktop darwin offers the
// full permission ceremony.
func computerLaneAvailable() bool { return true }

// computerPermissions prefights both macOS permissions WITHOUT raising a
// prompt: AXIsProcessTrusted answers the Accessibility grant and
// CGPreflightScreenCaptureAccess the Screen Recording grant, both cached
// answers read synchronously. Order is the pane's display order.
func computerPermissions() []ComputerPermission {
	return []ComputerPermission{
		{
			Name:      "Accessibility",
			Granted:   C.AXIsProcessTrusted() != 0,
			ManageURL: computerManageAccessibilityURL,
		},
		{
			Name:      "Screen Recording",
			Granted:   bool(C.CGPreflightScreenCaptureAccess()),
			ManageURL: computerManageScreenRecordingURL,
		},
	}
}

// promptComputerPermissions raises the OS prompts: the Accessibility prompt
// via kAXTrustedCheckOptionPrompt and the Screen Recording prompt via
// CGRequestScreenCaptureAccess. The calls return immediately after posting;
// the pane's Check again re-probes the answers afterwards.
func promptComputerPermissions() error {
	C.buddiesAXTrustedWithPrompt()
	C.CGRequestScreenCaptureAccess()
	return nil
}
