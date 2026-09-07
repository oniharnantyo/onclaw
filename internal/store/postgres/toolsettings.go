package postgres

import (
	"context"
	"encoding/json"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
	storeport "github.com/oniharnantyo/onclaw/internal/store"
)

// toolSettingStore implements storeport.ToolSettingsStore for PostgreSQL.
type toolSettingStore struct {
	db Executor
}

// NewToolSettingStore creates a new ToolSettingsStore with the given database executor.
func NewToolSettingStore(db Executor) storeport.ToolSettingsStore {
	return &toolSettingStore{db: db}
}

func (ts *toolSettingStore) Get(ctx context.Context, workspaceID, toolKey string) (*domain.WorkspaceToolSetting, error) {
	if workspaceID == "" || toolKey == "" {
		return nil, domain.ErrNotFound
	}

	query := `
		SELECT workspace_id, tool_key, enabled, config, updated_at
		FROM workspace_tool_settings
		WHERE workspace_id = $1 AND tool_key = $2
	`
	var setting domain.WorkspaceToolSetting
	var configJSON []byte
	err := ts.db.QueryRow(ctx, query, workspaceID, toolKey).Scan(
		&setting.WorkspaceID,
		&setting.ToolKey,
		&setting.Enabled,
		&configJSON,
		&setting.UpdatedAt,
	)
	if err != nil {
		return nil, convertError(err)
	}
	if err := json.Unmarshal(configJSON, &setting.Config); err != nil {
		return nil, convertError(err)
	}
	if setting.Config == nil {
		setting.Config = map[string]any{}
	}
	return &setting, nil
}

func (ts *toolSettingStore) Upsert(ctx context.Context, setting *domain.WorkspaceToolSetting) error {
	if setting == nil || setting.WorkspaceID == "" || setting.ToolKey == "" {
		return domain.ErrInvalid
	}

	now := time.Now().UTC()
	if setting.UpdatedAt.IsZero() {
		setting.UpdatedAt = now
	}

	configJSON, err := json.Marshal(setting.Config)
	if err != nil {
		return convertError(err)
	}
	if setting.Config == nil {
		configJSON = []byte("{}")
	}

	query := `
		INSERT INTO workspace_tool_settings (workspace_id, tool_key, enabled, config, updated_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (workspace_id, tool_key) DO UPDATE
		SET enabled = EXCLUDED.enabled,
		    config = EXCLUDED.config,
		    updated_at = EXCLUDED.updated_at
	`
	_, err = ts.db.Exec(ctx, query,
		setting.WorkspaceID,
		setting.ToolKey,
		setting.Enabled,
		configJSON,
		setting.UpdatedAt,
	)
	if err != nil {
		return convertError(err)
	}
	return nil
}

func (ts *toolSettingStore) List(ctx context.Context, workspaceID string) ([]domain.WorkspaceToolSetting, error) {
	if workspaceID == "" {
		return []domain.WorkspaceToolSetting{}, nil
	}

	query := `
		SELECT workspace_id, tool_key, enabled, config, updated_at
		FROM workspace_tool_settings
		WHERE workspace_id = $1
		ORDER BY tool_key ASC
	`
	rows, err := ts.db.Query(ctx, query, workspaceID)
	if err != nil {
		return nil, convertError(err)
	}
	defer rows.Close()

	settings := make([]domain.WorkspaceToolSetting, 0)
	for rows.Next() {
		var setting domain.WorkspaceToolSetting
		var configJSON []byte
		if err := rows.Scan(
			&setting.WorkspaceID,
			&setting.ToolKey,
			&setting.Enabled,
			&configJSON,
			&setting.UpdatedAt,
		); err != nil {
			return nil, convertError(err)
		}
		if err := json.Unmarshal(configJSON, &setting.Config); err != nil {
			return nil, convertError(err)
		}
		if setting.Config == nil {
			setting.Config = map[string]any{}
		}
		settings = append(settings, setting)
	}
	if err := rows.Err(); err != nil {
		return nil, convertError(err)
	}
	return settings, nil
}
