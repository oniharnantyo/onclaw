package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/oniharnantyo/onclaw/internal/agents"
	"github.com/oniharnantyo/onclaw/internal/agents/mcp/oauth"
	"github.com/oniharnantyo/onclaw/internal/auth/oauthstate"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/secrets"
	"github.com/oniharnantyo/onclaw/internal/services"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// ---------------------------------------------------------------------------
// MCP server OAuth surface (add-mcp-oauth-client tasks 6.1–6.3): the
// authorize-begin endpoints bound to server rows — BOTH scopes, permission-
// gated exactly like the server config routes (tools.write for the workspace
// registry, agents.write for agent-private servers) — the PUBLIC callback the
// provider redirects back to (no auth middleware; the signed single-use state
// is the authenticator, the sealed PKCE session rides an HTTP-only cookie),
// and the RFC 8628 device flow's begin/poll pair for headless instances.
//
// Every begin mints a NEW state/session (task 5.3: always a fresh consent);
// every completion REPLACES the server row's token set in one write
// (store.MCPTokens.Replace), persists a fresh DCR registration on the row
// (register once), and moves the row's status to connected — the legal exit
// from expired (domain.CanTransitionMCPStatus) — clearing the status detail.
// ---------------------------------------------------------------------------

// MCPOAuthSessionCookie carries the sealed PKCE verifier session from
// authorize-begin to the callback across the provider's browser round trip.
// SameSite=Lax so the cross-site redirect (a top-level GET navigation)
// presents it; the sealed payload is unforgeable and expires with the state.
const MCPOAuthSessionCookie = "onclaw_mcp_oauth_session"

// deviceSessionContext domain-separates the device-flow session blobs' HMAC
// from the other users of the instance master key.
const deviceSessionContext = "onclaw:mcp-oauth-device:v1"

// mcpServerRow is the flow's view of one bound server row: the facts the
// discovery/strategy chain and the completion persistence need.
type mcpServerRow struct {
	WorkspaceID string
	AgentID     string // empty = workspace scope
	ServerID    string
	AuthMode    string
	URL         string
	ClientID    string
	SecretValue string // stored BYO secret: envelope (or legacy plaintext)
	ToolCount   int
	// stamp applies a fresh DCR registration to the loaded row and persist
	// re-writes it (the register-once convention); they carry no other
	// mutation — status stays owned by the guarded SetStatus writes.
	stamp   func(clientID, sealedSecret string)
	persist func() error
}

// stateRejectionBounceDetail is the one generic detail every rejected
// state/session produces: the callback must not leak WHICH check failed
// (the connections flow's rejection convention).
const stateRejectionBounceDetail = "this authorization attempt could not be validated; start again from the MCP servers pane"

// mcpOAuthHandlers serves the MCP servers' OAuth authorization flows.
type mcpOAuthHandlers struct {
	settings *agents.MCPSettingsService
	// wsServers/agentServers serve the raw stored rows the flows need: the
	// BYO client secret envelope (legacy plaintext tolerated), the row's URL,
	// auth mode, and tool count. The hint/read surfaces stay on the settings
	// service.
	wsServers    store.WorkspaceMCPServers
	agentServers store.AgentMCPServers
	agents       store.AgentStore
	tokens       store.MCPTokens
	client       *oauth.Client
	// encKey seals the device-flow session blobs and the registration
	// secrets; the token envelopes ride the same workspace-AAD derivation.
	encKey []byte
	// publicBaseURL is the instance's externally reachable base URL the
	// redirect URI and the callback bounce derive from; empty legitimately
	// keeps the browser flow unavailable (headless instances use the device
	// flow), mirroring the connections OAuth contract.
	publicBaseURL string
}

// NewMCPOAuthHandlers creates a new mcpOAuthHandlers instance.
func NewMCPOAuthHandlers(settings *agents.MCPSettingsService, wsServers store.WorkspaceMCPServers, agentServers store.AgentMCPServers, agentStore store.AgentStore, tokens store.MCPTokens, client *oauth.Client, encKey []byte, publicBaseURL string) *mcpOAuthHandlers {
	return &mcpOAuthHandlers{
		settings:      settings,
		wsServers:     wsServers,
		agentServers:  agentServers,
		agents:        agentStore,
		tokens:        tokens,
		client:        client,
		encKey:        encKey,
		publicBaseURL: publicBaseURL,
	}
}

