package agents

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/agents/mcp"
	"github.com/oniharnantyo/buddies/internal/sessions"
	"github.com/oniharnantyo/buddies/internal/workspaces"
)

// The computer-use approval gate tests (add-computer-use tasks 5.1, design
// D7, spec chat-approvals "Computer-use destructive actions require
// approval"): a gated tool parks the turn with a persisted approval_required
// and no execution; approving resumes the exact call; denying feeds the
// fixed refusal and the turn continues; the ungated surface — read-only
// tools, stop, and a `type` targeting a token no observation marked secure —
// never parks.

// computerUseSource is the computer-use system server's test double: the
// registry entry the merge point iterates (opt-in, as the adapter's spec
// registers it), the tool table Tools serves, and the meta-carrying Invoke
// the session-dir wrap routes every call through — answering per raw tool
// name and recording each invocation's name, arguments, and meta, so tests
// can assert exactly what executed.
type computerUseSource struct {
	mu    sync.Mutex
	tools map[string][]tool.BaseTool
	outs  map[string]string // raw tool name → canned result

	names []string
	args  []string
	metas []map[string]any
}

func (s *computerUseSource) Tools(_ context.Context, ref mcp.Ref) ([]tool.BaseTool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tools[ref.ServerID], nil
}

func (s *computerUseSource) Invoke(_ context.Context, _ mcp.Ref, name, argsJSON string, meta map[string]any) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.names = append(s.names, name)
	s.args = append(s.args, argsJSON)
	s.metas = append(s.metas, meta)
	return s.outs[name], nil
}

func (s *computerUseSource) SystemServers() []mcp.SystemServerEntry {
	return []mcp.SystemServerEntry{{
		Ref:   mcp.Ref{ServerID: computerUseServerID, Name: computerUseServerID},
		OptIn: true, // add-computer-use design D2: opt-in registration
	}}
}

func (s *computerUseSource) invoked() (names, args []string, metas []map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.names...), append([]string(nil), s.args...), append([]map[string]any(nil), s.metas...)
}

// newComputerUseSource builds the double with the fixture surface's tools
// and per-tool canned results.
func newComputerUseSource(outs map[string]string) *computerUseSource {
	return &computerUseSource{
		tools: map[string][]tool.BaseTool{
			computerUseServerID: {
				fakeMCPTool{raw: "observe_window"},
				fakeMCPTool{raw: "kill_app"},
				fakeMCPTool{raw: "type"},
				fakeMCPTool{raw: "set_value"},
				fakeMCPTool{raw: "write_clipboard"},
				fakeMCPTool{raw: "click"},
				fakeMCPTool{raw: "stop"},
			},
		},
		outs: outs,
	}
}

// computerUseFixture wires the computer-use runner fixture: an agent rooted
// in a workspace whose server list names the opt-in computer-use server, so
// the merge point resolves the full pipeline — session-dir meta wrap,
// naming, attention (a no-op here), approval gate.
func computerUseFixture(t *testing.T, reply any, src *computerUseSource) *fixture {
	t.Helper()
	f := newFixture(t, reply, WithWorkspaces(workspaces.NewManager(t.TempDir())), WithMCPManager(src))
	agent := f.agent
	agent.Slug = "tester"
	agent.MCPServers = []string{computerUseServerID}
	require.NoError(t, f.store.Agents().Update(t.Context(), &agent))
	f.agent = agent
	return f
}

// observeOut is the canned observation result: one secure-annotated element
// (C2) and one plain element.
const observeOut = "window 'Editor'\n- text field E12 [secure]\n- button F31\n"

