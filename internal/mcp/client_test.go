package mcp_test

import (
	"context"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/mcp"
	"github.com/oniharnantyo/onclaw/internal/store"
)

func TestNewClient_InvalidTransport(t *testing.T) {
	ctx := context.Background()
	srv := &store.MCPServer{
		Name:      "invalid",
		Transport: "invalid_transport",
	}
	cli, err := mcp.NewClient(ctx, srv)
	if err == nil {
		t.Error("expected error for invalid transport, got nil")
	}
	if cli != nil {
		t.Errorf("expected nil client, got %v", cli)
	}
}

func TestNewClient_InvalidArgsJSON(t *testing.T) {
	ctx := context.Background()
	srv := &store.MCPServer{
		Name:      "invalid-args",
		Transport: "stdio",
		Command:   "echo",
		Args:      "{invalid json}",
	}
	_, err := mcp.NewClient(ctx, srv)
	if err == nil {
		t.Error("expected error for invalid args JSON, got nil")
	}
}

func TestNewClient_InvalidEnvJSON(t *testing.T) {
	ctx := context.Background()
	srv := &store.MCPServer{
		Name:      "invalid-env",
		Transport: "stdio",
		Command:   "echo",
		Env:       "{invalid json}",
	}
	_, err := mcp.NewClient(ctx, srv)
	if err == nil {
		t.Error("expected error for invalid env JSON, got nil")
	}
}

func TestNewClient_StdioCommandNotFound(t *testing.T) {
	ctx := context.Background()
	srv := &store.MCPServer{
		Name:      "non-existent-cmd",
		Transport: "stdio",
		Command:   "non_existent_command_12345",
		Args:      `[]`,
		Env:       `{"KEY": "VALUE"}`,
	}
	_, err := mcp.NewClient(ctx, srv)
	if err == nil {
		t.Error("expected error for non-existent command, got nil")
	}
}
