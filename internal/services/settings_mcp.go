package services

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/oniharnantyo/buddies/internal/agents"
	"github.com/oniharnantyo/buddies/internal/agents/mcp"
	"github.com/oniharnantyo/buddies/internal/store"
)

// ErrSystemMCPServer is returned for every mutation addressed at a
// system-provided server id (rebrand-browser-use-system-mcp task 3.3;
// add-computer-use design D2 generalizes the single id to the registry, spec
// agent-mcp "System-provided server"): the row is synthesized by the app and
// supervised by the MCP client manager, so it cannot be created, edited, or
// deleted here, and it is rejected from the stored-definition probe path —
// the supervised probe reports live state instead (ProbeMCPServer).
var ErrSystemMCPServer = errors.New("the id names a system-provided MCP server and cannot be created, edited, deleted, or probed here")

// rejectSystemMCPServer fails the call when the id names a registered
// system-provided server (add-computer-use design D2): every registered id
// is enumerated through mcp.SystemServerIDs, so a newly registered system
// server is rejected here without this service hardcoding one (spec
// agent-mcp "System-provided server": non-deletable, non-editable by
// construction). The wrapping error carries ErrSystemMCPServer.
func rejectSystemMCPServer(action, id string) error {
	for _, systemID := range mcp.SystemServerIDs() {
		if id == systemID {
			return fmt.Errorf("%s mcp server: %w", action, ErrSystemMCPServer)
		}
	}
	return nil
}

// CreateMCPServer stores a new MCP server definition, assigning its ID and
// timestamps. A duplicate server name is an error. The returned definition is
// redacted (add-integrations design D4): an integration credential entered
// through the sheet is stored but never returned — only its key-set state and
// last-four hint.
func (s *SettingsService) CreateMCPServer(ctx context.Context, server store.MCPServer) (store.MCPServer, error) {
	if err := rejectSystemMCPServer("create", server.ID); err != nil {
		return store.MCPServer{}, err
	}
	if err := s.store.MCPServers().Create(ctx, &server); err != nil {
		return store.MCPServer{}, fmt.Errorf("create mcp server: %w", err)
	}
	redactServerSecrets(&server)
	return server, nil
}

// MCPServer returns the MCP server with the given ID; store.ErrNotFound when
// absent. The integration credential is redacted (design D4): only
// CredentialSet and the last-four CredentialHint are exposed.
func (s *SettingsService) MCPServer(ctx context.Context, id string) (store.MCPServer, error) {
	server, err := s.store.MCPServers().ByID(ctx, id)
	if err != nil {
		return store.MCPServer{}, fmt.Errorf("get mcp server: %w", err)
	}
	redactServerSecrets(server)
	return *server, nil
}

// MCPServerView is one MCP pane row: a stored server definition, or a
// synthesized system-provided entry (rebrand-browser-use-system-mcp, spec
// agent-mcp "System-provided server"; add-computer-use design D2 generalizes
// the synthesis to the registry). The MCPServer fields are the row's stored
// view exactly as before; System distinguishes the app-provided servers,
// which have no store row behind them and no edit/remove affordances.
type MCPServerView struct {
	store.MCPServer
	// System marks an app-provided system server (browser-use, computer-use):
	// always enabled, non-deletable, non-editable, its status fed by live
	// supervision state.
	System bool `json:"system"`
}

// MCPServers returns all MCP servers, oldest first, with integration
// credentials redacted (design D4), followed by one synthesized row per
// registered system server (rebrand-browser-use-system-mcp task 3.3;
// add-computer-use design D2 generalizes the synthesis to the registry):
// always enabled, each row's connection status and discovered-tool count
// read from live supervision state rather than any store row, named by its
// spec's display name. The result is never nil.
func (s *SettingsService) MCPServers(ctx context.Context) ([]MCPServerView, error) {
	servers, err := s.store.MCPServers().List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list mcp servers: %w", err)
	}
	systemIDs := mcp.SystemServerIDs()
	views := make([]MCPServerView, 0, len(servers)+len(systemIDs))
	for i := range servers {
		redactServerSecrets(&servers[i])
		views = append(views, MCPServerView{MCPServer: servers[i]})
	}
	if s.systemServerState != nil {
		for _, id := range systemIDs {
			if view, ok := s.systemServerView(id); ok {
				views = append(views, view)
			}
		}
	}
	return views, nil
}

