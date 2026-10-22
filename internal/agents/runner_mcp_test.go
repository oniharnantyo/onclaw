package agents

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/agents/mcp"
	"github.com/oniharnantyo/buddies/internal/sessions"
	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/oniharnantyo/buddies/internal/store/fake"
)

// The MCP turn tests (tasks.md 2.3, spec agent-mcp): the server allowlist
// policy merges the agent's MCP servers' tools — every enabled server by
// default, only the named ones when the list is non-empty — into the turn's
// surface through the compose seam, with a FAKE connection (a stub
// mcp.ToolSource) — default offer, restriction, paused server, dead server,
// and a connection that dies mid-turn.

// fakeMCPTool is a minimal invokable tool standing in for an eino-ext mcp
// tool: its Info carries the server-assigned raw name (the naming pass
// renames it), and invocation fails when callErr is set — the "connection
// died between offer and call" shape.
type fakeMCPTool struct {
	raw     string
	callErr error
}

func (t fakeMCPTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: t.raw}, nil
}

func (t fakeMCPTool) InvokableRun(context.Context, string, ...tool.Option) (string, error) {
	if t.callErr != nil {
		return "", t.callErr
	}
	return `{"ok":true}`, nil
}

// stubMCPSource implements mcp.ToolSource: it records the refs it is asked
// for and answers from a per-server tool table or a per-server error table.
type stubMCPSource struct {
	mu    sync.Mutex
	refs  []mcp.Ref
	tools map[string][]tool.BaseTool // key: serverID
	errs  map[string]error           // key: serverID
}

func (s *stubMCPSource) Tools(_ context.Context, ref mcp.Ref) ([]tool.BaseTool, error) {
	s.mu.Lock()
	s.refs = append(s.refs, ref)
	s.mu.Unlock()
	if err := s.errs[ref.ServerID]; err != nil {
		return nil, err
	}
	return s.tools[ref.ServerID], nil
}

func (s *stubMCPSource) seenServerIDs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.refs))
	for _, ref := range s.refs {
		out = append(out, ref.ServerID)
	}
	return out
}

// seedMCPServer stores one MCP server row and, when listed is set, names it
// in the fixture agent's server list — which turns the agent from the
// every-server default into one restricted to the listed servers.
func (f *fixture) seedMCPServer(t *testing.T, name string, enabled bool, listed bool) store.MCPServer {
	t.Helper()
	server := &store.MCPServer{
		Name:      name,
		Transport: mcp.TransportStdio,
		Command:   "/bin/true",
		Enabled:   enabled,
	}
	require.NoError(t, f.store.MCPServers().Create(t.Context(), server))
	if listed {
		f.agent.MCPServers = append(f.agent.MCPServers, server.ID)
		require.NoError(t, f.store.Agents().Update(t.Context(), &f.agent))
	}
	return *server
}

// hasMCPPrefix reports whether any offered tool name carries the MCP
// namespace.
func hasMCPPrefix(names []string) bool {
	for _, name := range names {
		if strings.HasPrefix(name, "mcp__") {
			return true
		}
	}
	return false
}

