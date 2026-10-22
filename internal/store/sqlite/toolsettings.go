package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

type toolSettingStore struct{ e executor }

func (r *toolSettingStore) Set(ctx context.Context, tool string, settings string) error {
	now := nowUTC()

	_, err := r.e.ExecContext(ctx, `
INSERT INTO tool_settings (tool, settings, created_at, updated_at)
VALUES (?, ?, ?, ?)
ON CONFLICT (tool) DO UPDATE SET
settings = excluded.settings,
updated_at = excluded.updated_at`,
		tool, settings, formatTime(now), formatTime(now),
	)
	if err != nil {
		return fmt.Errorf("set tool %s settings: %w", tool, err)
	}
	return nil
}

func (r *toolSettingStore) Get(ctx context.Context, tool string) (string, bool, error) {
	row := r.e.QueryRowContext(ctx, `SELECT settings FROM tool_settings WHERE tool = ?`, tool)
	var settings string
	err := row.Scan(&settings)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// No stored row: the tool has no configuration (see the port
			// contract).
			return "", false, nil
		}
		return "", false, fmt.Errorf("get tool %s settings: %w", tool, err)
	}
	return settings, true, nil
}
