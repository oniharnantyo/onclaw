package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/agents/hooks"
	"github.com/oniharnantyo/buddies/internal/agents/subagents"
	"github.com/oniharnantyo/buddies/internal/memory"
	"github.com/oniharnantyo/buddies/internal/sessions"
	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/oniharnantyo/buddies/internal/store/fake"
	"github.com/oniharnantyo/buddies/internal/workspaces"
)

// The memory_search tool tests (add-agent-memory tasks.md 5.2): the result
// shape matches the spec, another agent's notes stay out of scope, every call
// records a recall trace, and the trace origin follows the turn — user, a
// scheduled run, or a delegated child.

// memoryToolFixture wires the memory_search tool over one workspace's memory
// directory and the fake store's memory rows.
type memoryToolFixture struct {
	st        *fake.Store
	workspace string
}

func newMemoryToolFixture(t *testing.T) *memoryToolFixture {
	t.Helper()
	st := fake.New()
	workspace := t.TempDir()
	require.NoError(t, memory.EnsureLayout(memory.NewLayout(workspace)))
	return &memoryToolFixture{st: st, workspace: workspace}
}

// seedNote writes one valid fact note into the fixture's memory directory.
func (f *memoryToolFixture) seedNote(t *testing.T, name, description, body string) {
	t.Helper()
	path := memory.NewLayout(f.workspace).NotePath(name)
	raw := fmt.Sprintf("---\nname: %s\ndescription: %s\ntype: reference\n---\n\n%s", name, description, body)
	require.NoError(t, os.WriteFile(path, []byte(raw), 0o600))
}

// newTool builds the tool over the fixture's workspace for agent agentID,
// resolving the turn context from turnCtx.
func (f *memoryToolFixture) newTool(t *testing.T, agentID string, turnCtx memoryTurnContextFunc) tool.BaseTool {
	t.Helper()
	base, err := newMemorySearchTool(f.workspace, agentID, f.st.Memory(), turnCtx)
	require.NoError(t, err)
	return base
}

// runMemorySearchTool invokes the tool once and decodes the result JSON.
func runMemorySearchTool(t *testing.T, base tool.BaseTool, args string) MemorySearchOutput {
	t.Helper()
	invokable, ok := base.(tool.InvokableTool)
	require.True(t, ok, "memory_search tool %T does not implement InvokableTool", base)
	out, err := invokable.InvokableRun(t.Context(), args)
	require.NoError(t, err, "read failures must be readable results, not errors: args %s", args)
	var res MemorySearchOutput
	require.NoError(t, json.Unmarshal([]byte(out), &res), "result %q", out)
	return res
}

// stubTurnContext resolves every call to a fixed turn context.
func stubTurnContext(sessionID, turnID, origin string) memoryTurnContextFunc {
	return func(context.Context) (string, string, string) {
		return sessionID, turnID, origin
	}
}

// TestMemorySearchTool_ResultShape pins the spec shape (spec agent-memory
// "Memory search tool"): ranked results, each with the note name, description,
// workspace-relative path, score, and a matched excerpt; the stronger match
// first.
func TestMemorySearchTool_ResultShape(t *testing.T) {
	f := newMemoryToolFixture(t)
	f.seedNote(t, "stack-choice", "The service runs on kubernetes and postgres",
		"We chose kubernetes for orchestration and postgres for storage.")
	f.seedNote(t, "db-notes", "Notes about postgres",
		"postgres backups run nightly.")

	tl := f.newTool(t, "agent-1", stubTurnContext("sess-1", "turn-1", store.MemoryOriginUser))
	res := runMemorySearchTool(t, tl, `{"query":"kubernetes postgres"}`)
	require.True(t, res.OK, "error: %s", res.Error)
	require.NotEmpty(t, res.Results)

	first := res.Results[0]
	require.Equal(t, "stack-choice", first.Name)
	require.Equal(t, "The service runs on kubernetes and postgres", first.Description)
	require.Equal(t, "memory/stack-choice.md", first.Path)
	require.Greater(t, first.Score, 0.0)
	require.NotEmpty(t, first.Excerpt)
	require.Greater(t, first.Score, res.Results[1].Score, "the two-term match outranks the one-term match")
}

