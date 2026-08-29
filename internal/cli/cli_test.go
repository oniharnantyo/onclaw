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
