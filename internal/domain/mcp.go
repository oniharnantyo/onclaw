package domain

import (
	"fmt"
	"strings"
	"time"
)

// MCP transports. Exactly one connection shape is meaningful per transport:
// stdio executes a local command; streamable HTTP and SSE connect to a URL.
const (
	MCPTransportStdio          = "stdio"
	MCPTransportStreamableHTTP = "streamable_http"
	MCPTransportSSE            = "sse"
)

// MCP connection auth modes (add-mcp-oauth-client spec: "MCP connection auth
// mode"). none is the default — the configured static header/env rows only,
// behavior unchanged from before OAuth existed; an empty AuthMode is stored
// and read as none so every pre-OAuth row is byte-identical. oauth dials the
// URL transports with the MCP OAuth client (bearer injection, discovery,
// token refresh) and is invalid for stdio.
const (
	MCPAuthModeNone  = "none"
	MCPAuthModeOAuth = "oauth"
)

// MCP connection probe statuses persisted on server rows. An empty status
// means the server has not been probed yet.
//
// expired (add-mcp-oauth-client design.md D6) extends the pair with the
// OAuth lifecycle state mirroring the connection statuses: it is entered
// ONLY by a failed token refresh — never by a probe or dial outcome, so a
// discarded attempt can never mask the recovery state — and it is left ONLY
// by reauthorization. A failed refresh fails open (the stored token still
// dials); the provider's error detail rides StatusError.
const (
	MCPStatusConnected = "connected"
	MCPStatusError     = "error"
	MCPStatusExpired   = "expired"
)

// ErrMCPServerNameTaken indicates an MCP server name is already used by
// another server within the same scope (workspace or agent, case-insensitive).
// It chains to ErrConflict so generic conflict mapping keeps working.
var ErrMCPServerNameTaken = fmt.Errorf("%w: mcp server name already taken", ErrConflict)

// EnvRow is one name-keyed row of an MCP connection's environment variables or
// HTTP headers. Names are the stable merge key for secret handling; values may
// carry secrets and are encrypted by the settings service before storage.
type EnvRow struct {
	Name  string `json:"name"`
	Value string `json:"value,omitempty"`
}

// MCPConnection is the transport-specific connection configuration shared by
// workspace-registered and agent-private MCP servers. Only the fields of the
// configured transport are meaningful; the others are ignored.
type MCPConnection struct {
	Transport string   `json:"transport"`
	Command   string   `json:"command,omitempty"`
	Args      []string `json:"args,omitempty"`
	Env       []EnvRow `json:"env,omitempty"`
	URL       string   `json:"url,omitempty"`
	Headers   []EnvRow `json:"headers,omitempty"`
	// AuthMode is one of the MCPAuthMode* constants; empty is none (the
	// pre-OAuth default — static rows only, byte-identical shape).
	AuthMode string `json:"auth_mode,omitempty"`
	// OAuthClientID is the bring-your-own app's pre-registered client id —
	// meaningful only in oauth mode. Empty with oauth mode means the client
	// identifies itself through discovery (metadata document or DCR).
	OAuthClientID string `json:"oauth_client_id,omitempty"`
	// OAuthClientSecret is the BYO confidential app's client secret —
	// meaningful only in oauth mode, and only with OAuthClientID (an id
	// without a secret is a public client and authorizes with PKCE). It
	// follows the secret-row convention: plaintext at the API edge, a
	// workspace-AAD AES-256-GCM envelope at rest, a last-4 hint on reads,
	// and the plaintext is sent only to the token endpoint — never logged,
	// never echoed beyond the hint.
	OAuthClientSecret string `json:"oauth_client_secret,omitempty"`
}

// Validate checks the connection against its transport: the transport must be
// one of the supported constants, stdio requires a command, streamable HTTP
// and SSE require a URL, and the transport's row names (env for stdio, headers
// for the URL transports) must be non-empty and unique within the connection.
// Fields belonging to the other transports are ignored. The auth mode must be
// one of the catalog values (empty = none); oauth mode requires a URL
// transport, and the BYO client rows are meaningful only there.
func (c *MCPConnection) Validate() error {
	if c == nil {
		return ErrInvalid
	}
	if err := ValidateMCPAuthMode(c.AuthMode); err != nil {
		return err
	}
	hasBYO := c.OAuthClientID != "" || c.OAuthClientSecret != ""
	switch c.Transport {
	case MCPTransportStdio:
		if c.Command == "" {
			return fmt.Errorf("%w: command is required for %s transport", ErrInvalid, MCPTransportStdio)
		}
		if c.AuthMode == MCPAuthModeOAuth {
			return fmt.Errorf("%w: auth mode %s requires a URL transport (%s or %s)", ErrInvalid, MCPAuthModeOAuth, MCPTransportStreamableHTTP, MCPTransportSSE)
		}
		if hasBYO {
			return fmt.Errorf("%w: oauth client rows are only valid with auth mode %s", ErrInvalid, MCPAuthModeOAuth)
		}
		return validateEnvRowNames(c.Env)
	case MCPTransportStreamableHTTP, MCPTransportSSE:
		if c.URL == "" {
			return fmt.Errorf("%w: url is required for %s transport", ErrInvalid, c.Transport)
		}
		if c.AuthMode == MCPAuthModeOAuth {
			if c.OAuthClientSecret != "" && c.OAuthClientID == "" {
				return fmt.Errorf("%w: oauth client secret requires a client id", ErrInvalid)
			}
		} else if hasBYO {
			return fmt.Errorf("%w: oauth client rows are only valid with auth mode %s", ErrInvalid, MCPAuthModeOAuth)
		}
		return validateEnvRowNames(c.Headers)
	default:
		return fmt.Errorf("%w: transport %q must be one of %s, %s, or %s", ErrInvalid, c.Transport, MCPTransportStdio, MCPTransportStreamableHTTP, MCPTransportSSE)
	}
}

