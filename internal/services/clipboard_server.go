//go:build server

package services

import (
	"errors"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// ErrClipboardUnavailable is the server build's answer to every clipboard
// call: there is no GUI run loop for the platform clipboard to dispatch to,
// and a manager call would block the bound method instead of failing. Found in
// the task 10.3 smoke, where the copy fallback hung with neither a check state
// nor a toast.
var ErrClipboardUnavailable = errors.New("clipboard: unavailable in server mode")

// clipboard refuses immediately in the server build.
func clipboard() (*application.ClipboardManager, error) {
	return nil, ErrClipboardUnavailable
}
