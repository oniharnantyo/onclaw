package sessions_test

import (
	"testing"

	"github.com/oniharnantyo/buddies/internal/sessions"
	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/stretchr/testify/require"
)

// The windowing and delete tests (tasks 6.1/6.5, design D5/D6, spec
// chat-view "Transcript windowing" / "Sessions organized per agent").

// seedLog appends n simple events to the session, one per turn, and returns
// nothing — the assertions read the log back.
func seedLog(t *testing.T, svc sessions.Service, sessionID string, n int) {
	t.Helper()
	for i := 1; i <= n; i++ {
		require.NoError(t, svc.Append(t.Context(), sessionID, []sessions.Event{
			{Kind: sessions.KindUserMessage, Text: "msg"},
		}))
	}
}

// TestService_EventsPage_NewestFirstAndBoundaries pins the paging contract
// (design D6): the newest page comes back in replay (ascending) order,
// beforeSeq pages backward from that boundary, and pages walk to the
// beginning without overlap or gaps.
func TestService_EventsPage_NewestFirstAndBoundaries(t *testing.T) {
	ctx := t.Context()
	svc, _ := newService(t)

	sess, err := svc.CreateSession(ctx, "agent-1", "")
	require.NoError(t, err)
	seedLog(t, svc, sess.ID, 10)

	// Newest page first: the last 4 events, ascending.
	page, err := svc.EventsPage(ctx, sess.ID, 0, 4)
	require.NoError(t, err)
	require.Len(t, page, 4)
	require.Equal(t, []int64{7, 8, 9, 10}, seqsOf(page))

	// Backward from a boundary: strictly older events, ascending, without
	// re-serving the boundary event.
	older, err := svc.EventsPage(ctx, sess.ID, page[0].Seq, 4)
	require.NoError(t, err)
	require.Equal(t, []int64{3, 4, 5, 6}, seqsOf(older))

	// The tail page stops at the beginning; short pages are fine.
	tail, err := svc.EventsPage(ctx, sess.ID, older[0].Seq, 4)
	require.NoError(t, err)
	require.Equal(t, []int64{1, 2}, seqsOf(tail), "the log's beginning ends the walk")

	// Past the beginning: an empty page, not nil, not an error.
	exhausted, err := svc.EventsPage(ctx, sess.ID, 1, 4)
	require.NoError(t, err)
	require.NotNil(t, exhausted)
	require.Empty(t, exhausted)

	// A non-positive limit takes the default page size (the log is shorter,
	// so the whole log comes back).
	def, err := svc.EventsPage(ctx, sess.ID, 0, 0)
	require.NoError(t, err)
	require.Len(t, def, 10)

	// An unknown session pages empty.
	none, err := svc.EventsPage(ctx, "nope", 0, 4)
	require.NoError(t, err)
	require.NotNil(t, none)
	require.Empty(t, none)
}

// TestService_EventsPage_MidTurnBoundary pins the spec's window-boundary
// rule: a page ending inside a turn's events still replays in order, so the
// reducer sees one continuous turn once both pages are folded.
func TestService_EventsPage_MidTurnBoundary(t *testing.T) {
	ctx := t.Context()
	svc, _ := newService(t)

	sess, err := svc.CreateSession(ctx, "agent-1", "")
	require.NoError(t, err)
	require.NoError(t, svc.Append(ctx, sess.ID, []sessions.Event{
		{TurnID: "turn-1", Kind: sessions.KindUserMessage, Text: "q"},
		{TurnID: "turn-1", Kind: sessions.KindToolActivity, Tool: &sessions.ToolActivity{CallID: "c1", Name: "lookup", Result: "r"}},
		{TurnID: "turn-1", Kind: sessions.KindAssistantMessage, Text: "a"},
		{TurnID: "turn-1", Kind: sessions.KindTurnCompleted, Usage: &sessions.Usage{}},
	}))

	// A limit of 2 splits the turn mid-way; the pieces stay in order and
	// reassemble without duplication.
	head, err := svc.EventsPage(ctx, sess.ID, 0, 2)
	require.NoError(t, err)
	require.Equal(t, []int64{3, 4}, seqsOf(head))

	tail, err := svc.EventsPage(ctx, sess.ID, head[0].Seq, 2)
	require.NoError(t, err)
	require.Equal(t, []int64{1, 2}, seqsOf(tail))

	for i, ev := range append(append([]sessions.Event{}, tail...), head...) {
		require.Equal(t, int64(i+1), ev.Seq)
		require.Equal(t, "turn-1", ev.TurnID, "the turn renders as one continuous entry")
	}
}

