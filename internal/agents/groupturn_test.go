package agents

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"

	"github.com/oniharnantyo/buddies/internal/sessions"
	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/oniharnantyo/buddies/internal/workspaces"
	"github.com/stretchr/testify/require"
)

// groupCursorCommit is one recorded cursor commit.
type groupCursorCommit struct {
	GroupID    string
	AgentID    string
	ThroughSeq int64
}

// fakeGroupTurnSource is a GroupTurnSource test double: it hands back canned
// tools and records every cursor commit.
type fakeGroupTurnSource struct {
	tools    []tool.BaseTool
	toolsErr error

	mu      sync.Mutex
	commits []groupCursorCommit
}

func (s *fakeGroupTurnSource) GroupTurnTools(_ context.Context, _, _ string) ([]tool.BaseTool, error) {
	if s.toolsErr != nil {
		return nil, s.toolsErr
	}
	return s.tools, nil
}

func (s *fakeGroupTurnSource) CommitCursor(_ context.Context, groupID, agentID string, throughSeq int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.commits = append(s.commits, groupCursorCommit{GroupID: groupID, AgentID: agentID, ThroughSeq: throughSeq})
	return nil
}

func (s *fakeGroupTurnSource) commitCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.commits)
}

func (s *fakeGroupTurnSource) lastCommit() groupCursorCommit {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.commits[len(s.commits)-1]
}

// newFakeGroupTool builds one eino invokable tool standing in for a
// source-provided group tool.
func newFakeGroupTool(name string) (tool.InvokableTool, error) {
	type input struct{}
	type output struct{}
	return utils.InferTool(name, "test "+name, func(_ context.Context, _ input) (output, error) {
		return output{}, nil
	})
}

// newGroupFixture wires the group turn seam onto the standard fixture: the
// fake source provides the three in-channel tools and the ref is bound.
func newGroupFixture(t *testing.T, reply any) (*fixture, *fakeGroupTurnSource) {
	t.Helper()
	src := &fakeGroupTurnSource{}
	for _, name := range []string{"group_history", "group_assign_task", "group_task_result"} {
		gt, err := newFakeGroupTool(name)
		require.NoError(t, err)
		src.tools = append(src.tools, gt)
	}
	ref := NewGroupTurnRef()
	ref.Set(src)
	f := newFixture(t, reply, WithGroupTurns(ref))
	return f, src
}

// newGroupWorkspaceFixture wires the group turn seam onto a workspace-rooted
// fixture: the manager gets a real temp workspace and the fixture agent the
// slug naming it, so the group turn's ordinary surface carries the
// workspace-rooted filesystem tools (fix-agent-group-member-tools task 3.1).
func newGroupWorkspaceFixture(t *testing.T, reply any) (*fixture, *fakeGroupTurnSource, *workspaces.Manager) {
	t.Helper()
	ws := workspaces.NewManager(t.TempDir())
	src := &fakeGroupTurnSource{}
	for _, name := range []string{"group_history", "group_assign_task", "group_task_result"} {
		gt, err := newFakeGroupTool(name)
		require.NoError(t, err)
		src.tools = append(src.tools, gt)
	}
	ref := NewGroupTurnRef()
	ref.Set(src)
	f := newFixture(t, reply, WithGroupTurns(ref), WithWorkspaces(ws))
	agent := f.agent
	agent.Slug = "tester"
	require.NoError(t, f.store.Agents().Update(t.Context(), &agent))
	f.agent = agent
	return f, src, ws
}

// groupPacket is a filled-in packet the packet tests tweak.
func groupPacket() GroupPacket {
	return GroupPacket{
		GroupID:        "group-1",
		GroupTitle:     "Launch Crew",
		Instructions:   "Report findings concisely.",
		Responsibility: "You scout primary sources.",
		Request:        "Find the docs link",
		Unread: []GroupTranscriptMessage{
			{ID: "m1", Seq: 7, Kind: GroupMessageKindMessage, AuthorKind: GroupAuthorUser, Text: "where are the docs?"},
			{ID: "m2", Seq: 8, Kind: GroupMessageKindMessage, AuthorKind: GroupAuthorAgent, AuthorAgentID: "agent-scout-2", Text: "I checked the wiki — nothing yet."},
		},
		TaskResults: []GroupTaskResult{
			{TaskID: "t-9", Result: "the spec is frozen"},
		},
		Recent: []GroupTranscriptMessage{
			{ID: "m3", Seq: 9, Kind: GroupMessageKindAssigned, AuthorKind: GroupAuthorCoordinator, Payload: `{"assigned":"Scout"}`},
		},
		Summary:        "Earlier the group planned the launch.",
		SummaryVersion: 2,
		ThroughSeq:     41,
	}
}

