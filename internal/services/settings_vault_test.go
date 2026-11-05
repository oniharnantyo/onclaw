package services

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/store/fake"
	"github.com/oniharnantyo/buddies/internal/vault"
)

// newTestVault builds a vault service over a fake store and a fixed key —
// the same shape internal/vault's own service tests use.
func newTestVault(t *testing.T) *vault.Service {
	t.Helper()
	key := make([]byte, vault.KeySize)
	for i := range key {
		key[i] = byte(i + 1)
	}
	return vault.NewService(fake.New().VaultItems(), &stubVaultKeys{key: key})
}

// stubVaultKeys is a fixed KeyProvider for the settings-layer vault tests.
type stubVaultKeys struct {
	key []byte
}

func (k *stubVaultKeys) Key() ([]byte, error) { return k.key, nil }

// TestSettingsVault_SaveRevealRemoveRoundTrip proves the settings surface's
// full round-trip over the fake store: a saved login lists redacted, reveals
// its password, and removes.
func TestSettingsVault_SaveRevealRemoveRoundTrip(t *testing.T) {
	ctx := context.Background()
	svc := NewSettingsService(fake.New(), WithVault(newTestVault(t)))

	require.NoError(t, svc.SaveVaultLogin(ctx, "Example", "https://example.com", "ada", "s3cret", "user"))

	logins, err := svc.VaultLogins(ctx)
	require.NoError(t, err)
	require.Len(t, logins, 1)
	got := logins[0]
	require.Equal(t, "Example", got.Name)
	require.Equal(t, "ada", got.Username)
	require.Equal(t, "https://example.com", got.Origin)
	require.Equal(t, "user", got.AddedBy)
	require.NotEmpty(t, got.ID)

	// The redacted shape: the serialized row must never carry a password.
	raw, err := json.Marshal(got)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "password")

	secret, err := svc.RevealVaultLogin(ctx, got.ID)
	require.NoError(t, err)
	require.Equal(t, "s3cret", secret)

	require.NoError(t, svc.RemoveVaultLogin(ctx, got.ID))
	logins, err = svc.VaultLogins(ctx)
	require.NoError(t, err)
	require.Empty(t, logins)
}

// TestSettingsVault_SaveValidation proves the settings-layer guards: origin,
// username, and password are required.
func TestSettingsVault_SaveValidation(t *testing.T) {
	ctx := context.Background()
	svc := NewSettingsService(fake.New(), WithVault(newTestVault(t)))

	require.Error(t, svc.SaveVaultLogin(ctx, "", "", "ada", "pw", ""))
	require.Error(t, svc.SaveVaultLogin(ctx, "", "https://example.com", "", "pw", ""))
	require.Error(t, svc.SaveVaultLogin(ctx, "", "https://example.com", "ada", "", ""))
}

// TestSettingsVault_SaveDerivesNameFromOrigin proves an empty name derives
// from the origin's host.
func TestSettingsVault_SaveDerivesNameFromOrigin(t *testing.T) {
	ctx := context.Background()
	svc := NewSettingsService(fake.New(), WithVault(newTestVault(t)))

	require.NoError(t, svc.SaveVaultLogin(ctx, "", "https://app.example.com/login", "ada", "pw", ""))

	logins, err := svc.VaultLogins(ctx)
	require.NoError(t, err)
	require.Len(t, logins, 1)
	require.Equal(t, "app.example.com", logins[0].Name)
}

// TestSettingsVault_UnwiredVaultRefuses proves the unwired default refuses
// with a readable error instead of panicking.
func TestSettingsVault_UnwiredVaultRefuses(t *testing.T) {
	ctx := context.Background()
	svc := NewSettingsService(fake.New())

	_, err := svc.VaultLogins(ctx)
	require.Error(t, err)
	require.Error(t, svc.SaveVaultLogin(ctx, "", "https://example.com", "ada", "pw", ""))
	require.Error(t, svc.RemoveVaultLogin(ctx, "id"))
	_, err = svc.RevealVaultLogin(ctx, "id")
	require.Error(t, err)
}

// TestSettingsVault_UpsertKeepsOneRow proves the (origin, username) natural
// key: a second save over the same pair updates in place — one row remains,
// and the reveal answers the new password.
func TestSettingsVault_UpsertKeepsOneRow(t *testing.T) {
	ctx := context.Background()
	svc := NewSettingsService(fake.New(), WithVault(newTestVault(t)))

	require.NoError(t, svc.SaveVaultLogin(ctx, "Example", "https://example.com", "ada", "first", "user"))
	require.NoError(t, svc.SaveVaultLogin(ctx, "Example", "https://example.com", "ada", "second", "user"))

	logins, err := svc.VaultLogins(ctx)
	require.NoError(t, err)
	require.Len(t, logins, 1)

	secret, err := svc.RevealVaultLogin(ctx, logins[0].ID)
	require.NoError(t, err)
	require.Equal(t, "second", secret)
}
