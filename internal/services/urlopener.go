package services

import (
	"errors"
	"log/slog"
)

// URLOpener opens a URL in the user's system browser. Services launch the
// MCP OAuth authorization page through it so they stay Wails-free (design
// D8; sibling of Emitter).
type URLOpener interface {
	// Open opens url in the system browser. It must not block on the user.
	Open(url string) error
}

// ErrNoBrowser is returned by hosts that cannot open a browser (server
// mode): the authorization flow fails with this message and the pane directs
// the user to the desktop app.
var ErrNoBrowser = errors.New("no browser available on this host; authorize this server from the buddies desktop app")

// NoBrowser is the URLOpener for headless hosts: it logs the attempt and
// returns ErrNoBrowser (null object, like Noop for Emitter).
type NoBrowser struct{}

// Open logs the attempt — never the URL — and returns ErrNoBrowser.
func (NoBrowser) Open(_ string) error {
	slog.Info("mcp oauth: authorization requested but this host has no browser")
	return ErrNoBrowser
}
