package groups

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/agents"
	"github.com/oniharnantyo/buddies/internal/sessions"
	"github.com/oniharnantyo/buddies/internal/store"
)

// Packet, cursor, and summary tests (add-agent-groups design D3, spec
// "Member threads and context packets"): the packet carries the group's
// standing context, the member's responsibility line, the request, and the
// unread rows; the cursor advances to the packet's ThroughSeq after a turn;
// a new/compacted member session receives the full packet; the recent window
// is bounded; the versioned summary builds only above the threshold.

// dispatchTo sends a mention to the agent and returns the submitted dispatch.
func dispatchTo(t *testing.T, h *harness, groupID string, agent *store.Agent, text string) agents.GroupTurnRequest {
	t.Helper()
	before := h.rt.submitCount()
	require.NoError(t, h.svc.Send(h.ctx, SendRequest{GroupID: groupID, Text: "@" + agent.Name + " " + text}))
	require.Equal(t, before+1, h.rt.submitCount())
	return h.rt.submitAt(before)
}

func TestPacket_ContentsAndCommit(t *testing.T) {
	h, g, atlas, _, _ := rosteredHarness(t)
	// Prior transcript: the roster change lines plus one user row.
	require.NoError(t, h.st.Groups().AppendMessage(h.ctx, &store.AgentGroupMessage{
		GroupID:    g.ID,
		Kind:       store.GroupMessageKindMessage,
		AuthorKind: store.GroupAuthorKindUser,
		Payload:    mustPayload(t, "prior context"),
	}))

	p := dispatchTo(t, h, g.ID, atlas, "work on it")

	require.Equal(t, g.ID, p.Packet.GroupID)
	require.Equal(t, "research title", p.Packet.GroupTitle)
	require.Equal(t, "be helpful", p.Packet.Instructions)
	require.Equal(t, "coordinates the crew", p.Packet.Responsibility, "the responsibility line derives from the agent description")
	require.Contains(t, p.Packet.Request, "work on it")
	require.False(t, p.Packet.FullPacket, "a seeded member dispatches a delta")
	// The unread rows run past the cursor (ThroughSeq 0) to the top —
	// including the dispatch's own user message row.
	require.Len(t, p.Packet.Unread, 5)
	require.Equal(t, "prior context", p.Packet.Unread[3].Text)
	require.Equal(t, "@Atlas work on it", p.Packet.Unread[4].Text)
	require.Equal(t, int64(5), p.Packet.ThroughSeq)
	require.Equal(t, int64(5), mustLastSeq(t, h, g.ID))

	// The runtime commits the cursor to the packet's ThroughSeq after the
	// turn's terminal outcome.
	h.rt.finishTurn(h.rt.submitCount()-1, nil)
	require.NoError(t, h.svc.CommitCursor(h.ctx, g.ID, atlas.ID, p.Packet.ThroughSeq))
	cur, err := h.st.Groups().GetCursor(h.ctx, g.ID, atlas.ID)
	require.NoError(t, err)
	require.Equal(t, p.Packet.ThroughSeq, cur.ThroughSeq)

	// The next packet carries only the unread rows.
	require.NoError(t, h.st.Groups().AppendMessage(h.ctx, &store.AgentGroupMessage{
		GroupID:    g.ID,
		Kind:       store.GroupMessageKindMessage,
		AuthorKind: store.GroupAuthorKindUser,
		Payload:    mustPayload(t, "fresh news"),
	}))
	p2 := dispatchTo(t, h, g.ID, atlas, "more work")
	require.Len(t, p2.Packet.Unread, 2, "a resumed member gets only the unread rows")
	require.Equal(t, "fresh news", p2.Packet.Unread[0].Text)
	require.Equal(t, "@Atlas more work", p2.Packet.Unread[1].Text)
	require.Len(t, p2.Packet.Recent, recentWindowBound(t, h, g.ID, cur.ThroughSeq), "the recent window carries already-delivered rows")
}

// mustLastSeq reads the group's transcript top.
func mustLastSeq(t *testing.T, h *harness, groupID string) int64 {
	t.Helper()
	seq, err := h.st.Groups().LastSeq(h.ctx, groupID)
	require.NoError(t, err)
	return seq
}

