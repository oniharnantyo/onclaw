package mcp

import (
	"context"
	"errors"
	"fmt"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// The dial-time OAuth auth step (add-mcp-oauth-client design.md D1): an
// oauth-mode connection resolves a usable bearer BEFORE the transport opens —
// refresh-on-dial semantics with fail-open inside the resolution — and a dial
// with no usable token returns the typed needs-authorization signal. Callers
// (the manager's status persistence, the probe endpoints) translate the signal
// into the server row's status detail; it is never a run failure (the runner
// already degrades per-server errors without failing the run). Static-mode
// connections never touch the seam: no OAuth machinery runs for auth mode
// none, byte-identical to the pre-OAuth dial.

// ErrAuthorizationRequired marks an oauth-mode dial whose server row has no
// usable stored credential: the row needs (re)authorization from the MCP
// servers pane. errors.Is is the family test; the wrapped detail carries the
// actionable guidance. It surfaces as the row's status detail — never a run
// failure — and clears when a reauthorization completes.
var ErrAuthorizationRequired = errors.New("mcp server requires OAuth authorization")

// OAuthDialCredentials is the dial-time credential resolution seam: it yields
// the access token one server row's oauth-mode dial should inject. The
// services layer implements it over the oauth.Client and the token store
// (services.NewMCPOAuthDialCredentials); the composition root wires it into
// the manager and the probe handlers. Implementations must return an error
// wrapping ErrAuthorizationRequired when the row has no usable credential;
// discovery and client-resolution failures surface as their own typed errors
// (the row's errored detail per the spec's undiscoverable/registration-refusal
// scenarios).
type OAuthDialCredentials interface {
	// BearerForDial resolves the bearer for the (workspace, agent, server)
	// row; an empty agentID selects the workspace scope.
	BearerForDial(ctx context.Context, workspaceID, agentID, serverID string) (string, error)
}

// connectAuthorized dials ref with the OAuth auth step: oauth-mode
// connections resolve their bearer through the credential seam and dial with
// it joined onto the static header rows; every other connection dials exactly
// as before. The credentials seam must be wired wherever an oauth-mode row can
// reach this path (the composition root does; the manager option and the probe
// handlers carry it) — an unwired seam behind an oauth-mode row is a
// composition bug and fails loudly.
func connectAuthorized(ctx context.Context, ref Ref, creds OAuthDialCredentials) (*connection, error) {
	conn := ref.Conn
	if conn.AuthMode == domain.MCPAuthModeOAuth {
		token, err := creds.BearerForDial(ctx, ref.WorkspaceID, ref.AgentID, ref.ServerID)
		if err != nil {
			return nil, err
		}
		conn = withBearer(conn, token)
	}
	return connect(ctx, conn)
}

// withBearer returns a copy of conn with the dial's bearer joined onto the
// header rows as the LAST Authorization row (design.md D1): the URL
// transports flatten the rows into a header map, so a static Authorization
// row is overridden by the live token while every other static header still
// applies alongside.
func withBearer(conn domain.MCPConnection, token string) domain.MCPConnection {
	conn.Headers = append(
		append([]domain.EnvRow{}, conn.Headers...),
		domain.EnvRow{Name: "Authorization", Value: "Bearer " + token},
	)
	return conn
}

// needsAuthorizationDetail is the actionable detail the credential seam wraps
// onto ErrAuthorizationRequired.
const needsAuthorizationDetail = "no usable OAuth credential is stored for this server — start (or re-start) its sign-in from the MCP servers pane"

// NewAuthorizationRequiredError builds the seam's needs-authorization error
// with the shared guidance detail.
func NewAuthorizationRequiredError() error {
	return fmt.Errorf("%w: %s", ErrAuthorizationRequired, needsAuthorizationDetail)
}

// IsAuthorizationRequired reports whether err is (or wraps) the
// needs-authorization signal.
func IsAuthorizationRequired(err error) bool {
	return errors.Is(err, ErrAuthorizationRequired)
}
