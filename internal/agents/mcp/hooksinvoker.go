package mcp

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/components/tool"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// WorkspaceServerRuntimeSource resolves one workspace-registered MCP server
// with its secret rows decrypted for a connection attempt. It is the narrow
// shape of the agents.MCPSettingsService runtime accessor, declared here so
// this package does not import the agents package (the composition root wires
// the settings service straight into NewHooksInvoker).
type WorkspaceServerRuntimeSource interface {
	WorkspaceServerForRuntime(ctx context.Context, workspaceID, serverID string) (*domain.WorkspaceMCPServer, error)
}

// HooksInvoker invokes one tool on a WORKSPACE-level MCP server on behalf of
// the agent hooks pipeline (design.md D11). It structurally satisfies the
// hooks package's narrow MCPInvoker port without importing it: an
// agent-private server id — or any id not present in the workspace registry —
// fails the workspace-scoped lookup below and is therefore unreachable by
// contract, never by convention.
type HooksInvoker struct {
	servers WorkspaceServerRuntimeSource
	manager ToolSource
}

// NewHooksInvoker builds the hooks MCP invoker from its two dependencies: the
// workspace server registry (decrypted reads) and the connection cache.
func NewHooksInvoker(servers WorkspaceServerRuntimeSource, manager ToolSource) *HooksInvoker {
	return &HooksInvoker{servers: servers, manager: manager}
}

// InvokeWorkspaceTool resolves serverID against the WORKSPACE registry only,
// connects through the MCPManager (cached after the first, potentially slow,
// cold connect), finds the server's raw tool by name, and invokes it with the
// given JSON arguments, returning the tool's text result.
func (inv *HooksInvoker) InvokeWorkspaceTool(ctx context.Context, workspaceID, serverID, toolName string, argsJSON []byte) (string, error) {
	server, err := inv.servers.WorkspaceServerForRuntime(ctx, workspaceID, serverID)
	if err != nil {
		// Unknown id, another workspace's server, or an agent-private id:
		// all domain.ErrNotFound — the hook sees one failure shape.
		return "", fmt.Errorf("resolve workspace mcp server: %w", err)
	}

	tools, err := inv.manager.Tools(ctx, Ref{
		WorkspaceID: workspaceID,
		ServerID:    server.ID,
		Name:        server.Name,
		Conn:        server.MCPConnection,
	})
	if err != nil {
		return "", fmt.Errorf("connect to mcp server %q: %w", server.Name, err)
	}

	for _, t := range tools {
		info, infoErr := t.Info(ctx)
		if infoErr != nil || info == nil || info.Name != toolName {
			continue
		}
		invokable, ok := t.(tool.InvokableTool)
		if !ok {
			return "", fmt.Errorf("tool %q on mcp server %q is not invokable", toolName, server.Name)
		}
		result, err := invokable.InvokableRun(ctx, string(argsJSON))
		if err != nil {
			return "", fmt.Errorf("invoke tool %q: %w", toolName, err)
		}
		return result, nil
	}
	return "", fmt.Errorf("tool %q not found on mcp server %q", toolName, server.Name)
}
