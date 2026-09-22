package domain

import (
	"fmt"
	"strings"
	"time"
)

// Connection access levels (design.md D5). The level is connection metadata:
// it drives the guided token scopes and the gallery display, while actual
// write enforcement rides the token's own scopes.
const (
	ConnectionAccessReadOnly  = "read_only"
	ConnectionAccessReadWrite = "read_write"
)

// Connection statuses persisted on the connection row (add-connection-oauth
// design.md D6). connected and error mirror the materialized server's probe
// statuses; expired is the OAuth-only state a failed refresh enters — the MCP
// runtime cannot write it, so an expired connection stays visibly recoverable
// no matter what later probe outcomes say. An empty status means "connected"
// at persistence: the store adapters default it, keeping pre-OAuth rows and
// callers valid.
const (
	ConnectionStatusConnected = "connected"
	ConnectionStatusError     = "error"
	ConnectionStatusExpired   = "expired"
)

// ErrUnknownRecipe indicates a connect attempt referencing a recipe id that is
// not registered (workspace-connections spec: "Unknown recipe rejected").
// It chains to ErrInvalid so generic validation mapping keeps working.
var ErrUnknownRecipe = fmt.Errorf("%w: unknown integration recipe", ErrInvalid)

// ErrConnectionExists indicates the workspace already holds a connection for
// the requested service — one connection per service per workspace (design.md
// D6). It chains to ErrConflict so generic conflict mapping keeps working.
var ErrConnectionExists = fmt.Errorf("%w: service already connected in workspace", ErrConflict)

// Connection is one workspace-scoped service connection (the GitHub
// integration of this workspace): the product object that owns a materialized
// workspace MCP server through the server's origin marker. The access token
// itself never lives here — it is the secret header row on the materialized
// server, read back only as a last-4 hint. The OAuth lifecycle state
// (add-connection-oauth design.md D1) does: the refresh-token ciphertext
// envelope, the access-token expiry, the consented scopes, and the status.
type Connection struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	// Service is the recipe id the connection was created from (e.g. "github").
	Service string `json:"service"`
	// AccessLevel is one of the ConnectionAccess* constants.
	AccessLevel string `json:"access_level"`
	// Status is one of the ConnectionStatus* constants (empty is stored as
	// connected). expired is set only by refresh failure and cleared only by
	// reauthorization (design.md D6).
	Status string `json:"status"`
	// RefreshCiphertext is the refresh token's AES-256-GCM envelope (the
	// instance master key, workspace ID as AAD — the same derivation as the
	// server's secret rows). Empty for PAT connections and never serialized;
	// the plaintext refresh token is never stored anywhere.
	RefreshCiphertext string `json:"-"`
	// ExpiresAt is the stored access token's expiry; nil for connections
	// whose token does not expire (PAT).
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	// GrantedScopes lists the scopes the provider consented to (OAuth
	// connections); empty for PAT connections.
	GrantedScopes []string  `json:"granted_scopes"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// Validate checks the connection structurally: workspace scope and service
// present, the access level in the catalog, and the status empty or in the
// catalog (empty is the pre-OAuth shape and is stored as connected).
func (c *Connection) Validate() error {
	if c == nil {
		return ErrInvalid
	}
	if c.WorkspaceID == "" {
		return fmt.Errorf("%w: workspace id cannot be empty", ErrInvalid)
	}
	if strings.TrimSpace(c.Service) == "" {
		return fmt.Errorf("%w: service cannot be empty", ErrInvalid)
	}
	if c.Status != "" {
		if err := ValidateConnectionStatus(c.Status); err != nil {
			return err
		}
	}
	return ValidateConnectionAccessLevel(c.AccessLevel)
}

// ValidateConnectionAccessLevel reports whether the given access level is one
// of the catalog values.
func ValidateConnectionAccessLevel(level string) error {
	switch level {
	case ConnectionAccessReadOnly, ConnectionAccessReadWrite:
		return nil
	default:
		return fmt.Errorf("%w: access level %q must be %s or %s", ErrInvalid, level, ConnectionAccessReadOnly, ConnectionAccessReadWrite)
	}
}

// IsValidConnectionAccessLevel reports whether the given access level is one
// of the catalog values.
func IsValidConnectionAccessLevel(level string) bool {
	return ValidateConnectionAccessLevel(level) == nil
}

// ValidateConnectionStatus reports whether the given status is one of the
// catalog values.
func ValidateConnectionStatus(status string) error {
	switch status {
	case ConnectionStatusConnected, ConnectionStatusError, ConnectionStatusExpired:
		return nil
	default:
		return fmt.Errorf("%w: status %q must be %s, %s, or %s", ErrInvalid, status, ConnectionStatusConnected, ConnectionStatusError, ConnectionStatusExpired)
	}
}

// IsValidConnectionStatus reports whether the given status is one of the
// catalog values.
func IsValidConnectionStatus(status string) bool {
	return ValidateConnectionStatus(status) == nil
}

// CanTransitionConnectionStatus reports whether the from→to status move is a
// legal connection-status transition (add-connection-oauth design.md D6):
//
//   - connected ↔ error mirror the materialized server's probe outcomes;
//   - expired is entered from a live status (connected or error) or held
//     idempotently under repeated refresh failure — only refresh failure
//     enters it, which the service layer enforces;
//   - expired is left only to connected by reauthorization — expired→error
//     is refused so a discarded probe attempt (store-nothing-on-failure,
//     design.md D5) can never mask the recovery state.
func CanTransitionConnectionStatus(from, to string) bool {
	if !IsValidConnectionStatus(from) || !IsValidConnectionStatus(to) {
		return false
	}
	if from == to {
		return true
	}
	if to == ConnectionStatusExpired {
		return from == ConnectionStatusConnected || from == ConnectionStatusError
	}
	if from == ConnectionStatusExpired {
		return to == ConnectionStatusConnected
	}
	return true
}
