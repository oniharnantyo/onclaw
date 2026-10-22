package sqlite

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/store"
)

// The transcript-windowing and session-delete adapter tests (tasks 6.1/6.5):
// the SQL paging pushes the window into the query (ORDER BY seq DESC LIMIT,
// replayed ascending) and the delete removes rows without FK enforcement on
// the event log.

// seedEvents appends n single-event batches to the session.
func seedEvents(t *testing.T, s store.Store, sessionID string, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		id, err := newID()
		require.NoError(t, err)
		require.NoError(t, s.SessionEvents().AppendEvents(t.Context(), sessionID, []store.SessionEvent{
			{EventID: id, Kind: "user_message", Payload: []byte(`{}`)},
		}))
	}
}

// TestSessionEvents_PagingOrderAndBoundaries pins the adapter's window
// semantics: newest page ascending, backward pages strictly below the
// boundary, walk to the beginning, empty (not nil) past it.
func TestSessionEvents_PagingOrderAndBoundaries(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	sess := &store.Session{AgentID: "agent-1"}
	require.NoError(t, s.Sessions().Create(ctx, sess))
	seedEvents(t, s, sess.ID, 10)

	page, err := s.SessionEvents().LoadEventsPage(ctx, sess.ID, 0, 4)
	require.NoError(t, err)
	require.Equal(t, []int64{7, 8, 9, 10}, seqsOfRows(page))

	older, err := s.SessionEvents().LoadEventsPage(ctx, sess.ID, 7, 4)
	require.NoError(t, err)
	require.Equal(t, []int64{3, 4, 5, 6}, seqsOfRows(older))

	tail, err := s.SessionEvents().LoadEventsPage(ctx, sess.ID, 3, 4)
	require.NoError(t, err)
	require.Equal(t, []int64{1, 2}, seqsOfRows(tail))

	past, err := s.SessionEvents().LoadEventsPage(ctx, sess.ID, 1, 4)
	require.NoError(t, err)
	require.NotNil(t, past)
	require.Empty(t, past)

	// A page boundary inside a turn keeps replay order across pages.
	mid, err := s.SessionEvents().LoadEventsPage(ctx, sess.ID, 0, 2)
	require.NoError(t, err)
	require.Equal(t, []int64{9, 10}, seqsOfRows(mid))
	rest, err := s.SessionEvents().LoadEventsPage(ctx, sess.ID, 9, 2)
	require.NoError(t, err)
	require.Equal(t, []int64{7, 8}, seqsOfRows(rest))

	// Unknown sessions page empty.
	none, err := s.SessionEvents().LoadEventsPage(ctx, "nope", 0, 4)
	require.NoError(t, err)
	require.NotNil(t, none)
	require.Empty(t, none)
}

// TestSessions_DeleteRemovesRowAndEvents pins the adapter delete: the
// session row goes, DeleteAll clears its event log, both are idempotent, and
// other sessions survive.
func TestSessions_DeleteRemovesRowAndEvents(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	gone := &store.Session{AgentID: "agent-1"}
	require.NoError(t, s.Sessions().Create(ctx, gone))
	seedEvents(t, s, gone.ID, 3)

	kept := &store.Session{AgentID: "agent-1"}
	require.NoError(t, s.Sessions().Create(ctx, kept))
	seedEvents(t, s, kept.ID, 1)

	require.NoError(t, s.SessionEvents().DeleteAll(ctx, gone.ID))
	require.NoError(t, s.Sessions().Delete(ctx, gone.ID))

	_, err := s.Sessions().ByID(ctx, gone.ID)
	require.ErrorIs(t, err, store.ErrNotFound)

	log, err := s.SessionEvents().LoadEvents(ctx, gone.ID)
	require.NoError(t, err)
	require.Empty(t, log)

	// Idempotent: deleting again is a no-op.
	require.NoError(t, s.SessionEvents().DeleteAll(ctx, gone.ID))
	require.NoError(t, s.Sessions().Delete(ctx, gone.ID))

	keptList, err := s.Sessions().ListByAgent(ctx, "agent-1")
	require.NoError(t, err)
	require.Len(t, keptList, 1)
	require.Equal(t, kept.ID, keptList[0].ID)
	keptLog, err := s.SessionEvents().LoadEvents(ctx, kept.ID)
	require.NoError(t, err)
	require.Len(t, keptLog, 1)
}

// seqsOfRows maps raw rows to their sequences.
func seqsOfRows(rows []store.SessionEvent) []int64 {
	out := make([]int64, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Seq)
	}
	return out
}
