package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/oniharnantyo/onclaw/internal/agents/mcp"
	"github.com/oniharnantyo/onclaw/internal/agents/mcp/oauth"
	"github.com/oniharnantyo/onclaw/internal/auth/oauthstate"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/secrets"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// ---------------------------------------------------------------------------
// OAuth authorization-code flow over workspace connections (add-connection-
// oauth tasks 2.1–2.5): the authorize-URL builder with a single-use signed
// state (design.md D4), the public callback that exchanges the code, probe-
// gates the candidate, and activates or discards the token set (D5), the
// reauthorization flow that replaces a token set in place (D6), and the
// refresh-on-resolution wrapper (D3). Everything rides the same connections
// machinery as the PAT path: the token materializes into the materialized
// server's single secret row and the lifecycle fields live on the connection
// row (D1) — the MCP runtime is untouched.
// ---------------------------------------------------------------------------

// OAuthCallbackPath is the public callback route the provider redirects back
// to; the instance app's redirect URI is PublicBaseURL + this path (derived,
// never stored — design.md D2).
const OAuthCallbackPath = "/api/v1/integrations/oauth/callback"

// MCPOAuthCallbackPath is the MCP servers' OAuth callback route (add-mcp-
// oauth-client tasks 6.1/D5); the MCP redirect URI is PublicBaseURL + this
// path, derived at read time and never stored. The HTTP routes bound for it
// live in the server layer (task 6.1); the refresh path here only ever uses
// it for metadata-document validation and registration.
const MCPOAuthCallbackPath = "/api/v1/mcp/oauth/callback"

// DefaultOAuthStateTTL bounds the signed state's validity (design.md D4: a
// short TTL); zero selects this default.
const DefaultOAuthStateTTL = 10 * time.Minute

// DefaultTokenHTTPTimeout bounds each token-endpoint exchange/refresh call.
const DefaultTokenHTTPTimeout = 15 * time.Second

// stateContext domain-separates the state HMAC from other uses of the
// instance encryption key.
const stateContext = "onclaw:connection-oauth-state:v1"

// stateRejectionDetail is the single generic detail every rejected state
// produces: the callback must not leak WHICH check failed (task 3.2).
const stateRejectionDetail = "this authorization attempt could not be validated; start again from the integrations gallery"

// OAuth callback outcome statuses — the redirect query's `status` values the
// web gallery consumes.
const (
	OAuthCallbackConnected = "connected"
	OAuthCallbackFailed    = "failed"
)

// Sentinel flow errors (task 3.2). All chain to domain.ErrInvalid so the
// generic sentinel mapping produces 400 invalid_request envelopes.
var (
	// ErrOAuthAppNotRegistered marks a connect/reauthorize attempt for a
	// provider whose instance app is not registered (task 2.5). Wrap sites
	// name the missing registration.
	ErrOAuthAppNotRegistered = fmt.Errorf("%w: the provider's OAuth app is not registered on this instance", domain.ErrInvalid)
	// ErrOAuthStateInvalid marks a callback state that failed signature, TTL,
	// single-use, or shape validation — deliberately ONE generic error so the
	// response never reveals which check rejected the attempt (design.md D4).
	ErrOAuthStateInvalid = fmt.Errorf("%w: %s", domain.ErrInvalid, stateRejectionDetail)
	// ErrOAuthExchange marks a token-endpoint refusal; wrap sites carry the
	// provider's message verbatim (the probe-failure convention).
	ErrOAuthExchange = fmt.Errorf("%w: the provider rejected the token exchange", domain.ErrInvalid)
	// ErrOAuthUnavailable marks OAuth flows on an instance with no public
	// base URL configured (config contract: validate at use, not at boot).
	ErrOAuthUnavailable = fmt.Errorf("%w: OAuth connect is unavailable because the instance public base URL is not configured", domain.ErrInvalid)
)

// OAuthCallbackResult is the callback flow's outcome: exactly what the
// browser redirect's query needs. A rejected state carries no RecipeID (the
// state is untrusted, so the recipe is unknown) and only the generic detail.
type OAuthCallbackResult struct {
	RecipeID string
	Status   string // OAuthCallbackConnected or OAuthCallbackFailed
	Detail   string // failure detail (urlencoded by the handler); empty on success
}

// oauthStateClaims is the signed state payload (design.md D4): the workspace
// and initiating user the consent belongs to, the recipe and access level it
// was started from, the connection being reauthorized (empty for a fresh
// connect), the one-time nonce, and the absolute expiry.
type oauthStateClaims struct {
	WorkspaceID  string `json:"w"`
	UserID       string `json:"u"`
	RecipeID     string `json:"r"`
	AccessLevel  string `json:"a,omitempty"`
	ConnectionID string `json:"c,omitempty"`
	Nonce        string `json:"n"`
	ExpiresAt    int64  `json:"e"`
}

// oauthTokenSet is the provider's token-endpoint response, parsed.
type oauthTokenSet struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int // seconds; 0 = the provider declared no expiry
	Scope        string
}

// beginConnect runs the connect-time half of the OAuth dispatch (tasks.md
// 2.1/2.5): validate the access level, enforce one-connection-per-service,
// require the registered instance app and a usable public base URL, then mint
// the signed state and build the provider authorize URL. Nothing is stored
// and no probe runs — activation happens at the callback.
func (s *ConnectionsService) beginConnect(ctx context.Context, workspaceID, userID string, recipe *domain.Recipe, accessLevel string) (string, error) {
	// The instance base URL is the flow's precondition: without it there is
	// no redirect URI to register and no bounce target (validated at use).
	if s.publicBaseURL == "" {
		return "", ErrOAuthUnavailable
	}
	// An empty access level selects the recipe's flow default (its first
	// declared level — ValidateRecipe guarantees at least one).
	if accessLevel == "" {
		accessLevel = recipe.AccessLevels[0]
	}
	if err := domain.ValidateConnectionAccessLevel(accessLevel); err != nil {
		return "", err
	}
	if !slices.Contains(recipe.AccessLevels, accessLevel) {
		return "", fmt.Errorf("%w: access level %q is not offered by %s", domain.ErrInvalid, accessLevel, recipe.Service)
	}

	// One connection per service (design.md D6): an already-connected service
	// reauthorizes instead of connecting again.
	if existing, err := s.connections.GetByService(ctx, workspaceID, recipe.ID); err == nil && existing != nil {
		return "", fmt.Errorf("%w: %s is already connected in this workspace (connection %s) — use reauthorize instead", domain.ErrConnectionExists, recipe.Service, existing.ID)
	} else if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return "", err
	}

	app, err := s.registeredApp(ctx, recipe)
	if err != nil {
		return "", err
	}

	claims := oauthStateClaims{
		WorkspaceID: workspaceID,
		UserID:      userID,
		RecipeID:    recipe.ID,
		AccessLevel: accessLevel,
	}
	return s.buildAuthorizeURL(claims, recipe, app)
}

