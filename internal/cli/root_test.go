package cli_test

import (
	"context"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/cli"
)

func TestNewRootCommandStructure(t *testing.T) {
	root := cli.NewRootCmd()
	if root == nil {
		t.Fatal("expected root cmd to be non-nil")
	}

	cmd := root.Command()
	if cmd == nil {
		t.Fatal("expected root command to be non-nil")
	}

	if cmd.Name != "onclaw" {
		t.Errorf("cmd.Name = %q, want %q", cmd.Name, "onclaw")
	}

	expectedCommands := map[string]bool{
		"server":     false,
		"migrate":    false,
		"user":       false,
		"superadmin": false,
	}

	for _, sub := range cmd.Commands {
		if _, ok := expectedCommands[sub.Name]; ok {
			expectedCommands[sub.Name] = true
		}
	}

	for name, found := range expectedCommands {
		if !found {
			t.Errorf("expected command %q in root command, but was not found", name)
		}
	}
}

func TestIndividualCommandStructs(t *testing.T) {
	serverCmd := cli.NewServerCmd()
	if serverCmd == nil || serverCmd.Command().Name != "server" {
		t.Errorf("expected serverCmd with name 'server'")
	}

	migrateCmd := cli.NewMigrateCmd()
	if migrateCmd == nil || migrateCmd.Command().Name != "migrate" {
		t.Errorf("expected migrateCmd with name 'migrate'")
	}

	userCmd := cli.NewUserCmd()
	if userCmd == nil || userCmd.Command().Name != "user" {
		t.Errorf("expected userCmd with name 'user'")
	}

	superadminCmd := cli.NewSuperadminCmd()
	if superadminCmd == nil || superadminCmd.Command().Name != "superadmin" {
		t.Errorf("expected superadminCmd with name 'superadmin'")
	}
}

func TestCommandHelp(t *testing.T) {
	cmd := cli.NewRootCommand()
	err := cmd.Run(context.Background(), []string{"onclaw", "--help"})
	if err != nil {
		t.Errorf("unexpected error running help: %v", err)
	}
}