// fetchServerRow resolves the bound row for the flow's scope; a row from
// another workspace is domain.ErrNotFound, indistinguishable from an unknown
// id (tenant isolation holds on every flow path).
func (h *mcpOAuthHandlers) fetchServerRow(ctx context.Context, workspaceID, agentID, serverID string) (*mcpServerRow, error) {
	if agentID == "" {
		row, err := h.wsServers.Get(ctx, workspaceID, serverID)
		if err != nil {
			return nil, err
		}
		return &mcpServerRow{
			WorkspaceID: workspaceID,
			ServerID:    serverID,
			AuthMode:    row.AuthMode,
			URL:         row.URL,
			ClientID:    row.OAuthClientID,
			SecretValue: row.OAuthClientSecret,
			ToolCount:   row.ToolCount,
			stamp: func(clientID, sealedSecret string) {
				row.OAuthClientID = clientID
				row.OAuthClientSecret = sealedSecret
			},
			persist: func() error {
				return h.wsServers.Update(ctx, row)
			},
		}, nil
	}
	row, err := h.agentServers.Get(ctx, agentID, serverID)
	if err != nil {
		return nil, err
	}
	if row.WorkspaceID != workspaceID {
		return nil, domain.ErrNotFound
	}
	return &mcpServerRow{
		WorkspaceID: workspaceID,
		AgentID:     agentID,
		ServerID:    serverID,
		AuthMode:    row.AuthMode,
		URL:         row.URL,
		ClientID:    row.OAuthClientID,
		SecretValue: row.OAuthClientSecret,
		ToolCount:   row.ToolCount,
		stamp: func(clientID, sealedSecret string) {
			row.OAuthClientID = clientID
			row.OAuthClientSecret = sealedSecret
		},
		persist: func() error {
			return h.agentServers.Update(ctx, row)
		},
	}, nil
}

// ---------------------------------------------------------------------------
// Authorize begin (tasks 6.1 + 5.3)
// ---------------------------------------------------------------------------

// BeginWorkspaceAuthorize starts a browser consent flow for a
// workspace-registered server (tools.write gate — the config tier).
func (h *mcpOAuthHandlers) BeginWorkspaceAuthorize(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	h.beginAuthorize(c, ws.ID, "", c.Param("id"))
}

// BeginAgentAuthorize starts a browser consent flow for an agent-private
// server (agents.write gate — the private-config tier).
func (h *mcpOAuthHandlers) BeginAgentAuthorize(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	agent, ok := h.resolveAgent(c, ws.ID)
	if !ok {
		return
	}
	h.beginAuthorize(c, ws.ID, agent.ID, c.Param("id"))
}

