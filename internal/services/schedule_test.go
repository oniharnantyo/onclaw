package services

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/gateway"
	"github.com/oniharnantyo/buddies/internal/scheduler"
	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/oniharnantyo/buddies/internal/store/fake"
)

// scheduleClock fixes the scheduler's clock so next-fire assertions are
// deterministic.
var scheduleClock = func() time.Time {
	return time.Date(2026, time.October, 21, 9, 0, 0, 0, time.UTC)
}

// recordingRuns records the scheduler Runs seam's calls. Every call happens
// synchronously inside the test goroutine (RunNow), so no mutex is needed.
type recordingRuns struct {
	created []string
	submits int
}

func (r *recordingRuns) Create(_ context.Context, _, _ string) (string, error) {
	id := fmt.Sprintf("sess-%d", len(r.created)+1)
	r.created = append(r.created, id)
	return id, nil
}

func (r *recordingRuns) Submit(_ context.Context, _, _ string) error {
	r.submits++
	return nil
}

// stubScheduleGateway reports a fixed state to the scheduler's tick — the
// Wails service tests never tick, so it only satisfies the constructor.
type stubScheduleGateway struct {
	state gateway.State
}

func (g *stubScheduleGateway) State() gateway.State { return g.state }

// scheduleFixture wires a bound ScheduleService over a real scheduler Service
// backed by the fake store.
type scheduleFixture struct {
	st      *fake.Store
	runs    *recordingRuns
	svc     *ScheduleService
	emitter *recordingEmitter
}

func newScheduleFixture(t *testing.T) *scheduleFixture {
	t.Helper()
	f := &scheduleFixture{
		st:      fake.New(),
		runs:    &recordingRuns{},
		emitter: &recordingEmitter{},
	}
	require.NoError(t, f.st.Agents().Create(context.Background(), &store.Agent{ID: "agt-1", Name: "Scout"}))
	sched := scheduler.New(f.st, f.runs, &stubScheduleGateway{state: gateway.StateStopped}, scheduler.WithClock(scheduleClock))
	f.svc = NewScheduleService(sched, f.emitter)
	return f
}

// createSchedule stores one enabled schedule through the bound service and
// waits for its (asynchronously emitted) change event to land, so later
// event-count snapshots are stable.
func (f *scheduleFixture) createSchedule(t *testing.T, name string) scheduler.ScheduleView {
	t.Helper()
	before := len(f.emitter.recorded())
	view, err := f.svc.Create(context.Background(), scheduler.ScheduleInput{
		Name:    name,
		AgentID: "agt-1",
		Prompt:  "hello",
		Cron:    "* * * * *",
		Enabled: true,
	})
	require.NoError(t, err)
	drainEvents(t, f.emitter, before, 1)
	return view
}

// drainEvents returns the events recorded after n, waiting (Eventually) for
// the expected count — the service emits on its own goroutine.
func drainEvents(t *testing.T, e *recordingEmitter, after, want int) []emittedEvent {
	t.Helper()
	require.Eventually(t, func() bool {
		return len(e.recorded())-after >= want
	}, time.Second, time.Millisecond)
	return e.recorded()[after : after+want]
}

func TestScheduleService_Create_EmitsSchedulesChanged(t *testing.T) {
	f := newScheduleFixture(t)
	ctx := context.Background()
	after := len(f.emitter.recorded())

	view, err := f.svc.Create(ctx, scheduler.ScheduleInput{
		Name: "Standup digest", AgentID: "agt-1", Prompt: "summarize", Cron: "30 9 * * *", Enabled: true,
	})
	require.NoError(t, err)
	require.NotEmpty(t, view.ID)

	events := drainEvents(t, f.emitter, after, 1)
	require.Equal(t, EventSchedulesChanged, events[0].Name)
	payload, ok := events[0].Data[0].(SchedulesChangedEvent)
	require.True(t, ok)
	require.Equal(t, view.ID, payload.ScheduleID)
	require.Equal(t, "agt-1", payload.AgentID)
	require.Empty(t, payload.SessionID) // a mutation, not a fire
}