func TestApprovalGate_KillAppParksOnPersistedApproval(t *testing.T) {
	src := newComputerUseSource(map[string]string{"kill_app": `{"status":"killed"}`})
	calls := 0
	f := computerUseFixture(t, func(input []*schema.Message) fakeReply {
		calls++
		if calls == 1 {
			return toolCallsReply("call-1", "mcp__computer-use__kill_app", `{"app":"Safari"}`)
		}
		sawResult := false
		for _, msg := range input {
			if msg.Role == schema.Tool && msg.ToolCallID == "call-1" {
				sawResult = true
			}
		}
		if !sawResult {
			return fakeReply{err: errors.New("the tool result never reached the model")}
		}
		return fakeReply{text: "Safari is closed", promptTokens: 10, completionTokens: 2}
	}, src)
	sess := f.newSession(t)

	turn, err := f.mgr.Send(t.Context(), sess.ID, "close Safari")
	require.NoError(t, err)

	// The turn parks on the approval card: the persisted approval_required
	// names the requested action and its target.
	events := f.awaitEvent(t, sess.ID, sessions.KindApprovalRequired)
	require.Len(t, events, 2)
	require.Equal(t, string(sessions.KindUserMessage), string(events[0].Kind))
	pending := events[1]
	require.NotNil(t, pending.Approval)
	require.NotEmpty(t, pending.Approval.InterruptID)
	require.Equal(t, "kill_app Safari", pending.Approval.Command,
		"the card payload names the action and the target app")
	require.NotEmpty(t, pending.Approval.CheckpointID)
	require.Equal(t, turn.TurnID(), pending.TurnID)

	// Nothing executed: no invocation reached the adapter.
	names, _, _ := src.invoked()
	require.Empty(t, names, "the gated call must not execute while pending")

	// The run's checkpoint is on disk and the session stays busy.
	_, ok, err := f.store.Checkpoints().Get(t.Context(), pending.Approval.CheckpointID)
	require.NoError(t, err)
	require.True(t, ok, "the interrupted run's checkpoint is persisted")
	_, err = f.mgr.Send(t.Context(), sess.ID, "while parked")
	require.ErrorIs(t, err, ErrTurnInProgress)

	// The pending state derives from a fresh replay of the log.
	derived := pendingApprovalFromEvents(mustEvents(t, f, sess.ID))
	require.NotNil(t, derived)
	require.Equal(t, pending.Approval.InterruptID, derived.interruptID)

	// Denying to leave the turn behind: covered by its own test.
	require.NoError(t, f.mgr.ResolveApproval(t.Context(), sess.ID, pending.Approval.InterruptID, false))
	require.NoError(t, turn.Wait())
}

func TestApprovalGate_ApproveResumesTheExactCall(t *testing.T) {
	root := t.TempDir()
	src := newComputerUseSource(map[string]string{"kill_app": `{"status":"killed"}`})
	calls := 0
	f := newFixture(t, func(input []*schema.Message) fakeReply {
		calls++
		if calls == 1 {
			return toolCallsReply("call-1", "mcp__computer-use__kill_app", `{"app":"Safari"}`)
		}
		sawResult := false
		for _, msg := range input {
			if msg.Role == schema.Tool && msg.ToolCallID == "call-1" {
				sawResult = true
			}
		}
		if !sawResult {
			return fakeReply{err: errors.New("the tool result never reached the model")}
		}
		return fakeReply{text: "Safari is closed", promptTokens: 10, completionTokens: 2}
	}, WithWorkspaces(workspaces.NewManager(root)), WithMCPManager(src))
	f.agent.Slug = "tester"
	f.agent.MCPServers = []string{computerUseServerID}
	require.NoError(t, f.store.Agents().Update(t.Context(), &f.agent))
	sess := f.newSession(t)

	turn, err := f.mgr.Send(t.Context(), sess.ID, "close Safari")
	require.NoError(t, err)
	events := f.awaitEvent(t, sess.ID, sessions.KindApprovalRequired)
	pending := events[1]

	require.NoError(t, f.mgr.ResolveApproval(t.Context(), sess.ID, pending.Approval.InterruptID, true))
	require.NoError(t, turn.Wait())

	events = mustEvents(t, f, sess.ID)
	require.Equal(t, []string{
		string(sessions.KindUserMessage),
		string(sessions.KindApprovalRequired),
		string(sessions.KindApprovalResolved),
		string(sessions.KindToolActivity),
		string(sessions.KindAssistantMessage),
		string(sessions.KindTurnCompleted),
	}, kindsOf(events))
	require.Equal(t, sessions.ApprovalApproved, events[2].Approval.Outcome)

	// The approval resumed the EXACT call: one invocation, same raw tool,
	// same arguments, the adapter's result reported to the model.
	names, args, metas := src.invoked()
	require.Equal(t, []string{"kill_app"}, names)
	require.Equal(t, []string{`{"app":"Safari"}`}, args)
	toolRec := events[3].Tool
	require.NotNil(t, toolRec)
	require.Equal(t, "mcp__computer-use__kill_app", toolRec.Name)
	require.Equal(t, `{"app":"Safari"}`, toolRec.Arguments)
	require.Equal(t, `{"status":"killed"}`, toolRec.Result)
	require.Equal(t, sessions.ToolStatusCompleted, toolRec.Status)
	require.Equal(t, "Safari is closed", textOf(events, sessions.KindAssistantMessage))

	// The call rode the C1 meta: the session's computer directory, absolute
	// and workspace-relative.
	require.Len(t, metas, 1)
	require.Equal(t, filepath.Join(root, "tester", "sessions", sess.ID, workspaces.DirComputer), metas[0][MetaScreenshotDir])
	require.Equal(t, "sessions/"+sess.ID+"/computer", metas[0][MetaScreenshotRel])
}

