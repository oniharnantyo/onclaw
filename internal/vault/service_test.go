package vault

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/oniharnantyo/buddies/internal/store/fake"
	"github.com/stretchr/testify/require"
)

// stubKeys is a fixed KeyProvider for service tests.
type stubKeys struct {
	key []byte
	err error
}

func (k *stubKeys) Key() ([]byte, error) {
	if k.err != nil {
		return nil, k.err
	}
	return k.key, nil
}

func newTestService(t *testing.T) *Service {
	t.Helper()
	key := make([]byte, KeySize)
	for i := range key {
		key[i] = byte(i + 1)
	}
	return NewService(fake.New().VaultItems(), &stubKeys{key: key})
}

// TestService_SaveLogin_UpsertSemantics covers design D5's natural key:
// the same origin+username updates the password in place (name and
// addedBy stay); a different username or origin inserts a new row.
func TestService_SaveLogin_UpsertSemantics(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)

	first, err := s.SaveLogin(ctx, "https://example.com", "ada", "hunter2", "Example", "atlas")
	require.NoError(t, err)
	require.NotEmpty(t, first.ID)
	require.Equal(t, "https://example.com", first.Origin)
	require.Equal(t, "ada", first.Username)
	require.Equal(t, "Example", first.Name)
	require.Equal(t, "atlas", first.AddedBy)

	password, err := s.RevealLogin(ctx, first.ID)
	require.NoError(t, err)
	require.Equal(t, "hunter2", password)

	t.Run("same origin+username updates the password", func(t *testing.T) {
		again, err := s.SaveLogin(ctx, "https://example.com", "ada", "n3w-p4ss", "Renamed", "other-agent")
		require.NoError(t, err)
		require.Equal(t, first.ID, again.ID, "the pair must update the existing row")
		require.Equal(t, "Example", again.Name, "the stored name must stay")
		require.Equal(t, "atlas", again.AddedBy, "the stored addedBy must stay")

		password, err := s.RevealLogin(ctx, first.ID)
		require.NoError(t, err)
		require.Equal(t, "n3w-p4ss", password)

		logins, err := s.Logins(ctx)
		require.NoError(t, err)
		require.Len(t, logins, 1, "the upsert must not duplicate")
	})

	t.Run("different username inserts", func(t *testing.T) {
		other, err := s.SaveLogin(ctx, "https://example.com", "grace", "g-pass", "Example", "atlas")
		require.NoError(t, err)
		require.NotEqual(t, first.ID, other.ID)

		logins, err := s.Logins(ctx)
		require.NoError(t, err)
		require.Len(t, logins, 2)
	})

	t.Run("different origin inserts", func(t *testing.T) {
		elsewhere, err := s.SaveLogin(ctx, "https://other.example", "ada", "o-pass", "Other", "atlas")
		require.NoError(t, err)
		require.NotEqual(t, first.ID, elsewhere.ID)

		logins, err := s.Logins(ctx)
		require.NoError(t, err)
		require.Len(t, logins, 3)
	})

	t.Run("origin is normalized before comparison", func(t *testing.T) {
		normalized, err := s.SaveLogin(ctx, "HTTPS://Example.COM/", "ada", "same-pass", "X", "Y")
		require.NoError(t, err)
		require.Equal(t, first.ID, normalized.ID, "case and trailing slash must not fork the pair")
	})

	t.Run("empty origin or username is rejected", func(t *testing.T) {
		_, err := s.SaveLogin(ctx, "", "ada", "p", "n", "a")
		require.Error(t, err)
		_, err = s.SaveLogin(ctx, "https://example.com", "", "p", "n", "a")
		require.Error(t, err)
	})
}

