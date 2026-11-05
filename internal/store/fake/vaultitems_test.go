package fake_test

import (
	"context"
	"testing"

	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/stretchr/testify/require"
)

// TestVaultItems_CRUD round-trips the fake's vault item store: Save
// assigns ID and stamps, reads never alias internal state (the sealed
// blob is copied), List orders by creation, and absent paths are the
// store sentinel — the same contract the sqlite adapter satisfies.
func TestVaultItems_CRUD(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

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
	createdAt := item.CreatedAt

	got, err := s.VaultItems().ByID(ctx, item.ID)
	require.NoError(t, err)
	require.Equal(t, item.SecretBlob, got.SecretBlob)

	// Mutating a returned blob must not corrupt the fake's state.
	got.SecretBlob[0] = 0x00
	again, err := s.VaultItems().ByID(ctx, item.ID)
	require.NoError(t, err)
	require.Equal(t, []byte{0x01, 0x02, 0xFE, 0xFF}, again.SecretBlob)

	pair, err := s.VaultItems().ByOriginUsername(ctx, "https://example.com", "ada")
	require.NoError(t, err)
	require.Equal(t, item.ID, pair.ID)

	item.SecretBlob = []byte{0xAA}
	require.NoError(t, s.VaultItems().Save(ctx, item))
	updated, err := s.VaultItems().ByID(ctx, item.ID)
	require.NoError(t, err)
	require.Equal(t, []byte{0xAA}, updated.SecretBlob)
	require.True(t, updated.CreatedAt.Equal(createdAt), "the upsert must keep the original CreatedAt")

	items, err := s.VaultItems().List(ctx)
	require.NoError(t, err)
	require.Len(t, items, 1)

	require.NoError(t, s.VaultItems().Remove(ctx, item.ID))
	_, err = s.VaultItems().ByID(ctx, item.ID)
	require.ErrorIs(t, err, store.ErrNotFound)
	require.ErrorIs(t, s.VaultItems().Remove(ctx, item.ID), store.ErrNotFound)
	_, err = s.VaultItems().ByOriginUsername(ctx, "https://example.com", "ada")
	require.ErrorIs(t, err, store.ErrNotFound)
}