func TestApprovalGate_DenyFeedsTheFixedRefusalAndTheTurnContinues(t *testing.T) {
	src := newComputerUseSource(map[string]string{"kill_app": `{"status":"killed"}`})
	f := computerUseFixture(t, func(input []*schema.Message) fakeReply {
		var denial string
		for _, msg := range input {
			if msg.Role == schema.Tool && msg.ToolCallID == "call-1" {
				denial = msg.Content
			}
		}
		if denial == "" {
			return toolCallsReply("call-1", "mcp__computer-use__kill_app", `{"app":"Safari"}`)
		}
		return fakeReply{text: "understood, I will leave it open", promptTokens: 10, completionTokens: 2}
	}, src)
	sess := f.newSession(t)

	turn, err := f.mgr.Send(t.Context(), sess.ID, "close Safari")
	require.NoError(t, err)
	events := f.awaitEvent(t, sess.ID, sessions.KindApprovalRequired)
	pending := events[1]

	require.NoError(t, f.mgr.ResolveApproval(t.Context(), sess.ID, pending.Approval.InterruptID, false))
	require.NoError(t, turn.Wait())

	events = mustEvents(t, f, sess.ID)
	require.Equal(t, []string{
		string(sessions.KindUserMessage),
		string(sessions.KindApprovalRequired),
		string(sessions.KindApprovalResolved),
		string(sessions.KindToolActivity),
		string(sessions.KindAssistantMessage),
		string(sessions.KindTurnCompleted),
	}, kindsOf(events))
	require.Equal(t, sessions.ApprovalDenied, events[2].Approval.Outcome)

	// The fixed refusal is the tool result the model read, and the desktop
	// was never touched.
	toolRec := events[3].Tool
	require.NotNil(t, toolRec)
	require.Contains(t, toolRec.Result, "denied approval")
	require.Contains(t, toolRec.Result, "kill_app Safari")
	names, _, _ := src.invoked()
	require.Empty(t, names, "a denied action never executes")

	// The model's final answer proves it read the refusal.
	require.Equal(t, "understood, I will leave it open", textOf(events, sessions.KindAssistantMessage))
}

func TestApprovalGate_UngatedToolsNeverPark(t *testing.T) {
	src := newComputerUseSource(map[string]string{
		"observe_window": observeOut,
		"click":          `{"status":"clicked"}`,
	})
	calls := 0
	f := computerUseFixture(t, func(input []*schema.Message) fakeReply {
		calls++
		if calls == 1 {
			return toolCallsReply("call-obs", "mcp__computer-use__observe_window", `{}`)
		}
		if calls == 2 {
			return toolCallsReply("call-click", "mcp__computer-use__click", `{"x":10,"y":20}`)
		}
		sawClick := false
		for _, msg := range input {
			if msg.Role == schema.Tool && msg.ToolCallID == "call-click" {
				sawClick = true
			}
		}
		if !sawClick {
			return fakeReply{err: errors.New("the click result never reached the model")}
		}
		return fakeReply{text: "observed and clicked", promptTokens: 10, completionTokens: 2}
	}, src)
	sess := f.newSession(t)

	turn, err := f.mgr.Send(t.Context(), sess.ID, "look and click")
	require.NoError(t, err)
	require.NoError(t, turn.Wait(), "read-only and ungated act tools run without parking")

	events := mustEvents(t, f, sess.ID)
	require.Empty(t, eventsOfKind(events, sessions.KindApprovalRequired), "no approval ever parks")
	require.Empty(t, eventsOfKind(events, sessions.KindApprovalResolved))
	require.Equal(t, []string{
		string(sessions.KindUserMessage),
		string(sessions.KindToolActivity),
		string(sessions.KindToolActivity),
		string(sessions.KindAssistantMessage),
		string(sessions.KindTurnCompleted),
	}, kindsOf(events))

	names, _, _ := src.invoked()
	require.Equal(t, []string{"observe_window", "click"}, names)
}