// BeginReauthorize starts a new consent flow for an EXISTING connection
// (tasks.md 2.4): the state names the connection, so the callback replaces
// its token set in place — connection id, materialized server, and agent
// attachments preserved. The connection must be an OAuth connection in the
// workspace; unknown/cross-workspace ids are domain.ErrNotFound.
func (s *ConnectionsService) BeginReauthorize(ctx context.Context, workspaceID, userID, connectionID string) (string, error) {
	conn, err := s.connections.Get(ctx, workspaceID, connectionID)
	if err != nil {
		return "", err
	}
	recipe := domain.RecipeByID(conn.Service)
	if recipe == nil || recipe.AuthKind != domain.RecipeAuthOAuth {
		return "", fmt.Errorf("%w: connection %s does not use OAuth authorization", domain.ErrInvalid, conn.ID)
	}
	app, err := s.registeredApp(ctx, recipe)
	if err != nil {
		return "", err
	}

	claims := oauthStateClaims{
		WorkspaceID:  workspaceID,
		UserID:       userID,
		RecipeID:     recipe.ID,
		AccessLevel:  conn.AccessLevel,
		ConnectionID: conn.ID,
	}
	return s.buildAuthorizeURL(claims, recipe, app)
}

// registeredApp resolves the provider's registered instance app — the
// availability gate of record for OAuth recipes (tasks.md 2.5). A missing
// registration is a naming error, never a silent fallback.
func (s *ConnectionsService) registeredApp(ctx context.Context, recipe *domain.Recipe) (*domain.InstanceOAuthApp, error) {
	app, err := s.apps.Get(ctx, recipe.ID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, fmt.Errorf("%w: %s needs the instance admin to register its OAuth app before it can be connected", ErrOAuthAppNotRegistered, recipe.Service)
		}
		return nil, err
	}
	return app, nil
}

// buildAuthorizeURL mints the single-use state and composes the provider
// authorize URL from the recipe's declared endpoint and the registered app
// (design.md D4/D7 — the endpoint is never user input).
func (s *ConnectionsService) buildAuthorizeURL(claims oauthStateClaims, recipe *domain.Recipe, app *domain.InstanceOAuthApp) (string, error) {
	if s.publicBaseURL == "" {
		return "", ErrOAuthUnavailable
	}

	raw, err := s.sealState(claims)
	if err != nil {
		return "", err
	}

	endpoint, err := url.Parse(recipe.AuthorizeURL)
	if err != nil {
		return "", fmt.Errorf("%w: recipe %q declares an unparseable authorize url", domain.ErrInvalid, recipe.ID)
	}
	query := endpoint.Query()
	query.Set("response_type", "code")
	query.Set("client_id", app.ClientID)
	query.Set("redirect_uri", DeriveOAuthRedirectURI(s.publicBaseURL))
	query.Set("state", raw)
	if scopes := recipeScopesForLevel(recipe, claims.AccessLevel); len(scopes) > 0 {
		query.Set("scope", strings.Join(scopes, " "))
	}
	endpoint.RawQuery = query.Encode()
	return endpoint.String(), nil
}

// recipeScopesForLevel resolves the recipe's consent scopes for one access
// level; unknown levels yield none (the authorize URL then omits the param).
func recipeScopesForLevel(recipe *domain.Recipe, accessLevel string) []string {
	for _, s := range recipe.Scopes {
		if s.AccessLevel == accessLevel {
			return s.Scopes
		}
	}
	return nil
}

// DeriveOAuthRedirectURI derives the instance app's redirect URI from the
// instance public base URL (task 2.6) — derived at read time, never stored.
// Delegates to the shared oauthstate machinery (add-mcp-oauth-client D5).
func DeriveOAuthRedirectURI(publicBaseURL string) string {
	return oauthstate.DeriveRedirectURI(publicBaseURL, OAuthCallbackPath)
}

// ---------------------------------------------------------------------------
// Signed state (design.md D4) — the machinery lives in internal/auth/oauthstate
// (add-mcp-oauth-client D5); this flow owns its claims shape, its required-
// field/TTL checks, and its one generic rejection.
// ---------------------------------------------------------------------------

// sealState fills the claims' nonce and expiry defaults, serializes, and MACs
// the claims through the shared sealer: the embedded expiry and the one-time
// nonce make the state short-lived and single-use.
func (s *ConnectionsService) sealState(claims oauthStateClaims) (string, error) {
	if claims.Nonce == "" {
		claims.Nonce = s.state.Nonces().Issue()
	}
	if claims.ExpiresAt == 0 {
		claims.ExpiresAt = time.Now().Add(s.stateTTL).Unix()
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("marshal oauth state: %w", err)
	}
	return s.state.Seal(payload), nil
}

// openState verifies the state and consumes its nonce — replay of a consumed
// state is rejected before any token exchange (design.md D4). Every failure
// is the ONE generic sentinel.
func (s *ConnectionsService) openState(raw string) (oauthStateClaims, error) {
	claims, err := s.verifyState(raw)
	if err != nil {
		return oauthStateClaims{}, err
	}
	if !s.state.Nonces().Consume(claims.Nonce) {
		return oauthStateClaims{}, ErrOAuthStateInvalid
	}
	return claims, nil
}

// verifyState checks the state's signature, shape, and TTL without consuming
// its nonce — the shared verification behind openState and the tolerant
// RecipeIDFromState.
func (s *ConnectionsService) verifyState(raw string) (oauthStateClaims, error) {
	payload, err := s.state.Open(raw)
	if err != nil {
		return oauthStateClaims{}, ErrOAuthStateInvalid
	}
	var claims oauthStateClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return oauthStateClaims{}, ErrOAuthStateInvalid
	}
	if claims.WorkspaceID == "" || claims.UserID == "" || claims.RecipeID == "" || claims.Nonce == "" {
		return oauthStateClaims{}, ErrOAuthStateInvalid
	}
	if time.Now().Unix() > claims.ExpiresAt {
		return oauthStateClaims{}, ErrOAuthStateInvalid
	}
	return claims, nil
}

// RecipeIDFromState tolerantly extracts the recipe id from a signed,
// unexpired state — used by the callback's provider-denial redirect so the
// gallery can surface the failure inline under the right card. It returns ""
// for absent, malformed, forged, or expired states (the redirect then omits
// the oauth param, the pre-fix behavior) and NEVER consumes the nonce: a
// denial ends the flow, but the state stays single-use against the callback.
func (s *ConnectionsService) RecipeIDFromState(raw string) string {
	claims, err := s.verifyState(raw)
	if err != nil {
		return ""
	}
	return claims.RecipeID
}

// ---------------------------------------------------------------------------
// Callback (tasks.md 2.2, design.md D4/D5)
// ---------------------------------------------------------------------------

// HandleCallback completes the consent round trip: validate the state (every
// rejection precedes any token exchange), exchange the code at the recipe's
// token endpoint, probe the candidate with the fresh token, and either
// activate the connection (fresh connect) or replace the token set in place
// (reauthorization). Probe failure discards the exchanged token set — no
// connection row, no server row, nothing written (store-nothing hygiene,
// design.md D5). The result is exactly the redirect descriptor: failures
// travel as OAuthCallbackFailed with a client-safe detail, never as errors.
func (s *ConnectionsService) HandleCallback(ctx context.Context, code, state string) OAuthCallbackResult {
	claims, err := s.openState(state)
	if err != nil {
		return OAuthCallbackResult{Status: OAuthCallbackFailed, Detail: stateRejectionDetail}
	}
	recipe := domain.RecipeByID(claims.RecipeID)
	if recipe == nil || recipe.AuthKind != domain.RecipeAuthOAuth {
		return OAuthCallbackResult{RecipeID: claims.RecipeID, Status: OAuthCallbackFailed, Detail: stateRejectionDetail}
	}
	if strings.TrimSpace(code) == "" {
		return OAuthCallbackResult{RecipeID: recipe.ID, Status: OAuthCallbackFailed, Detail: "the provider did not return an authorization code"}
	}

	app, err := s.registeredApp(ctx, recipe)
	if err != nil {
		return OAuthCallbackResult{RecipeID: recipe.ID, Status: OAuthCallbackFailed, Detail: err.Error()}
	}

	tokenSet, err := s.exchangeCode(ctx, recipe, app, code)
	if err != nil {
		return OAuthCallbackResult{RecipeID: recipe.ID, Status: OAuthCallbackFailed, Detail: err.Error()}
	}

	if claims.ConnectionID != "" {
		return s.completeReauthorization(ctx, claims, recipe, tokenSet)
	}
	return s.completeConnect(ctx, claims, recipe, tokenSet)
}

