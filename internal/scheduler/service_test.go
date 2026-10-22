package scheduler

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/oniharnantyo/buddies/internal/gateway"
	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// wedNoon is a fixed service clock: Wednesday 2026-10-21 10:00 local. The
// next weekday 09:00 after it is Thursday the 22nd.
func wedNoon() time.Time { return at(21, 10, 0) }

func TestServiceCreateValidation(t *testing.T) {
	tests := []struct {
		name      string
		input     ScheduleInput
		wantField string
		wantMsg   []string
	}{
		{
			name:      "empty name",
			input:     ScheduleInput{AgentID: "agent-1", Prompt: "p", Cron: "0 9 * * 1-5"},
			wantField: "name",
			wantMsg:   []string{"name is required"},
		},
		{
			name:      "blank name",
			input:     ScheduleInput{Name: "   ", AgentID: "agent-1", Prompt: "p", Cron: "0 9 * * 1-5"},
			wantField: "name",
			wantMsg:   []string{"name is required"},
		},
		{
			name:      "empty prompt",
			input:     ScheduleInput{Name: "n", AgentID: "agent-1", Cron: "0 9 * * 1-5"},
			wantField: "prompt",
			wantMsg:   []string{"prompt is required"},
		},
		{
			name:      "empty agent",
			input:     ScheduleInput{Name: "n", Prompt: "p", Cron: "0 9 * * 1-5"},
			wantField: "agent",
			wantMsg:   []string{"agent is required"},
		},
		{
			name:      "unknown agent",
			input:     ScheduleInput{Name: "n", AgentID: "ghost", Prompt: "p", Cron: "0 9 * * 1-5"},
			wantField: "agent",
			wantMsg:   []string{"agent ghost does not exist"},
		},
		{
			name:      "not an expression",
			input:     ScheduleInput{Name: "n", AgentID: "agent-1", Prompt: "p", Cron: "every monday"},
			wantField: "recurrence",
			wantMsg:   []string{fiveFieldHint, `got "every monday"`},
		},
		{
			name:      "hour out of range",
			input:     ScheduleInput{Name: "n", AgentID: "agent-1", Prompt: "p", Cron: "0 25 * * *"},
			wantField: "recurrence",
			wantMsg:   []string{fiveFieldHint, "25"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, wedNoon(), gateway.StateRunning)
			f.seedAgent(t, "agent-1")

			_, err := f.svc.Create(context.Background(), tt.input)
			verr := requireValidation(t, err, tt.wantField, tt.wantMsg...)
			if tt.wantField == "recurrence" {
				// The recurrence message is the parser's error text verbatim.
				_, perr := ParseFiveField(tt.input.Cron)
				require.Error(t, perr)
				require.Equal(t, perr.Error(), verr.Message)
			}

			// Nothing is stored for a rejected write.
			rows, listErr := f.st.Schedules().List(context.Background())
			require.NoError(t, listErr)
			assert.Empty(t, rows)
		})
	}
}

func TestServiceCreateDuplicateName(t *testing.T) {
	f := newFixture(t, wedNoon(), gateway.StateRunning)
	agent := f.seedAgent(t, "agent-1")
	first := f.createSchedule(t, "Standup digest", agent, "p", "0 9 * * 1-5", true)

	_, err := f.svc.Create(context.Background(), ScheduleInput{
		Name: "standup digest", AgentID: agent, Prompt: "p", Cron: "*/5 * * * *",
	})
	verr := requireValidation(t, err, "name", fmt.Sprintf("a schedule named %q already exists (id %s)", "Standup digest", first.ID))
	require.Equal(t, first.ID, verr.ExistingID)
}

func TestServiceCreateStoresTrimmedWithNextFire(t *testing.T) {
	f := newFixture(t, wedNoon(), gateway.StateRunning)
	agent := f.seedAgent(t, "agent-1")

	view := f.createSchedule(t, "  Standup digest  ", agent, "  Summarize standup  ", " 0 9 * * 1-5 ", true)

	assert.Equal(t, "Standup digest", view.Name)
	assert.Equal(t, "Summarize standup", view.Prompt)
	assert.Equal(t, "0 9 * * 1-5", view.Cron)
	assert.True(t, view.Enabled)
	// Next weekday 09:00 after Wednesday 10:00 is Thursday 09:00.
	requireTimePtr(t, view.NextFireAt, at(22, 9, 0))
	assert.Equal(t, "Every weekday at 09:00", view.Description)

	row := mustRow(t, f.st, view.ID)
	requireTimePtr(t, row.NextFireAt, at(22, 9, 0))
}

