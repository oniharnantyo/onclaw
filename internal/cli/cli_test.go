package cli_test

import (
	"context"
	"strings"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/cli"
)

func TestSubcommandHelp(t *testing.T) {
	tests := [][]string{
		{"onclaw", "server", "--help"},
		{"onclaw", "migrate", "--help"},
		{"onclaw", "migrate", "up", "--help"},
		{"onclaw", "migrate", "down", "--help"},
		{"onclaw", "migrate", "status", "--help"},
		{"onclaw", "migrate", "version", "--help"},
		{"onclaw", "user", "--help"},
		{"onclaw", "user", "create", "--help"},
		{"onclaw", "user", "list", "--help"},
		{"onclaw", "user", "disable", "--help"},
		{"onclaw", "superadmin", "--help"},
		{"onclaw", "superadmin", "create", "--help"},
	}

	for _, args := range tests {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			cmd := cli.NewRootCommand()
			if err := cmd.Run(context.Background(), args); err != nil {
				t.Fatalf("command %v failed: %v", args, err)
			}
		})
	}
}

func TestMissingDatabaseURL(t *testing.T) {
	tests := [][]string{
		{"onclaw", "server"},
		{"onclaw", "migrate", "up"},
		{"onclaw", "migrate", "down"},
		{"onclaw", "migrate", "status"},
		{"onclaw", "migrate", "version"},
		{"onclaw", "user", "list"},
		{"onclaw", "user", "disable", "--email", "test@example.com"},
	}

	for _, args := range tests {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			cmd := cli.NewRootCommand()
			err := cmd.Run(context.Background(), args)
			if err == nil {
				t.Fatalf("expected error when running %v without DATABASE_URL, got nil", args)
			}
			if !strings.Contains(err.Error(), "database URL is required") && !strings.Contains(err.Error(), "failed to open") {
				t.Logf("got error: %v", err)
			}
		})
	}
}

func TestServerEncryptionKeyRequirement(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantSubstr []string
	}{
		{
			name: "server without encryption key",
			args: []string{"onclaw", "server", "--database-url", "postgres://localhost:5432/test"},
			wantSubstr: []string{"ONCLAW_ENCRYPTION_KEY", "openssl rand -hex 32"},
		},
		{
			name: "server with short encryption key",
			args: []string{"onclaw", "server", "--database-url", "postgres://localhost:5432/test", "--encryption-key", "short"},
			wantSubstr: []string{"ONCLAW_ENCRYPTION_KEY", "openssl rand -hex 32"},
		},
		{
			name: "server with invalid hex encryption key",
			args: []string{"onclaw", "server", "--database-url", "postgres://localhost:5432/test", "--encryption-key", "not-a-valid-key"},
			wantSubstr: []string{"ONCLAW_ENCRYPTION_KEY", "openssl rand -hex 32"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := cli.NewRootCommand()
			err := cmd.Run(context.Background(), tt.args)
			if err == nil {
				t.Fatalf("expected error for %v, got nil", tt.args)
			}
			for _, sub := range tt.wantSubstr {
				if !strings.Contains(err.Error(), sub) {
					t.Errorf("error %q does not contain expected substring %q", err.Error(), sub)
				}
			}
		})
	}
}
