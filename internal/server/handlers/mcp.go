package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/agents"
	"github.com/oniharnantyo/onclaw/internal/agents/mcp"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/secrets"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// DefaultMCPProbeTimeout bounds each probe's fresh dial, handshake, and tool
// listing (workspace-mcp delta: bounded ~10s so a save never hangs past the
// bound). The composition root may override it through the router options.
const DefaultMCPProbeTimeout = 10 * time.Second

// MCPInvalidator drops the connection manager's cached entry for one server so
// a mutating config write (update, delete — the master switch rides update)
// takes effect on the very next resolution (design.md D5). *mcp.MCPManager
// satisfies it; the narrow interface keeps this handler honest about the one
// capability it uses.
type MCPInvalidator interface {
	Invalidate(workspaceID, serverID string)
}

// ConnectionNamer names the connection that owns a managed server for the
// origin-marker pointer error (add-workspace-connections design.md D11).
// *services.ConnectionsService satisfies it; the narrow interface keeps this
// handler decoupled from the connections service.
type ConnectionNamer interface {
	ConnectionDisplayName(ctx context.Context, workspaceID, connectionID string) string
}

// ---------------------------------------------------------------------------
// Payload and view shapes (web api.ts McpServerPayload / ApiMcpServer)
// ---------------------------------------------------------------------------

// mcpSecretRowInput is one write-side env/header row. Values are write-only:
// an omitted or empty value keeps the stored secret; a non-empty value (or an
// already-encrypted envelope) replaces it.
type mcpSecretRowInput struct {
	Name  string `json:"name"`
	Value string `json:"value,omitempty"`
}

// mcpServerRequest is the shared create/update payload for MCP servers — the
// workspace registry and agent-private servers take the exact same shape.
type mcpServerRequest struct {
	Name      string              `json:"name"`
	Transport string              `json:"transport"`
	Command   string              `json:"command,omitempty"`
	Args      []string            `json:"args,omitempty"`
	Env       []mcpSecretRowInput `json:"env,omitempty"`
	URL       string              `json:"url,omitempty"`
	Headers   []mcpSecretRowInput `json:"headers,omitempty"`
	Enabled   *bool               `json:"enabled,omitempty"`
	// Auth mode (add-mcp-oauth-client): "none" (or empty — the default) for
	// the static rows above, "oauth" for bearer/refresh dials. An explicit
	// non-oauth mode on a patch clears the BYO client rows (domain validation
	// refuses them outside oauth mode).
	AuthMode string `json:"auth_mode,omitempty"`
	// OAuthClientID is the bring-your-own app's client id; a URL value routes
	// to the client-id metadata document strategy. OAuthClientSecret is the
	// confidential app's secret — write-only like the row secrets: an omitted
	// or empty value keeps the stored secret, a plaintext value replaces it
	// (encrypted at persistence with the workspace AAD).
	OAuthClientID     string `json:"oauth_client_id,omitempty"`
	OAuthClientSecret string `json:"oauth_client_secret,omitempty"`
}

// carriesConnection reports whether the payload touches the connection at all
// (a pure {enabled} pause/resume patch must not).
func (r *mcpServerRequest) carriesConnection() bool {
	return r.Transport != "" || r.Command != "" || r.Args != nil ||
		r.Env != nil || r.URL != "" || r.Headers != nil ||
		r.AuthMode != "" || r.OAuthClientID != "" || r.OAuthClientSecret != ""
}

// connection builds the domain connection for a full create or a transport
// switch.
func (r *mcpServerRequest) connection() domain.MCPConnection {
	conn := domain.MCPConnection{
		Transport:         r.Transport,
		Command:           r.Command,
		Args:              r.Args,
		URL:               r.URL,
		AuthMode:          r.AuthMode,
		OAuthClientID:     r.OAuthClientID,
		OAuthClientSecret: r.OAuthClientSecret,
	}
	conn.Env = envInputRows(r.Env)
	conn.Headers = envInputRows(r.Headers)
	return conn
}

