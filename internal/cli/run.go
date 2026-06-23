package cli

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"
)

// runCommand is the SAMPLE subcommand. It demonstrates the full wiring pattern
// (ensure resources are loaded, read args, log structured context, print to
// stdout). The real agent implementation will replace the placeholder body.
func runCommand(st *appState) *cli.Command {
	return &cli.Command{
		Name:  "run",
		Usage: "Run a prompt through the onclaw agent (placeholder)",
		ArgsUsage: "[prompt]",
		Description: "SAMPLE subcommand demonstrating the wiring pattern. Reads a prompt " +
			"argument, logs the resolved configuration, and prints a placeholder. " +
			"Real agent logic will be implemented here.",
		Action: func(ctx context.Context, c *cli.Command) error {
			if err := st.ensure(c); err != nil {
				return err
			}

			prompt := c.Args().First()
			if prompt == "" {
				prompt = "(no prompt provided)"
			}

			st.log.Info("run invoked",
				"prompt", prompt,
				"model", st.cfg.Model,
				"log_level", st.cfg.LogLevel,
				"concurrency", st.cfg.Concurrency,
			)

			fmt.Println("onclaw agent is not implemented yet (boilerplate).")
			fmt.Printf("prompt: %s\n", prompt)
			return nil
		},
	}
}
