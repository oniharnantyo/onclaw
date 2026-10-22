package cli

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"

	"github.com/oniharnantyo/buddies/internal/services"
)

// GUIRunner runs the host's graphical frontend over an assembled App. The
// desktop entrypoint provides the window+tray implementation; it returns
// when the application exits.
type GUIRunner func(ctx context.Context, app *App) error

// NewRootCommand returns the "buddies" root command: the default action
// (no subcommand) runs the GUI through run, "serve" starts server mode, and
// "version" prints the application version (design D4). emitter receives
// the services' events and opener launches their system-browser flows
// inside both the GUI and serve actions; entrypoints supply their host
// adapters (the desktop main wires the Wails event bus and browser).
func NewRootCommand(emitter services.Emitter, opener services.URLOpener, run GUIRunner) *cli.Command {
	return &cli.Command{
		Name:  services.AppName,
		Usage: "A local agent gateway that keeps running in the background",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			app, err := Assemble(ctx, emitter, opener)
			if err != nil {
				return err
			}
			defer app.Close()
			return run(ctx, app)
		},
		Commands: []*cli.Command{
			newServeCommand(emitter, opener),
			newVersionCommand(),
		},
	}
}

// newServeCommand returns the "serve" subcommand: it assembles the
// application and hands it to the host-specific server action (build-tag
// seam; see serve.go / serve_server.go).
func newServeCommand(emitter services.Emitter, opener services.URLOpener) *cli.Command {
	return &cli.Command{
		Name:  "serve",
		Usage: "Serve the app as a web application (server mode)",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			app, err := Assemble(ctx, emitter, opener)
			if err != nil {
				return err
			}
			defer app.Close()
			return serveAction(ctx, app)
		},
	}
}

// newVersionCommand returns the "version" subcommand: it prints the
// application name and version.
func newVersionCommand() *cli.Command {
	return &cli.Command{
		Name:  "version",
		Usage: "Print the application version",
		Action: func(_ context.Context, cmd *cli.Command) error {
			fmt.Fprintf(cmd.Root().Writer, "%s version %s\n", services.AppName, services.AppVersion)
			return nil
		},
	}
}