// completeConnect activates a fresh connection from the exchanged token set:
// probe-gate first (nothing is stored on failure), then persist the
// connection with its full token set and materialize its server — the same
// shape as the PAT connect path, plus the lifecycle fields (contract §3: one
// Create).
func (s *ConnectionsService) completeConnect(ctx context.Context, claims oauthStateClaims, recipe *domain.Recipe, tokenSet *oauthTokenSet) OAuthCallbackResult {
	// One connection per service: the user may have connected the service in
	// another tab while consenting — the conflict redirects with guidance.
	if existing, err := s.connections.GetByService(ctx, claims.WorkspaceID, recipe.ID); err == nil && existing != nil {
		return OAuthCallbackResult{RecipeID: recipe.ID, Status: OAuthCallbackFailed, Detail: fmt.Sprintf("%s is already connected in this workspace (connection %s) — use reauthorize instead", recipe.Service, existing.ID)}
	} else if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return OAuthCallbackResult{RecipeID: recipe.ID, Status: OAuthCallbackFailed, Detail: err.Error()}
	}

	// Origin parameters are PAT-only (domain.ValidateRecipe), so an OAuth
	// connect carries none: the recipe's declared endpoint materializes
	// byte-identically.
	server, err := materializeServer(claims.WorkspaceID, recipe, tokenSet.AccessToken, "")
	if err != nil {
		return OAuthCallbackResult{RecipeID: recipe.ID, Status: OAuthCallbackFailed, Detail: err.Error()}
	}
	toolCount, err := s.probeServer(ctx, claims.WorkspaceID, "connection-probe", server.Name, server.MCPConnection)
	if err != nil {
		return OAuthCallbackResult{RecipeID: recipe.ID, Status: OAuthCallbackFailed, Detail: fmt.Sprintf("%v: %v", ErrProbeFailed, err)}
	}

	conn := &domain.Connection{
		WorkspaceID:   claims.WorkspaceID,
		Service:       recipe.ID,
		AccessLevel:   claims.AccessLevel,
		Status:        domain.ConnectionStatusConnected,
		GrantedScopes: grantedScopes(tokenSet),
	}
	if tokenSet.ExpiresIn > 0 {
		expiresAt := time.Now().Add(time.Duration(tokenSet.ExpiresIn) * time.Second)
		conn.ExpiresAt = &expiresAt
	}
	if tokenSet.RefreshToken != "" {
		envelope, err := secrets.Encrypt(s.encKey, []byte(claims.WorkspaceID), []byte(tokenSet.RefreshToken))
		if err != nil {
			return OAuthCallbackResult{RecipeID: recipe.ID, Status: OAuthCallbackFailed, Detail: err.Error()}
		}
		conn.RefreshCiphertext = envelope
	}

	if err := s.connections.Create(ctx, conn); err != nil {
		return OAuthCallbackResult{RecipeID: recipe.ID, Status: OAuthCallbackFailed, Detail: err.Error()}
	}
	// Birth-stamp the origin marker and materialize the server (design.md
	// D1); compensate the connection away if the materialization fails — the
	// PAT path's cleanup, mirrored.
	server.OriginConnectionID = conn.ID
	if err := s.settings.CreateWorkspaceServer(ctx, server); err != nil {
		if delErr := s.connections.Delete(ctx, claims.WorkspaceID, conn.ID); delErr != nil {
			return OAuthCallbackResult{RecipeID: recipe.ID, Status: OAuthCallbackFailed, Detail: fmt.Sprintf("mcp server creation failed (%v) and the connection cleanup failed too: %v", err, delErr)}
		}
		return OAuthCallbackResult{RecipeID: recipe.ID, Status: OAuthCallbackFailed, Detail: err.Error()}
	}
	if err := s.settings.SetWorkspaceServerStatus(ctx, claims.WorkspaceID, server.ID, domain.MCPStatusConnected, "", toolCount); err != nil {
		return OAuthCallbackResult{RecipeID: recipe.ID, Status: OAuthCallbackFailed, Detail: err.Error()}
	}
	return OAuthCallbackResult{RecipeID: recipe.ID, Status: OAuthCallbackConnected}
}

// completeReauthorization replaces an existing connection's token set in
// place (tasks.md 2.4): the connection id, the materialized server, and every
// agent attachment are preserved; the token row and the lifecycle fields are
// replaced and the status returns to connected (expired → connected is the
// one legal exit from expired, design.md D6). Probe failure replaces nothing.
func (s *ConnectionsService) completeReauthorization(ctx context.Context, claims oauthStateClaims, recipe *domain.Recipe, tokenSet *oauthTokenSet) OAuthCallbackResult {
	conn, err := s.connections.Get(ctx, claims.WorkspaceID, claims.ConnectionID)
	if err != nil {
		return OAuthCallbackResult{RecipeID: recipe.ID, Status: OAuthCallbackFailed, Detail: err.Error()}
	}

	// The probe candidate resolves against the connection's STORED origin
	// (add-recipe-base-url tasks.md 2.3 — immutability): reauthorization
	// replaces the token set only, never the origin or the materialized URL.
	server, err := materializeServer(claims.WorkspaceID, recipe, tokenSet.AccessToken, conn.Origin)
	if err != nil {
		return OAuthCallbackResult{RecipeID: recipe.ID, Status: OAuthCallbackFailed, Detail: err.Error()}
	}
	toolCount, err := s.probeServer(ctx, claims.WorkspaceID, "connection-probe", server.Name, server.MCPConnection)
	if err != nil {
		return OAuthCallbackResult{RecipeID: recipe.ID, Status: OAuthCallbackFailed, Detail: fmt.Sprintf("%v: %v", ErrProbeFailed, err)}
	}

	// In-place token replacement on the connection row.
	conn.Status = domain.ConnectionStatusConnected
	conn.GrantedScopes = grantedScopes(tokenSet)
	if tokenSet.ExpiresIn > 0 {
		expiresAt := time.Now().Add(time.Duration(tokenSet.ExpiresIn) * time.Second)
		conn.ExpiresAt = &expiresAt
	}
	if tokenSet.RefreshToken != "" {
		envelope, err := secrets.Encrypt(s.encKey, []byte(claims.WorkspaceID), []byte(tokenSet.RefreshToken))
		if err != nil {
			return OAuthCallbackResult{RecipeID: recipe.ID, Status: OAuthCallbackFailed, Detail: err.Error()}
		}
		conn.RefreshCiphertext = envelope
	}
	if err := s.connections.UpdateTokenLifecycle(ctx, conn); err != nil {
		return OAuthCallbackResult{RecipeID: recipe.ID, Status: OAuthCallbackFailed, Detail: err.Error()}
	}

	// In-place token replacement on the materialized server's secret row:
	// merge-on-name keeps every other stored secret and the server identity.
	linked, err := s.writeServerToken(ctx, claims.WorkspaceID, conn.ID, recipe, tokenSet.AccessToken)
	if err != nil {
		return OAuthCallbackResult{RecipeID: recipe.ID, Status: OAuthCallbackFailed, Detail: err.Error()}
	}
	if err := s.settings.SetWorkspaceServerStatus(ctx, claims.WorkspaceID, linked.ID, domain.MCPStatusConnected, "", toolCount); err != nil {
		return OAuthCallbackResult{RecipeID: recipe.ID, Status: OAuthCallbackFailed, Detail: err.Error()}
	}
	return OAuthCallbackResult{RecipeID: recipe.ID, Status: OAuthCallbackConnected}
}

