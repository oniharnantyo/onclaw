package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/mark3labs/mcp-go/server"
	"github.com/urfave/cli/v3"

	"github.com/oniharnantyo/buddies/internal/browseruse"
	"github.com/oniharnantyo/buddies/internal/services"
)

// newBrowserMCPCommand returns the "browser-mcp" subcommand: the Browser Use
// system MCP server (design D2) — the same buddies binary speaking MCP over
// stdio. The parent spawns it with every flag set explicitly; the flag
// defaults are only fallbacks. It keeps no network listener and exits when
// its input stream closes, so a dead parent leaves no orphan.
func newBrowserMCPCommand() *cli.Command {
	return &cli.Command{
		Name:  "browser-mcp",
		Usage: "Run the Browser Use MCP server on stdio (spawned by buddies itself)",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "browser-dir",
				Usage:    "Browser root directory: profiles/, screenshots/, and the ownership lockfile live under it.",
				Required: true,
			},
			&cli.StringFlag{
				Name:  "default-profile",
				Usage: "Profile used when a tool call names none.",
				Value: "default",
			},
			&cli.BoolFlag{
				Name:  "headless",
				Usage: "Launch Chromium without a window.",
				Value: true,
			},
			&cli.IntFlag{
				Name:  "idle-timeout",
				Usage: "Seconds an idle profile browser survives before the janitor closes it.",
				Value: 120,
			},
			&cli.IntFlag{
				Name:  "action-timeout",
				Usage: "Seconds one tool action may take.",
				Value: 30,
			},
			&cli.BoolFlag{
				Name:  "allow-private",
				Usage: "Permit navigating private and local network addresses.",
				Value: false,
			},
		},
		Action: runBrowserMCP,
	}
}

// runBrowserMCP serves the Browser Use MCP server on stdio until stdin EOF or
// SIGINT/SIGTERM, then shuts every live profile browser down (cookie jar
// export, Chromium exit) and releases the single-owner lock.
func runBrowserMCP(ctx context.Context, cmd *cli.Command) error {
	cfg := browseruse.Config{
		BrowserDir:           cmd.String("browser-dir"),
		DefaultProfile:       cmd.String("default-profile"),
		Headless:             cmd.Bool("headless"),
		IdleTimeoutSeconds:   cmd.Int("idle-timeout"),
		ActionTimeoutSeconds: cmd.Int("action-timeout"),
		AllowPrivate:         cmd.Bool("allow-private"),
	}
	if cfg.BrowserDir == "" {
		return errors.New("browser-mcp: --browser-dir is required")
	}

	// Single ownership of the browser dir comes before anything else: a live
	// conflict is the startup failure the parent surfaces in the pane.
	releaseLock, err := browseruse.AcquireBrowserLock(cfg.BrowserDir)
	if err != nil {
		return err // printed to stderr by the entrypoint; exit stays nonzero
	}

	srv := browseruse.NewMCPServer(cfg, services.AppVersion)
	stdio := server.NewStdioServer(srv)

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Listen returns on stdin EOF (the standard MCP stdio contract), on a
	// signal, or on a stream error — every path falls through to the same
	// graceful teardown.
	listenErr := stdio.Listen(ctx, os.Stdin, os.Stdout)
	shutdownErr := browseruse.ShutdownAllProfiles()
	if err := releaseLock(); err != nil {
		if shutdownErr == nil {
			shutdownErr = err
		}
	}
	if listenErr != nil && !errors.Is(listenErr, context.Canceled) {
		if shutdownErr != nil {
			return fmt.Errorf("%w; shutdown: %w", listenErr, shutdownErr)
		}
		return listenErr
	}
	return shutdownErr
}
