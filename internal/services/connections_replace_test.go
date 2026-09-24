package services_test

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/secrets"
	"github.com/oniharnantyo/onclaw/internal/services"
)

// ---------------------------------------------------------------------------
// Connection token replacement (add-connection-edit tasks 2.1–2.4,
// design.md D2): probe-gated in-place swaps for both kinds — the MCP secret
// row through the MCP settings machinery, the http envelope beside
// connectHTTP's create — plus the failed-probe keep, the OAuth-kind refusal,
// and empty-token validation. Fake stores, real crypto, upstream stubbed.
// ---------------------------------------------------------------------------

// storedTokenRow decrypts the named header/env row off the connection's
// materialized server (or fails if the row is gone).
func storedTokenRow(t *testing.T, env *connectionsTestEnv, serverID, rowName string) string {
	t.Helper()
	server, err := env.store.WorkspaceMCPServers().Get(context.Background(), env.wsID, serverID)
	if err != nil {
		t.Fatalf("load server %s: %v", serverID, err)
	}
	for _, row := range append(append([]domain.EnvRow(nil), server.Headers...), server.Env...) {
		if row.Name == rowName {
			plaintext, err := secrets.Decrypt([]byte(testEncKey), []byte(env.wsID), row.Value)
			if err != nil {
				t.Fatalf("decrypt row %s: %v", rowName, err)
			}
			return string(plaintext)
		}
	}
	t.Fatalf("row %q not found on server %s", rowName, serverID)
	return ""
}

// The spec's "Replacement swaps the token in place": the stored secret row is
// rewritten, the hint updates, and every attachment and identity field
// survives; other stored secret rows merge through untouched.
func TestReplaceToken_MCPSwapsSecretRowInPlace(t *testing.T) {
	env := newConnectionsTestEnv(t)
	ctx := context.Background()

	view, err := connectView(t, env, "github", "", testToken)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	agent := seedAttachmentAgent(t, env, "Atlas", "atlas-replace", view.ServerID)

	// An unrelated secret row on the materialized server: the merge-on-name
	// write must keep it while replacing the token row.
	server, err := env.store.WorkspaceMCPServers().Get(ctx, env.wsID, view.ServerID)
	if err != nil {
		t.Fatalf("load server: %v", err)
	}
	server.Headers = append(server.Headers, domain.EnvRow{Name: "X-Extra", Value: "keep-me"})
	if err := env.store.WorkspaceMCPServers().Update(ctx, server); err != nil {
		t.Fatalf("seed extra secret row: %v", err)
	}

	newToken := "ghp_rotated-token-9876"
	got, err := env.svc.ReplaceToken(ctx, env.wsID, view.ID, newToken)
	if err != nil {
		t.Fatalf("replace token: %v", err)
	}

	// The hint emerges from the normal view build: last-4 of the composed
	// row value ("Bearer " + token).
	if want := "9876"; got.TokenHint != want {
		t.Errorf("expected refreshed hint %q, got %q", want, got.TokenHint)
	}
	// Identity, origin, access level, status, and attachments untouched.
	if got.ID != view.ID || got.Service != view.Service || got.Origin != view.Origin || got.AccessLevel != view.AccessLevel {
		t.Errorf("identity fields moved: %+v vs %+v", got.Connection, view.Connection)
	}
	if got.Status != domain.MCPStatusConnected || got.ServerID != view.ServerID {
		t.Errorf("expected the linked server intact, got %q/%q", got.Status, got.ServerID)
	}
	if !slices.Equal(got.AttachedAgents, []string{"Atlas"}) {
		t.Errorf("attached agents = %v, want [Atlas] without re-attachment", got.AttachedAgents)
	}
	if want := []string{view.ServerID}; !slices.Equal(agentAttachments(t, env, agent.ID), want) {
		t.Errorf("agent enabled_mcps = %v, want unchanged %v", agentAttachments(t, env, agent.ID), want)
	}

	// The stored row authenticates with the NEW token now (scheme-composed),
	// and the extra secret row survived the merge.
	if want := "Bearer " + newToken; storedTokenRow(t, env, view.ServerID, "Authorization") != want {
		t.Errorf("expected the swapped row value %q, got %q", want, storedTokenRow(t, env, view.ServerID, "Authorization"))
	}
	if want := "keep-me"; storedTokenRow(t, env, view.ServerID, "X-Extra") != want {
		t.Errorf("expected the extra row to survive the merge, got %q", storedTokenRow(t, env, view.ServerID, "X-Extra"))
	}
}