// TestMemorySearchTool_ScopedToWorkspace pins the scoping rule: a matching
// note in ANOTHER agent's workspace is absent — the tool searches only the
// session agent's memory directory.
func TestMemorySearchTool_ScopedToWorkspace(t *testing.T) {
	f := newMemoryToolFixture(t)
	other := t.TempDir()
	otherLayout := memory.NewLayout(other)
	require.NoError(t, memory.EnsureLayout(otherLayout))
	require.NoError(t, os.WriteFile(otherLayout.NotePath("foreign"),
		[]byte("---\nname: foreign\ndescription: Another agent's note\ntype: reference\n---\n\nthe zopfli compression note belongs elsewhere.\n"), 0o600))
	f.seedNote(t, "local", "This agent's note", "nothing about the queried terms here.")

	tl := f.newTool(t, "agent-1", stubTurnContext("sess-1", "turn-1", store.MemoryOriginUser))

	// The foreign note's terms come back empty.
	res := runMemorySearchTool(t, tl, `{"query":"zopfli compression"}`)
	require.True(t, res.OK, "error: %s", res.Error)
	require.Empty(t, res.Results)

	// And a query that matches both agents' notes surfaces only the local one.
	f.seedNote(t, "shared-term", "Mentions zopfli too", "zopfli appears here as well.")
	res = runMemorySearchTool(t, tl, `{"query":"zopfli"}`)
	require.True(t, res.OK, "error: %s", res.Error)
	for _, hit := range res.Results {
		require.NotEqual(t, "foreign", hit.Name, "another agent's note must stay out of scope")
	}
}

// TestMemorySearchTool_TracePerCall pins the trace contract (design D6): one
// recall trace row per invocation, carrying the agent, the turn context, the
// query, and the hit paths with scores.
func TestMemorySearchTool_TracePerCall(t *testing.T) {
	f := newMemoryToolFixture(t)
	f.seedNote(t, "greeting", "A greeting note", "the user greets everyone warmly.")

	tl := f.newTool(t, "agent-1", stubTurnContext("sess-7", "turn-9", store.MemoryOriginUser))
	res := runMemorySearchTool(t, tl, `{"query":"greeting"}`)
	require.True(t, res.OK, "error: %s", res.Error)
	res = runMemorySearchTool(t, tl, `{"query":"nothing matches this"}`)
	require.True(t, res.OK, "error: %s", res.Error)

	traces, err := f.st.Memory().ListTracesSince(t.Context(), "agent-1", time.Time{})
	require.NoError(t, err)
	require.Len(t, traces, 2, "one trace row per call")
	first := traces[0]
	require.Equal(t, "agent-1", first.AgentID)
	require.Equal(t, "sess-7", first.SessionID)
	require.Equal(t, "turn-9", first.TurnID)
	require.Equal(t, store.MemoryOriginUser, first.Origin)
	require.Equal(t, "greeting", first.Query)
	require.NotEmpty(t, first.Hits, "the matching call recorded its hits")
	require.Equal(t, "memory/greeting.md", first.Hits[0].Path)
	require.Greater(t, first.Hits[0].Score, 0.0)
	require.False(t, first.CreatedAt.IsZero(), "the store stamps the trace time")
	require.Empty(t, traces[1].Hits, "the miss recorded no hits — still a trace")
}