// TestTurn_MCPOfferedWhenListed is the core acceptance path through an
// explicit server list: the listed enabled server's tool is offered to the
// model even though the agent's tool allowlist is narrowed to another tool
// entirely — the server list, not the tool allowlist, gates MCP (design D3).
// The model calls the renamed tool, the stub connection serves the result,
// and the transcript records it.
func TestTurn_MCPOfferedWhenListed(t *testing.T) {
	calls := 0
	f := newFixture(t, func(input []*schema.Message) fakeReply {
		calls++
		if calls == 1 {
			return toolCallsReply("call-1", "mcp__github__create_issue", `{}`)
		}
		// The follow-up is only reachable if the MCP tool result is in
		// context.
		sawToolResult := false
		for _, msg := range input {
			if msg.Role == schema.Tool && strings.Contains(msg.Content, `"ok"`) {
				sawToolResult = true
			}
		}
		if !sawToolResult {
			return fakeReply{err: errors.New("the mcp tool result never reached the model")}
		}
		return fakeReply{text: "issue created", promptTokens: 30, completionTokens: 5}
	}, WithMCPManager(&stubMCPSource{tools: map[string][]tool.BaseTool{
		// The stub answers by server id; seeded below via the fixture.
	}}))

	// The agent's tool allowlist is narrowed to a different tool entirely:
	// the MCP tool must still be offered (bypass, design D3).
	f.agent.Tools = []string{ToolCurrentTime}
	require.NoError(t, f.store.Agents().Update(t.Context(), &f.agent))
	server := f.seedMCPServer(t, "github", true, true)

	// Point the stub's tool table at the created server id.
	src := f.mgr.mcpManager.(*stubMCPSource)
	src.tools[server.ID] = []tool.BaseTool{fakeMCPTool{raw: "create_issue"}}

	sess := f.newSession(t)
	turn, err := f.mgr.Send(t.Context(), sess.ID, "create an issue")
	require.NoError(t, err)
	require.NoError(t, turn.Wait(), "the mcp round trip must complete")

	// The model was offered the renamed MCP tool (and the allowlisted one).
	offered := f.model.toolsOf(0)
	require.Contains(t, offered, "mcp__github__create_issue")
	require.Contains(t, offered, ToolCurrentTime)

	// Only the listed server reached the connection layer.
	require.Equal(t, []string{server.ID}, src.seenServerIDs())

	// The transcript carries the terminal tool record with the stub result.
	events, err := f.sessions.Events(t.Context(), sess.ID)
	require.NoError(t, err)
	require.Equal(t, []string{
		string(sessions.KindUserMessage),
		string(sessions.KindToolActivity),
		string(sessions.KindAssistantMessage),
		string(sessions.KindTurnCompleted),
	}, kindsOf(events))
	tool := events[1].Tool
	require.NotNil(t, tool)
	require.Equal(t, "call-1", tool.CallID)
	require.Equal(t, "mcp__github__create_issue", tool.Name)
	require.Equal(t, sessions.ToolStatusCompleted, tool.Status)
	require.JSONEq(t, `{"ok":true}`, tool.Result)
	require.False(t, tool.IsError)
}

// TestTurn_MCPOfferedByDefault pins spec scenario "Servers are offered by
// default": an agent whose server list is empty is offered every enabled
// server's tools without naming any — the tools-style allowlist reading
// (design D3), not an opt-in.
func TestTurn_MCPOfferedByDefault(t *testing.T) {
	f := newFixture(t, func(_ []*schema.Message) fakeReply {
		return fakeReply{text: "nothing to do", promptTokens: 10, completionTokens: 3}
	}, WithMCPManager(&stubMCPSource{tools: map[string][]tool.BaseTool{}}))

	require.Empty(t, f.agent.MCPServers, "fixture premise: the agent names no servers")
	server := f.seedMCPServer(t, "github", true, false)
	src := f.mgr.mcpManager.(*stubMCPSource)
	src.tools[server.ID] = []tool.BaseTool{fakeMCPTool{raw: "create_issue"}}

	sess := f.newSession(t)
	turn, err := f.mgr.Send(t.Context(), sess.ID, "hello")
	require.NoError(t, err)
	require.NoError(t, turn.Wait())

	require.Contains(t, f.model.toolsOf(0), "mcp__github__create_issue")
	require.Equal(t, []string{server.ID}, src.seenServerIDs())
}

