package config

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/secrets"
	"github.com/urfave/cli/v3"
)

// Default configuration constants.
const (
	DefaultListenAddr          = ":8080"
	DefaultTokenTTL            = 24 * time.Hour
	DefaultDataDir             = "./data"
	DefaultCacheDir            = ".onclaw/cache"
	DefaultStorageDriver       = "local"
	DefaultRunDrainWindow      = 30 * time.Second
	DefaultSchedulerTick       = 15 * time.Second
	DefaultSchedulerRunTimeout = 10 * time.Minute
	// DefaultHeartbeatTick / DefaultHeartbeatRunTimeout /
	// DefaultHeartbeatConcurrency are the ambient-work defaults
	// (add-agent-heartbeat D14): a slower claim loop than the scheduler's and
	// half its concurrency.
	DefaultHeartbeatTick        = 30 * time.Second
	DefaultHeartbeatRunTimeout  = 10 * time.Minute
	DefaultHeartbeatConcurrency = 2
	DefaultHooksCommandEnabled  = true
	DefaultHooksScriptEnabled   = true
	// DefaultLangfuseSampleRate exports every traced turn; fractions sample
	// deterministically per run (integrate-langfuse-tracing D5).
	DefaultLangfuseSampleRate = 1.0
)

// Config represents runtime configuration assembled from flags, environment variables, and defaults.
type Config struct {
	DatabaseURL            string        `json:"database_url"`
	ListenAddr             string        `json:"listen_addr"`
	JWTSecret              string        `json:"-"`
	EncryptionKey          string        `json:"-"`
	TokenTTL               time.Duration `json:"token_ttl"`
	DataDir                string        `json:"data_dir"`
	CacheDir               string        `json:"cache_dir"`
	OnClawDir              string        `json:"onclaw_dir"`
	StorageDriver          string        `json:"storage_driver"`
	SuperadminEmail        string        `json:"superadmin_email,omitempty"`
	SuperadminPassword     string        `json:"-"`
	SuperadminPasswordFile string        `json:"superadmin_password_file,omitempty"`
	RunDrainWindow         time.Duration `json:"run_drain_window"`
	SchedulerTick          time.Duration `json:"scheduler_tick"`
	SchedulerRunTimeout    time.Duration `json:"scheduler_run_timeout"`
	HeartbeatTick          time.Duration `json:"heartbeat_tick"`
	HeartbeatRunTimeout    time.Duration `json:"heartbeat_run_timeout"`
	HeartbeatConcurrency   int           `json:"heartbeat_concurrency"`
	HooksCommandEnabled    bool          `json:"hooks_command_enabled"`
	HooksScriptEnabled     bool          `json:"hooks_script_enabled"`
	// WhatsAppCloudAPIBase overrides the WhatsApp Cloud API endpoint
	// (add-whatsapp-gateway design D10). Empty keeps graph.facebook.com.
	WhatsAppCloudAPIBase string  `json:"whatsapp_cloud_api_base,omitempty"`
	LangfuseHost         string  `json:"langfuse_host,omitempty"`
	LangfusePublicKey    string  `json:"-"`
	LangfuseSecretKey    string  `json:"-"`
	LangfuseSampleRate   float64 `json:"langfuse_sample_rate"`
}

