package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"

	"github.com/oniharnantyo/buddies/internal/agents"
	"github.com/oniharnantyo/buddies/internal/agents/mcp"
	"github.com/oniharnantyo/buddies/internal/store"
)

// The authorization-required status reasons the probe and the OAuth flows
// persist (design D5/D8/D9): each names the pane action that resolves the
// state. The frontend mirrors these in its tests — a reword here is a
// cross-layer contract change.
const (
	// MCPOAuthRequiredMessage marks an OAuth-mode server whose tokens are
	// missing, expired, or rejected: sign in again.
	MCPOAuthRequiredMessage = "Authorization required — click Authorize to sign in."
	// MCPOAuthRequiredToEnableMessage marks a none-mode server found to
	// advertise OAuth: the flow must be enabled through Authorize.
	MCPOAuthRequiredToEnableMessage = "This server requires OAuth authorization — click Authorize to enable it."
	// MCPDisconnectedMessage marks an OAuth server whose session was cut.
	MCPDisconnectedMessage = "Disconnected — click Authorize to sign in again."
)

// mcpAuthorizing holds the servers with an authorization flow in flight.
// Package-level rather than service state because server ids are globally
// unique and the process holds exactly one SettingsService — a second would
// be a wiring bug, not a second authorization lane — and the guard must span
// the out-of-band completion goroutine regardless of call stacks.
var mcpAuthorizing = struct {
	sync.Mutex
	inFlight map[string]struct{}
}{inFlight: make(map[string]struct{})}

// acquireAuthorization claims the server's single in-flight authorization
// slot; false means one is already running.
func acquireAuthorization(serverID string) bool {
	mcpAuthorizing.Lock()
	defer mcpAuthorizing.Unlock()
	if _, busy := mcpAuthorizing.inFlight[serverID]; busy {
		return false
	}
	mcpAuthorizing.inFlight[serverID] = struct{}{}
	return true
}

// releaseAuthorization frees the server's authorization slot.
func releaseAuthorization(serverID string) {
	mcpAuthorizing.Lock()
	defer mcpAuthorizing.Unlock()
	delete(mcpAuthorizing.inFlight, serverID)
}

// AuthorizeMCPServer starts the OAuth browser flow for one server (spec
// agent-mcp "OAuth server authentication"): bounded discovery of the server's
// OAuth advertisement, dynamic client registration when the stored
// registration is missing or was minted by another authorization server, the
// loopback callback listener, and the system browser (design D8). Only that
// synchronous part runs here, bounded like the probe; the browser round-trip
// is human-timescale and completes out-of-band (design D2) — the completion
// re-probes the row and announces the outcome through the mcp:oauth:completed
// event. The registration is persisted before the browser opens so a retried
// flow never re-registers (design D4, redirect fallback included). stdio
// servers, servers that do not advertise OAuth, and a second concurrent
// authorization of the same server are errors; store.ErrNotFound when absent.
func (s *SettingsService) AuthorizeMCPServer(ctx context.Context, id string) error {
	stored, err := s.store.MCPServers().ByID(ctx, id)
	if err != nil {
		return fmt.Errorf("authorize mcp server: %w", err)
	}
	if stored.Transport == mcp.TransportStdio {
		return fmt.Errorf("authorize mcp server: transport %q does not support oauth", stored.Transport)
	}
	if !acquireAuthorization(id) {
		return errors.New("authorize mcp server: authorization already in progress")
	}

	authz, err := s.startMCPOAuth(ctx, stored)
	if err != nil {
		releaseAuthorization(id)
		return err
	}

	go s.awaitMCPOAuthCompletion(id, authz.Done)
	return nil
}

