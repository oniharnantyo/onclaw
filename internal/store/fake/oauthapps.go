package fake

import (
	"context"
	"sort"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// oauthAppStore implements store.OAuthApps in memory. Rows are
// instance-scoped (keyed by provider) — the master-tenant boundary, with no
// workspace partition anywhere in the port.
type oauthAppStore struct {
	s *fakeStore
}

// Upsert create-or-replaces the provider's app: identity (provider) and
// CreatedAt are fixed at birth, the credential fields are replaced, and
// UpdatedAt advances (the PUT semantics the instance-admin pane rides).
func (oas *oauthAppStore) Upsert(ctx context.Context, app *domain.InstanceOAuthApp) error {
	if app == nil {
		return domain.ErrInvalid
	}
	if err := app.Validate(); err != nil {
		return err
	}

	oas.s.mu.Lock()
	defer oas.s.mu.Unlock()

	now := time.Now().UTC()
	stored, exists := oas.s.oauthApps[app.Provider]
	if !exists {
		stored = &domain.InstanceOAuthApp{
			Provider:  app.Provider,
			CreatedAt: now,
		}
		oas.s.oauthApps[app.Provider] = stored
	}
	stored.ClientID = app.ClientID
	stored.ClientSecretCiphertext = app.ClientSecretCiphertext
	stored.ClientSecretHint = app.ClientSecretHint
	stored.UpdatedAt = now

	// Reflect the stored row back: the caller sees the birth timestamps and
	// the normalized identity.
	app.CreatedAt = stored.CreatedAt
	app.UpdatedAt = stored.UpdatedAt
	return nil
}

func (oas *oauthAppStore) Get(ctx context.Context, provider string) (*domain.InstanceOAuthApp, error) {
	if provider == "" {
		return nil, domain.ErrNotFound
	}

	oas.s.mu.RLock()
	defer oas.s.mu.RUnlock()

	app, exists := oas.s.oauthApps[provider]
	if !exists {
		return nil, domain.ErrNotFound
	}
	return cloneInstanceOAuthApp(app), nil
}

func (oas *oauthAppStore) List(ctx context.Context) ([]domain.InstanceOAuthApp, error) {
	oas.s.mu.RLock()
	defer oas.s.mu.RUnlock()

	apps := make([]domain.InstanceOAuthApp, 0, len(oas.s.oauthApps))
	for _, app := range oas.s.oauthApps {
		apps = append(apps, *cloneInstanceOAuthApp(app))
	}
	sort.Slice(apps, func(i, j int) bool {
		return apps[i].Provider < apps[j].Provider
	})
	return apps, nil
}