// recentWindowBound computes the expected recent-window size: the last
// recentWindow delivered rows, bounded by what exists below the cursor.
func recentWindowBound(t *testing.T, h *harness, groupID string, cursor int64) int {
	t.Helper()
	rows, err := h.st.Groups().ListMessages(h.ctx, groupID, cursor+1, recentWindow)
	require.NoError(t, err)
	return len(rows)
}

func TestPacket_NewMemberWithoutCursorGetsFullPacket(t *testing.T) {
	h, g, atlas, _, _ := rosteredHarness(t)
	// Wipe the cursor: a member whose acceptance state is lost rebuilds it.
	require.NoError(t, h.st.Groups().SetCursor(h.ctx, store.AgentGroupContext{GroupID: g.ID, AgentID: atlas.ID}))

	p := dispatchTo(t, h, g.ID, atlas, "hello")
	require.True(t, p.Packet.FullPacket, "a member without a cursor receives the full packet")
	require.Len(t, p.Packet.Unread, len(h.transcript(g.ID)), "the full packet covers the transcript from the beginning")
	require.Len(t, p.Packet.Recent, 0, "the full packet needs no recent window")

	// The runtime's commit rebuilds the read position.
	require.NoError(t, h.svc.CommitCursor(h.ctx, g.ID, atlas.ID, p.Packet.ThroughSeq))
	cur, err := h.st.Groups().GetCursor(h.ctx, g.ID, atlas.ID)
	require.NoError(t, err)
	require.Equal(t, p.Packet.ThroughSeq, cur.ThroughSeq)
}

func TestPacket_CompactedSessionGetsFullPacket(t *testing.T) {
	h, g, atlas, _, _ := rosteredHarness(t)

	// First dispatch: a delta; the runtime commits.
	p1 := dispatchTo(t, h, g.ID, atlas, "work")
	require.False(t, p1.Packet.FullPacket)
	task1 := h.groupTasks(g.ID)[0]
	h.rt.finishTurn(h.rt.submitCount()-1, nil)
	h.settleTask(task1.TaskID, store.GroupTaskStateCompleted)
	require.NoError(t, h.svc.CommitCursor(h.ctx, g.ID, atlas.ID, p1.Packet.ThroughSeq))

	// The member's session compacts: a checkpoint appears. The next packet
	// covers the transcript from the beginning and the read position
	// rebuilds.
	sessID := h.rt.sessionFor(atlas.ID)
	require.NoError(t, h.svc.deps.Sessions.SaveCheckpoint(h.ctx, sessID, sessions.Checkpoint{
		Summary: "compacted", UpToSeq: 3, TokensBefore: 100, TokensAfter: 10,
	}))
	p2 := dispatchTo(t, h, g.ID, atlas, "more")
	require.True(t, p2.Packet.FullPacket, "a compacted member receives the full packet")
	require.Len(t, p2.Packet.Unread, len(h.transcript(g.ID)))

	// Steady state: the unchanged checkpoint yields a delta again. The
	// store orders tasks by random hex id — pick the second task by
	// exclusion, never by position.
	var task2 store.AgentGroupTask
	for _, t := range h.groupTasks(g.ID) {
		if t.TaskID != task1.TaskID {
			task2 = t
		}
	}
	h.rt.finishTurn(h.rt.submitCount()-1, nil)
	h.settleTask(task2.TaskID, store.GroupTaskStateCompleted)
	require.NoError(t, h.svc.CommitCursor(h.ctx, g.ID, atlas.ID, p2.Packet.ThroughSeq))
	p3 := dispatchTo(t, h, g.ID, atlas, "again")
	require.False(t, p3.Packet.FullPacket, "an unchanged checkpoint dispatches deltas")
}