// systemServerView synthesizes one registered system server's pane row from
// live supervision state (rebrand-browser-use-system-mcp task 3.3;
// add-computer-use design D2 generalizes the synthesis to the registry):
// always enabled, status and discovered-tool count read from the supervised
// child, named by the spec's display name. ok=false when the runtime
// supervises no system server with the given id, or the id carries no
// display name — the same conditions under which the listing omits the row,
// so every reachable System row resolves the same way here.
func (s *SettingsService) systemServerView(id string) (MCPServerView, bool) {
	state, ok := s.systemServerState(id)
	if !ok {
		return MCPServerView{}, false
	}
	name, ok := mcp.SystemServerDisplayName(id)
	if !ok {
		return MCPServerView{}, false
	}
	return MCPServerView{
		MCPServer: store.MCPServer{
			ID:          id,
			Name:        name,
			Transport:   mcp.TransportStdio,
			Enabled:     true,
			Status:      state.Status,
			StatusError: state.StatusError,
			ToolCount:   state.ToolCount,
		},
		System: true,
	}, true
}

// UpdateMCPServer rewrites an existing MCP server definition, addressed by
// server.ID; store.ErrNotFound when absent. Connection status fields
// (Status, StatusError, ToolCount) persist the last probe outcome and are
// rewritten as given. Blank write-only secrets — the integration credential
// and the OAuth client secret, pre-registered or DCR-issued alike — preserve
// the stored ones: the redacted reads mean an edit round-trips no secret —
// while a non-blank value replaces it (add-integrations design D4,
// fix-bridge-secret-redaction design D2). A successful update invalidates
// the server's cached connection so the new definition takes effect on the
// next use (design D7); invalidation never fails the write. The returned
// definition is redacted.
func (s *SettingsService) UpdateMCPServer(ctx context.Context, server store.MCPServer) (store.MCPServer, error) {
	if err := rejectSystemMCPServer("update", server.ID); err != nil {
		return store.MCPServer{}, err
	}
	if server.Auth.IntegrationCredential == "" || server.Auth.ClientSecret == "" {
		stored, err := s.store.MCPServers().ByID(ctx, server.ID)
		if err != nil {
			return store.MCPServer{}, fmt.Errorf("update mcp server: %w", err)
		}
		if server.Auth.IntegrationCredential == "" {
			server.Auth.IntegrationCredential = stored.Auth.IntegrationCredential
		}
		if server.Auth.ClientSecret == "" {
			server.Auth.ClientSecret = stored.Auth.ClientSecret
		}
	}
	if err := s.store.MCPServers().Update(ctx, &server); err != nil {
		return store.MCPServer{}, fmt.Errorf("update mcp server: %w", err)
	}
	s.invalidateMCP(server.ID)
	redactServerSecrets(&server)
	return server, nil
}

// DeleteMCPServer removes the MCP server; store.ErrNotFound when absent. A
// successful delete invalidates (and thereby closes) the server's cached
// connection, reaping any stdio child (design D7); invalidation never fails
// the write.
func (s *SettingsService) DeleteMCPServer(ctx context.Context, id string) error {
	if err := rejectSystemMCPServer("delete", id); err != nil {
		return err
	}
	if err := s.store.MCPServers().Delete(ctx, id); err != nil {
		return fmt.Errorf("delete mcp server: %w", err)
	}
	s.invalidateMCP(id)
	return nil
}

