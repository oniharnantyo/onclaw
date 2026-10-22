//go:build !server

package services

import (
	"errors"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// clipboard returns the running desktop application's clipboard manager; an
// error when no application is running (the service can only serve a live
// host).
func clipboard() (*application.ClipboardManager, error) {
	app := application.Get()
	if app == nil || app.Clipboard == nil {
		return nil, errors.New("clipboard: application not running")
	}
	return app.Clipboard, nil
}