func envInputRows(in []mcpSecretRowInput) []domain.EnvRow {
	if in == nil {
		return nil
	}
	out := make([]domain.EnvRow, 0, len(in))
	for _, row := range in {
		out = append(out, domain.EnvRow{Name: row.Name, Value: row.Value})
	}
	return out
}

// applyMCPServerPatch overlays the request's editable fields onto a loaded
// (runtime) row and reports whether anything was requested. Overlay rules
// follow the web contract: a blank name keeps the stored one; the master
// switch is taken only when present; switching the transport rebuilds the
// connection from the payload alone (the select reconfigures the form);
// otherwise connection fields overlay per-field, where nil row lists leave
// the stored rows untouched and blank values keep stored secrets through the
// service's name-keyed merge.
func applyMCPServerPatch(name *string, conn *domain.MCPConnection, enabled *bool, req *mcpServerRequest) bool {
	changed := false
	if trimmed := strings.TrimSpace(req.Name); trimmed != "" && trimmed != *name {
		*name = trimmed
		changed = true
	}
	if req.Enabled != nil && *req.Enabled != *enabled {
		*enabled = *req.Enabled
		changed = true
	}
	if req.carriesConnection() {
		if req.Transport != "" && req.Transport != conn.Transport {
			*conn = req.connection()
			return true
		}
		if req.Transport != "" {
			conn.Transport = req.Transport
			changed = true
		}
		if req.Command != "" {
			conn.Command = req.Command
			changed = true
		}
		if req.Args != nil {
			conn.Args = req.Args
			changed = true
		}
		if req.Env != nil {
			conn.Env = envInputRows(req.Env)
			changed = true
		}
		if req.URL != "" {
			conn.URL = req.URL
			changed = true
		}
		if req.Headers != nil {
			conn.Headers = envInputRows(req.Headers)
			changed = true
		}
		// Auth mode: an explicit value switches the mode; a switch OFF oauth
		// clears the BYO client rows (domain validation refuses them outside
		// oauth mode). An empty value keeps the stored mode.
		if req.AuthMode != "" && req.AuthMode != conn.AuthMode {
			conn.AuthMode = req.AuthMode
			changed = true
			if req.AuthMode != domain.MCPAuthModeOAuth {
				conn.OAuthClientID = ""
				conn.OAuthClientSecret = ""
			}
		}
		if req.OAuthClientID != "" && req.OAuthClientID != conn.OAuthClientID {
			conn.OAuthClientID = req.OAuthClientID
			changed = true
		}
		// The secret is write-only: an empty value keeps the stored envelope
		// (the service-side plaintext rule), a plaintext value replaces it.
		if req.OAuthClientSecret != "" && req.OAuthClientSecret != conn.OAuthClientSecret {
			conn.OAuthClientSecret = req.OAuthClientSecret
			changed = true
		}
	}
	return changed
}

// mcpSecretRowView is the read view of one env/header row: the write-only
// value never round-trips — reads carry only the stored last-4 hint.
type mcpSecretRowView struct {
	Name      string `json:"name"`
	ValueHint string `json:"value_hint,omitempty"`
}

// mcpServerView is the read view of one registered server (workspace or
// agent-private): hint-only secret rows, no plaintext or ciphertext.
type mcpServerView struct {
	ID          string             `json:"id"`
	WorkspaceID string             `json:"workspace_id,omitempty"`
	AgentID     string             `json:"agent_id,omitempty"`
	Name        string             `json:"name"`
	Transport   string             `json:"transport"`
	Command     string             `json:"command,omitempty"`
	Args        []string           `json:"args,omitempty"`
	Env         []mcpSecretRowView `json:"env,omitempty"`
	URL         string             `json:"url,omitempty"`
	Headers     []mcpSecretRowView `json:"headers,omitempty"`
	Enabled     bool               `json:"enabled"`
	Status      string             `json:"status"`
	StatusError string             `json:"status_error,omitempty"`
	ToolCount   int                `json:"tool_count"`
	CreatedAt   time.Time          `json:"created_at"`
	UpdatedAt   time.Time          `json:"updated_at"`
	// Auth mode (add-mcp-oauth-client): "none"/empty reads as the static
	// default; "oauth" rows carry the BYO client id and a last-4 secret hint
	// — never the secret itself. The web reads auth_mode + status to place
	// the sign-in / re-authorize affordance and the expired chip.
	AuthMode              string `json:"auth_mode,omitempty"`
	OAuthClientID         string `json:"oauth_client_id,omitempty"`
	OAuthClientSecretHint string `json:"oauth_client_secret_hint,omitempty"`
}