// ProbeMCPServer reports a row's connection state on demand (spec agent-mcp
// "Probe and tool discovery"). A stored server is dialed fresh and the
// outcome persists onto the row's status fields: connected with the
// discovered-tool count, or error with the underlying reason and a zero
// count. Refresh is invalidate-then-probe (design D6, spec "Connection
// refresh"): the row's cached connection is dropped before dialing — always,
// even when the probe then fails — so what the row shows and what agents use
// never diverge. An authorization-required outcome (design D5) persists
// StatusAuthRequired with the Authorize-call-to-action for the row's auth
// mode. A probe never mutates the definition — the row is read first and
// only the status/status_error/tool_count columns are rewritten. The dial is
// bounded at the manager's connect timeout; store.ErrNotFound when absent.
//
// A supervised system server has no stored definition to dial: the
// supervision loop is its prober — it spawns the child and feeds the row's
// status and tool count continuously — so the on-demand probe reports the
// live supervision state (spec agent-mcp "System-provided server": the
// system server behaves exactly like a user server, probed on demand). When
// that supervision has hit its crash-loop cap — respawning stopped, nothing
// recovers it automatically — the on-demand probe is the manual path past
// the cap: it re-arms supervision (the supervisor's explicit restart) and
// reports the re-armed state; the next tick's spawn attempt lands the real
// outcome. A registered system id the runtime does not supervise has no row
// to report and stays rejected with ErrSystemMCPServer.
func (s *SettingsService) ProbeMCPServer(ctx context.Context, id string) (MCPServerView, error) {
	if s.systemServerState != nil {
		if view, ok := s.systemServerView(id); ok {
			if state, capped := s.systemServerState(id); capped && state.Capped && s.systemServerRestart != nil {
				s.systemServerRestart(id)
				if re, ok := s.systemServerView(id); ok {
					view = re
				}
			}
			return view, nil
		}
	}
	if err := rejectSystemMCPServer("probe", id); err != nil {
		return MCPServerView{}, err
	}
	stored, err := s.store.MCPServers().ByID(ctx, id)
	if err != nil {
		return MCPServerView{}, fmt.Errorf("probe mcp server: %w", err)
	}
	s.invalidateMCP(id)

	pctx, cancel := context.WithTimeout(ctx, mcp.DefaultConnectTimeout)
	defer cancel()
	count, probeErr := mcp.Probe(pctx, agents.MCPServerRef(*stored, s.store.MCPOAuthTokens()))

	status, statusError := mcp.StatusConnected, ""
	if probeErr != nil {
		status, statusError, count = mcp.StatusError, probeErr.Error(), 0
		if errors.Is(probeErr, mcp.ErrAuthRequired) {
			status = mcp.StatusAuthRequired
			statusError = MCPOAuthRequiredMessage
			if !stored.Auth.IsOAuth() {
				statusError = MCPOAuthRequiredToEnableMessage
			}
		}
	}
	stored.Status = status
	stored.StatusError = statusError
	stored.ToolCount = count
	if err := s.store.MCPServers().Update(ctx, stored); err != nil {
		return MCPServerView{}, fmt.Errorf("probe mcp server: %w", err)
	}
	redactServerSecrets(stored)
	return MCPServerView{MCPServer: *stored}, nil
}

// redactServerSecrets rewrites one server's auth to the redacted read shape
// (add-integrations design D4, fix-bridge-secret-redaction design D1): every
// secret class on the row — the integration credential and the OAuth client
// secret, pre-registered or DCR-issued alike — is consumed and replaced by
// its computed views: CredentialSet/ClientSecretSet plus the last-four
// CredentialHint/ClientSecretHint. Invariant: no settings-service read or
// listing ever returns a client secret or integration credential, whatever
// created the definition.
func redactServerSecrets(server *store.MCPServer) {
	cred := server.Auth.IntegrationCredential
	server.Auth.IntegrationCredential = ""
	server.CredentialSet = cred != ""
	server.CredentialHint = keyHint(cred)
	secret := server.Auth.ClientSecret
	server.Auth.ClientSecret = ""
	server.ClientSecretSet = secret != ""
	server.ClientSecretHint = keyHint(secret)
}

