package services

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/oniharnantyo/buddies/internal/vault"
)

// The Saved-logins methods on SettingsService (add-browser-login-handover
// design D5, spec settings "Browser Use pane"): a thin CRUD wrapper over the
// vault service, mirroring the CredentialSet row discipline — every struct
// crossing the Wails boundary is a redacted view; the plaintext secret
// surfaces only through the explicit reveal.
//
// The vault dependency is optional by constructor convention: unwired, the
// methods refuse with a readable error instead of panicking.

// VaultLogin is one saved login as the Browser Use pane's Saved logins
// section renders it (the CredentialSet discipline): a redacted view — no
// secret material — so it may cross the bridge freely. Reveal goes through
// RevealVaultLogin only.
type VaultLogin struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Username string `json:"username"`
	Origin   string `json:"origin"`
	// AddedBy records who handed the secret over (the agent name, or the
	// user through settings).
	AddedBy string `json:"added_by"`
}

// VaultLogins lists every saved login as a redacted view — never the
// password — in the store's stable order. The result is never nil. Unwired
// vault is a wiring bug: a readable error, not an empty listing.
func (s *SettingsService) VaultLogins(ctx context.Context) ([]VaultLogin, error) {
	if s.vault == nil {
		return nil, fmt.Errorf("list vault logins: vault is not available")
	}
	views, err := s.vault.Logins(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]VaultLogin, 0, len(views))
	for _, v := range views {
		out = append(out, VaultLogin{
			ID:       v.ID,
			Name:     v.Name,
			Username: v.Username,
			Origin:   v.Origin,
			AddedBy:  v.AddedBy,
		})
	}
	return out, nil
}

// SaveVaultLogin stores (or, for a known origin+username pair, updates) one
// login. Origin, username, and password are required; an empty name is
// derived from the origin's host. Upsert semantics live in the vault
// service (design D5).
func (s *SettingsService) SaveVaultLogin(ctx context.Context, name, origin, username, password, addedBy string) error {
	if s.vault == nil {
		return fmt.Errorf("save vault login: vault is not available")
	}
	if strings.TrimSpace(origin) == "" {
		return fmt.Errorf("save vault login: origin is required")
	}
	if strings.TrimSpace(username) == "" {
		return fmt.Errorf("save vault login: username is required")
	}
	if password == "" {
		return fmt.Errorf("save vault login: password is required")
	}
	if strings.TrimSpace(name) == "" {
		name = originHostName(origin)
	}
	if _, err := s.vault.SaveLogin(ctx, origin, username, password, name, addedBy); err != nil {
		return err
	}
	return nil
}

// RemoveVaultLogin deletes one saved login; an unknown id is an error.
func (s *SettingsService) RemoveVaultLogin(ctx context.Context, id string) error {
	if s.vault == nil {
		return fmt.Errorf("remove vault login: vault is not available")
	}
	return s.vault.RemoveLogin(ctx, id)
}

// RevealVaultLogin returns one saved login's decrypted password — the
// settings-only reveal (design D5). The caller owns the value from here: it
// must never be logged or persisted.
func (s *SettingsService) RevealVaultLogin(ctx context.Context, id string) (string, error) {
	if s.vault == nil {
		return "", fmt.Errorf("reveal vault login: vault is not available")
	}
	return s.vault.RevealLogin(ctx, id)
}

// WithVault supplies the vault service the Saved-logins and resolve-input
// surfaces use. A nil service is ignored — the unwired default stands
// (mirrors WithMCPInvalidate).
func WithVault(v *vault.Service) SettingsOption {
	return func(s *SettingsService) {
		if v != nil {
			s.vault = v
		}
	}
}

// originHostName extracts the host an origin URL names, for deriving a
// login's display name. An unparseable origin falls back to the raw string.
func originHostName(origin string) string {
	u, err := url.Parse(origin)
	if err != nil || u.Hostname() == "" {
		return origin
	}
	return u.Hostname()
}
