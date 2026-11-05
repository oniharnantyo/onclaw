package store

import "context"

// VaultItemStore persists the vault's items (add-browser-login-handover
// design D5): metadata in the clear, secrets as sealed blobs the store
// treats as opaque bytes. Absent rows read as ErrNotFound.
type VaultItemStore interface {
	// Save create-or-replaces the item keyed by ID: an empty ID is
	// assigned by the store and both timestamps are stamped; a present ID
	// rewrites the mutable columns (kind, name, origin, username, added_by,
	// secret blob) in place, keeping the original CreatedAt.
	Save(ctx context.Context, item *VaultItem) error
	// ByID returns the item with the given ID; ErrNotFound when absent.
	ByID(ctx context.Context, id string) (*VaultItem, error)
	// ByOriginUsername returns the item stored for the origin+username
	// pair — the vault's natural key (design D5: same origin+username ⇒
	// password update); ErrNotFound when absent.
	ByOriginUsername(ctx context.Context, origin, username string) (*VaultItem, error)
	// List returns every item ordered by creation time, then ID. The
	// result is never nil.
	List(ctx context.Context) ([]VaultItem, error)
	// Remove deletes the item; ErrNotFound when absent.
	Remove(ctx context.Context, id string) error
}