// TestService_Logins_RedactedShape covers the redaction invariant: the
// listing carries views only, and no JSON projection of the service's
// output ever contains the password.
func TestService_Logins_RedactedShape(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)

	_, err := s.SaveLogin(ctx, "https://example.com", "ada", "super-secret-password", "Example", "atlas")
	require.NoError(t, err)
	_, err = s.SaveLogin(ctx, "https://other.example", "grace", "another-secret", "Other", "atlas")
	require.NoError(t, err)

	logins, err := s.Logins(ctx)
	require.NoError(t, err)
	require.Len(t, logins, 2)
	require.Equal(t, "Example", logins[0].Name, "list order follows the store (creation, then id)")
	require.Equal(t, "Other", logins[1].Name)

	raw, err := json.Marshal(logins)
	require.NoError(t, err)
	blob := string(raw)
	require.NotContains(t, blob, "password", "no password field may exist on the view")
	require.NotContains(t, blob, "super-secret-password", "no plaintext may appear in the projection")
	require.NotContains(t, blob, "another-secret")

	// Even the domain type must never serialize the secret.
	login := Login{Password: "super-secret-password", Name: "Example"}
	raw, err = json.Marshal(login)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "password")
	require.NotContains(t, string(raw), "super-secret-password")
}

// TestService_LoginsForOrigin covers the handover path's helper: only the
// logins whose stored pattern matches the page origin are offered.
func TestService_LoginsForOrigin(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)

	example, err := s.SaveLogin(ctx, "https://example.com", "ada", "p1", "Example", "atlas")
	require.NoError(t, err)
	accounts, err := s.SaveLogin(ctx, "https://accounts.example.com", "sam", "p2", "Accounts", "atlas")
	require.NoError(t, err)
	_, err = s.SaveLogin(ctx, "https://evil.com", "mallory", "p3", "Evil", "atlas")
	require.NoError(t, err)
	httpLogin, err := s.SaveLogin(ctx, "http://insecure.example", "clicky", "p4", "HTTP", "atlas")
	require.NoError(t, err)

	matches, err := s.LoginsForOrigin(ctx, "https://login.example.com/auth")
	require.NoError(t, err)
	require.Len(t, matches, 2, "the registrable domain matches both example.com rows")
	require.Equal(t, example.ID, matches[0].ID)
	require.Equal(t, accounts.ID, matches[1].ID)

	matches, err = s.LoginsForOrigin(ctx, "https://evil.com")
	require.NoError(t, err)
	require.Len(t, matches, 1)

	matches, err = s.LoginsForOrigin(ctx, "http://example.com")
	require.NoError(t, err)
	require.Empty(t, matches, "http origins never match")
	require.Len(t, matches, 0)
	require.NotContains(t, viewIDs(matches), httpLogin.ID)
}

// TestService_RemoveAndReveal covers removal and the reveal sentinel.
func TestService_RemoveAndReveal(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)

	view, err := s.SaveLogin(ctx, "https://example.com", "ada", "p", "Example", "atlas")
	require.NoError(t, err)

	require.NoError(t, s.RemoveLogin(ctx, view.ID))
	_, err = s.RevealLogin(ctx, view.ID)
	require.ErrorIs(t, err, store.ErrNotFound)
	require.ErrorIs(t, s.RemoveLogin(ctx, view.ID), store.ErrNotFound)
}

// TestService_LoginsEmpty covers the empty-vault contract: the listing is
// empty, not nil.
func TestService_LoginsEmpty(t *testing.T) {
	s := newTestService(t)
	logins, err := s.Logins(context.Background())
	require.NoError(t, err)
	require.NotNil(t, logins)
	require.Empty(t, logins)
}

// TestService_KeyFailure surfaces the key resolution errors instead of
// storing plaintext.
func TestService_KeyFailure(t *testing.T) {
	ctx := context.Background()
	s := NewService(fake.New().VaultItems(), &stubKeys{err: errors.New("no keyring")})

	_, err := s.SaveLogin(ctx, "https://example.com", "ada", "p", "n", "a")
	require.Error(t, err)

	_, err = s.RevealLogin(ctx, "some-id")
	require.Error(t, err)

	logins, err := s.Logins(ctx)
	require.NoError(t, err)
	require.Empty(t, logins, "a failed seal must not store anything")
}

// viewIDs extracts view ids for assertions.
func viewIDs(views []LoginView) []string {
	out := make([]string, 0, len(views))
	for _, v := range views {
		out = append(out, v.ID)
	}
	return out
}

// guard: the service error strings must never carry secret material.
func TestService_ErrorsCarryNoSecret(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)

	_, err := s.SaveLogin(ctx, "", "ada", "the-secret-value", "n", "a")
	require.Error(t, err)
	require.False(t, strings.Contains(err.Error(), "the-secret-value"), "no value may appear in any error string")
}
