package services

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// recordingEmitter is a hand-written Emitter fake: it records every emitted
// event for assertions.
type recordingEmitter struct {
	mu     sync.Mutex
	events []emittedEvent
}

// emittedEvent is one recorded Emit call.
type emittedEvent struct {
	Name string
	Data []any
}

func (e *recordingEmitter) Emit(name string, data ...any) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events = append(e.events, emittedEvent{Name: name, Data: data})
}

func (e *recordingEmitter) recorded() []emittedEvent {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]emittedEvent(nil), e.events...)
}

func TestNoop_Emit_Discards(t *testing.T) {
	require.NotPanics(t, func() { Noop{}.Emit("gateway:state", GatewayStateEvent{State: "running"}) })
}
