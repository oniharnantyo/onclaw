// Package services — workspace service connections (add-workspace-connections
// tasks 2.1–2.5): the product layer that turns a recipe and an access token
// into a workspace connection backed by the existing MCP runtime. A connection
// materializes an ordinary workspace MCP server (design.md D1) whose secret
// header row carries the token through the MCP settings machinery (D4); the
// recipe's probe gates every connect and status refresh (D3).
package services

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/oniharnantyo/onclaw/internal/agents/mcp"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// ErrProbeFailed marks a connect rejected by the recipe's probe: the payload
// was well-formed but the upstream service refused the token (or was
// unreachable). The handler maps it to 400 invalid_request with the upstream
// message verbatim — no sentinel prefix — so the user can correct the token
// (workspace-connections spec: "Probe failure blocks connect").
var ErrProbeFailed = errors.New("probe failed")

// WorkspaceMCPSecretStore is the narrow MCP-settings seam the connections
// service composes (design.md D4): registering a workspace server with its
// secret rows encrypted, reading hint and runtime views, and persisting probe
// outcomes. *agents.MCPSettingsService satisfies it structurally — the same
// interface-first composition the runner uses for services.ModelCatalog — so
// connections reuse the one secret path: AES-256-GCM rows bound to the
// workspace, merge-on-name writes, last-4-hint reads.
type WorkspaceMCPSecretStore interface {
	CreateWorkspaceServer(ctx context.Context, server *domain.WorkspaceMCPServer) error
	WorkspaceServer(ctx context.Context, workspaceID, id string) (*domain.WorkspaceMCPServer, error)
	WorkspaceServerForRuntime(ctx context.Context, workspaceID, id string) (*domain.WorkspaceMCPServer, error)
	SetWorkspaceServerStatus(ctx context.Context, workspaceID, id, status, statusError string, toolCount int) error
}

// ConnectionProber dials one candidate MCP connection fresh (never a cache
// hit — a probe is a new connection attempt by definition) and reports the
// exposed-tool count. It is the mcp.Probe shape decoupled from the mcp
// package's Ref so unit tests stub a plain function. Connect is probe-gated
// on it (design.md D3).
type ConnectionProber func(ctx context.Context, workspaceID, serverID, name string, conn domain.MCPConnection) (toolCount int, err error)

// DefaultConnectionProbeTimeout bounds each probe's fresh dial, handshake,
// and tool listing — matching the MCP handler's DefaultMCPProbeTimeout.
const DefaultConnectionProbeTimeout = 10 * time.Second

// ConnectionsService implements the connection lifecycle over its granular
// stores: connect (probe-gated, one connection per service), the joined read
// view, disconnect (the store's atomic cascade), and probe-on-demand.
type ConnectionsService struct {
	connections  store.Connections
	wsServers    store.WorkspaceMCPServers
	agents       store.AgentStore
	settings     WorkspaceMCPSecretStore
	probe        ConnectionProber
	probeTimeout time.Duration
}

// ConnectionsOption configures a ConnectionsService.
type ConnectionsOption func(*ConnectionsService)

// WithProbeTimeout bounds each probe's fresh connection attempt. Zero or
// negative selects DefaultConnectionProbeTimeout.
func WithProbeTimeout(d time.Duration) ConnectionsOption {
	return func(s *ConnectionsService) {
		if d > 0 {
			s.probeTimeout = d
		}
	}
}

// WithProber overrides the dialing function (the unit-test seam; the
// production default is the mcp runtime's fresh-dial probe). A nil function
// is ignored.
func WithProber(fn ConnectionProber) ConnectionsOption {
	return func(s *ConnectionsService) {
		if fn != nil {
			s.probe = fn
		}
	}
}

