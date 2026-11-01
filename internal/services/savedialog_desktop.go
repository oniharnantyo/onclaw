//go:build !server

package services

import (
	"fmt"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// NewHostSaveDialog returns the desktop host's SaveDialog: the wails-native
// save dialog, resolved lazily against the running application.
func NewHostSaveDialog() SaveDialog {
	return hostSaveDialog{}
}

// hostSaveDialog drives the wails save dialog; like clipboard(), the
// application is resolved at call time because the service can only serve a
// live host.
type hostSaveDialog struct{}

// SaveFile opens the native save dialog seeded with the artifact file name.
// An empty chosen path is the user cancelling, mapped to ("", nil); no
// application running (services can only serve a live host) is
// ErrExportUnavailable.
func (hostSaveDialog) SaveFile(name string) (string, error) {
	app := application.Get()
	if app == nil || app.Dialog == nil {
		return "", ErrExportUnavailable
	}
	dest, err := app.Dialog.SaveFileWithOptions(&application.SaveFileDialogOptions{
		Filename:             name,
		CanCreateDirectories: true,
	}).PromptForSingleSelection()
	if err != nil {
		return "", fmt.Errorf("save as: dialog: %w", err)
	}
	if dest == "" {
		return "", nil
	}
	return dest, nil
}

// nativeDialogsAvailable reports whether this build can open native dialogs:
// the desktop build can.
func nativeDialogsAvailable() bool { return true }