func TestPacket_RecentWindowBounded(t *testing.T) {
	h, g, atlas, _, _ := rosteredHarness(t)
	// Fill the transcript beyond the recent window and commit the cursor.
	for i := 0; i < recentWindow+7; i++ {
		require.NoError(t, h.st.Groups().AppendMessage(h.ctx, &store.AgentGroupMessage{
			GroupID:    g.ID,
			Kind:       store.GroupMessageKindMessage,
			AuthorKind: store.GroupAuthorKindUser,
			Payload:    mustPayload(t, fmt.Sprintf("row %d", i)),
		}))
	}
	lastSeq := mustLastSeq(t, h, g.ID)
	require.NoError(t, h.svc.CommitCursor(h.ctx, g.ID, atlas.ID, lastSeq))

	p := dispatchTo(t, h, g.ID, atlas, "next")
	require.Len(t, p.Packet.Unread, 1, "the member is caught up except the new request itself")
	require.Equal(t, "@Atlas next", p.Packet.Unread[0].Text)
	require.Len(t, p.Packet.Recent, recentWindow, "the bounded recent window rides a caught-up member's packet")
	require.Equal(t, lastSeq-recentWindow+1, p.Packet.Recent[0].Seq, "the window holds the LAST delivered rows, oldest first")
	require.Equal(t, lastSeq, p.Packet.Recent[recentWindow-1].Seq)
}

func TestPacket_DependentTaskResults(t *testing.T) {
	h, g, atlas, scout, _ := rosteredHarness(t)
	require.NoError(t, h.svc.Send(h.ctx, SendRequest{GroupID: g.ID, Text: "@Atlas run project"}))
	root := h.groupTasks(g.ID)[0]

	// The lead delegates a child to Scout, whose result completes it.
	assignThroughTool(t, h, g.ID, atlas.ID, "Scout", "child work")
	var child store.AgentGroupTask
	for _, task := range h.groupTasks(g.ID) {
		if task.TaskID != root.TaskID {
			child = task
		}
	}
	resultThroughTool(t, h, g.ID, scout.ID, "child finished")
	// The root stays open (stopped mid-run): the reply tier continues an
	// OPEN task, and the root's chain must survive for the follow-up to
	// carry the child's result.
	require.NoError(t, h.svc.StopTask(h.ctx, root.TaskID, "hold"))
	require.Equal(t, store.GroupTaskStateCompleted, h.taskByID(child.TaskID).State)

	// A follow-up dispatch on the root's chain carries the completed
	// result as a shared decision.
	require.NoError(t, h.svc.Send(h.ctx, SendRequest{GroupID: g.ID, Text: "wrap it up", ReplyToMessageID: root.RequestMessageID}))
	req := h.rt.submitAt(h.rt.submitCount() - 1)
	require.Len(t, req.Packet.TaskResults, 1)
	require.Equal(t, child.TaskID, req.Packet.TaskResults[0].TaskID)
	require.Equal(t, "child finished", req.Packet.TaskResults[0].Result)
}

func TestSummary_OnlyAboveThreshold(t *testing.T) {
	h, g, atlas, _, _ := rosteredHarness(t)

	// Below the threshold: no summarization.
	p := dispatchTo(t, h, g.ID, atlas, "small talk")
	h.rt.finishTurn(h.rt.submitCount()-1, nil)
	require.NoError(t, h.svc.CommitCursor(h.ctx, g.ID, atlas.ID, p.Packet.ThroughSeq))
	require.Eventually(t, func() bool { return true }, 10*time.Millisecond, time.Millisecond, "settle")
	require.Empty(t, h.rtSummaries(), "no summary below the threshold")

	// Above the threshold: one asynchronous, versioned summarization over
	// the older range.
	for i := 0; i < summaryThreshold+5; i++ {
		require.NoError(t, h.st.Groups().AppendMessage(h.ctx, &store.AgentGroupMessage{
			GroupID:    g.ID,
			Kind:       store.GroupMessageKindMessage,
			AuthorKind: store.GroupAuthorKindUser,
			Payload:    mustPayload(t, fmt.Sprintf("line %d", i)),
		}))
	}
	p2 := dispatchTo(t, h, g.ID, atlas, "big catch-up")
	require.Greater(t, len(p2.Packet.Unread), summaryThreshold)
	h.rt.finishTurn(h.rt.submitCount()-1, nil)
	require.NoError(t, h.svc.CommitCursor(h.ctx, g.ID, atlas.ID, p2.Packet.ThroughSeq))

	require.Eventually(t, func() bool { return len(h.rtSummaries()) == 1 }, 5*time.Second, 2*time.Millisecond, "summary builds")
	call := h.rtSummaries()[0]
	require.Equal(t, atlas.ID, call.AgentID)
	require.Equal(t, summaryInstruction, call.Instruction)

	sum, err := h.st.Groups().GetSummary(h.ctx, g.ID)
	require.NoError(t, err)
	require.Equal(t, int64(1), sum.Version, "the summary is versioned")
	require.Equal(t, "summary of "+renderTranscriptRowsOf(t, h, g.ID, sum.ThroughSeq), sum.Text)
	require.Equal(t, p2.Packet.ThroughSeq-recentWindow, sum.ThroughSeq, "the summary covers the rows older than the recent window")
}

