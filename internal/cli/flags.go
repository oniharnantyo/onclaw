package cli

import "github.com/urfave/cli/v3"

// globalFlags are defined on the root command and override config + env for
// every subcommand. Env binding is handled by Viper (ONCLAW_*); these flags are
// the highest-priority override layer.
func globalFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:  "config",
			Usage: "path to config file (default: search ., ~/.config/onclaw, /etc/onclaw)",
		},
		&cli.StringFlag{
			Name:  "log-level",
			Usage: "log level: debug|info|warn|error (overrides config / ONCLAW_LOG_LEVEL)",
		},
		&cli.StringFlag{
			Name:  "log-format",
			Usage: "log format: text|json (overrides config / ONCLAW_LOG_FORMAT)",
		},
	}
}
