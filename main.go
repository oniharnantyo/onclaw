package main

import (
	"context"
	"fmt"
	"os"

	"github.com/oniharnantyo/onclaw/internal/cli"
	"github.com/oniharnantyo/onclaw/internal/config"
)

func main() {
	if err := config.LoadDotEnv(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	cmd := cli.NewRootCommand()
	if err := cmd.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
