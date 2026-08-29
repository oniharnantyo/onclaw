package cli

import (
	"context"
	"errors"
	"fmt"

	"github.com/oniharnantyo/onclaw/internal/config"
	"github.com/oniharnantyo/onclaw/internal/store/postgres"
	"github.com/urfave/cli/v3"
)

// migrateCmd handles database migration CLI commands.
type migrateCmd struct{}

// NewMigrateCmd creates a new migrateCmd instance.
func NewMigrateCmd() *migrateCmd {
	return &migrateCmd{}
}

// Command returns the *cli.Command definition for "migrate".
func (m *migrateCmd) Command() *cli.Command {
	return &cli.Command{
		Name:  "migrate",
		Usage: "Run database migrations",
		Flags: config.MigrateFlags(),
		Commands: []*cli.Command{
			m.upCommand(),
			m.downCommand(),
			m.statusCommand(),
			m.versionCommand(),
		},
	}
}

func (m *migrateCmd) upCommand() *cli.Command {
	return &cli.Command{
		Name:   "up",
		Usage:  "Apply all pending migrations",
		Flags:  config.MigrateFlags(),
		Action: m.RunUp,
	}
}

// RunUp applies all pending migrations.
func (m *migrateCmd) RunUp(ctx context.Context, cmd *cli.Command) error {
	cfg := config.FromMigrateContext(ctx, cmd)
	if cfg.DatabaseURL == "" {
		return errors.New("database URL is required (specify --database-url or DATABASE_URL)")
	}

	migrator := postgres.NewMigrator(cfg.DatabaseURL)
	if err := migrator.Up(); err != nil {
		return fmt.Errorf("migration up failed: %w", err)
	}

	fmt.Println("Migrations applied successfully")
	return nil
}

func (m *migrateCmd) downCommand() *cli.Command {
	return &cli.Command{
		Name:  "down",
		Usage: "Rollback the most recent migration or all migrations",
		Flags: append([]cli.Flag{
			&cli.IntFlag{
				Name:  "steps",
				Value: 1,
				Usage: "Number of migrations to roll back (0 for all)",
			},
			&cli.BoolFlag{
				Name:  "all",
				Usage: "Roll back all migrations",
			},
		}, config.MigrateFlags()...),
		Action: m.RunDown,
	}
}

// RunDown rolls back migrations.
func (m *migrateCmd) RunDown(ctx context.Context, cmd *cli.Command) error {
	cfg := config.FromMigrateContext(ctx, cmd)
	if cfg.DatabaseURL == "" {
		return errors.New("database URL is required (specify --database-url or DATABASE_URL)")
	}

	steps := int(cmd.Int("steps"))
	if cmd.Bool("all") {
		steps = 0
	}

	migrator := postgres.NewMigrator(cfg.DatabaseURL)
	if err := migrator.Down(steps); err != nil {
		return fmt.Errorf("migration down failed: %w", err)
	}

	if steps == 0 {
		fmt.Println("All migrations rolled back successfully")
	} else {
		fmt.Printf("Rolled back %d migration(s) successfully\n", steps)
	}
	return nil
}

func (m *migrateCmd) statusCommand() *cli.Command {
	return &cli.Command{
		Name:   "status",
		Usage:  "Show current migration status",
		Flags:  config.MigrateFlags(),
		Action: m.RunStatus,
	}
}

// RunStatus retrieves and displays current migration status.
func (m *migrateCmd) RunStatus(ctx context.Context, cmd *cli.Command) error {
	cfg := config.FromMigrateContext(ctx, cmd)
	if cfg.DatabaseURL == "" {
		return errors.New("database URL is required (specify --database-url or DATABASE_URL)")
	}

	migrator := postgres.NewMigrator(cfg.DatabaseURL)
	version, dirty, err := migrator.Status()
	if err != nil {
		return fmt.Errorf("failed to retrieve migration status: %w", err)
	}

	if version == 0 && !dirty {
		fmt.Println("Database is clean (no migrations applied)")
	} else {
		fmt.Printf("Current migration version: %d (dirty: %t)\n", version, dirty)
	}
	return nil
}

func (m *migrateCmd) versionCommand() *cli.Command {
	return &cli.Command{
		Name:   "version",
		Usage:  "Show current migration version",
		Flags:  config.MigrateFlags(),
		Action: m.RunVersion,
	}
}

// RunVersion retrieves and displays current migration version.
func (m *migrateCmd) RunVersion(ctx context.Context, cmd *cli.Command) error {
	cfg := config.FromMigrateContext(ctx, cmd)
	if cfg.DatabaseURL == "" {
		return errors.New("database URL is required (specify --database-url or DATABASE_URL)")
	}

	migrator := postgres.NewMigrator(cfg.DatabaseURL)
	version, dirty, err := migrator.Status()
	if err != nil {
		return fmt.Errorf("failed to retrieve migration version: %w", err)
	}

	fmt.Printf("Version: %d (dirty: %t)\n", version, dirty)
	return nil
}
