package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/secrets"
)

// ---------------------------------------------------------------------------
// HTTP-kind connections (add-connection-http tasks 3.1–3.3): a recipe whose
// kind is http contributes its declared verb tools WITHOUT materializing a
// workspace MCP server. The connection lifecycle is otherwise identical —
// probe-gated connect, status, disconnect cascade — with three kind-specific
// differences:
//
//   - The probe is the recipe's declared call (method + path against the
//     pinned base URL, contract §4) with the token composed into the auth
//     header (TokenScheme + " " + token, or the raw token), not an MCP dial.
//   - The token lives as an AES-256-GCM envelope on the connection row's
//     ciphertext column — the same workspace-scoped AAD and instance master
//     key as every other secret (D3) — because no server row exists to hold
//     a secret row. It is reachable ONLY through CredentialForConnection and
//     reads back as a last-4 hint; the column is json:"-" so it never
//     serializes.
//   - Attachment uses the CONNECTION id in Agent.EnabledMCPS (there is no
//     server id), so disconnect strips that id from every agent service-side
//     — the store cascade only removes linked-server ids (contract §2).
// ---------------------------------------------------------------------------

// httpProbeBodyExcerptCap bounds the provider-body excerpt quoted in probe
// errors — the web-fetch response-discipline convention (D4): a size cap so
// an upstream error page cannot flood the connect dialog.
const httpProbeBodyExcerptCap = 4096

// httpBodyTruncationMarker appends when the provider body exceeded the
// excerpt cap (the web.fetch truncation-marker convention, sized down).
const httpBodyTruncationMarker = "…[truncated]"

// connectHTTP runs the kind-aware connect (tasks 3.1): the recipe's probe
// call gates BEFORE anything is stored (store-nothing-on-failure — a failure
// leaves no connection row and no ciphertext), then the connection persists
// with the token's encrypted envelope. NO workspace MCP server row and NO
// origin marker are created; the declared verb surface is the recipe's verbs.
func (s *ConnectionsService) connectHTTP(ctx context.Context, workspaceID string, recipe *domain.Recipe, accessLevel, token string) (*ConnectResult, error) {
	if err := s.probeHTTPBound(ctx, recipe, token); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrProbeFailed, err)
	}

	envelope, err := secrets.Encrypt(s.encKey, []byte(workspaceID), []byte(token))
	if err != nil {
		return nil, fmt.Errorf("encrypt connection token: %w", err)
	}
	conn := &domain.Connection{
		WorkspaceID: workspaceID,
		Service:     recipe.ID,
		AccessLevel: accessLevel,
		Status:      domain.ConnectionStatusConnected,
		// The http secret row: the access token's envelope rides the
		// connection's ciphertext column (workspace-scoped, instance master
		// key, json:"-" — never serialized), readable only through
		// CredentialForConnection.
		RefreshCiphertext: envelope,
	}
	if err := s.connections.Create(ctx, conn); err != nil {
		return nil, err
	}
	view, err := s.buildView(ctx, workspaceID, conn)
	if err != nil {
		return nil, err
	}
	return &ConnectResult{Connection: view}, nil
}

// probeHTTPBound runs the http probe within the probe bound — the same
// DefaultConnectionProbeTimeout/WithProbeTimeout knob the MCP lane uses
// (contract §4).
func (s *ConnectionsService) probeHTTPBound(ctx context.Context, recipe *domain.Recipe, token string) error {
	pctx, cancel := context.WithTimeout(ctx, s.probeTimeout)
	defer cancel()
	return s.probeHTTP(pctx, recipe, token)
}

