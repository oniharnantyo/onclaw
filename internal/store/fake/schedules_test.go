package fake_test

import (
	"context"
	"testing"
	"time"

	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/stretchr/testify/require"
)

// fakeAtUTC builds a fixed UTC time for test seeds.
func fakeAtUTC(minute int) time.Time {
	return time.Date(2026, 10, 21, 9, minute, 0, 0, time.UTC)
}

// TestSchedules_Fake_CRUDAndNotFound mirrors the sqlite adapter's lifecycle
// over the fake: create stamps and assigns, reads copy (no aliasing), update
// rewrites the definition, and every absent-row path is the sentinel.
func TestSchedules_Fake_CRUDAndNotFound(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	agent := &store.Agent{Name: "atlas"}
	require.NoError(t, s.Agents().Create(ctx, agent))

	next := fakeAtUTC(30)
	in := &store.Schedule{
		Name:          "morning report",
		AgentID:       agent.ID,
		Prompt:        "summarize the news",
		Cron:          "30 9 * * *",
		Enabled:       true,
		NextFireAt:    &next,
		LastRunStatus: store.ScheduleRunStatusRan,
	}
	require.NoError(t, s.Schedules().Create(ctx, in))
	require.NotEmpty(t, in.ID, "Create must assign the ID")
	require.False(t, in.CreatedAt.IsZero(), "Create must stamp CreatedAt")
	require.False(t, in.UpdatedAt.IsZero(), "Create must stamp UpdatedAt")

	got, err := s.Schedules().ByID(ctx, in.ID)
	require.NoError(t, err)
	require.Equal(t, "morning report", got.Name)
	require.Equal(t, agent.ID, got.AgentID)
	require.Equal(t, "30 9 * * *", got.Cron)
	require.True(t, got.Enabled)
	require.NotNil(t, got.NextFireAt)
	require.True(t, got.NextFireAt.Equal(next), "NextFireAt must round-trip exactly")
	require.Nil(t, got.LastRunAt)
	require.Equal(t, store.ScheduleRunStatusRan, got.LastRunStatus)
	require.True(t, got.CreatedAt.Equal(in.CreatedAt))
	require.True(t, got.UpdatedAt.Equal(in.UpdatedAt))

	// Mutating a returned schedule must not alias the fake's state.
	*got.NextFireAt = fakeAtUTC(59)
	again, err := s.Schedules().ByID(ctx, in.ID)
	require.NoError(t, err)
	require.True(t, again.NextFireAt.Equal(next), "returned schedules must be copies")

	// List orders by name, case-insensitive.
	require.NoError(t, s.Schedules().Create(ctx, &store.Schedule{Name: "zeta", AgentID: agent.ID, Prompt: "p", Cron: "* * * * *"}))
	require.NoError(t, s.Schedules().Create(ctx, &store.Schedule{Name: "Midway", AgentID: agent.ID, Prompt: "p", Cron: "* * * * *"}))
	list, err := s.Schedules().List(ctx)
	require.NoError(t, err)
	names := make([]string, 0, len(list))
	for _, sched := range list {
		names = append(names, sched.Name)
	}
	require.Equal(t, []string{"Midway", "morning report", "zeta"}, names)

	// Update rewrites the definition and stamps UpdatedAt.
	in.Name = "renamed"
	in.Prompt = "new prompt"
	in.Cron = "0 12 * * *"
	in.Enabled = false
	in.NextFireAt = nil
	require.NoError(t, s.Schedules().Update(ctx, in))
	got, err = s.Schedules().ByID(ctx, in.ID)
	require.NoError(t, err)
	require.Equal(t, "renamed", got.Name)
	require.Equal(t, "new prompt", got.Prompt)
	require.Equal(t, "0 12 * * *", got.Cron)
	require.False(t, got.Enabled)
	require.Nil(t, got.NextFireAt)
	require.True(t, got.CreatedAt.Equal(in.CreatedAt), "Update must not touch CreatedAt")

	// Absent paths are the sentinel.
	_, err = s.Schedules().ByID(ctx, "missing")
	require.ErrorIs(t, err, store.ErrNotFound)
	_, err = s.Schedules().ByName(ctx, "missing")
	require.ErrorIs(t, err, store.ErrNotFound)
	require.ErrorIs(t, s.Schedules().Update(ctx, &store.Schedule{ID: "missing", Name: "x"}), store.ErrNotFound)
	require.ErrorIs(t, s.Schedules().Delete(ctx, "missing"), store.ErrNotFound)
	require.ErrorIs(t, s.Schedules().RecordFire(ctx, "missing", store.ScheduleFire{At: fakeAtUTC(0)}), store.ErrNotFound)

	require.NoError(t, s.Schedules().Delete(ctx, in.ID))
	_, err = s.Schedules().ByID(ctx, in.ID)
	require.ErrorIs(t, err, store.ErrNotFound)
}

