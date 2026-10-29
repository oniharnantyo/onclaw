package fake_test

import (
	"context"
	"testing"
	"time"

	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/oniharnantyo/buddies/internal/store/fake"
	"github.com/stretchr/testify/require"
)

// TestSessions_LifecycleAndOrdering covers the session registry contract:
// store-assigned IDs, per-agent listing ordered by latest activity, touch,
// title updates, and the not-found paths.
func TestSessions_LifecycleAndOrdering(t *testing.T) {
	ctx := context.Background()
	s := fake.New()

	first := &store.Session{AgentID: "agent-1", Title: "First"}
	require.NoError(t, s.Sessions().Create(ctx, first))
	require.NotEmpty(t, first.ID)
	require.False(t, first.CreatedAt.IsZero())
	require.Equal(t, first.CreatedAt, first.LastActivityAt, "activity defaults to creation time")

	second := &store.Session{AgentID: "agent-1"}
	require.NoError(t, s.Sessions().Create(ctx, second))

	other := &store.Session{AgentID: "agent-2"}
	require.NoError(t, s.Sessions().Create(ctx, other))

	// Advance the first session's activity so it sorts ahead of the second.
	later := first.LastActivityAt.Add(2 * time.Minute)
	require.NoError(t, s.Sessions().Touch(ctx, first.ID, later))

	list, err := s.Sessions().ListByAgent(ctx, "agent-1")
	require.NoError(t, err)
	require.Len(t, list, 2)
	require.Equal(t, first.ID, list[0].ID, "most recent activity first")
	require.Equal(t, second.ID, list[1].ID)

	otherList, err := s.Sessions().ListByAgent(ctx, "agent-2")
	require.NoError(t, err)
	require.Len(t, otherList, 1)
	require.Equal(t, other.ID, otherList[0].ID)

	none, err := s.Sessions().ListByAgent(ctx, "agent-none")
	require.NoError(t, err)
	require.NotNil(t, none)
	require.Empty(t, none)

	require.NoError(t, s.Sessions().SetTitle(ctx, second.ID, "Second"))
	got, err := s.Sessions().ByID(ctx, second.ID)
	require.NoError(t, err)
	require.Equal(t, "Second", got.Title)
	require.Equal(t, second.CreatedAt, got.CreatedAt)

	require.ErrorIs(t, s.Sessions().Touch(ctx, "nope", later), store.ErrNotFound)
	require.ErrorIs(t, s.Sessions().SetTitle(ctx, "nope", "x"), store.ErrNotFound)
	_, err = s.Sessions().ByID(ctx, "nope")
	require.ErrorIs(t, err, store.ErrNotFound)
}

// TestSessions_ListAcrossAgents covers the all-sessions listing: one call
// returns every agent's sessions, most recent activity first, and the empty
// result is non-nil.
func TestSessions_ListAcrossAgents(t *testing.T) {
	ctx := context.Background()
	s := fake.New()

	first := &store.Session{AgentID: "agent-1"}
	require.NoError(t, s.Sessions().Create(ctx, first))
	second := &store.Session{AgentID: "agent-2"}
	require.NoError(t, s.Sessions().Create(ctx, second))

	// Advance the second session's activity so it sorts ahead of the first.
	later := first.LastActivityAt.Add(2 * time.Minute)
	require.NoError(t, s.Sessions().Touch(ctx, second.ID, later))

	list, err := s.Sessions().List(ctx)
	require.NoError(t, err)
	require.Len(t, list, 2)
	require.Equal(t, second.ID, list[0].ID, "most recent activity first")
	require.Equal(t, first.ID, list[1].ID)

	require.NoError(t, s.Sessions().Delete(ctx, first.ID))
	require.NoError(t, s.Sessions().Delete(ctx, second.ID))
	list, err = s.Sessions().List(ctx)
	require.NoError(t, err)
	require.NotNil(t, list)
	require.Empty(t, list)
}