// lastUserText returns the last user message's content of one model call.
func lastUserText(input []*schema.Message) string {
	for i := len(input) - 1; i >= 0; i-- {
		if input[i].Role == schema.User {
			return input[i].Content
		}
	}
	return ""
}

// TestSubmitGroup_QueuesBehindChatTurn covers the lane's core scenario
// (add-agent-groups spec "Group dispatch waits behind a chat turn"): a group
// dispatch addressed to an agent with an in-flight chat turn queues without
// error and starts — through the normal turn path — only after the chat turn
// reaches its terminal outcome.
func TestSubmitGroup_QueuesBehindChatTurn(t *testing.T) {
	release := make(chan struct{})
	f, src := newGroupFixture(t, func(_ []*schema.Message) fakeReply {
		<-release
		return fakeReply{text: "done", promptTokens: 1, completionTokens: 1}
	})
	sess := f.newSession(t)
	member := f.newSession(t)

	chat, err := f.mgr.Send(t.Context(), sess.ID, "chat one")
	require.NoError(t, err)

	packet := groupPacket()
	packet.Request = "the group request"
	group, err := f.mgr.SubmitGroup(t.Context(), GroupTurnRequest{SessionID: member.ID, Packet: packet})
	require.NoError(t, err, "a group dispatch to a busy agent queues, never fails with ErrTurnInProgress")

	// The dispatch is parked: the chat turn's model call is the only one.
	require.Eventually(t, func() bool { return len(f.model.calls()) == 1 },
		time.Second, time.Millisecond, "the chat turn's model call starts")
	require.Equal(t, "chat one", lastUserText(f.model.calls()[0]))

	close(release)
	require.NoError(t, chat.Wait())
	require.NoError(t, group.Wait())

	// The group turn ran after the chat turn, on the packet.
	calls := f.model.calls()
	require.Len(t, calls, 2)
	require.Equal(t, "the group request", lastUserText(calls[1]), "the group turn starts only after the chat turn's terminal outcome")
	require.Equal(t, 1, src.commitCount(), "the cursor commits after the dispatch")
	require.Equal(t, groupCursorCommit{GroupID: "group-1", AgentID: f.agent.ID, ThroughSeq: 41}, src.lastCommit())

	// The queued dispatch ran through the normal startTurn/finishTurn path:
	// the member session carries the request and the terminal events under
	// the submitted turn id.
	events, err := f.sessions.Events(t.Context(), member.ID)
	require.NoError(t, err)
	require.Equal(t, []string{
		string(sessions.KindUserMessage),
		string(sessions.KindAssistantMessage),
		string(sessions.KindTurnCompleted),
	}, kindsOf(events))
	require.Equal(t, group.TurnID(), events[0].TurnID)
	require.Equal(t, "the group request", events[0].Text)
}