// beginAuthorize runs the shared begin flow: require a public base URL and an
// oauth-mode row, resolve the discovery + client-identification chain
// (persisting a fresh DCR registration — register once), then mint the
// single-use state and PKCE session. Nothing token-shaped exists yet; the
// completion happens at the callback.
func (h *mcpOAuthHandlers) beginAuthorize(c *gin.Context, workspaceID, agentID, serverID string) {
	// The browser flow's precondition: without a public base URL there is no
	// redirect URI to register and no bounce target (validated at use, like
	// the connections flow).
	if h.publicBaseURL == "" {
		AbortWithError(c, http.StatusBadRequest, CodeInvalidRequest, "OAuth sign-in is unavailable because the instance public base URL is not configured; use the device authorization flow")
		return
	}
	row, err := h.fetchServerRow(c.Request.Context(), workspaceID, agentID, serverID)
	if err != nil {
		RespondError(c, err)
		return
	}
	if row.AuthMode != domain.MCPAuthModeOAuth {
		AbortWithError(c, http.StatusBadRequest, CodeInvalidRequest, "this server is not configured for OAuth authorization")
		return
	}

	meta, resolved, err := h.discoverAndResolve(c.Request.Context(), row)
	if err != nil {
		abortResolutionFailure(c, err)
		return
	}
	// Register once (task 4.3): a fresh DCR result is stamped onto the row
	// BEFORE the browser leaves, so the callback (and every later refresh and
	// dial) re-enters the strategy order as a BYO client.
	if err := h.stampRegistration(c.Request.Context(), row, resolved); err != nil {
		RespondError(c, err)
		return
	}

	begin, err := h.client.BeginAuthorization(oauth.BeginParams{
		Meta:        meta,
		Client:      resolved,
		RedirectURI: oauthstate.DeriveRedirectURI(h.publicBaseURL, services.MCPOAuthCallbackPath),
		Scopes:      meta.Resource.ScopesSupported,
		Claims: oauth.StateClaims{
			WorkspaceID: workspaceID,
			AgentID:     agentID,
			ServerID:    serverID,
		},
	})
	if err != nil {
		RespondError(c, err)
		return
	}

	// The sealed PKCE session rides an HTTP-only cookie through the
	// provider's browser round trip; the state travels on the authorize URL.
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(MCPOAuthSessionCookie, begin.Session,
		int(oauth.DefaultStateTTL.Seconds())+60, "/", "", h.secureCookies(), true)

	RespondOK(c, gin.H{"authorize_url": begin.AuthorizeURL})
}

// ---------------------------------------------------------------------------
// Callback (tasks 6.1 + 5.3)
// ---------------------------------------------------------------------------

// Callback completes the browser consent round trip. The route is PUBLIC —
// the browser arrives from the provider's redirect and the signed single-use
// state is the authenticator (design.md D5). The flow re-discovers (cached)
// against the state's bound server row, re-resolves the client (a persisted
// DCR registration re-enters as BYO), exchanges the code, REPLACES the row's
// token set in one write, persists any fresh registration, and moves the row
// to connected — the legal exit from expired — before bouncing the browser
// back to the settings pane. Every rejection is a bounce, never a JSON
// envelope that would leak which check failed.
func (h *mcpOAuthHandlers) Callback(c *gin.Context) {
	if h.publicBaseURL == "" {
		AbortWithError(c, http.StatusBadRequest, CodeInvalidRequest, "the OAuth callback is unavailable because the instance public base URL is not configured")
		return
	}

	// Provider-side denial (the user declined): bounce with the provider's
	// message; the peeked state names the row when it is valid.
	if errParam := strings.TrimSpace(c.Query("error")); errParam != "" {
		detail := strings.TrimSpace(c.Query("error_description"))
		if detail == "" {
			detail = errParam
		}
		claims, _ := h.client.PeekState(c.Query("state"))
		h.bounce(c, claims.ServerID, "failed", "the provider declined the authorization: "+detail)
		return
	}

	state := c.Query("state")
	claims, err := h.client.PeekState(state)
	if err != nil {
		// Forged/unknown/expired state: the state is untrusted, so the row is
		// unknown — the generic bounce carries no row reference.
		h.bounce(c, "", "failed", stateRejectionBounceDetail)
		return
	}
	row, err := h.fetchServerRow(c.Request.Context(), claims.WorkspaceID, claims.AgentID, claims.ServerID)
	if err != nil || row.AuthMode != domain.MCPAuthModeOAuth {
		h.bounce(c, claims.ServerID, "failed", stateRejectionBounceDetail)
		return
	}
	session, err := c.Cookie(MCPOAuthSessionCookie)
	if err != nil || session == "" {
		h.bounce(c, claims.ServerID, "failed", stateRejectionBounceDetail)
		return
	}

	meta, resolved, err := h.discoverAndResolve(c.Request.Context(), row)
	if err != nil {
		h.bounce(c, claims.ServerID, "failed", resolutionFailureDetail(err))
		return
	}
	result, err := h.client.CompleteAuthorization(c.Request.Context(), oauth.CompleteParams{
		Meta:        meta,
		Client:      resolved,
		RedirectURI: oauthstate.DeriveRedirectURI(h.publicBaseURL, services.MCPOAuthCallbackPath),
		Code:        c.Query("code"),
		State:       state,
		Session:     session,
	})
	if err != nil {
		if errors.Is(err, oauth.ErrStateInvalid) {
			h.bounce(c, claims.ServerID, "failed", stateRejectionBounceDetail)
			return
		}
		h.bounce(c, claims.ServerID, "failed", err.Error())
		return
	}

	// Clear the single-use session cookie; the flow is done either way.
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(MCPOAuthSessionCookie, "", -1, "/", "", h.secureCookies(), true)

	if err := h.complete(c.Request.Context(), row, meta, resolved, result.Tokens); err != nil {
		h.bounce(c, claims.ServerID, "failed", err.Error())
		return
	}
	h.bounce(c, claims.ServerID, "connected", "")
}