// startMCPOAuth runs the flow's synchronous part (design D2): discover,
// adopt the discovered advertisement into the row's auth settings, register
// or reuse the client, persist the registration, and open the browser. Any
// failure — a browser that cannot open included (design D8) — leaves the row
// auth_required with the reason.
func (s *SettingsService) startMCPOAuth(ctx context.Context, stored *store.MCPServer) (*mcp.Authorization, error) {
	pctx, cancel := context.WithTimeout(ctx, mcp.DefaultConnectTimeout)
	defer cancel()
	d, err := mcp.Discover(pctx, stored.Transport, stored.URL, stored.Headers)
	if err != nil {
		return nil, fmt.Errorf("authorize mcp server: %w", err)
	}

	// The flow flips the row to OAuth mode and adopts the advertisement
	// (design D9: scopes are discovered, never configured). A registration
	// minted by a different authorization server cannot be reused — clear it
	// so the flow re-registers there.
	auth := stored.Auth
	auth.Mode = store.MCPAuthModeOAuth
	if auth.Issuer != "" && auth.Issuer != d.Issuer {
		auth.ClientID, auth.ClientSecret, auth.RedirectURI = "", "", ""
	}
	auth.Issuer = d.Issuer
	auth.ResourceMetadataURL = d.ResourceMetadataURL
	auth.AuthServerMetadataURL = d.AuthServerMetadataURL
	auth.Scopes = slices.Clone(d.Scopes)

	authz, err := mcp.StartAuthorization(pctx, mcp.AuthorizeRequest{
		OAuth: mcp.OAuth{
			Enabled:               true,
			ClientID:              auth.ClientID,
			ClientSecret:          auth.ClientSecret,
			RedirectURI:           auth.RedirectURI,
			Scopes:                auth.Scopes,
			AuthServerMetadataURL: auth.AuthServerMetadataURL,
			// A stored client id is never re-registered (design D3's
			// oauth_confidential for console-minted clients; design D4's
			// persisted registration for retried DCR flows) — not even when
			// the callback port rebinds.
			PreRegistered: auth.ClientID != "",
			// Catalog-declared extras ride the row (design D3): Google's
			// access_type=offline&prompt=consent.
			ExtraAuthParams: auth.ExtraAuthParams,
			Tokens:          agents.NewMCPTokenStore(stored.ID, s.store.MCPOAuthTokens()),
		},
		Discovery:  *d,
		ClientName: "buddies",
	})
	if err != nil {
		return nil, fmt.Errorf("authorize mcp server: %w", err)
	}

	// Persist the registration before the browser opens (design D4): a
	// retried flow reuses it instead of re-registering. Direct store write —
	// the row is otherwise untouched and the cached connection must not be
	// dropped mid-flow for no reason.
	auth.ClientID = authz.ClientID
	auth.ClientSecret = authz.ClientSecret
	auth.RedirectURI = authz.RedirectURI
	stored.Auth = auth
	if err := s.store.MCPServers().Update(ctx, stored); err != nil {
		authz.Cancel()
		return nil, fmt.Errorf("authorize mcp server: persist registration: %w", err)
	}

	if err := s.urlOpener.Open(authz.URL); err != nil {
		authz.Cancel()
		s.remarkMCPAuthRequired(ctx, stored.ID, err.Error())
		return nil, fmt.Errorf("authorize mcp server: open browser: %w", err)
	}
	return authz, nil
}

// awaitMCPOAuthCompletion finishes the flow out-of-band (design D2): on
// success the fresh tokens are put to use immediately — invalidate and probe
// (design D6) — and the completion event is announced only after the row
// reflects the outcome, so a frontend refetch on the event sees the probed
// row. On failure the row returns to auth_required with the reason.
func (s *SettingsService) awaitMCPOAuthCompletion(id string, done <-chan error) {
	err := <-done
	releaseAuthorization(id)

	ctx := context.Background()
	if err == nil {
		if _, perr := s.ProbeMCPServer(ctx, id); perr != nil {
			// The probe already persisted its own outcome on the row; the
			// flow itself succeeded, so the event still reports ok.
			slog.WarnContext(ctx, "mcp oauth: post-authorization probe failed", "server_id", id, "error", perr)
		}
		s.emitter.Emit(EventMCPOAuthCompleted, MCPOAuthCompletedEvent{ServerID: id, OK: true})
		return
	}
	s.remarkMCPAuthRequired(ctx, id, err.Error())
	s.emitter.Emit(EventMCPOAuthCompleted, MCPOAuthCompletedEvent{ServerID: id, OK: false, Error: err.Error()})
}

