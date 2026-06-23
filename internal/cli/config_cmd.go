package cli

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/urfave/cli/v3"
)

func configCommand(st *appState) *cli.Command {
	return &cli.Command{
		Name:  "config",
		Usage: "Inspect onclaw configuration",
		Commands: []*cli.Command{
			{
				Name:  "show",
				Usage: "Print the resolved configuration (defaults < file < env < flags)",
				Action: func(ctx context.Context, c *cli.Command) error {
					if err := st.ensure(c); err != nil {
						return err
					}
					b, err := json.MarshalIndent(st.cfg, "", "  ")
					if err != nil {
						return err
					}
					fmt.Println(string(b))
					return nil
				},
			},
			{
				Name:  "path",
				Usage: "Print the config file in use and all searched paths",
				Action: func(ctx context.Context, c *cli.Command) error {
					if err := st.ensure(c); err != nil {
						return err
					}
					if st.cfg.LoadedFrom == "" {
						fmt.Println("No config file found; using defaults + env.")
					} else {
						fmt.Println("Config file:", st.cfg.LoadedFrom)
					}
					fmt.Println("Searched paths:")
					for _, p := range st.cfg.SearchPaths {
						fmt.Println("  -", p)
					}
					return nil
				},
			},
		},
	}
}
