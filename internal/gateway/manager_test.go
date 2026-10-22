package gateway

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestManager_Start_Success(t *testing.T) {
	m := NewManager()

	require.Equal(t, StateStopped, m.State())
	require.NoError(t, m.Start(context.Background()))
	require.Equal(t, StateRunning, m.State())
}

func TestManager_Stop_Success(t *testing.T) {
	m := NewManager()

	require.NoError(t, m.Start(context.Background()))
	require.NoError(t, m.Stop())
	require.Equal(t, StateStopped, m.State())
}

func TestManager_PauseResume_Success(t *testing.T) {
	m := NewManager()
	require.NoError(t, m.Start(context.Background()))

	require.NoError(t, m.Pause())
	require.Equal(t, StatePaused, m.State())

	require.NoError(t, m.Resume())
	require.Equal(t, StateRunning, m.State())
}

func TestManager_StopFromPaused_Success(t *testing.T) {
	m := NewManager()
	require.NoError(t, m.Start(context.Background()))
	require.NoError(t, m.Pause())

	require.NoError(t, m.Stop())
	require.Equal(t, StateStopped, m.State())
}

func TestManager_InvalidTransitions(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(t *testing.T, m Manager)
		action func(t *testing.T, m Manager) error
	}{
		{
			name:  "start while running",
			setup: func(t *testing.T, m Manager) { require.NoError(t, m.Start(context.Background())) },
			action: func(t *testing.T, m Manager) error {
				return m.Start(context.Background())
			},
		},
		{
			name:  "start while paused",
			setup: func(t *testing.T, m Manager) { require.NoError(t, m.Start(context.Background())) },
			action: func(t *testing.T, m Manager) error {
				require.NoError(t, m.Pause())
				return m.Start(context.Background())
			},
		},
		{
			name:  "stop while stopped",
			setup: func(t *testing.T, _ Manager) {},
			action: func(t *testing.T, m Manager) error {
				return m.Stop()
			},
		},
		{
			name:  "pause while stopped",
			setup: func(t *testing.T, _ Manager) {},
			action: func(t *testing.T, m Manager) error {
				return m.Pause()
			},
		},
		{
			name:  "pause while paused",
			setup: func(t *testing.T, m Manager) { require.NoError(t, m.Start(context.Background())) },
			action: func(t *testing.T, m Manager) error {
				require.NoError(t, m.Pause())
				return m.Pause()
			},
		},
		{
			name:  "resume while running",
			setup: func(t *testing.T, m Manager) { require.NoError(t, m.Start(context.Background())) },
			action: func(t *testing.T, m Manager) error {
				return m.Resume()
			},
		},
		{
			name:  "resume while stopped",
			setup: func(t *testing.T, _ Manager) {},
			action: func(t *testing.T, m Manager) error {
				return m.Resume()
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewManager()
			tt.setup(t, m)

			err := tt.action(t, m)

			require.Error(t, err)
		})
	}
}

func TestManager_StateString(t *testing.T) {
	tests := []struct {
		state State
		want  string
	}{
		{StateStopped, "stopped"},
		{StateRunning, "running"},
		{StatePaused, "paused"},
		{State(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			require.Equal(t, tt.want, tt.state.String())
		})
	}
}

func TestManager_Subscribe_NotifyOnTransitions(t *testing.T) {
	m := NewManager()

	var mu sync.Mutex
	var got [][2]State
	m.Subscribe(func(from, to State) {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, [2]State{from, to})
	})

	require.NoError(t, m.Start(context.Background()))
	require.NoError(t, m.Pause())
	require.NoError(t, m.Resume())
	require.NoError(t, m.Stop())

	mu.Lock()
	defer mu.Unlock()
	want := [][2]State{
		{StateStopped, StateRunning},
		{StateRunning, StatePaused},
		{StatePaused, StateRunning},
		{StateRunning, StateStopped},
	}
	require.Equal(t, want, got)
}

func TestManager_Subscribe_CancelStopsNotifications(t *testing.T) {
	m := NewManager()

	calls := 0
	cancel := m.Subscribe(func(_, _ State) { calls++ })

	require.NoError(t, m.Start(context.Background()))
	require.Equal(t, 1, calls)

	cancel()
	cancel() // idempotent

	require.NoError(t, m.Stop())
	require.Equal(t, 1, calls)
}

func TestManager_Subscribe_ListenerMayCallState(t *testing.T) {
	m := NewManager()

	done := make(chan State, 1)
	m.Subscribe(func(_, _ State) {
		// Re-entering the manager from a listener must not deadlock.
		done <- m.State()
	})

	require.NoError(t, m.Start(context.Background()))
	require.Equal(t, StateRunning, <-done)
}
