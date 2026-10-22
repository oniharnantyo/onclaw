package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/oniharnantyo/buddies/internal/store"
)

type gatewaySettingsStore struct{ e executor }

func (g *gatewaySettingsStore) Get(ctx context.Context) (*store.GatewaySettings, error) {
	var settings store.GatewaySettings
	var updatedAt string

	row := g.e.QueryRowContext(ctx,
		`SELECT enabled, default_agent_id, updated_at FROM gateway_settings WHERE id = 1`)
	err := row.Scan(&settings.Enabled, &settings.DefaultAgentID, &updatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, readErr("get gateway settings", err)
	}
	if settings.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return nil, err
	}
	return &settings, nil
}

func (g *gatewaySettingsStore) Save(ctx context.Context, settings *store.GatewaySettings) error {
	settings.UpdatedAt = nowUTC()

	_, err := g.e.ExecContext(ctx, `
INSERT INTO gateway_settings (id, enabled, default_agent_id, updated_at)
VALUES (1, ?, ?, ?)
ON CONFLICT (id) DO UPDATE SET
enabled = excluded.enabled,
default_agent_id = excluded.default_agent_id,
updated_at = excluded.updated_at`,
		boolInt(settings.Enabled), settings.DefaultAgentID, formatTime(settings.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("save gateway settings: %w", err)
	}
	return nil
}
