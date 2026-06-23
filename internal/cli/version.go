package cli

import (
	"context"
	"fmt"

	"github.com/oniharnantyo/onclaw/internal/version"
	"github.com/urfave/cli/v3"
)

func versionCommand() *cli.Command {
	return &cli.Command{
		Name:   "version",
		Usage:  "Print the onclaw version",
		Action: func(context.Context, *cli.Command) error { fmt.Println(version.String()); return nil },
	}
}