// probeHTTP executes the recipe's declared probe call against the pinned base
// URL (contract §4): recipe.Probe.Method  BaseURL + Probe.Path with the single
// auth header composed from TokenScheme. Any 2xx succeeds; a non-2xx, a
// transport error, or a timeout fails with the status code and the provider's
// message — never headers, never the token.
func (s *ConnectionsService) probeHTTP(ctx context.Context, recipe *domain.Recipe, token string) error {
	req, err := http.NewRequestWithContext(ctx, recipe.Probe.Method, httpJoinedURL(recipe.BaseURL, recipe.Probe.Path), nil)
	if err != nil {
		return fmt.Errorf("the probe call could not be built: %v", err)
	}
	req.Header.Set(recipe.TokenHeader, composeTokenValue(recipe, token))
	req.Header.Set("Accept", "application/json")

	// The shared HTTP lane (the OAuth token endpoint's client): the probe
	// bound above is the effective deadline.
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("the probe call could not be completed: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, httpProbeBodyExcerptCap+1))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	message := httpProviderMessage(body, httpProbeBodyExcerptCap)
	if message == "" {
		return fmt.Errorf("the provider answered %s", resp.Status)
	}
	return fmt.Errorf("the provider answered %s: %s", resp.Status, message)
}

// httpJoinedURL joins the pinned base URL and a rooted path. Both halves are
// recipe data validated at registration (rooted path, absolute base URL), so
// the join is pure string composition — never user input.
func httpJoinedURL(baseURL, path string) string {
	return strings.TrimRight(baseURL, "/") + path
}

// composeTokenValue applies the change-1 composition rule: scheme == "" means
// the raw token is the value, else "scheme token". Used for the http auth
// header exactly as materializeServer uses it for the secret row value.
func composeTokenValue(recipe *domain.Recipe, token string) string {
	if recipe.TokenScheme == "" {
		return token
	}
	return recipe.TokenScheme + " " + token
}

// httpProviderMessage extracts the provider's human message from a failure
// body: common JSON error fields first, else the size-capped excerpt. The
// result never carries headers or credential material — provider messages
// quote at most the rejected token's acceptance, not its value.
func httpProviderMessage(body []byte, cap int) string {
	var parsed struct {
		Message     string `json:"message"`
		Error       string `json:"error"`
		Err         string `json:"err"`
		Description string `json:"error_description"`
	}
	if err := json.Unmarshal(body, &parsed); err == nil {
		for _, v := range []string{parsed.Message, parsed.Error, parsed.Err, parsed.Description} {
			if v != "" {
				return v
			}
		}
	}
	excerpt := strings.TrimSpace(string(body))
	if len(body) > cap {
		excerpt = strings.TrimSpace(string(body[:cap])) + httpBodyTruncationMarker
	}
	return excerpt
}

// ---------------------------------------------------------------------------
// Credential resolution (contract §3) — the seam the runner's connection
// tool source consumes per verb-call.
// ---------------------------------------------------------------------------

// CredentialForConnection returns the http-kind connection's DECRYPTED RAW
// token — no scheme composition; the request engine composes the header value
// per the recipe's TokenScheme at call time. Unknown and cross-workspace
// connection ids are domain.ErrNotFound (chained), indistinguishable. Errors
// NEVER contain credential material (D3).
func (s *ConnectionsService) CredentialForConnection(ctx context.Context, workspaceID, connectionID string) (string, error) {
	conn, err := s.connections.Get(ctx, workspaceID, connectionID)
	if err != nil {
		return "", err
	}
	if !isHTTPConnection(conn.Service) {
		return "", fmt.Errorf("%w: connection %s does not carry http verb tools", domain.ErrInvalid, connectionID)
	}
	return s.decryptConnectionToken(workspaceID, conn)
}

// decryptConnectionToken opens the connection's token envelope (workspace ID
// as AAD). An absent envelope and an unopenable one are both naming errors —
// the plaintext or the envelope text never appears in any message.
func (s *ConnectionsService) decryptConnectionToken(workspaceID string, conn *domain.Connection) (string, error) {
	if conn.RefreshCiphertext == "" {
		return "", fmt.Errorf("%w: connection %s has no stored credential", domain.ErrNotFound, conn.ID)
	}
	plaintext, err := secrets.Decrypt(s.encKey, []byte(workspaceID), conn.RefreshCiphertext)
	if err != nil {
		return "", fmt.Errorf("connection %s credential could not be opened: %v", conn.ID, err)
	}
	return string(plaintext), nil
}

// isHTTPConnection reports whether the service's recipe declares http kind.
// An unregistered recipe is not an http connection.
func isHTTPConnection(service string) bool {
	recipe := domain.RecipeByID(service)
	return recipe != nil && recipe.Kind == domain.RecipeKindHTTP
}

// ---------------------------------------------------------------------------
// Agent attachment (contract §2) — http connections attach by CONNECTION id.
// ---------------------------------------------------------------------------

// AttachedHTTPConnections lists the http-kind connections attached to one
// agent: every Agent.EnabledMCPS id resolved AS A SERVER FIRST (an MCP-kind
// attachment) and only when no server row resolves AS A CONNECTION, checked
// http kind (the resolve-as-server-first-else-connection-lookup). Ids that
// resolve as neither are inert references — skipped, matching the MCP
// convention for references left by deletes. Unknown or cross-workspace
// agents are domain.ErrNotFound.
func (s *ConnectionsService) AttachedHTTPConnections(ctx context.Context, workspaceID, agentID string) ([]domain.Connection, error) {
	agent, err := s.agents.ByID(ctx, workspaceID, agentID)
	if err != nil {
		return nil, err
	}
	out := []domain.Connection{}
	for _, id := range agent.EnabledMCPS {
		server, err := s.wsServers.Get(ctx, workspaceID, id)
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return nil, err
		}
		if server != nil {
			continue // an MCP-kind attachment: the MCP policy owns it.
		}
		conn, err := s.connections.Get(ctx, workspaceID, id)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				continue // inert reference
			}
			return nil, err
		}
		if !isHTTPConnection(conn.Service) {
			continue
		}
		out = append(out, *conn)
	}
	return out, nil
}

