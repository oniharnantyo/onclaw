package services

import (
	"errors"
)

// SaveDialog opens the host's native save dialog. The Save As export
// (add-artifact-save-as design D1) launches it through this seam so services
// stay Wails-free (sibling of Emitter/URLOpener).
type SaveDialog interface {
	// SaveFile opens the host's native save dialog seeded with the artifact
	// file name. It returns the chosen destination path, or the empty string
	// when the user cancelled the dialog.
	SaveFile(name string) (string, error)
}

// ErrExportUnavailable is returned by hosts that have no native save dialog
// (server mode): the export cannot proceed on this host. Returned bare — its
// message is the frontend contract.
var ErrExportUnavailable = errors.New("save as: no native dialog on this host")

// ErrExportCancelled is the answer when the user cancelled the native save
// dialog: nothing was written. Returned bare — its message is the frontend
// contract.
var ErrExportCancelled = errors.New("save as: cancelled")

// NoSaveDialog is the SaveDialog for hosts without a native dialog (server
// mode): its SaveFile returns ErrExportUnavailable (null object, like
// NoBrowser for URLOpener).
type NoSaveDialog struct{}

// SaveFile returns ErrExportUnavailable — there is no dialog to open.
func (NoSaveDialog) SaveFile(_ string) (string, error) {
	return "", ErrExportUnavailable
}