// Integration auth method kinds — the IntegrationConnectInput.AuthMethod
// vocabulary (add-integrations design D3): the frontend catalog declares
// which kinds a service's endpoint accepts; each kind maps onto existing
// machinery.
const (
	// IntegrationAuthMethodDCR connects through OAuth 2.1 with dynamic
	// client registration (GitLab, GitHub, Linear, ...): the first
	// Authorize call registers the client.
	IntegrationAuthMethodDCR = "oauth_dcr"
	// IntegrationAuthMethodConfidential connects through OAuth 2.1 with a
	// pre-registered confidential client minted by the provider's console
	// (Google, Slack): ClientID and ClientSecret ride the row's auth
	// settings and the authorize flow never re-registers them.
	IntegrationAuthMethodConfidential = "oauth_confidential"
	// IntegrationAuthMethodToken connects with a static token/API key,
	// stored in the auth JSON and rendered into the request at dial time
	// (design D3/D4).
	IntegrationAuthMethodToken = "token"
)

// IntegrationConnectInput is the catalog-driven connect sheet payload
// (add-integrations): the frontend catalog owns labels, endpoints, and the
// auth-method declarations; the backend stores the service tag opaquely
// (design D2).
type IntegrationConnectInput struct {
	// Service is the catalog service id (e.g. "gitlab") naming the connect
	// submission; required.
	Service string
	// Endpoint is the instance URL — the catalog default or the user-edited
	// template fill; required.
	Endpoint string
	// AuthMethod is one of the IntegrationAuthMethod* kinds; required.
	AuthMethod string
	// Credential is the token/API key, required for the token method and
	// ignored otherwise. It is stored in the auth JSON, never in Headers
	// (design D4).
	Credential string
	// ClientID and ClientSecret are the pre-registered confidential
	// client's credentials, required for oauth_confidential.
	ClientID     string
	ClientSecret string
	// ExtraAuthParams are extra query parameters appended to the authorize
	// URL (e.g. Google's access_type=offline&prompt=consent).
	ExtraAuthParams map[string]string
	// CredentialHeaderStyle is the catalog-declared header style for the
	// token method (design D3): one of the MCPAuthHeaderStyle* values
	// including the "custom:"/"header:" prefixed forms. Empty means bearer.
	CredentialHeaderStyle string
	// Products, when non-empty, turns the input into a family connect
	// (design D6): one tagged row per product id instead of a single row.
	Products []string
	// ProductEndpoints maps each family product id to its endpoint (design
	// D6: products dial different hosts). Required for every product when
	// Products is non-empty; Endpoint then only validates non-empty.
	ProductEndpoints map[string]string
}

