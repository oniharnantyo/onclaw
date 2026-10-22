package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/oniharnantyo/buddies/internal/gateway"
	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// dailyFixture stores the "Standup digest" schedule — daily at 09:00 —
// through the service on Tuesday 2026-10-20 at 09:30, so its next fire is
// 2026-10-21 09:00, and returns the fixture, the view, and that slot.
func dailyFixture(t *testing.T, state gateway.State) (*fixture, ScheduleView, time.Time) {
	t.Helper()
	f := newFixture(t, at(20, 9, 30), state)
	agent := f.seedAgent(t, "agent-1")
	view := f.createSchedule(t, "Standup digest", agent, "Summarize the standup notes", "0 9 * * *", true)
	return f, view, at(21, 9, 0)
}

func TestTickFiresDueWhenRunning(t *testing.T) {
	tests := []struct {
		name string
		now  time.Time // when the tick runs, all within LateTolerance
	}{
		{name: "exactly at the slot", now: at(21, 9, 0)},
		{name: "90 seconds late", now: atSec(21, 9, 1, 30)},
		{name: "exactly at the tolerance", now: atSec(21, 9, 2, 0)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, view, fireAt := dailyFixture(t, gateway.StateRunning)

			var rec recordingListener
			cancel := f.svc.Subscribe(rec.onChange)
			defer cancel()

			require.NoError(t, f.svc.Tick(context.Background(), tt.now))

			// The run started with the exact title and the prompt.
			require.Len(t, f.runs.creates, 1)
			assert.Equal(t, createCall{AgentID: "agent-1", Title: "Standup digest — 2026-10-21 09:00"}, f.runs.creates[0])
			require.Len(t, f.runs.submits, 1)
			assert.Equal(t, submitCall{SessionID: "sess-1", Prompt: "Summarize the standup notes"}, f.runs.submits[0])

			// Last run recorded as ran with the session; slot advanced.
			row := mustRow(t, f.st, view.ID)
			assert.Equal(t, store.ScheduleRunStatusRan, row.LastRunStatus)
			assert.Equal(t, "sess-1", row.LastRunSessionID)
			assert.Empty(t, row.LastRunError)
			requireTimePtr(t, row.LastRunAt, fireAt)
			requireTimePtr(t, row.NextFireAt, at(22, 9, 0))

			// The fire is announced with the created session.
			require.Len(t, rec.changes, 1)
			assert.Equal(t, Change{ScheduleID: view.ID, SessionID: "sess-1", AgentID: "agent-1"}, rec.changes[0])
		})
	}
}

func TestTickMissesWhenPaused(t *testing.T) {
	f, view, fireAt := dailyFixture(t, gateway.StatePaused)

	var rec recordingListener
	cancel := f.svc.Subscribe(rec.onChange)
	defer cancel()

	// Within tolerance, but Paused means skipped, never run late.
	require.NoError(t, f.svc.Tick(context.Background(), atSec(21, 9, 1, 0)))

	assert.Empty(t, f.runs.creates)
	assert.Empty(t, f.runs.submits)

	row := mustRow(t, f.st, view.ID)
	assert.Equal(t, store.ScheduleRunStatusMissed, row.LastRunStatus)
	assert.Empty(t, row.LastRunSessionID)
	requireTimePtr(t, row.LastRunAt, fireAt) // the skipped slot itself
	requireTimePtr(t, row.NextFireAt, at(22, 9, 0))

	require.Len(t, rec.changes, 1)
	assert.Equal(t, Change{ScheduleID: view.ID, AgentID: "agent-1"}, rec.changes[0])
}

func TestTickMissesWhenOverdueBeyondTolerance(t *testing.T) {
	f, view, fireAt := dailyFixture(t, gateway.StateRunning)

	// Three hours past the slot while Running: the process was effectively
	// not ticking — skip, don't run late.
	require.NoError(t, f.svc.Tick(context.Background(), at(21, 12, 0)))

	assert.Empty(t, f.runs.creates)

	row := mustRow(t, f.st, view.ID)
	assert.Equal(t, store.ScheduleRunStatusMissed, row.LastRunStatus)
	// For a daily schedule the most recent missed slot is the stored one.
	requireTimePtr(t, row.LastRunAt, fireAt)
	requireTimePtr(t, row.NextFireAt, at(22, 9, 0))
}

func TestTickMissesJustPastTolerance(t *testing.T) {
	f, view, _ := dailyFixture(t, gateway.StateRunning)

	require.NoError(t, f.svc.Tick(context.Background(), atSec(21, 9, 2, 1)))

	row := mustRow(t, f.st, view.ID)
	assert.Equal(t, store.ScheduleRunStatusMissed, row.LastRunStatus)
	requireTimePtr(t, row.LastRunAt, at(21, 9, 0))
	requireTimePtr(t, row.NextFireAt, at(22, 9, 0))
	assert.Empty(t, f.runs.creates)
}

