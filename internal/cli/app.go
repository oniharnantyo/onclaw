// Package cli wires the urfave/cli v3 command tree for onclaw. The root command
// owns the global flags and a Before hook that loads config and builds the
// logger; subcommands consume both via a small closed-over appState.
package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/oniharnantyo/onclaw/internal/config"
	"github.com/oniharnantyo/onclaw/internal/logging"
	"github.com/oniharnantyo/onclaw/internal/version"
	"github.com/urfave/cli/v3"
)

// New builds the root *cli.Command for onclaw.
func New() *cli.Command {
	st := &appState{}
	return &cli.Command{
		Name:     "onclaw",
		Usage:    "Open on-device AI coding agent for low-resource devices",
		Version:  version.String(),
		Flags:    globalFlags(),
		Before:   st.before,
		Commands: []*cli.Command{
			versionCommand(),
			configCommand(st),
			runCommand(st),
		},
	}
}

// before is the root Before hook: load config (honoring --config), apply the
// --log-level/--log-format overrides, build the logger, and stash both on the
// shared appState so subcommand actions can use them.
func (s *appState) before(ctx context.Context, c *cli.Command) (context.Context, error) {
	cfg, err := config.Load(c.String("config"))
	if err != nil {
		return ctx, err
	}

	// CLI flags are the top-priority override layer.
	if v := c.String("log-level"); v != "" {
		cfg.LogLevel = v
	}
	if v := c.String("log-format"); v != "" {
		cfg.LogFormat = v
	}

	logger, err := logging.New(cfg.LogLevel, cfg.LogFormat, os.Stderr)
	if err != nil {
		return ctx, fmt.Errorf("init logging: %w", err)
	}

	s.cfg = cfg
	s.log = logger
	return ctx, nil
}

// ensure guarantees config + logger are loaded. It is a no-op once before() has
// run, and is called from subcommand actions as a safety net in case the root
// Before hook did not execute for that command path.
func (s *appState) ensure(c *cli.Command) error {
	if s.cfg != nil {
		return nil
	}
	_, err := s.before(context.Background(), c)
	return err
}