// mcpStatusView normalizes the stored status for the API: an empty status
// (a row the service has never probed) reads as the migration default
// "unknown".
func mcpStatusView(status string) string {
	if status == "" {
		return "unknown"
	}
	return status
}

func secretRowViews(rows []domain.EnvRow) []mcpSecretRowView {
	if len(rows) == 0 {
		return nil
	}
	out := make([]mcpSecretRowView, 0, len(rows))
	for _, row := range rows {
		out = append(out, mcpSecretRowView{Name: row.Name, ValueHint: row.Value})
	}
	return out
}

func (v *mcpServerView) fillConnection(encKey []byte, workspaceID string, conn domain.MCPConnection) {
	v.Transport = conn.Transport
	v.Command = conn.Command
	v.Args = conn.Args
	v.Env = secretRowViews(conn.Env)
	v.URL = conn.URL
	v.Headers = secretRowViews(conn.Headers)
	v.AuthMode = conn.AuthMode
	v.OAuthClientID = conn.OAuthClientID
	v.OAuthClientSecretHint = oauthClientSecretHint(encKey, []byte(workspaceID), conn.OAuthClientSecret)
}

// oauthClientSecretHint renders the BYO client secret's client-safe hint: a
// stored envelope (the only writer shape — the handlers encrypt on write)
// decrypts to recover the hinted tail, undecryptable envelopes hint empty,
// and plaintext rows (legacy or test writes) hint from their own tail. The
// value itself never crosses to a client.
func oauthClientSecretHint(encKey, aad []byte, value string) string {
	if value == "" {
		return ""
	}
	plaintext := value
	if strings.HasPrefix(value, secrets.Version1Prefix) {
		decrypted, err := secrets.Decrypt(encKey, aad, value)
		if err != nil {
			return ""
		}
		plaintext = string(decrypted)
	}
	if len(plaintext) > 4 {
		return plaintext[len(plaintext)-4:]
	}
	return plaintext
}

func newWorkspaceMCPServerView(encKey []byte, row *domain.WorkspaceMCPServer) mcpServerView {
	v := mcpServerView{
		ID:          row.ID,
		WorkspaceID: row.WorkspaceID,
		Name:        row.Name,
		Enabled:     row.Enabled,
		Status:      mcpStatusView(row.Status),
		StatusError: row.StatusError,
		ToolCount:   row.ToolCount,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}
	v.fillConnection(encKey, row.WorkspaceID, row.MCPConnection)
	return v
}