// The spec's "Failed probe keeps the stored token": nothing is stored and the
// upstream message surfaces.
func TestReplaceToken_FailedProbeKeepsStoredToken(t *testing.T) {
	env := newConnectionsTestEnv(t)
	ctx := context.Background()

	view, err := connectView(t, env, "github", "", testToken)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	agent := seedAttachmentAgent(t, env, "Atlas", "atlas-failed-probe", view.ServerID)

	env.probeErr = errors.New("Bad credentials")
	_, err = env.svc.ReplaceToken(ctx, env.wsID, view.ID, "ghp_rejected-token")
	if !errors.Is(err, services.ErrProbeFailed) {
		t.Fatalf("expected ErrProbeFailed, got %v", err)
	}
	if !strings.Contains(err.Error(), "probe failed: Bad credentials") {
		t.Errorf("expected the upstream message verbatim, got %q", err.Error())
	}

	// The previously stored token keeps authenticating; the hint and the
	// attachment are unchanged.
	if want := "Bearer " + testToken; storedTokenRow(t, env, view.ServerID, "Authorization") != want {
		t.Errorf("expected the stored row untouched, got %q", storedTokenRow(t, env, view.ServerID, "Authorization"))
	}
	got, err := env.svc.Get(ctx, env.wsID, view.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.TokenHint != testTokenHint {
		t.Errorf("expected the old hint %q, got %q", testTokenHint, got.TokenHint)
	}
	if !slices.Equal(got.AttachedAgents, []string{"Atlas"}) {
		t.Errorf("attached agents = %v, want [Atlas]", got.AttachedAgents)
	}
	if want := []string{view.ServerID}; !slices.Equal(agentAttachments(t, env, agent.ID), want) {
		t.Errorf("agent enabled_mcps = %v, want unchanged %v", agentAttachments(t, env, agent.ID), want)
	}
}

// The spec's "OAuth-kind replacement refused": the sentinel error directs to
// reauthorization and nothing is probed or written.
func TestReplaceToken_OAuthKindRefused(t *testing.T) {
	env := newConnectionsTestEnv(t)
	ctx := context.Background()

	conn := &domain.Connection{
		WorkspaceID:   env.wsID,
		Service:       "atlassian", // the builtin OAuth-kind MCP recipe
		AccessLevel:   domain.ConnectionAccessReadOnly,
		Status:        domain.ConnectionStatusConnected,
		GrantedScopes: []string{"read:jira-work"},
	}
	if err := env.store.Connections().Create(ctx, conn); err != nil {
		t.Fatalf("seed oauth connection: %v", err)
	}

	_, err := env.svc.ReplaceToken(ctx, env.wsID, conn.ID, "whatever-token")
	if !errors.Is(err, services.ErrTokenReplaceUnsupported) {
		t.Fatalf("expected ErrTokenReplaceUnsupported, got %v", err)
	}
	if !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("expected the sentinel to chain domain.ErrInvalid, got %v", err)
	}
	if !strings.Contains(err.Error(), "reauthoriz") {
		t.Errorf("expected the message to direct to reauthorization, got %q", err.Error())
	}
	if env.probeCalls != 0 {
		t.Errorf("the refusal must not probe, got %d calls", env.probeCalls)
	}
	after, err := env.store.Connections().Get(ctx, env.wsID, conn.ID)
	if err != nil {
		t.Fatalf("reload connection: %v", err)
	}
	if after.RefreshCiphertext != "" || after.Status != domain.ConnectionStatusConnected {
		t.Errorf("the refusal must not write, got %+v", after)
	}
}

