package domain

import (
	"fmt"
	"strings"
	"time"
)

// InstanceOAuthApp is the instance's registered OAuth app for one provider
// (add-connection-oauth design.md D2): one app per provider per instance —
// self-hosted operators register their own provider app, and every workspace
// authorizes through it while the consented tokens stay per-connection
// workspace rows.
//
// Credential handling follows the ProviderConfig precedent: ClientSecret is
// write-only — it crosses the API boundary on the way in, never on the way
// out. At rest it exists only as ClientSecretCiphertext, an AES-256-GCM
// envelope sealed with the instance master key and the EMPTY additional
// authenticated data — the instance-scoped derivation (instance hooks are
// workspace-unscoped and bind their secrets to the empty AAD; this row is
// instance-scoped the same way). Reads carry presence and the last-4
// ClientSecretHint only. The redirect URI shown to the operator is derived
// from the instance's public base URL at read time (design.md D2), never
// stored — changing the base URL cannot desync the pane.
type InstanceOAuthApp struct {
	// Provider is the recipe provider id the app serves (e.g. "atlassian");
	// it is the row's identity — one app per provider.
	Provider string `json:"provider"`
	// ClientID is the provider-issued client identifier (public).
	ClientID string `json:"client_id"`
	// ClientSecretCiphertext is the client secret's encrypted envelope.
	// Never serialized and never returned by any read.
	ClientSecretCiphertext string `json:"-"`
	// ClientSecretHint is the secret's last-4 display hint; empty when no
	// secret is stored.
	ClientSecretHint string    `json:"client_secret_hint,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// Validate checks the app structurally: provider and client id present. The
// client secret's presence is lifecycle state, not shape — an update may
// legitimately re-save with only the hint carried through — so the ciphertext
// is not validated here; the service layer refuses a FIRST registration
// without a secret.
func (a *InstanceOAuthApp) Validate() error {
	if a == nil {
		return ErrInvalid
	}
	if strings.TrimSpace(a.Provider) == "" {
		return fmt.Errorf("%w: provider cannot be empty", ErrInvalid)
	}
	if strings.TrimSpace(a.ClientID) == "" {
		return fmt.Errorf("%w: client id cannot be empty", ErrInvalid)
	}
	return nil
}
