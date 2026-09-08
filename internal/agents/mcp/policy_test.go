package mcp

import (
	"context"
	"slices"
	"strings"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// fakePolicy is the in-package MCPPolicy fake: it models the contract the
// settings-service-backed implementation must honor (workspace-mcp +
// agent-runtime deltas) — workspace rows pass the enabled master switch AND
// the opt-in filter here; agent rows pass the enabled switch here — so tests
// can drive the full filtering semantics without a store.
type fakePolicy struct {
	workspace map[string][]domain.WorkspaceMCPServer // key: workspaceID
	agents    map[string][]domain.AgentMCPServer     // key: agentID

	workspaceErr error
	agentErr     error
}

func newFakePolicy() *fakePolicy {
	return &fakePolicy{
		workspace: make(map[string][]domain.WorkspaceMCPServer),
		agents:    make(map[string][]domain.AgentMCPServer),
	}
}

func (f *fakePolicy) addWorkspace(workspaceID string, servers ...domain.WorkspaceMCPServer) {
	f.workspace[workspaceID] = append(f.workspace[workspaceID], servers...)
}

func (f *fakePolicy) addAgent(agentID string, servers ...domain.AgentMCPServer) {
	f.agents[agentID] = append(f.agents[agentID], servers...)
}

// WorkspaceServers implements MCPPolicy: enabled ∧ id ∈ optInIDs.
func (f *fakePolicy) WorkspaceServers(_ context.Context, workspaceID string, optInIDs []string) ([]domain.WorkspaceMCPServer, error) {
	if f.workspaceErr != nil {
		return nil, f.workspaceErr
	}
	var out []domain.WorkspaceMCPServer
	for _, s := range f.workspace[workspaceID] {
		if !s.Enabled || !slices.Contains(optInIDs, s.ID) {
			continue
		}
		out = append(out, s)
	}
	return out, nil
}

// AgentServers implements MCPPolicy: enabled private rows only.
func (f *fakePolicy) AgentServers(_ context.Context, agentID string) ([]domain.AgentMCPServer, error) {
	if f.agentErr != nil {
		return nil, f.agentErr
	}
	var out []domain.AgentMCPServer
	for _, s := range f.agents[agentID] {
		if !s.Enabled {
			continue
		}
		out = append(out, s)
	}
	return out, nil
}

// recordingStatus is the in-package StatusWriter fake: it records writes and
// can fail them (D8: a failing write must never fail the run).
type recordingStatus struct {
	workspace []statusCall
	agent     []statusCall
	err       error
}

type statusCall struct {
	scopeID   string
	serverID  string
	status    string
	statusErr string
	toolCount int
}

func (r *recordingStatus) SetWorkspaceStatus(_ context.Context, workspaceID, serverID, status, statusErr string, toolCount int) error {
	if r.err != nil {
		return r.err
	}
	r.workspace = append(r.workspace, statusCall{workspaceID, serverID, status, statusErr, toolCount})
	return nil
}

func (r *recordingStatus) SetAgentStatus(_ context.Context, agentID, serverID, status, statusErr string, toolCount int) error {
	if r.err != nil {
		return r.err
	}
	r.agent = append(r.agent, statusCall{agentID, serverID, status, statusErr, toolCount})
	return nil
}

func (r *recordingStatus) lastWorkspace(serverID string) (statusCall, bool) {
	for i := len(r.workspace) - 1; i >= 0; i-- {
		if strings.EqualFold(r.workspace[i].serverID, serverID) {
			return r.workspace[i], true
		}
	}
	return statusCall{}, false
}

func (r *recordingStatus) lastAgent(serverID string) (statusCall, bool) {
	for i := len(r.agent) - 1; i >= 0; i-- {
		if strings.EqualFold(r.agent[i].serverID, serverID) {
			return r.agent[i], true
		}
	}
	return statusCall{}, false
}