// TestSchedules_Fake_DuplicateNameCaseInsensitive mirrors the sqlite unique
// index: create and update-to-existing reject case-insensitive duplicates.
func TestSchedules_Fake_DuplicateNameCaseInsensitive(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	agent := &store.Agent{Name: "atlas"}
	require.NoError(t, s.Agents().Create(ctx, agent))

	first := &store.Schedule{Name: "Pay Invoice", AgentID: agent.ID, Prompt: "p", Cron: "* * * * *"}
	require.NoError(t, s.Schedules().Create(ctx, first))

	err := s.Schedules().Create(ctx, &store.Schedule{Name: "pay invoice", AgentID: agent.ID, Prompt: "p", Cron: "* * * * *"})
	require.ErrorIs(t, err, store.ErrDuplicate)

	// A row may keep its own name in another case.
	first.Name = "PAY INVOICE"
	require.NoError(t, s.Schedules().Update(ctx, first))

	second := &store.Schedule{Name: "other", AgentID: agent.ID, Prompt: "p", Cron: "* * * * *"}
	require.NoError(t, s.Schedules().Create(ctx, second))
	second.Name = "pay invoice"
	require.ErrorIs(t, s.Schedules().Update(ctx, second), store.ErrDuplicate)
}

// TestSchedules_Fake_ByNameCaseInsensitiveAndListDue mirrors the NOCASE
// lookup and the due filter (enabled + slot at or before now, ordered by
// slot).
func TestSchedules_Fake_ByNameCaseInsensitiveAndListDue(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	agent := &store.Agent{Name: "atlas"}
	require.NoError(t, s.Agents().Create(ctx, agent))

	in := &store.Schedule{Name: "Morning Report", AgentID: agent.ID, Prompt: "p", Cron: "* * * * *"}
	require.NoError(t, s.Schedules().Create(ctx, in))
	got, err := s.Schedules().ByName(ctx, "morning report")
	require.NoError(t, err)
	require.Equal(t, in.ID, got.ID)

	now := fakeAtUTC(0)
	past := fakeAtUTC(-1)
	past2 := fakeAtUTC(-2)
	future := fakeAtUTC(1)

	due := &store.Schedule{Name: "due", AgentID: agent.ID, Prompt: "p", Cron: "* * * * *", Enabled: true, NextFireAt: &past}
	require.NoError(t, s.Schedules().Create(ctx, due))
	earlier := &store.Schedule{Name: "earlier", AgentID: agent.ID, Prompt: "p", Cron: "* * * * *", Enabled: true, NextFireAt: &past2}
	require.NoError(t, s.Schedules().Create(ctx, earlier))
	late := &store.Schedule{Name: "late", AgentID: agent.ID, Prompt: "p", Cron: "* * * * *", Enabled: true, NextFireAt: &future}
	require.NoError(t, s.Schedules().Create(ctx, late))
	disabled := &store.Schedule{Name: "disabled", AgentID: agent.ID, Prompt: "p", Cron: "* * * * *", Enabled: false, NextFireAt: &past}
	require.NoError(t, s.Schedules().Create(ctx, disabled))
	unset := &store.Schedule{Name: "unset", AgentID: agent.ID, Prompt: "p", Cron: "* * * * *", Enabled: true}
	require.NoError(t, s.Schedules().Create(ctx, unset))

	gotList, err := s.Schedules().ListDue(ctx, now)
	require.NoError(t, err)
	require.Len(t, gotList, 2, "only enabled schedules holding a slot at or before now are due")
	require.Equal(t, earlier.ID, gotList[0].ID)
	require.Equal(t, due.ID, gotList[1].ID)
}

