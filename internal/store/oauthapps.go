package store

import (
	"context"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// OAuthApps manages the instance's per-provider OAuth app registrations
// (add-connection-oauth design.md D2). These rows are instance-scoped — the
// master tenant boundary, NOT workspace rows: one registered app per provider
// serves every workspace's authorization flows. There is no workspace scope
// and no workspace backfill.
//
// The stored row never carries the client secret in plaintext: the service
// layer seals it with the instance master key and the EMPTY AAD (the
// instance-scoped derivation, mirroring instance hooks) into
// ClientSecretCiphertext and stores only that envelope plus the last-4
// ClientSecretHint. The redirect URI is derived from the instance's public
// base URL at read time — never stored.
type OAuthApps interface {
	// Upsert create-or-replaces the provider's app: an existing row keeps its
	// CreatedAt and Provider identity while ClientID, the secret envelope, and
	// the hint are replaced and UpdatedAt advances. The provider is the
	// unique key. Invalid rows return domain.ErrInvalid.
	Upsert(ctx context.Context, app *domain.InstanceOAuthApp) error
	// Get returns the provider's registered app; domain.ErrNotFound when the
	// provider has none — the gallery's missing-registration signal.
	Get(ctx context.Context, provider string) (*domain.InstanceOAuthApp, error)
	// List returns every registered app ordered by provider id.
	List(ctx context.Context) ([]domain.InstanceOAuthApp, error)
}
