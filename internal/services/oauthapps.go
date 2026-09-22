// Path: internal/services/oauthapps.go — instance OAuth app administration
// (add-connection-oauth tasks 2.6): register or update one OAuth app per
// provider, seal the client secret with the instance-scoped key derivation,
// and serve the hint-only views with the derived redirect URI.
package services

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/secrets"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// OAuthAppsService administers the instance's per-provider OAuth app
// registrations (design.md D2): one app per provider serves every workspace's
// authorization flows. The client secret is write-only — it crosses the API
// boundary on the way in, sealed with the instance master key and the EMPTY
// additional authenticated data (the instance-scoped derivation, the instance
// hooks precedent) — and reads carry presence plus the last-4 hint only. The
// redirect URI shown to the operator derives from the instance public base URL
// at read time, never stored.
type OAuthAppsService struct {
	apps          store.OAuthApps
	encKey        []byte
	publicBaseURL string
}

// NewOAuthAppsService builds the service from its granular dependencies.
func NewOAuthAppsService(apps store.OAuthApps, encKey []byte, publicBaseURL string) *OAuthAppsService {
	return &OAuthAppsService{apps: apps, encKey: encKey, publicBaseURL: publicBaseURL}
}

// OAuthAppView is the app's read model: the stored row plus the derived
// redirect URI. The client secret itself never appears — the embedded
// domain.InstanceOAuthApp already omits the ciphertext from JSON and carries
// the last-4 hint.
type OAuthAppView struct {
	domain.InstanceOAuthApp
	RedirectURI string `json:"redirect_uri"`
}

// RedirectURI returns the redirect URI to configure at the provider, derived
// from the instance public base URL. Empty when no base URL is configured —
// the pane surfaces the gap instead of a misleading URI.
func (s *OAuthAppsService) RedirectURI() string {
	return DeriveOAuthRedirectURI(s.publicBaseURL)
}

// Upsert create-or-replaces the provider's app (the PUT semantics): client
// id and secret are replaced, created_at is fixed at birth, updated_at
// advances. The provider must name a registered OAuth recipe — a typo must
// fail loudly, not silently create an app no recipe can use. The secret is
// required on the FIRST registration; an update may omit it to keep the
// stored credential (the hint-only round trip).
func (s *OAuthAppsService) Upsert(ctx context.Context, provider, clientID, clientSecret string) (*OAuthAppView, error) {
	provider = strings.TrimSpace(provider)
	recipe := domain.RecipeByID(provider)
	if recipe == nil || recipe.AuthKind != domain.RecipeAuthOAuth {
		return nil, fmt.Errorf("%w: %q is not an OAuth integration provider", domain.ErrInvalid, provider)
	}
	clientID = strings.TrimSpace(clientID)

	app := &domain.InstanceOAuthApp{Provider: provider, ClientID: clientID}
	if err := app.Validate(); err != nil {
		return nil, err
	}

	clientSecret = strings.TrimSpace(clientSecret)
	if clientSecret == "" {
		// An update without a secret keeps the stored credential; a first
		// registration has nothing to keep and is refused (domain contract:
		// the secret is lifecycle state, not shape).
		existing, err := s.apps.Get(ctx, provider)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				return nil, fmt.Errorf("%w: client secret is required to register the app", domain.ErrInvalid)
			}
			return nil, err
		}
		app.ClientSecretCiphertext = existing.ClientSecretCiphertext
		app.ClientSecretHint = existing.ClientSecretHint
	} else {
		// The instance-scoped derivation (contract §4): EMPTY AAD — this row
		// is instance-scoped, workspace-unscoped like the instance hooks.
		sealed, err := secrets.Encrypt(s.encKey, nil, []byte(clientSecret))
		if err != nil {
			return nil, err
		}
		app.ClientSecretCiphertext = sealed
		app.ClientSecretHint = domain.GenerateKeyHint(clientSecret)
	}

	if err := s.apps.Upsert(ctx, app); err != nil {
		return nil, err
	}
	return newOAuthAppView(app, s.RedirectURI()), nil
}

// Get returns one registered app as its view; an unregistered provider is
// domain.ErrNotFound.
func (s *OAuthAppsService) Get(ctx context.Context, provider string) (*OAuthAppView, error) {
	app, err := s.apps.Get(ctx, strings.TrimSpace(provider))
	if err != nil {
		return nil, err
	}
	return newOAuthAppView(app, s.RedirectURI()), nil
}

// List returns every registered app as views, provider-ordered.
func (s *OAuthAppsService) List(ctx context.Context) ([]OAuthAppView, error) {
	apps, err := s.apps.List(ctx)
	if err != nil {
		return nil, err
	}
	views := make([]OAuthAppView, 0, len(apps))
	for i := range apps {
		views = append(views, *newOAuthAppView(&apps[i], s.RedirectURI()))
	}
	return views, nil
}

// newOAuthAppView projects a stored row into its read model.
func newOAuthAppView(app *domain.InstanceOAuthApp, redirectURI string) *OAuthAppView {
	return &OAuthAppView{InstanceOAuthApp: *app, RedirectURI: redirectURI}
}
