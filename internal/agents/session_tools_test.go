package agents

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/tool"
	agenthooks "github.com/oniharnantyo/onclaw/internal/agents/hooks"
	"github.com/oniharnantyo/onclaw/internal/domain"
)

// ----- fakes -----

// fakeWorkSessions records CloseWorkSession calls and returns a synthesized
// closed session, mirroring the chokepoint's terminal transition.
type fakeWorkSessions struct {
	mu    sync.Mutex
	calls []struct {
		workspaceID string
		channelID   string
		agentID     string
		summary     string
	}
	session *domain.WorkSession
	err     error
}

func (f *fakeWorkSessions) CloseWorkSession(_ context.Context, workspaceID, channelID, agentID, summary string) (*domain.WorkSession, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, struct {
		workspaceID string
		channelID   string
		agentID     string
		summary     string
	}{workspaceID, channelID, agentID, summary})
	if f.session != nil {
		return f.session, nil
	}
	closedAt := time.Date(2026, 9, 9, 15, 4, 5, 0, time.UTC)
	return &domain.WorkSession{
		ID:       "wsess-1",
		Goal:     "Ship it",
		Status:   domain.WorkSessionClosed,
		Summary:  summary,
		ClosedAt: &closedAt,
	}, nil
}

// fakeProjectSpace records Ensure calls and returns a fixed directory.
type fakeProjectSpace struct {
	mu      sync.Mutex
	ensured []struct {
		workspaceID string
		channelSlug string
	}
	dir string
	err error
}

func (f *fakeProjectSpace) Ensure(workspaceID, channelSlug string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ensured = append(f.ensured, struct {
		workspaceID string
		channelSlug string
	}{workspaceID, channelSlug})
	if f.dir != "" {
		return f.dir, nil
	}
	return "/tmp/onclaw/workspaces/acme/projects/" + channelSlug, nil
}

// facilitatorContext builds a ToolContext whose bindings satisfy the
// session.close exposure conditions.
func facilitatorContext() (ToolContext, *fakeWorkSessions) {
	sessions := &fakeWorkSessions{}
	tctx := ToolContext{
		WorkspaceID:   "ws-1",
		AgentID:       "ag-1",
		ChannelID:     "ch-1",
		WorkSessionID: "wsess-1",
		ChannelRole:   domain.ChannelMemberRoleFacilitator,
		WorkSessions:  sessions,
	}
	return tctx, sessions
}

// ----- session.close tool (channel-teams task 4) -----

