package sessions_test

import (
	"context"
	"testing"

	"github.com/oniharnantyo/buddies/internal/sessions"
	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/oniharnantyo/buddies/internal/store/fake"
	"github.com/stretchr/testify/require"
)

// newService wires a sessions.Service against a fresh in-memory store.
func newService(t *testing.T) (sessions.Service, *fake.Store) {
	t.Helper()
	fs := fake.New()
	return sessions.NewService(fs), fs
}

// TestService_SessionLifecyclePerAgent covers 9.1's store seam: sessions are
// created per agent and listed by latest activity.
func TestService_SessionLifecyclePerAgent(t *testing.T) {
	ctx := context.Background()
	svc, _ := newService(t)

	first, err := svc.CreateSession(ctx, "agent-1", "")
	require.NoError(t, err)
	require.NotEmpty(t, first.ID)
	require.Equal(t, "agent-1", first.AgentID)

	second, err := svc.CreateSession(ctx, "agent-1", "titled")
	require.NoError(t, err)

	_, err = svc.CreateSession(ctx, "agent-2", "")
	require.NoError(t, err)

	got, err := svc.Session(ctx, first.ID)
	require.NoError(t, err)
	require.Equal(t, first.ID, got.ID)

	_, err = svc.Session(ctx, "nope")
	require.ErrorIs(t, err, store.ErrNotFound)

	// Appending activity to the older session lifts it to the top.
	require.NoError(t, svc.Append(ctx, first.ID, []sessions.Event{{Kind: sessions.KindUserMessage, Text: "hi"}}))

	list, err := svc.SessionsByAgent(ctx, "agent-1")
	require.NoError(t, err)
	require.Len(t, list, 2)
	require.Equal(t, first.ID, list[0].ID, "latest activity first")
	require.Equal(t, second.ID, list[1].ID)

	// Auto-titling: the runtime renames a session from its first message.
	require.NoError(t, svc.SetTitle(ctx, first.ID, "hi"))
	got, err = svc.Session(ctx, first.ID)
	require.NoError(t, err)
	require.Equal(t, "hi", got.Title)

	none, err := svc.SessionsByAgent(ctx, "agent-none")
	require.NoError(t, err)
	require.NotNil(t, none)
	require.Empty(t, none)
}