// bounce composes the settings-pane redirect for a callback outcome (the
// connections flow's bounce contract, MCP pane): the web reads pane, the
// server reference, the status, and the client-safe detail.
func (h *mcpOAuthHandlers) bounce(c *gin.Context, serverID, status, detail string) {
	values := url.Values{}
	values.Set("pane", "mcp")
	if serverID != "" {
		values.Set("mcp_oauth", serverID)
	}
	values.Set("status", status)
	if detail != "" {
		values.Set("detail", detail)
	}
	c.Redirect(http.StatusFound, strings.TrimRight(h.publicBaseURL, "/")+"/settings?"+values.Encode())
}

// complete persists a finished authorization (browser or device): the token
// set replaces the row's stored set in one write, a fresh DCR registration is
// stamped onto the row (register once), and the row returns to connected with
// the status detail cleared — expired's only exit (design.md D6).
func (h *mcpOAuthHandlers) complete(ctx context.Context, row *mcpServerRow, meta *oauth.Metadata, resolved *oauth.ResolvedClient, tokens *oauth.TokenSet) error {
	issuer := tokens.Issuer
	if issuer == "" {
		issuer = meta.AuthorizationServer.Issuer
	}
	if err := h.replaceTokens(ctx, row, tokens, issuer); err != nil {
		return err
	}
	if err := h.stampRegistration(ctx, row, resolved); err != nil {
		return err
	}
	if row.AgentID == "" {
		return h.settings.SetWorkspaceServerStatus(ctx, row.WorkspaceID, row.ServerID, domain.MCPStatusConnected, "", row.ToolCount)
	}
	return h.settings.SetAgentServerStatus(ctx, row.WorkspaceID, row.AgentID, row.ServerID, domain.MCPStatusConnected, "", row.ToolCount)
}

// replaceTokens re-encrypts the granted token set under the workspace AAD and
// create-or-replaces the row's single token row in one write (task 5.3).
func (h *mcpOAuthHandlers) replaceTokens(ctx context.Context, row *mcpServerRow, tokens *oauth.TokenSet, issuer string) error {
	aad := []byte(row.WorkspaceID)
	accessEnvelope, err := secrets.Encrypt(h.encKey, aad, []byte(tokens.AccessToken))
	if err != nil {
		return err
	}
	tokenRow := &domain.MCPToken{
		WorkspaceID:           row.WorkspaceID,
		AgentID:               row.AgentID,
		ServerID:              row.ServerID,
		AccessTokenCiphertext: accessEnvelope,
		GrantedScopes:         tokens.Scopes,
		Issuer:                issuer,
	}
	if tokenRow.GrantedScopes == nil {
		tokenRow.GrantedScopes = []string{}
	}
	if tokens.ExpiresIn > 0 {
		expiresAt := time.Now().Add(tokens.ExpiresIn)
		tokenRow.ExpiresAt = &expiresAt
	}
	if tokens.RefreshToken != "" {
		refreshEnvelope, err := secrets.Encrypt(h.encKey, aad, []byte(tokens.RefreshToken))
		if err != nil {
			return err
		}
		tokenRow.RefreshTokenCiphertext = refreshEnvelope
	}
	return h.tokens.Replace(ctx, tokenRow)
}