// TestMemorySearchTool_RejectionsAndFailSoft pins the readable results (spec
// agent-memory: a read failure is a readable error result; a missing memory
// directory is an empty result): an empty query rejects, an unreadable memory
// directory rejects readably, and a missing directory is a normal empty
// result.
func TestMemorySearchTool_RejectionsAndFailSoft(t *testing.T) {
	t.Run("empty query rejects readably", func(t *testing.T) {
		f := newMemoryToolFixture(t)
		tl := f.newTool(t, "agent-1", stubTurnContext("s", "t", store.MemoryOriginUser))
		res := runMemorySearchTool(t, tl, `{"query":""}`)
		require.False(t, res.OK)
		require.Contains(t, res.Error, "provide a query")
	})

	t.Run("unreadable memory directory rejects readably", func(t *testing.T) {
		f := newMemoryToolFixture(t)
		// The memory path itself is a regular file: every read of it fails.
		require.NoError(t, os.RemoveAll(f.workspace))
		require.NoError(t, os.MkdirAll(f.workspace, 0o700))
		require.NoError(t, os.WriteFile(memory.NewLayout(f.workspace).Dir, []byte("not a directory"), 0o600))
		tl := f.newTool(t, "agent-1", stubTurnContext("s", "t", store.MemoryOriginUser))
		res := runMemorySearchTool(t, tl, `{"query":"anything"}`)
		require.False(t, res.OK)
		require.Contains(t, res.Error, "memory could not be read")
	})

	t.Run("missing memory directory is an empty result", func(t *testing.T) {
		st := fake.New()
		base, err := newMemorySearchTool(t.TempDir(), "agent-1", st.Memory(), stubTurnContext("s", "t", store.MemoryOriginUser))
		require.NoError(t, err)
		res := runMemorySearchTool(t, base, `{"query":"anything"}`)
		require.True(t, res.OK)
		require.Empty(t, res.Results)
	})
}

// TestGate_MemorySearchWithhold pins the catalog gate: workspace-scoped like
// every workspace tool — offered to a rootable agent, withheld from a slugless
// one (child turns inherit the parent's offered surface, which the child-turn
// end-to-end test below exercises for real).
func TestGate_MemorySearchWithhold(t *testing.T) {
	ctx := context.Background()

	t.Run("rootable agent is offered the tool", func(t *testing.T) {
		fs := fake.New()
		m := newManager(fs, sessions.NewService(fs), &fakeRegistry{},
			WithWorkspaces(workspaces.NewManager(t.TempDir())), WithBrowserDir(t.TempDir()))
		surface, err := m.resolveTurnTools(ctx, store.Agent{Name: "scout", Slug: "scout"})
		require.NoError(t, err)
		names, err := surface.names(ctx)
		require.NoError(t, err)
		require.Contains(t, names, ToolMemorySearch)
	})

	t.Run("slugless agent is withheld", func(t *testing.T) {
		fs := fake.New()
		m := newManager(fs, sessions.NewService(fs), &fakeRegistry{},
			WithWorkspaces(workspaces.NewManager(t.TempDir())), WithBrowserDir(t.TempDir()))
		surface, err := m.resolveTurnTools(ctx, store.Agent{Name: "legacy"})
		require.NoError(t, err)
		names, err := surface.names(ctx)
		require.NoError(t, err)
		require.NotContains(t, names, ToolMemorySearch)
	})
}

// newMemoryTurnFixture is a workspace fixture for end-to-end origin tests: the
// agent gets a slug (so the gate offers the workspace tools) and delegation
// enabled (so the child-turn test dispatches a real subagent), and its
// workspace directory is materialized; the workspace path comes back for
// seeding.
func newMemoryTurnFixture(t *testing.T, reply any, opts ...Option) (*fixture, string) {
	t.Helper()
	ws := workspaces.NewManager(t.TempDir())
	f := newFixture(t, reply, append([]Option{WithWorkspaces(ws)}, opts...)...)
	agent := f.agent
	agent.Slug = "tester"
	// The store preserves the column verbatim; the services layer stamps the
	// create-time default, so a turn fixture opts the capability in explicitly
	// (the newWorkspaceFixture convention).
	agent.SubagentsEnabled = true
	require.NoError(t, f.store.Agents().Update(t.Context(), &agent))
	f.agent = agent
	dir, err := ws.Create("tester")
	require.NoError(t, err)
	return f, dir
}

