package fake

import (
	"context"

	"github.com/oniharnantyo/buddies/internal/store"
)

type gatewaySettingsStore struct{ s *Store }

func (g gatewaySettingsStore) Get(_ context.Context) (*store.GatewaySettings, error) {
	g.s.mu.Lock()
	defer g.s.mu.Unlock()

	if g.s.gatewaySettings == nil {
		return nil, nil
	}
	out := *g.s.gatewaySettings
	return &out, nil
}

func (g gatewaySettingsStore) Save(_ context.Context, settings *store.GatewaySettings) error {
	g.s.mu.Lock()
	defer g.s.mu.Unlock()

	settings.UpdatedAt = nowUTC()
	cp := *settings
	g.s.gatewaySettings = &cp
	return nil
}