func TestSessionCloseTool(t *testing.T) {
	tctx, sessions := facilitatorContext()
	tools := resolvedChannelTools(t, tctx, []string{"memory", SessionToolClose})
	closeTool := toolByName(t, tools, SessionToolClose)

	out, err := closeTool.InvokableRun(context.Background(), `{"summary":"retry queue shipped; specs live under /project"}`)
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}
	var result struct {
		SessionID string `json:"session_id"`
		Status    string `json:"status"`
		ClosedAt  string `json:"closed_at"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("result %q is not the compact JSON confirmation: %v", out, err)
	}
	if result.SessionID != "wsess-1" || result.Status != string(domain.WorkSessionClosed) || result.ClosedAt == "" {
		t.Fatalf("close result = %+v", result)
	}
	sessions.mu.Lock()
	calls := sessions.calls
	sessions.mu.Unlock()
	if len(calls) != 1 ||
		calls[0].workspaceID != "ws-1" || calls[0].channelID != "ch-1" ||
		calls[0].agentID != "ag-1" || calls[0].summary != "retry queue shipped; specs live under /project" {
		t.Fatalf("CloseWorkSession calls = %+v", calls)
	}

	// A blank summary is rejected; the session stays open.
	if _, err := closeTool.InvokableRun(context.Background(), `{"summary":"   "}`); err == nil {
		t.Fatal("blank summary must be rejected")
	}
	sessions.mu.Lock()
	got := len(sessions.calls)
	sessions.mu.Unlock()
	if got != 1 {
		t.Fatalf("rejected call must not close the session, calls = %d", got)
	}

	// A session-store failure surfaces as a tool error.
	sessions.err = domain.ErrConflict
	if _, err := closeTool.InvokableRun(context.Background(), `{"summary":"x"}`); err == nil {
		t.Fatal("a failing close must surface as a tool error")
	}
}

func TestSessionCloseTool_ConstructionFailsOutsideExposure(t *testing.T) {
	// Non-channel run.
	if _, err := newSessionCloseTool(ToolContext{WorkspaceID: "ws-1"}); err == nil {
		t.Fatal("session.close must not construct outside a channel run")
	}
	// Channel run, no active session.
	if _, err := newSessionCloseTool(ToolContext{WorkspaceID: "ws-1", ChannelID: "ch-1"}); err == nil {
		t.Fatal("session.close must not construct without an active work session")
	}
	// Session channel run, but the agent is not the facilitator.
	if _, err := newSessionCloseTool(ToolContext{
		WorkspaceID: "ws-1", ChannelID: "ch-1", WorkSessionID: "wsess-1",
		WorkSessions: &fakeWorkSessions{},
	}); err == nil {
		t.Fatal("session.close must not construct for a non-facilitator")
	}
	// Facilitator with an unwired sessions port.
	if _, err := newSessionCloseTool(ToolContext{
		WorkspaceID: "ws-1", ChannelID: "ch-1", WorkSessionID: "wsess-1",
		ChannelRole: domain.ChannelMemberRoleFacilitator,
	}); err == nil {
		t.Fatal("session.close must not construct without the work sessions port")
	}
}

func TestSessionToolScoping(t *testing.T) {
	// Exposed runs append session.close after the caller's allowlist.
	exposed := scopeSessionToolsIn([]string{"memory"}, true)
	if len(exposed) != 2 || exposed[0] != "memory" || exposed[1] != SessionToolClose {
		t.Fatalf("exposed toolset = %v", exposed)
	}
	// Everyone else keeps their allowlist untouched.
	untouched := scopeSessionToolsIn([]string{"memory"}, false)
	if len(untouched) != 1 || untouched[0] != "memory" {
		t.Fatalf("unexposed toolset = %v", untouched)
	}
	// Non-exposed runs strip it even when allowlisted.
	stripped := withoutSessionTools([]string{"memory", SessionToolClose})
	if len(stripped) != 1 || stripped[0] != "memory" {
		t.Fatalf("stripped toolset = %v", stripped)
	}
	// The registry registers it under the dotted name.
	if names := NewDefaultToolRegistry(nil).Names(); !containsString(names, SessionToolClose) {
		t.Fatalf("registry must register session.close, got %v", names)
	}
}

func TestSessionCloseCatalogEntry(t *testing.T) {
	entry, ok := ToolCatalogEntryByKey(SessionToolClose)
	if !ok {
		t.Fatalf("catalog entry missing for %s", SessionToolClose)
	}
	if entry.DisplayName != "Close Work Session" || entry.Group != "channel" {
		t.Fatalf("catalog entry for session.close = %+v", entry)
	}
	if !entry.AlwaysOn {
		t.Fatalf("session.close must be AlwaysOn")
	}
}

// Hooks target tools by exact dotted name (hooks D8): the session.close list
// entry selects session.close and nothing else.
func TestSessionCloseHookMatcherTargetsToolName(t *testing.T) {
	m, err := agenthooks.CompileMatcher(SessionToolClose)
	if err != nil {
		t.Fatalf("compile matcher: %v", err)
	}
	if !m.Matches(SessionToolClose) {
		t.Fatal("matcher must select session.close")
	}
	if m.Matches(ChannelToolPost) || m.Matches(ChannelToolHistory) {
		t.Fatal("matcher must not select the channel tools")
	}
}

// ----- resolve-time wiring (channel-teams task 4/5) -----

// seedSessionFixtures adds (or re-roles) the agent member in the roster so
// facilitator-only paths can be exercised over the channel fixtures.
func seedSessionFixtures(fcc *fakeChannelContext, agentID string, role domain.ChannelMemberRole) {
	for i := range fcc.members {
		m := &fcc.members[i]
		if m.MemberType == domain.ChannelMemberTypeAgent && m.AgentID == agentID {
			m.Role = role
			return
		}
	}
	fcc.members = append(fcc.members, domain.ChannelMember{
		WorkspaceID: fcc.channel.WorkspaceID,
		ChannelID:   fcc.channel.ID,
		MemberType:  domain.ChannelMemberTypeAgent,
		AgentID:     agentID,
		Role:        role,
	})
}

func TestRunner_SessionRunRequiresSessionPorts(t *testing.T) {
	mdl := &hooksModel{final: "done"}
	fcc, _, opts := channelRunContext()
	_, runner, ws, ag, req := setupHooksRunnerWithOpts(t, mdl, opts)

	sessionReq := req
	sessionReq.Origin = OriginChannel
	sessionReq.ChannelID = "ch-1"
	sessionReq.WorkSessionID = "wsess-1"
	sessionReq.SessionID = "chan_ch-1_" + ag.ID
	seedChannelFixtures(fcc, ws.ID, sessionReq.UserID)
	seedSessionFixtures(fcc, ag.ID, domain.ChannelMemberRoleMember)

	if _, err := runner.Run(context.Background(), sessionReq); err == nil || !strings.Contains(err.Error(), "WithWorkSessions") {
		t.Fatalf("session run without session ports must fail at resolve time, got %v", err)
	}
}

// TestRunner_SessionCloseExposure drives resolve() end to end: the
// facilitator's session run resolves session.close and mounts the project
// space; plain channel, non-facilitator, and direct runs do not.
func TestRunner_SessionCloseExposure(t *testing.T) {
	mdl := &hooksModel{final: "done"}
	fcc, _, opts := channelRunContext()
	projectDir := t.TempDir()
	sessions := &fakeWorkSessions{}
	opts = append(opts,
		WithWorkSessions(sessions),
		WithProjectSpace(&fakeProjectSpace{dir: projectDir}),
	)
	_, runner, ws, ag, req := setupHooksRunnerWithOpts(t, mdl, opts)

	sessionReq := req
	sessionReq.Origin = OriginChannel
	sessionReq.ChannelID = "ch-1"
	sessionReq.WorkSessionID = "wsess-1"
	sessionReq.SessionID = "chan_ch-1_" + ag.ID
	seedChannelFixtures(fcc, ws.ID, sessionReq.UserID)
	seedSessionFixtures(fcc, ag.ID, domain.ChannelMemberRoleFacilitator)
	fcc.active = &domain.WorkSession{Goal: "Ship the retry queue", Status: domain.WorkSessionOpen, Budget: 12, HopsUsed: 0}

	cfg, resolved, err := runner.resolve(context.Background(), sessionReq, ws, ag, nil)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if cfg.Filesystem == nil || cfg.Filesystem.ProjectMountDir != projectDir {
		t.Fatalf("facilitator session run must mount the project dir, cfg = %+v", cfg.Filesystem)
	}
	if !resolvedHasTool(t, resolved, SessionToolClose) {
		t.Fatalf("facilitator session run must resolve session.close, got %v", resolvedToolNames(t, resolved))
	}

	// The same run without a session (plain channel run): no session.close,
	// and the channel tools still ride along.
	plainReq := sessionReq
	plainReq.WorkSessionID = ""
	plainReq.SessionID = "chan_ch-1_plain"
	_, resolved, err = runner.resolve(context.Background(), plainReq, ws, ag, nil)
	if err != nil {
		t.Fatalf("resolve plain: %v", err)
	}
	if resolvedHasTool(t, resolved, SessionToolClose) {
		t.Fatalf("plain channel run must not resolve session.close, got %v", resolvedToolNames(t, resolved))
	}
	if !resolvedHasTool(t, resolved, ChannelToolPost) {
		t.Fatalf("channel run must keep the channel toolset, got %v", resolvedToolNames(t, resolved))
	}

	// A non-facilitator session run: session.close stripped even though the
	// agent's denylist doesn't name it.
	memberAgent := *ag
	seedSessionFixtures(fcc, ag.ID, domain.ChannelMemberRoleMember)
	memberReq := sessionReq
	memberReq.SessionID = "chan_ch-1_member"
	_, resolved, err = runner.resolve(context.Background(), memberReq, ws, &memberAgent, nil)
	if err != nil {
		t.Fatalf("resolve member: %v", err)
	}
	if resolvedHasTool(t, resolved, SessionToolClose) {
		t.Fatalf("non-facilitator session run must not resolve session.close, got %v", resolvedToolNames(t, resolved))
	}

	// session.close outside any session run is stripped too.
	directAgent := *ag
	_, resolved, err = runner.resolve(context.Background(), req, ws, &directAgent, nil)
	if err != nil {
		t.Fatalf("resolve direct: %v", err)
	}
	if resolvedHasTool(t, resolved, SessionToolClose) {
		t.Fatalf("direct run must not resolve session.close, got %v", resolvedToolNames(t, resolved))
	}
}

// resolvedToolNames extracts every resolved tool's name.
func resolvedToolNames(t *testing.T, tools []tool.BaseTool) []string {
	t.Helper()
	names := make([]string, 0, len(tools))
	for _, tl := range tools {
		info, err := tl.Info(context.Background())
		if err != nil {
			t.Fatalf("Info: %v", err)
		}
		if info != nil {
			names = append(names, info.Name)
		}
	}
	return names
}

// resolvedHasTool reports whether the resolved set contains the named tool.
func resolvedHasTool(t *testing.T, tools []tool.BaseTool, name string) bool {
	t.Helper()
	for _, n := range resolvedToolNames(t, tools) {
		if n == name {
			return true
		}
	}
	return false
}
