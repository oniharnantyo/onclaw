package mcp

import (
	"context"

	"github.com/mark3labs/mcp-go/client"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// SetConnectClient allows stubbing/mocking of connectClient in tests.
func SetConnectClient(m Manager, fn func(context.Context, *store.MCPServer) (*client.Client, error)) {
	m.(*manager).connectClient = fn
}
