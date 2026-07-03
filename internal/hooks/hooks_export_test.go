package hooks

import (
	"context"
	"github.com/oniharnantyo/onclaw/internal/store"
)

func SetTestConsoleCaptured(fn func(string)) {
	testConsoleCaptured = fn
}

func LockTestConsole() {
	testConsoleMu.Lock()
}

func UnlockTestConsole() {
	testConsoleMu.Unlock()
}

func (d *Dispatcher) ResolveHooks(ctx context.Context, agent string, event Event) ([]*store.Hook, error) {
	return d.resolveHooks(ctx, agent, event)
}
