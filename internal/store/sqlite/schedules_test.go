package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/stretchr/testify/require"
)

// seedScheduleAgent creates one parent agents row, satisfying the schedules
// table's foreign key.
func seedScheduleAgent(t *testing.T, ctx context.Context, s *Store) *store.Agent {
	t.Helper()
	agent := &store.Agent{Name: "scheduler-agent"}
	require.NoError(t, s.Agents().Create(ctx, agent))
	return agent
}

// atUTC builds a fixed UTC time for test seeds.
func atUTC(minute int) time.Time {
	return time.Date(2026, 10, 21, 9, minute, 0, 0, time.UTC)
}

// TestSchedules_CreateReadRoundTrip covers the full column round trip with
// both the nil and the non-nil nullable times.
func TestSchedules_CreateReadRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	agent := seedScheduleAgent(t, ctx, s)

	next := atUTC(30)
	in := &store.Schedule{
		Name:             "morning report",
		AgentID:          agent.ID,
		Prompt:           "summarize the news",
		Cron:             "30 9 * * *",
		Enabled:          true,
		NextFireAt:       &next,
		LastRunStatus:    store.ScheduleRunStatusRan,
		LastRunError:     "",
		LastRunSessionID: "sess-1",
	}
	require.NoError(t, s.Schedules().Create(ctx, in))
	require.NotEmpty(t, in.ID, "Create must assign the ID")
	require.False(t, in.CreatedAt.IsZero(), "Create must stamp CreatedAt")
	require.False(t, in.UpdatedAt.IsZero(), "Create must stamp UpdatedAt")

	got, err := s.Schedules().ByID(ctx, in.ID)
	require.NoError(t, err)
	require.Equal(t, in.ID, got.ID)
	require.Equal(t, "morning report", got.Name)
	require.Equal(t, agent.ID, got.AgentID)
	require.Equal(t, "summarize the news", got.Prompt)
	require.Equal(t, "30 9 * * *", got.Cron)
	require.True(t, got.Enabled)
	require.NotNil(t, got.NextFireAt)
	require.True(t, got.NextFireAt.Equal(next), "NextFireAt must round-trip exactly")
	require.Nil(t, got.LastRunAt, "LastRunAt stays nil when never fired")
	require.Equal(t, store.ScheduleRunStatusRan, got.LastRunStatus)
	require.Empty(t, got.LastRunError)
	require.Equal(t, "sess-1", got.LastRunSessionID)
	require.True(t, got.CreatedAt.Equal(in.CreatedAt))
	require.True(t, got.UpdatedAt.Equal(in.UpdatedAt))

	// A schedule with every optional field empty reads back nil/empty.
	plain := &store.Schedule{Name: "plain", AgentID: agent.ID, Prompt: "hi", Cron: "* * * * *"}
	require.NoError(t, s.Schedules().Create(ctx, plain))
	got, err = s.Schedules().ByID(ctx, plain.ID)
	require.NoError(t, err)
	require.Nil(t, got.NextFireAt)
	require.Nil(t, got.LastRunAt)
	require.Empty(t, got.LastRunStatus)
}

// TestSchedules_NotFound covers the sentinel on every addressing operation:
// ByID, ByName, Update, Delete, and RecordFire on an absent row.
func TestSchedules_NotFound(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	schedules := s.Schedules()

	_, err := schedules.ByID(ctx, "missing")
	require.ErrorIs(t, err, store.ErrNotFound)
	_, err = schedules.ByName(ctx, "missing")
	require.ErrorIs(t, err, store.ErrNotFound)
	require.ErrorIs(t, schedules.Update(ctx, &store.Schedule{ID: "missing", Name: "x"}), store.ErrNotFound)
	require.ErrorIs(t, schedules.Delete(ctx, "missing"), store.ErrNotFound)
	require.ErrorIs(t, schedules.RecordFire(ctx, "missing", store.ScheduleFire{At: atUTC(0)}), store.ErrNotFound)
}

// TestSchedules_DuplicateNameCaseInsensitive covers the NOCASE unique index:
// a create or an update-to-existing rejects a name differing only in case.
func TestSchedules_DuplicateNameCaseInsensitive(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	agent := seedScheduleAgent(t, ctx, s)
	schedules := s.Schedules()

	first := &store.Schedule{Name: "Pay Invoice", AgentID: agent.ID, Prompt: "p", Cron: "* * * * *"}
	require.NoError(t, schedules.Create(ctx, first))

	dup := &store.Schedule{Name: "pay invoice", AgentID: agent.ID, Prompt: "p", Cron: "* * * * *"}
	err := schedules.Create(ctx, dup)
	require.ErrorIs(t, err, store.ErrDuplicate, "the NOCASE unique index must reject case variants")

	// An update may keep the row's own name but not take another's.
	first.Name = "PAY INVOICE"
	require.NoError(t, schedules.Update(ctx, first), "renaming to a case variant of itself is not a collision")
	first.Name = "pay invoice"
	require.NoError(t, schedules.Update(ctx, first), "the row's own name in another case must not collide with itself")

	second := &store.Schedule{Name: "other", AgentID: agent.ID, Prompt: "p", Cron: "* * * * *"}
	require.NoError(t, schedules.Create(ctx, second))
	second.Name = "PAY INVOICE"
	err = schedules.Update(ctx, second)
	require.ErrorIs(t, err, store.ErrDuplicate, "an update onto another schedule's name must be rejected")
}