// TestSubmitGroup_TwoDispatchesRunSequentially covers the lane's ordering
// scenario (add-agent-groups spec "Lane drains in arrival order"): two group
// dispatches to one agent start in arrival order and never run concurrently.
func TestSubmitGroup_TwoDispatchesRunSequentially(t *testing.T) {
	var mu sync.Mutex
	inFlight, maxInFlight := 0, 0
	var order []string
	f, src := newGroupFixture(t, func(input []*schema.Message) fakeReply {
		mu.Lock()
		inFlight++
		if inFlight > maxInFlight {
			maxInFlight = inFlight
		}
		order = append(order, lastUserText(input))
		mu.Unlock()
		time.Sleep(20 * time.Millisecond)
		mu.Lock()
		inFlight--
		mu.Unlock()
		return fakeReply{text: "ok", promptTokens: 1, completionTokens: 1}
	})
	member := f.newSession(t)

	first := groupPacket()
	first.Request = "dispatch one"
	turn1, err := f.mgr.SubmitGroup(t.Context(), GroupTurnRequest{SessionID: member.ID, Packet: first})
	require.NoError(t, err)

	second := groupPacket()
	second.Request = "dispatch two"
	turn2, err := f.mgr.SubmitGroup(t.Context(), GroupTurnRequest{SessionID: member.ID, Packet: second})
	require.NoError(t, err)

	require.NoError(t, turn1.Wait())
	require.NoError(t, turn2.Wait())

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, []string{"dispatch one", "dispatch two"}, order, "the lane drains in arrival order")
	require.Equal(t, 1, maxInFlight, "two group dispatches to one agent never run concurrently")
	require.Equal(t, 2, src.commitCount())
	require.Equal(t, int64(41), src.lastCommit().ThroughSeq)
}

// TestSubmitGroup_PacketInputNotReplay covers the packet input contract
// (add-agent-groups design D3): the model input is built from the packet —
// group context in the system instruction, transcript rows, task results and
// the request as the fresh user input — and never from the member session's
// replayed log. The cursor commits the packet's ThroughSeq.
func TestSubmitGroup_PacketInputNotReplay(t *testing.T) {
	f, src := newGroupFixture(t, func(_ []*schema.Message) fakeReply {
		return fakeReply{text: "on it", promptTokens: 10, completionTokens: 2}
	})
	member := f.newSession(t)

	// A replayed marker in the member session's log must never reach the
	// model: the packet replaces the session replay (design D2).
	require.NoError(t, f.sessions.Append(t.Context(), member.ID, []sessions.Event{
		{Kind: sessions.KindUserMessage, Text: "REPLAY-MARKER-742"},
	}))

	packet := groupPacket()
	turn, err := f.mgr.SubmitGroup(t.Context(), GroupTurnRequest{SessionID: member.ID, Packet: packet})
	require.NoError(t, err)
	require.NoError(t, turn.Wait())

	calls := f.model.calls()
	require.Len(t, calls, 1)
	sys := calls[0][0]
	require.Equal(t, schema.System, sys.Role)
	require.Contains(t, sys.Content, "# Agent group: Launch Crew")
	require.Contains(t, sys.Content, "Report findings concisely.")
	require.Contains(t, sys.Content, "You scout primary sources.")
	require.Contains(t, sys.Content, "## Earlier group history (summary v2)")
	require.Contains(t, sys.Content, "Earlier the group planned the launch.")
	require.Contains(t, sys.Content, operatingContract)
	require.Contains(t, sys.Content, "# Environment")

	var joined strings.Builder
	for _, msg := range calls[0] {
		joined.WriteString(msg.Content + "\n")
	}
	input := joined.String()
	require.Contains(t, input, "[user] where are the docs?")
	require.Contains(t, input, "[agent agent-scout-2] I checked the wiki — nothing yet.")
	require.Contains(t, input, "[task t-9] the spec is frozen")
	require.Contains(t, input, `{"assigned":"Scout"}`)
	require.NotContains(t, input, "REPLAY-MARKER-742")
	require.Equal(t, "Find the docs link", lastUserText(calls[0]), "the packet's request rides as the fresh input")

	// The group tools ride BESIDE the ordinary catalog surface
	// (fix-agent-group-member-tools design D1): the member's gated catalog
	// tools and the three in-channel group tools are offered together.
	require.ElementsMatch(t,
		[]string{ToolCurrentTime, ToolHTTPGet, ToolWebFetch, ToolWebSearch,
			"group_history", "group_assign_task", "group_task_result"},
		f.model.toolsOf(0))

	// The cursor commits the packet's coverage.
	require.Equal(t, 1, src.commitCount())
	require.Equal(t, groupCursorCommit{GroupID: "group-1", AgentID: f.agent.ID, ThroughSeq: 41}, src.lastCommit())

	// The ordinary tool surface of a non-group turn is unchanged: the same
	// fixture's chat turn still offers the catalog tools, never the group
	// tools.
	chat := f.newSession(t)
	ordinary, err := f.mgr.Send(t.Context(), chat.ID, "hello")
	require.NoError(t, err)
	require.NoError(t, ordinary.Wait())
	calls = f.model.calls()
	require.Len(t, calls, 2)
	ordinaryTools := f.model.toolsOf(1)
	require.Contains(t, ordinaryTools, ToolCurrentTime)
	require.NotContains(t, ordinaryTools, "group_history")
	require.ElementsMatch(t,
		[]string{ToolCurrentTime, ToolHTTPGet, ToolWebFetch, ToolWebSearch},
		ordinaryTools)
}

