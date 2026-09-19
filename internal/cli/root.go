package cli

import (
	"github.com/urfave/cli/v3"
)

// rootCmd builds the top-level onclaw CLI command tree with its subcommand dependencies.
type rootCmd struct {
	serverCmd     *serverCmd
	migrateCmd    *migrateCmd
	userCmd       *userCmd
	superadminCmd *superadminCmd
	evalMemoryCmd *evalMemoryCmd
}

// NewRootCmd creates a new rootCmd with instantiated subcommand handlers.
func NewRootCmd() *rootCmd {
	return &rootCmd{
		serverCmd:     NewServerCmd(),
		migrateCmd:    NewMigrateCmd(),
		userCmd:       NewUserCmd(),
		superadminCmd: NewSuperadminCmd(),
		evalMemoryCmd: NewEvalMemoryCmd(),
	}
}

// Command builds and returns the top-level *cli.Command.
func (r *rootCmd) Command() *cli.Command {
	return &cli.Command{
		Name:  "onclaw",
		Usage: "OnClaw control plane and backend service",
		Commands: []*cli.Command{
			r.serverCmd.Command(),
			r.migrateCmd.Command(),
			r.userCmd.Command(),
			r.superadminCmd.Command(),
			r.evalMemoryCmd.Command(),
		},
	}
}

// NewRootCommand builds and returns the top-level onclaw CLI command tree.
func NewRootCommand() *cli.Command {
	return NewRootCmd().Command()
}