// TestSchedules_ByNameCaseInsensitive covers the NOCASE lookup.
func TestSchedules_ByNameCaseInsensitive(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	agent := seedScheduleAgent(t, ctx, s)

	in := &store.Schedule{Name: "Morning Report", AgentID: agent.ID, Prompt: "p", Cron: "* * * * *"}
	require.NoError(t, s.Schedules().Create(ctx, in))

	got, err := s.Schedules().ByName(ctx, "morning report")
	require.NoError(t, err)
	require.Equal(t, in.ID, got.ID)
}

// TestSchedules_ListOrdersByNameCaseInsensitive covers the NOCASE ordering.
func TestSchedules_ListOrdersByNameCaseInsensitive(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	agent := seedScheduleAgent(t, ctx, s)
	schedules := s.Schedules()

	for _, name := range []string{"zeta", "alpha", "Midway"} {
		require.NoError(t, schedules.Create(ctx, &store.Schedule{Name: name, AgentID: agent.ID, Prompt: "p", Cron: "* * * * *"}))
	}

	list, err := schedules.List(ctx)
	require.NoError(t, err)
	names := make([]string, 0, len(list))
	for _, sched := range list {
		names = append(names, sched.Name)
	}
	require.Equal(t, []string{"alpha", "Midway", "zeta"}, names, "List must sort by name, case-insensitive")
}

// TestSchedules_CascadeOnAgentDelete covers migration 000013's foreign key:
// deleting the agents row removes the agent's schedules with it.
func TestSchedules_CascadeOnAgentDelete(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	agent := &store.Agent{Name: "doomed"}
	require.NoError(t, s.Agents().Create(ctx, agent))
	survivor := seedScheduleAgent(t, ctx, s)

	kept := &store.Schedule{Name: "kept", AgentID: survivor.ID, Prompt: "p", Cron: "* * * * *"}
	require.NoError(t, s.Schedules().Create(ctx, kept))
	gone := &store.Schedule{Name: "gone", AgentID: agent.ID, Prompt: "p", Cron: "* * * * *"}
	require.NoError(t, s.Schedules().Create(ctx, gone))

	require.NoError(t, s.Agents().Delete(ctx, agent.ID))

	_, err := s.Schedules().ByID(ctx, gone.ID)
	require.ErrorIs(t, err, store.ErrNotFound, "deleting the agent must cascade its schedule away")
	_, err = s.Schedules().ByID(ctx, kept.ID)
	require.NoError(t, err, "another agent's schedule must survive the cascade")
}

// TestSchedules_ListDue covers the due filter: an enabled schedule holding a
// slot at or before now is due; a future slot, a disabled schedule, and a
// null slot are not.
func TestSchedules_ListDue(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	agent := seedScheduleAgent(t, ctx, s)
	schedules := s.Schedules()

	now := atUTC(0)
	past := atUTC(-1)
	past2 := atUTC(-2)
	future := atUTC(1)

	due := &store.Schedule{Name: "due", AgentID: agent.ID, Prompt: "p", Cron: "* * * * *", Enabled: true, NextFireAt: &past}
	require.NoError(t, schedules.Create(ctx, due))
	earlier := &store.Schedule{Name: "earlier", AgentID: agent.ID, Prompt: "p", Cron: "* * * * *", Enabled: true, NextFireAt: &past2}
	require.NoError(t, schedules.Create(ctx, earlier))
	late := &store.Schedule{Name: "late", AgentID: agent.ID, Prompt: "p", Cron: "* * * * *", Enabled: true, NextFireAt: &future}
	require.NoError(t, schedules.Create(ctx, late))
	disabled := &store.Schedule{Name: "disabled", AgentID: agent.ID, Prompt: "p", Cron: "* * * * *", Enabled: false, NextFireAt: &past}
	require.NoError(t, schedules.Create(ctx, disabled))
	unset := &store.Schedule{Name: "unset", AgentID: agent.ID, Prompt: "p", Cron: "* * * * *", Enabled: true}
	require.NoError(t, schedules.Create(ctx, unset))

	got, err := schedules.ListDue(ctx, now)
	require.NoError(t, err)
	require.Len(t, got, 2, "only the enabled schedules holding a slot at or before now are due")
	require.Equal(t, earlier.ID, got[0].ID, "due schedules must be ordered by next_fire_at")
	require.Equal(t, due.ID, got[1].ID)

	// The boundary is inclusive: a slot exactly at now is due. At now =
	// past2, `earlier` (whose slot equals now) is due and `due` (a minute
	// later) is not.
	got, err = schedules.ListDue(ctx, past2)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, earlier.ID, got[0].ID)
}