func TestServiceCreateDisabledHasNoNextFire(t *testing.T) {
	f := newFixture(t, wedNoon(), gateway.StateRunning)
	agent := f.seedAgent(t, "agent-1")

	view := f.createSchedule(t, "Off", agent, "p", "0 9 * * 1-5", false)

	assert.False(t, view.Enabled)
	assert.Nil(t, view.NextFireAt)
}

func TestServiceSetEnabled(t *testing.T) {
	f := newFixture(t, wedNoon(), gateway.StateRunning)
	agent := f.seedAgent(t, "agent-1")
	view := f.createSchedule(t, "Standup", agent, "p", "0 9 * * 1-5", true)

	// Toggling off clears the next fire time.
	off, err := f.svc.SetEnabled(context.Background(), view.ID, false)
	require.NoError(t, err)
	assert.False(t, off.Enabled)
	assert.Nil(t, off.NextFireAt)
	assert.Nil(t, mustRow(t, f.st, view.ID).NextFireAt)

	// Toggling on recomputes it from the recurrence and the clock.
	on, err := f.svc.SetEnabled(context.Background(), view.ID, true)
	require.NoError(t, err)
	assert.True(t, on.Enabled)
	requireTimePtr(t, on.NextFireAt, at(22, 9, 0))
	requireTimePtr(t, mustRow(t, f.st, view.ID).NextFireAt, at(22, 9, 0))
}

func TestServiceUpdate(t *testing.T) {
	f := newFixture(t, wedNoon(), gateway.StateRunning)
	agent := f.seedAgent(t, "agent-1")
	view := f.createSchedule(t, "Standup", agent, "p", "0 9 * * 1-5", true)

	// Full replace of the five input fields; next fire recomputed from the
	// new expression.
	updated, err := f.svc.Update(context.Background(), view.ID, ScheduleInput{
		Name: "Renamed", AgentID: agent, Prompt: "new prompt", Cron: "30 12 * * *", Enabled: true,
	})
	require.NoError(t, err)
	assert.Equal(t, "Renamed", updated.Name)
	assert.Equal(t, "new prompt", updated.Prompt)
	assert.Equal(t, "30 12 * * *", updated.Cron)
	assert.Equal(t, "Daily at 12:30", updated.Description)
	// Next 12:30 after Wednesday 10:00 is today 12:30.
	requireTimePtr(t, updated.NextFireAt, at(21, 12, 30))

	// Update to disabled clears the next fire time.
	disabled, err := f.svc.Update(context.Background(), view.ID, ScheduleInput{
		Name: "Renamed", AgentID: agent, Prompt: "new prompt", Cron: "30 12 * * *", Enabled: false,
	})
	require.NoError(t, err)
	assert.Nil(t, disabled.NextFireAt)

	// Last-run state survives an update.
	fire := store.ScheduleFire{At: at(21, 9, 0), Status: store.ScheduleRunStatusRan, SessionID: "sess-1"}
	require.NoError(t, f.st.Schedules().RecordFire(context.Background(), view.ID, fire))
	_, err = f.svc.Update(context.Background(), view.ID, ScheduleInput{
		Name: "Renamed", AgentID: agent, Prompt: "new prompt", Cron: "30 12 * * *", Enabled: true,
	})
	require.NoError(t, err)
	row := mustRow(t, f.st, view.ID)
	assert.Equal(t, store.ScheduleRunStatusRan, row.LastRunStatus)
	assert.Equal(t, "sess-1", row.LastRunSessionID)

	// Unknown id wraps store.ErrNotFound.
	_, err = f.svc.Update(context.Background(), "nope", ScheduleInput{
		Name: "n", AgentID: agent, Prompt: "p", Cron: "0 9 * * 1-5", Enabled: true,
	})
	require.ErrorIs(t, err, store.ErrNotFound)
}

func TestServiceUpdateValidation(t *testing.T) {
	f := newFixture(t, wedNoon(), gateway.StateRunning)
	agent := f.seedAgent(t, "agent-1")
	first := f.createSchedule(t, "Standup digest", agent, "p", "0 9 * * 1-5", true)
	second := f.createSchedule(t, "Other", agent, "p", "*/5 * * * *", true)

	// Renaming onto another schedule's name (case-insensitively) names the
	// existing schedule.
	_, err := f.svc.Update(context.Background(), second.ID, ScheduleInput{
		Name: "STANDUP DIGEST", AgentID: agent, Prompt: "p", Cron: "*/5 * * * *", Enabled: true,
	})
	verr := requireValidation(t, err, "name", `a schedule named "Standup digest" already exists`)
	require.Equal(t, first.ID, verr.ExistingID)

	// Keeping its own name is not a duplicate.
	_, err = f.svc.Update(context.Background(), second.ID, ScheduleInput{
		Name: "Other", AgentID: agent, Prompt: "p", Cron: "*/5 * * * *", Enabled: true,
	})
	require.NoError(t, err)

	// Every other field is validated on update too.
	_, err = f.svc.Update(context.Background(), second.ID, ScheduleInput{
		Name: "Other", AgentID: agent, Prompt: "p", Cron: "nope", Enabled: true,
	})
	requireValidation(t, err, "recurrence")
}

