package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/oniharnantyo/buddies/internal/store"
)

type notificationSettingsStore struct{ e executor }

func (n *notificationSettingsStore) Get(ctx context.Context) (*store.NotificationSettings, error) {
	var settings store.NotificationSettings
	var updatedAt string

	row := n.e.QueryRowContext(ctx,
		`SELECT enabled, turns, attention, schedules, groups, updated_at FROM notification_settings WHERE id = 1`)
	err := row.Scan(&settings.Enabled, &settings.Turns, &settings.Attention, &settings.Schedules, &settings.Groups, &updatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, readErr("get notification settings", err)
	}
	if settings.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return nil, err
	}
	return &settings, nil
}

func (n *notificationSettingsStore) Save(ctx context.Context, settings *store.NotificationSettings) error {
	settings.UpdatedAt = nowUTC()

	_, err := n.e.ExecContext(ctx, `
INSERT INTO notification_settings (id, enabled, turns, attention, schedules, groups, updated_at)
VALUES (1, ?, ?, ?, ?, ?, ?)
ON CONFLICT (id) DO UPDATE SET
enabled = excluded.enabled,
turns = excluded.turns,
attention = excluded.attention,
schedules = excluded.schedules,
groups = excluded.groups,
updated_at = excluded.updated_at`,
		boolInt(settings.Enabled), boolInt(settings.Turns), boolInt(settings.Attention),
		boolInt(settings.Schedules), boolInt(settings.Groups), formatTime(settings.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("save notification settings: %w", err)
	}
	return nil
}