// stampRegistration persists a fresh dynamic registration onto the row (task
// 4.3: register once — the persisted id/secret re-enter every later
// resolution as a BYO client). The registration secret rides the same
// workspace-AAD envelope the BYO rows use.
func (h *mcpOAuthHandlers) stampRegistration(ctx context.Context, row *mcpServerRow, resolved *oauth.ResolvedClient) error {
	if resolved == nil || resolved.Registration == nil || resolved.ReusedRegistration {
		return nil
	}
	reg := resolved.Registration
	if reg.ClientID == "" {
		return nil
	}
	sealedSecret := ""
	if reg.ClientSecret != "" {
		envelope, err := secrets.Encrypt(h.encKey, []byte(row.WorkspaceID), []byte(reg.ClientSecret))
		if err != nil {
			return err
		}
		sealedSecret = envelope
	}
	row.stamp(reg.ClientID, sealedSecret)
	return row.persist()
}

// ---------------------------------------------------------------------------
// Device flow (task 6.2) — headless instances (no public base URL)
// ---------------------------------------------------------------------------

// deviceFlowSession is the sealed poll-session blob: the paste-back flow's
// binding (workspace/agent/server) and the provider's device code. HMAC-
// sealed under the instance master key — unforgeable, opaque to the client,
// short-lived. Repeat polls are the protocol (RFC 8628), so nothing here is
// single-use; the provider's device code is.
type deviceFlowSession struct {
	WorkspaceID string `json:"w"`
	AgentID     string `json:"g,omitempty"`
	ServerID    string `json:"s"`
	DeviceCode  string `json:"d"`
	ExpiresAt   int64  `json:"e"`
}

// deviceSessionTTL bounds a device-flow session blob's sealed lifetime; it
// covers the RFC 8628 default authorization window (10 minutes) with slack.
const deviceSessionTTL = 15 * time.Minute

// BeginWorkspaceDevice starts a device authorization for a
// workspace-registered server (tools.write gate).
func (h *mcpOAuthHandlers) BeginWorkspaceDevice(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	h.beginDevice(c, ws.ID, "", c.Param("id"))
}

// BeginAgentDevice starts a device authorization for an agent-private server
// (agents.write gate).
func (h *mcpOAuthHandlers) BeginAgentDevice(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	agent, ok := h.resolveAgent(c, ws.ID)
	if !ok {
		return
	}
	h.beginDevice(c, ws.ID, agent.ID, c.Param("id"))
}

// beginDevice runs the shared device-begin flow: discovery + client
// resolution (registering once when DCR applies — headless instances
// included: the device grant needs no redirect URI), the RFC 8628
// authorization request, and the sealed session blob the UI echoes to the
// poll endpoint.
func (h *mcpOAuthHandlers) beginDevice(c *gin.Context, workspaceID, agentID, serverID string) {
	row, err := h.fetchServerRow(c.Request.Context(), workspaceID, agentID, serverID)
	if err != nil {
		RespondError(c, err)
		return
	}
	if row.AuthMode != domain.MCPAuthModeOAuth {
		AbortWithError(c, http.StatusBadRequest, CodeInvalidRequest, "this server is not configured for OAuth authorization")
		return
	}
	meta, resolved, err := h.discoverAndResolve(c.Request.Context(), row)
	if err != nil {
		abortResolutionFailure(c, err)
		return
	}
	// The device begin stamps its DCR registration the same way the browser
	// begin does: register once, before any paste-back round trip.
	if err := h.stampRegistration(c.Request.Context(), row, resolved); err != nil {
		RespondError(c, err)
		return
	}
	grant, err := h.client.BeginDeviceAuthorization(c.Request.Context(), meta, resolved, meta.Resource.ScopesSupported)
	if err != nil {
		abortResolutionFailure(c, err)
		return
	}

	sealer := oauthstate.NewSealer(h.encKey, deviceSessionContext, deviceSessionTTL)
	payload, err := json.Marshal(deviceFlowSession{
		WorkspaceID: workspaceID,
		AgentID:     agentID,
		ServerID:    serverID,
		DeviceCode:  grant.DeviceCode,
		ExpiresAt:   time.Now().Add(grant.ExpiresIn).Unix(),
	})
	if err != nil {
		RespondError(c, err)
		return
	}

	response := gin.H{
		"device_session":   sealer.Seal(payload),
		"user_code":        grant.UserCode,
		"verification_uri": grant.VerificationURI,
		"expires_in":       int(grant.ExpiresIn.Seconds()),
		"interval":         int(grant.Interval.Seconds()),
	}
	if grant.VerificationURIComplete != "" {
		response["verification_uri_complete"] = grant.VerificationURIComplete
	}
	RespondOK(c, response)
}