// TestTurn_MCPRestrictedToListedServers pins spec scenario "A restricted
// agent sees only the named servers": once the list names one enabled server,
// the unnamed enabled server contributes nothing and never reaches the
// connection layer.
func TestTurn_MCPRestrictedToListedServers(t *testing.T) {
	f := newFixture(t, func(_ []*schema.Message) fakeReply {
		return fakeReply{text: "nothing to do", promptTokens: 10, completionTokens: 3}
	}, WithMCPManager(&stubMCPSource{tools: map[string][]tool.BaseTool{}}))

	listed := f.seedMCPServer(t, "linear", true, true)
	unlisted := f.seedMCPServer(t, "github", true, false)
	src := f.mgr.mcpManager.(*stubMCPSource)
	src.tools[listed.ID] = []tool.BaseTool{fakeMCPTool{raw: "list_rows"}}
	src.tools[unlisted.ID] = []tool.BaseTool{fakeMCPTool{raw: "create_issue"}}

	sess := f.newSession(t)
	turn, err := f.mgr.Send(t.Context(), sess.ID, "hello")
	require.NoError(t, err)
	require.NoError(t, turn.Wait())

	offered := f.model.toolsOf(0)
	require.Contains(t, offered, "mcp__linear__list_rows")
	require.NotContains(t, offered, "mcp__github__create_issue")
	require.Equal(t, []string{listed.ID}, src.seenServerIDs(),
		"the unlisted server must never reach the connection layer")

	events, err := f.sessions.Events(t.Context(), sess.ID)
	require.NoError(t, err)
	require.Empty(t, eventsOfKind(events, sessions.KindToolActivity))
}

// TestTurn_MCPPausedServerNotOffered pins the policy's master switch: a
// listed but disabled server is filtered in the policy, before the
// connection layer.
func TestTurn_MCPPausedServerNotOffered(t *testing.T) {
	f := newFixture(t, func(_ []*schema.Message) fakeReply {
		return fakeReply{text: "ok", promptTokens: 10, completionTokens: 3}
	}, WithMCPManager(&stubMCPSource{
		tools: map[string][]tool.BaseTool{"srv-x": {fakeMCPTool{raw: "paused_tool"}}},
	}))

	f.seedMCPServer(t, "github", false, true) // listed, but paused

	sess := f.newSession(t)
	turn, err := f.mgr.Send(t.Context(), sess.ID, "hello")
	require.NoError(t, err)
	require.NoError(t, turn.Wait())

	require.False(t, hasMCPPrefix(f.model.toolsOf(0)))
	require.Empty(t, f.mgr.mcpManager.(*stubMCPSource).seenServerIDs())
}

// TestTurn_MCPDeadServerSkipped pins the resolve degradation (design D7): one
// listed server failing to connect contributes zero tools and never fails
// the turn — the other listed server's tools are still offered.
func TestTurn_MCPDeadServerSkipped(t *testing.T) {
	calls := 0
	f := newFixture(t, func(_ []*schema.Message) fakeReply {
		calls++
		if calls == 1 {
			return toolCallsReply("call-1", "mcp__linear__list_rows", `{}`)
		}
		return fakeReply{text: "done", promptTokens: 15, completionTokens: 3}
	}, WithMCPManager(&stubMCPSource{
		tools: map[string][]tool.BaseTool{},
		errs:  map[string]error{"srv-dead": errors.New("connection refused")},
	}))

	alive := f.seedMCPServer(t, "linear", true, true)
	dead := f.seedMCPServer(t, "github", true, true)
	src := f.mgr.mcpManager.(*stubMCPSource)
	src.tools[alive.ID] = []tool.BaseTool{fakeMCPTool{raw: "list_rows"}}

	sess := f.newSession(t)
	turn, err := f.mgr.Send(t.Context(), sess.ID, "list the rows")
	require.NoError(t, err)
	require.NoError(t, turn.Wait(), "a dead mcp server must not fail the turn")

	// Both servers were consulted (the dead one at connect), but only the
	// alive one's tool was offered.
	require.ElementsMatch(t, []string{alive.ID, dead.ID}, src.seenServerIDs())
	require.Contains(t, f.model.toolsOf(0), "mcp__linear__list_rows")
	require.NotContains(t, f.model.toolsOf(0), "mcp__github__")
}

