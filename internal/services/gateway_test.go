package services

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/gateway"
)

// fakeGatewayManager is a hand-written gateway.Manager fake recording
// listeners and the Start calls it received.
type fakeGatewayManager struct {
	mu        sync.Mutex
	state     gateway.State
	listeners []gateway.StateListener
	started   int

	pauseErr error
}

func (m *fakeGatewayManager) Start(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.started++
	m.setState(gateway.StateRunning)
	return nil
}

func (m *fakeGatewayManager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.setState(gateway.StateStopped)
	return nil
}

func (m *fakeGatewayManager) Pause() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pauseErr != nil {
		return m.pauseErr
	}
	m.setState(gateway.StatePaused)
	return nil
}

func (m *fakeGatewayManager) Resume() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.setState(gateway.StateRunning)
	return nil
}

func (m *fakeGatewayManager) State() gateway.State {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state
}

func (m *fakeGatewayManager) Subscribe(fn gateway.StateListener) (cancel func()) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.listeners = append(m.listeners, fn)
	var once sync.Once
	return func() { once.Do(func() {}) }
}

// setState commits a transition and notifies listeners outside the lock.
func (m *fakeGatewayManager) setState(to gateway.State) {
	from := m.state
	m.state = to
	listeners := append([]gateway.StateListener(nil), m.listeners...)
	for _, fn := range listeners {
		fn(from, to)
	}
}

func TestGatewayService_Start_State_Pause_Resume(t *testing.T) {
	gw := &fakeGatewayManager{}
	svc := NewGatewayService(gw, Noop{})
	ctx := context.Background()

	require.NoError(t, svc.Start(ctx))
	require.Equal(t, 1, gw.started)

	state, err := svc.State(ctx)
	require.NoError(t, err)
	require.Equal(t, gateway.StateRunning.String(), state)

	require.NoError(t, svc.Pause(ctx))
	state, err = svc.State(ctx)
	require.NoError(t, err)
	require.Equal(t, gateway.StatePaused.String(), state)

	require.NoError(t, svc.Resume(ctx))
	state, err = svc.State(ctx)
	require.NoError(t, err)
	require.Equal(t, gateway.StateRunning.String(), state)
}

func TestGatewayService_Pause_PropagatesError(t *testing.T) {
	gw := &fakeGatewayManager{}
	gw.pauseErr = errors.New("cannot pause gateway from stopped state: must be running")
	svc := NewGatewayService(gw, Noop{})

	err := svc.Pause(context.Background())
	require.ErrorIs(t, err, gw.pauseErr)
}

func TestGatewayService_StateChange_EmitsEvent(t *testing.T) {
	gw := &fakeGatewayManager{}
	emitter := &recordingEmitter{}
	svc := NewGatewayService(gw, emitter)

	require.NoError(t, svc.Start(context.Background()))

	require.Eventually(t, func() bool {
		recorded := emitter.recorded()
		return len(recorded) == 1
	}, time.Second, time.Millisecond)

	recorded := emitter.recorded()
	require.Equal(t, EventGatewayState, recorded[0].Name)
	state, ok := recorded[0].Data[0].(GatewayStateEvent)
	require.True(t, ok)
	require.Equal(t, gateway.StateRunning.String(), state.State)
}
