package gateway

import (
	"context"
	"fmt"
	"sync"
)

// manager is the concrete Manager implementation. All state access is
// guarded by mu; listeners are invoked outside the lock so they may safely
// call back into the manager.
type manager struct {
	mu        sync.RWMutex
	state     State
	listeners map[int]StateListener
	nextID    int
}

func newManager() *manager {
	return &manager{
		state:     StateStopped,
		listeners: make(map[int]StateListener),
	}
}

func (m *manager) Start(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.state != StateStopped {
		return fmt.Errorf("cannot start gateway from %s state: already started", m.state)
	}
	return m.transition(StateRunning)
}

func (m *manager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.state == StateStopped {
		return fmt.Errorf("cannot stop gateway: already stopped")
	}
	return m.transition(StateStopped)
}

func (m *manager) Pause() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.state != StateRunning {
		return fmt.Errorf("cannot pause gateway from %s state: must be running", m.state)
	}
	return m.transition(StatePaused)
}

func (m *manager) Resume() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.state != StatePaused {
		return fmt.Errorf("cannot resume gateway from %s state: must be paused", m.state)
	}
	return m.transition(StateRunning)
}

func (m *manager) State() State {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state
}

func (m *manager) Subscribe(fn StateListener) (cancel func()) {
	m.mu.Lock()
	defer m.mu.Unlock()

	id := m.nextID
	m.nextID++
	m.listeners[id] = fn

	var once sync.Once
	return func() {
		once.Do(func() {
			m.mu.Lock()
			defer m.mu.Unlock()
			delete(m.listeners, id)
		})
	}
}

// transition commits a state change and notifies listeners. Callers must
// hold mu for writing and have validated the transition beforehand.
func (m *manager) transition(to State) error {
	from := m.state
	m.state = to

	listeners := make([]StateListener, 0, len(m.listeners))
	for _, fn := range m.listeners {
		listeners = append(listeners, fn)
	}
	m.mu.Unlock()
	defer m.mu.Lock()

	for _, fn := range listeners {
		fn(from, to)
	}
	return nil
}
