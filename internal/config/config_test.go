package config_test

import (
	"context"
	"strings"
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
	if parsedCfg.CacheDir != config.DefaultCacheDir {
		t.Errorf("CacheDir = %q, want %q", parsedCfg.CacheDir, config.DefaultCacheDir)
	}
	if parsedCfg.OnClawDir == "" {
		t.Errorf("OnClawDir = %q, want non-empty default", parsedCfg.OnClawDir)
	}
	if !strings.HasSuffix(parsedCfg.WorkspaceRoot(), "workspaces") {
		t.Errorf("WorkspaceRoot = %q, want path ending in workspaces", parsedCfg.WorkspaceRoot())
	}
	if parsedCfg.StorageDriver != config.DefaultStorageDriver {
		t.Errorf("StorageDriver = %q, want %q", parsedCfg.StorageDriver, config.DefaultStorageDriver)
	}
	if parsedCfg.RunDrainWindow != config.DefaultRunDrainWindow {
		t.Errorf("RunDrainWindow = %v, want %v", parsedCfg.RunDrainWindow, config.DefaultRunDrainWindow)
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
		"--cache-dir", "/tmp/cache",
		"--onclaw-dir", "/tmp/onclaw",
		"--storage-driver", "local",
		"--superadmin-email", "admin@example.com",
		"--superadmin-password", "adminpass123",
		"--run-drain-window", "45s",
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
	if parsedCfg.CacheDir != "/tmp/cache" {
		t.Errorf("CacheDir = %q, want %q", parsedCfg.CacheDir, "/tmp/cache")
	}
	if parsedCfg.OnClawDir != "/tmp/onclaw" {
		t.Errorf("OnClawDir = %q, want %q", parsedCfg.OnClawDir, "/tmp/onclaw")
	}
	if parsedCfg.WorkspaceRoot() != "/tmp/onclaw/workspaces" {
		t.Errorf("WorkspaceRoot() = %q, want %q", parsedCfg.WorkspaceRoot(), "/tmp/onclaw/workspaces")
	}
	if parsedCfg.SuperadminEmail != "admin@example.com" {
		t.Errorf("SuperadminEmail = %q, want %q", parsedCfg.SuperadminEmail, "admin@example.com")
	}
	if parsedCfg.SuperadminPassword != "adminpass123" {
		t.Errorf("SuperadminPassword = %q, want %q", parsedCfg.SuperadminPassword, "adminpass123")
	}
	if parsedCfg.RunDrainWindow != 45*time.Second {
		t.Errorf("RunDrainWindow = %v, want 45s", parsedCfg.RunDrainWindow)
	}
}

func TestFromServerContextRunDrainWindowEnv(t *testing.T) {
	var parsedCfg *config.Config

	t.Setenv("ONCLAW_RUN_DRAIN_WINDOW", "90s")

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

	if parsedCfg.RunDrainWindow != 90*time.Second {
		t.Errorf("RunDrainWindow = %v, want 90s from ONCLAW_RUN_DRAIN_WINDOW", parsedCfg.RunDrainWindow)
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

func TestFromServerContextEncryptionKey(t *testing.T) {
	var parsedCfg *config.Config

	cmd := &cli.Command{
		Name:  "server",
		Flags: config.ServerFlags(),
		Action: func(ctx context.Context, c *cli.Command) error {
			parsedCfg = config.FromServerContext(ctx, c)
			return nil
		},
	}

	hexKey := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	args := []string{
		"server",
		"--encryption-key", hexKey,
	}

	err := cmd.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if parsedCfg.EncryptionKey != hexKey {
		t.Errorf("EncryptionKey = %q, want %q", parsedCfg.EncryptionKey, hexKey)
	}

	keyBytes, err := parsedCfg.ParsedEncryptionKey()
	if err != nil {
		t.Fatalf("ParsedEncryptionKey failed: %v", err)
	}
	if len(keyBytes) != 32 {
		t.Errorf("key length = %d, want 32", len(keyBytes))
	}
}

func TestParseEncryptionKey(t *testing.T) {
	validHex := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	validHexUpper := "0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF"
	// 32 bytes: "01234567890123456789012345678901"
	validStdB64 := "MDEyMzQ1Njc4OTAxMjM0NTY3ODkwMTIzNDU2Nzg5MDE="
	validRawStdB64 := "MDEyMzQ1Njc4OTAxMjM0NTY3ODkwMTIzNDU2Nzg5MDE"
	// 32 bytes with base64 url-safe chars (- and _):
	// byte slice with 0xfb, 0xff -> "+/" in std, "-_" in url
	// 30 zeros followed by 0xfb, 0xff
	validURLB64 := "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA-_8="
	validRawURLB64 := "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA-_8"

	tests := []struct {
		name        string
		rawKey      string
		expectErr   bool
		expectedLen int
	}{
		{
			name:        "valid 32-byte lowercase hex",
			rawKey:      validHex,
			expectErr:   false,
			expectedLen: 32,
		},
		{
			name:        "valid 32-byte uppercase hex",
			rawKey:      validHexUpper,
			expectErr:   false,
			expectedLen: 32,
		},
		{
			name:        "valid 32-byte hex with leading/trailing whitespace",
			rawKey:      "  " + validHex + "  \n",
			expectErr:   false,
			expectedLen: 32,
		},
		{
			name:        "valid 32-byte standard base64",
			rawKey:      validStdB64,
			expectErr:   false,
			expectedLen: 32,
		},
		{
			name:        "valid 32-byte raw standard base64",
			rawKey:      validRawStdB64,
			expectErr:   false,
			expectedLen: 32,
		},
		{
			name:        "valid 32-byte URL base64",
			rawKey:      validURLB64,
			expectErr:   false,
			expectedLen: 32,
		},
		{
			name:        "valid 32-byte raw URL base64",
			rawKey:      validRawURLB64,
			expectErr:   false,
			expectedLen: 32,
		},
		{
			name:      "empty string",
			rawKey:    "",
			expectErr: true,
		},
		{
			name:      "whitespace only",
			rawKey:    "   \t\n",
			expectErr: true,
		},
		{
			name:      "hex too short (16 bytes / 32 hex chars)",
			rawKey:    "0123456789abcdef0123456789abcdef",
			expectErr: true,
		},
		{
			name:      "hex too long (33 bytes / 66 hex chars)",
			rawKey:    "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdefaa",
			expectErr: true,
		},
		{
			name:      "base64 too short (16 bytes)",
			rawKey:    "MDEyMzQ1Njc4OTAxMjM0NQ==",
			expectErr: true,
		},
		{
			name:      "base64 too long (48 bytes)",
			rawKey:    "MDEyMzQ1Njc4OTAxMjM0NTY3ODkwMTIzNDU2Nzg5MDEyMzQ1Njc4OTAxMjM0NTY3",
			expectErr: true,
		},
		{
			name:      "invalid characters (neither hex nor base64)",
			rawKey:    "not a valid key!@#$%^&*()",
			expectErr: true,
		},
		{
			name:      "arbitrary short word",
			rawKey:    "short",
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, err := config.ParseEncryptionKey(tt.rawKey)
			if tt.expectErr {
				if err == nil {
					t.Errorf("ParseEncryptionKey(%q) expected error, got nil", tt.rawKey)
				}
				// All errors must name ONCLAW_ENCRYPTION_KEY and openssl rand -hex 32
				errStr := err.Error()
				if !strings.Contains(errStr, "ONCLAW_ENCRYPTION_KEY") {
					t.Errorf("error %q does not contain 'ONCLAW_ENCRYPTION_KEY'", errStr)
				}
				if !strings.Contains(errStr, "openssl rand -hex 32") {
					t.Errorf("error %q does not contain 'openssl rand -hex 32'", errStr)
				}
			} else {
				if err != nil {
					t.Fatalf("ParseEncryptionKey(%q) unexpected error: %v", tt.rawKey, err)
				}
				if len(key) != tt.expectedLen {
					t.Errorf("key length = %d, want %d", len(key), tt.expectedLen)
				}
			}
		})
	}
}

func TestFromServerContextLangfuseDefaults(t *testing.T) {
	var parsedCfg *config.Config

	cmd := &cli.Command{
		Name:  "server",
		Flags: config.ServerFlags(),
		Action: func(ctx context.Context, c *cli.Command) error {
			parsedCfg = config.FromServerContext(ctx, c)
			return nil
		},
	}

	if err := cmd.Run(context.Background(), []string{"server"}); err != nil {
		t.Fatalf("unexpected error running command: %v", err)
	}

	if parsedCfg.LangfuseHost != "" {
		t.Errorf("LangfuseHost = %q, want empty (tracing disabled)", parsedCfg.LangfuseHost)
	}
	if parsedCfg.LangfusePublicKey != "" || parsedCfg.LangfuseSecretKey != "" {
		t.Errorf("Langfuse keys = %q/%q, want empty (tracing disabled)", parsedCfg.LangfusePublicKey, parsedCfg.LangfuseSecretKey)
	}
	if parsedCfg.LangfuseSampleRate != config.DefaultLangfuseSampleRate {
		t.Errorf("LangfuseSampleRate = %g, want %g", parsedCfg.LangfuseSampleRate, config.DefaultLangfuseSampleRate)
	}
	if parsedCfg.LangfuseConfigured() {
		t.Error("LangfuseConfigured() = true for unset configuration, want false")
	}
	if err := parsedCfg.ValidateLangfuse(); err != nil {
		t.Errorf("ValidateLangfuse() = %v, want nil for unset configuration", err)
	}
}

func TestFromServerContextLangfuseFlags(t *testing.T) {
	var parsedCfg *config.Config

	args := []string{
		"server",
		"--langfuse-host", "https://langfuse.example.com",
		"--langfuse-public-key", "pk-lf-test",
		"--langfuse-secret-key", "sk-lf-test",
		"--langfuse-sample-rate", "0.25",
	}

	cmd := &cli.Command{
		Name:  "server",
		Flags: config.ServerFlags(),
		Action: func(ctx context.Context, c *cli.Command) error {
			parsedCfg = config.FromServerContext(ctx, c)
			return nil
		},
	}

	if err := cmd.Run(context.Background(), args); err != nil {
		t.Fatalf("unexpected error running command: %v", err)
	}

	if parsedCfg.LangfuseHost != "https://langfuse.example.com" {
		t.Errorf("LangfuseHost = %q, want https://langfuse.example.com", parsedCfg.LangfuseHost)
	}
	if parsedCfg.LangfusePublicKey != "pk-lf-test" {
		t.Errorf("LangfusePublicKey = %q, want pk-lf-test", parsedCfg.LangfusePublicKey)
	}
	if parsedCfg.LangfuseSecretKey != "sk-lf-test" {
		t.Errorf("LangfuseSecretKey = %q, want sk-lf-test", parsedCfg.LangfuseSecretKey)
	}
	if parsedCfg.LangfuseSampleRate != 0.25 {
		t.Errorf("LangfuseSampleRate = %g, want 0.25", parsedCfg.LangfuseSampleRate)
	}
	if !parsedCfg.LangfuseConfigured() {
		t.Error("LangfuseConfigured() = false for fully set configuration, want true")
	}
	if err := parsedCfg.ValidateLangfuse(); err != nil {
		t.Errorf("ValidateLangfuse() = %v, want nil for fully set configuration", err)
	}
}

func TestFromServerContextLangfuseEnv(t *testing.T) {
	var parsedCfg *config.Config

	t.Setenv("ONCLAW_LANGFUSE_HOST", "https://langfuse.internal:3000")
	t.Setenv("ONCLAW_LANGFUSE_PUBLIC_KEY", "pk-lf-env")
	t.Setenv("ONCLAW_LANGFUSE_SECRET_KEY", "sk-lf-env")
	t.Setenv("ONCLAW_LANGFUSE_SAMPLE_RATE", "0.5")

	cmd := &cli.Command{
		Name:  "server",
		Flags: config.ServerFlags(),
		Action: func(ctx context.Context, c *cli.Command) error {
			parsedCfg = config.FromServerContext(ctx, c)
			return nil
		},
	}

	if err := cmd.Run(context.Background(), []string{"server"}); err != nil {
		t.Fatalf("unexpected error running command: %v", err)
	}

	if parsedCfg.LangfuseHost != "https://langfuse.internal:3000" {
		t.Errorf("LangfuseHost = %q, want value from ONCLAW_LANGFUSE_HOST", parsedCfg.LangfuseHost)
	}
	if parsedCfg.LangfusePublicKey != "pk-lf-env" {
		t.Errorf("LangfusePublicKey = %q, want value from ONCLAW_LANGFUSE_PUBLIC_KEY", parsedCfg.LangfusePublicKey)
	}
	if parsedCfg.LangfuseSecretKey != "sk-lf-env" {
		t.Errorf("LangfuseSecretKey = %q, want value from ONCLAW_LANGFUSE_SECRET_KEY", parsedCfg.LangfuseSecretKey)
	}
	if parsedCfg.LangfuseSampleRate != 0.5 {
		t.Errorf("LangfuseSampleRate = %g, want value from ONCLAW_LANGFUSE_SAMPLE_RATE", parsedCfg.LangfuseSampleRate)
	}
	if !parsedCfg.LangfuseConfigured() {
		t.Error("LangfuseConfigured() = false for env-configured tracing, want true")
	}
}

func TestValidateLangfusePartialConfig(t *testing.T) {
	tests := []struct {
		name        string
		cfg         config.Config
		errContains string
	}{
		{
			name:        "host only",
			cfg:         config.Config{LangfuseHost: "https://langfuse.example.com"},
			errContains: "ONCLAW_LANGFUSE_PUBLIC_KEY",
		},
		{
			name:        "host and public key, missing secret key",
			cfg:         config.Config{LangfuseHost: "https://langfuse.example.com", LangfusePublicKey: "pk-lf-test"},
			errContains: "ONCLAW_LANGFUSE_SECRET_KEY",
		},
		{
			name:        "keys without host",
			cfg:         config.Config{LangfusePublicKey: "pk-lf-test", LangfuseSecretKey: "sk-lf-test"},
			errContains: "ONCLAW_LANGFUSE_HOST",
		},
		{
			name:        "full config with over-unity sample rate",
			cfg:         config.Config{LangfuseHost: "https://langfuse.example.com", LangfusePublicKey: "pk", LangfuseSecretKey: "sk", LangfuseSampleRate: 1.5},
			errContains: "ONCLAW_LANGFUSE_SAMPLE_RATE",
		},
		{
			name:        "full config with negative sample rate",
			cfg:         config.Config{LangfuseHost: "https://langfuse.example.com", LangfusePublicKey: "pk", LangfuseSecretKey: "sk", LangfuseSampleRate: -0.1},
			errContains: "ONCLAW_LANGFUSE_SAMPLE_RATE",
		},
		{
			name:        "bad sample rate with tracing otherwise disabled",
			cfg:         config.Config{LangfuseSampleRate: 7},
			errContains: "ONCLAW_LANGFUSE_SAMPLE_RATE",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.ValidateLangfuse()
			if err == nil {
				t.Fatalf("ValidateLangfuse() = nil, want error containing %q", tt.errContains)
			}
			if !strings.Contains(err.Error(), tt.errContains) {
				t.Errorf("error %q does not contain %q", err.Error(), tt.errContains)
			}
		})
	}
}

func TestValidateLangfuseBoundariesAndUnsetForms(t *testing.T) {
	tests := []struct {
		name string
		cfg  config.Config
		want bool // want nil error
	}{
		{name: "zero-value config", cfg: config.Config{}, want: true},
		{name: "zero sample rate is the unset form", cfg: config.Config{LangfuseSampleRate: 0}, want: true},
		{name: "rate 1.0 with full config", cfg: config.Config{LangfuseHost: "h", LangfusePublicKey: "pk", LangfuseSecretKey: "sk", LangfuseSampleRate: 1.0}, want: true},
		{name: "fractional rate with full config", cfg: config.Config{LangfuseHost: "h", LangfusePublicKey: "pk", LangfuseSecretKey: "sk", LangfuseSampleRate: 0.001}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.ValidateLangfuse()
			if tt.want && err != nil {
				t.Fatalf("ValidateLangfuse() = %v, want nil", err)
			}
			if !tt.want && err == nil {
				t.Fatal("ValidateLangfuse() = nil, want error")
			}
		})
	}
}