// ConnectIntegration creates the service-tagged MCP server row(s) for one
// catalog-declared connect submission and probes each (spec add-integrations
// "Connect flow"): a token credential rides the row's auth JSON — never
// Headers (design D4) — an oauth_confidential row stores its pre-registered
// client, and an oauth_dcr row is left for Authorize to register. A family
// input (design D6) creates one row per product, each named and tagged with
// its product id, sharing the input's auth settings. Resubmitting a service
// that already has its row is the edit path (spec "Connect flow": editing
// reopens the same sheet): the single service's row is updated in place —
// only the endpoint and the auth settings are rewritten; name, service tag,
// enabled flag, transport, and timestamps stay stored — and the row is
// re-probed. A family resubmission skips its already-connected products (spec
// "Adding a product later reuses the setup") and creates only the missing
// ones. Blank write-only inputs — the token credential, the confidential
// client id and secret — adopt the stored auth of an existing row of the same
// service or family (spec scenario "Edit keeps the stored credential"); on a
// fresh connect the blanks stay blank and are rejected as before. Every
// created or updated row is probed immediately, so the returned row carries
// its tile's initial state — connected, authorization-required, or failed
// with the reason; a probe outcome never fails the call. The first created
// row is returned (family callers find the rest through MCPServers by tag);
// when nothing was created, the first existing row is re-probed and
// returned. An unknown auth method, a method missing its inputs, an empty
// service, endpoint, or product id is an error; so is a duplicate row name.
func (s *SettingsService) ConnectIntegration(ctx context.Context, input IntegrationConnectInput) (store.MCPServer, error) {
	if strings.TrimSpace(input.Service) == "" {
		return store.MCPServer{}, errors.New("connect integration: service is required")
	}
	if strings.TrimSpace(input.Endpoint) == "" {
		return store.MCPServer{}, errors.New("connect integration: endpoint is required")
	}

	family := len(input.Products) > 0
	services := []string{input.Service}
	endpointOf := func(string) string { return input.Endpoint }
	if family {
		services = input.Products
		endpointOf = func(service string) string { return input.ProductEndpoints[service] }
	}

	// The rows already tagged with the submitted services decide edit versus
	// create: a single service's tag updates its row in place, a family's
	// tagged products are skipped as already connected.
	listed, err := s.store.MCPServers().List(ctx)
	if err != nil {
		return store.MCPServer{}, fmt.Errorf("connect integration: %w", err)
	}
	byService := make(map[string]store.MCPServer, len(listed))
	for _, row := range listed {
		if row.Service == "" {
			continue
		}
		if _, ok := byService[row.Service]; !ok {
			byService[row.Service] = row
		}
	}

	// The edit sheet round-trips no secret (design D4), so a blank
	// write-only input adopts the stored auth of the service's row — or, for
	// a family product-only resubmission, of any already-connected product
	// row of the input. Without a stored row the blanks stay blank, so a
	// fresh credential-less connect is still rejected by integrationAuth.
	for _, service := range append([]string{input.Service}, services...) {
		if stored, ok := byService[service]; ok {
			input = withStoredAuth(input, stored.Auth)
			break
		}
	}
	auth, err := integrationAuth(input)
	if err != nil {
		return store.MCPServer{}, err
	}

	var (
		firstCreated  store.MCPServer
		firstExisting store.MCPServer
	)
	for _, service := range services {
		if strings.TrimSpace(service) == "" {
			return store.MCPServer{}, errors.New("connect integration: product id is required")
		}
		stored, exists := byService[service]
		if exists && family {
			// Already connected (spec "Adding a product later reuses the
			// setup"): skip — no duplicate row, no rewrite of the stored
			// setup.
			if firstExisting.ID == "" {
				firstExisting = stored
			}
			continue
		}
		endpoint := endpointOf(service)
		if strings.TrimSpace(endpoint) == "" {
			return store.MCPServer{}, fmt.Errorf("connect integration: endpoint for %q is required", service)
		}
		if exists {
			// Edit in place (spec "Connect flow"): only the endpoint and the
			// auth settings are rewritten — name, service tag, enabled flag,
			// transport, and timestamps stay stored — and the probe below
			// refreshes the status fields.
			edited := stored
			edited.URL = endpoint
			edited.Auth = auth
			if _, err := s.UpdateMCPServer(ctx, edited); err != nil {
				return store.MCPServer{}, fmt.Errorf("connect integration: %w", err)
			}
			probed, err := s.ProbeMCPServer(ctx, edited.ID)
			if err != nil {
				return store.MCPServer{}, fmt.Errorf("connect integration: %w", err)
			}
			stored = probed.MCPServer
			if firstExisting.ID == "" {
				firstExisting = stored
			}
			continue
		}
		created, err := s.CreateMCPServer(ctx, store.MCPServer{
			Name:      service,
			Transport: mcp.TransportStreamableHTTP,
			URL:       endpoint,
			Auth:      auth,
			Service:   service,
			Enabled:   true,
		})
		if err != nil {
			return store.MCPServer{}, fmt.Errorf("connect integration: %w", err)
		}
		probed, err := s.ProbeMCPServer(ctx, created.ID)
		if err != nil {
			return store.MCPServer{}, fmt.Errorf("connect integration: %w", err)
		}
		if firstCreated.ID == "" {
			firstCreated = probed.MCPServer
		}
	}
	if firstCreated.ID != "" {
		return firstCreated, nil
	}
	// Everything submitted was already connected: services is never empty, so
	// an existing row is at hand; re-probe it so the returned view is current.
	probed, err := s.ProbeMCPServer(ctx, firstExisting.ID)
	if err != nil {
		return store.MCPServer{}, fmt.Errorf("connect integration: %w", err)
	}
	return probed.MCPServer, nil
}

