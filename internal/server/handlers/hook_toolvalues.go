package handlers

import (
	"context"
	"log/slog"
	"sort"

	"github.com/oniharnantyo/onclaw/internal/agents"
	agenthooks "github.com/oniharnantyo/onclaw/internal/agents/hooks"
	"github.com/oniharnantyo/onclaw/internal/agents/mcp"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// workspaceToolValueSource implements agenthooks.ToolValueSource over the real
// value set a workspace's hooks match against (design.md D8): the built-in
// tool surface (capability registry names — including the individual
// browser.* tools — plus the catalog's fs middleware tools and the reserved
// shell name, which have no registry constructors), and the workspace-level
// MCP servers' tools enumerated through the shared connection manager the
// same way the runner names them (mcp__<server>__<tool>).
type workspaceToolValueSource struct {
	registry   agents.ToolRegistry
	mcpServers store.WorkspaceMCPServers
	manager    mcp.ToolSource
}

// NewWorkspaceHookToolValueSource builds the adapter. registry is the
// built-in tool surface (a fresh default registry — its Names are identical
// to the runner's; only the names are read, no tool is constructed); manager
// is the shared MCP connection cache.
func NewWorkspaceHookToolValueSource(registry agents.ToolRegistry, mcpServers store.WorkspaceMCPServers, manager mcp.ToolSource) agenthooks.ToolValueSource {
	return &workspaceToolValueSource{registry: registry, mcpServers: mcpServers, manager: manager}
}

// VisibleToolNames enumerates the tool names currently visible to the
// workspace. It degrades gracefully: an MCP server that fails to connect
// contributes zero tools (the match count is advisory; a dead server must not
// block a save), and an empty workspace MCP registry simply adds nothing.
func (s *workspaceToolValueSource) VisibleToolNames(ctx context.Context, workspaceID string) ([]string, error) {
	seen := make(map[string]struct{})
	names := make([]string, 0)

	add := func(name string) {
		if name == "" || name == agents.BrowserToolAlias {
			return // the alias expands at runtime; only real names match
		}
		if _, dup := seen[name]; dup {
			return
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}

	for _, name := range s.registry.Names() {
		add(name)
	}
	for _, entry := range agents.ToolCatalog() {
		add(entry.Key)
	}
	s.addWorkspaceMCPNames(ctx, workspaceID, add)

	sort.Strings(names)
	return names, nil
}

// addWorkspaceMCPNames appends the enabled workspace-level servers' tool
// names, named exactly as the runner names them for invocation. Per-server
// failures are logged and skipped (graceful degradation, design.md D8).
func (s *workspaceToolValueSource) addWorkspaceMCPNames(ctx context.Context, workspaceID string, add func(string)) {
	rows, err := s.mcpServers.List(ctx, workspaceID)
	if err != nil {
		slog.WarnContext(ctx, "hook match count: workspace mcp servers unavailable; counting registry tools only",
			"workspace_id", workspaceID, "error", err)
		return
	}
	for _, row := range rows {
		if !row.Enabled {
			continue // paused servers contribute no runtime tools (policy gate)
		}
		ref := mcp.Ref{WorkspaceID: workspaceID, ServerID: row.ID, Name: row.Name, Conn: row.MCPConnection}
		serverTools, err := s.manager.Tools(ctx, ref)
		if err != nil {
			slog.WarnContext(ctx, "hook match count: mcp server unavailable; skipping its tools",
				"workspace_id", workspaceID, "server", row.Name, "error", err)
			continue
		}
		named := mcp.ApplyNames(row.Name, serverTools, mcp.NewNamer())
		for _, t := range named {
			info, err := t.Info(ctx)
			if err != nil || info == nil {
				continue
			}
			add(info.Name)
		}
	}
}
