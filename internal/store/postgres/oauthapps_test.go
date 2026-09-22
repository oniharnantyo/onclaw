//go:build integration

package postgres_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// The instance OAuth apps store (add-connection-oauth tasks.md 1.4): rows are
// instance-scoped and provider-unique; Upsert is the PUT — identity and
// created_at fixed at birth, credentials replaced, updated_at advanced.
// Mirrors the fake's oauthapps_test.go semantics.
func TestIntegration_OAuthAppStore_UpsertGetList(t *testing.T) {
	s, _, ctx := setupTestSchema(t)

	app := &domain.InstanceOAuthApp{
		Provider:               "atlassian",
		ClientID:               "Aa1Bb2Cc3Dd4",
		ClientSecretCiphertext: "v1:Y2xpZW50:ZW52ZWxvcGU",
		ClientSecretHint:       "t4",
	}
	if err := s.OAuthApps().Upsert(ctx, app); err != nil {
		t.Fatalf("unexpected upsert error: %v", err)
	}
	if app.CreatedAt.IsZero() || app.UpdatedAt.IsZero() {
		t.Fatal("expected timestamps to be set")
	}

	got, err := s.OAuthApps().Get(ctx, "atlassian")
	if err != nil {
		t.Fatalf("unexpected get error: %v", err)
	}
	if got.Provider != "atlassian" || got.ClientID != app.ClientID || got.ClientSecretCiphertext != app.ClientSecretCiphertext || got.ClientSecretHint != app.ClientSecretHint {
		t.Fatalf("unexpected app: %+v", got)
	}

	// Upsert replaces credentials, keeps identity + created_at, advances
	// updated_at.
	firstCreatedAt := got.CreatedAt
	replacement := &domain.InstanceOAuthApp{
		Provider:               "atlassian",
		ClientID:               "Zz9Yy8Xx7",
		ClientSecretCiphertext: "v1:bmV3:ZW52ZWxvcGU",
		ClientSecretHint:       "w7",
	}
	if err := s.OAuthApps().Upsert(ctx, replacement); err != nil {
		t.Fatalf("unexpected replacement upsert error: %v", err)
	}
	got, err = s.OAuthApps().Get(ctx, "atlassian")
	if err != nil {
		t.Fatalf("unexpected get error: %v", err)
	}
	if got.ClientID != "Zz9Yy8Xx7" || got.ClientSecretCiphertext != replacement.ClientSecretCiphertext || got.ClientSecretHint != "w7" {
		t.Fatalf("expected credentials replaced, got %+v", got)
	}
	if !got.CreatedAt.Equal(firstCreatedAt) {
		t.Fatalf("expected created_at fixed at birth, got %v vs %v", got.CreatedAt, firstCreatedAt)
	}
	if !got.UpdatedAt.After(firstCreatedAt) {
		t.Fatalf("expected updated_at to advance, got %v <= %v", got.UpdatedAt, firstCreatedAt)
	}

	// Validation and empty-scope guards mirror the fake.
	if err := s.OAuthApps().Upsert(ctx, nil); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for nil app, got %v", err)
	}
	if err := s.OAuthApps().Upsert(ctx, &domain.InstanceOAuthApp{Provider: "slack"}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for missing client id, got %v", err)
	}
	if _, err := s.OAuthApps().Get(ctx, "slack"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unregistered provider, got %v", err)
	}
	if _, err := s.OAuthApps().Get(ctx, ""); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for empty provider, got %v", err)
	}

	// List is provider-ordered and instance-wide.
	for _, p := range []string{"slack", "linear"} {
		if err := s.OAuthApps().Upsert(ctx, &domain.InstanceOAuthApp{
			Provider: p,
			ClientID: "client-" + p,
		}); err != nil {
			t.Fatalf("unexpected upsert error for %s: %v", p, err)
		}
	}
	apps, err := s.OAuthApps().List(ctx)
	if err != nil {
		t.Fatalf("unexpected list error: %v", err)
	}
	providers := make([]string, 0, len(apps))
	for _, a := range apps {
		providers = append(providers, a.Provider)
	}
	if !slices.Equal(providers, []string{"atlassian", "linear", "slack"}) {
		t.Fatalf("expected provider-ordered [atlassian linear slack], got %v", providers)
	}
}