func newAgentMCPServerView(encKey []byte, row *domain.AgentMCPServer) mcpServerView {
	v := mcpServerView{
		ID:          row.ID,
		WorkspaceID: row.WorkspaceID,
		AgentID:     row.AgentID,
		Name:        row.Name,
		Enabled:     row.Enabled,
		Status:      mcpStatusView(row.Status),
		StatusError: row.StatusError,
		ToolCount:   row.ToolCount,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}
	v.fillConnection(encKey, row.WorkspaceID, row.MCPConnection)
	return v
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// mcpServerHandlers serves the workspace MCP registry and the agent-private
// MCP servers (design.md D10): reads are membership-level, registry writes
// ride tools.write, private-server writes ride agents.write. Every mutating
// save probes — a bounded fresh dial persisted on the row and returned in the
// response — and invalidates the connection manager's cached entry so config
// changes take effect immediately.
type mcpServerHandlers struct {
	settings     *agents.MCPSettingsService
	agents       store.AgentStore
	invalidator  MCPInvalidator
	probeTimeout time.Duration
	// connections names the owning connection for managed-server pointer
	// errors (design.md D11); the composition root always resolves it.
	connections ConnectionNamer
	// encKey seals the BYO OAuth client secret at persistence (the
	// workspace-AAD envelope the dial-time resolution opens) and renders the
	// read views' secret hint.
	encKey []byte
	// oauthCreds resolves oauth-mode rows' probe bearers (add-mcp-oauth-client
	// design.md D1 — probe/run parity); the composition root always wires it.
	oauthCreds mcp.OAuthDialCredentials
}

// NewMCPServerHandlers creates a new mcpServerHandlers instance. A probe
// timeout of zero selects DefaultMCPProbeTimeout.
func NewMCPServerHandlers(settings *agents.MCPSettingsService, agents store.AgentStore, invalidator MCPInvalidator, probeTimeout time.Duration, connections ConnectionNamer, encKey []byte, oauthCreds mcp.OAuthDialCredentials) *mcpServerHandlers {
	if probeTimeout <= 0 {
		probeTimeout = DefaultMCPProbeTimeout
	}
	return &mcpServerHandlers{settings: settings, agents: agents, invalidator: invalidator, probeTimeout: probeTimeout, connections: connections, encKey: encKey, oauthCreds: oauthCreds}
}

// sealOAuthClientSecret encrypts a write's plaintext BYO client secret into
// the workspace-AAD envelope before persistence (add-mcp-oauth-client task
// 2.2b/D4: the dial-time resolution opens the envelope, so plaintext must
// never survive a write). Envelopes pass through untouched; empty keeps the
// stored secret through the update merge.
func (h *mcpServerHandlers) sealOAuthClientSecret(workspaceID string, conn *domain.MCPConnection) error {
	if conn.OAuthClientSecret == "" || strings.HasPrefix(conn.OAuthClientSecret, secrets.Version1Prefix) {
		return nil
	}
	envelope, err := secrets.Encrypt(h.encKey, []byte(workspaceID), []byte(conn.OAuthClientSecret))
	if err != nil {
		return fmt.Errorf("seal oauth client secret: %w", err)
	}
	conn.OAuthClientSecret = envelope
	return nil
}

// rejectManagedServer enforces the managed-server immutability rule
// (add-workspace-connections design.md D11): an origin-marked server is
// read-plus-probe only through the MCP surfaces — the connection is the
// single authority over its URL, secret rows, and lifecycle. Edit and delete
// requests are rejected with a 409 naming the owning connection and pointing
// at the Integrations surface; probe and status stay allowed. Reports whether
// the server is managed (and the request was rejected).
func (h *mcpServerHandlers) rejectManagedServer(c *gin.Context, workspaceID, originConnectionID string) bool {
	if originConnectionID == "" {
		return false
	}
	name := h.connections.ConnectionDisplayName(c.Request.Context(), workspaceID, originConnectionID)
	AbortWithError(c, http.StatusConflict, CodeConflict,
		fmt.Sprintf("this mcp server is managed by the %s connection (%s); manage or disconnect it from workspace settings, Integrations", name, originConnectionID))
	return true
}

// resolveAgent resolves an agent in the workspace by slug first, falling back
// to ID (the agentHandlers convention). Unknown slugs and agents of another
// workspace are domain.ErrNotFound indistinguishably.
func (h *mcpServerHandlers) resolveAgent(ctx context.Context, workspaceID, identifier string) (*domain.Agent, error) {
	if workspaceID == "" || identifier == "" {
		return nil, domain.ErrNotFound
	}
	agent, err := h.agents.BySlug(ctx, workspaceID, identifier)
	if err == nil {
		return agent, nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return nil, err
	}
	return h.agents.ByID(ctx, workspaceID, identifier)
}

func bindMCPServerRequest(c *gin.Context) (*mcpServerRequest, bool) {
	var req mcpServerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, domain.ErrInvalid)
		return nil, false
	}
	return &req, true
}

// respondSettingsError maps the settings service's fielded validation errors
// to 422 with per-field details (the tool-settings convention) and everything
// else through the standard sentinel mapping — unknown ids and cross-tenant
// rows are 404, name conflicts chain to 409 should one ever escape the
// service's fielded conversion.
func respondSettingsError(c *gin.Context, err error) {
	var cfgErr *agents.ConfigValidationError
	if errors.As(err, &cfgErr) {
		details := make([]ErrorDetail, 0, len(cfgErr.Errors))
		for _, fe := range cfgErr.Errors {
			details = append(details, ErrorDetail{Field: fe.Field, Message: fe.Message})
		}
		AbortWithError(c, http.StatusUnprocessableEntity, CodeInvalidRequest, cfgErr.Error(), details...)
		return
	}
	RespondError(c, err)
}

