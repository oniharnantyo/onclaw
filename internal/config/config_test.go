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