// TestSchedules_Fake_RecordFire mirrors the one-update mark-and-advance:
// every last-run field lands and a nil NextFireAt clears the slot.
func TestSchedules_Fake_RecordFire(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	agent := &store.Agent{Name: "atlas"}
	require.NoError(t, s.Agents().Create(ctx, agent))

	slot := fakeAtUTC(0)
	advanced := fakeAtUTC(1)
	in := &store.Schedule{Name: "fired", AgentID: agent.ID, Prompt: "p", Cron: "* * * * *", Enabled: true, NextFireAt: &slot}
	require.NoError(t, s.Schedules().Create(ctx, in))

	require.NoError(t, s.Schedules().RecordFire(ctx, in.ID, store.ScheduleFire{
		At:         fakeAtUTC(30),
		Status:     store.ScheduleRunStatusRan,
		SessionID:  "sess-42",
		NextFireAt: &advanced,
	}))
	got, err := s.Schedules().ByID(ctx, in.ID)
	require.NoError(t, err)
	require.NotNil(t, got.LastRunAt)
	require.True(t, got.LastRunAt.Equal(fakeAtUTC(30)))
	require.Equal(t, store.ScheduleRunStatusRan, got.LastRunStatus)
	require.Equal(t, "sess-42", got.LastRunSessionID)
	require.NotNil(t, got.NextFireAt)
	require.True(t, got.NextFireAt.Equal(advanced))

	// A nil NextFireAt clears the slot.
	require.NoError(t, s.Schedules().RecordFire(ctx, in.ID, store.ScheduleFire{
		At:     fakeAtUTC(31),
		Status: store.ScheduleRunStatusFailed,
		Error:  "agent gone",
	}))
	got, err = s.Schedules().ByID(ctx, in.ID)
	require.NoError(t, err)
	require.Equal(t, store.ScheduleRunStatusFailed, got.LastRunStatus)
	require.Equal(t, "agent gone", got.LastRunError)
	require.Nil(t, got.NextFireAt)

	gotList, err := s.Schedules().ListDue(ctx, fakeAtUTC(120))
	require.NoError(t, err)
	require.Empty(t, gotList, "a schedule with a cleared slot is not due")
}

// TestSchedules_Fake_CascadeOnAgentDelete mirrors the sqlite FK cascade:
// deleting the agent removes its schedules and no others.
func TestSchedules_Fake_CascadeOnAgentDelete(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	doomed := &store.Agent{Name: "doomed"}
	require.NoError(t, s.Agents().Create(ctx, doomed))
	survivor := &store.Agent{Name: "survivor"}
	require.NoError(t, s.Agents().Create(ctx, survivor))

	gone := &store.Schedule{Name: "gone", AgentID: doomed.ID, Prompt: "p", Cron: "* * * * *"}
	require.NoError(t, s.Schedules().Create(ctx, gone))
	kept := &store.Schedule{Name: "kept", AgentID: survivor.ID, Prompt: "p", Cron: "* * * * *"}
	require.NoError(t, s.Schedules().Create(ctx, kept))

	require.NoError(t, s.Agents().Delete(ctx, doomed.ID))

	_, err := s.Schedules().ByID(ctx, gone.ID)
	require.ErrorIs(t, err, store.ErrNotFound, "deleting the agent must cascade its schedule away")
	_, err = s.Schedules().ByID(ctx, kept.ID)
	require.NoError(t, err, "another agent's schedule must survive the cascade")
}