// TestTurn_MCPConnectionDiesMidTurn pins spec scenario "Server dies mid-turn":
// the connection is alive when the tools are offered but dead when the call
// executes — the tool result reports the failure and the turn continues.
func TestTurn_MCPConnectionDiesMidTurn(t *testing.T) {
	calls := 0
	f := newFixture(t, func(input []*schema.Message) fakeReply {
		calls++
		if calls == 1 {
			return toolCallsReply("call-1", "mcp__github__create_issue", `{}`)
		}
		// The follow-up is only reachable if the failure surfaced as the
		// tool result.
		sawErrorResult := false
		for _, msg := range input {
			if msg.Role == schema.Tool && strings.Contains(msg.Content, "connection closed") {
				sawErrorResult = true
			}
		}
		if !sawErrorResult {
			return fakeReply{err: errors.New("the mcp failure never reached the model")}
		}
		return fakeReply{text: "the server died as reported", promptTokens: 20, completionTokens: 5}
	}, WithMCPManager(&stubMCPSource{tools: map[string][]tool.BaseTool{}}))

	server := f.seedMCPServer(t, "github", true, true)
	src := f.mgr.mcpManager.(*stubMCPSource)
	// Alive at offer time (Tools succeeds), dead at call time.
	src.tools[server.ID] = []tool.BaseTool{
		fakeMCPTool{raw: "create_issue", callErr: errors.New("connection closed")},
	}

	sess := f.newSession(t)
	turn, err := f.mgr.Send(t.Context(), sess.ID, "create an issue")
	require.NoError(t, err)
	require.NoError(t, turn.Wait(), "a mid-turn mcp failure must not fail the turn")

	events, err := f.sessions.Events(t.Context(), sess.ID)
	require.NoError(t, err)
	require.Equal(t, []string{
		string(sessions.KindUserMessage),
		string(sessions.KindToolActivity),
		string(sessions.KindAssistantMessage),
		string(sessions.KindTurnCompleted),
	}, kindsOf(events))
	tool := events[1].Tool
	require.NotNil(t, tool)
	require.Equal(t, sessions.ToolStatusFailed, tool.Status)
	require.True(t, tool.IsError)
	require.Contains(t, tool.Result, "connection closed")
	require.Equal(t, "the server died as reported", textOf(events, sessions.KindAssistantMessage))
}

// TestTurn_MCPDefaultsContributeNothing pins the default wiring: no
// WithMCPManager, resolution yields zero MCP tools with no nil-dereference —
// exactly as before the MCP integration.
func TestTurn_MCPDefaultsContributeNothing(t *testing.T) {
	f := newFixture(t, func(_ []*schema.Message) fakeReply {
		return fakeReply{text: "ok", promptTokens: 10, completionTokens: 3}
	})

	f.seedMCPServer(t, "github", true, true) // listed, but no source wired

	sess := f.newSession(t)
	turn, err := f.mgr.Send(t.Context(), sess.ID, "hello")
	require.NoError(t, err)
	require.NoError(t, turn.Wait())

	require.False(t, hasMCPPrefix(f.model.toolsOf(0)))
}

// TestWithMCPManagerOption pins the wiring contract: the default is the
// no-op source, a non-nil source replaces it, and a nil argument keeps the
// default.
func TestWithMCPManagerOption(t *testing.T) {
	fs := fake.New()
	m := newManager(fs, sessions.NewService(fs), &fakeRegistry{})
	require.IsType(t, noopMCPServer{}, m.mcpManager)

	src := &stubMCPSource{}
	m = newManager(fs, sessions.NewService(fs), &fakeRegistry{}, WithMCPManager(src))
	require.Equal(t, mcp.ToolSource(src), m.mcpManager)

	m = newManager(fs, sessions.NewService(fs), &fakeRegistry{}, WithMCPManager(nil))
	require.IsType(t, noopMCPServer{}, m.mcpManager)
}
