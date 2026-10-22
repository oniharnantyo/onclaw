//go:build server

package cli

import (
	"context"
	"fmt"
)

// serveHost is the server entrypoint's host runner, installed by
// main_server.go via SetServeHost (the assets embed and the Wails emitter
// adapter live in package main's sibling of the desktop main.go, so the
// runner must be injected rather than constructed here).
var serveHost func(ctx context.Context, app *App) error

// SetServeHost installs the server-mode host runner. The entrypoint calls
// it during init; calling it twice is a programming error.
func SetServeHost(runner func(ctx context.Context, app *App) error) {
	serveHost = runner
}

// serveAction is the server build's serve seam: it runs the same host the
// entrypoint's default action runs (Options.Server behind the
// bearer-gated front server).
func serveAction(ctx context.Context, app *App) error {
	if serveHost == nil {
		return fmt.Errorf("serve: server host runner not installed")
	}
	return serveHost(ctx, app)
}
