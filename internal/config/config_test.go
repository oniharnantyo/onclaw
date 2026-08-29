package config_test

import (
	"context"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/config"
	"github.com/urfave/cli/v3"
)

func TestFromServerContextDefaults(t *testing.T) {
	var parsedCfg *config.Config

	cmd := &cli.Command{
		Name:  "server",
		Flags: config.ServerFlags(),
		Action: func(ctx context.Context, c *cli.Command) error {
			parsedCfg = config.FromServerContext(ctx, c)
			return nil
		},
	}

	err := cmd.Run(context.Background(), []string{"server"})
	if err != nil {
		t.Fatalf("unexpected error running command: %v", err)
	}

	if parsedCfg == nil {
		t.Fatal("expected parsedCfg to be non-nil")
	}

	if parsedCfg.ListenAddr != config.DefaultListenAddr {
		t.Errorf("ListenAddr = %q, want %q", parsedCfg.ListenAddr, config.DefaultListenAddr)
	}
	if parsedCfg.TokenTTL != config.DefaultTokenTTL {
		t.Errorf("TokenTTL = %v, want %v", parsedCfg.TokenTTL, config.DefaultTokenTTL)
	}
	if parsedCfg.DataDir != config.DefaultDataDir {
		t.Errorf("DataDir = %q, want %q", parsedCfg.DataDir, config.DefaultDataDir)
	}
	if parsedCfg.StorageDriver != config.DefaultStorageDriver {
		t.Errorf("StorageDriver = %q, want %q", parsedCfg.StorageDriver, config.DefaultStorageDriver)
	}
	if parsedCfg.DatabaseURL != "" {
		t.Errorf("DatabaseURL = %q, want empty", parsedCfg.DatabaseURL)
	}
}

func TestFromServerContextCustomFlags(t *testing.T) {
	var parsedCfg *config.Config

	cmd := &cli.Command{
		Name:  "server",
		Flags: config.ServerFlags(),
		Action: func(ctx context.Context, c *cli.Command) error {
			parsedCfg = config.FromServerContext(ctx, c)
			return nil
		},
	}

	args := []string{
		"server",
		"--listen-addr", ":9090",
		"--database-url", "postgres://user:pass@localhost:5432/testdb",
		"--jwt-secret", "supersecretkey",
		"--token-ttl", "12h",
		"--data-dir", "/tmp/data",
		"--storage-driver", "local",
		"--superadmin-email", "admin@example.com",
		"--superadmin-password", "adminpass123",
	}

	err := cmd.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error running command: %v", err)
	}

	if parsedCfg.ListenAddr != ":9090" {
		t.Errorf("ListenAddr = %q, want %q", parsedCfg.ListenAddr, ":9090")
	}
	if parsedCfg.DatabaseURL != "postgres://user:pass@localhost:5432/testdb" {
		t.Errorf("DatabaseURL = %q, want postgres DSN", parsedCfg.DatabaseURL)
	}
	if parsedCfg.JWTSecret != "supersecretkey" {
		t.Errorf("JWTSecret = %q, want %q", parsedCfg.JWTSecret, "supersecretkey")
	}
	if parsedCfg.TokenTTL != 12*time.Hour {
		t.Errorf("TokenTTL = %v, want 12h", parsedCfg.TokenTTL)
	}
	if parsedCfg.DataDir != "/tmp/data" {
		t.Errorf("DataDir = %q, want %q", parsedCfg.DataDir, "/tmp/data")
	}
	if parsedCfg.SuperadminEmail != "admin@example.com" {
		t.Errorf("SuperadminEmail = %q, want %q", parsedCfg.SuperadminEmail, "admin@example.com")
	}
	if parsedCfg.SuperadminPassword != "adminpass123" {
		t.Errorf("SuperadminPassword = %q, want %q", parsedCfg.SuperadminPassword, "adminpass123")
	}
}

func TestFromMigrateContext(t *testing.T) {
	var parsedCfg *config.Config

	cmd := &cli.Command{
		Name:  "migrate",
		Flags: config.MigrateFlags(),
		Action: func(ctx context.Context, c *cli.Command) error {
			parsedCfg = config.FromMigrateContext(ctx, c)
			return nil
		},
	}

	err := cmd.Run(context.Background(), []string{"migrate", "--database-url", "postgres://localhost:5432/db"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if parsedCfg.DatabaseURL != "postgres://localhost:5432/db" {
		t.Errorf("DatabaseURL = %q, want postgres://localhost:5432/db", parsedCfg.DatabaseURL)
	}
}
