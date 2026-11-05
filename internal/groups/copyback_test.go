package groups

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/sessions"
	"github.com/oniharnantyo/buddies/internal/store"
)

// Copy-back tests (add-agent-groups design D7, spec "Member results copied
// back with attribution"): final assistant text rows copy into the shared
// transcript authored by the member, keyed by the turn id so a regenerated
// turn REPLACES its earlier copy; tool and reasoning rows never copy; task
// results land as authored result rows through the in-channel tool.

func TestCopyBack_FinalAssistantTextCopiesOnce(t *testing.T) {
	h, g, _, scout, _ := rosteredHarness(t)
	require.NoError(t, h.svc.Send(h.ctx, SendRequest{GroupID: g.ID, Text: "@Scout summarize"}))
	task := h.groupTasks(g.ID)[0]

	// The member turn's final assistant text rides the live subscription.
	h.emitToPump(h.rt.sessionFor(scout.ID), sessions.Event{
		TurnID: "turn-scout-1",
		Kind:   sessions.KindAssistantMessage,
		Text:   "here is the summary",
	})
	requireEventually(t, "copy-back lands", func() bool {
		for _, r := range h.messagesOf(g.ID, store.GroupMessageKindMessage) {
			if r.MessageID == "turn-scout-1" {
				return true
			}
		}
		return false
	})

	// Exactly one authored message for that answer.
	rows := h.messagesOf(g.ID, store.GroupMessageKindMessage)
	var copied []store.AgentGroupMessage
	for _, r := range rows {
		if r.MessageID == "turn-scout-1" {
			copied = append(copied, r)
		}
	}
	require.Len(t, copied, 1)
	require.Equal(t, store.GroupAuthorKindAgent, copied[0].AuthorKind)
	require.Equal(t, scout.ID, deref(copied[0].AuthorAgentID))
	require.Equal(t, "here is the summary", payloadText(copied[0]))
	require.NotNil(t, copied[0].TaskID, "the copied row links the task the turn served")
	require.Equal(t, task.TaskID, *copied[0].TaskID)
}

func TestCopyBack_RegenerateReplacesNotDuplicates(t *testing.T) {
	h, g, _, scout, _ := rosteredHarness(t)
	require.NoError(t, h.svc.Send(h.ctx, SendRequest{GroupID: g.ID, Text: "@Scout summarize"}))
	sessID := h.rt.sessionFor(scout.ID)

	h.emitToPump(sessID, sessions.Event{
		TurnID: "turn-1",
		Kind:   sessions.KindAssistantMessage,
		Text:   "first answer",
	})
	requireEventually(t, "first copy lands", func() bool {
		return len(h.rtCopy(g.ID, "turn-1")) == 1
	})

	// The regenerated turn's reply events carry the original turn id in
	// VariantOf: the stable message id replaces the superseded copy.
	h.emitToPump(sessID, sessions.Event{
		TurnID:    "turn-2",
		VariantOf: "turn-1",
		Kind:      sessions.KindAssistantMessage,
		Text:      "better answer",
	})
	requireEventually(t, "replacement lands", func() bool {
		rows := h.rtCopy(g.ID, "turn-1")
		return len(rows) == 1 && payloadText(rows[0]) == "better answer"
	})
	require.Len(t, h.rtCopy(g.ID, "turn-1"), 1, "the transcript holds one row for the turn")
	require.Len(t, h.rtCopy(g.ID, "turn-2"), 0, "the variant's own turn id never mints a second row")
}

func TestCopyBack_NonTextRowsNeverCopy(t *testing.T) {
	h, g, _, scout, _ := rosteredHarness(t)
	require.NoError(t, h.svc.Send(h.ctx, SendRequest{GroupID: g.ID, Text: "@Scout summarize"}))
	sessID := h.rt.sessionFor(scout.ID)
	before := len(h.transcript(g.ID))

	h.emitToPump(sessID, sessions.Event{TurnID: "t", Kind: sessions.KindTextDelta, Text: "del"})
	h.emitToPump(sessID, sessions.Event{TurnID: "t", Kind: sessions.KindReasoningDelta, Text: "th"})
	h.emitToPump(sessID, sessions.Event{TurnID: "t", Kind: sessions.KindReasoning, Text: "thinking"})
	h.emitToPump(sessID, sessions.Event{TurnID: "t", Kind: sessions.KindToolActivity,
		Tool: &sessions.ToolActivity{CallID: "c", Name: "ls", Status: sessions.ToolStatusCompleted, Result: "files"}})
	h.emitToPump(sessID, sessions.Event{TurnID: "t", Kind: sessions.KindTurnCompleted})
	h.emitToPump(sessID, sessions.Event{TurnID: "t", Kind: sessions.KindAssistantMessage, Text: "   "})

	requireEventually(t, "pump drains", func() bool { return true })
	require.Len(t, h.transcript(g.ID), before, "tool, reasoning, delta, and blank rows never copy")
}

func TestCopyBack_ResultLandsAsAuthoredResultRow(t *testing.T) {
	h, g, _, scout, _ := rosteredHarness(t)
	require.NoError(t, h.svc.Send(h.ctx, SendRequest{GroupID: g.ID, Text: "@Scout dig"}))
	task := h.groupTasks(g.ID)[0]

	resultThroughTool(t, h, g.ID, scout.ID, "the dig is done")

	results := h.messagesOf(g.ID, store.GroupMessageKindResult)
	require.Len(t, results, 1)
	require.Equal(t, store.GroupAuthorKindAgent, results[0].AuthorKind)
	require.Equal(t, scout.ID, deref(results[0].AuthorAgentID))
	require.Equal(t, task.TaskID, *results[0].TaskID)
	require.Equal(t, "the dig is done", payloadText(results[0]))
	require.Equal(t, store.GroupTaskStateCompleted, h.taskByID(task.TaskID).State)
}

func TestCopyBack_RemoveMemberStopsThePump(t *testing.T) {
	h, g, _, scout, _ := rosteredHarness(t)
	sessID := h.rt.sessionFor(scout.ID)
	h.rt.mu.Lock()
	subs := append([]*fakeSubscription(nil), h.rt.subs[sessID]...)
	h.rt.mu.Unlock()
	require.Len(t, subs, 1)

	require.NoError(t, h.svc.RemoveMember(h.ctx, g.ID, scout.ID))
	before := len(h.transcript(g.ID))

	// The removed member's subscription was closed: the pump is gone, and
	// late events reach nothing.
	require.False(t, subs[0].emit(sessions.Event{TurnID: "late", Kind: sessions.KindAssistantMessage, Text: "ghost write"}),
		"the pump's subscription is closed after member removal")
	require.Len(t, h.transcript(g.ID), before)
}

// rtCopy returns the transcript rows copied for one turn id.
func (h *harness) rtCopy(groupID, turnID string) []store.AgentGroupMessage {
	h.t.Helper()
	out := make([]store.AgentGroupMessage, 0)
	for _, r := range h.messagesOf(groupID, store.GroupMessageKindMessage) {
		if r.MessageID == turnID {
			out = append(out, r)
		}
	}
	return out
}
