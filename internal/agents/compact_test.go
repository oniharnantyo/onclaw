package agents

import (
	"testing"

	"github.com/cloudwego/eino/schema"

	"github.com/oniharnantyo/buddies/internal/sessions"
	"github.com/stretchr/testify/require"
)

// TestManager_CompactionAtThreshold is 4.5's acceptance test: a session
// driven past the compaction threshold compacts on the next turn — the older
// history is summarized into a persisted checkpoint, a compaction marker is
// recorded, and the next turn runs with the compacted working context (the
// summary replaces the old messages; nothing older than the boundary is
// replayed to the model).
func TestManager_CompactionAtThreshold(t *testing.T) {
	const summaryText = "summary: the user greeted the agent and was answered"

	f := newFixture(t, func(input []*schema.Message) fakeReply {
		if isSummarizeCall(input) {
			return fakeReply{text: summaryText, promptTokens: 500, completionTokens: 30}
		}
		// Normal turns: turn 1 lands above the compaction threshold
		// (0.8 x 200000 = 160000), so the NEXT turn must compact first.
		return fakeReply{text: "turn answer", promptTokens: 170000, completionTokens: 1000}
	}, WithPromptDocs(&fakePromptDocs{identity: "You are terse."}))
	sess := f.newSession(t)

	// Turn 1: a normal turn that pushes usage past the threshold.
	turn1, err := f.mgr.Send(t.Context(), sess.ID, "hello")
	require.NoError(t, err)
	require.NoError(t, turn1.Wait())

	// Nothing compacted yet.
	events, err := f.sessions.Events(t.Context(), sess.ID)
	require.NoError(t, err)
	require.Empty(t, eventsOfKind(events, sessions.KindCompaction))

	// Turn 2: the gate trips; the summarizer runs and the turn proceeds.
	turn2, err := f.mgr.Send(t.Context(), sess.ID, "what happened so far?")
	require.NoError(t, err)
	require.NoError(t, turn2.Wait())

	// The compaction marker is persisted with its estimates and checkpoint
	// reference, in order before this turn's user message.
	events, err = f.sessions.Events(t.Context(), sess.ID)
	require.NoError(t, err)
	markers := eventsOfKind(events, sessions.KindCompaction)
	require.Len(t, markers, 1)
	marker := markers[0]
	require.NotNil(t, marker.Compaction)
	require.Equal(t, sessions.CheckpointID(sess.ID), marker.Compaction.CheckpointID)
	require.Equal(t, summaryText, marker.Compaction.Summary)
	require.Equal(t, 171000, marker.Compaction.TokensBefore, "the usage that tripped the gate")
	require.Positive(t, marker.Compaction.TokensAfter)

	// The checkpoint is persisted and bounded at the log position before
	// turn 2 (turn 1's three events: user, assistant, completed).
	cp, ok, err := f.sessions.Checkpoint(t.Context(), sess.ID)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, summaryText, cp.Summary)
	require.Equal(t, int64(3), cp.UpToSeq)

	// Turn 2's model input is compacted: system prompt + summary, then the
	// new user message — the old turns are summarized, not replayed.
	// Recorded calls: turn 1's normal call, turn 2's summarizer, turn 2's
	// normal call.
	calls := f.model.calls()
	require.Len(t, calls, 3)

	normal := calls[2]
	require.False(t, isSummarizeCall(normal))
	require.Equal(t, schema.System, normal[0].Role)
	// The system message carries the composed persona (identity) plus the
	// compaction summary — the summarizer call is persona-free, the turn
	// call is not.
	require.Contains(t, normal[0].Content, "You are terse.", "the persona must ride the working context")
	require.Contains(t, normal[0].Content, summaryText, "the summary must ride the working context")
	require.Len(t, normal, 2, "no pre-compaction messages may be replayed")
	require.Equal(t, "what happened so far?", normal[1].Content)
	for _, msg := range normal {
		require.NotContains(t, msg.Content, "hello", "turn 1's user message must be out of the working context")
	}

	// The stored transcript is NEVER truncated by compaction (spec:
	// compaction affects the working context only).
	full, err := f.sessions.Events(t.Context(), sess.ID)
	require.NoError(t, err)
	turn1User := eventsOfKind(full, sessions.KindUserMessage)
	require.Len(t, turn1User, 2)
	require.Equal(t, "hello", turn1User[0].Text, "turn 1's transcript must survive compaction")
	require.Len(t, full, len(events), "compaction only appends; the log is intact")
}