// NewConnectionsService builds the service from its granular dependencies.
// settings is the MCP settings service the token's secret row rides (never
// nil — the composition root resolves it before construction).
func NewConnectionsService(connections store.Connections, wsServers store.WorkspaceMCPServers, agents store.AgentStore, settings WorkspaceMCPSecretStore, opts ...ConnectionsOption) *ConnectionsService {
	s := &ConnectionsService{
		connections:  connections,
		wsServers:    wsServers,
		agents:       agents,
		settings:     settings,
		probe:        defaultProber(),
		probeTimeout: DefaultConnectionProbeTimeout,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// defaultProber wires the mcp runtime's fresh-dial probe.
func defaultProber() ConnectionProber {
	return func(ctx context.Context, workspaceID, serverID, name string, conn domain.MCPConnection) (int, error) {
		return mcp.Probe(ctx, mcp.Ref{WorkspaceID: workspaceID, ServerID: serverID, Name: name, Conn: conn})
	}
}

// ---------------------------------------------------------------------------
// Connect (tasks 2.1/2.2)
// ---------------------------------------------------------------------------

// Connect resolves the recipe, validates the access level and token, probes
// the candidate connection BEFORE anything is stored, and on probe success
// persists the connection plus its materialized workspace MCP server — the
// token encrypted as the recipe's single secret row through the MCP settings
// machinery. One connection per service per workspace (design.md D6): a
// duplicate returns domain.ErrConnectionExists naming the existing connection.
// A probe failure stores nothing and returns an ErrProbeFailed error carrying
// the upstream message (design.md D3).
func (s *ConnectionsService) Connect(ctx context.Context, workspaceID, recipeID, accessLevel, token string) (*ConnectionView, error) {
	if workspaceID == "" {
		return nil, fmt.Errorf("%w: workspace id cannot be empty", domain.ErrInvalid)
	}
	recipe := domain.RecipeByID(strings.TrimSpace(recipeID))
	if recipe == nil {
		return nil, fmt.Errorf("%w: %q", domain.ErrUnknownRecipe, recipeID)
	}
	// Coming-soon recipes are gallery declarations, not connectable services
	// (tasks.md 2.5): they accept no connect requests.
	if recipe.Availability != domain.RecipeAvailable {
		return nil, fmt.Errorf("%w: %s is coming soon and cannot be connected yet", domain.ErrInvalid, recipe.Service)
	}
	// An empty access level selects the recipe's flow default (its first
	// declared level — ValidateRecipe guarantees at least one).
	if accessLevel == "" {
		accessLevel = recipe.AccessLevels[0]
	}
	if err := domain.ValidateConnectionAccessLevel(accessLevel); err != nil {
		return nil, err
	}
	if !slices.Contains(recipe.AccessLevels, accessLevel) {
		return nil, fmt.Errorf("%w: access level %q is not offered by %s", domain.ErrInvalid, accessLevel, recipe.Service)
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, fmt.Errorf("%w: token cannot be empty", domain.ErrInvalid)
	}

	// One connection per service (design.md D6): the conflict names the
	// existing connection. The store's uniqueness constraint stays
	// authoritative; this check is the friendly pre-read.
	if existing, err := s.connections.GetByService(ctx, workspaceID, recipe.ID); err == nil && existing != nil {
		return nil, fmt.Errorf("%w: %s is already connected in this workspace (connection %s)", domain.ErrConnectionExists, recipe.Service, existing.ID)
	} else if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return nil, err
	}

	server := materializeServer(workspaceID, recipe, token)

	// Probe before persist (design.md D3): a failure stores nothing — no
	// connection row, no server row, no token ciphertext — and the upstream
	// message is what reaches the client.
	toolCount, err := s.probeServer(ctx, workspaceID, "connection-probe", server.Name, server.MCPConnection)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrProbeFailed, err)
	}

	conn := &domain.Connection{
		WorkspaceID: workspaceID,
		Service:     recipe.ID,
		AccessLevel: accessLevel,
	}
	if err := s.connections.Create(ctx, conn); err != nil {
		return nil, err
	}
	// Birth-stamp the origin marker: the connection owns this server from
	// creation (design.md D1/D11) — the marker is written only here, never
	// through Update.
	server.OriginConnectionID = conn.ID
	if err := s.settings.CreateWorkspaceServer(ctx, server); err != nil {
		// Compensate: the connection must not outlive its materialization
		// failure. Delete is the full cascade over a row that has no linked
		// server yet, so the workspace is left exactly as it was.
		if delErr := s.connections.Delete(ctx, workspaceID, conn.ID); delErr != nil {
			return nil, fmt.Errorf("mcp server creation failed (%v) and the connection cleanup failed too: %w", err, delErr)
		}
		return nil, err
	}
	if err := s.settings.SetWorkspaceServerStatus(ctx, workspaceID, server.ID, domain.MCPStatusConnected, "", toolCount); err != nil {
		return nil, err
	}
	return s.buildView(ctx, workspaceID, conn)
}