// remarkMCPAuthRequired rewrites a freshly re-read row's status fields to the
// authorization-required state with reason (design D2/D5): the definition —
// the persisted registration included — is never touched. A failure to
// persist is logged, not raised: the caller's own error carries the news.
func (s *SettingsService) remarkMCPAuthRequired(ctx context.Context, id, statusError string) {
	stored, err := s.store.MCPServers().ByID(ctx, id)
	if err == nil {
		stored.Status = mcp.StatusAuthRequired
		stored.StatusError = statusError
		stored.ToolCount = 0
		err = s.store.MCPServers().Update(ctx, stored)
	}
	if err != nil {
		slog.WarnContext(ctx, "mcp oauth: cannot re-mark row auth_required", "server_id", id, "error", err)
	}
}

// DisconnectMCPServer revokes and clears one server's OAuth session (design
// D9; spec scenario "Disconnect revokes and clears"): a best-effort
// revocation at the authorization server's advertised revocation endpoint —
// refresh token first, else the access token — then the stored tokens are
// deleted, the cached connection is invalidated, and the row returns to
// auth_required. The registration is kept verbatim so re-authorizing never
// re-registers (design D4). store.ErrNotFound when absent; a server that is
// not in OAuth mode has nothing to disconnect.
func (s *SettingsService) DisconnectMCPServer(ctx context.Context, id string) (store.MCPServer, error) {
	stored, err := s.store.MCPServers().ByID(ctx, id)
	if err != nil {
		return store.MCPServer{}, fmt.Errorf("disconnect mcp server: %w", err)
	}
	if !stored.Auth.IsOAuth() {
		return store.MCPServer{}, fmt.Errorf("disconnect mcp server: auth mode %q is not oauth", stored.Auth.Mode)
	}

	tok, err := s.store.MCPOAuthTokens().Get(ctx, id)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return store.MCPServer{}, fmt.Errorf("disconnect mcp server: get token: %w", err)
	}
	if tok != nil && stored.Auth.AuthServerMetadataURL != "" {
		s.revokeMCPToken(ctx, *stored, tok.Token)
	}
	if err := s.store.MCPOAuthTokens().Delete(ctx, id); err != nil {
		return store.MCPServer{}, fmt.Errorf("disconnect mcp server: delete token: %w", err)
	}
	s.invalidateMCP(id)

	stored.Status = mcp.StatusAuthRequired
	stored.StatusError = MCPDisconnectedMessage
	stored.ToolCount = 0
	if err := s.store.MCPServers().Update(ctx, stored); err != nil {
		return store.MCPServer{}, fmt.Errorf("disconnect mcp server: %w", err)
	}
	redactIntegrationCredential(stored)
	return *stored, nil
}

// revokeMCPToken revokes the stored token set at the authorization server's
// advertised revocation endpoint, refresh token first (design D9). The whole
// step is best-effort — every failure is logged and swallowed: a failed
// revocation must not block a disconnect, and a lost revocation degrades to
// an orphaned token at the provider, never to a stuck row.
func (s *SettingsService) revokeMCPToken(ctx context.Context, stored store.MCPServer, encoded []byte) {
	md, err := mcp.FetchAuthServerMetadata(ctx, stored.Auth.AuthServerMetadataURL)
	if err != nil {
		slog.WarnContext(ctx, "mcp oauth: metadata fetch for revocation failed", "server_id", stored.ID, "error", err)
		return
	}
	if md.RevocationEndpoint == "" {
		return
	}
	var token mcp.Token
	if err := json.Unmarshal(encoded, &token); err != nil {
		slog.WarnContext(ctx, "mcp oauth: stored token decode for revocation failed", "server_id", stored.ID, "error", err)
		return
	}
	value, hint := token.AccessToken, "access_token"
	if token.RefreshToken != "" {
		value, hint = token.RefreshToken, "refresh_token"
	}
	if err := mcp.RevokeToken(ctx, md.RevocationEndpoint, value, hint,
		stored.Auth.ClientID, stored.Auth.ClientSecret); err != nil {
		slog.WarnContext(ctx, "mcp oauth: token revocation failed", "server_id", stored.ID, "error", err)
	}
}