// TestSubmitGroup_WorkspaceRidesTheTurn covers the composite-surface
// scenarios (fix-agent-group-member-tools spec "Member turn keeps its
// workspace" and "Group tools ride beside the work tools"): a member's group
// turn carries the workspace-rooted filesystem tools beside the group tools,
// the environment block names the member's session directory (design D2 —
// the session-relative paths ride), and a read_file call really answers from
// the member's own workspace.
func TestSubmitGroup_WorkspaceRidesTheTurn(t *testing.T) {
	f, _, ws := newGroupWorkspaceFixture(t, func(input []*schema.Message) fakeReply {
		if sawToolMessage(input) {
			return fakeReply{text: "read it", promptTokens: 1, completionTokens: 1}
		}
		return toolCallsReply("call-read", ToolReadFile, `{"file_path":"/workspace/notes.md"}`)
	})
	// A file in the member's own workspace: the jailed read_file must reach
	// it through the /workspace mount.
	require.NoError(t, os.MkdirAll(filepath.Join(ws.Root(), "tester"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(ws.Root(), "tester", "notes.md"), []byte("the workspace note"), 0o600))

	member := f.newSession(t)
	turn, err := f.mgr.SubmitGroup(t.Context(), GroupTurnRequest{SessionID: member.ID, Packet: groupPacket()})
	require.NoError(t, err)
	require.NoError(t, turn.Wait())

	// The surface: workspace-rooted fs tools, the catalog utilities, and the
	// three group tools, all offered together.
	tools := f.model.toolsOf(0)
	for _, name := range []string{ToolReadFile, ToolLs, ToolCurrentTime} {
		require.Contains(t, tools, name, "the ordinary work surface rides the group turn")
	}
	for _, name := range []string{"group_history", "group_assign_task", "group_task_result"} {
		require.Contains(t, tools, name, "the in-channel group tool rides beside the work tools")
	}

	// The environment block names the member's session directory (design
	// D2): the group branch no longer zero-clears the session-relative paths.
	sys := f.model.calls()[0][0]
	require.Contains(t, sys.Content, "Session directory: /workspace/sessions/")

	// And the read really answered from the member's workspace.
	events, err := f.sessions.Events(t.Context(), member.ID)
	require.NoError(t, err)
	activities := eventsOfKind(events, sessions.KindToolActivity)
	require.NotEmpty(t, activities, "the group turn's read_file call executed")
	read := activities[len(activities)-1]
	require.Equal(t, ToolReadFile, read.Tool.Name)
	require.Equal(t, sessions.ToolStatusCompleted, read.Tool.Status)
	require.Contains(t, read.Tool.Result, "the workspace note")
}

// TestSubmitGroup_AllowlistStillGovernsWorkTools covers the gate scenario
// (fix-agent-group-member-tools spec "Agent allowlist still governs the work
// tools"): an agent allowlist that excludes the filesystem tools excludes
// them from a group turn too, while the in-channel group tools remain
// available.
func TestSubmitGroup_AllowlistStillGovernsWorkTools(t *testing.T) {
	f, _, _ := newGroupWorkspaceFixture(t, func(_ []*schema.Message) fakeReply {
		return fakeReply{text: "ok", promptTokens: 1, completionTokens: 1}
	})
	agent := f.agent
	agent.Tools = []string{ToolCurrentTime}
	require.NoError(t, f.store.Agents().Update(t.Context(), &agent))
	f.agent = agent

	member := f.newSession(t)
	turn, err := f.mgr.SubmitGroup(t.Context(), GroupTurnRequest{SessionID: member.ID, Packet: groupPacket()})
	require.NoError(t, err)
	require.NoError(t, turn.Wait())

	require.ElementsMatch(t,
		[]string{ToolCurrentTime, "group_history", "group_assign_task", "group_task_result"},
		f.model.toolsOf(0), "the allowlist withholds the fs tools; the group tools remain")
}

// TestSubmitGroup_GroupToolNameCollisionFails covers the merge guard
// (fix-agent-group-member-tools design D1): a source-provided tool named like
// an ordinary catalog tool fails the turn with a clear error instead of
// silently shadowing the catalog tool.
func TestSubmitGroup_GroupToolNameCollisionFails(t *testing.T) {
	src := &fakeGroupTurnSource{}
	for _, name := range []string{ToolCurrentTime, "group_history", "group_assign_task", "group_task_result"} {
		gt, err := newFakeGroupTool(name)
		require.NoError(t, err)
		src.tools = append(src.tools, gt)
	}
	ref := NewGroupTurnRef()
	ref.Set(src)
	f := newFixture(t, func(_ []*schema.Message) fakeReply { return fakeReply{text: "ok"} }, WithGroupTurns(ref))
	member := f.newSession(t)

	turn, err := f.mgr.SubmitGroup(t.Context(), GroupTurnRequest{SessionID: member.ID, Packet: groupPacket()})
	require.NoError(t, err, "the collision is the turn's outcome, not SubmitGroup's")
	require.ErrorContains(t, turn.Wait(), "collides")
	require.ErrorContains(t, turn.Wait(), ToolCurrentTime)
	require.False(t, f.mgr.Busy(t.Context(), member.ID), "the member session is released")
}

// TestSubmitGroup_FullPacketRebuildsCursor covers the full-packet fallback
// (add-agent-groups design D3): a packet flagged full — a new or compacted
// member session — renders its whole history and the cursor commits (is
// rebuilt) to the packet's ThroughSeq like any other dispatch.
func TestSubmitGroup_FullPacketRebuildsCursor(t *testing.T) {
	f, src := newGroupFixture(t, func(_ []*schema.Message) fakeReply {
		return fakeReply{text: "ok", promptTokens: 1, completionTokens: 1}
	})
	member := f.newSession(t)

	packet := groupPacket()
	packet.FullPacket = true
	packet.Unread = append(packet.Unread, GroupTranscriptMessage{
		ID: "m0", Seq: 3, Kind: GroupMessageKindMessage, AuthorKind: GroupAuthorUser, Text: "the very first ask",
	})
	turn, err := f.mgr.SubmitGroup(t.Context(), GroupTurnRequest{SessionID: member.ID, Packet: packet})
	require.NoError(t, err)
	require.NoError(t, turn.Wait())

	calls := f.model.calls()
	require.Len(t, calls, 1)
	var joined strings.Builder
	for _, msg := range calls[0] {
		joined.WriteString(msg.Content + "\n")
	}
	require.Contains(t, joined.String(), "the very first ask", "the full packet renders its whole history")
	require.Equal(t, 1, src.commitCount())
	require.Equal(t, int64(41), src.lastCommit().ThroughSeq, "the cursor is rebuilt to the packet's ThroughSeq")
}

// TestSubmitGroup_Unavailable covers the seam's wiring states: no ref, and a
// ref the groups service has not bound into yet, both fail with
// ErrGroupTurnsUnavailable; binding the source late unblocks dispatching.
func TestSubmitGroup_Unavailable(t *testing.T) {
	f := newFixture(t, func(_ []*schema.Message) fakeReply { return fakeReply{text: "ok"} })
	member := f.newSession(t)

	_, err := f.mgr.SubmitGroup(t.Context(), GroupTurnRequest{SessionID: member.ID, Packet: groupPacket()})
	require.ErrorIs(t, err, ErrGroupTurnsUnavailable)

	ref := NewGroupTurnRef()
	f2 := newFixture(t, func(_ []*schema.Message) fakeReply { return fakeReply{text: "ok"} }, WithGroupTurns(ref))
	member2 := f2.newSession(t)

	_, err = f2.mgr.SubmitGroup(t.Context(), GroupTurnRequest{SessionID: member2.ID, Packet: groupPacket()})
	require.ErrorIs(t, err, ErrGroupTurnsUnavailable, "an unbound ref is unavailable")

	// Late binding: the groups service binds after the manager was built.
	src := &fakeGroupTurnSource{}
	ref.Set(src)
	turn, err := f2.mgr.SubmitGroup(t.Context(), GroupTurnRequest{SessionID: member2.ID, Packet: groupPacket()})
	require.NoError(t, err)
	require.NoError(t, turn.Wait())
	require.Equal(t, 1, src.commitCount())
}

// TestSubmitGroup_UnknownSession covers the request-level rejection.
func TestSubmitGroup_UnknownSession(t *testing.T) {
	f, _ := newGroupFixture(t, func(_ []*schema.Message) fakeReply { return fakeReply{text: "ok"} })
	_, err := f.mgr.SubmitGroup(t.Context(), GroupTurnRequest{SessionID: "nope", Packet: groupPacket()})
	require.ErrorIs(t, err, store.ErrNotFound)
}

// TestSubmitGroup_SourceToolsError covers the dispatch failure path: a group
// tool resolution failure finishes the submitted Turn with the error, commits
// no cursor, and leaves the member session free.
func TestSubmitGroup_SourceToolsError(t *testing.T) {
	f, src := newGroupFixture(t, func(_ []*schema.Message) fakeReply { return fakeReply{text: "ok"} })
	src.toolsErr = errors.New("group store down")
	member := f.newSession(t)

	turn, err := f.mgr.SubmitGroup(t.Context(), GroupTurnRequest{SessionID: member.ID, Packet: groupPacket()})
	require.NoError(t, err, "the failure is the turn's outcome, not SubmitGroup's")
	require.ErrorContains(t, turn.Wait(), "group store down")

	require.Equal(t, 0, src.commitCount(), "a failed dispatch commits no cursor")
	require.False(t, f.mgr.Busy(t.Context(), member.ID), "the member session is released")

	// The session takes a new dispatch normally.
	src.toolsErr = nil
	retry, err := f.mgr.SubmitGroup(t.Context(), GroupTurnRequest{SessionID: member.ID, Packet: groupPacket()})
	require.NoError(t, err)
	require.NoError(t, retry.Wait())
}

// TestSummarizeText_CallsModelDirectly covers the summarizer seam
// (add-agent-groups design D3): one direct model call — the compaction
// summarizer prompt with the caller's instruction appended, the material as
// the user message — returns the reply text.
func TestSummarizeText_CallsModelDirectly(t *testing.T) {
	f := newFixture(t, func(_ []*schema.Message) fakeReply {
		return fakeReply{text: "the dense summary", promptTokens: 30, completionTokens: 5}
	})

	got, err := f.mgr.SummarizeText(t.Context(), f.agent.ID, "Focus on decisions.", "the transcript body")
	require.NoError(t, err)
	require.Equal(t, "the dense summary", got)

	calls := f.model.calls()
	require.Len(t, calls, 1)
	require.Len(t, calls[0], 2)
	require.Equal(t, schema.System, calls[0][0].Role)
	require.Equal(t, summarizeSystemPrompt+"\n\nFocus on decisions.", calls[0][0].Content)
	require.Equal(t, schema.User, calls[0][1].Role)
	require.Equal(t, "the transcript body", calls[0][1].Content)

	// No instruction: the bare summarizer prompt leads.
	got, err = f.mgr.SummarizeText(t.Context(), f.agent.ID, "", "more material")
	require.NoError(t, err)
	require.Equal(t, "the dense summary", got)
	require.Equal(t, summarizeSystemPrompt, f.model.calls()[1][0].Content)

	_, err = f.mgr.SummarizeText(t.Context(), "nope", "instruction", "text")
	require.ErrorIs(t, err, store.ErrNotFound)
}

// TestSummarizeText_IgnoresBusyLane pins the no-lane constraint
// (add-agent-groups design D3): the summarizer call succeeds while the
// agent's lane and the session's busy slot are held by an in-flight turn —
// acquiring either would deadlock against the turn that triggered it.
func TestSummarizeText_IgnoresBusyLane(t *testing.T) {
	release := make(chan struct{})
	f := newFixture(t, func(input []*schema.Message) fakeReply {
		// The summarizer call must go through while the chat turn blocks:
		// only the chat call parks on the release.
		if isSummarizeCall(input) {
			return fakeReply{text: "lane-free summary", promptTokens: 1, completionTokens: 1}
		}
		<-release
		return fakeReply{text: "done", promptTokens: 1, completionTokens: 1}
	})
	sess := f.newSession(t)

	chat, err := f.mgr.Send(t.Context(), sess.ID, "long-running chat turn")
	require.NoError(t, err)
	require.True(t, f.mgr.Busy(t.Context(), sess.ID))

	got, err := f.mgr.SummarizeText(t.Context(), f.agent.ID, "", "text")
	require.NoError(t, err, "SummarizeText must not acquire the lane or the session slot")
	require.Equal(t, "lane-free summary", got)

	close(release)
	require.NoError(t, chat.Wait())
}

// TestCancel_DropsQueuedGroupDispatch covers the lane's cancellation
// handling (add-agent-groups design D6): cancelling a member session whose
// dispatch is still queued drops the dispatch — its Turn completes with
// ErrTurnCancelled and no turn runs for it.
func TestCancel_DropsQueuedGroupDispatch(t *testing.T) {
	release := make(chan struct{})
	f, src := newGroupFixture(t, func(_ []*schema.Message) fakeReply {
		<-release
		return fakeReply{text: "done", promptTokens: 1, completionTokens: 1}
	})
	sess := f.newSession(t)
	member := f.newSession(t)

	chat, err := f.mgr.Send(t.Context(), sess.ID, "chat one")
	require.NoError(t, err)

	group, err := f.mgr.SubmitGroup(t.Context(), GroupTurnRequest{SessionID: member.ID, Packet: groupPacket()})
	require.NoError(t, err)

	require.NoError(t, f.mgr.Cancel(t.Context(), member.ID))
	require.ErrorIs(t, group.Wait(), ErrTurnCancelled, "the queued dispatch is dropped, not run")

	close(release)
	require.NoError(t, chat.Wait())

	// Only the chat turn ran; no cursor commits for the dropped dispatch.
	require.Eventually(t, func() bool { return len(f.model.calls()) == 1 }, time.Second, time.Millisecond)
	require.Equal(t, 0, src.commitCount())

	// The session takes a fresh dispatch after the cancel.
	retry, err := f.mgr.SubmitGroup(t.Context(), GroupTurnRequest{SessionID: member.ID, Packet: groupPacket()})
	require.NoError(t, err)
	require.NoError(t, retry.Wait())
	require.Equal(t, 1, src.commitCount())
}

// TestSend_BusySessionFailsFastWithQueuedGroupWork pins the ordinary
// semantics against the lane (add-agent-groups design D6): a group dispatch
// queues on a busy agent without error, while an ordinary double-Send on the
// busy session still fails fast with ErrTurnInProgress — and an ordinary send
// on the agent's other session still starts.
func TestSend_BusySessionFailsFastWithQueuedGroupWork(t *testing.T) {
	release := make(chan struct{})
	f, _ := newGroupFixture(t, func(_ []*schema.Message) fakeReply {
		<-release
		return fakeReply{text: "done", promptTokens: 1, completionTokens: 1}
	})
	sess := f.newSession(t)
	other := f.newSession(t)
	member := f.newSession(t)

	chat, err := f.mgr.Send(t.Context(), sess.ID, "chat one")
	require.NoError(t, err)

	group, err := f.mgr.SubmitGroup(t.Context(), GroupTurnRequest{SessionID: member.ID, Packet: groupPacket()})
	require.NoError(t, err, "the group dispatch queues")

	// Ordinary double-send: still fail-fast, the lane does not queue it.
	_, err = f.mgr.Send(t.Context(), sess.ID, "chat two")
	require.ErrorIs(t, err, ErrTurnInProgress)

	// Ordinary semantics elsewhere are unchanged: another session of the
	// same agent still runs concurrently with the queued group work.
	otherTurn, err := f.mgr.Send(t.Context(), other.ID, "chat on another session")
	require.NoError(t, err)

	close(release)
	require.NoError(t, chat.Wait())
	require.NoError(t, otherTurn.Wait())
	require.NoError(t, group.Wait())
}