func TestApprovalGate_TypeOnNonSecureTokenDoesNotPark(t *testing.T) {
	src := newComputerUseSource(map[string]string{
		"observe_window": observeOut, // E12 is secure; F31 is not
		"type":           `{"status":"typed"}`,
	})
	f := computerUseFixture(t, func(input []*schema.Message) fakeReply {
		observed, typed := false, false
		for _, msg := range input {
			if msg.Role != schema.Tool {
				continue
			}
			if msg.ToolCallID == "call-obs" {
				observed = true
			}
			if msg.ToolCallID == "call-type" {
				typed = true
			}
		}
		switch {
		case !observed:
			return toolCallsReply("call-obs", "mcp__computer-use__observe_window", `{}`)
		case !typed:
			return toolCallsReply("call-type", "mcp__computer-use__type", `{"element":"F31","text":"hello"}`)
		default:
			return fakeReply{text: "typed the plain field", promptTokens: 10, completionTokens: 2}
		}
	}, src)
	sess := f.newSession(t)

	turn, err := f.mgr.Send(t.Context(), sess.ID, "fill the plain field")
	require.NoError(t, err)
	require.NoError(t, turn.Wait(), "typing into a non-secure element runs without parking")

	events := mustEvents(t, f, sess.ID)
	require.Empty(t, eventsOfKind(events, sessions.KindApprovalRequired))

	names, args, _ := src.invoked()
	require.Equal(t, []string{"observe_window", "type"}, names)
	require.Equal(t, []string{`{}`, `{"element":"F31","text":"hello"}`}, args)
}

func TestApprovalGate_TypeOnSecureTokenParks(t *testing.T) {
	src := newComputerUseSource(map[string]string{
		"observe_window": observeOut, // E12 [secure]
		"type":           `{"status":"typed"}`,
	})
	f := computerUseFixture(t, func(input []*schema.Message) fakeReply {
		observed, typed := false, false
		for _, msg := range input {
			if msg.Role != schema.Tool {
				continue
			}
			if msg.ToolCallID == "call-obs" {
				observed = true
			}
			if msg.ToolCallID == "call-type" {
				typed = true
			}
		}
		switch {
		case !observed:
			return toolCallsReply("call-obs", "mcp__computer-use__observe_window", `{}`)
		case !typed:
			return toolCallsReply("call-type", "mcp__computer-use__type", `{"element":"E12","text":"hunter2"}`)
		default:
			return fakeReply{text: "filled", promptTokens: 10, completionTokens: 2}
		}
	}, src)
	sess := f.newSession(t)

	turn, err := f.mgr.Send(t.Context(), sess.ID, "fill the password field")
	require.NoError(t, err)
	events := f.awaitEvent(t, sess.ID, sessions.KindApprovalRequired)
	// The observe call's terminal record precedes the park: the approval
	// event is the log's last.
	pending := events[len(events)-1]
	require.NotNil(t, pending.Approval)
	require.Equal(t, "type E12", pending.Approval.Command,
		"credential typing parks with the field's token as the target")

	// The typed text itself never rides the payload; the call never executed.
	names, _, _ := src.invoked()
	require.Equal(t, []string{"observe_window"}, names)

	// Deny ends the turn with the refusal.
	require.NoError(t, f.mgr.ResolveApproval(t.Context(), sess.ID, pending.Approval.InterruptID, false))
	require.NoError(t, turn.Wait())
	events = mustEvents(t, f, sess.ID)
	toolActivities := eventsOfKind(events, sessions.KindToolActivity)
	require.Len(t, toolActivities, 2, "the observe record and the denial record")
	toolRec := toolActivities[1].Tool
	require.NotNil(t, toolRec)
	require.Contains(t, toolRec.Result, "denied approval")
	require.Contains(t, toolRec.Result, "type E12")
}

