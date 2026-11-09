package fake

import (
	"context"

	"github.com/oniharnantyo/buddies/internal/store"
)

type notificationSettingsStore struct{ s *Store }

func (n notificationSettingsStore) Get(_ context.Context) (*store.NotificationSettings, error) {
	n.s.mu.Lock()
	defer n.s.mu.Unlock()

	if n.s.notificationSettings == nil {
		return nil, nil
	}
	out := *n.s.notificationSettings
	return &out, nil
}

func (n notificationSettingsStore) Save(_ context.Context, settings *store.NotificationSettings) error {
	n.s.mu.Lock()
	defer n.s.mu.Unlock()

	settings.UpdatedAt = nowUTC()
	cp := *settings
	n.s.notificationSettings = &cp
	return nil
}
