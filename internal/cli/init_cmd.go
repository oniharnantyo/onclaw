package cli

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/oniharnantyo/onclaw/internal/agent"
	"github.com/oniharnantyo/onclaw/internal/llm"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/urfave/cli/v3"
)

type initStep struct {
	name string
	run  func(ctx context.Context, st *appState, mgr *llm.Service, db *sql.DB, in io.Reader, out io.Writer) error
}

var initSteps = []initStep{
	{
		name: "Provider Setup",
		run: func(ctx context.Context, st *appState, mgr *llm.Service, db *sql.DB, in io.Reader, out io.Writer) error {
			return runProviderSetup(ctx, mgr, db, st.cfg.DbPath, in, out)
		},
	},
	{
		name: "Agent Setup",
		run: func(ctx context.Context, st *appState, mgr *llm.Service, db *sql.DB, in io.Reader, out io.Writer) error {
			return runAgentSetup(ctx, st, mgr, db, in, out)
		},
	},
}

func runInit(ctx context.Context, st *appState, mgr *llm.Service, db *sql.DB, in io.Reader, out io.Writer) error {
	fmt.Fprintln(out, "┌─────────────────────────────────────────┐")
	fmt.Fprintln(out, "│      Welcome to Onclaw Onboarding!      │")
	fmt.Fprintln(out, "└─────────────────────────────────────────┘")
	fmt.Fprintln(out, "This guided setup will configure your device.")
	fmt.Fprintln(out, "")

	for _, step := range initSteps {
		fmt.Fprintf(out, "--- Step: %s ---\n", step.name)
		if err := step.run(ctx, st, mgr, db, in, out); err != nil {
			return err
		}
		fmt.Fprintln(out)
	}

	fmt.Fprintln(out, "┌─────────────────────────────────────────┐")
	fmt.Fprintln(out, "│   Onboarding Completed Successfully!    │")
	fmt.Fprintln(out, "└─────────────────────────────────────────┘")
	return nil
}

func initCommand(st *appState) *cli.Command {
	return &cli.Command{
		Name:  "init",
		Usage: "Initialize onclaw interactively",
		Action: func(ctx context.Context, c *cli.Command) error {
			mgr, db, err := st.getProviderManager(c)
			if err != nil {
				return err
			}
			defer db.Close()

			return runInit(ctx, st, mgr, db, os.Stdin, os.Stdout)
		},
	}
}

func runAgentSetup(ctx context.Context, st *appState, mgr *llm.Service, db *sql.DB, in io.Reader, out io.Writer) error {
	masterAgent, err := st.getOrSeedMasterAgent(ctx, db, mgr)
	if err != nil {
		return fmt.Errorf("get/seed master agent: %w", err)
	}

	fmt.Fprintf(out, "Configuring master agent...\n")
	fmt.Fprintf(out, "Agent name: %s\n", masterAgent.Name)
	fmt.Fprintf(out, "Workspace: %s\n", masterAgent.Workspace)

	profiles, err := mgr.ListProfiles(ctx)
	if err != nil {
		return fmt.Errorf("list profiles: %w", err)
	}

	if len(profiles) == 0 {
		return fmt.Errorf("no provider profiles configured; please configure a provider first")
	}

	var selectedProfile *store.Profile
	if len(profiles) == 1 {
		selectedProfile = profiles[0]
		fmt.Fprintf(out, "Automatically binding master agent to provider %q and model %q.\n", selectedProfile.Name, selectedProfile.Model)
	} else {
		var names []string
		for _, p := range profiles {
			names = append(names, p.Name)
		}
		idx, err := promptChoice("Select provider profile for master agent", names, in, out)
		if err != nil {
			return err
		}
		selectedProfile = profiles[idx]
		fmt.Fprintf(out, "Binding master agent to provider %q and model %q.\n", selectedProfile.Name, selectedProfile.Model)
	}

	masterAgent.Provider = selectedProfile.Name
	masterAgent.Model = selectedProfile.Model

	_, err = db.ExecContext(ctx, "UPDATE agents SET provider = ?, model = ? WHERE name = 'master'", masterAgent.Provider, masterAgent.Model)
	if err != nil {
		return fmt.Errorf("failed to persist master agent configuration: %w", err)
	}

	// Seed workspace
	if err := agent.SeedWorkspace(masterAgent.Workspace); err != nil {
		return fmt.Errorf("seed master agent workspace: %w", err)
	}
	if err := agent.SeedBootstrap(masterAgent.Workspace); err != nil {
		return fmt.Errorf("seed master agent bootstrap: %w", err)
	}
	home, err := os.UserHomeDir()
	if err == nil {
		_ = agent.SeedGlobalUser(filepath.Join(home, ".onclaw"))
	}

	fmt.Fprintln(out, "Agent setup complete. Onboarding is deferred to your first run/chat.")
	return nil
}