// renderTranscriptRowsOf renders the summary's covered range for the fake
// summarizer's echo.
func renderTranscriptRowsOf(t *testing.T, h *harness, groupID string, throughSeq int64) string {
	t.Helper()
	rows, err := h.svc.readTranscriptRange(h.ctx, groupID, 0, throughSeq)
	require.NoError(t, err)
	return renderTranscriptRows(rows)
}

// rtSummaries snapshots the fake's summarize calls.
func (h *harness) rtSummaries() []summarizeCall {
	h.rt.mu.Lock()
	defer h.rt.mu.Unlock()
	return append([]summarizeCall(nil), h.rt.summaries...)
}

// TestSummary_CompletesWithoutBlocking pins that the async build never
// blocks the turn-end path: the summary lands after the turn's terminal
// state is persisted, within the test deadline.
func TestSummary_CompletesWithoutBlocking(t *testing.T) {
	h, g, atlas, _, _ := rosteredHarness(t)
	for i := 0; i < summaryThreshold+1; i++ {
		require.NoError(t, h.st.Groups().AppendMessage(h.ctx, &store.AgentGroupMessage{
			GroupID:    g.ID,
			Kind:       store.GroupMessageKindMessage,
			AuthorKind: store.GroupAuthorKindUser,
			Payload:    mustPayload(t, "x"),
		}))
	}
	p := dispatchTo(t, h, g.ID, atlas, "go")
	h.rt.finishTurn(h.rt.submitCount()-1, nil)
	require.NoError(t, h.svc.CommitCursor(h.ctx, g.ID, atlas.ID, p.Packet.ThroughSeq))
	require.Eventually(t, func() bool {
		_, err := h.st.Groups().GetSummary(h.ctx, g.ID)
		return err == nil
	}, 5*time.Second, 2*time.Millisecond, "the summary lands asynchronously")
}

// A cursor bound to a session the user deleted from the rail used to poison
// dispatch forever: SubmitGroup loads the session and failed, the task
// reverted to queued, and every boot warned and retried. buildPacket now
// heals — re-minting the member session (the AddMember creation path) and
// reseeding the cursor, so the dispatch lands on a live thread with a full
// context packet.
func TestPacket_HealsDeletedMemberSession(t *testing.T) {
	h, g, atlas, _, _ := rosteredHarness(t)

	cur, err := h.st.Groups().GetCursor(h.ctx, g.ID, atlas.ID)
	require.NoError(t, err)
	require.NotEmpty(t, cur.SessionID, "group creation seeds member cursors")
	require.NoError(t, h.st.Sessions().Delete(h.ctx, cur.SessionID))

	p := dispatchTo(t, h, g.ID, atlas, "again after deletion")
	require.NotEqual(t, cur.SessionID, p.SessionID, "the dispatch lands on a re-minted member session")
	require.True(t, p.Packet.FullPacket, "the fresh thread rebuilds the full context")

	cur2, err := h.st.Groups().GetCursor(h.ctx, g.ID, atlas.ID)
	require.NoError(t, err)
	require.Equal(t, p.SessionID, cur2.SessionID, "the cursor reseeds to the new session")
	sess, err := h.st.Sessions().ByID(h.ctx, cur2.SessionID)
	require.NoError(t, err)
	require.Equal(t, atlas.ID, sess.AgentID, "the re-minted session belongs to the member's agent")
}
