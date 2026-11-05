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

// init registers the typed custom events the services emit so the binding
// generator produces their frontend definitions. The set mirrors main.go's
// desktop registration exactly: the server build generates the bindings the
// shared frontend compiles against (build:frontend runs under this tag
// set), so a missing registration here drops its payload type and breaks
// the frontend build. The per-session event names (SessionEventName) are
// dynamic and intentionally unregistered; the per-group event name
// (GroupEventName) is dynamic the same way, and its payload registers under
// the documented "<id>" placeholder so the generator emits the GroupEvent
// type for the frontend.
func init() {
	application.RegisterEvent[services.GatewayStateEvent](services.EventGatewayState)
	application.RegisterEvent[services.MCPOAuthCompletedEvent](services.EventMCPOAuthCompleted)
	application.RegisterEvent[services.SchedulesChangedEvent](services.EventSchedulesChanged)
	application.RegisterEvent[services.KnowledgeChangedEvent](services.EventKnowledgeChanged)
	application.RegisterEvent[services.TurnsChangedEvent](services.EventTurnsChanged)
	application.RegisterEvent[services.ChannelChangedEvent](services.EventChannelChanged)
	application.RegisterEvent[services.ChannelStateEvent](services.EventChannelState)
	application.RegisterEvent[services.GroupsChangedEvent](services.EventGroupsChanged)
	application.RegisterEvent[services.GroupEvent]("group:<id>:event")
}

func main() {
	emitter := cli.NewWailsEmitter()

	// Both the default action (bare `buddies-server`, e.g. in Docker) and
	// `buddies serve` run the same host; options come from env and the
	// post-"--" flags.
	runner := func(ctx context.Context, app *cli.App) error {
		return cli.RunServerHost(ctx, app, emitter, application.AssetFileServerFS(assets), cli.ParseServerOptions(),
			// The shared frontend imports the window-theme binding, so the
			// server-tag binding generation must discover the type; bound
			// here it is inert (no native window handle is ever set).
			application.NewService(NewWindowThemeService()))
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
