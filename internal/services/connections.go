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
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/oniharnantyo/onclaw/internal/agents/mcp"
	"github.com/oniharnantyo/onclaw/internal/auth/oauthstate"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// ErrProbeFailed marks a connect rejected by the recipe's probe: the payload
// was well-formed but the upstream service refused the token (or was
// unreachable). The handler maps it to 400 invalid_request with the upstream
// message verbatim — no sentinel prefix — so the user can correct the token
// (workspace-connections spec: "Probe failure blocks connect").
var ErrProbeFailed = errors.New("probe failed")

// ErrTokenReplaceUnsupported marks a token-replacement attempt on an
// OAuth-kind connection: OAuth rotates its credential by reauthorization, so
// it accepts no replacement token (add-connection-edit spec: "OAuth-kind
// replacement refused"). Chains to domain.ErrInvalid so the generic sentinel
// mapping produces a 400 envelope whose message directs to reauthorization.
var ErrTokenReplaceUnsupported = fmt.Errorf("%w: this connection authorizes through OAuth — rotate its credential by reauthorizing instead of replacing the token", domain.ErrInvalid)

// AttachmentTxRunner is the narrow transaction seam the atomic attachment
// diff applies through (add-connection-edit design.md D1): it opens one
// transaction and hands the closure the tx-scoped AgentStore, so a closure
// error leaves every agent row exactly as it was. The composition root
// adapts store.Store.WithTx into it (the documented transaction seam) —
// the same shape skills.TxProvider uses.
type AttachmentTxRunner func(ctx context.Context, fn func(ctx context.Context, agents store.AgentStore) error) error

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
	// UpdateWorkspaceServer replaces a server's editable fields with the
	// name-keyed secret merge — the OAuth refresh/reauthorization write-through
	// rides it to renew the token row without touching any other secret
	// (add-connection-oauth D1). *agents.MCPSettingsService satisfies it.
	UpdateWorkspaceServer(ctx context.Context, server *domain.WorkspaceMCPServer) error
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
// stores: connect (probe-gated, one connection per service, dispatching OAuth
// recipes to the authorize builder), the joined read view, disconnect (the
// store's atomic cascade), probe-on-demand, the OAuth consent round trip
// (add-connection-oauth tasks 2.1–2.5, connections_oauth.go), and the
// refresh-on-resolution credential wrapper.
type ConnectionsService struct {
	connections   store.Connections
	wsServers     store.WorkspaceMCPServers
	agents        store.AgentStore
	// attachmentTx is the atomic attachment diff's transaction seam
	// (add-connection-edit design.md D1); nil applies the diff through agents
	// directly — the non-transactional fallback that keeps embedders honest.
	attachmentTx AttachmentTxRunner
	settings     WorkspaceMCPSecretStore
	probe         ConnectionProber
	probeTimeout  time.Duration
	apps          store.OAuthApps
	encKey        []byte
	publicBaseURL string
	// httpClient is the service's one outbound HTTP lane: the OAuth token
	// endpoint's exchanges/refreshes AND the http-kind probes ride it (both
	// bounded — the probe additionally by probeTimeout).
	httpClient *http.Client
	stateTTL   time.Duration
	// state is the shared single-use signed-state machinery
	// (internal/auth/oauthstate): the HMAC-sealed state format and the
	// single-use nonce store, built after the options apply so WithStateTTL
	// bounds it. The flow owns its claims shape and its generic rejection.
	state *oauthstate.Sealer
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

// WithTokenHTTPClient overrides the HTTP client the OAuth token endpoint
// exchanges and refreshes ride (add-connection-oauth 3.3; the unit-test seam,
// mirroring the WithProber precedent). A nil client is ignored.
func WithTokenHTTPClient(client *http.Client) ConnectionsOption {
	return func(s *ConnectionsService) {
		if client != nil {
			s.httpClient = client
		}
	}
}

// WithStateTTL bounds the OAuth signed state's validity (design.md D4). Zero
// or negative selects DefaultOAuthStateTTL.
func WithStateTTL(d time.Duration) ConnectionsOption {
	return func(s *ConnectionsService) {
		if d > 0 {
			s.stateTTL = d
		}
	}
}

// WithAttachmentTx binds the atomic attachment diff's narrow transaction seam
// (add-connection-edit design.md D1): run opens one transaction and hands the
// closure the tx-scoped AgentStore — the composition root adapts
// store.Store.WithTx into it. A nil runner is ignored; without a seam
// SetAttachedAgents applies through the plain agent store (the fallback that
// keeps tests and embedders honest — the real installation always wires the
// seam).
func WithAttachmentTx(run AttachmentTxRunner) ConnectionsOption {
	return func(s *ConnectionsService) {
		if run != nil {
			s.attachmentTx = run
		}
	}
}

// NewConnectionsService builds the service from its granular dependencies.
// settings is the MCP settings service the token's secret row rides (never
// nil — the composition root resolves it before construction). apps is the
// instance OAuth apps store backing the OAuth flows' registration gate, encKey
// the instance master key (the state HMAC and the refresh-token envelopes),
// and publicBaseURL the instance's externally reachable base URL the redirect
// URIs derive from (empty legitimately disables OAuth connect until set —
// validated at use, never at boot).
func NewConnectionsService(connections store.Connections, wsServers store.WorkspaceMCPServers, agents store.AgentStore, settings WorkspaceMCPSecretStore, apps store.OAuthApps, encKey []byte, publicBaseURL string, opts ...ConnectionsOption) *ConnectionsService {
	s := &ConnectionsService{
		connections:   connections,
		wsServers:     wsServers,
		agents:        agents,
		settings:      settings,
		probe:         defaultProber(),
		probeTimeout:  DefaultConnectionProbeTimeout,
		apps:          apps,
		encKey:        encKey,
		publicBaseURL: publicBaseURL,
		httpClient:    &http.Client{Timeout: DefaultTokenHTTPTimeout},
		stateTTL:      DefaultOAuthStateTTL,
	}
	for _, opt := range opts {
		opt(s)
	}
	s.state = oauthstate.NewSealer(encKey, stateContext, s.stateTTL)
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

// ConnectResult is the connect action's outcome: a PAT connect completes
// synchronously and carries the joined view; an OAuth connect defers to the
// browser consent flow and carries the provider authorize URL (tasks.md
// 2.1/3.1) — exactly one of the two is set.
type ConnectResult struct {
	Connection   *ConnectionView
	AuthorizeURL string
}

// Connect resolves the recipe, validates the access level and token, resolves
// the base-URL origin, probes the candidate connection BEFORE anything is
// stored, and on probe success persists the connection plus its materialized
// workspace MCP server — the token encrypted as the recipe's single secret row
// through the MCP settings machinery. Uniqueness is per (service, resolved
// origin) (add-recipe-base-url tasks.md 2.1): a duplicate returns
// domain.ErrConnectionExists naming the existing connection (and the origin it
// collides on, for parametrized recipes). A probe failure stores nothing and
// returns an ErrProbeFailed error carrying the upstream message (design.md
// D3).
//
// origin is the connect request's base-URL value: empty selects the recipe's
// declared default when it parametrizes one, and a submitted origin for a
// recipe that declares none is IGNORED — the connection resolves to the
// recipe's fixed endpoint (spec: "Undeclared recipes ignore origin");
// resolveConnectOrigin. The resolved origin is stored on the connection and
// immutable afterwards.
//
// OAuth recipes (auth kind oauth) dispatch to the authorize-URL builder
// BEFORE the availability check — their effective availability is the
// instance app registration, checked there (add-connection-oauth tasks.md
// 2.1/2.5) — and return an authorize URL; activation completes at the public
// callback. userID is the initiating user the consent is bound to (the signed
// state carries it, design.md D4). Origin parameters are PAT-only
// (domain.ValidateRecipe), so the OAuth flow never carries one.
func (s *ConnectionsService) Connect(ctx context.Context, workspaceID, userID, recipeID, accessLevel, token, origin string) (*ConnectResult, error) {
	if workspaceID == "" {
		return nil, fmt.Errorf("%w: workspace id cannot be empty", domain.ErrInvalid)
	}
	recipe := domain.RecipeByID(strings.TrimSpace(recipeID))
	if recipe == nil {
		return nil, fmt.Errorf("%w: %q", domain.ErrUnknownRecipe, recipeID)
	}
	resolvedOrigin, err := resolveConnectOrigin(recipe, strings.TrimSpace(origin))
	if err != nil {
		return nil, err
	}
	// OAuth recipes route to the authorize builder ahead of the coming-soon
	// rejection: their availability gate is the registered instance app.
	if recipe.AuthKind == domain.RecipeAuthOAuth {
		authorizeURL, err := s.beginConnect(ctx, workspaceID, userID, recipe, accessLevel)
		if err != nil {
			return nil, err
		}
		return &ConnectResult{AuthorizeURL: authorizeURL}, nil
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

	// One connection per (service, resolved origin) (add-recipe-base-url
	// tasks.md 2.1): the conflict names the existing connection. The store's
	// uniqueness index stays authoritative; this check is the friendly
	// pre-read and precedes the probe.
	if existing, err := s.existingConnection(ctx, workspaceID, recipe.ID, resolvedOrigin); err != nil {
		return nil, err
	} else if existing != nil {
		return nil, connectionExistsError(recipe, existing)
	}

	// HTTP-kind recipes skip materialization entirely (add-connection-http
	// tasks 3.1): no workspace MCP server row and no origin marker — the
	// recipe's declared probe call gates the connect and the token rides the
	// connection's encrypted envelope column (connections_http.go).
	if recipe.Kind == domain.RecipeKindHTTP {
		return s.connectHTTP(ctx, workspaceID, recipe, accessLevel, token, resolvedOrigin)
	}

	server, err := materializeServer(workspaceID, recipe, token, resolvedOrigin)
	if err != nil {
		return nil, err
	}

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
		Origin:      resolvedOrigin,
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
	view, err := s.buildView(ctx, workspaceID, conn)
	if err != nil {
		return nil, err
	}
	return &ConnectResult{Connection: view}, nil
}

// resolveConnectOrigin resolves the connect request's base-URL value for the
// recipe (add-recipe-base-url tasks.md 2.4): a recipe without an origin
// parameter has a fixed endpoint — a submitted origin is silently ignored and
// the connect proceeds as if empty (spec: "Undeclared recipes ignore origin");
// a parametrized recipe validates the submission through domain.ParseOrigin
// and falls back to the declared default when the field came back empty. The
// resolved origin is what the connection stores and the uniqueness comparison
// keys on.
func resolveConnectOrigin(recipe *domain.Recipe, origin string) (string, error) {
	if recipe.OriginParam == nil {
		return "", nil
	}
	if origin == "" {
		origin = recipe.OriginParam.Default
	}
	resolved, err := domain.ParseOrigin(origin)
	if err != nil {
		return "", fmt.Errorf("%s: %w", recipe.OriginParam.Name, err)
	}
	return resolved, nil
}

// existingConnection returns the workspace's stored connection colliding with
// the candidate (service, origin) pair, or nil when the connect may proceed.
// The workspace-scoped List keeps the pre-read at the service layer — the
// store's composite unique index remains the concurrency backstop.
func (s *ConnectionsService) existingConnection(ctx context.Context, workspaceID, service, origin string) (*domain.Connection, error) {
	connections, err := s.connections.List(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	for i := range connections {
		if connections[i].Service == service && connections[i].Origin == origin {
			return &connections[i], nil
		}
	}
	return nil, nil
}

// connectionExistsError builds the conflict error for a colliding connect:
// the detail names the existing connection, plus the origin it collides on
// when the recipe parametrizes one (add-recipe-base-url tasks.md 2.1).
func connectionExistsError(recipe *domain.Recipe, existing *domain.Connection) error {
	if existing.Origin != "" {
		return fmt.Errorf("%w: %s is already connected in this workspace for origin %s (connection %s)", domain.ErrConnectionExists, recipe.Service, existing.Origin, existing.ID)
	}
	return fmt.Errorf("%w: %s is already connected in this workspace (connection %s)", domain.ErrConnectionExists, recipe.Service, existing.ID)
}

// materializeServer builds the candidate workspace MCP server from the
// recipe's declared facts (design.md D7 — the endpoint is never user input):
// the recipe's transport and endpoint, the display name, and the token as the
// single secret row (header for URL transports, env row for stdio) composed
// with the recipe's scheme (design.md D4). The row is created enabled;
// plaintext lives only until the settings service encrypts it. origin is the
// connection's resolved base-URL origin (add-recipe-base-url tasks.md 2.3):
// for URL transports the materialized URL is domain.ResolveRecipeBase's
// resolution — the origin joined with the recipe's declared endpoint path,
// byte-identical to the declared endpoint for recipes without an origin
// parameter. The row is returned un-stored; an unresolvable surface is an
// error, never a half-built server.
func materializeServer(workspaceID string, recipe *domain.Recipe, token, origin string) (*domain.WorkspaceMCPServer, error) {
	value := token
	if recipe.TokenScheme != "" {
		value = recipe.TokenScheme + " " + token
	}
	srv := &domain.WorkspaceMCPServer{
		WorkspaceID: workspaceID,
		Name:        materializedName(recipe, origin),
		Enabled:     true,
	}
	if recipe.Transport == domain.MCPTransportStdio {
		srv.MCPConnection = domain.MCPConnection{
			Transport: recipe.Transport,
			Command:   recipe.Command,
			Env:       []domain.EnvRow{{Name: recipe.TokenHeader, Value: value}},
		}
		return srv, nil
	}
	base, err := domain.ResolveRecipeBase(recipe, origin)
	if err != nil {
		return nil, err
	}
	srv.MCPConnection = domain.MCPConnection{
		Transport: recipe.Transport,
		URL:       base,
		Headers:   []domain.EnvRow{{Name: recipe.TokenHeader, Value: value}},
	}
	return srv, nil
}

// materializedName derives the materialized server's display name: the
// recipe's service name, suffixed with the connected origin's host when the
// connection resolved a non-default origin. Per-(service, origin) uniqueness
// (add-recipe-base-url tasks.md 2.1) means two materialized servers of one
// recipe may coexist in a workspace, and the settings service requires
// distinct names; default-origin and non-parametrized connects keep the bare
// service name byte-identically.
func materializedName(recipe *domain.Recipe, origin string) string {
	if origin == "" || (recipe.OriginParam != nil && origin == recipe.OriginParam.Default) {
		return recipe.Service
	}
	if u, err := url.Parse(origin); err == nil && u.Host != "" {
		return recipe.Service + " (" + u.Host + ")"
	}
	return recipe.Service + " (" + origin + ")"
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
	// TierCounts is the connection's declared exposed-tool read/write split
	// (add-integration-authority task 2.5), projected from its recipe via
	// domain.ToolTierCounts — declaration-derived counts (verbs for http
	// kind, the tool-tier list for mcp kind). An unregistered recipe counts
	// zero. Always served: the agent config integrations section reads it to
	// show what non-admin members can drive.
	TierCounts domain.RecipeTierCounts `json:"tier_counts"`
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
// attached to that server. An http-kind connection has no server row to join:
// its view is kind-aware (status from the persisted row, verb-count tool
// count, attachment by connection id — connections_http.go). An mcp
// connection whose server row is absent reads as unknown with no attachments
// — absence is a degraded state, not an error.
func (s *ConnectionsService) buildView(ctx context.Context, workspaceID string, conn *domain.Connection) (*ConnectionView, error) {
	view := &ConnectionView{Connection: *conn, AttachedAgents: []string{}}

	// Declaration-derived tier counts (add-integration-authority task 2.5):
	// every view carries its recipe's read/write split; an unregistered
	// recipe counts zero (domain.ToolTierCounts' nil rule).
	view.TierCounts = domain.ToolTierCounts(domain.RecipeByID(conn.Service))

	if recipe := domain.RecipeByID(conn.Service); recipe != nil && recipe.Kind == domain.RecipeKindHTTP {
		return s.buildHTTPView(ctx, workspaceID, conn, recipe)
	}

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
	// A persisted `expired` connection status wins over the server row's
	// status: expired is the OAuth recovery state the MCP runtime cannot
	// write, so no later probe outcome may mask it (add-connection-oauth D6).
	// The provider error detail still rides the server row's status_error.
	if conn.Status == domain.ConnectionStatusExpired {
		view.Status = domain.ConnectionStatusExpired
	} else if view.Status == "" {
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
	// HTTP-kind connections attach by CONNECTION id (there is no server id):
	// the store cascade below strips only linked-server ids, so the connection
	// id is stripped from every agent's enabled_mcps first (add-connection-http
	// tasks 3.2, contract §2). MCP-kind disconnect is untouched.
	if isHTTPConnection(conn.Service) {
		if err := s.stripHTTPAttachments(ctx, workspaceID, conn.ID); err != nil {
			return "", err
		}
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
	// HTTP-kind connections have no linked server: the probe re-runs the
	// recipe's declared call against the pinned base URL with the stored
	// credential and persists the outcome on the connection row through
	// UpdateTokenLifecycle (add-connection-http tasks 3.2, contract §4).
	if recipe := domain.RecipeByID(conn.Service); recipe != nil && recipe.Kind == domain.RecipeKindHTTP {
		return s.probeHTTPConnection(ctx, workspaceID, conn, recipe)
	}
	server, err := s.wsServers.GetByOriginConnection(ctx, workspaceID, conn.ID)
	if err != nil {
		return nil, err
	}
	if server == nil {
		return nil, fmt.Errorf("%w: connection %s has no linked mcp server", domain.ErrNotFound, conn.ID)
	}

	// Refresh-on-resolution (add-connection-oauth D3): a probe dials the
	// connection's credentials, so an OAuth token inside its refresh margin
	// is renewed first — the write-through renews the server's token row the
	// runtime view below then reads. A failed refresh persists the expired
	// transition and the provider error, and the view returns it as a status
	// (the MCP probe convention: a failed status is not a request error).
	if conn.RefreshCiphertext != "" {
		if _, _, err := s.refreshIfNeeded(ctx, conn, server); err != nil {
			return s.buildView(ctx, workspaceID, conn)
		}
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

// ConnectionServiceOf resolves a workspace connection's recipe id (the
// domain.Connection.Service value). It is the runner's
// agents.ConnectionOriginLookup seam (add-integration-authority task 2.1),
// satisfied structurally — the CredentialForConnection precedent — so the
// MCP resolution pass can walk the origin link (server row's
// OriginConnectionID → connection → recipe) without importing this package.
func (s *ConnectionsService) ConnectionServiceOf(ctx context.Context, workspaceID, connectionID string) (string, error) {
	conn, err := s.connections.Get(ctx, workspaceID, connectionID)
	if err != nil {
		return "", err
	}
	return conn.Service, nil
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

// ---------------------------------------------------------------------------
// Attachment management (add-connection-edit tasks 1.1–1.3, design.md D1)
// ---------------------------------------------------------------------------

// SetAttachedAgents sets the COMPLETE set of agents attached to a connection
// (add-connection-edit spec: "Connection attachment management"): it resolves
// the connection's attach id per kind, validates every submitted agent id
// exists in the workspace, diffs the desired set against the current
// attachment, and applies exactly the affected agents' changes inside the
// attachment transaction seam — every affected agent's allowlist updates or
// none does. An empty desired set detaches everyone; re-saving the current
// set writes nothing. Unknown or cross-workspace connections are
// domain.ErrNotFound (the Get convention); an unknown agent id is
// domain.ErrInvalid naming it, with nothing changed.
//
// The stored id per agent is the connection's materialized server id for MCP
// kind and the connection's own id for http kind — identical to what the
// agent-side opt-in stores, so attachment from either side is
// indistinguishable. Each affected agent is re-read INSIDE the seam and only
// the connection's id is added or removed in its enabled_mcps slice (order of
// the other entries preserved), so a concurrent MCP-server opt-in cannot be
// clobbered by a stale array write.
func (s *ConnectionsService) SetAttachedAgents(ctx context.Context, workspaceID, connectionID string, agentIDs []string) (*ConnectionView, error) {
	conn, err := s.connections.Get(ctx, workspaceID, connectionID)
	if err != nil {
		return nil, err
	}
	attachID, err := s.attachmentTargetID(ctx, workspaceID, conn)
	if err != nil {
		return nil, err
	}
	desired := normalizeAgentIDs(agentIDs)
	desiredSet := make(map[string]struct{}, len(desired))
	for _, id := range desired {
		desiredSet[id] = struct{}{}
	}

	workspaceAgents, err := s.agents.ListForWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	// Every submitted id must exist in the workspace BEFORE anything is
	// diffed or written — the rejection is atomic by construction (spec:
	// "Unknown agent rejected atomically").
	known := make(map[string]struct{}, len(workspaceAgents))
	for i := range workspaceAgents {
		known[workspaceAgents[i].ID] = struct{}{}
	}
	for _, id := range desired {
		if _, ok := known[id]; !ok {
			return nil, fmt.Errorf("%w: agent %s does not exist in this workspace", domain.ErrInvalid, id)
		}
	}

	// The diff: only agents whose membership differs get written — an
	// idempotent re-save affects no one.
	var affected []string
	for i := range workspaceAgents {
		agent := &workspaceAgents[i]
		_, want := desiredSet[agent.ID]
		if want != slices.Contains(agent.EnabledMCPS, attachID) {
			affected = append(affected, agent.ID)
		}
	}

	apply := func(ctx context.Context, agents store.AgentStore) error {
		for _, agentID := range affected {
			// Tx-scoped read-modify-write: re-read the row inside the seam so
			// only the connection's id moves in the freshest array.
			agent, err := agents.ByID(ctx, workspaceID, agentID)
			if err != nil {
				return err
			}
			_, want := desiredSet[agent.ID]
			agent.EnabledMCPS = setAttachmentMembership(agent.EnabledMCPS, attachID, want)
			if err := agents.Update(ctx, agent); err != nil {
				return err
			}
		}
		return nil
	}
	if s.attachmentTx != nil {
		if err := s.attachmentTx(ctx, apply); err != nil {
			return nil, err
		}
	} else if err := apply(ctx, s.agents); err != nil {
		return nil, err
	}
	return s.Get(ctx, workspaceID, conn.ID)
}

// attachmentTargetID resolves the id a connection stores in an attached
// agent's enabled_mcps — exactly the resolution buildView reads back: the
// connection's own id for http kind (there is no server row), the linked
// server's id otherwise. A degraded MCP connection whose server row is gone
// has nothing to diff against and is refused (the Probe precedent) — its
// attachments are inert references, not manageable state.
func (s *ConnectionsService) attachmentTargetID(ctx context.Context, workspaceID string, conn *domain.Connection) (string, error) {
	if isHTTPConnection(conn.Service) {
		return conn.ID, nil
	}
	server, err := s.wsServers.GetByOriginConnection(ctx, workspaceID, conn.ID)
	if err != nil {
		return "", err
	}
	if server == nil {
		return "", fmt.Errorf("%w: connection %s has no linked mcp server", domain.ErrNotFound, conn.ID)
	}
	return server.ID, nil
}

// normalizeAgentIDs trims the submitted ids, drops empties, and dedupes
// preserving first occurrence — the desired set is order-free and
// duplicate-tolerant.
func normalizeAgentIDs(ids []string) []string {
	out := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// setAttachmentMembership adds or removes attachID in an enabled_mcps slice:
// adding appends (the existing entries keep their order), removing keeps the
// survivors' relative order. A membership already in the desired state is
// returned unchanged.
func setAttachmentMembership(ids []string, attachID string, member bool) []string {
	if slices.Contains(ids, attachID) == member {
		return ids
	}
	if member {
		return append(ids, attachID)
	}
	stripped := make([]string, 0, len(ids))
	for _, id := range ids {
		if id != attachID {
			stripped = append(stripped, id)
		}
	}
	return stripped
}

// ---------------------------------------------------------------------------
// Token replacement (add-connection-edit tasks 2.1–2.3, design.md D2)
// ---------------------------------------------------------------------------

// ReplaceToken swaps an existing connection's access token in place
// (add-connection-edit spec: "Connection token replacement"): the candidate
// credential is probed exactly the way connect probes a candidate BEFORE any
// write, so a failed probe stores nothing and the previously stored token
// keeps authenticating — the upstream message surfaces through the
// ErrProbeFailed wrap. On success the token row is rewritten in place (MCP
// kind: the materialized server's secret row through the MCP settings
// machinery; http kind: the connection's encrypted envelope —
// replaceHTTPToken) while identity, origin, access level, and every agent
// attachment are untouched. The refreshed view's token hint emerges from the
// normal view build.
//
// OAuth-kind connections are refused with ErrTokenReplaceUnsupported —
// reauthorization is their credential-rotation path (an OAuth `expired`
// status is therefore unreachable here, so a successful replace can never
// manufacture `connected` over it). An empty token is invalid: the
// keep-semantics live at the endpoint/dialog layer, which skips the call for
// an empty submission (design.md D3). Unknown or cross-workspace connections
// are domain.ErrNotFound.
func (s *ConnectionsService) ReplaceToken(ctx context.Context, workspaceID, connectionID, token string) (*ConnectionView, error) {
	conn, err := s.connections.Get(ctx, workspaceID, connectionID)
	if err != nil {
		return nil, err
	}
	recipe := domain.RecipeByID(conn.Service)
	if recipe == nil {
		return nil, fmt.Errorf("%w: %q", domain.ErrUnknownRecipe, conn.Service)
	}
	if recipe.AuthKind == domain.RecipeAuthOAuth {
		return nil, ErrTokenReplaceUnsupported
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, fmt.Errorf("%w: token cannot be empty", domain.ErrInvalid)
	}
	if recipe.Kind == domain.RecipeKindHTTP {
		return s.replaceHTTPToken(ctx, workspaceID, conn, recipe, token)
	}
	return s.replaceMCPToken(ctx, workspaceID, conn, recipe, token)
}

// replaceMCPToken probes the candidate the way connect does —
// materializeServer composes it into an ad-hoc MCPConnection resolved against
// the connection's STORED origin (immutability: a replace never dials any
// origin but the one the connection was connected against) and probeServer
// dials it without persisting — then swaps the linked server's token secret
// row through writeServerToken and refreshes the server status from the
// probe's outcome: the completeReauthorization sequence minus the lifecycle
// write a PAT row never needs (the connection row itself is untouched).
func (s *ConnectionsService) replaceMCPToken(ctx context.Context, workspaceID string, conn *domain.Connection, recipe *domain.Recipe, token string) (*ConnectionView, error) {
	server, err := materializeServer(workspaceID, recipe, token, conn.Origin)
	if err != nil {
		return nil, err
	}
	toolCount, err := s.probeServer(ctx, workspaceID, "connection-probe", server.Name, server.MCPConnection)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrProbeFailed, err)
	}
	linked, err := s.writeServerToken(ctx, workspaceID, conn.ID, recipe, token)
	if err != nil {
		return nil, err
	}
	if err := s.settings.SetWorkspaceServerStatus(ctx, workspaceID, linked.ID, domain.MCPStatusConnected, "", toolCount); err != nil {
		return nil, err
	}
	return s.buildView(ctx, workspaceID, conn)
}