// The gate's unit level: payload synthesis, the secure-token store's bounds,
// the C2 annotation parse, and the merge-point wiring.

func TestComputerUseApprovalPayload(t *testing.T) {
	// Pre-stock a session with one secure token for the conditional cases.
	const sid = "e7238190a1b2c3d4e5f60718293a4b5c"
	sessionSecureTokens.record(sid, "E12")

	t.Cleanup(func() {
		// Drop the stocked session so later tests start clean.
		sessionSecureTokens.mu.Lock()
		delete(sessionSecureTokens.sessions, sid)
		sessionSecureTokens.mu.Unlock()
	})

	cases := []struct {
		name    string
		tool    string
		args    string
		session string
		payload string
		gated   bool
	}{
		{name: "kill_app names the app", tool: "mcp__computer-use__kill_app", args: `{"app":"Safari"}`, session: sid, payload: "kill_app Safari", gated: true},
		{name: "kill_app with an app name containing spaces", tool: "mcp__computer-use__kill_app", args: `{"app":"Google Chrome"}`, session: sid, payload: "kill_app Google Chrome", gated: true},
		{name: "kill_app without a parseable target still gates", tool: "mcp__computer-use__kill_app", args: `{"oops":1}`, session: sid, payload: "kill_app", gated: true},
		{name: "kill_app with malformed args still gates", tool: "mcp__computer-use__kill_app", args: `not json`, session: sid, payload: "kill_app", gated: true},
		{name: "collision-suffixed kill_app still gates", tool: "mcp__computer-use__kill_app_2", args: `{"app":"Safari"}`, session: sid, payload: "kill_app Safari", gated: true},
		{name: "write_clipboard names the clipboard", tool: "mcp__computer-use__write_clipboard", args: `{"text":"irrelevant"}`, session: sid, payload: "write_clipboard clipboard", gated: true},
		{name: "type on a secure token gates", tool: "mcp__computer-use__type", args: `{"element":"E12","text":"hunter2"}`, session: sid, payload: "type E12", gated: true},
		{name: "type on an unknown token does not gate", tool: "mcp__computer-use__type", args: `{"element":"F31","text":"hello"}`, session: sid, gated: false},
		{name: "type without a parseable target does not gate", tool: "mcp__computer-use__type", args: `{}`, session: sid, gated: false},
		{name: "set_value on a secure token gates", tool: "mcp__computer-use__set_value", args: `{"element":"E12","value":"x"}`, session: sid, payload: "set_value E12", gated: true},
		{name: "another session's secure set does not leak", tool: "mcp__computer-use__type", args: `{"element":"E12"}`, session: "e7238190a1b2c3d4e5f60718293a4b5d", gated: false},
		{name: "click never gates", tool: "mcp__computer-use__click", args: `{"x":1,"y":2}`, session: sid, gated: false},
		{name: "stop never gates", tool: "mcp__computer-use__stop", args: `{}`, session: sid, gated: false},
		{name: "observe never gates", tool: "mcp__computer-use__observe_window", args: `{}`, session: sid, gated: false},
		{name: "another server's tool never gates", tool: "mcp__browser-use__kill_app", args: `{"app":"Safari"}`, session: sid, gated: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload, gated := computerUseApproval(tc.tool, tc.args, tc.session)
			require.Equal(t, tc.gated, gated)
			require.Equal(t, tc.payload, payload)
		})
	}
}

func TestSecureAnnotatedTokens(t *testing.T) {
	require.Equal(t, []string{"E12", "F31"},
		secureAnnotatedTokens("- text field E12 [secure]\n- list F31 [secure] and more\n"))
	require.Nil(t, secureAnnotatedTokens("- button F31\nnothing marked"))
	require.Nil(t, secureAnnotatedTokens(""))
}