// seedMemoryNote writes one fact note into a workspace's memory directory.
func seedMemoryNote(t *testing.T, workspace, name, body string) {
	t.Helper()
	layout := memory.NewLayout(workspace)
	require.NoError(t, memory.EnsureLayout(layout))
	raw := fmt.Sprintf("---\nname: %s\ndescription: A seeded note\ntype: reference\n---\n\n%s", name, body)
	require.NoError(t, os.WriteFile(layout.NotePath(name), []byte(raw), 0o600))
}

// memorySearchThenAnswerReply scripts a turn whose model calls memory_search
// once and then answers.
func memorySearchThenAnswerReply(query string) func([]*schema.Message) fakeReply {
	return func(input []*schema.Message) fakeReply {
		if sawToolMessage(input) {
			return fakeReply{text: "recalled from memory"}
		}
		return toolCallsReply("call-1", ToolMemorySearch, fmt.Sprintf(`{"query":%q}`, query))
	}
}

// tracesOf lists the agent's memory traces.
func tracesOf(t *testing.T, f *fixture) []store.MemoryTrace {
	t.Helper()
	traces, err := f.store.Memory().ListTracesSince(t.Context(), f.agent.ID, time.Time{})
	require.NoError(t, err)
	return traces
}

// TestMemorySearch_UserTurnTraceOrigin pins the origin gate end to end (design
// D12): a plain Send's memory_search records a user-origin trace wired to the
// session and turn that issued it.
func TestMemorySearch_UserTurnTraceOrigin(t *testing.T) {
	f, wsDir := newMemoryTurnFixture(t, memorySearchThenAnswerReply("zopfli"))
	seedMemoryNote(t, wsDir, "compression", "the user prefers zopfli compression for assets.")

	ctx := t.Context()
	sess := f.newSession(t)
	turn, err := f.mgr.Send(ctx, sess.ID, "what compression do i prefer?")
	require.NoError(t, err)
	require.NoError(t, turn.Wait())

	traces := tracesOf(t, f)
	require.Len(t, traces, 1)
	require.Equal(t, store.MemoryOriginUser, traces[0].Origin)
	require.Equal(t, sess.ID, traces[0].SessionID)
	require.Equal(t, turn.TurnID(), traces[0].TurnID)
	require.Equal(t, "zopfli", traces[0].Query)
	require.NotEmpty(t, traces[0].Hits, "the note was among the hits")
}

// TestMemorySearch_SchedulerOriginTrace pins the scheduler leg of the origin
// gate (spec agent-memory "Scheduler-origin trace is ignored by
// consolidation"): a scheduled run's search is stored with origin scheduler.
func TestMemorySearch_SchedulerOriginTrace(t *testing.T) {
	f, wsDir := newMemoryTurnFixture(t, memorySearchThenAnswerReply("zopfli"))
	seedMemoryNote(t, wsDir, "compression", "the user prefers zopfli compression for assets.")

	ctx := t.Context()
	sess := f.newSession(t)
	turn, err := f.mgr.Submit(ctx, SendRequest{
		SessionID: sess.ID,
		Message:   "morning digest",
		Origin:    hooks.OriginScheduler,
	})
	require.NoError(t, err)
	require.NoError(t, turn.Wait())

	traces := tracesOf(t, f)
	require.Len(t, traces, 1)
	require.Equal(t, store.MemoryOriginScheduler, traces[0].Origin)
	require.Equal(t, sess.ID, traces[0].SessionID)
}

// childFollowupCall reports whether the input is the delegated child's
// post-tool-result model call: the child's fresh context opens with the task
// text as its only user message, and a tool result follows the child's own
// call. The parent's calls carry a different user message, so the task text on
// a user message discriminates the child's turns.
func childFollowupCall(input []*schema.Message, task string) bool {
	sawUser, sawTool := false, false
	for _, m := range input {
		switch m.Role {
		case schema.User:
			if !strings.Contains(m.Content, task) {
				return false
			}
			sawUser = true
		case schema.Tool:
			sawTool = true
		}
	}
	return sawUser && sawTool
}