// ValidateMCPAuthMode reports whether the given auth mode is one of the
// catalog values (empty is the pre-OAuth default and reads as none).
func ValidateMCPAuthMode(mode string) error {
	switch mode {
	case "", MCPAuthModeNone, MCPAuthModeOAuth:
		return nil
	default:
		return fmt.Errorf("%w: auth mode %q must be %s or %s", ErrInvalid, mode, MCPAuthModeNone, MCPAuthModeOAuth)
	}
}

// IsValidMCPAuthMode reports whether the given auth mode is one of the
// catalog values.
func IsValidMCPAuthMode(mode string) bool {
	return ValidateMCPAuthMode(mode) == nil
}

// ValidateMCPStatus reports whether the given status is one of the catalog
// values (empty is the not-yet-probed shape). expired is OAuth-lifecycle
// only: the service layer restricts its writes to the refresh-failure path,
// mirroring the connection-status contract.
func ValidateMCPStatus(status string) error {
	switch status {
	case "", MCPStatusConnected, MCPStatusError, MCPStatusExpired:
		return nil
	default:
		return fmt.Errorf("%w: mcp status %q must be %s, %s, or %s", ErrInvalid, status, MCPStatusConnected, MCPStatusError, MCPStatusExpired)
	}
}

// IsValidMCPStatus reports whether the given status is one of the catalog
// values.
func IsValidMCPStatus(status string) bool {
	return ValidateMCPStatus(status) == nil
}

// CanTransitionMCPStatus reports whether the from→to status move is a legal
// server-status transition (add-mcp-oauth-client design.md D6), mirroring
// CanTransitionConnectionStatus:
//
//   - connected ↔ error mirror probe and dial outcomes;
//   - expired is entered from a live status (connected or error) or held
//     idempotently under repeated refresh failure — only refresh failure
//     enters it, which the service layer enforces;
//   - expired is left only to connected by reauthorization — expired→error
//     is refused so a discarded probe or dial attempt can never mask the
//     recovery state.
func CanTransitionMCPStatus(from, to string) bool {
	if !IsValidMCPStatus(from) || !IsValidMCPStatus(to) {
		return false
	}
	if from == to {
		return true
	}
	if to == MCPStatusExpired {
		return from == MCPStatusConnected || from == MCPStatusError
	}
	if from == MCPStatusExpired {
		return to == MCPStatusConnected
	}
	return true
}

// validateEnvRowNames checks that every row has a non-blank name and that
// names are unique (exactly, case-sensitively) within the list.
func validateEnvRowNames(rows []EnvRow) error {
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		if strings.TrimSpace(row.Name) == "" {
			return fmt.Errorf("%w: env/header row name cannot be empty", ErrInvalid)
		}
		if _, dup := seen[row.Name]; dup {
			return fmt.Errorf("%w: env/header row name %q is duplicated", ErrInvalid, row.Name)
		}
		seen[row.Name] = struct{}{}
	}
	return nil
}