// The http-kind envelope replace: the candidate token is probed against the
// stored origin and the encrypted envelope is swapped in place; attachments
// and identity survive.
func TestReplaceToken_HTTPEnvelopeReplaced(t *testing.T) {
	upstream := newStubUpstream()
	defer upstream.srv.Close()
	recipe := registerHTTPTestRecipe(t, "stubhttp-replace", upstream.srv.URL, "")
	env := newConnectionsTestEnv(t)
	ctx := context.Background()

	view, err := connectHTTPStub(t, env, recipe.ID, httpTestToken)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	agent := seedAttachmentAgent(t, env, "Atlas", "atlas-http-replace", view.ID)

	newToken := "stub-live-token-9999"
	got, err := env.svc.ReplaceToken(ctx, env.wsID, view.ID, newToken)
	if err != nil {
		t.Fatalf("replace token: %v", err)
	}
	if want := "9999"; got.TokenHint != want {
		t.Errorf("expected refreshed hint %q, got %q", want, got.TokenHint)
	}
	if got.ID != view.ID || got.Service != view.Service || got.Origin != view.Origin || got.AccessLevel != view.AccessLevel {
		t.Errorf("identity fields moved: %+v vs %+v", got.Connection, view.Connection)
	}
	if got.Status != domain.ConnectionStatusConnected {
		t.Errorf("expected status connected, got %q", got.Status)
	}
	if !slices.Equal(got.AttachedAgents, []string{"Atlas"}) {
		t.Errorf("attached agents = %v, want [Atlas]", got.AttachedAgents)
	}
	if want := []string{view.ID}; !slices.Equal(agentAttachments(t, env, agent.ID), want) {
		t.Errorf("agent enabled_mcps = %v, want unchanged %v", agentAttachments(t, env, agent.ID), want)
	}

	// The probe call ran the recipe's declared request with the NEW token
	// against the stored origin.
	reqs := upstream.captured()
	if len(reqs) != 2 {
		t.Fatalf("expected connect + replace probes upstream, got %d", len(reqs))
	}
	if reqs[1].uri != "/v1/me" || reqs[1].authValue != newToken {
		t.Errorf("expected the candidate token on the replace probe, got %+v", reqs[1])
	}

	// The stored envelope decrypts to the new raw token (same envelope
	// format, same workspace AAD).
	stored, err := env.store.Connections().Get(ctx, env.wsID, view.ID)
	if err != nil {
		t.Fatalf("reload connection: %v", err)
	}
	plaintext, err := secrets.Decrypt([]byte(testEncKey), []byte(env.wsID), stored.RefreshCiphertext)
	if err != nil || string(plaintext) != newToken {
		t.Errorf("expected the new token under the workspace AAD, got %q (%v)", plaintext, err)
	}
}

// The spec's "Failed probe keeps the stored token" on the http kind: a
// rejected candidate leaves the old envelope and surfaces the provider
// message.
func TestReplaceToken_HTTPFailedProbeKeepsEnvelope(t *testing.T) {
	upstream := newStubUpstream()
	defer upstream.srv.Close()
	recipe := registerHTTPTestRecipe(t, "stubhttp-replace-fail", upstream.srv.URL, "")
	env := newConnectionsTestEnv(t)
	ctx := context.Background()

	view, err := connectHTTPStub(t, env, recipe.ID, httpTestToken)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	upstream.setResponse(http.StatusUnauthorized, `{"error":"this token is revoked"}`)
	_, err = env.svc.ReplaceToken(ctx, env.wsID, view.ID, "stub-rejected-token")
	if !errors.Is(err, services.ErrProbeFailed) {
		t.Fatalf("expected ErrProbeFailed, got %v", err)
	}
	for _, want := range []string{"401 Unauthorized", "this token is revoked"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected the error to carry %q, got %q", want, err.Error())
		}
	}

	got, err := env.svc.Get(ctx, env.wsID, view.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.TokenHint != httpTestHint {
		t.Errorf("expected the old hint %q, got %q", httpTestHint, got.TokenHint)
	}
	stored, err := env.store.Connections().Get(ctx, env.wsID, view.ID)
	if err != nil {
		t.Fatalf("reload connection: %v", err)
	}
	plaintext, err := secrets.Decrypt([]byte(testEncKey), []byte(env.wsID), stored.RefreshCiphertext)
	if err != nil || string(plaintext) != httpTestToken {
		t.Errorf("expected the stored token untouched, got %q (%v)", plaintext, err)
	}
}