// probeServer dials ref fresh (never the manager's cache — a probe is a new
// connection attempt by definition) within the probe bound and reports the
// outcome in the persisted-status vocabulary. OAuth-mode rows resolve their
// bearer through the credential seam first: a needs-authorization or
// discovery failure surfaces as the row's error detail, exactly as the run
// path reports it (probe/run parity, add-mcp-oauth-client design.md D1).
func (h *mcpServerHandlers) probeServer(ctx context.Context, ref mcp.Ref) (status, statusError string, toolCount int) {
	pctx, cancel := context.WithTimeout(ctx, h.probeTimeout)
	defer cancel()
	count, err := mcp.Probe(pctx, ref, h.oauthCreds)
	if err != nil {
		return domain.MCPStatusError, err.Error(), 0
	}
	return domain.MCPStatusConnected, "", count
}

// probePersistRespondWorkspace runs the on-save probe for a workspace server,
// persists the outcome on the row, and responds with the refreshed hint view.
func (h *mcpServerHandlers) probePersistRespondWorkspace(c *gin.Context, httpStatus int, workspaceID, id string) {
	ctx := c.Request.Context()
	row, err := h.settings.WorkspaceServerForRuntime(ctx, workspaceID, id)
	if err != nil {
		RespondError(c, err)
		return
	}
	status, statusError, toolCount := h.probeServer(ctx, mcp.Ref{WorkspaceID: workspaceID, ServerID: id, Name: row.Name, Conn: row.MCPConnection})
	if err := h.settings.SetWorkspaceServerStatus(ctx, workspaceID, id, status, statusError, toolCount); err != nil {
		RespondError(c, err)
		return
	}
	view, err := h.settings.WorkspaceServer(ctx, workspaceID, id)
	if err != nil {
		RespondError(c, err)
		return
	}
	RespondJSON(c, httpStatus, gin.H{"server": newWorkspaceMCPServerView(h.encKey, view)})
}

// probePersistRespondAgent is the agent-private counterpart of
// probePersistRespondWorkspace.
func (h *mcpServerHandlers) probePersistRespondAgent(c *gin.Context, httpStatus int, workspaceID, agentID, id string) {
	ctx := c.Request.Context()
	row, err := h.settings.AgentServerForRuntime(ctx, workspaceID, agentID, id)
	if err != nil {
		RespondError(c, err)
		return
	}
	status, statusError, toolCount := h.probeServer(ctx, mcp.Ref{WorkspaceID: workspaceID, ServerID: id, Name: row.Name, Conn: row.MCPConnection})
	if err := h.settings.SetAgentServerStatus(ctx, workspaceID, agentID, id, status, statusError, toolCount); err != nil {
		RespondError(c, err)
		return
	}
	view, err := h.settings.AgentServer(ctx, workspaceID, agentID, id)
	if err != nil {
		RespondError(c, err)
		return
	}
	RespondJSON(c, httpStatus, gin.H{"server": newAgentMCPServerView(h.encKey, view)})
}

// -------------------------------------------------------------------------
// Workspace registry (tools.write for writes; reads ride membership)
// -------------------------------------------------------------------------

// ListWorkspaceServers returns the workspace's registered servers as hint
// views.
func (h *mcpServerHandlers) ListWorkspaceServers(c *gin.Context) {
	ws := MustCurrentWorkspace(c)

	rows, err := h.settings.ListWorkspaceServers(c.Request.Context(), ws.ID)
	if err != nil {
		RespondError(c, err)
		return
	}
	items := make([]mcpServerView, 0, len(rows))
	for i := range rows {
		items = append(items, newWorkspaceMCPServerView(h.encKey, &rows[i]))
	}
	RespondOK(c, gin.H{"servers": items})
}

