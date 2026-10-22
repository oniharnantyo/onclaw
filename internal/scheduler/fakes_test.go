package scheduler

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/oniharnantyo/buddies/internal/gateway"
	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/oniharnantyo/buddies/internal/store/fake"
	"github.com/stretchr/testify/require"
)

// atSec is at() with seconds, for tolerance-boundary cases.
func atSec(day, hour, minute, second int) time.Time {
	return time.Date(2026, time.October, day, hour, minute, second, 0, testZone)
}

// createCall is one recorded fakeRuns.Create.
type createCall struct {
	AgentID string
	Title   string
}

// submitCall is one recorded fakeRuns.Submit.
type submitCall struct {
	SessionID string
	Prompt    string
}

// fakeRuns records every Runs call so tests can assert the exact fire path.
// Calls are all synchronous (the service never fires in the background), so
// no mutex is needed.
type fakeRuns struct {
	creates   []createCall
	submits   []submitCall
	createErr error // returned by Create; the call is still recorded
	submitErr error // returned by Submit; the call is still recorded
	sessions  int
}

func (f *fakeRuns) Create(_ context.Context, agentID, title string) (string, error) {
	f.creates = append(f.creates, createCall{AgentID: agentID, Title: title})
	if f.createErr != nil {
		return "", f.createErr
	}
	f.sessions++
	return fmt.Sprintf("sess-%d", f.sessions), nil
}

func (f *fakeRuns) Submit(_ context.Context, sessionID, prompt string) error {
	f.submits = append(f.submits, submitCall{SessionID: sessionID, Prompt: prompt})
	return f.submitErr
}

// stubGateway reports a fixed state the test sets.
type stubGateway struct {
	state gateway.State
}

func (g *stubGateway) State() gateway.State { return g.state }

// recordingListener collects the Changes a subscription receives.
type recordingListener struct {
	changes []Change
}

func (r *recordingListener) onChange(c Change) { r.changes = append(r.changes, c) }

// fixture wires a Service over the fake store, a recording Runs, a fixed
// gateway state, and a fixed clock.
type fixture struct {
	st   store.Store
	runs *fakeRuns
	gw   *stubGateway
	svc  *Service
}

func newFixture(t *testing.T, now time.Time, state gateway.State) *fixture {
	t.Helper()
	st := fake.New()
	runs := &fakeRuns{}
	gw := &stubGateway{state: state}
	svc := New(st, runs, gw, WithClock(func() time.Time { return now }))
	return &fixture{st: st, runs: runs, gw: gw, svc: svc}
}

// seedAgent stores an agent with the given id and returns the id.
func (f *fixture) seedAgent(t *testing.T, id string) string {
	t.Helper()
	require.NoError(t, f.st.Agents().Create(context.Background(), &store.Agent{ID: id, Name: "Agent " + id}))
	return id
}

// createSchedule stores a schedule through the service and returns its view.
func (f *fixture) createSchedule(t *testing.T, name, agentID, prompt, cron string, enabled bool) ScheduleView {
	t.Helper()
	view, err := f.svc.Create(context.Background(), ScheduleInput{
		Name:    name,
		AgentID: agentID,
		Prompt:  prompt,
		Cron:    cron,
		Enabled: enabled,
	})
	require.NoError(t, err)
	return view
}

// seedSchedule stores a schedule straight into the store, bypassing service
// validation — for rows the service itself could never produce (a disabled
// row with a due slot, a nonexistent agent, a broken expression).
func (f *fixture) seedSchedule(t *testing.T, row store.Schedule) store.Schedule {
	t.Helper()
	require.NoError(t, f.st.Schedules().Create(context.Background(), &row))
	return row
}

// mustRow returns the raw stored schedule.
func mustRow(t *testing.T, st store.Store, id string) store.Schedule {
	t.Helper()
	row, err := st.Schedules().ByID(context.Background(), id)
	require.NoError(t, err)
	return *row
}

// requireValidation asserts err is a *ValidationError with the given field
// and message substrings.
func requireValidation(t *testing.T, err error, field string, wantMsg ...string) *ValidationError {
	t.Helper()
	require.Error(t, err)
	var verr *ValidationError
	require.ErrorAs(t, err, &verr)
	require.Equal(t, field, verr.Field)
	for _, want := range wantMsg {
		require.Contains(t, verr.Message, want)
	}
	return verr
}

// requireTimePtr asserts t is non-nil and the same instant as want.
func requireTimePtr(t *testing.T, got *time.Time, want time.Time) time.Time {
	t.Helper()
	require.NotNil(t, got)
	require.True(t, got.Equal(want), "got %s, want %s", got, want)
	return *got
}

// errAny is a marker error for fake failures.
var errAny = errors.New("injected failure")
