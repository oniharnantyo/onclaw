package scheduler

import (
	"context"
	"testing"

	"github.com/oniharnantyo/buddies/internal/gateway"
	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunNowOnDisabledPausedSchedule(t *testing.T) {
	// No gateway check, no enabled check: Paused and disabled still run.
	f := newFixture(t, at(21, 10, 0), gateway.StatePaused)
	agent := f.seedAgent(t, "agent-1")
	created := f.createSchedule(t, "Manual digest", agent, "Summarize now", "0 9 * * 1-5", false)

	var rec recordingListener
	cancel := f.svc.Subscribe(rec.onChange)
	defer cancel()

	view, err := f.svc.RunNow(context.Background(), created.ID)
	require.NoError(t, err)

	require.Len(t, f.runs.creates, 1)
	assert.Equal(t, createCall{AgentID: agent, Title: "Manual digest — 2026-10-21 10:00"}, f.runs.creates[0])
	require.Len(t, f.runs.submits, 1)
	assert.Equal(t, submitCall{SessionID: "sess-1", Prompt: "Summarize now"}, f.runs.submits[0])

	assert.Equal(t, store.ScheduleRunStatusRan, view.LastRunStatus)
	assert.Equal(t, "sess-1", view.LastRunSessionID)
	// The schedule stays disabled with no next fire time.
	assert.False(t, view.Enabled)
	assert.Nil(t, view.NextFireAt)

	row := mustRow(t, f.st, created.ID)
	requireTimePtr(t, row.LastRunAt, at(21, 10, 0))
	assert.Nil(t, row.NextFireAt)

	require.Len(t, rec.changes, 1)
	assert.Equal(t, Change{ScheduleID: created.ID, SessionID: "sess-1", AgentID: agent}, rec.changes[0])
}

func TestRunNowLeavesNextFireUnchanged(t *testing.T) {
	// Created Tuesday 09:30, so the stored next fire is Wednesday 09:00.
	f, created, fireAt := dailyFixture(t, gateway.StateRunning)

	view, err := f.svc.RunNow(context.Background(), created.ID)
	require.NoError(t, err)

	assert.Equal(t, store.ScheduleRunStatusRan, view.LastRunStatus)
	assert.Equal(t, "sess-1", view.LastRunSessionID)
	// The state machine was bypassed: the stored slot is identical.
	requireTimePtr(t, view.NextFireAt, fireAt)
	row := mustRow(t, f.st, created.ID)
	requireTimePtr(t, row.NextFireAt, fireAt)
}

func TestRunNowFailureKeepsNextFireAndRecordsReason(t *testing.T) {
	f := newFixture(t, at(21, 10, 0), gateway.StateRunning)
	agent := f.seedAgent(t, "agent-1")
	created := f.createSchedule(t, "Manual", agent, "p", "0 9 * * 1-5", false)
	f.runs.createErr = errAny

	view, err := f.svc.RunNow(context.Background(), created.ID)
	require.NoError(t, err)

	assert.Equal(t, store.ScheduleRunStatusFailed, view.LastRunStatus)
	assert.Equal(t, errAny.Error(), view.LastRunError)
	assert.Empty(t, view.LastRunSessionID)
	assert.Nil(t, view.NextFireAt)
	assert.Empty(t, f.runs.submits)
}

func TestRunNowUnknownSchedule(t *testing.T) {
	f := newFixture(t, at(21, 10, 0), gateway.StateRunning)

	_, err := f.svc.RunNow(context.Background(), "nope")
	require.ErrorIs(t, err, store.ErrNotFound)
	assert.Empty(t, f.runs.creates)
}

func TestRunNowWithStoppedGatewayStillRuns(t *testing.T) {
	f := newFixture(t, at(21, 10, 0), gateway.StateStopped)
	agent := f.seedAgent(t, "agent-1")
	created := f.createSchedule(t, "Manual", agent, "p", "0 9 * * 1-5", true)

	view, err := f.svc.RunNow(context.Background(), created.ID)
	require.NoError(t, err)
	assert.Equal(t, store.ScheduleRunStatusRan, view.LastRunStatus)
	require.Len(t, f.runs.creates, 1)
}