// CreateWorkspaceServer registers a server and probes it.
func (h *mcpServerHandlers) CreateWorkspaceServer(c *gin.Context) {
	ws := MustCurrentWorkspace(c)

	req, ok := bindMCPServerRequest(c)
	if !ok {
		return
	}

	srv := &domain.WorkspaceMCPServer{
		WorkspaceID:   ws.ID,
		Name:          strings.TrimSpace(req.Name),
		MCPConnection: req.connection(),
		Enabled:       true,
	}
	if req.Enabled != nil {
		srv.Enabled = *req.Enabled
	}
	if err := h.sealOAuthClientSecret(ws.ID, &srv.MCPConnection); err != nil {
		RespondError(c, err)
		return
	}
	if err := h.settings.CreateWorkspaceServer(c.Request.Context(), srv); err != nil {
		respondSettingsError(c, err)
		return
	}
	h.probePersistRespondWorkspace(c, http.StatusCreated, ws.ID, srv.ID)
}

// PatchWorkspaceServer replaces a server's editable fields and re-probes. An
// empty secret value keeps the stored secret (service-side name-keyed merge).
func (h *mcpServerHandlers) PatchWorkspaceServer(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	id := c.Param("id")

	req, ok := bindMCPServerRequest(c)
	if !ok {
		return
	}

	ctx := c.Request.Context()
	row, err := h.settings.WorkspaceServerForRuntime(ctx, ws.ID, id)
	if err != nil {
		RespondError(c, err)
		return
	}

	// Managed server (origin-marked): the connection owns its URL, secret
	// rows, and lifecycle — edit is rejected with the pointer error (D11).
	if h.rejectManagedServer(c, ws.ID, row.OriginConnectionID) {
		return
	}

	if !applyMCPServerPatch(&row.Name, &row.MCPConnection, &row.Enabled, req) {
		RespondError(c, fmt.Errorf("%w: no fields to update", domain.ErrInvalid))
		return
	}

	if err := h.sealOAuthClientSecret(ws.ID, &row.MCPConnection); err != nil {
		RespondError(c, err)
		return
	}
	if err := h.settings.UpdateWorkspaceServer(ctx, row); err != nil {
		respondSettingsError(c, err)
		return
	}
	h.invalidator.Invalidate(ws.ID, id)
	h.probePersistRespondWorkspace(c, http.StatusOK, ws.ID, id)
}

// DeleteWorkspaceServer removes a registry entry; agent opt-in references
// become inert by design. Origin-marked (managed) servers are rejected with
// the pointer error — disconnecting the owning connection is the delete (D11).
func (h *mcpServerHandlers) DeleteWorkspaceServer(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	id := c.Param("id")

	row, err := h.settings.WorkspaceServer(c.Request.Context(), ws.ID, id)
	if err != nil {
		RespondError(c, err)
		return
	}
	if h.rejectManagedServer(c, ws.ID, row.OriginConnectionID) {
		return
	}

	if err := h.settings.DeleteWorkspaceServer(c.Request.Context(), ws.ID, id); err != nil {
		RespondError(c, err)
		return
	}
	h.invalidator.Invalidate(ws.ID, id)
	RespondNoContent(c)
}

// ProbeWorkspaceServer re-probes on demand: a bounded fresh dial whose
// outcome persists on the row and returns in the response.
func (h *mcpServerHandlers) ProbeWorkspaceServer(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	id := c.Param("id")

	// Existence and tenancy first: an unknown or cross-tenant id is 404
	// before any dialing.
	if _, err := h.settings.WorkspaceServer(c.Request.Context(), ws.ID, id); err != nil {
		RespondError(c, err)
		return
	}
	h.probePersistRespondWorkspace(c, http.StatusOK, ws.ID, id)
}

// -------------------------------------------------------------------------
// Agent-private servers (agents.write for writes; reads ride agents.read)
// -------------------------------------------------------------------------

// resolveAgentScope resolves the agent addressed by the route and returns its
// id, 404 for unknown slugs and cross-tenant agents alike.
func (h *mcpServerHandlers) resolveAgentScope(c *gin.Context) (*domain.Workspace, *domain.Agent, bool) {
	ws := MustCurrentWorkspace(c)
	agent, err := h.resolveAgent(c.Request.Context(), ws.ID, c.Param("agent"))
	if err != nil {
		RespondError(c, err)
		return nil, nil, false
	}
	return ws, agent, true
}