// devicePollRequest is the poll endpoint's body: the sealed session blob from
// the begin response.
type devicePollRequest struct {
	DeviceSession string `json:"device_session"`
}

// PollWorkspaceDevice polls one device-flow token round for a
// workspace-registered server (tools.write gate; the UI polls it).
func (h *mcpOAuthHandlers) PollWorkspaceDevice(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	h.pollDevice(c, ws.ID, "", c.Param("id"))
}

// PollAgentDevice polls one device-flow token round for an agent-private
// server (agents.write gate).
func (h *mcpOAuthHandlers) PollAgentDevice(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	agent, ok := h.resolveAgent(c, ws.ID)
	if !ok {
		return
	}
	h.pollDevice(c, ws.ID, agent.ID, c.Param("id"))
}

// pollDevice runs one RFC 8628 §3.5 poll round and translates the outcome for
// the UI: pending / slow_down keep it polling; completed stores the tokens,
// stamps any fresh registration, and clears the row's status detail;
// expired / denied end the flow. The blob's binding must match the route's
// scope — a mismatched or tampered blob is a 400, never a token write.
func (h *mcpOAuthHandlers) pollDevice(c *gin.Context, workspaceID, agentID, serverID string) {
	var req devicePollRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.DeviceSession == "" {
		RespondError(c, domain.ErrInvalid)
		return
	}
	sealer := oauthstate.NewSealer(h.encKey, deviceSessionContext, deviceSessionTTL)
	payload, err := sealer.Open(req.DeviceSession)
	if err != nil {
		RespondError(c, fmt.Errorf("%w: the device session is not valid", domain.ErrInvalid))
		return
	}
	var blob deviceFlowSession
	if err := json.Unmarshal(payload, &blob); err != nil {
		RespondError(c, fmt.Errorf("%w: the device session is not valid", domain.ErrInvalid))
		return
	}
	if blob.WorkspaceID != workspaceID || blob.AgentID != agentID || blob.ServerID != serverID {
		RespondError(c, fmt.Errorf("%w: the device session does not belong to this server", domain.ErrInvalid))
		return
	}
	if time.Now().Unix() > blob.ExpiresAt {
		RespondOK(c, gin.H{"status": "expired"})
		return
	}
	row, err := h.fetchServerRow(c.Request.Context(), workspaceID, agentID, serverID)
	if err != nil {
		RespondError(c, err)
		return
	}
	if row.AuthMode != domain.MCPAuthModeOAuth {
		AbortWithError(c, http.StatusBadRequest, CodeInvalidRequest, "this server is not configured for OAuth authorization")
		return
	}
	meta, resolved, err := h.discoverAndResolve(c.Request.Context(), row)
	if err != nil {
		abortResolutionFailure(c, err)
		return
	}
	result, err := h.client.PollDeviceToken(c.Request.Context(), meta, resolved, &oauth.DeviceGrant{DeviceCode: blob.DeviceCode})
	if err != nil {
		// Transport/refusal trouble: the flow itself is not terminal — the UI
		// may retry within the device code's lifetime.
		AbortWithError(c, http.StatusBadGateway, CodeInternal, err.Error())
		return
	}
	switch result.Outcome {
	case oauth.DeviceSlowDown:
		RespondOK(c, gin.H{"status": "slow_down"})
	case oauth.DeviceExpiredToken:
		RespondOK(c, gin.H{"status": "expired"})
	case oauth.DeviceAccessDenied:
		detail := "the authorization was denied"
		if result.Detail != "" {
			detail = result.Detail
		}
		RespondOK(c, gin.H{"status": "denied", "detail": detail})
	case oauth.DeviceSuccess:
		if err := h.complete(c.Request.Context(), row, meta, resolved, result.Tokens); err != nil {
			AbortWithError(c, http.StatusBadGateway, CodeInternal, err.Error())
			return
		}
		RespondOK(c, gin.H{"status": "completed"})
	default: // DeviceAuthorizationPending and anything unrecognized.
		RespondOK(c, gin.H{"status": "pending"})
	}
}

