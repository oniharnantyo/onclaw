package agents

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"slices"

	"github.com/cloudwego/eino/components/tool"

	"github.com/oniharnantyo/buddies/internal/agents/mcp"
	"github.com/oniharnantyo/buddies/internal/store"
)

// The MCP seam (spec agent-mcp): an agent sees an enabled MCP server's tools
// when its server list is empty — every enabled server, the default — or
// names that server; a non-empty list restricts the agent to the named
// servers, the same allowlist reading as agent.Tools and agent.Skills
// (design D3). The server list — not the built-in tool allowlist and not the
// Tools-pane global toggles — is the whole gate for MCP tools (design D3/D7).

// noopMCPServer is the default MCP tool source: no connections, so
// resolution contributes zero MCP tools. It exists so the manager holds no
// nil dependencies at the point of use (composition-root wiring replaces it
// through WithMCPManager).
type noopMCPServer struct{}

func (noopMCPServer) Tools(context.Context, mcp.Ref) ([]tool.BaseTool, error) {
	return nil, nil
}

// MCPServerRef adapts a stored server definition into the mcp package's Ref.
// Shared by the turn resolution and the settings service's probe so both
// dial from exactly the same view of a row. An OAuth-mode row (design D4)
// hydrates the connection's OAuth settings from the stored registration,
// with tokens flowing through the NewMCPTokenStore adapter; a none-mode row
// keeps the zero value — the static-header dial.
func MCPServerRef(server store.MCPServer, tokens store.MCPOAuthTokenStore) mcp.Ref {
	ref := mcp.Ref{
		ServerID: server.ID,
		Name:     server.Name,
		Conn: mcp.Connection{
			Transport: server.Transport,
			Command:   server.Command,
			Args:      slices.Clone(server.Args),
			Env:       maps.Clone(server.Env),
			URL:       server.URL,
			Headers:   maps.Clone(server.Headers),

			// The integration credential rides the auth JSON (design D4) and
			// is injected into the request only at dial time (design D3) —
			// never into Headers, so it never reaches the frontend.
			IntegrationCredential: server.Auth.IntegrationCredential,
			CredentialHeaderStyle: server.Auth.CredentialHeaderStyle,
		},
	}
	if server.Auth.IsOAuth() {
		ref.Conn.OAuth = mcp.OAuth{
			Enabled:               true,
			ClientID:              server.Auth.ClientID,
			ClientSecret:          server.Auth.ClientSecret,
			RedirectURI:           server.Auth.RedirectURI,
			Scopes:                slices.Clone(server.Auth.Scopes),
			AuthServerMetadataURL: server.Auth.AuthServerMetadataURL,
			Tokens:                NewMCPTokenStore(server.ID, tokens),
		}
	}
	return ref
}

// resolveMCPTools appends the agent's MCP servers' tools after the built-ins
// (design D7): every enabled server when the agent's list is empty, otherwise
// the enabled servers it names, in store list order — the pass-shared
// mcp.Namer makes collision suffixes deterministic given that stable order. A
// per-server connection failure contributes zero tools and never fails the
// turn (design D7: skip and let the probe surface the failure); a store-level
// read failure is infrastructure failure and fails the resolution like any
// other store error.
func (m *manager) resolveMCPTools(ctx context.Context, agent store.Agent) ([]tool.BaseTool, error) {
	servers, err := m.store.MCPServers().List(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolve mcp servers: %w", err)
	}

	unrestricted := len(agent.MCPServers) == 0
	var tools []tool.BaseTool
	namer := mcp.NewNamer()
	for _, server := range servers {
		if !server.Enabled {
			continue
		}
		if !unrestricted && !slices.Contains(agent.MCPServers, server.ID) {
			continue
		}
		serverTools, err := m.mcpManager.Tools(ctx, MCPServerRef(server, m.store.MCPOAuthTokens()))
		if err != nil {
			slog.WarnContext(ctx, "mcp server unavailable; skipping its tools",
				"server", server.Name, "error", err)
			continue
		}
		tools = append(tools, mcp.ApplyNames(server.Name, serverTools, namer)...)
	}
	return tools, nil
}
