package mcp

import (
	"context"
	"slices"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// SettingsRuntime is the settings-service slice the service-backed MCPPolicy
// and StatusWriter consume (tasks 5.4, design.md D6/D8/D10). *agents.
// MCPSettingsService satisfies it structurally — the runtime accessors decrypt
// secret rows for connection attempts and the Set*Status methods keep the
// service's guarded persistence (status vocabulary + tenancy scope) — so the
// composition root wires them without this package importing the agents
// package (which would cycle: agents → mcp).
type SettingsRuntime interface {
	// WorkspaceServersForRuntime returns the workspace's registered servers
	// with secret row values decrypted.
	WorkspaceServersForRuntime(ctx context.Context, workspaceID string) ([]domain.WorkspaceMCPServer, error)
	// AgentServersForRuntimeByID returns an agent's private servers with
	// secret row values decrypted, addressed by agent id alone.
	AgentServersForRuntimeByID(ctx context.Context, agentID string) ([]domain.AgentMCPServer, error)
	// SetWorkspaceServerStatus persists a probe/runtime connection outcome on
	// a workspace-registered server.
	SetWorkspaceServerStatus(ctx context.Context, workspaceID, id, status, statusError string, toolCount int) error
	// SetAgentServerStatusByID persists a probe/runtime connection outcome on
	// an agent-private server, addressed by agent id alone.
	SetAgentServerStatusByID(ctx context.Context, agentID, id, status, statusError string, toolCount int) error
}

// SettingsPolicy implements MCPPolicy over the settings service's runtime
// accessors. The filtering is the policy's job, here and nowhere else (the
// port contract): workspace rows pass the enabled master switch AND the
// agent's opt-in allowlist; agent-private rows pass the enabled switch —
// paused rows are excluded before they can reach the connection layer.
// Returned rows are already decrypted, so the runner can hand Conn straight
// to the manager.
type SettingsPolicy struct {
	src SettingsRuntime
}

// NewSettingsPolicy builds the MCPPolicy over a settings service.
func NewSettingsPolicy(src SettingsRuntime) *SettingsPolicy {
	return &SettingsPolicy{src: src}
}

// WorkspaceServers implements MCPPolicy: enabled ∧ id ∈ optInIDs.
func (p *SettingsPolicy) WorkspaceServers(ctx context.Context, workspaceID string, optInIDs []string) ([]domain.WorkspaceMCPServer, error) {
	rows, err := p.src.WorkspaceServersForRuntime(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	out := make([]domain.WorkspaceMCPServer, 0, len(rows))
	for _, row := range rows {
		if !row.Enabled || !slices.Contains(optInIDs, row.ID) {
			continue
		}
		out = append(out, row)
	}
	return out, nil
}

// AgentServers implements MCPPolicy: enabled private rows only.
func (p *SettingsPolicy) AgentServers(ctx context.Context, agentID string) ([]domain.AgentMCPServer, error) {
	rows, err := p.src.AgentServersForRuntimeByID(ctx, agentID)
	if err != nil {
		return nil, err
	}
	out := make([]domain.AgentMCPServer, 0, len(rows))
	for _, row := range rows {
		if !row.Enabled {
			continue
		}
		out = append(out, row)
	}
	return out, nil
}

// SettingsStatusWriter implements StatusWriter over the settings service's
// guarded persistence: runtime connection outcomes land on the stored rows
// (design.md D8) through the same status vocabulary and tenancy guards the
// probe path uses.
type SettingsStatusWriter struct {
	src SettingsRuntime
}

// NewSettingsStatusWriter builds the StatusWriter over a settings service.
func NewSettingsStatusWriter(src SettingsRuntime) *SettingsStatusWriter {
	return &SettingsStatusWriter{src: src}
}

// SetWorkspaceStatus implements StatusWriter.
func (w *SettingsStatusWriter) SetWorkspaceStatus(ctx context.Context, workspaceID, serverID, status, statusError string, toolCount int) error {
	return w.src.SetWorkspaceServerStatus(ctx, workspaceID, serverID, status, statusError, toolCount)
}

// SetAgentStatus implements StatusWriter.
func (w *SettingsStatusWriter) SetAgentStatus(ctx context.Context, agentID, serverID, status, statusError string, toolCount int) error {
	return w.src.SetAgentServerStatusByID(ctx, agentID, serverID, status, statusError, toolCount)
}
