package cli

import (
	"log/slog"

	"github.com/oniharnantyo/onclaw/internal/config"
)

// appState carries runtime resources (resolved config + logger) from the root
// command's Before hook into subcommand actions. Both fields are populated by
// ensure(); subcommands read them through the helper getters which guarantee
// setup has run regardless of how urfave/cli chains Before hooks.
type appState struct {
	cfg *config.Config
	log *slog.Logger
}