// WorkspaceMCPServer is one workspace-registered MCP server: a shared,
// admin-managed registry entry agents opt into through Agent.EnabledMCPS.
type WorkspaceMCPServer struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	Name        string `json:"name"`
	MCPConnection
	Enabled     bool   `json:"enabled"`
	Status      string `json:"status"`
	StatusError string `json:"status_error,omitempty"`
	ToolCount   int    `json:"tool_count"`
	// OriginConnectionID is the connection that materialized this server
	// (add-workspace-connections design.md D1); empty for hand-made servers.
	// The marker is birth-stamped: the connection is the single authority over
	// a managed server's URL, secret rows, and lifecycle (design.md D11), so
	// it is never rewritten through Update.
	OriginConnectionID string    `json:"origin_connection_id,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// Validate checks the server structurally: workspace scope and name present,
// and the embedded connection valid for its transport.
func (s *WorkspaceMCPServer) Validate() error {
	if s == nil {
		return ErrInvalid
	}
	if s.WorkspaceID == "" {
		return fmt.Errorf("%w: workspace id cannot be empty", ErrInvalid)
	}
	if strings.TrimSpace(s.Name) == "" {
		return fmt.Errorf("%w: name cannot be empty", ErrInvalid)
	}
	return s.MCPConnection.Validate()
}

// AgentMCPServer is one agent-private MCP server, addressed and usable only by
// its owning agent. It dies with the agent.
type AgentMCPServer struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	AgentID     string `json:"agent_id"`
	Name        string `json:"name"`
	MCPConnection
	Enabled     bool      `json:"enabled"`
	Status      string    `json:"status"`
	StatusError string    `json:"status_error,omitempty"`
	ToolCount   int       `json:"tool_count"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Validate checks the server structurally: workspace and agent scope, name,
// and the embedded connection valid for its transport.
func (s *AgentMCPServer) Validate() error {
	if s == nil {
		return ErrInvalid
	}
	if s.WorkspaceID == "" {
		return fmt.Errorf("%w: workspace id cannot be empty", ErrInvalid)
	}
	if s.AgentID == "" {
		return fmt.Errorf("%w: agent id cannot be empty", ErrInvalid)
	}
	if strings.TrimSpace(s.Name) == "" {
		return fmt.Errorf("%w: name cannot be empty", ErrInvalid)
	}
	return s.MCPConnection.Validate()
}

// MCPToken scope kinds (add-mcp-oauth-client design.md D4): the stored OAuth
// token set's ownership tier. workspace tokens back workspace-registered
// servers (AgentID empty); agent tokens back agent-private servers and ride
// the owning agent's id.
const (
	MCPTokenScopeWorkspace = "workspace"
	MCPTokenScopeAgent     = "agent"
)

// MCPToken is one MCP server row's stored OAuth token set (add-mcp-oauth-client
// design.md D4): a dedicated store row keyed by (scope kind, server id),
// separate from the user-visible header/env rows so the UI's secret-row model
// (hint/last-4, manual edit) stays meaningful while refresh mutates the tokens
// invisibly. Envelopes use the same derivation as the connection refresh
// ciphertext: AES-256-GCM under the instance master key with the workspace ID
// as AAD. Token material never serializes to clients — presence reads only —
// and is never logged.
type MCPToken struct {
	ID string `json:"id"`
	// WorkspaceID is the tenant partition — every token query carries it.
	WorkspaceID string `json:"workspace_id"`
	// AgentID is the owning agent for agent-scope tokens; empty means
	// workspace scope (ScopeKind reports which).
	AgentID string `json:"agent_id,omitempty"`
	// ServerID is the MCP server row the tokens dial for (workspace_mcp_servers
	// or agent_mcp_servers per the scope kind). The server row's deletion
	// cascades the token row.
	ServerID string `json:"server_id"`
	// AccessTokenCiphertext is the access token's envelope. Required: a token
	// row without an access token is meaningless.
	AccessTokenCiphertext string `json:"-"`
	// RefreshTokenCiphertext is the refresh token's envelope; empty for
	// providers that issue no refresh token (no refresh, expiry-only).
	RefreshTokenCiphertext string     `json:"-"`
	ExpiresAt              *time.Time `json:"expires_at,omitempty"`
	// GrantedScopes lists the scopes the provider granted; always an array.
	GrantedScopes []string `json:"granted_scopes"`
	// Issuer is the authorization server issuer the tokens were issued by
	// (RFC 8414 issuer; RFC 9207 iss checks bind refreshes to it).
	Issuer    string    `json:"issuer,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ScopeKind reports the token row's ownership tier: MCPTokenScopeAgent when
// it carries an owning agent, MCPTokenScopeWorkspace otherwise.
func (t *MCPToken) ScopeKind() string {
	if t == nil || t.AgentID == "" {
		return MCPTokenScopeWorkspace
	}
	return MCPTokenScopeAgent
}

// Validate checks the token row structurally: workspace scope, server id, and
// the access envelope present; agent-scope rows name their owning agent.
func (t *MCPToken) Validate() error {
	if t == nil {
		return ErrInvalid
	}
	if t.WorkspaceID == "" {
		return fmt.Errorf("%w: workspace id cannot be empty", ErrInvalid)
	}
	if t.ServerID == "" {
		return fmt.Errorf("%w: server id cannot be empty", ErrInvalid)
	}
	if t.AccessTokenCiphertext == "" {
		return fmt.Errorf("%w: access token envelope cannot be empty", ErrInvalid)
	}
	return nil
}
