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

func New() *cli.Command {
	st := &appState{}
	return &cli.Command{
		Name:    "onclaw",
		Usage:   "Open on-device AI coding agent for low-resource devices",
		Version: version.String(),
		Flags:   globalFlags(),
		Before:  st.before,
		Commands: []*cli.Command{
			initCommand(st),
			versionCommand(),
			configCommand(st),
			runCommand(st),
			chatCommand(st),
			providerCommand(st),
			agentCommand(st),
		},
	}
}

func (s *appState) before(ctx context.Context, c *cli.Command) (context.Context, error) {
	cfg, err := config.Load(c.String("config"))
	if err != nil {
		return ctx, err
	}

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

func (s *appState) ensure(c *cli.Command) error {
	if s.cfg != nil {
		return nil
	}
	_, err := s.before(context.Background(), c)
	return err
}