func TestServiceDelete(t *testing.T) {
	f := newFixture(t, wedNoon(), gateway.StateRunning)
	agent := f.seedAgent(t, "agent-1")
	view := f.createSchedule(t, "Standup", agent, "p", "0 9 * * 1-5", true)

	require.NoError(t, f.svc.Delete(context.Background(), view.ID))
	rows, err := f.st.Schedules().List(context.Background())
	require.NoError(t, err)
	assert.Empty(t, rows)

	// Deleting again is store.ErrNotFound (wrapped).
	err = f.svc.Delete(context.Background(), view.ID)
	require.ErrorIs(t, err, store.ErrNotFound)
}

func TestServiceGetAndList(t *testing.T) {
	f := newFixture(t, wedNoon(), gateway.StateRunning)
	agent := f.seedAgent(t, "agent-1")

	_, err := f.svc.Get(context.Background(), "nope")
	require.ErrorIs(t, err, store.ErrNotFound)

	f.createSchedule(t, "Beta", agent, "p", "0 9 * * 1-5", true)
	f.createSchedule(t, "alpha", agent, "p", "*/5 * * * *", true)

	views, err := f.svc.List(context.Background())
	require.NoError(t, err)
	require.Len(t, views, 2)
	// The store's case-insensitive name order is preserved.
	assert.Equal(t, "alpha", views[0].Name)
	assert.Equal(t, "Beta", views[1].Name)
	assert.Equal(t, "Every weekday at 09:00", views[1].Description)
}

func TestServicePreview(t *testing.T) {
	f := newFixture(t, wedNoon(), gateway.StateRunning)

	p := f.svc.Preview("0 9 * * 1-5")
	assert.True(t, p.OK)
	assert.Empty(t, p.Error)
	assert.Equal(t, "Every weekday at 09:00", p.Description)
	require.Len(t, p.Next, 3)
	assert.True(t, p.Next[0].Equal(at(22, 9, 0)))
	assert.True(t, p.Next[1].Equal(at(23, 9, 0)))
	assert.True(t, p.Next[2].Equal(at(26, 9, 0))) // Monday

	bad := f.svc.Preview("every monday")
	assert.False(t, bad.OK)
	assert.Contains(t, bad.Error, fiveFieldHint)
	assert.Empty(t, bad.Next)
}

func TestServiceSubscribeNotifiesOnMutations(t *testing.T) {
	f := newFixture(t, wedNoon(), gateway.StateRunning)
	agent := f.seedAgent(t, "agent-1")

	var rec recordingListener
	cancel := f.svc.Subscribe(rec.onChange)

	view := f.createSchedule(t, "Standup", agent, "p", "0 9 * * 1-5", true)
	_, err := f.svc.Update(context.Background(), view.ID, ScheduleInput{
		Name: "Standup", AgentID: agent, Prompt: "p2", Cron: "0 9 * * 1-5", Enabled: true,
	})
	require.NoError(t, err)
	_, err = f.svc.SetEnabled(context.Background(), view.ID, false)
	require.NoError(t, err)
	require.NoError(t, f.svc.Delete(context.Background(), view.ID))

	require.Len(t, rec.changes, 4)
	for _, ch := range rec.changes {
		assert.Equal(t, view.ID, ch.ScheduleID)
		assert.Equal(t, agent, ch.AgentID)
		assert.Empty(t, ch.SessionID)
	}

	// Cancel stops delivery; calling it twice is safe.
	cancel()
	cancel()
	again, err := f.svc.Create(context.Background(), ScheduleInput{
		Name: "Once more", AgentID: agent, Prompt: "p", Cron: "0 9 * * 1-5", Enabled: true,
	})
	require.NoError(t, err)
	require.NoError(t, f.svc.Delete(context.Background(), again.ID))
	assert.Len(t, rec.changes, 4)
}

func TestValidationErrorShape(t *testing.T) {
	err := error(&ValidationError{Field: "name", Message: "name is required"})
	require.EqualError(t, err, "name: name is required")
	require.True(t, errors.Is(err, err)) // sanity: usable as a normal error
}