// stripHTTPAttachments removes the connection id from every agent's
// enabled_mcps (tasks 3.2): the store's Delete cascade strips only
// linked-server ids, so an http connection's attachment cleanup is this
// read-modify-write over the agent store's Update (which persists
// EnabledMCPS). Run BEFORE the connection row dies — the delete is the point
// of no return, and a failed strip must leave the (still existing) connection
// re-disconnectable rather than orphaned references.
func (s *ConnectionsService) stripHTTPAttachments(ctx context.Context, workspaceID, connectionID string) error {
	agents, err := s.agents.ListForWorkspace(ctx, workspaceID)
	if err != nil {
		return err
	}
	for i := range agents {
		agent := &agents[i]
		if !slices.Contains(agent.EnabledMCPS, connectionID) {
			continue
		}
		stripped := make([]string, 0, len(agent.EnabledMCPS))
		for _, id := range agent.EnabledMCPS {
			if id != connectionID {
				stripped = append(stripped, id)
			}
		}
		agent.EnabledMCPS = stripped
		if err := s.agents.Update(ctx, agent); err != nil {
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Kind-aware views (tasks 3.3)
// ---------------------------------------------------------------------------

// buildHTTPView joins one http-kind connection: status from the PERSISTED
// connection row (there is no server row to join — connect is probe-gated so
// the row is born connected), the token hint decrypted to its last-4, the
// declared verb count as the tool count, and the agents attached by
// CONNECTION id. The server fields carry the http shape: no server_id, and
// server_enabled explicitly false.
func (s *ConnectionsService) buildHTTPView(ctx context.Context, workspaceID string, conn *domain.Connection, recipe *domain.Recipe) (*ConnectionView, error) {
	serverEnabled := false
	view := &ConnectionView{
		Connection:     *conn,
		Status:         conn.Status,
		TokenHint:      httpTokenHint(workspaceID, conn, s.encKey),
		ServerEnabled:  &serverEnabled,
		ToolCount:      len(recipe.Verbs),
		AttachedAgents: []string{},
	}
	if view.Status == "" {
		// The store persists empty as connected; a probe-gated http connect
		// never stores an unprobed row, so unknown would be a lie.
		view.Status = domain.ConnectionStatusConnected
	}
	names, err := s.attachedAgentNames(ctx, workspaceID, conn.ID)
	if err != nil {
		return nil, err
	}
	view.AttachedAgents = names
	return view, nil
}

// httpTokenHint renders the stored token as its last-4 hint (the established
// secret convention): the envelope is decrypted only to recover the hinted
// tail, and an unopenable envelope hints as empty rather than leaking.
func httpTokenHint(workspaceID string, conn *domain.Connection, encKey []byte) string {
	plaintext, err := secrets.Decrypt(encKey, []byte(workspaceID), conn.RefreshCiphertext)
	if err != nil {
		return ""
	}
	value := string(plaintext)
	if len(value) > 4 {
		return value[len(value)-4:]
	}
	return value
}

// probeHTTPConnection re-runs the recipe's probe call with the STORED
// credential (tasks 3.2) and persists the outcome onto the connection row
// through UpdateTokenLifecycle — the only status write path an http
// connection has (contract §4). The refreshed view returns either way: a
// failed probe is a status, not a request error, and the provider message
// rides the response view's status_error (the connection row carries no error
// column, so the detail is response-level, like the MCP row's joined join).
func (s *ConnectionsService) probeHTTPConnection(ctx context.Context, workspaceID string, conn *domain.Connection, recipe *domain.Recipe) (*ConnectionView, error) {
	token, err := s.decryptConnectionToken(workspaceID, conn)
	if err != nil {
		return nil, err
	}

	probeErr := s.probeHTTPBound(ctx, recipe, token)
	status := domain.ConnectionStatusConnected
	if probeErr != nil {
		status = domain.ConnectionStatusError
	}
	// The transition guard is honored: refused moves (e.g. expired) never
	// write. The loaded row carries the envelope, so the lifecycle write
	// preserves the credential.
	if conn.Status == "" {
		conn.Status = domain.ConnectionStatusConnected
	}
	if domain.CanTransitionConnectionStatus(conn.Status, status) {
		conn.Status = status
		if err := s.connections.UpdateTokenLifecycle(ctx, conn); err != nil {
			return nil, err
		}
	}

	view, err := s.buildView(ctx, workspaceID, conn)
	if err != nil {
		return nil, err
	}
	if probeErr != nil {
		view.StatusError = probeErr.Error()
	}
	return view, nil
}