// TestMemorySearch_ChildTurnTraceOrigin pins the subagent leg of the origin
// gate (spec agent-memory: a child turn MAY call memory_search and its traces
// carry origin subagent) — end to end through a real delegation: the parent
// dispatches, the child searches memory and answers, the parent collects.
func TestMemorySearch_ChildTurnTraceOrigin(t *testing.T) {
	task := "look up the compression preference in memory"
	childAnswer := "child: the user prefers zopfli"
	f, wsDir := newMemoryTurnFixture(t, func(input []*schema.Message) fakeReply {
		if _, ok := childCall(input, task); ok {
			return toolCallsReply("child-call-1", ToolMemorySearch, `{"query":"zopfli"}`)
		}
		if childFollowupCall(input, task) {
			return fakeReply{text: childAnswer}
		}
		if !sawToolMessage(input) {
			return toolCallsReply("call-1", ToolAgent, fmt.Sprintf(`{"prompt":%q,"description":"memory lookup"}`, task))
		}
		if strings.Contains(textOfMessages(input, schema.Tool), childAnswer) {
			return fakeReply{text: "Delegated: preference known."}
		}
		time.Sleep(5 * time.Millisecond)
		return toolCallsReply("call-2", ToolTaskOutput, `{"task_id":"task-1"}`)
	})
	seedMemoryNote(t, wsDir, "compression", "the user prefers zopfli compression for assets.")

	ctx := t.Context()
	sess := f.newSession(t)
	turn, err := f.mgr.Send(ctx, sess.ID, "delegate the memory lookup")
	require.NoError(t, err)
	require.NoError(t, turn.Wait())

	// Only the child searched memory: one trace, subagent origin, the
	// dispatching turn's session and coordinates.
	traces := tracesOf(t, f)
	require.Len(t, traces, 1)
	require.Equal(t, store.MemoryOriginSubagent, traces[0].Origin)
	require.Equal(t, sess.ID, traces[0].SessionID)
	require.Equal(t, turn.TurnID(), traces[0].TurnID, "the child runs within the turn that dispatched it")
	require.Equal(t, "zopfli", traces[0].Query)
	require.NotEmpty(t, traces[0].Hits)
}

// TestMemoryTurnContext pins the resolver's unit edges: a context with no
// eino session reads as other; a child marker reads as subagent even when the
// lane dropped the session (the marker carries it, best-effort); and the
// dispatch-origin mapping covers the trace vocabulary.
func TestMemoryTurnContext(t *testing.T) {
	fs := fake.New()
	m := newManager(fs, sessions.NewService(fs), &fakeRegistry{}, WithBrowserDir(t.TempDir()))

	sessionID, turnID, origin := m.memoryTurnContext(context.Background())
	require.Empty(t, sessionID)
	require.Empty(t, turnID)
	require.Equal(t, store.MemoryOriginOther, origin)

	sessionID, _, origin = m.memoryTurnContext(subagents.WithChildTurn(context.Background(), "sess-child"))
	require.Equal(t, "sess-child", sessionID, "the delegation marker carries the session")
	require.Equal(t, store.MemoryOriginSubagent, origin)

	_, _, origin = m.memoryTurnContext(subagents.WithChildTurn(context.Background(), ""))
	require.Equal(t, store.MemoryOriginSubagent, origin, "a child marker reads as subagent even without a session")

	require.Equal(t, store.MemoryOriginUser, memoryTraceOrigin(""))
	require.Equal(t, store.MemoryOriginUser, memoryTraceOrigin(hooks.OriginUser))
	require.Equal(t, store.MemoryOriginScheduler, memoryTraceOrigin(hooks.OriginScheduler))
	require.Equal(t, store.MemoryOriginOther, memoryTraceOrigin("telegram"))
}