func TestTickMultiSlotMissRecordsMostRecentSlot(t *testing.T) {
	f := newFixture(t, at(21, 8, 0), gateway.StateRunning)
	agent := f.seedAgent(t, "agent-1")
	view := f.createSchedule(t, "Pulse", agent, "p", "* * * * *", true) // every minute

	// Thirty minutes of downtime: the stored slot is 08:01, now is 08:31 on
	// the boundary — the missed slot recorded is the one at now itself, not
	// the stored one.
	now := at(21, 8, 31)
	require.NoError(t, f.svc.Tick(context.Background(), now))

	row := mustRow(t, f.st, view.ID)
	assert.Equal(t, store.ScheduleRunStatusMissed, row.LastRunStatus)
	requireTimePtr(t, row.LastRunAt, now)
	requireTimePtr(t, row.NextFireAt, at(21, 8, 32))
	assert.Empty(t, f.runs.creates)
}

func TestTickMultiSlotMissOffBoundary(t *testing.T) {
	f := newFixture(t, at(21, 8, 0), gateway.StateRunning)
	agent := f.seedAgent(t, "agent-1")
	view := f.createSchedule(t, "Pulse", agent, "p", "* * * * *", true)

	// Now between two slots: the most recent slot at or before now wins.
	require.NoError(t, f.svc.Tick(context.Background(), atSec(21, 8, 31, 30)))

	row := mustRow(t, f.st, view.ID)
	requireTimePtr(t, row.LastRunAt, at(21, 8, 31))
	requireTimePtr(t, row.NextFireAt, at(21, 8, 32))
}

func TestTickWaitsAtBootWhenStoppedWithinTolerance(t *testing.T) {
	f, view, fireAt := dailyFixture(t, gateway.StateStopped)

	require.NoError(t, f.svc.Tick(context.Background(), atSec(21, 9, 1, 0)))

	// The row is untouched: still due, still unfired, waiting for the
	// gateway to come up.
	assert.Empty(t, f.runs.creates)
	row := mustRow(t, f.st, view.ID)
	assert.Empty(t, row.LastRunStatus)
	assert.Nil(t, row.LastRunAt)
	requireTimePtr(t, row.NextFireAt, fireAt)
}

func TestTickDisabledRowNeverFires(t *testing.T) {
	f := newFixture(t, at(21, 9, 0), gateway.StateRunning)
	agent := f.seedAgent(t, "agent-1")
	slot := at(21, 9, 0)
	// A disabled row with a stored slot could only exist through a store
	// write (the service clears the slot on disable) — seed it directly.
	row := f.seedSchedule(t, store.Schedule{
		Name: "Off", AgentID: agent, Prompt: "p", Cron: "0 9 * * *", Enabled: false, NextFireAt: &slot,
	})

	require.NoError(t, f.svc.Tick(context.Background(), at(21, 9, 30)))

	assert.Empty(t, f.runs.creates)
	stored := mustRow(t, f.st, row.ID)
	assert.False(t, stored.Enabled)
	requireTimePtr(t, stored.NextFireAt, slot)
	assert.Empty(t, stored.LastRunStatus)
}

func TestTickCreateFailureRecordsFailedAndAdvances(t *testing.T) {
	f, view, _ := dailyFixture(t, gateway.StateRunning)
	f.runs.createErr = errAny

	var rec recordingListener
	cancel := f.svc.Subscribe(rec.onChange)
	defer cancel()

	require.NoError(t, f.svc.Tick(context.Background(), at(21, 9, 0)))

	assert.Empty(t, f.runs.submits)
	row := mustRow(t, f.st, view.ID)
	assert.Equal(t, store.ScheduleRunStatusFailed, row.LastRunStatus)
	assert.Equal(t, errAny.Error(), row.LastRunError)
	assert.Empty(t, row.LastRunSessionID)
	// The next fire still advances.
	requireTimePtr(t, row.NextFireAt, at(22, 9, 0))

	require.Len(t, rec.changes, 1)
	assert.Equal(t, view.ID, rec.changes[0].ScheduleID)
	assert.Empty(t, rec.changes[0].SessionID)
}

