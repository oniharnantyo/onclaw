package mcp

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/oniharnantyo/onclaw/internal/store"
)

type mockServerStore struct {
	servers []*store.MCPServer
}

func (m *mockServerStore) AddServer(ctx context.Context, s *store.MCPServer) error {
	m.servers = append(m.servers, s)
	return nil
}

func (m *mockServerStore) GetServer(ctx context.Context, name string) (*store.MCPServer, error) {
	for _, s := range m.servers {
		if s.Name == name {
			return s, nil
		}
	}
	return nil, fmt.Errorf("not found")
}

func (m *mockServerStore) ListServers(ctx context.Context) ([]*store.MCPServer, error) {
	return m.servers, nil
}

func (m *mockServerStore) UpdateServer(ctx context.Context, s *store.MCPServer) error {
	return nil
}

func (m *mockServerStore) RemoveServer(ctx context.Context, name string) error {
	return nil
}

func TestManager_Tools(t *testing.T) {
	ctx := context.Background()

	// 1. Create a mock MCP server with a tool
	s1 := server.NewMCPServer("Server1", "1.0.0")
	t1 := mcp.NewTool("echo", mcp.WithDescription("Echo something"))
	s1.AddTool(t1, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultText("hello"), nil
	})

	// 2. Set up store with one enabled and one disabled server
	st := &mockServerStore{
		servers: []*store.MCPServer{
			{
				Name:      "server1",
				Transport: "stdio",
				Enabled:   1,
			},
			{
				Name:      "disabled-server",
				Transport: "stdio",
				Enabled:   0,
			},
			{
				Name:      "bad-server",
				Transport: "stdio",
				Enabled:   1,
			},
		},
	}

	mgr := NewManager(st).(*manager)

	// Mock the connection client
	mgr.connectClient = func(ctx context.Context, srv *store.MCPServer) (*client.Client, error) {
		if srv.Name == "bad-server" {
			return nil, errors.New("connection failed")
		}
		if srv.Name == "server1" {
			cli, err := client.NewInProcessClient(s1)
			if err != nil {
				return nil, err
			}
			initReq := mcp.InitializeRequest{}
			initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
			initReq.Params.ClientInfo = mcp.Implementation{
				Name:    "test",
				Version: "1.0.0",
			}
			if _, err := cli.Initialize(ctx, initReq); err != nil {
				return nil, err
			}
			return cli, nil
		}
		return nil, fmt.Errorf("unknown server: %s", srv.Name)
	}

	// First call to Tools should lazily load
	tools, err := mgr.Tools(ctx)
	if err != nil {
		t.Fatalf("Tools() returned error: %v", err)
	}

	// Verify tools were returned and "bad-server" failure was isolated (did not fail the whole call)
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}

	info, err := tools[0].Info(ctx)
	if err != nil {
		t.Fatalf("failed to get tool info: %v", err)
	}

	if info.Name != "echo" {
		t.Errorf("expected tool name 'echo', got %q", info.Name)
	}

	// Verify caching - calling Tools again should return cached tools without invoking connectClient
	mgr.connectClient = func(ctx context.Context, srv *store.MCPServer) (*client.Client, error) {
		t.Error("connectClient should not be called when cached")
		return nil, errors.New("should not be called")
	}

	cachedTools, err := mgr.Tools(ctx)
	if err != nil {
		t.Fatalf("Tools() on cache returned error: %v", err)
	}
	if len(cachedTools) != 1 {
		t.Fatalf("expected 1 cached tool, got %d", len(cachedTools))
	}

	// Test Close is idempotent and cleans up
	if err := mgr.Close(); err != nil {
		t.Errorf("Close() returned error: %v", err)
	}

	if err := mgr.Close(); err != nil {
		t.Errorf("second Close() returned error (not idempotent): %v", err)
	}
}

func TestManager_Reload(t *testing.T) {
	ctx := context.Background()

	// 1. Create a mock MCP server with a tool
	s1 := server.NewMCPServer("Server1", "1.0.0")
	t1 := mcp.NewTool("echo", mcp.WithDescription("Echo something"))
	s1.AddTool(t1, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultText("hello"), nil
	})

	s2 := server.NewMCPServer("Server2", "1.0.0")
	t2 := mcp.NewTool("search", mcp.WithDescription("Search something"))
	s2.AddTool(t2, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultText("found"), nil
	})

	st := &mockServerStore{
		servers: []*store.MCPServer{
			{
				Name:      "server1",
				Transport: "stdio",
				Enabled:   1,
			},
		},
	}

	mgr := NewManager(st).(*manager)

	mgr.connectClient = func(ctx context.Context, srv *store.MCPServer) (*client.Client, error) {
		var s *server.MCPServer
		if srv.Name == "server1" {
			s = s1
		} else if srv.Name == "server2" {
			s = s2
		} else {
			return nil, fmt.Errorf("unknown server: %s", srv.Name)
		}
		cli, err := client.NewInProcessClient(s)
		if err != nil {
			return nil, err
		}
		initReq := mcp.InitializeRequest{}
		initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
		initReq.Params.ClientInfo = mcp.Implementation{
			Name:    "test",
			Version: "1.0.0",
		}
		if _, err := cli.Initialize(ctx, initReq); err != nil {
			return nil, err
		}
		return cli, nil
	}

	// 2. Call Tools() to cache
	tools, err := mgr.Tools(ctx)
	if err != nil {
		t.Fatalf("Tools() returned error: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}

	// 3. Add server2 to store, but don't reload yet
	st.servers = append(st.servers, &store.MCPServer{
		Name:      "server2",
		Transport: "stdio",
		Enabled:   1,
	})

	// 4. Stale tools cache should still return 1 tool
	tools, err = mgr.Tools(ctx)
	if err != nil {
		t.Fatalf("Tools() returned error: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("expected cached 1 tool, got %d", len(tools))
	}

	// 5. Reload
	mgr.Reload()

	// 6. Tools should now load both
	tools, err = mgr.Tools(ctx)
	if err != nil {
		t.Fatalf("Tools() returned error: %v", err)
	}
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(tools))
	}

	// 7. Verify reload on empty/new state does not panic
	emptyMgr := NewManager(&mockServerStore{}).(*manager)
	emptyMgr.Reload() // should not panic
}
