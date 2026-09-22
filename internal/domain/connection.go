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
// workspace MCP server through the server's origin marker. The token itself
// never lives here — it is the secret header row on the materialized server,
// read back only as a last-4 hint.
type Connection struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	// Service is the recipe id the connection was created from (e.g. "github").
	Service string `json:"service"`
	// AccessLevel is one of the ConnectionAccess* constants.
	AccessLevel string    `json:"access_level"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Validate checks the connection structurally: workspace scope and service
// present, and the access level in the catalog.
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
