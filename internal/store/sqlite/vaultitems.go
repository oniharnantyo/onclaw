package sqlite

import (
	"context"
	"fmt"

	"github.com/oniharnantyo/buddies/internal/store"
)

type vaultItemStore struct{ e executor }

const vaultItemColumns = `id, kind, name, origin, username, added_by, secret_blob, created_at, updated_at`

// Save create-or-replaces the vault item keyed by ID: an empty ID is
// assigned by the store and both timestamps stamped; a present ID rewrites
// the mutable columns in place, keeping the original created_at.
func (s *vaultItemStore) Save(ctx context.Context, item *store.VaultItem) error {
	if item.ID == "" {
		id, err := newID()
		if err != nil {
			return fmt.Errorf("save vault item: %w", err)
		}
		item.ID = id
	}
	stampCreated(&item.CreatedAt, &item.UpdatedAt)

	_, err := s.e.ExecContext(ctx, `
INSERT INTO vault_items (`+vaultItemColumns+`)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
kind = excluded.kind, name = excluded.name, origin = excluded.origin,
username = excluded.username, added_by = excluded.added_by,
secret_blob = excluded.secret_blob, updated_at = excluded.updated_at`,
		item.ID, item.Kind, item.Name, item.Origin, item.Username, item.AddedBy,
		item.SecretBlob, formatTime(item.CreatedAt), formatTime(item.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("save vault item %s: %w", item.ID, err)
	}
	return nil
}

// ByID returns the item with the given ID; ErrNotFound when absent.
func (s *vaultItemStore) ByID(ctx context.Context, id string) (*store.VaultItem, error) {
	row := s.e.QueryRowContext(ctx,
		`SELECT `+vaultItemColumns+` FROM vault_items WHERE id = ?`, id)
	item, err := scanVaultItem(row)
	if err != nil {
		return nil, readErr("get vault item", err)
	}
	return item, nil
}

// ByOriginUsername returns the item stored for the origin+username pair —
// the vault's natural upsert key; ErrNotFound when absent.
func (s *vaultItemStore) ByOriginUsername(ctx context.Context, origin, username string) (*store.VaultItem, error) {
	row := s.e.QueryRowContext(ctx,
		`SELECT `+vaultItemColumns+` FROM vault_items WHERE origin = ? AND username = ?`, origin, username)
	item, err := scanVaultItem(row)
	if err != nil {
		return nil, readErr("get vault item by origin+username", err)
	}
	return item, nil
}

// List returns every item ordered by creation time, then ID.
func (s *vaultItemStore) List(ctx context.Context) ([]store.VaultItem, error) {
	rows, err := s.e.QueryContext(ctx,
		`SELECT `+vaultItemColumns+` FROM vault_items ORDER BY created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("list vault items: %w", err)
	}
	defer rows.Close()

	out := make([]store.VaultItem, 0)
	for rows.Next() {
		item, err := scanVaultItem(rows)
		if err != nil {
			return nil, fmt.Errorf("list vault items: %w", err)
		}
		out = append(out, *item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list vault items: %w", err)
	}
	return out, nil
}

// Remove deletes the item; ErrNotFound when absent.
func (s *vaultItemStore) Remove(ctx context.Context, id string) error {
	res, err := s.e.ExecContext(ctx, `DELETE FROM vault_items WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("remove vault item %s: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("remove vault item %s: %w", id, store.ErrNotFound)
	}
	return nil
}

// scanVaultItem scans one vault_items row from a single- or multi-row
// result.
func scanVaultItem(row scanner) (*store.VaultItem, error) {
	var item store.VaultItem
	var createdAt, updatedAt string
	if err := row.Scan(&item.ID, &item.Kind, &item.Name, &item.Origin, &item.Username,
		&item.AddedBy, &item.SecretBlob, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	var err error
	if item.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, err
	}
	if item.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return nil, err
	}
	return &item, nil
}
