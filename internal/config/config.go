package config

import (
	"context"
	"time"

	"github.com/urfave/cli/v3"
)

// Default configuration constants.
const (
	DefaultListenAddr    = ":8080"
	DefaultTokenTTL      = 24 * time.Hour
	DefaultDataDir       = "./data"
	DefaultStorageDriver = "local"
)

// Config represents runtime configuration assembled from flags, environment variables, and defaults.
type Config struct {
	DatabaseURL            string        `json:"database_url"`
	ListenAddr             string        `json:"listen_addr"`
	JWTSecret              string        `json:"-"`
	TokenTTL               time.Duration `json:"token_ttl"`
	DataDir                string        `json:"data_dir"`
	StorageDriver          string        `json:"storage_driver"`
	SuperadminEmail        string        `json:"superadmin_email,omitempty"`
	SuperadminPassword     string        `json:"-"`
	SuperadminPasswordFile string        `json:"superadmin_password_file,omitempty"`
}

// ServerFlags returns flags for the `server` command.
func ServerFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:    "listen-addr",
			Value:   DefaultListenAddr,
			Usage:   "Server listen address",
			Sources: cli.EnvVars("ONCLAW_LISTEN_ADDR"),
		},
		&cli.StringFlag{
			Name:    "database-url",
			Usage:   "PostgreSQL connection DSN",
			Sources: cli.EnvVars("DATABASE_URL"),
		},
		&cli.StringFlag{
			Name:    "jwt-secret",
			Usage:   "JWT signing secret (HS256)",
			Sources: cli.EnvVars("ONCLAW_JWT_SECRET"),
		},
		&cli.DurationFlag{
			Name:    "token-ttl",
			Value:   DefaultTokenTTL,
			Usage:   "JWT token time-to-live",
			Sources: cli.EnvVars("ONCLAW_TOKEN_TTL"),
		},
		&cli.StringFlag{
			Name:    "data-dir",
			Value:   DefaultDataDir,
			Usage:   "Path to data directory for local storage",
			Sources: cli.EnvVars("ONCLAW_DATA_DIR"),
		},
		&cli.StringFlag{
			Name:    "storage-driver",
			Value:   DefaultStorageDriver,
			Usage:   "Storage driver name",
			Sources: cli.EnvVars("ONCLAW_STORAGE_DRIVER"),
		},
		&cli.StringFlag{
			Name:    "superadmin-email",
			Usage:   "Superadmin email for initial instance seeding",
			Sources: cli.EnvVars("ONCLAW_SUPERADMIN_EMAIL"),
		},
		&cli.StringFlag{
			Name:    "superadmin-password",
			Usage:   "Superadmin password for initial instance seeding",
			Sources: cli.EnvVars("ONCLAW_SUPERADMIN_PASSWORD"),
		},
		&cli.StringFlag{
			Name:    "superadmin-password-file",
			Usage:   "Path to file containing superadmin password",
			Sources: cli.EnvVars("ONCLAW_SUPERADMIN_PASSWORD_FILE"),
		},
	}
}

// MigrateFlags returns flags for `migrate` commands.
func MigrateFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:    "database-url",
			Usage:   "PostgreSQL connection DSN",
			Sources: cli.EnvVars("DATABASE_URL"),
		},
	}
}

// UserFlags returns common flags for `user` commands.
func UserFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:    "database-url",
			Usage:   "PostgreSQL connection DSN",
			Sources: cli.EnvVars("DATABASE_URL"),
		},
		&cli.StringFlag{
			Name:    "data-dir",
			Value:   DefaultDataDir,
			Usage:   "Path to data directory for local storage",
			Sources: cli.EnvVars("ONCLAW_DATA_DIR"),
		},
		&cli.StringFlag{
			Name:    "storage-driver",
			Value:   DefaultStorageDriver,
			Usage:   "Storage driver name",
			Sources: cli.EnvVars("ONCLAW_STORAGE_DRIVER"),
		},
	}
}

// SuperadminFlags returns flags for `superadmin` commands.
func SuperadminFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:    "database-url",
			Usage:   "PostgreSQL connection DSN",
			Sources: cli.EnvVars("DATABASE_URL"),
		},
	}
}

// FromServerContext constructs a Config from the server command's CLI context.
func FromServerContext(ctx context.Context, cmd *cli.Command) *Config {
	return &Config{
		ListenAddr:             cmd.String("listen-addr"),
		DatabaseURL:            cmd.String("database-url"),
		JWTSecret:              cmd.String("jwt-secret"),
		TokenTTL:               cmd.Duration("token-ttl"),
		DataDir:                cmd.String("data-dir"),
		StorageDriver:          cmd.String("storage-driver"),
		SuperadminEmail:        cmd.String("superadmin-email"),
		SuperadminPassword:     cmd.String("superadmin-password"),
		SuperadminPasswordFile: cmd.String("superadmin-password-file"),
	}
}

// FromMigrateContext constructs a Config from the migrate command's CLI context.
func FromMigrateContext(ctx context.Context, cmd *cli.Command) *Config {
	return &Config{
		DatabaseURL: cmd.String("database-url"),
	}
}

// FromUserContext constructs a Config from the user command's CLI context.
func FromUserContext(ctx context.Context, cmd *cli.Command) *Config {
	return &Config{
		DatabaseURL:   cmd.String("database-url"),
		DataDir:       cmd.String("data-dir"),
		StorageDriver: cmd.String("storage-driver"),
	}
}

// FromSuperadminContext constructs a Config from the superadmin command's CLI context.
func FromSuperadminContext(ctx context.Context, cmd *cli.Command) *Config {
	return &Config{
		DatabaseURL: cmd.String("database-url"),
	}
}