func TestTickSubmitFailureKeepsSessionID(t *testing.T) {
	f, view, _ := dailyFixture(t, gateway.StateRunning)
	f.runs.submitErr = errAny

	require.NoError(t, f.svc.Tick(context.Background(), at(21, 9, 0)))

	row := mustRow(t, f.st, view.ID)
	assert.Equal(t, store.ScheduleRunStatusFailed, row.LastRunStatus)
	assert.Equal(t, errAny.Error(), row.LastRunError)
	assert.Equal(t, "sess-1", row.LastRunSessionID)
	requireTimePtr(t, row.NextFireAt, at(22, 9, 0))
}

func TestTickAgentGoneRecordsFailed(t *testing.T) {
	f := newFixture(t, at(21, 9, 0), gateway.StateRunning)
	slot := at(21, 9, 0)
	// The agent was deleted but the schedule survived (a race the store's
	// cascade normally closes) — seed the orphan directly.
	row := f.seedSchedule(t, store.Schedule{
		Name: "Orphan", AgentID: "ghost-agent", Prompt: "p", Cron: "0 9 * * *", Enabled: true, NextFireAt: &slot,
	})

	require.NoError(t, f.svc.Tick(context.Background(), at(21, 9, 0)))

	assert.Empty(t, f.runs.creates)
	stored := mustRow(t, f.st, row.ID)
	assert.Equal(t, store.ScheduleRunStatusFailed, stored.LastRunStatus)
	assert.Equal(t, "agent not found", stored.LastRunError)
	requireTimePtr(t, stored.NextFireAt, at(22, 9, 0))
}

func TestTickSelfHealsFutureNextFire(t *testing.T) {
	f, view, _ := dailyFixture(t, gateway.StateRunning)

	// Corrupt the stored slot: still in the future, but wrong (an hour past
	// the true next recurrence).
	stored := mustRow(t, f.st, view.ID)
	wrong := at(21, 10, 0)
	stored.NextFireAt = &wrong
	require.NoError(t, f.st.Schedules().Update(context.Background(), &stored))

	// A tick well before the slot recomputes it from the expression.
	require.NoError(t, f.svc.Tick(context.Background(), at(20, 10, 0)))

	row := mustRow(t, f.st, view.ID)
	requireTimePtr(t, row.NextFireAt, at(21, 9, 0))
	// No run happened; last-run state is untouched.
	assert.Empty(t, f.runs.creates)
	assert.Empty(t, row.LastRunStatus)
}

func TestTickSelfHealLeavesDueAndDisabledRowsAlone(t *testing.T) {
	f, view, fireAt := dailyFixture(t, gateway.StateStopped)

	// The due row (Stopped, within tolerance) is still waiting and must not
	// be pushed forward by the self-heal pass.
	require.NoError(t, f.svc.Tick(context.Background(), atSec(21, 9, 1, 0)))

	row := mustRow(t, f.st, view.ID)
	requireTimePtr(t, row.NextFireAt, fireAt)
	assert.Empty(t, row.LastRunStatus)
}

func TestTickBrokenStoredExpressionIsLoggedNotFatal(t *testing.T) {
	f := newFixture(t, at(21, 9, 0), gateway.StateRunning)
	agent := f.seedAgent(t, "agent-1")
	slot := at(21, 9, 0)
	// A broken expression can only arrive through a direct store write;
	// the tick must skip the row instead of panicking or mis-advancing it.
	row := f.seedSchedule(t, store.Schedule{
		Name: "Broken", AgentID: agent, Prompt: "p", Cron: "not a cron", Enabled: true, NextFireAt: &slot,
	})

	require.NoError(t, f.svc.Tick(context.Background(), at(21, 9, 30)))

	assert.Empty(t, f.runs.creates)
	stored := mustRow(t, f.st, row.ID)
	requireTimePtr(t, stored.NextFireAt, slot)
	assert.Empty(t, stored.LastRunStatus)
}

func TestTickMixedSchedulesAreIndependent(t *testing.T) {
	f := newFixture(t, at(20, 9, 30), gateway.StateRunning)
	agent := f.seedAgent(t, "agent-1")
	firing := f.createSchedule(t, "Firing", agent, "p", "0 9 * * *", true)
	later := f.createSchedule(t, "Later", agent, "p", "30 9 * * *", true)

	// One tick at 09:00: the first schedule fires, the second's slot (09:30)
	// is still in the future and must stay untouched.
	require.NoError(t, f.svc.Tick(context.Background(), at(21, 9, 0)))

	fired := mustRow(t, f.st, firing.ID)
	assert.Equal(t, store.ScheduleRunStatusRan, fired.LastRunStatus)
	requireTimePtr(t, fired.NextFireAt, at(22, 9, 0))
	waiting := mustRow(t, f.st, later.ID)
	assert.Empty(t, waiting.LastRunStatus)
	requireTimePtr(t, waiting.NextFireAt, at(21, 9, 30))
}