// A successful replace clears a stored error status where the transition
// rules allow it (error → connected); the refreshed view carries no stale
// detail.
func TestReplaceToken_HTTPErrorStatusClearsOnSuccess(t *testing.T) {
	upstream := newStubUpstream()
	defer upstream.srv.Close()
	recipe := registerHTTPTestRecipe(t, "stubhttp-replace-status", upstream.srv.URL, "")
	env := newConnectionsTestEnv(t)
	ctx := context.Background()

	view, err := connectHTTPStub(t, env, recipe.ID, httpTestToken)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	upstream.setResponse(http.StatusInternalServerError, `{"message":"upstream down"}`)
	if _, err := env.svc.Probe(ctx, env.wsID, view.ID); err != nil {
		t.Fatalf("probe: %v", err)
	}

	upstream.setResponse(http.StatusOK, `{"ok":true}`)
	got, err := env.svc.ReplaceToken(ctx, env.wsID, view.ID, "stub-live-token-8888")
	if err != nil {
		t.Fatalf("replace token: %v", err)
	}
	if got.Status != domain.ConnectionStatusConnected || got.StatusError != "" {
		t.Errorf("expected a clean connected view, got %q/%q", got.Status, got.StatusError)
	}
	if want := "8888"; got.TokenHint != want {
		t.Errorf("expected refreshed hint %q, got %q", want, got.TokenHint)
	}
}

// An empty token is invalid at the service: the keep-semantics live at the
// endpoint/dialog layer, which skips the call (design.md D3) — the service
// never guesses.
func TestReplaceToken_EmptyTokenInvalid(t *testing.T) {
	env := newConnectionsTestEnv(t)
	ctx := context.Background()

	mcpView, err := connectView(t, env, "github", "", testToken)
	if err != nil {
		t.Fatalf("mcp connect: %v", err)
	}
	upstream := newStubUpstream()
	defer upstream.srv.Close()
	recipe := registerHTTPTestRecipe(t, "stubhttp-replace-empty", upstream.srv.URL, "")
	httpView, err := connectHTTPStub(t, env, recipe.ID, httpTestToken)
	if err != nil {
		t.Fatalf("http connect: %v", err)
	}

	probesBefore := env.probeCalls
	for _, connID := range []string{mcpView.ID, httpView.ID} {
		for _, token := range []string{"", "   "} {
			if _, err := env.svc.ReplaceToken(ctx, env.wsID, connID, token); !errors.Is(err, domain.ErrInvalid) {
				t.Errorf("connection %s token %q: expected ErrInvalid, got %v", connID, token, err)
			}
		}
	}
	if env.probeCalls != probesBefore {
		t.Errorf("empty-token rejections must not probe, got %d extra calls", env.probeCalls-probesBefore)
	}
}

// Unknown and cross-workspace connections are domain.ErrNotFound.
func TestReplaceToken_UnknownConnection(t *testing.T) {
	env := newConnectionsTestEnv(t)

	if _, err := env.svc.ReplaceToken(context.Background(), env.wsID, "no-such-connection", "token"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}
