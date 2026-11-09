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

	"github.com/oniharnantyo/buddies/internal/computeruse"
	"github.com/oniharnantyo/buddies/internal/services"
)

// newComputerMCPCommand returns the "computer-mcp" subcommand (add-computer-use
// design D1, the coordination contract's exact argv): the Computer Use system
// MCP server — the same buddies binary speaking MCP over stdio, curating the
// vendored cua-driver daemon named by --socket. The parent spawns it with
// every flag set explicitly; the flag defaults are only fallbacks. It keeps
// no network listener and exits when its input stream closes, so a dead
// parent leaves no orphan.
func newComputerMCPCommand() *cli.Command {
	return &cli.Command{
		// The literal mirrors browser_mcp.go and the mcp registry's
		// ComputerUseSubcommand (internal/agents/mcp) — the coordination
		// contract's exact subcommand name.
		Name:  "computer-mcp",
		Usage: "Run the Computer Use MCP server on stdio (spawned by buddies itself)",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "socket",
				Usage:    "The driver daemon's private local socket to reach the vendored driver through.",
				Required: true,
			},
			&cli.StringFlag{
				Name:     "computer-dir",
				Usage:    "Computer Use data directory: fallback screenshots live under screenshots/ inside it.",
				Required: true,
			},
			&cli.BoolFlag{
				Name:  "ready",
				Usage: "Serve the tool surface (the parent's permission warm-up gate verdict); without it the server offers zero tools.",
				Value: false,
			},
		},
		Action: runComputerMCP,
	}
}

// runComputerMCP serves the Computer Use MCP server on stdio until stdin EOF
// or SIGINT/SIGTERM. When the parent's readiness verdict is false the server
// offers zero tools (spec computer-use "Permission warm-up gate": until
// every required permission is granted the server offers nothing).
func runComputerMCP(ctx context.Context, cmd *cli.Command) error {
	cfg := computeruse.ServerConfig{
		SocketPath:  cmd.String("socket"),
		ComputerDir: cmd.String("computer-dir"),
		Ready:       cmd.Bool("ready"),
	}
	if cfg.SocketPath == "" {
		return errors.New("computer-mcp: --socket is required")
	}
	if cfg.ComputerDir == "" {
		return errors.New("computer-mcp: --computer-dir is required")
	}

	srv := computeruse.NewMCPServer(cfg, services.AppVersion)
	stdio := server.NewStdioServer(srv)

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Listen returns on stdin EOF (the standard MCP stdio contract), on a
	// signal, or on a stream error — the same graceful contract as
	// browser-mcp.
	listenErr := stdio.Listen(ctx, os.Stdin, os.Stdout)
	if listenErr != nil && !errors.Is(listenErr, context.Canceled) {
		return fmt.Errorf("computer-mcp: %w", listenErr)
	}
	return nil
}