// withStoredAuth folds a stored row's write-only auth fields into a connect
// input wherever the input leaves them blank (add-integrations design D4,
// spec scenario "Edit keeps the stored credential"): the redacted reads mean
// an edit sheet resubmits no secret, so a blank token credential keeps the
// stored one and its header style, and blank confidential client fields keep
// the stored client. Non-blank inputs replace the stored values.
func withStoredAuth(input IntegrationConnectInput, stored store.MCPServerAuth) IntegrationConnectInput {
	switch input.AuthMethod {
	case IntegrationAuthMethodToken:
		if input.Credential == "" {
			input.Credential = stored.IntegrationCredential
			input.CredentialHeaderStyle = stored.CredentialHeaderStyle
		}
	case IntegrationAuthMethodConfidential:
		if input.ClientID == "" {
			input.ClientID = stored.ClientID
		}
		if input.ClientSecret == "" {
			input.ClientSecret = stored.ClientSecret
		}
	}
	return input
}

// integrationAuth builds the auth settings a connect input declares
// (add-integrations design D3): each catalog auth-method kind maps onto the
// existing MCPServerAuth machinery. The credential and the client secret live
// in the auth JSON (design D4), never in Headers.
func integrationAuth(input IntegrationConnectInput) (store.MCPServerAuth, error) {
	switch input.AuthMethod {
	case IntegrationAuthMethodDCR:
		return store.MCPServerAuth{
			Mode:            store.MCPAuthModeOAuth,
			ExtraAuthParams: input.ExtraAuthParams,
		}, nil
	case IntegrationAuthMethodConfidential:
		if input.ClientID == "" || input.ClientSecret == "" {
			return store.MCPServerAuth{}, errors.New("connect integration: oauth_confidential requires a client id and secret")
		}
		return store.MCPServerAuth{
			Mode:            store.MCPAuthModeOAuth,
			ClientID:        input.ClientID,
			ClientSecret:    input.ClientSecret,
			ExtraAuthParams: input.ExtraAuthParams,
		}, nil
	case IntegrationAuthMethodToken:
		if input.Credential == "" {
			return store.MCPServerAuth{}, errors.New("connect integration: token method requires a credential")
		}
		// The empty header style means bearer (the store vocabulary).
		return store.MCPServerAuth{
			IntegrationCredential: input.Credential,
			CredentialHeaderStyle: input.CredentialHeaderStyle,
		}, nil
	default:
		return store.MCPServerAuth{}, fmt.Errorf("connect integration: unknown auth method %q", input.AuthMethod)
	}
}

// DisconnectIntegration removes an integration's server definition
// (add-integrations design D1: an integration is a tagged MCPServer row, so
// removing one is a delete): the row goes and its cached connection is
// invalidated. store.ErrNotFound when absent.
func (s *SettingsService) DisconnectIntegration(ctx context.Context, id string) error {
	if err := s.DeleteMCPServer(ctx, id); err != nil {
		return fmt.Errorf("disconnect integration: %w", err)
	}
	return nil
}
