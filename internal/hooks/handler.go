package hooks

import (
	"context"
	"fmt"
	"sync"
)

// Handler executes hook logic and returns an allow/block decision.
type Handler interface {
	Run(ctx context.Context, payload Payload) (Decision, error)
}

// HandlerFactory constructs a Handler from a JSON configuration slice.
type HandlerFactory func(cfg []byte) (Handler, error)

var (
	registryMu sync.RWMutex
	registry   = make(map[string]HandlerFactory)
)

// Register registers a handler factory for a type.
func Register(handlerType string, f HandlerFactory) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[handlerType] = f
}

// New creates a new handler instance from the registered factories.
func New(handlerType string, cfg []byte) (Handler, error) {
	registryMu.RLock()
	factory, exists := registry[handlerType]
	registryMu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("unknown hook handler type: %s", handlerType)
	}
	return factory(cfg)
}