// WorkspaceRoot returns the derived workspace root directory: <OnClawDir>/workspaces.
func (c *Config) WorkspaceRoot() string {
	return domain.WorkspaceRoot(c.OnClawDir)
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
			Name:    "encryption-key",
			Usage:   "Instance master encryption key (32-byte hex or base64)",
			Sources: cli.EnvVars("ONCLAW_ENCRYPTION_KEY"),
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
			Name:    "cache-dir",
			Value:   DefaultCacheDir,
			Usage:   "Path to cache directory for model catalog and temporary files",
			Sources: cli.EnvVars("ONCLAW_CACHE_DIR"),
		},
		&cli.StringFlag{
			Name:    "onclaw-dir",
			Value:   domain.DefaultOnClawDir(),
			Usage:   "Root directory for OnClaw runtime files",
			Sources: cli.EnvVars("ONCLAW_DIR"),
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
		&cli.DurationFlag{
			Name:    "run-drain-window",
			Value:   DefaultRunDrainWindow,
			Usage:   "Graceful-shutdown window for in-flight agent runs to finish before cancellation",
			Sources: cli.EnvVars("ONCLAW_RUN_DRAIN_WINDOW"),
		},
		&cli.DurationFlag{
			Name:    "scheduler-tick",
			Value:   DefaultSchedulerTick,
			Usage:   "Scheduler claim-loop cadence (how often due standing orders are claimed and fired)",
			Sources: cli.EnvVars("ONCLAW_SCHEDULER_TICK"),
		},
		&cli.DurationFlag{
			Name:    "scheduler-run-timeout",
			Value:   DefaultSchedulerRunTimeout,
			Usage:   "Wall-clock budget for one scheduler run before its event tap is cancelled",
			Sources: cli.EnvVars("ONCLAW_SCHEDULER_RUN_TIMEOUT"),
		},
		&cli.DurationFlag{
			Name:    "heartbeat-tick",
			Value:   DefaultHeartbeatTick,
			Usage:   "Heartbeat claim-loop cadence (how often due agent heartbeats are claimed and fired)",
			Sources: cli.EnvVars("ONCLAW_HEARTBEAT_TICK"),
		},
		&cli.DurationFlag{
			Name:    "heartbeat-run-timeout",
			Value:   DefaultHeartbeatRunTimeout,
			Usage:   "Wall-clock budget for one heartbeat tick before its event tap is cancelled",
			Sources: cli.EnvVars("ONCLAW_HEARTBEAT_RUN_TIMEOUT"),
		},
		&cli.IntFlag{
			Name:    "heartbeat-concurrency",
			Value:   DefaultHeartbeatConcurrency,
			Usage:   "How many heartbeat ticks may fire concurrently (ambient work stays light)",
			Sources: cli.EnvVars("ONCLAW_HEARTBEAT_CONCURRENCY"),
		},
		&cli.BoolFlag{
			Name:    "hooks-command-enabled",
			Value:   DefaultHooksCommandEnabled,
			Usage:   "Enable the command hook handler (kill switch for command-type agent hooks)",
			Sources: cli.EnvVars("ONCLAW_HOOKS_COMMAND_ENABLED"),
		},
		&cli.BoolFlag{
			Name:    "hooks-script-enabled",
			Value:   DefaultHooksScriptEnabled,
			Usage:   "Enable the script hook handler (kill switch for script-type agent hooks)",
			Sources: cli.EnvVars("ONCLAW_HOOKS_SCRIPT_ENABLED"),
		},
		&cli.StringFlag{
			Name:    "whatsapp-cloud-api-base",
			Usage:   "Override the WhatsApp Cloud API endpoint (tests, proxies); default is the public graph.facebook.com",
			Sources: cli.EnvVars("ONCLAW_WHATSAPP_CLOUD_API_BASE"),
		},
		&cli.StringFlag{
			Name:    "langfuse-host",
			Usage:   "Langfuse server URL for optional trace export (e.g. https://langfuse.example.com); unset disables tracing",
			Sources: cli.EnvVars("ONCLAW_LANGFUSE_HOST"),
		},
		&cli.StringFlag{
			Name:    "langfuse-public-key",
			Usage:   "Langfuse public API key (pk-lf-...)",
			Sources: cli.EnvVars("ONCLAW_LANGFUSE_PUBLIC_KEY"),
		},
		&cli.StringFlag{
			Name:    "langfuse-secret-key",
			Usage:   "Langfuse secret API key (sk-lf-...)",
			Sources: cli.EnvVars("ONCLAW_LANGFUSE_SECRET_KEY"),
		},
		&cli.FloatFlag{
			Name:    "langfuse-sample-rate",
			Value:   DefaultLangfuseSampleRate,
			Usage:   "Fraction of turns exported to Langfuse (1.0 = all; fractions sample deterministically per run)",
			Sources: cli.EnvVars("ONCLAW_LANGFUSE_SAMPLE_RATE"),
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
		EncryptionKey:          cmd.String("encryption-key"),
		TokenTTL:               cmd.Duration("token-ttl"),
		DataDir:                cmd.String("data-dir"),
		CacheDir:               cmd.String("cache-dir"),
		OnClawDir:              cmd.String("onclaw-dir"),
		StorageDriver:          cmd.String("storage-driver"),
		SuperadminEmail:        cmd.String("superadmin-email"),
		SuperadminPassword:     cmd.String("superadmin-password"),
		SuperadminPasswordFile: cmd.String("superadmin-password-file"),
		RunDrainWindow:         cmd.Duration("run-drain-window"),
		SchedulerTick:          cmd.Duration("scheduler-tick"),
		SchedulerRunTimeout:    cmd.Duration("scheduler-run-timeout"),
		HeartbeatTick:          cmd.Duration("heartbeat-tick"),
		HeartbeatRunTimeout:    cmd.Duration("heartbeat-run-timeout"),
		HeartbeatConcurrency:   cmd.Int("heartbeat-concurrency"),
		HooksCommandEnabled:    cmd.Bool("hooks-command-enabled"),
		HooksScriptEnabled:     cmd.Bool("hooks-script-enabled"),
		WhatsAppCloudAPIBase:   cmd.String("whatsapp-cloud-api-base"),
		LangfuseHost:           cmd.String("langfuse-host"),
		LangfusePublicKey:      cmd.String("langfuse-public-key"),
		LangfuseSecretKey:      cmd.String("langfuse-secret-key"),
		LangfuseSampleRate:     cmd.Float("langfuse-sample-rate"),
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

// ParseEncryptionKey decodes and validates a 32-byte encryption key from a hex or base64 string.
// If the key is missing or invalid, it returns an error naming ONCLAW_ENCRYPTION_KEY and openssl rand -hex 32.
func ParseEncryptionKey(raw string) ([]byte, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("ONCLAW_ENCRYPTION_KEY is required (generate with: openssl rand -hex 32)")
	}

	key, err := secrets.ParseKey(raw)
	if err != nil {
		return nil, errors.New("invalid ONCLAW_ENCRYPTION_KEY: must decode to exactly 32 bytes as hex or base64 (generate with: openssl rand -hex 32)")
	}
	return key, nil
}

// ParsedEncryptionKey decodes and returns the configured 32-byte encryption key.
func (c *Config) ParsedEncryptionKey() ([]byte, error) {
	return ParseEncryptionKey(c.EncryptionKey)
}

// LangfuseConfigured reports whether Langfuse trace export is switched on:
// all three backend coordinates (host, public key, secret key) are present.
// With any coordinate missing, tracing is off and the composition root must
// not wire the handler (integrate-langfuse-tracing D1).
func (c *Config) LangfuseConfigured() bool {
	return strings.TrimSpace(c.LangfuseHost) != "" &&
		strings.TrimSpace(c.LangfusePublicKey) != "" &&
		strings.TrimSpace(c.LangfuseSecretKey) != ""
}

// ValidateLangfuse checks the optional Langfuse tracing configuration:
// completely unset means disabled (no error); a partially set configuration
// is an error naming the missing ONCLAW_LANGFUSE_* variable so a typo cannot
// silently half-enable tracing; the sample rate must be a fraction in (0, 1]
// whenever it is supplied — a value of 0 is the unset form (the exporter
// applies its 1.0 default) and negative or over-unity values are errors even
// while tracing is otherwise disabled, because set-but-invalid configuration
// fails fast rather than being ignored.
func (c *Config) ValidateLangfuse() error {
	if c.LangfuseSampleRate < 0 || c.LangfuseSampleRate > 1 {
		return errors.New("ONCLAW_LANGFUSE_SAMPLE_RATE must be a fraction in (0, 1] (1.0 exports every turn)")
	}

	host := strings.TrimSpace(c.LangfuseHost)
	publicKey := strings.TrimSpace(c.LangfusePublicKey)
	secretKey := strings.TrimSpace(c.LangfuseSecretKey)

	if host == "" && publicKey == "" && secretKey == "" {
		return nil
	}
	if host == "" {
		return errors.New("langfuse tracing requires ONCLAW_LANGFUSE_HOST when any ONCLAW_LANGFUSE_* variable is set")
	}
	if publicKey == "" {
		return errors.New("langfuse tracing requires ONCLAW_LANGFUSE_PUBLIC_KEY when any ONCLAW_LANGFUSE_* variable is set")
	}
	if secretKey == "" {
		return errors.New("langfuse tracing requires ONCLAW_LANGFUSE_SECRET_KEY when any ONCLAW_LANGFUSE_* variable is set")
	}
	return nil
}
