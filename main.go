// Command onclaw is the entrypoint for the onclaw AI coding agent CLI. It stays
// intentionally tiny: all command wiring lives in internal/cli, and config +
// logging setup happens in the root command's Before hook so global flags are
// honored.
package main

import (
	"context"
	"log"
	"os"

	clicmd "github.com/oniharnantyo/onclaw/internal/cli"
	_ "github.com/oniharnantyo/onclaw/internal/agent/tools/browser"
)

func main() {
	if err := clicmd.New().Run(context.Background(), os.Args); err != nil {
		log.Fatal(err)
	}
}