func TestSecureTokenStoreBounds(t *testing.T) {
	store := newSecureTokenStore()

	// The per-session FIFO cap evicts the oldest token.
	const sid = "e7238190a1b2c3d4e5f60718293a4b5c"
	for i := 0; i < maxSecureTokensPerSession+1; i++ {
		store.record(sid, fmt.Sprintf("T%03d", i))
	}
	require.True(t, store.contains(sid, "T256"))
	require.False(t, store.contains(sid, "T000"), "the oldest token was evicted at the cap")

	// The session LRU cap evicts the least-recently-used session.
	for i := 0; i < maxSecureTokenSessions+1; i++ {
		store.record(fmt.Sprintf("sess-%02d", i), "X")
	}
	require.False(t, store.contains("sess-00", "X"), "the oldest session was evicted at the cap")
	require.True(t, store.contains("sess-01", "X"))
}

func TestWrapApprovalToolsCategories(t *testing.T) {
	m, _ := newWrapManager(t, "scout")
	record := func(*sessions.Event, bool) error { return nil }
	scope := attentionScope{sessionID: "e7238190a1b2c3d4e5f60718293a4b5c", record: record}

	observe := &stubRawTool{name: "observe_window"}
	kill := &stubRawTool{name: "kill_app"}
	typing := &stubRawTool{name: "type"}
	click := &stubRawTool{name: "click"}
	stop := &stubRawTool{name: "stop"}

	wrapped := m.wrapApprovalTools([]tool.BaseTool{observe, kill, typing, click, stop}, scope)
	require.Len(t, wrapped, 5)

	// Observation and gated tools are wrapped; the rest pass through by
	// identity.
	for _, wt := range wrapped[:3] {
		_, isWrap := wt.(*approvalTool)
		require.True(t, isWrap, "observe and gated tools are wrapped, got %T", wt)
	}
	require.Same(t, click, wrapped[3], "an ungated act tool passes through untouched")
	require.Same(t, stop, wrapped[4], "stop — the kill switch — passes through untouched")

	// A scope without a record (never production) leaves the surface bare.
	require.Equal(t, []tool.BaseTool{observe, kill, typing, click, stop},
		m.wrapApprovalTools([]tool.BaseTool{observe, kill, typing, click, stop}, attentionScope{sessionID: scope.sessionID}))
}

func TestMergePointWiresTheApprovalGateForComputerUseOnly(t *testing.T) {
	src := newSystemMCPSource()
	src.entries = append(src.entries, mcp.SystemServerEntry{
		Ref:   mcp.Ref{ServerID: computerUseServerID, Name: computerUseServerID},
		OptIn: true,
	})
	src.tools[mcp.SystemServerID] = []tool.BaseTool{fakeMCPTool{raw: "navigate"}}
	src.tools[computerUseServerID] = []tool.BaseTool{fakeMCPTool{raw: "kill_app"}}

	f := newFixture(t, func(_ []*schema.Message) fakeReply {
		return fakeReply{text: "ok", promptTokens: 1, completionTokens: 1}
	}, WithMCPManager(src))
	// A non-empty list restricts the agent to the named servers: both the
	// browser-use system server and the opt-in computer-use one are named.
	f.agent.MCPServers = []string{mcp.SystemServerID, computerUseServerID}

	scope := attentionScope{
		sessionID: "e7238190a1b2c3d4e5f60718293a4b5c",
		record:    func(*sessions.Event, bool) error { return nil },
	}
	tools, err := f.mgr.resolveMCPTools(t.Context(), f.agent, scope)
	require.NoError(t, err)
	names := mcpToolNames(t, tools)
	require.Contains(t, names, "mcp__computer-use__kill_app")
	require.Contains(t, names, "mcp__browser-use__navigate")

	byName := map[string]tool.BaseTool{}
	for i, name := range names {
		byName[name] = tools[i]
	}
	_, gated := byName["mcp__computer-use__kill_app"].(*approvalTool)
	require.True(t, gated, "the computer-use gated tool rides the approval wrap")
	_, gated = byName["mcp__browser-use__navigate"].(*approvalTool)
	require.False(t, gated, "the browser-use surface carries no approval wrap")
}

// compile-time: the double satisfies the seams the merge point and the wraps
// read.
var (
	_ mcp.ToolSource         = (*computerUseSource)(nil)
	_ mcp.SystemServerSource = (*computerUseSource)(nil)
	_ metaInvoker            = (*computerUseSource)(nil)
)
