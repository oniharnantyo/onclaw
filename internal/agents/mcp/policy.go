package mcp

import (
	"context"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// MCPPolicy resolves which MCP servers an agent's execution may draw tools
// from (design.md D6). It is the MCP counterpart of the runner's ToolPolicy
// gate: MCP tools are deliberately independent of the `tools` allowlist, the
// per-turn allowed-tools overrides, and the workspace tool gate — the opt-in
// allowlist, the master switches, and private attachment are the whole
// contract.
//
// The settings-service-backed implementation is wired by a later wave
// (tasks 3.2/3.3 own MCPSettingsService; the composition root then constructs
// the policy over it and injects it through the runner's WithMCPPolicy
// option). Tests use the in-package fakes (policy_test.go) or their own
// mocks.
type MCPPolicy interface {
	// WorkspaceServers returns the workspace-registered servers the agent
	// opted into: rows whose Enabled master switch is on AND whose id is in
	// optInIDs (agent.EnabledMCPS). The master switch filters HERE, in the
	// policy, so a paused server never reaches the connection layer.
	WorkspaceServers(ctx context.Context, workspaceID string, optInIDs []string) ([]domain.WorkspaceMCPServer, error)
	// AgentServers returns the agent's private server rows, enabled-gated the
	// same way as workspace rows: private servers follow the workspace
	// servers' rules (workspace-mcp delta), so a paused private row is
	// excluded here and never reaches the connection layer.
	AgentServers(ctx context.Context, agentID string) ([]domain.AgentMCPServer, error)
}

// StatusWriter persists MCP connection outcomes onto server rows (design.md
// D8): a runtime connection failure flips the stored status to error so the
// pane surfaces it; a success records connected with the tool count. Writes
// are best-effort at every call site — a failing status write never fails a
// run. The composition root wires an adapter over the two store ports'
// SetStatus methods (a later wave owns that wiring).
type StatusWriter interface {
	// SetWorkspaceStatus records the outcome for a workspace-registered
	// server (store.WorkspaceMCPServers.SetStatus).
	SetWorkspaceStatus(ctx context.Context, workspaceID, serverID, status, statusError string, toolCount int) error
	// SetAgentStatus records the outcome for an agent-private server
	// (store.AgentMCPServers.SetStatus).
	SetAgentStatus(ctx context.Context, agentID, serverID, status, statusError string, toolCount int) error
}
