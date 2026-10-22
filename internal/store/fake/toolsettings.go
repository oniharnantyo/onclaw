package fake

import (
	"context"
)

type toolSettingStore struct{ s *Store }

func (r toolSettingStore) Set(_ context.Context, tool string, settings string) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()

	r.s.toolSettings[tool] = settings
	return nil
}

func (r toolSettingStore) Get(_ context.Context, tool string) (string, bool, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()

	settings, ok := r.s.toolSettings[tool]
	return settings, ok, nil
}
