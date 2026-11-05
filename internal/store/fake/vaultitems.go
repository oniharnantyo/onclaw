package fake

import (
	"context"
	"fmt"
	"sort"

	"github.com/oniharnantyo/buddies/internal/store"
)

type vaultItemStore struct{ s *Store }

// Save create-or-replaces the vault item keyed by ID: an empty ID is
// assigned by the store and both timestamps stamped; a present ID rewrites
// the mutable columns in place, keeping the original CreatedAt.
func (f vaultItemStore) Save(_ context.Context, item *store.VaultItem) error {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()

	if item.ID == "" {
		item.ID = nextID()
	}
	if stored, ok := f.s.vaultItems[item.ID]; ok {
		item.CreatedAt = stored.CreatedAt
	}
	if item.UpdatedAt.IsZero() {
		item.UpdatedAt = nowUTC()
	}
	if item.CreatedAt.IsZero() {
		item.CreatedAt = item.UpdatedAt
	}
	f.s.vaultItems[item.ID] = cloneVaultItem(*item)
	return nil
}

// ByID returns the item with the given ID; ErrNotFound when absent.
func (f vaultItemStore) ByID(_ context.Context, id string) (*store.VaultItem, error) {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()

	item, ok := f.s.vaultItems[id]
	if !ok {
		return nil, fmt.Errorf("get vault item %s: %w", id, store.ErrNotFound)
	}
	out := cloneVaultItem(item)
	return &out, nil
}

// ByOriginUsername returns the item stored for the origin+username pair —
// the vault's natural upsert key; ErrNotFound when absent.
func (f vaultItemStore) ByOriginUsername(_ context.Context, origin, username string) (*store.VaultItem, error) {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()

	for _, item := range f.s.vaultItems {
		if item.Origin == origin && item.Username == username {
			out := cloneVaultItem(item)
			return &out, nil
		}
	}
	return nil, fmt.Errorf("get vault item by origin+username: %w", store.ErrNotFound)
}

// List returns every item ordered by creation time, then ID. The result is
// never nil.
func (f vaultItemStore) List(_ context.Context) ([]store.VaultItem, error) {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()

	out := make([]store.VaultItem, 0, len(f.s.vaultItems))
	for _, item := range f.s.vaultItems {
		out = append(out, cloneVaultItem(item))
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// Remove deletes the item; ErrNotFound when absent.
func (f vaultItemStore) Remove(_ context.Context, id string) error {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()

	if _, ok := f.s.vaultItems[id]; !ok {
		return fmt.Errorf("remove vault item %s: %w", id, store.ErrNotFound)
	}
	delete(f.s.vaultItems, id)
	return nil
}

// cloneVaultItem copies every value crossing the fake's boundary: the
// sealed blob is a byte slice and must never alias internal state.
func cloneVaultItem(in store.VaultItem) store.VaultItem {
	in.SecretBlob = cloneBytes(in.SecretBlob)
	return in
}