// materializeServer builds the candidate workspace MCP server from the
// recipe's declared facts (design.md D7 — the endpoint is never user input):
// the recipe's transport and endpoint, the display name, and the token as the
// single secret row (header for URL transports, env row for stdio) composed
// with the recipe's scheme (design.md D4). The row is created enabled;
// plaintext lives only until the settings service encrypts it.
func materializeServer(workspaceID string, recipe *domain.Recipe, token string) *domain.WorkspaceMCPServer {
	value := token
	if recipe.TokenScheme != "" {
		value = recipe.TokenScheme + " " + token
	}
	srv := &domain.WorkspaceMCPServer{
		WorkspaceID: workspaceID,
		Name:        recipe.Service,
		Enabled:     true,
	}
	if recipe.Transport == domain.MCPTransportStdio {
		srv.MCPConnection = domain.MCPConnection{
			Transport: recipe.Transport,
			Command:   recipe.Command,
			Env:       []domain.EnvRow{{Name: recipe.TokenHeader, Value: value}},
		}
		return srv
	}
	srv.MCPConnection = domain.MCPConnection{
		Transport: recipe.Transport,
		URL:       recipe.Endpoint,
		Headers:   []domain.EnvRow{{Name: recipe.TokenHeader, Value: value}},
	}
	return srv
}

// ---------------------------------------------------------------------------
// Read views (task 2.3)
// ---------------------------------------------------------------------------

// ConnectionView is the connection read model: the stored connection joined
// with the materialized server's live status, the attached agents' names, and
// the hint-only token display. The token itself never appears — reads carry
// the last-4 hint only (workspace-connections spec: "Token never readable").
type ConnectionView struct {
	domain.Connection
	Status        string `json:"status"`
	StatusError   string `json:"status_error,omitempty"`
	TokenHint     string `json:"token_hint,omitempty"`
	ServerID      string `json:"server_id,omitempty"`
	ServerEnabled *bool  `json:"server_enabled,omitempty"`
	ToolCount     int    `json:"tool_count,omitempty"`
	// AttachedAgents lists the names of agents whose enabled_mcps include the
	// materialized server. Always an array, empty when none are attached.
	AttachedAgents []string `json:"attached_agents"`
}

// connectionStatusUnknown is the API's status for a server that has never
// been probed (the stored empty-status row).
const connectionStatusUnknown = "unknown"

