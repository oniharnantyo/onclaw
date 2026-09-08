package mcp

import (
	"context"
	"errors"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// fakeSettingsRuntime models the MCPSettingsService slice the settings-backed
// policy and status writer consume: pre-decrypted rows come back from the
// ForRuntime accessors, status writes are recorded.
type fakeSettingsRuntime struct {
	workspace map[string][]domain.WorkspaceMCPServer
	agents    map[string][]domain.AgentMCPServer

	workspaceErr error
	agentErr     error

	statusCalls []fakeStatusCall
	statusErr   error
}

type fakeStatusCall struct {
	agentScoped bool
	scopeID     string
	serverID    string
	status      string
	statusErr   string
	toolCount   int
}

func newFakeSettingsRuntime() *fakeSettingsRuntime {
	return &fakeSettingsRuntime{
		workspace: make(map[string][]domain.WorkspaceMCPServer),
		agents:    make(map[string][]domain.AgentMCPServer),
	}
}

func (f *fakeSettingsRuntime) WorkspaceServersForRuntime(_ context.Context, workspaceID string) ([]domain.WorkspaceMCPServer, error) {
	if f.workspaceErr != nil {
		return nil, f.workspaceErr
	}
	return f.workspace[workspaceID], nil
}

func (f *fakeSettingsRuntime) AgentServersForRuntimeByID(_ context.Context, agentID string) ([]domain.AgentMCPServer, error) {
	if f.agentErr != nil {
		return nil, f.agentErr
	}
	return f.agents[agentID], nil
}

func (f *fakeSettingsRuntime) SetWorkspaceServerStatus(_ context.Context, workspaceID, id, status, statusError string, toolCount int) error {
	if f.statusErr != nil {
		return f.statusErr
	}
	f.statusCalls = append(f.statusCalls, fakeStatusCall{scopeID: workspaceID, serverID: id, status: status, statusErr: statusError, toolCount: toolCount})
	return nil
}

func (f *fakeSettingsRuntime) SetAgentServerStatusByID(_ context.Context, agentID, id, status, statusError string, toolCount int) error {
	if f.statusErr != nil {
		return f.statusErr
	}
	f.statusCalls = append(f.statusCalls, fakeStatusCall{agentScoped: true, scopeID: agentID, serverID: id, status: status, statusErr: statusError, toolCount: toolCount})
	return nil
}

// Scenario: Opt-in exposes the whole server / No opt-in exposes nothing /
// Paused server wins over opt-in — the policy filters enabled ∧ opt-in.
func TestSettingsPolicy_WorkspaceOptInAndMasterSwitch(t *testing.T) {
	src := newFakeSettingsRuntime()
	src.workspace["ws1"] = []domain.WorkspaceMCPServer{
		{ID: "srv-on", WorkspaceID: "ws1", Name: "OptedIn", Enabled: true},
		{ID: "srv-paused", WorkspaceID: "ws1", Name: "OptedInPaused", Enabled: false},
		{ID: "srv-notopted", WorkspaceID: "ws1", Name: "NotOptedIn", Enabled: true},
	}
	policy := NewSettingsPolicy(src)

	rows, err := policy.WorkspaceServers(context.Background(), "ws1", []string{"srv-on", "srv-paused", "srv-unknown"})
	if err != nil {
		t.Fatalf("WorkspaceServers: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != "srv-on" {
		t.Fatalf("expected only the enabled opted-in server, got %+v", rows)
	}
}

// Scenario: the policy is the only authority for private rows — enabled rows
// pass, paused rows never reach the connection layer.
func TestSettingsPolicy_AgentEnabledFilter(t *testing.T) {
	src := newFakeSettingsRuntime()
	src.agents["agent1"] = []domain.AgentMCPServer{
		{ID: "priv-on", WorkspaceID: "ws1", AgentID: "agent1", Name: "On", Enabled: true},
		{ID: "priv-paused", WorkspaceID: "ws1", AgentID: "agent1", Name: "Paused", Enabled: false},
	}
	policy := NewSettingsPolicy(src)

	rows, err := policy.AgentServers(context.Background(), "agent1")
	if err != nil {
		t.Fatalf("AgentServers: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != "priv-on" {
		t.Fatalf("expected only the enabled private row, got %+v", rows)
	}

	// An agent with no private rows yields an empty list, not an error.
	empty, err := policy.AgentServers(context.Background(), "agent-none")
	if err != nil || len(empty) != 0 {
		t.Fatalf("expected empty result for an agent without private servers, got %+v (%v)", empty, err)
	}
}

// Policy/store failures propagate — the runner turns them into resolution
// failures, never silent no-tools.
func TestSettingsPolicy_ErrorPropagation(t *testing.T) {
	src := newFakeSettingsRuntime()
	src.workspaceErr = errors.New("db down")
	policy := NewSettingsPolicy(src)

	if _, err := policy.WorkspaceServers(context.Background(), "ws1", nil); err == nil {
		t.Fatal("expected the workspace error to propagate")
	}

	src2 := newFakeSettingsRuntime()
	src2.agentErr = errors.New("db down")
	policy2 := NewSettingsPolicy(src2)
	if _, err := policy2.AgentServers(context.Background(), "agent1"); err == nil {
		t.Fatal("expected the agent error to propagate")
	}
}

// The status writer forwards to the guarded persistence unchanged.
func TestSettingsStatusWriter_ForwardsStatusWrites(t *testing.T) {
	src := newFakeSettingsRuntime()
	writer := NewSettingsStatusWriter(src)
	ctx := context.Background()

	if err := writer.SetWorkspaceStatus(ctx, "ws1", "srv1", domain.MCPStatusConnected, "", 3); err != nil {
		t.Fatalf("SetWorkspaceStatus: %v", err)
	}
	if err := writer.SetAgentStatus(ctx, "agent1", "priv1", domain.MCPStatusError, "dial failed", 0); err != nil {
		t.Fatalf("SetAgentStatus: %v", err)
	}

	if len(src.statusCalls) != 2 {
		t.Fatalf("expected 2 status writes, got %+v", src.statusCalls)
	}
	wsCall, agentCall := src.statusCalls[0], src.statusCalls[1]
	if wsCall.agentScoped || wsCall.scopeID != "ws1" || wsCall.serverID != "srv1" || wsCall.status != domain.MCPStatusConnected || wsCall.toolCount != 3 {
		t.Errorf("unexpected workspace status call: %+v", wsCall)
	}
	if !agentCall.agentScoped || agentCall.scopeID != "agent1" || agentCall.serverID != "priv1" || agentCall.status != domain.MCPStatusError || agentCall.statusErr != "dial failed" {
		t.Errorf("unexpected agent status call: %+v", agentCall)
	}

	// A failing sink surfaces its error; the runner logs it best-effort.
	src.statusErr = errors.New("write failed")
	if err := writer.SetWorkspaceStatus(ctx, "ws1", "srv1", domain.MCPStatusConnected, "", 1); err == nil {
		t.Fatal("expected the sink error to propagate")
	}
}

// Compile-time interface checks: the adapters must satisfy the runner ports.
var (
	_ MCPPolicy    = (*SettingsPolicy)(nil)
	_ StatusWriter = (*SettingsStatusWriter)(nil)
)