// writeServerToken replaces the token row's value on the connection's
// materialized server through the MCP settings machinery (design.md D1): the
// stored row's other secrets merge through untouched and the new plaintext is
// encrypted at persistence. The plaintext exists only in memory. The updated
// server is returned (the caller persists status against its id).
func (s *ConnectionsService) writeServerToken(ctx context.Context, workspaceID, connectionID string, recipe *domain.Recipe, accessToken string) (*domain.WorkspaceMCPServer, error) {
	server, err := s.wsServers.GetByOriginConnection(ctx, workspaceID, connectionID)
	if err != nil {
		return nil, err
	}
	if server == nil {
		return nil, fmt.Errorf("%w: connection %s has no linked mcp server", domain.ErrNotFound, connectionID)
	}

	value := accessToken
	if recipe.TokenScheme != "" {
		value = recipe.TokenScheme + " " + accessToken
	}
	row := domain.EnvRow{Name: recipe.TokenHeader, Value: value}
	replaced := false
	for i := range server.Headers {
		if server.Headers[i].Name == recipe.TokenHeader {
			server.Headers[i] = row
			replaced = true
		}
	}
	if !replaced {
		server.Headers = append(server.Headers, row)
	}
	for i := range server.Env {
		if server.Env[i].Name == recipe.TokenHeader {
			server.Env[i] = row
			replaced = true
		}
	}
	return server, s.settings.UpdateWorkspaceServer(ctx, server)
}

// ---------------------------------------------------------------------------
// Token endpoint (exchange + refresh)
// ---------------------------------------------------------------------------

// exchangeCode swaps the authorization code for a token set at the recipe's
// token endpoint with the registered app's decrypted client credentials. The
// provider's error message rides verbatim (the probe-failure convention);
// secrets never do.
func (s *ConnectionsService) exchangeCode(ctx context.Context, recipe *domain.Recipe, app *domain.InstanceOAuthApp, code string) (*oauthTokenSet, error) {
	clientSecret, err := secrets.Decrypt(s.encKey, nil, app.ClientSecretCiphertext)
	if err != nil {
		return nil, fmt.Errorf("%w: the registered app's client secret could not be opened: %v", ErrOAuthExchange, err)
	}

	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {app.ClientID},
		"client_secret": {string(clientSecret)},
		"redirect_uri":  {DeriveOAuthRedirectURI(s.publicBaseURL)},
	}
	return s.postTokenForm(ctx, recipe.TokenURL, form)
}

// refresh exchanges the stored refresh token for a fresh access token
// (tasks.md 2.3). An omitted response refresh token keeps the stored one —
// Atlassian rotates, others replay.
func (s *ConnectionsService) refresh(ctx context.Context, recipe *domain.Recipe, app *domain.InstanceOAuthApp, refreshToken string) (*oauthTokenSet, error) {
	clientSecret, err := secrets.Decrypt(s.encKey, nil, app.ClientSecretCiphertext)
	if err != nil {
		return nil, fmt.Errorf("%w: the registered app's client secret could not be opened: %v", ErrOAuthExchange, err)
	}

	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {app.ClientID},
		"client_secret": {string(clientSecret)},
	}
	return s.postTokenForm(ctx, recipe.TokenURL, form)
}

// postTokenForm POSTs one form-encoded grant request and parses the JSON
// token response.
func (s *ConnectionsService) postTokenForm(ctx context.Context, tokenURL string, form url.Values) (*oauthTokenSet, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrOAuthExchange, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: the token endpoint could not be reached: %v", ErrOAuthExchange, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("%w: reading the token response failed: %v", ErrOAuthExchange, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%w: the provider answered %s: %s", ErrOAuthExchange, resp.Status, tokenEndpointError(body))
	}

	var parsed struct {
		AccessToken      string `json:"access_token"`
		RefreshToken     string `json:"refresh_token"`
		ExpiresIn        int    `json:"expires_in"`
		Scope            string `json:"scope"`
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("%w: the token response was not valid JSON", ErrOAuthExchange)
	}
	if parsed.AccessToken == "" {
		detail := tokenEndpointError(body)
		if detail == "" {
			detail = "the token response carried no access token"
		}
		return nil, fmt.Errorf("%w: %s", ErrOAuthExchange, detail)
	}
	return &oauthTokenSet{
		AccessToken:  parsed.AccessToken,
		RefreshToken: parsed.RefreshToken,
		ExpiresIn:    parsed.ExpiresIn,
		Scope:        parsed.Scope,
	}, nil
}