// TestManager_CompactionUsageFoldsIntoTurn pins the accounting: the
// summarizer call's real tokens fold into the compacting turn's usage, and
// the turn completes with the compacted context's final input size.
func TestManager_CompactionUsageFoldsIntoTurn(t *testing.T) {
	// Turn 1 reports above-threshold usage; the post-compaction turns run on
	// a small (compacted) context and report little.
	normalCalls := 0
	f := newFixture(t, func(input []*schema.Message) fakeReply {
		if isSummarizeCall(input) {
			return fakeReply{text: "summary", promptTokens: 500, completionTokens: 30}
		}
		normalCalls++
		if normalCalls == 1 {
			return fakeReply{text: "answer", promptTokens: 170000, completionTokens: 1000}
		}
		return fakeReply{text: "answer", promptTokens: 900, completionTokens: 100}
	})
	sess := f.newSession(t)

	turn1, err := f.mgr.Send(t.Context(), sess.ID, "push the context up")
	require.NoError(t, err)
	require.NoError(t, turn1.Wait())

	turn2, err := f.mgr.Send(t.Context(), sess.ID, "again")
	require.NoError(t, err)
	require.NoError(t, turn2.Wait())

	events, err := f.sessions.Events(t.Context(), sess.ID)
	require.NoError(t, err)
	completed := eventsOfKind(events, sessions.KindTurnCompleted)
	require.Len(t, completed, 2)
	require.NotNil(t, completed[1].Usage)
	require.Equal(t, 500+900, completed[1].Usage.InputTokens, "summarizer + turn calls sum")
	require.Equal(t, 30+100, completed[1].Usage.OutputTokens)
	require.Equal(t, 900, completed[1].Usage.FinalInputTokens, "the last call's input wins")

	// After compaction the used tokens drop back under the limit.
	usage, err := f.mgr.ContextUsage(t.Context(), sess.ID)
	require.NoError(t, err)
	require.Less(t, usage.Used, 160000, "the session continues within the limit")
}

// TestManager_CompactionMarkerRidesLiveStream covers the live view of a
// compaction: subscribers see the marker between the user message and the
// turn's output.
func TestManager_CompactionMarkerRidesLiveStream(t *testing.T) {
	f := newFixture(t, func(input []*schema.Message) fakeReply {
		if isSummarizeCall(input) {
			return fakeReply{text: "summary", promptTokens: 500, completionTokens: 30}
		}
		return fakeReply{text: "answer", promptTokens: 170000, completionTokens: 1000}
	})
	sess := f.newSession(t)

	turn1, err := f.mgr.Send(t.Context(), sess.ID, "fill up")
	require.NoError(t, err)
	require.NoError(t, turn1.Wait())

	collector := collect(t, f.mgr, sess.ID)
	turn2, err := f.mgr.Send(t.Context(), sess.ID, "continue")
	require.NoError(t, err)
	require.NoError(t, turn2.Wait())

	live := collector.Wait()
	kinds := kindsOf(live)
	markerIdx := -1
	for i, kind := range kinds {
		if kind == string(sessions.KindCompaction) {
			markerIdx = i
		}
	}
	require.NotEqual(t, -1, markerIdx, "the marker must ride the live stream")
	require.Equal(t, string(sessions.KindUserMessage), kinds[markerIdx-1],
		"the marker lands after this turn's user message and before its deltas and outputs")

	// The turn's outputs still follow on the same live stream.
	after := kinds[markerIdx+1:]
	require.Contains(t, after, string(sessions.KindAssistantMessage))
	require.Contains(t, after, string(sessions.KindTurnCompleted))
}