// TestService_DeleteRemovesSessionAndTranscript pins the delete contract
// (spec chat-view): the session, its events, and its checkpoint disappear
// together; other sessions are untouched; an unknown session is a safe
// no-op.
func TestService_DeleteRemovesSessionAndTranscript(t *testing.T) {
	ctx := t.Context()
	svc, _ := newService(t)

	agent := "agent-1"
	sess, err := svc.CreateSession(ctx, agent, "")
	require.NoError(t, err)
	seedLog(t, svc, sess.ID, 3)
	require.NoError(t, svc.SaveCheckpoint(ctx, sess.ID, sessions.Checkpoint{Summary: "s", UpToSeq: 2}))

	other, err := svc.CreateSession(ctx, agent, "")
	require.NoError(t, err)
	seedLog(t, svc, other.ID, 1)

	require.NoError(t, svc.Delete(ctx, sess.ID))

	_, err = svc.Session(ctx, sess.ID)
	require.ErrorIs(t, err, errNotFound)

	events, err := svc.Events(ctx, sess.ID)
	require.NoError(t, err)
	require.Empty(t, events, "the transcript is gone with the session")

	_, ok, err := svc.Checkpoint(ctx, sess.ID)
	require.NoError(t, err)
	require.False(t, ok, "the compaction checkpoint is gone with the session")

	// The agent's other sessions are untouched.
	kept, err := svc.SessionsByAgent(ctx, agent)
	require.NoError(t, err)
	require.Len(t, kept, 1)
	require.Equal(t, other.ID, kept[0].ID)
	keptEvents, err := svc.Events(ctx, other.ID)
	require.NoError(t, err)
	require.Len(t, keptEvents, 1)

	// Deleting the already-deleted (or never-known) session is a safe no-op.
	require.NoError(t, svc.Delete(ctx, sess.ID))
	require.NoError(t, svc.Delete(ctx, "no-such-session"))
}

// TestService_PayloadRoundTrip_AttachmentsAndVariants pins the new payload
// fields (tasks 5.3/7.1): attachment metadata and the variant_of marker
// persist with the event and replay verbatim.
func TestService_PayloadRoundTrip_AttachmentsAndVariants(t *testing.T) {
	ctx := t.Context()
	svc, _ := newService(t)

	sess, err := svc.CreateSession(ctx, "agent-1", "")
	require.NoError(t, err)

	sent := []sessions.Event{
		{
			TurnID: "turn-1", Kind: sessions.KindUserMessage, Text: "look",
			Attachments: []sessions.Attachment{
				{Key: "0123456789abcdef0123456789abcdef", Name: "shot.png", Mime: "image/png", Size: 4},
			},
		},
		{
			TurnID: "turn-2", Kind: sessions.KindAssistantMessage, Text: "alternative",
			VariantOf: "turn-1",
		},
	}
	require.NoError(t, svc.Append(ctx, sess.ID, sent))

	events, err := svc.Events(ctx, sess.ID)
	require.NoError(t, err)
	require.Len(t, events, 2)
	require.Equal(t, sent[0].Attachments, events[0].Attachments, "attachment metadata round-trips")
	require.Equal(t, "turn-1", events[1].VariantOf, "the variant marker round-trips")
}

// seqsOf maps a page to its sequence numbers, for readable order assertions.
func seqsOf(events []sessions.Event) []int64 {
	out := make([]int64, 0, len(events))
	for _, ev := range events {
		out = append(out, ev.Seq)
	}
	return out
}

// errNotFound is the store's absent-row sentinel, aliased for readable
// assertions.
var errNotFound = store.ErrNotFound
