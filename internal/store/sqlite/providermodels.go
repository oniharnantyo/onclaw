package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/oniharnantyo/buddies/internal/store"
)

type providerModelStore struct{ e executor }

const providerModelColumns = `id, provider_id, model_id, context_window, image_input, pdf_input, video_input, efforts, enabled, created_at, updated_at`

func isUniqueConstraintErr(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "UNIQUE constraint failed")
}

func (s *providerModelStore) Create(ctx context.Context, model *store.ProviderModel) error {
	stampCreated(&model.CreatedAt, &model.UpdatedAt)

	efforts, err := encodeEfforts(model.Efforts)
	if err != nil {
		return fmt.Errorf("create provider model: %w", err)
	}

	var res sql.Result
	if model.ID != 0 {
		res, err = s.e.ExecContext(ctx, `
INSERT INTO provider_models (`+providerModelColumns+`)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			model.ID, model.ProviderID, model.ModelID, model.ContextWindow,
			boolInt(model.ImageInput), boolInt(model.PDFInput), boolInt(model.VideoInput),
			efforts, boolInt(model.Enabled),
			formatTime(model.CreatedAt), formatTime(model.UpdatedAt),
		)
	} else {
		res, err = s.e.ExecContext(ctx, `
INSERT INTO provider_models (provider_id, model_id, context_window, image_input, pdf_input, video_input, efforts, enabled, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			model.ProviderID, model.ModelID, model.ContextWindow,
			boolInt(model.ImageInput), boolInt(model.PDFInput), boolInt(model.VideoInput),
			efforts, boolInt(model.Enabled),
			formatTime(model.CreatedAt), formatTime(model.UpdatedAt),
		)
	}
	if err != nil {
		if isUniqueConstraintErr(err) {
			return fmt.Errorf("create provider model: %w", store.ErrDuplicate)
		}
		return fmt.Errorf("create provider model: %w", err)
	}

	if model.ID == 0 {
		id, err := res.LastInsertId()
		if err != nil {
			return fmt.Errorf("create provider model: last insert id: %w", err)
		}
		model.ID = id
	}
	return nil
}

func (s *providerModelStore) ByID(ctx context.Context, id int64) (*store.ProviderModel, error) {
	row := s.e.QueryRowContext(ctx, `
SELECT `+providerModelColumns+` FROM provider_models WHERE id = ?`, id)
	model, err := scanProviderModel(row)
	if err != nil {
		return nil, readErr("get provider model", err)
	}
	return model, nil
}

func (s *providerModelStore) ByProvider(ctx context.Context, providerID string) ([]*store.ProviderModel, error) {
	rows, err := s.e.QueryContext(ctx, `
SELECT `+providerModelColumns+` FROM provider_models WHERE provider_id = ? ORDER BY id ASC`, providerID)
	if err != nil {
		return nil, fmt.Errorf("by provider %s: %w", providerID, err)
	}
	defer rows.Close()

	out := make([]*store.ProviderModel, 0)
	for rows.Next() {
		m, err := scanProviderModel(rows)
		if err != nil {
			return nil, fmt.Errorf("by provider %s: %w", providerID, err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("by provider %s: %w", providerID, err)
	}
	return out, nil
}

func (s *providerModelStore) Update(ctx context.Context, model *store.ProviderModel) error {
	efforts, err := encodeEfforts(model.Efforts)
	if err != nil {
		return fmt.Errorf("update provider model %d: %w", model.ID, err)
	}

	model.UpdatedAt = nowUTC()
	res, err := s.e.ExecContext(ctx, `
UPDATE provider_models
SET provider_id = ?, model_id = ?, context_window = ?, image_input = ?, pdf_input = ?, video_input = ?, efforts = ?, enabled = ?, updated_at = ?
WHERE id = ?`,
		model.ProviderID, model.ModelID, model.ContextWindow,
		boolInt(model.ImageInput), boolInt(model.PDFInput), boolInt(model.VideoInput),
		efforts, boolInt(model.Enabled),
		formatTime(model.UpdatedAt), model.ID,
	)
	if err != nil {
		if isUniqueConstraintErr(err) {
			return fmt.Errorf("update provider model %d: %w", model.ID, store.ErrDuplicate)
		}
		return fmt.Errorf("update provider model %d: %w", model.ID, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("update provider model %d: %w", model.ID, store.ErrNotFound)
	}
	return nil
}

func (s *providerModelStore) Delete(ctx context.Context, id int64) error {
	res, err := s.e.ExecContext(ctx, `DELETE FROM provider_models WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete provider model %d: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("delete provider model %d: %w", id, store.ErrNotFound)
	}
	return nil
}

func (s *providerModelStore) DeleteByProvider(ctx context.Context, providerID string) error {
	_, err := s.e.ExecContext(ctx, `DELETE FROM provider_models WHERE provider_id = ?`, providerID)
	if err != nil {
		return fmt.Errorf("delete provider models for %s: %w", providerID, err)
	}
	return nil
}

func (s *providerModelStore) UpsertMissing(ctx context.Context, models []*store.ProviderModel) (int, error) {
	if len(models) == 0 {
		return 0, nil
	}
	inserted := 0
	for _, m := range models {
		stampCreated(&m.CreatedAt, &m.UpdatedAt)
		efforts, err := encodeEfforts(m.Efforts)
		if err != nil {
			return inserted, fmt.Errorf("upsert missing model %s for %s: %w", m.ModelID, m.ProviderID, err)
		}
		res, err := s.e.ExecContext(ctx, `
INSERT INTO provider_models (provider_id, model_id, context_window, image_input, pdf_input, video_input, efforts, enabled, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (provider_id, model_id) DO NOTHING`,
			m.ProviderID, m.ModelID, m.ContextWindow,
			boolInt(m.ImageInput), boolInt(m.PDFInput), boolInt(m.VideoInput),
			efforts, boolInt(m.Enabled),
			formatTime(m.CreatedAt), formatTime(m.UpdatedAt),
		)
		if err != nil {
			return inserted, fmt.Errorf("upsert missing model %s for %s: %w", m.ModelID, m.ProviderID, err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return inserted, fmt.Errorf("upsert missing model %s for %s: %w", m.ModelID, m.ProviderID, err)
		}
		if n > 0 {
			inserted++
			if id, err := res.LastInsertId(); err == nil && id != 0 {
				m.ID = id
			}
		}
	}
	return inserted, nil
}

func scanProviderModel(row scanner) (*store.ProviderModel, error) {
	var m store.ProviderModel
	var efforts, createdAt, updatedAt string

	err := row.Scan(
		&m.ID,
		&m.ProviderID,
		&m.ModelID,
		&m.ContextWindow,
		&m.ImageInput,
		&m.PDFInput,
		&m.VideoInput,
		&efforts,
		&m.Enabled,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		return nil, err
	}
	if m.Efforts, err = decodeEfforts(efforts); err != nil {
		return nil, err
	}
	if m.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, err
	}
	if m.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return nil, err
	}
	return &m, nil
}

// encodeEfforts encodes a model row's reasoning-effort declaration for the
// efforts column (design D2): "" when undeclared, otherwise the JSON string
// array in declared order.
func encodeEfforts(efforts []string) (string, error) {
	if len(efforts) == 0 {
		return "", nil
	}
	return jsonMarshal(efforts)
}

// decodeEfforts decodes the efforts column back into the declaration: ""
// reads as nil (undeclared); malformed JSON is a read error.
func decodeEfforts(raw string) ([]string, error) {
	if raw == "" {
		return nil, nil
	}
	var efforts []string
	if err := jsonUnmarshal(raw, &efforts); err != nil {
		return nil, err
	}
	return efforts, nil
}