// Get returns one connection as its joined view. Unknown and cross-workspace
// ids are domain.ErrNotFound, indistinguishable.
func (s *ConnectionsService) Get(ctx context.Context, workspaceID, id string) (*ConnectionView, error) {
	conn, err := s.connections.Get(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	return s.buildView(ctx, workspaceID, conn)
}

// List returns the workspace's connections as joined views, creation order.
func (s *ConnectionsService) List(ctx context.Context, workspaceID string) ([]ConnectionView, error) {
	connections, err := s.connections.List(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	views := make([]ConnectionView, 0, len(connections))
	for i := range connections {
		view, err := s.buildView(ctx, workspaceID, &connections[i])
		if err != nil {
			return nil, err
		}
		views = append(views, *view)
	}
	return views, nil
}

// buildView joins one connection with its materialized server (status,
// enabled switch, tool count, token hint) and the names of the agents
// attached to that server. A connection whose server row is absent reads as
// unknown with no attachments — absence is a degraded state, not an error.
func (s *ConnectionsService) buildView(ctx context.Context, workspaceID string, conn *domain.Connection) (*ConnectionView, error) {
	view := &ConnectionView{Connection: *conn, AttachedAgents: []string{}}

	server, err := s.wsServers.GetByOriginConnection(ctx, workspaceID, conn.ID)
	if err != nil {
		return nil, err
	}
	if server == nil {
		view.Status = connectionStatusUnknown
		return view, nil
	}

	// The hint view: every secret row value replaced by its last-4 hint.
	hinted, err := s.settings.WorkspaceServer(ctx, workspaceID, server.ID)
	if err != nil {
		return nil, err
	}

	enabled := server.Enabled
	view.ServerID = server.ID
	view.ServerEnabled = &enabled
	view.ToolCount = server.ToolCount
	view.Status = server.Status
	view.StatusError = server.StatusError
	if view.Status == "" {
		view.Status = connectionStatusUnknown
	}
	view.TokenHint = secretRowHint(hinted, tokenRowName(conn.Service))

	names, err := s.attachedAgentNames(ctx, workspaceID, server.ID)
	if err != nil {
		return nil, err
	}
	view.AttachedAgents = names
	return view, nil
}

// tokenRowName resolves the recipe row (header or stdio env row) that carries
// the token; an unregistered recipe degrades to no hint rather than guessing.
func tokenRowName(service string) string {
	if recipe := domain.RecipeByID(service); recipe != nil {
		return recipe.TokenHeader
	}
	return ""
}

// secretRowHint reads the last-4 token hint out of a hint-view server row:
// the first header or env row matching the recipe's token row name.
func secretRowHint(server *domain.WorkspaceMCPServer, rowName string) string {
	if server == nil || rowName == "" {
		return ""
	}
	for _, row := range server.Headers {
		if row.Name == rowName {
			return row.Value
		}
	}
	for _, row := range server.Env {
		if row.Name == rowName {
			return row.Value
		}
	}
	return ""
}

// attachedAgentNames lists the names of the workspace's agents whose
// enabled_mcps include serverID.
func (s *ConnectionsService) attachedAgentNames(ctx context.Context, workspaceID, serverID string) ([]string, error) {
	agents, err := s.agents.ListForWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	names := []string{}
	for _, agent := range agents {
		if slices.Contains(agent.EnabledMCPS, serverID) {
			names = append(names, agent.Name)
		}
	}
	return names, nil
}

// ---------------------------------------------------------------------------
// Disconnect (task 2.3, design.md D8)
// ---------------------------------------------------------------------------

// Disconnect removes the connection through the store's atomic cascade (the
// linked server row and every agent's attachment reference die with it; the
// token ciphertext becomes unrecoverable) and returns the materialized
// server's id — empty when the connection had none — so the caller can drop
// the connection manager's cached entry. Unknown or cross-workspace ids are
// domain.ErrNotFound.
func (s *ConnectionsService) Disconnect(ctx context.Context, workspaceID, id string) (serverID string, err error) {
	conn, err := s.connections.Get(ctx, workspaceID, id)
	if err != nil {
		return "", err
	}
	if server, err := s.wsServers.GetByOriginConnection(ctx, workspaceID, conn.ID); err != nil {
		return "", err
	} else if server != nil {
		serverID = server.ID
	}
	if err := s.connections.Delete(ctx, workspaceID, conn.ID); err != nil {
		return "", err
	}
	return serverID, nil
}

// ---------------------------------------------------------------------------
// Probe on demand (task 2.4)
// ---------------------------------------------------------------------------

// Probe re-runs the probe against the linked server's stored connection
// (secret rows decrypted for the dial only) and persists the outcome through
// the existing probe path. The refreshed view is returned either way — a
// failed probe is a status, not a request error (the MCP registry's probe
// endpoint behaves the same).
func (s *ConnectionsService) Probe(ctx context.Context, workspaceID, id string) (*ConnectionView, error) {
	conn, err := s.connections.Get(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	server, err := s.wsServers.GetByOriginConnection(ctx, workspaceID, conn.ID)
	if err != nil {
		return nil, err
	}
	if server == nil {
		return nil, fmt.Errorf("%w: connection %s has no linked mcp server", domain.ErrNotFound, conn.ID)
	}
	row, err := s.settings.WorkspaceServerForRuntime(ctx, workspaceID, server.ID)
	if err != nil {
		return nil, err
	}

	toolCount, probeErr := s.probeServer(ctx, workspaceID, server.ID, row.Name, row.MCPConnection)
	status, statusErr := domain.MCPStatusConnected, ""
	if probeErr != nil {
		status, statusErr = domain.MCPStatusError, probeErr.Error()
	}
	if err := s.settings.SetWorkspaceServerStatus(ctx, workspaceID, server.ID, status, statusErr, toolCount); err != nil {
		return nil, err
	}
	return s.buildView(ctx, workspaceID, conn)
}

// probeServer dials within the probe bound.
func (s *ConnectionsService) probeServer(ctx context.Context, workspaceID, serverID, name string, conn domain.MCPConnection) (int, error) {
	pctx, cancel := context.WithTimeout(ctx, s.probeTimeout)
	defer cancel()
	return s.probe(pctx, workspaceID, serverID, name, conn)
}

// ---------------------------------------------------------------------------
// Managed-server pointer (design.md D11)
// ---------------------------------------------------------------------------

// ConnectionDisplayName returns the human name for a connection — the
// recipe's service display name when the recipe is still registered, else the
// raw service id, else the id itself. The MCP server surfaces' managed-server
// pointer error uses it to name the owning connection.
func (s *ConnectionsService) ConnectionDisplayName(ctx context.Context, workspaceID, connectionID string) string {
	conn, err := s.connections.Get(ctx, workspaceID, connectionID)
	if err != nil || conn == nil {
		return connectionID
	}
	if recipe := domain.RecipeByID(conn.Service); recipe != nil {
		return recipe.Service
	}
	return conn.Service
}