// ListAgentServers returns the agent's private servers as hint views.
func (h *mcpServerHandlers) ListAgentServers(c *gin.Context) {
	ws, agent, ok := h.resolveAgentScope(c)
	if !ok {
		return
	}

	rows, err := h.settings.ListAgentServers(c.Request.Context(), ws.ID, agent.ID)
	if err != nil {
		RespondError(c, err)
		return
	}
	items := make([]mcpServerView, 0, len(rows))
	for i := range rows {
		items = append(items, newAgentMCPServerView(h.encKey, &rows[i]))
	}
	RespondOK(c, gin.H{"servers": items})
}

// CreateAgentServer attaches a private server to the agent and probes it.
func (h *mcpServerHandlers) CreateAgentServer(c *gin.Context) {
	ws, agent, ok := h.resolveAgentScope(c)
	if !ok {
		return
	}

	req, ok2 := bindMCPServerRequest(c)
	if !ok2 {
		return
	}

	srv := &domain.AgentMCPServer{
		WorkspaceID:   ws.ID,
		AgentID:       agent.ID,
		Name:          strings.TrimSpace(req.Name),
		MCPConnection: req.connection(),
		Enabled:       true,
	}
	if req.Enabled != nil {
		srv.Enabled = *req.Enabled
	}
	if err := h.sealOAuthClientSecret(ws.ID, &srv.MCPConnection); err != nil {
		RespondError(c, err)
		return
	}
	if err := h.settings.CreateAgentServer(c.Request.Context(), srv); err != nil {
		respondSettingsError(c, err)
		return
	}
	h.probePersistRespondAgent(c, http.StatusCreated, ws.ID, agent.ID, srv.ID)
}

// PatchAgentServer replaces a private server's editable fields and re-probes.
func (h *mcpServerHandlers) PatchAgentServer(c *gin.Context) {
	ws, agent, ok := h.resolveAgentScope(c)
	if !ok {
		return
	}
	id := c.Param("id")

	req, ok2 := bindMCPServerRequest(c)
	if !ok2 {
		return
	}

	ctx := c.Request.Context()
	row, err := h.settings.AgentServerForRuntime(ctx, ws.ID, agent.ID, id)
	if err != nil {
		RespondError(c, err)
		return
	}

	if !applyMCPServerPatch(&row.Name, &row.MCPConnection, &row.Enabled, req) {
		RespondError(c, fmt.Errorf("%w: no fields to update", domain.ErrInvalid))
		return
	}

	if err := h.sealOAuthClientSecret(ws.ID, &row.MCPConnection); err != nil {
		RespondError(c, err)
		return
	}
	if err := h.settings.UpdateAgentServer(ctx, row); err != nil {
		respondSettingsError(c, err)
		return
	}
	h.invalidator.Invalidate(ws.ID, id)
	h.probePersistRespondAgent(c, http.StatusOK, ws.ID, agent.ID, id)
}

// DeleteAgentServer removes a private server from the agent.
func (h *mcpServerHandlers) DeleteAgentServer(c *gin.Context) {
	ws, agent, ok := h.resolveAgentScope(c)
	if !ok {
		return
	}
	id := c.Param("id")

	if err := h.settings.DeleteAgentServer(c.Request.Context(), ws.ID, agent.ID, id); err != nil {
		RespondError(c, err)
		return
	}
	h.invalidator.Invalidate(ws.ID, id)
	RespondNoContent(c)
}

// ProbeAgentServer re-probes a private server on demand.
func (h *mcpServerHandlers) ProbeAgentServer(c *gin.Context) {
	ws, agent, ok := h.resolveAgentScope(c)
	if !ok {
		return
	}
	id := c.Param("id")

	if _, err := h.settings.AgentServer(c.Request.Context(), ws.ID, agent.ID, id); err != nil {
		RespondError(c, err)
		return
	}
	h.probePersistRespondAgent(c, http.StatusOK, ws.ID, agent.ID, id)
}
