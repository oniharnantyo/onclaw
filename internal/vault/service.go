package vault

import (
	"context"
	"errors"
	"fmt"

	"github.com/oniharnantyo/buddies/internal/store"
)

// Service is the vault facade (add-browser-login-handover design D5):
// redacted listings, (origin, username) upsert semantics, the
// settings-only reveal, and the origin matcher the browser handover path
// consults. The plaintext secret never appears in a listing, a log line,
// or an error string.
type Service struct {
	items store.VaultItemStore
	keys  KeyProvider
}

// NewService returns a vault service over the item store and the key
// provider. Both dependencies are required — a nil dependency is a wiring
// bug, not a runtime condition.
func NewService(items store.VaultItemStore, keys KeyProvider) *Service {
	return &Service{items: items, keys: keys}
}

// SaveLogin seals and stores a login. The normalized (origin, username)
// pair is the natural key: a known pair updates the password in place and
// keeps the stored name and addedBy (design D5); a new pair inserts.
// Returns the redacted view of the saved row — receipts show the ID,
// never the secret.
func (s *Service) SaveLogin(ctx context.Context, origin, username, password, name, addedBy string) (*LoginView, error) {
	origin = NormalizeOrigin(origin)
	if origin == "" || username == "" {
		return nil, fmt.Errorf("save login: origin and username are required")
	}
	existing, err := s.items.ByOriginUsername(ctx, origin, username)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("save login: %w", err)
	}

	key, err := s.keys.Key()
	if err != nil {
		return nil, fmt.Errorf("save login: %w", err)
	}
	sealed, err := Seal(key, []byte(password))
	if err != nil {
		return nil, fmt.Errorf("save login: %w", err)
	}

	if existing == nil {
		item := &store.VaultItem{
			Kind:       store.VaultKindLogin,
			Name:       name,
			Origin:     origin,
			Username:   username,
			AddedBy:    addedBy,
			SecretBlob: sealed,
		}
		if err := s.items.Save(ctx, item); err != nil {
			return nil, fmt.Errorf("save login: %w", err)
		}
		view := viewOf(item)
		return &view, nil
	}

	// Same origin+username: the password updates; name and addedBy stay.
	existing.SecretBlob = sealed
	if err := s.items.Save(ctx, existing); err != nil {
		return nil, fmt.Errorf("save login: %w", err)
	}
	view := viewOf(existing)
	return &view, nil
}

// Logins returns every login as a redacted view — never the password — in
// the store's stable order (creation time, then ID). The result is never
// nil.
func (s *Service) Logins(ctx context.Context) ([]LoginView, error) {
	items, err := s.items.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list logins: %w", err)
	}
	return loginViews(items), nil
}

// LoginsForOrigin returns the redacted logins whose stored pattern may be
// used on the page origin (OriginMatches semantics) — the helper the
// browser handover path consults before offering a saved login.
func (s *Service) LoginsForOrigin(ctx context.Context, origin string) ([]LoginView, error) {
	items, err := s.items.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list logins for origin: %w", err)
	}
	matching := make([]store.VaultItem, 0, len(items))
	for _, item := range items {
		if item.Kind == store.VaultKindLogin && OriginMatches(item.Origin, origin) {
			matching = append(matching, item)
		}
	}
	return loginViews(matching), nil
}

// RemoveLogin deletes a login; ErrNotFound when absent.
func (s *Service) RemoveLogin(ctx context.Context, id string) error {
	if err := s.items.Remove(ctx, id); err != nil {
		return fmt.Errorf("remove login: %w", err)
	}
	return nil
}

// RevealLogin returns the login's decrypted password — the settings-only
// consumer (design D5). The caller owns the value from here: never log
// it, never serialize it.
func (s *Service) RevealLogin(ctx context.Context, id string) (string, error) {
	item, err := s.items.ByID(ctx, id)
	if err != nil {
		return "", fmt.Errorf("reveal login: %w", err)
	}
	key, err := s.keys.Key()
	if err != nil {
		return "", fmt.Errorf("reveal login: %w", err)
	}
	plain, err := Open(key, item.SecretBlob)
	if err != nil {
		return "", fmt.Errorf("reveal login: %w", err)
	}
	return string(plain), nil
}

// loginViews projects the items onto redacted views, keeping the store's
// order. The result is never nil.
func loginViews(items []store.VaultItem) []LoginView {
	out := make([]LoginView, 0, len(items))
	for _, item := range items {
		if item.Kind != store.VaultKindLogin {
			continue
		}
		out = append(out, viewOf(&item))
	}
	return out
}

// viewOf projects a stored item onto its redacted view: the secret blob —
// already opaque ciphertext — never crosses.
func viewOf(item *store.VaultItem) LoginView {
	return LoginView{
		ID:       item.ID,
		Name:     item.Name,
		Username: item.Username,
		Origin:   item.Origin,
		AddedBy:  item.AddedBy,
	}
}
