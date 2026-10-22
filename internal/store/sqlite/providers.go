package sqlite

import (
	"context"
	"fmt"

	"github.com/oniharnantyo/buddies/internal/store"
)

type providerStore struct{ e executor }

const providerColumns = `id, type, name, base_url, api_key, enabled, created_at, updated_at`

func (p *providerStore) Create(ctx context.Context, provider *store.Provider) error {
	if provider.ID == "" {
		id, err := newID()
		if err != nil {
			return fmt.Errorf("create provider: %w", err)
		}
		provider.ID = id
	}
	stampCreated(&provider.CreatedAt, &provider.UpdatedAt)

	_, err := p.e.ExecContext(ctx, `
INSERT INTO providers (`+providerColumns+`)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		provider.ID, provider.Type, provider.Name, provider.BaseURL, provider.APIKey,
		boolInt(provider.Enabled), formatTime(provider.CreatedAt), formatTime(provider.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("create provider: %w", err)
	}
	return nil
}

func (p *providerStore) ByID(ctx context.Context, id string) (*store.Provider, error) {
	row := p.e.QueryRowContext(ctx, `SELECT `+providerColumns+` FROM providers WHERE id = ?`, id)
	provider, err := scanProvider(row)
	if err != nil {
		return nil, readErr("get provider", err)
	}
	return provider, nil
}

func (p *providerStore) List(ctx context.Context) ([]store.Provider, error) {
	rows, err := p.e.QueryContext(ctx,
		`SELECT `+providerColumns+` FROM providers ORDER BY created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("list providers: %w", err)
	}
	defer rows.Close()

	providers := make([]store.Provider, 0)
	for rows.Next() {
		provider, err := scanProvider(rows)
		if err != nil {
			return nil, fmt.Errorf("list providers: %w", err)
		}
		providers = append(providers, *provider)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list providers: %w", err)
	}
	return providers, nil
}

func (p *providerStore) Update(ctx context.Context, provider *store.Provider) error {
	provider.UpdatedAt = nowUTC()

	res, err := p.e.ExecContext(ctx, `
UPDATE providers SET type = ?, name = ?, base_url = ?, api_key = ?, enabled = ?, updated_at = ?
WHERE id = ?`,
		provider.Type, provider.Name, provider.BaseURL, provider.APIKey,
		boolInt(provider.Enabled), formatTime(provider.UpdatedAt), provider.ID,
	)
	if err != nil {
		return fmt.Errorf("update provider: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("update provider %s: %w", provider.ID, store.ErrNotFound)
	}
	return nil
}

func (p *providerStore) Delete(ctx context.Context, id string) error {
	res, err := p.e.ExecContext(ctx, `DELETE FROM providers WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete provider: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("delete provider %s: %w", id, store.ErrNotFound)
	}
	return nil
}

func scanProvider(row scanner) (*store.Provider, error) {
	var provider store.Provider
	var createdAt, updatedAt string

	err := row.Scan(&provider.ID, &provider.Type, &provider.Name, &provider.BaseURL,
		&provider.APIKey, &provider.Enabled, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	if provider.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, err
	}
	if provider.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return nil, err
	}
	return &provider, nil
}