// ---------------------------------------------------------------------------
// Shared flow steps
// ---------------------------------------------------------------------------

// discoverAndResolve runs the discovery + client-identification chain for the
// row: the cached discovery, then BYO → metadata-document → DCR with the
// row's configured (or register-once persisted) client identity. The device
// flow reaches here on headless instances too (its grant needs no redirect
// URI); only a metadata-document client id requires one, and its validation
// failure then surfaces with guidance.
func (h *mcpOAuthHandlers) discoverAndResolve(ctx context.Context, row *mcpServerRow) (*oauth.Metadata, *oauth.ResolvedClient, error) {
	redirectURI := ""
	if h.publicBaseURL != "" {
		redirectURI = oauthstate.DeriveRedirectURI(h.publicBaseURL, services.MCPOAuthCallbackPath)
	}
	meta, err := h.client.Discovery().Discover(ctx, row.URL)
	if err != nil {
		return nil, nil, err
	}
	resolved, err := h.client.ResolveClient(ctx, meta, oauth.ClientIdentity{
		ConfiguredClientID:     row.ClientID,
		ConfiguredClientSecret: h.decryptClientSecret(row.WorkspaceID, row.SecretValue),
		RedirectURI:            redirectURI,
	})
	if err != nil {
		return nil, nil, err
	}
	return meta, resolved, nil
}

// decryptClientSecret opens the BYO row's secret envelope (the workspace-AAD
// derivation); rows written before the envelope convention carry plaintext,
// which passes through. An undecryptable envelope yields no secret.
func (h *mcpOAuthHandlers) decryptClientSecret(workspaceID, stored string) string {
	if stored == "" {
		return ""
	}
	if strings.HasPrefix(stored, secrets.Version1Prefix) {
		plaintext, err := secrets.Decrypt(h.encKey, []byte(workspaceID), stored)
		if err != nil {
			return ""
		}
		return string(plaintext)
	}
	return stored
}

// resolveAgent resolves an agent by slug then ID inside the workspace (the
// mcpServerHandlers convention); unknown and cross-tenant agents are 404.
func (h *mcpOAuthHandlers) resolveAgent(c *gin.Context, workspaceID string) (*domain.Agent, bool) {
	identifier := c.Param("agent")
	agent, err := h.agents.BySlug(c.Request.Context(), workspaceID, identifier)
	if err != nil {
		if !errors.Is(err, domain.ErrNotFound) {
			RespondError(c, err)
			return nil, false
		}
		agent, err = h.agents.ByID(c.Request.Context(), workspaceID, identifier)
		if err != nil {
			RespondError(c, err)
			return nil, false
		}
	}
	return agent, true
}

// secureCookies marks the session cookie Secure when the instance serves
// HTTPS publicly.
func (h *mcpOAuthHandlers) secureCookies() bool {
	return strings.HasPrefix(h.publicBaseURL, "https://")
}

// abortResolutionFailure maps a discovery/strategy failure onto the 422
// envelope with the typed detail plus its operator guidance — the remediation
// text the spec requires, surfaced at begin/poll time.
func abortResolutionFailure(c *gin.Context, err error) {
	AbortWithError(c, http.StatusUnprocessableEntity, CodeUnprocessable, resolutionFailureDetail(err))
}

// resolutionFailureDetail renders a discovery or client-resolution error with
// its guidance; other errors render verbatim.
func resolutionFailureDetail(err error) string {
	detail := err.Error()
	var derr *oauth.DiscoveryError
	if errors.As(err, &derr) {
		return detail + " — " + derr.Guidance()
	}
	var cerr *oauth.ClientResolutionError
	if errors.As(err, &cerr) {
		return detail + " — " + cerr.Guidance()
	}
	return detail
}