// tokenEndpointError extracts the provider's OAuth error message from a
// failure body — the human detail surfaces to the user, nothing else does.
func tokenEndpointError(body []byte) string {
	var parsed struct {
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	if err := json.Unmarshal(body, &parsed); err == nil && (parsed.ErrorDescription != "" || parsed.Error != "") {
		if parsed.ErrorDescription != "" {
			return parsed.ErrorDescription
		}
		return parsed.Error
	}
	return strings.TrimSpace(string(body))
}

// grantedScopes normalizes the provider's scope echo (space- or comma-
// separated) into the connection's stored array.
func grantedScopes(tokenSet *oauthTokenSet) []string {
	if strings.TrimSpace(tokenSet.Scope) == "" {
		return []string{}
	}
	fields := strings.FieldsFunc(tokenSet.Scope, func(r rune) bool {
		return r == ' ' || r == ',' || r == '\t' || r == '\n'
	})
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Refresh-on-resolution (tasks.md 2.3, design.md D3)
// ---------------------------------------------------------------------------

// ResolveCredential is the credential-resolution wrapper (design.md D3): it
// yields the connection's token row value — the exact string the materialized
// server's secret row carries — refreshing the access token first when the
// stored expiry is inside the recipe's refresh margin (recipe value, else
// DefaultRecipeRefreshMargin). A successful refresh writes through BOTH rows:
// the connection's lifecycle fields (UpdateTokenLifecycle) and the
// materialized server's token secret row (through the MCP settings machinery)
// — the MCP runtime needs no changes. A failed refresh flips the connection
// to expired with the provider error as the server's status detail and
// returns the error.
//
// Non-OAuth connections (PAT, no refresh envelope, no declared expiry) pass
// straight through: resolution never mutates them.
func (s *ConnectionsService) ResolveCredential(ctx context.Context, workspaceID, service string) (string, error) {
	conn, err := s.connections.GetByService(ctx, workspaceID, service)
	if err != nil {
		return "", err
	}
	server, err := s.wsServers.GetByOriginConnection(ctx, workspaceID, conn.ID)
	if err != nil {
		return "", err
	}
	if server == nil {
		return "", fmt.Errorf("%w: connection %s has no linked mcp server", domain.ErrNotFound, conn.ID)
	}

	if _, refreshed, err := s.refreshIfNeeded(ctx, conn, server); err != nil {
		return "", err
	} else if !refreshed {
		// Passthrough: the stored row's value is current.
		row, err := s.settings.WorkspaceServerForRuntime(ctx, workspaceID, server.ID)
		if err != nil {
			return "", err
		}
		return tokenRowValue(row, recipeTokenHeader(conn.Service)), nil
	}
	// Refreshed: the write-through renewed the server's token row.
	row, err := s.settings.WorkspaceServerForRuntime(ctx, workspaceID, server.ID)
	if err != nil {
		return "", err
	}
	return tokenRowValue(row, recipeTokenHeader(conn.Service)), nil
}

// refreshIfNeeded performs the within-margin refresh when the connection
// carries a refresh envelope and its expiry is inside the margin. refreshed
// reports whether the token set was renewed. The connection status is NOT
// changed on success: per design.md D6 expired is cleared only by
// reauthorization. A failure persists the expired transition (from any live
// status — connected/error→expired, expired→expired idempotent) and the
// provider error as the server's status detail.
func (s *ConnectionsService) refreshIfNeeded(ctx context.Context, conn *domain.Connection, server *domain.WorkspaceMCPServer) (tokenValue string, refreshed bool, err error) {
	recipe := domain.RecipeByID(conn.Service)
	if recipe == nil || recipe.AuthKind != domain.RecipeAuthOAuth || conn.RefreshCiphertext == "" || conn.ExpiresAt == nil {
		return "", false, nil
	}

	margin := recipe.RefreshMargin
	if margin <= 0 {
		margin = domain.DefaultRecipeRefreshMargin
	}
	if time.Until(*conn.ExpiresAt) > margin {
		return "", false, nil
	}

	app, err := s.apps.Get(ctx, conn.Service)
	if err != nil {
		detail := fmt.Sprintf("%s needs the instance admin to register its OAuth app before its token can be refreshed", recipe.Service)
		s.expireConnection(ctx, conn, server, detail)
		return "", false, errors.New(detail)
	}

	refreshToken, err := secrets.Decrypt(s.encKey, []byte(conn.WorkspaceID), conn.RefreshCiphertext)
	if err != nil {
		detail := "the stored refresh token could not be opened"
		s.expireConnection(ctx, conn, server, detail)
		return "", false, errors.New(detail)
	}

	tokenSet, err := s.refresh(ctx, recipe, app, string(refreshToken))
	if err != nil {
		detail := err.Error()
		s.expireConnection(ctx, conn, server, detail)
		return "", false, err
	}

	// Renew the lifecycle fields. An omitted refresh token keeps the stored
	// envelope (rotation vs replay); an omitted scope echo keeps the granted
	// set. Status is untouched (D6: cleared only by reauthorization).
	if tokenSet.RefreshToken != "" {
		envelope, err := secrets.Encrypt(s.encKey, []byte(conn.WorkspaceID), []byte(tokenSet.RefreshToken))
		if err != nil {
			return "", false, err
		}
		conn.RefreshCiphertext = envelope
	}
	expiresAt := time.Now().Add(time.Duration(tokenSet.ExpiresIn) * time.Second)
	conn.ExpiresAt = &expiresAt
	if scopes := grantedScopes(tokenSet); len(scopes) > 0 {
		conn.GrantedScopes = scopes
	}
	if err := s.connections.UpdateTokenLifecycle(ctx, conn); err != nil {
		return "", false, err
	}

	// Write-through the new access token into the server's token row (D1).
	if _, err := s.writeServerToken(ctx, conn.WorkspaceID, conn.ID, recipe, tokenSet.AccessToken); err != nil {
		return "", false, err
	}

	value := tokenSet.AccessToken
	if recipe.TokenScheme != "" {
		value = recipe.TokenScheme + " " + tokenSet.AccessToken
	}
	return value, true, nil
}

// expireConnection persists the expired transition (tasks.md 2.3): the
// connection row moves to expired through UpdateTokenLifecycle — the
// transition's only write path — and the provider error lands on the
// materialized server's status detail (the connection row deliberately
// carries no error column; the view joins it). The transition guard is
// honored: refused moves never write.
func (s *ConnectionsService) expireConnection(ctx context.Context, conn *domain.Connection, server *domain.WorkspaceMCPServer, detail string) {
	if !domain.CanTransitionConnectionStatus(conn.Status, domain.ConnectionStatusExpired) {
		return
	}
	conn.Status = domain.ConnectionStatusExpired
	if err := s.connections.UpdateTokenLifecycle(ctx, conn); err != nil {
		slog.Error("connection expired transition persist failed", "connection_id", conn.ID, "error", err)
	}
	if server != nil {
		if err := s.settings.SetWorkspaceServerStatus(ctx, conn.WorkspaceID, server.ID, domain.MCPStatusError, detail, server.ToolCount); err != nil {
			slog.Error("connection expiry status detail persist failed", "connection_id", conn.ID, "error", err)
		}
	}
}

// refreshForServer is the runtime resolution hook's entry point: for a
// connection-linked server it runs the within-margin refresh with the dual
// write-through (connection row + server token row). Servers without a linked
// connection, PAT connections, and rows outside the margin are a no-op; a
// refresh failure has already persisted the expired transition and the
// provider error — the error return is the caller's fail-open signal, never a
// reason to withhold the stored credential.
func (s *ConnectionsService) refreshForServer(ctx context.Context, workspaceID, serverID string) error {
	server, err := s.wsServers.Get(ctx, workspaceID, serverID)
	if err != nil || server == nil || server.OriginConnectionID == "" {
		// Unknown id (the inner service reports it) or an ordinary
		// hand-made server: nothing connection-owned to refresh.
		return nil
	}
	conn, err := s.connections.Get(ctx, workspaceID, server.OriginConnectionID)
	if err != nil || conn.RefreshCiphertext == "" {
		// Unlinked/deleted connection or a PAT connection: pass through.
		return nil
	}
	_, _, err = s.refreshIfNeeded(ctx, conn, server)
	return err
}

// ---------------------------------------------------------------------------
// Runtime credential source (tasks.md 2.3 on the run path, design.md D3)
// ---------------------------------------------------------------------------

// RuntimeSettingsService is the MCP settings service slice the runtime
// credential source wraps — declared structurally (the same interface-first
// composition as WorkspaceMCPSecretStore: *agents.MCPSettingsService satisfies
// it; services cannot import the agents package, which imports services).
type RuntimeSettingsService interface {
	WorkspaceServerForRuntime(ctx context.Context, workspaceID, id string) (*domain.WorkspaceMCPServer, error)
	WorkspaceServersForRuntime(ctx context.Context, workspaceID string) ([]domain.WorkspaceMCPServer, error)
	AgentServersForRuntimeByID(ctx context.Context, agentID string) ([]domain.AgentMCPServer, error)
	SetWorkspaceServerStatus(ctx context.Context, workspaceID, id, status, statusError string, toolCount int) error
	SetAgentServerStatusByID(ctx context.Context, agentID, id, status, statusError string, toolCount int) error
}

// RuntimeCredentialSource is the composition-root adapter that puts
// refresh-on-resolution on the run path (the spec scenario: an attached
// agent's run resolving the connection's credentials): it implements the two
// structural ports the MCP runtime consumes — mcp.SettingsRuntime (the
// service-backed MCPPolicy + StatusWriter resolving every run's toolset) and
// mcp.WorkspaceServerRuntimeSource (the hooks mcp_tool invoker) — wrapping the
// real settings service. Any CONNECTION-LINKED server row resolved for runtime
// first runs the within-margin refresh; the dual write-through renews the
// connection's lifecycle row and the server's Authorization secret row, and
// the inner read then yields the fresh credential. With the MCP OAuth
// refresher wired (WithMCPOAuthTokenRefresher), OAUTH-mode server rows ride
// the same fail-open refresh against the MCP token store (add-mcp-oauth-client
// tasks 4.6/5.2, design.md D6/D7). Everything else — ordinary servers, status
// persistence — delegates untouched.
//
// Refresh failures are FAIL-OPEN by design (design.md: the run degrades
// exactly as it does for an errored MCP server): the expired transition and
// the provider error are already persisted by the refresh path, and the
// stored credential is returned so the dial proceeds.
type RuntimeCredentialSource struct {
	inner    RuntimeSettingsService
	conns    *ConnectionsService
	mcpOAuth MCPOAuthTokenRefresher
}

// RuntimeSourceOption configures a RuntimeCredentialSource.
type RuntimeSourceOption func(*RuntimeCredentialSource)

// WithMCPOAuthTokenRefresher wires the MCP OAuth token refresh (the oauth-mode
// half of refresh-on-resolution) into the source. Nil is ignored — an
// unwired source keeps the connection-only behavior byte-identical.
func WithMCPOAuthTokenRefresher(r MCPOAuthTokenRefresher) RuntimeSourceOption {
	return func(s *RuntimeCredentialSource) {
		if r != nil {
			s.mcpOAuth = r
		}
	}
}

// MCPOAuthTokenRefresher is the MCP-oauth half of the runtime credential
// source (add-mcp-oauth-client task 5.2): the fail-open within-margin refresh
// of the stored OAuth token sets for oauth-mode server rows — both scopes,
// workspace-registered (agentID empty) and agent-private. Static-mode rows
// are a no-op; resolution never fails a dial.
type MCPOAuthTokenRefresher interface {
	// RefreshForServer renews one server row's token set when it is inside
	// the refresh margin. Unknown ids, non-oauth rows, rows with no stored
	// credential, and discovery/strategy trouble are silent no-ops; only
	// store-level failures surface (and the callers fail open regardless).
	RefreshForServer(ctx context.Context, workspaceID, agentID, serverID string) error
	// RefreshForAgentByID renews every oauth-mode row of one agent
	// (design.md D7: the agent-private path gets the same wrapper the
	// workspace path has).
	RefreshForAgentByID(ctx context.Context, agentID string) error
}

// NewRuntimeCredentialSource wraps a runtime settings service with the
// connections service's refresh-on-resolution (and, when wired, the MCP
// OAuth token refresh).
func NewRuntimeCredentialSource(inner RuntimeSettingsService, conns *ConnectionsService, opts ...RuntimeSourceOption) *RuntimeCredentialSource {
	s := &RuntimeCredentialSource{inner: inner, conns: conns}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// WorkspaceServerForRuntime resolves one server for a dial, refreshing its
// linked connection first when the token is inside the margin (fail-open),
// then its MCP OAuth token set when the row is oauth-mode (fail-open).
func (s *RuntimeCredentialSource) WorkspaceServerForRuntime(ctx context.Context, workspaceID, id string) (*domain.WorkspaceMCPServer, error) {
	// A refresh failure has already persisted the expired transition and the
	// provider error on the rows; the dial proceeds on the stored credential.
	_ = s.conns.refreshForServer(ctx, workspaceID, id)
	if s.mcpOAuth != nil {
		// OAuth-mode rows renew through the MCP token store; every other row
		// no-ops inside the refresher.
		_ = s.mcpOAuth.RefreshForServer(ctx, workspaceID, "", id)
	}
	return s.inner.WorkspaceServerForRuntime(ctx, workspaceID, id)
}

// WorkspaceServersForRuntime resolves the workspace's server set for a run's
// toolset, refreshing every connection-linked row inside its margin first
// (fail-open per row), then every oauth-mode row's MCP token set.
func (s *RuntimeCredentialSource) WorkspaceServersForRuntime(ctx context.Context, workspaceID string) ([]domain.WorkspaceMCPServer, error) {
	if rows, err := s.conns.wsServers.List(ctx, workspaceID); err == nil {
		for i := range rows {
			if rows[i].OriginConnectionID != "" {
				_ = s.conns.refreshForServer(ctx, workspaceID, rows[i].ID)
				continue
			}
			if s.mcpOAuth != nil && rows[i].AuthMode == domain.MCPAuthModeOAuth {
				_ = s.mcpOAuth.RefreshForServer(ctx, workspaceID, "", rows[i].ID)
			}
		}
	}
	return s.inner.WorkspaceServersForRuntime(ctx, workspaceID)
}

// AgentServersForRuntimeByID resolves an agent's private server set for a
// run's toolset. Agent-private servers have no connections, but oauth-mode
// private rows ride the same MCP token refresh the workspace rows get
// (design.md D7 — probe/run parity for private servers; fail-open per row).
func (s *RuntimeCredentialSource) AgentServersForRuntimeByID(ctx context.Context, agentID string) ([]domain.AgentMCPServer, error) {
	if s.mcpOAuth != nil {
		_ = s.mcpOAuth.RefreshForAgentByID(ctx, agentID)
	}
	return s.inner.AgentServersForRuntimeByID(ctx, agentID)
}

// ---------------------------------------------------------------------------
// MCP OAuth token refresher (tasks 4.6/5.2): the concrete MCPOAuthTokenRefresher
// over the token store, the two MCP server stores, and the oauth.Client — the
// oauth package's CredentialStore and StatusSink seams implemented so that
// package stays store-agnostic (the envelopes ride the workspace-AAD
// derivation, the same as every workspace-scoped secret).
// ---------------------------------------------------------------------------

// mcpOAuthTokenRefresher implements MCPOAuthTokenRefresher, oauth.CredentialStore,
// and oauth.StatusSink.
type mcpOAuthTokenRefresher struct {
	tokens       store.MCPTokens
	wsServers    store.WorkspaceMCPServers
	agentServers store.AgentMCPServers
	settings     RuntimeSettingsService
	encKey       []byte
	// publicBaseURL feeds the derived redirect URI for metadata-document
	// validation and registration; headless instances leave it empty (their
	// BYO/DCR-persisted client ids never need it for a refresh).
	publicBaseURL string
	client        *oauth.Client
}

// NewMCPOAuthTokenRefresher builds the MCP OAuth token refresher from its
// granular dependencies.
func NewMCPOAuthTokenRefresher(tokens store.MCPTokens, wsServers store.WorkspaceMCPServers, agentServers store.AgentMCPServers, settings RuntimeSettingsService, encKey []byte, publicBaseURL string, client *oauth.Client) MCPOAuthTokenRefresher {
	return &mcpOAuthTokenRefresher{
		tokens:        tokens,
		wsServers:     wsServers,
		agentServers:  agentServers,
		settings:      settings,
		encKey:        encKey,
		publicBaseURL: publicBaseURL,
		client:        client,
	}
}

// RefreshForServer runs the within-margin MCP OAuth refresh for one row.
// Non-oauth rows, unknown ids, rows without a client identity or stored
// credential, and discovery/strategy failures are silent no-ops — discovery
// trouble is not a refresh refusal, and the needs-authorization surface is
// the dial path's job. Store-level failures surface (callers fail open).
func (r *mcpOAuthTokenRefresher) RefreshForServer(ctx context.Context, workspaceID, agentID, serverID string) error {
	serverURL, clientID, clientSecret := "", "", ""
	if agentID == "" {
		row, err := r.wsServers.Get(ctx, workspaceID, serverID)
		if err != nil || row == nil {
			return err
		}
		if row.AuthMode != domain.MCPAuthModeOAuth {
			return nil
		}
		serverURL, clientID, clientSecret = row.URL, row.OAuthClientID, row.OAuthClientSecret
	} else {
		row, err := r.agentServers.Get(ctx, agentID, serverID)
		if err != nil || row == nil {
			return err
		}
		if row.AuthMode != domain.MCPAuthModeOAuth {
			return nil
		}
		serverURL, clientID, clientSecret = row.URL, row.OAuthClientID, row.OAuthClientSecret
	}
	// No client identity on the row: the flow never completed (or a DCR
	// registration was never persisted) — there is nothing to present at the
	// token endpoint and nothing to refresh with.
	if clientID == "" {
		return nil
	}
	redirectURI := ""
	if r.publicBaseURL != "" {
		redirectURI = oauthstate.DeriveRedirectURI(r.publicBaseURL, MCPOAuthCallbackPath)
	} else if strings.HasPrefix(clientID, "https://") {
		// A metadata-document client id cannot be validated (or registered)
		// without a public base URL; headless instances skip the refresh
		// rather than fail the row.
		return nil
	}
	meta, err := r.client.Discovery().Discover(ctx, serverURL)
	if err != nil {
		// Fail-open silent: discovery trouble is not a refresh refusal; the
		// stored token still dials and no status moves.
		return nil
	}
	resolved, err := r.client.ResolveClient(ctx, meta, oauth.ClientIdentity{
		ConfiguredClientID:     clientID,
		ConfiguredClientSecret: r.decryptClientSecret(workspaceID, clientSecret),
		RedirectURI:            redirectURI,
		// A persisted DCR registration lands on the row as a client id, so
		// it re-enters here as BYO — the register-once convention (task 4.3).
	})
	if err != nil {
		// Same fail-open silence: the dial proceeds on the stored credential.
		return nil
	}
	_, err = r.client.EnsureFreshCredential(ctx, oauth.EnsureCredentialParams{Meta: meta, Client: resolved}, oauth.CredentialRef{
		WorkspaceID: workspaceID,
		AgentID:     agentID,
		ServerID:    serverID,
	}, r, r)
	return err
}

// RefreshForAgentByID renews every oauth-mode private row of one agent,
// continuing past per-row failures (fail-open) and surfacing the first.
func (r *mcpOAuthTokenRefresher) RefreshForAgentByID(ctx context.Context, agentID string) error {
	if agentID == "" {
		return nil
	}
	rows, err := r.agentServers.List(ctx, agentID)
	if err != nil {
		return err
	}
	var firstErr error
	for i := range rows {
		if rows[i].AuthMode != domain.MCPAuthModeOAuth {
			continue
		}
		if err := r.RefreshForServer(ctx, rows[i].WorkspaceID, agentID, rows[i].ID); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// decryptClientSecret opens the BYO row's secret envelope (the workspace-AAD
// derivation). Rows written before the envelope convention landed carry
// plaintext, which passes through — an undecryptable envelope yields no
// secret, and the resolution then simply fails (fail-open no-op).
func (r *mcpOAuthTokenRefresher) decryptClientSecret(workspaceID, stored string) string {
	if stored == "" {
		return ""
	}
	if strings.HasPrefix(stored, secrets.Version1Prefix) {
		plaintext, err := secrets.Decrypt(r.encKey, []byte(workspaceID), stored)
		if err != nil {
			return ""
		}
		return string(plaintext)
	}
	return stored
}

// Load implements oauth.CredentialStore: the stored token set decrypted for
// the lifecycle. Envelope decryption uses the workspace ID as AAD — the same
// derivation every workspace-scoped secret uses.
func (r *mcpOAuthTokenRefresher) Load(ctx context.Context, ref oauth.CredentialRef) (*oauth.Credential, error) {
	row, err := r.tokens.Get(ctx, ref.WorkspaceID, ref.AgentID, ref.ServerID)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, oauth.ErrNoCredential
	}
	if err != nil {
		return nil, err
	}
	access, err := secrets.Decrypt(r.encKey, []byte(ref.WorkspaceID), row.AccessTokenCiphertext)
	if err != nil {
		return nil, err
	}
	cred := &oauth.Credential{
		AccessToken:   string(access),
		ExpiresAt:     row.ExpiresAt,
		GrantedScopes: row.GrantedScopes,
		Issuer:        row.Issuer,
	}
	if cred.GrantedScopes == nil {
		cred.GrantedScopes = []string{}
	}
	if row.RefreshTokenCiphertext != "" {
		if refreshToken, err := secrets.Decrypt(r.encKey, []byte(ref.WorkspaceID), row.RefreshTokenCiphertext); err == nil {
			cred.RefreshToken = string(refreshToken)
		} else {
			// The envelope cannot be opened: the credential still dials, and
			// the lifecycle treats the refresh as failed (the expired
			// transition).
			cred.RefreshUnreadable = true
		}
	}
	return cred, nil
}

// Replace implements oauth.CredentialStore: the renewed token set re-encrypted
// and create-or-replaced in one write (the store's Replace is the atomic
// write the reauthorization and refresh paths share).
func (r *mcpOAuthTokenRefresher) Replace(ctx context.Context, ref oauth.CredentialRef, cred *oauth.Credential) error {
	aad := []byte(ref.WorkspaceID)
	accessEnvelope, err := secrets.Encrypt(r.encKey, aad, []byte(cred.AccessToken))
	if err != nil {
		return err
	}
	row := &domain.MCPToken{
		WorkspaceID:           ref.WorkspaceID,
		AgentID:               ref.AgentID,
		ServerID:              ref.ServerID,
		AccessTokenCiphertext: accessEnvelope,
		ExpiresAt:             cred.ExpiresAt,
		GrantedScopes:         cred.GrantedScopes,
		Issuer:                cred.Issuer,
	}
	if row.GrantedScopes == nil {
		row.GrantedScopes = []string{}
	}
	if cred.RefreshToken != "" {
		refreshEnvelope, err := secrets.Encrypt(r.encKey, aad, []byte(cred.RefreshToken))
		if err != nil {
			return err
		}
		row.RefreshTokenCiphertext = refreshEnvelope
	}
	return r.tokens.Replace(ctx, row)
}

// MarkExpired implements oauth.StatusSink: the refresh lifecycle's ONLY
// status write, guarded by the design.md D6 transition rules
// (domain.CanTransitionMCPStatus — expired is entered only from a live
// status and held idempotently; refused moves never write).
func (r *mcpOAuthTokenRefresher) MarkExpired(ctx context.Context, ref oauth.CredentialRef, detail string) error {
	if ref.AgentID == "" {
		row, err := r.wsServers.Get(ctx, ref.WorkspaceID, ref.ServerID)
		if err != nil {
			return err
		}
		if !domain.CanTransitionMCPStatus(row.Status, domain.MCPStatusExpired) {
			return nil
		}
		return r.settings.SetWorkspaceServerStatus(ctx, ref.WorkspaceID, ref.ServerID, domain.MCPStatusExpired, detail, row.ToolCount)
	}
	row, err := r.agentServers.Get(ctx, ref.AgentID, ref.ServerID)
	if err != nil {
		return err
	}
	if !domain.CanTransitionMCPStatus(row.Status, domain.MCPStatusExpired) {
		return nil
	}
	return r.settings.SetAgentServerStatusByID(ctx, ref.AgentID, ref.ServerID, domain.MCPStatusExpired, detail, row.ToolCount)
}

// NewMCPOAuthDialCredentials exposes the same token machinery as
// NewMCPOAuthTokenRefresher under the MCP runtime's dial-time seam
// (add-mcp-oauth-client task 5.1, design.md D1): the mcp package resolves an
// oauth-mode dial's bearer through it before the transport opens. Same
// granular dependencies, same stores — build both from one composition-root
// site so the oauth.Client (and its discovery cache) is shared.
func NewMCPOAuthDialCredentials(tokens store.MCPTokens, wsServers store.WorkspaceMCPServers, agentServers store.AgentMCPServers, settings RuntimeSettingsService, encKey []byte, publicBaseURL string, client *oauth.Client) mcp.OAuthDialCredentials {
	return &mcpOAuthTokenRefresher{
		tokens:        tokens,
		wsServers:     wsServers,
		agentServers:  agentServers,
		settings:      settings,
		encKey:        encKey,
		publicBaseURL: publicBaseURL,
		client:        client,
	}
}

// BearerForDial implements mcp.OAuthDialCredentials: the usable access token
// for one server row's oauth-mode dial (design.md D1).
//
//   - A stored token inside its refresh margin is renewed first
//     (EnsureFreshCredential's fail-open semantics: a refused refresh
//     persists the expired transition through the status sink and the STORED
//     token still dials).
//   - No usable credential → the mcp.ErrAuthorizationRequired signal the
//     dial turns into the row's needs-authorization status detail — never a
//     run failure.
//   - Discovery and client-resolution failures surface as their typed errors
//     (the spec's undiscoverable-server and registration-refusal details).
//
// A row with no client identity (BYO id and no persisted registration) cannot
// run the refresh lifecycle; it fails open on a stored token directly and
// reports needs-authorization otherwise — the authorize-begin flow is what
// mints the client identity (DCR) or the user supplies one (BYO).
func (r *mcpOAuthTokenRefresher) BearerForDial(ctx context.Context, workspaceID, agentID, serverID string) (string, error) {
	serverURL, clientID, clientSecret := "", "", ""
	if agentID == "" {
		row, err := r.wsServers.Get(ctx, workspaceID, serverID)
		if err != nil {
			return "", err
		}
		serverURL, clientID, clientSecret = row.URL, row.OAuthClientID, row.OAuthClientSecret
	} else {
		row, err := r.agentServers.Get(ctx, agentID, serverID)
		if err != nil {
			return "", err
		}
		serverURL, clientID, clientSecret = row.URL, row.OAuthClientID, row.OAuthClientSecret
	}
	ref := oauth.CredentialRef{WorkspaceID: workspaceID, AgentID: agentID, ServerID: serverID}
	if clientID == "" {
		cred, err := r.Load(ctx, ref)
		if errors.Is(err, oauth.ErrNoCredential) {
			return "", mcp.NewAuthorizationRequiredError()
		}
		if err != nil {
			return "", err
		}
		// Fail open: the dial only needs a bearer; the refresh lifecycle (and
		// the needs-authorization surface when it stops working) resumes once
		// the row carries a client identity again.
		return cred.AccessToken, nil
	}
	redirectURI := ""
	if r.publicBaseURL != "" {
		redirectURI = oauthstate.DeriveRedirectURI(r.publicBaseURL, MCPOAuthCallbackPath)
	}
	meta, err := r.client.Discovery().Discover(ctx, serverURL)
	if err != nil {
		return "", err
	}
	resolved, err := r.client.ResolveClient(ctx, meta, oauth.ClientIdentity{
		ConfiguredClientID:     clientID,
		ConfiguredClientSecret: r.decryptClientSecret(workspaceID, clientSecret),
		RedirectURI:            redirectURI,
		// A persisted DCR registration lands on the row as a client id, so
		// it re-enters here as BYO — the register-once convention (task 4.3).
	})
	if err != nil {
		return "", err
	}
	token, err := r.client.EnsureFreshCredential(ctx, oauth.EnsureCredentialParams{Meta: meta, Client: resolved}, ref, r, r)
	if errors.Is(err, oauth.ErrNoCredential) {
		return "", mcp.NewAuthorizationRequiredError()
	}
	return token, err
}

// SetWorkspaceServerStatus delegates to the inner service's guarded
// persistence.
func (s *RuntimeCredentialSource) SetWorkspaceServerStatus(ctx context.Context, workspaceID, id, status, statusError string, toolCount int) error {
	return s.inner.SetWorkspaceServerStatus(ctx, workspaceID, id, status, statusError, toolCount)
}

// SetAgentServerStatusByID delegates to the inner service's guarded
// persistence.
func (s *RuntimeCredentialSource) SetAgentServerStatusByID(ctx context.Context, agentID, id, status, statusError string, toolCount int) error {
	return s.inner.SetAgentServerStatusByID(ctx, agentID, id, status, statusError, toolCount)
}

// ---------------------------------------------------------------------------
// Gallery availability (tasks.md 2.5)
// ---------------------------------------------------------------------------

// EnrichRecipes computes the gallery's effective availability at serve time:
// an OAuth recipe is available iff its provider's instance app is registered
// — the flip needs only the registration, no code change (spec: OAuth recipe
// availability). The declared availability is registry data; what is served
// is always the computed value. PAT recipes pass through untouched.
func (s *ConnectionsService) EnrichRecipes(ctx context.Context) []domain.Recipe {
	recipes := domain.Recipes()
	for i := range recipes {
		recipe := &recipes[i]
		if recipe.AuthKind != domain.RecipeAuthOAuth {
			continue
		}
		if _, err := s.apps.Get(ctx, recipe.ID); err == nil {
			recipe.Availability = domain.RecipeAvailable
		} else {
			recipe.Availability = domain.RecipeComingSoon
		}
		if s.publicBaseURL != "" {
			recipe.OauthRedirectURI = DeriveOAuthRedirectURI(s.publicBaseURL)
		}
	}
	return recipes
}

// recipeTokenHeader resolves the recipe's token row name; an unregistered
// recipe yields no row (the value then reads as empty).
func recipeTokenHeader(service string) string {
	if recipe := domain.RecipeByID(service); recipe != nil {
		return recipe.TokenHeader
	}
	return ""
}

// tokenRowValue reads the token row's value out of a runtime-view server.
func tokenRowValue(server *domain.WorkspaceMCPServer, rowName string) string {
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
