package mcp

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	mcpp "github.com/cloudwego/eino-ext/components/tool/mcp"
	"github.com/cloudwego/eino/components/tool"
	"github.com/mark3labs/mcp-go/client"
	"github.com/oniharnantyo/onclaw/internal/agent/tools"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// Manager manages the lifecycle of multiple MCP client connections.
type Manager interface {
	// Tools discovers and aggregates tools across all enabled MCP servers.
	Tools(ctx context.Context) ([]tool.BaseTool, error)
	// Close cleanly closes all active MCP client connections.
	Close() error
	// Reload drops all active clients and tools cache to force reload on the next Tools call.
	Reload()
}

type manager struct {
	store         store.MCPServerStore
	clients       map[string]*client.Client
	tools         []tool.BaseTool
	mu            sync.Mutex
	loaded        bool
	connectClient func(ctx context.Context, srv *store.MCPServer) (*client.Client, error)
}

// NewManager creates a new MCP Manager.
func NewManager(s store.MCPServerStore) Manager {
	return &manager{
		store:         s,
		clients:       make(map[string]*client.Client),
		connectClient: NewClient,
	}
}

func (m *manager) Tools(ctx context.Context) ([]tool.BaseTool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.loaded {
		return m.tools, nil
	}

	servers, err := m.store.ListServers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list mcp servers: %w", err)
	}

	var allTools []tool.BaseTool

	for _, srv := range servers {
		if srv.Enabled != 1 {
			continue
		}

		// Connect to the client with a timeout for robustness
		cliCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		cli, err := m.connectClient(cliCtx, srv)
		cancel()

		if err != nil {
			log.Printf("MCP Manager: Failed to connect to server %q: %v. Skipping.", srv.Name, err)
			continue
		}

		m.clients[srv.Name] = cli

		// Retrieve tools from this server
		toolsCtx, toolsCancel := context.WithTimeout(ctx, 5*time.Second)
		srvTools, err := mcpp.GetTools(toolsCtx, &mcpp.Config{Cli: cli})
		toolsCancel()

		if err != nil {
			log.Printf("MCP Manager: Failed to get tools from server %q: %v. Skipping.", srv.Name, err)
			continue
		}

		for _, t := range srvTools {
			if invokable, ok := t.(tool.InvokableTool); ok {
				allTools = append(allTools, tools.WrapRedacted(invokable))
			} else {
				allTools = append(allTools, t)
			}
		}
	}

	m.tools = allTools
	m.loaded = true
	return m.tools, nil
}

func (m *manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.closeAndReset()
}

func (m *manager) Reload() {
	m.mu.Lock()
	defer m.mu.Unlock()
	_ = m.closeAndReset()
}

func (m *manager) closeAndReset() error {
	var firstErr error
	for name, cli := range m.clients {
		if err := cli.Close(); err != nil {
			log.Printf("MCP Manager: Error closing client %q: %v", name, err)
			if firstErr == nil {
				firstErr = err
			}
		}
		delete(m.clients, name)
	}

	m.loaded = false
	m.tools = nil
	return firstErr
}