// TestSchedules_RecordFire covers the one-UPDATE mark-and-advance: every
// last-run field lands, a nil fire.NextFireAt clears the stored slot, and a
// fired schedule leaves the due set.
func TestSchedules_RecordFire(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	agent := seedScheduleAgent(t, ctx, s)
	schedules := s.Schedules()

	slot := atUTC(0)
	advanced := atUTC(1)
	in := &store.Schedule{Name: "fired", AgentID: agent.ID, Prompt: "p", Cron: "* * * * *", Enabled: true, NextFireAt: &slot}
	require.NoError(t, schedules.Create(ctx, in))

	require.NoError(t, schedules.RecordFire(ctx, in.ID, store.ScheduleFire{
		At:         atUTC(30),
		Status:     store.ScheduleRunStatusRan,
		SessionID:  "sess-42",
		NextFireAt: &advanced,
	}))

	got, err := schedules.ByID(ctx, in.ID)
	require.NoError(t, err)
	require.NotNil(t, got.LastRunAt)
	require.True(t, got.LastRunAt.Equal(atUTC(30)), "RecordFire must stamp LastRunAt")
	require.Equal(t, store.ScheduleRunStatusRan, got.LastRunStatus)
	require.Equal(t, "sess-42", got.LastRunSessionID)
	require.NotNil(t, got.NextFireAt)
	require.True(t, got.NextFireAt.Equal(advanced), "RecordFire must advance NextFireAt")
	require.True(t, got.UpdatedAt.After(in.UpdatedAt) || got.UpdatedAt.Equal(in.UpdatedAt))

	// A nil NextFireAt clears the stored slot (the scheduler's pause/miss path).
	require.NoError(t, schedules.RecordFire(ctx, in.ID, store.ScheduleFire{
		At:     atUTC(31),
		Status: store.ScheduleRunStatusFailed,
		Error:  "agent gone",
	}))
	got, err = schedules.ByID(ctx, in.ID)
	require.NoError(t, err)
	require.NotNil(t, got.LastRunAt)
	require.True(t, got.LastRunAt.Equal(atUTC(31)))
	require.Equal(t, store.ScheduleRunStatusFailed, got.LastRunStatus)
	require.Equal(t, "agent gone", got.LastRunError)
	require.Empty(t, got.LastRunSessionID)
	require.Nil(t, got.NextFireAt, "a nil fire.NextFireAt must clear the slot")

	dueList, err := schedules.ListDue(ctx, atUTC(120))
	require.NoError(t, err)
	require.Empty(t, dueList, "a schedule with a cleared slot is not due")
}

// TestSchedules_UpdateRewritesDefinitionAndKeepsLastRun covers Update's
// field set: the definition columns move, the last-run record and CreatedAt
// do not.
func TestSchedules_UpdateRewritesDefinitionAndKeepsLastRun(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	agent := seedScheduleAgent(t, ctx, s)
	schedules := s.Schedules()

	slot := atUTC(0)
	in := &store.Schedule{Name: "before", AgentID: agent.ID, Prompt: "old", Cron: "* * * * *", Enabled: true, NextFireAt: &slot}
	require.NoError(t, schedules.Create(ctx, in))
	fired := atUTC(10)
	require.NoError(t, schedules.RecordFire(ctx, in.ID, store.ScheduleFire{At: fired, Status: store.ScheduleRunStatusRan}))

	in.Name = "after"
	in.Prompt = "new"
	in.Cron = "0 12 * * *"
	in.Enabled = false
	in.NextFireAt = nil
	require.NoError(t, schedules.Update(ctx, in))

	got, err := schedules.ByID(ctx, in.ID)
	require.NoError(t, err)
	require.Equal(t, "after", got.Name)
	require.Equal(t, "new", got.Prompt)
	require.Equal(t, "0 12 * * *", got.Cron)
	require.False(t, got.Enabled)
	require.Nil(t, got.NextFireAt, "Update must write the nullable slot")
	require.NotNil(t, got.LastRunAt, "Update must not touch the last-run record")
	require.True(t, got.LastRunAt.Equal(fired))
	require.Equal(t, store.ScheduleRunStatusRan, got.LastRunStatus)
	require.True(t, got.CreatedAt.Equal(in.CreatedAt), "Update must not touch CreatedAt")
}

// TestSchedules_VisibleThroughTx covers the tx-scoped schedule store: a
// create inside WithTx commits with the transaction.
func TestSchedules_VisibleThroughTx(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	agent := seedScheduleAgent(t, ctx, s)

	err := s.WithTx(ctx, func(tx store.Store) error {
		return tx.Schedules().Create(ctx, &store.Schedule{Name: "in-tx", AgentID: agent.ID, Prompt: "p", Cron: "* * * * *"})
	})
	require.NoError(t, err)

	got, err := s.Schedules().ByName(ctx, "in-tx")
	require.NoError(t, err)
	require.Equal(t, "in-tx", got.Name)
}
