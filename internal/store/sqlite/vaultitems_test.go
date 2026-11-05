package sqlite

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/stretchr/testify/require"
)

// TestVaultItems_CRUD round-trips the vault item aggregate: Save assigns
// ID and stamps, ByID/ByOriginUsername read back the sealed blob verbatim,
// List orders by creation, Remove deletes, and every absent read is the
// store sentinel.
func TestVaultItems_CRUD(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	item := &store.VaultItem{
		Kind:       store.VaultKindLogin,
		Name:       "Example",
		Origin:     "https://example.com",
		Username:   "ada",
		AddedBy:    "atlas",
		SecretBlob: []byte{0x01, 0x02, 0xFE, 0xFF},
	}
	require.NoError(t, s.VaultItems().Save(ctx, item))
	require.NotEmpty(t, item.ID)
	require.False(t, item.CreatedAt.IsZero())
	require.False(t, item.UpdatedAt.IsZero())
	createdAt := item.CreatedAt

	t.Run("by id round-trips the blob", func(t *testing.T) {
		got, err := s.VaultItems().ByID(ctx, item.ID)
		require.NoError(t, err)
		require.Equal(t, store.VaultKindLogin, got.Kind)
		require.Equal(t, "Example", got.Name)
		require.Equal(t, "https://example.com", got.Origin)
		require.Equal(t, "ada", got.Username)
		require.Equal(t, "atlas", got.AddedBy)
		require.Equal(t, item.SecretBlob, got.SecretBlob)
	})

	t.Run("by origin+username finds the pair", func(t *testing.T) {
		got, err := s.VaultItems().ByOriginUsername(ctx, "https://example.com", "ada")
		require.NoError(t, err)
		require.Equal(t, item.ID, got.ID)
	})

	t.Run("absent reads are ErrNotFound", func(t *testing.T) {
		_, err := s.VaultItems().ByID(ctx, "missing")
		require.ErrorIs(t, err, store.ErrNotFound)
		_, err = s.VaultItems().ByOriginUsername(ctx, "https://example.com", "nobody")
		require.ErrorIs(t, err, store.ErrNotFound)
	})

	t.Run("list orders by creation", func(t *testing.T) {
		second := &store.VaultItem{Kind: store.VaultKindLogin, Name: "Other",
			Origin: "https://other.example", Username: "grace", SecretBlob: []byte{0x09}}
		require.NoError(t, s.VaultItems().Save(ctx, second))

		items, err := s.VaultItems().List(ctx)
		require.NoError(t, err)
		require.GreaterOrEqual(t, len(items), 2)
		require.Equal(t, item.ID, items[0].ID)
		require.Equal(t, second.ID, items[1].ID)
		for i := 1; i < len(items); i++ {
			require.False(t, items[i].CreatedAt.Before(items[i-1].CreatedAt))
		}
	})

	t.Run("upsert rewrites in place keeping created_at", func(t *testing.T) {
		item.SecretBlob = []byte{0xAA}
		require.NoError(t, s.VaultItems().Save(ctx, item))

		got, err := s.VaultItems().ByID(ctx, item.ID)
		require.NoError(t, err)
		require.Equal(t, []byte{0xAA}, got.SecretBlob)
		require.True(t, got.CreatedAt.Equal(createdAt), "the upsert must keep the original created_at")
	})

	t.Run("remove deletes", func(t *testing.T) {
		require.NoError(t, s.VaultItems().Remove(ctx, item.ID))
		_, err := s.VaultItems().ByID(ctx, item.ID)
		require.ErrorIs(t, err, store.ErrNotFound)
		require.ErrorIs(t, s.VaultItems().Remove(ctx, item.ID), store.ErrNotFound)
	})

	t.Run("the sealed blob never serializes", func(t *testing.T) {
		raw, err := json.Marshal(item)
		require.NoError(t, err)
		require.NotContains(t, string(raw), "secret_blob",
			"the sealed blob carries the json:\"-\" discipline (CredentialSet precedent)")
	})
}