// TestService_EventsRoundTrip is the transcript contract: events appended in
// order come back decoded, in log order, with store-assigned ids and
// sequences, one row per kind's payload shape.
func TestService_EventsRoundTrip(t *testing.T) {
	ctx := context.Background()
	svc, _ := newService(t)

	sess, err := svc.CreateSession(ctx, "agent-1", "")
	require.NoError(t, err)

	turn1 := "turn-1"
	batch := []sessions.Event{
		{TurnID: turn1, Kind: sessions.KindUserMessage, Text: "hello"},
		{TurnID: turn1, Kind: sessions.KindTextDelta, Text: "he"},
		{TurnID: turn1, Kind: sessions.KindTextDelta, Text: "llo"},
		{TurnID: turn1, Kind: sessions.KindReasoning, Text: "thinking"},
		{TurnID: turn1, Kind: sessions.KindAssistantMessage, Text: "hello there"},
		{TurnID: turn1, Kind: sessions.KindToolActivity, Tool: &sessions.ToolActivity{
			CallID: "call-1", Name: "lookup", Arguments: `{"q":"x"}`, Result: "found",
		}},
		{TurnID: turn1, Kind: sessions.KindTurnCompleted, Usage: &sessions.Usage{
			InputTokens: 10, OutputTokens: 5, TotalTokens: 15, FinalInputTokens: 10,
		}},
	}
	require.NoError(t, svc.Append(ctx, sess.ID, batch))

	turn2 := "turn-2"
	require.NoError(t, svc.Append(ctx, sess.ID, []sessions.Event{
		{TurnID: turn2, Kind: sessions.KindTurnError, Error: "provider unreachable"},
	}))

	events, err := svc.Events(ctx, sess.ID)
	require.NoError(t, err)
	require.Len(t, events, len(batch)+1)

	// Identity and order: ids assigned, sequences 1..n in append order.
	seen := map[string]bool{}
	for i, ev := range events {
		require.NotEmpty(t, ev.ID, "Append must assign event ids")
		require.False(t, seen[ev.ID], "event ids must be unique")
		seen[ev.ID] = true
		require.Equal(t, int64(i+1), ev.Seq, "sequences must follow append order")
		require.False(t, ev.OccurredAt.IsZero())
	}

	// Payload fidelity per kind, spot-checked.
	require.Equal(t, sessions.KindUserMessage, events[0].Kind)
	require.Equal(t, "hello", events[0].Text)

	require.Equal(t, sessions.KindTextDelta, events[1].Kind)
	require.Equal(t, "he", events[1].Text)

	require.Equal(t, sessions.KindReasoning, events[3].Kind)
	require.Equal(t, "thinking", events[3].Text)

	require.Equal(t, sessions.KindAssistantMessage, events[4].Kind)
	require.Equal(t, "hello there", events[4].Text)

	require.Equal(t, sessions.KindToolActivity, events[5].Kind)
	require.NotNil(t, events[5].Tool)
	require.Equal(t, "lookup", events[5].Tool.Name)
	require.Equal(t, `{"q":"x"}`, events[5].Tool.Arguments)

	require.Equal(t, sessions.KindTurnCompleted, events[6].Kind)
	require.NotNil(t, events[6].Usage)
	require.Equal(t, 15, events[6].Usage.TotalTokens)
	require.Equal(t, turn1, events[6].TurnID)

	require.Equal(t, sessions.KindTurnError, events[7].Kind)
	require.Equal(t, "provider unreachable", events[7].Error)
	require.Equal(t, turn2, events[7].TurnID)

	// Unknown sessions replay empty, not nil.
	empty, err := svc.Events(ctx, "nope")
	require.NoError(t, err)
	require.NotNil(t, empty)
	require.Empty(t, empty)

	// Appending nothing is a no-op that still must not error.
	require.NoError(t, svc.Append(ctx, sess.ID, nil))
}

// TestService_CheckpointPersistence covers the compaction checkpoint
// contract: absent reads, create-or-replace, and session binding.
func TestService_CheckpointPersistence(t *testing.T) {
	ctx := context.Background()
	svc, _ := newService(t)

	sess, err := svc.CreateSession(ctx, "agent-1", "")
	require.NoError(t, err)

	cp, ok, err := svc.Checkpoint(ctx, sess.ID)
	require.NoError(t, err)
	require.False(t, ok)
	require.Zero(t, cp)

	first := sessions.Checkpoint{Summary: "older talk", UpToSeq: 3, TokensBefore: 100, TokensAfter: 20}
	require.NoError(t, svc.SaveCheckpoint(ctx, sess.ID, first))

	got, ok, err := svc.Checkpoint(ctx, sess.ID)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, first.Summary, got.Summary)
	require.Equal(t, int64(3), got.UpToSeq)
	require.Equal(t, sess.ID, got.SessionID)
	require.NotEmpty(t, got.ID, "the checkpoint id is deterministic per session")
	require.False(t, got.CreatedAt.IsZero())

	// A second compaction supersedes the first.
	require.NoError(t, svc.SaveCheckpoint(ctx, sess.ID, sessions.Checkpoint{
		Summary: "older and newer talk", UpToSeq: 9,
	}))
	got, ok, err = svc.Checkpoint(ctx, sess.ID)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "older and newer talk", got.Summary)
	require.Equal(t, int64(9), got.UpToSeq)

	// Checkpoints are session-scoped: another session has none.
	other, err := svc.CreateSession(ctx, "agent-1", "")
	require.NoError(t, err)
	_, ok, err = svc.Checkpoint(ctx, other.ID)
	require.NoError(t, err)
	require.False(t, ok)
}
