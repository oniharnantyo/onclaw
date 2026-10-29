//go:build server

// buddies server entrypoint (design D1/D4): assembles the same application
// through internal/cli and serves it headlessly — the Wails application runs
// in server mode bound to a loopback-internal port, and RunServerHost fronts
// it with an optional bearer-token gate so HTTP and both WebSocket endpoints
// (/wails/events, /wails/stream/ws) are authenticated uniformly (spike:
// server-mode auth gap). The same frontend/dist assets are embedded, so a
// browser loading the public address gets the identical app over the Wails
// WebSocket bridge.
package main

import (
	"context"
	"embed"
	"log"
	"os"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/oniharnantyo/buddies/internal/cli"
	"github.com/oniharnantyo/buddies/internal/services"
)

// Wails uses Go's `embed` package to embed the frontend files into the
// binary. Any files in the frontend/dist folder will be embedded into the
// binary and made available to the frontend.
//
//go:embed all:frontend/dist
var assets embed.FS

// init registers the typed custom event the services emit so the binding
// generator produces its frontend definition (the per-session event names
// are dynamic and intentionally unregistered).
func init() {
	application.RegisterEvent[services.GatewayStateEvent](services.EventGatewayState)
	application.RegisterEvent[services.ChannelChangedEvent](services.EventChannelChanged)
	application.RegisterEvent[services.ChannelStateEvent](services.EventChannelState)
}

func main() {
	emitter := cli.NewWailsEmitter()

	// Both the default action (bare `buddies-server`, e.g. in Docker) and
	// `buddies serve` run the same host; options come from env and the
	// post-"--" flags.
	runner := func(ctx context.Context, app *cli.App) error {
		return cli.RunServerHost(ctx, app, emitter, application.AssetFileServerFS(assets), cli.ParseServerOptions())
	}
	cli.SetServeHost(runner)

	// NoBrowser is the headless URLOpener (design D8): MCP OAuth
	// authorization fails with a message directing the user to the desktop
	// app, which shares this database and its tokens.
	cmd := cli.NewRootCommand(emitter, services.NoBrowser{}, runner)
	if err := cmd.Run(context.Background(), os.Args); err != nil {
		log.Fatal(err)
	}
}