func TestScheduleService_Update_SetEnabled_Delete_EmitSchedulesChanged(t *testing.T) {
	f := newScheduleFixture(t)
	ctx := context.Background()
	view := f.createSchedule(t, "Standup digest")

	t.Run("update", func(t *testing.T) {
		after := len(f.emitter.recorded())
		updated, err := f.svc.Update(ctx, view.ID, scheduler.ScheduleInput{
			Name: "Standup digest", AgentID: "agt-1", Prompt: "summarize twice", Cron: "30 9 * * 1-5", Enabled: true,
		})
		require.NoError(t, err)
		require.Equal(t, "summarize twice", updated.Prompt)

		events := drainEvents(t, f.emitter, after, 1)
		payload := events[0].Data[0].(SchedulesChangedEvent)
		require.Equal(t, view.ID, payload.ScheduleID)
	})

	t.Run("setEnabled", func(t *testing.T) {
		after := len(f.emitter.recorded())
		toggled, err := f.svc.SetEnabled(ctx, view.ID, false)
		require.NoError(t, err)
		require.False(t, toggled.Enabled)
		require.Nil(t, toggled.NextFireAt) // disabling clears the next fire

		events := drainEvents(t, f.emitter, after, 1)
		payload := events[0].Data[0].(SchedulesChangedEvent)
		require.Equal(t, view.ID, payload.ScheduleID)
	})

	t.Run("delete", func(t *testing.T) {
		after := len(f.emitter.recorded())
		require.NoError(t, f.svc.Delete(ctx, view.ID))

		events := drainEvents(t, f.emitter, after, 1)
		payload := events[0].Data[0].(SchedulesChangedEvent)
		require.Equal(t, view.ID, payload.ScheduleID)
	})
}

func TestScheduleService_RunNow_EmitsEventWithSessionID(t *testing.T) {
	f := newScheduleFixture(t)
	ctx := context.Background()
	view := f.createSchedule(t, "Standup digest")
	after := len(f.emitter.recorded())

	ran, err := f.svc.RunNow(ctx, view.ID)
	require.NoError(t, err)
	require.Equal(t, "sess-1", ran.LastRunSessionID)
	require.Equal(t, string(store.ScheduleRunStatusRan), ran.LastRunStatus)
	require.Equal(t, 1, f.runs.submits)

	// The run's event carries the session id so the chat list can surface
	// the background-created session (design D10).
	events := drainEvents(t, f.emitter, after, 1)
	payload := events[0].Data[0].(SchedulesChangedEvent)
	require.Equal(t, view.ID, payload.ScheduleID)
	require.Equal(t, "sess-1", payload.SessionID)
	require.Equal(t, "agt-1", payload.AgentID)
}

func TestScheduleService_Preview_PassesThrough(t *testing.T) {
	f := newScheduleFixture(t)
	ctx := context.Background()

	t.Run("ok", func(t *testing.T) {
		preview, err := f.svc.Preview(ctx, "30 9 * * *")
		require.NoError(t, err)
		require.True(t, preview.OK)
		require.NotEmpty(t, preview.Description)
		require.Len(t, preview.Next, 3)
		require.True(t, preview.Next[0].Before(preview.Next[1]))
		require.True(t, preview.Next[1].Before(preview.Next[2]))
	})

	t.Run("error", func(t *testing.T) {
		preview, err := f.svc.Preview(ctx, "not a cron")
		require.NoError(t, err) // an invalid expression is a Preview, not a Go error
		require.False(t, preview.OK)
		require.NotEmpty(t, preview.Error)
	})
}

func TestScheduleService_ValidationError_ReturnedUnwrapped(t *testing.T) {
	f := newScheduleFixture(t)

	_, err := f.svc.Create(context.Background(), scheduler.ScheduleInput{Name: "", AgentID: "agt-1", Prompt: "p", Cron: "* * * * *", Enabled: true})
	require.Error(t, err)
	var verr *scheduler.ValidationError
	require.ErrorAs(t, err, &verr) // unwrapped: errors.As must reach it
	require.Equal(t, "name", verr.Field)
	require.Equal(t, "name: name is required", err.Error())
	require.NotContains(t, err.Error(), "create schedule") // no wrapping context

	// Duplicate names identify the existing schedule.
	existing := f.createSchedule(t, "Standup digest")
	_, err = f.svc.Create(context.Background(), scheduler.ScheduleInput{
		Name: "standup DIGEST", AgentID: "agt-1", Prompt: "p", Cron: "* * * * *", Enabled: true,
	})
	require.ErrorAs(t, err, &verr)
	require.Equal(t, "name", verr.Field)
	require.Equal(t, existing.ID, verr.ExistingID)
}

func TestScheduleService_List_EmptyReturnsSliceNotNil(t *testing.T) {
	f := newScheduleFixture(t)

	views, err := f.svc.List(context.Background())
	require.NoError(t, err)
	require.NotNil(t, views)
	require.Empty(t, views)

	f.createSchedule(t, "Standup digest")
	views, err = f.svc.List(context.Background())
	require.NoError(t, err)
	require.Len(t, views, 1)
}

func TestScheduleService_OtherErrors_Wrapped(t *testing.T) {
	f := newScheduleFixture(t)

	err := f.svc.Delete(context.Background(), "missing")
	require.Error(t, err)
	require.ErrorIs(t, err, store.ErrNotFound) // chain preserved through the wrap
	require.Contains(t, err.Error(), "delete schedule")
}
