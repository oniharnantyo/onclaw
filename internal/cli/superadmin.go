package cli

import (
	"context"
	"fmt"

	"github.com/oniharnantyo/onclaw/internal/bootstrap"
	"github.com/oniharnantyo/onclaw/internal/config"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/urfave/cli/v3"
)

// superadminCmd handles superadmin management CLI commands.
type superadminCmd struct{}

// NewSuperadminCmd creates a new superadminCmd instance.
func NewSuperadminCmd() *superadminCmd {
	return &superadminCmd{}
}

// Command returns the *cli.Command definition for "superadmin".
func (s *superadminCmd) Command() *cli.Command {
	return &cli.Command{
		Name:  "superadmin",
		Usage: "Manage superadmin accounts in the master workspace",
		Flags: config.SuperadminFlags(),
		Commands: []*cli.Command{
			s.createCommand(),
		},
	}
}

func (s *superadminCmd) createCommand() *cli.Command {
	return &cli.Command{
		Name:  "create",
		Usage: "Create or seed a superadmin user",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "email",
				Usage:    "Superadmin email address",
				Required: true,
			},
			&cli.StringFlag{
				Name:     "name",
				Usage:    "Superadmin name",
				Required: true,
			},
			&cli.StringFlag{
				Name:     "password",
				Usage:    "Superadmin password",
				Required: true,
			},
		},
		Action: s.RunCreate,
	}
}

// RunCreate creates or seeds a superadmin user.
func (s *superadminCmd) RunCreate(ctx context.Context, cmd *cli.Command) error {
	cfg := config.FromSuperadminContext(ctx, cmd)
	st, err := store.Open(ctx, "postgres", store.DSNConfig{DSN: cfg.DatabaseURL})
	if err != nil {
		return fmt.Errorf("failed to open store: %w", err)
	}
	defer st.Close()

	seedCfg := bootstrap.SuperadminSeedConfig{
		Email:    cmd.String("email"),
		Name:     cmd.String("name"),
		Password: cmd.String("password"),
	}

	bootstrapper := bootstrap.New(st)
	user, err := bootstrapper.SeedSuperadmin(ctx, seedCfg)
	if err != nil {
		return fmt.Errorf("failed to seed superadmin: %w", err)
	}
	if user == nil {
		fmt.Println("Superadmin already seeded in master tenant; skipped")
		return nil
	}

	fmt.Printf("Superadmin %s (%s) created successfully\n", user.Name, user.Email)
	return nil
}
