package services

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/agents/mcp"
	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/oniharnantyo/buddies/internal/store/fake"
)

// The MCP settings tests (tasks.md 2.2, spec settings "MCP pane" +
// agent-mcp "Probe and tool discovery"): the probe writes only the status
// fields — connected + tool count on success, error + reason on failure —
// and never mutates the definition, using the real probe against the mcp
// package's compiled stdio stub server (onclaw's probe-test approach).

// The stub server's configured env row and arg (must match
// internal/agents/mcp/testdata/mockmcpserver, whose own tests pin them).
const (
	envMarkerMCP = "BUDDIES_MCP_TEST_MARKER"
	argsFlagMCP  = "--marker=child-args-123"
)

var (
	compileStubOnce sync.Once
	stubServerBin   string
	compileStubErr  error
)

// compileStubServer builds the mcp package's in-process stdio MCP stub once
// for the whole test binary; the probe drives it as a real child server.
func compileStubServer(t *testing.T) string {
	t.Helper()
	compileStubOnce.Do(func() {
		_, thisFile, _, ok := runtime.Caller(0)
		if !ok {
			compileStubErr = errors.New("cannot locate test source dir")
			return
		}
		// The stub lives next to the mcp package it serves: ../agents/mcp.
		pkgDir := filepath.Dir(thisFile)
		serverDir := filepath.Join(pkgDir, "..", "agents", "mcp", "testdata", "mockmcpserver")
		goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
		if _, err := os.Stat(goBin); err != nil {
			goBin = "go"
		}
		dir, err := os.MkdirTemp("", "buddies-mcp-probe")
		if err != nil {
			compileStubErr = err
			return
		}
		bin := filepath.Join(dir, "mockmcpserver")
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, goBin, "build", "-o", bin, serverDir)
		if out, err := cmd.CombinedOutput(); err != nil {
			compileStubErr = fmt.Errorf("compile mockmcpserver: %v\n%s", err, out)
		}
		stubServerBin = bin
	})
	if compileStubErr != nil {
		t.Fatalf("stub server unavailable: %v", compileStubErr)
	}
	return stubServerBin
}

// stdioStubServer builds a stdio server row for the stub binary, so the
// probe's env/args plumbing is exercised end to end.
func stdioStubServer(name, command string) store.MCPServer {
	return store.MCPServer{
		Name:      name,
		Transport: mcp.TransportStdio,
		Command:   command,
		Args:      []string{argsFlagMCP},
		Env:       map[string]string{envMarkerMCP: "secret-123"},
		Enabled:   true,
	}
}

// TestProbeMCPServerConnected proves the healthy path (spec scenario
// "Healthy server shows state"): probing the real stub server persists
// connected with the discovered-tool count, and the stored definition is
// untouched.
func TestProbeMCPServerConnected(t *testing.T) {
	ctx := context.Background()
	fs := fake.New()
	svc := NewSettingsService(fs)
	bin := compileStubServer(t)

	created, err := svc.CreateMCPServer(ctx, stdioStubServer("github", bin))
	require.NoError(t, err)
	// A stale prior failure is replaced by the fresh outcome.
	created.Status = mcp.StatusError
	created.StatusError = "stale probe failure"
	require.NoError(t, fs.MCPServers().Update(ctx, &created))

	probed, err := svc.ProbeMCPServer(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, mcp.StatusConnected, probed.Status)
	require.Empty(t, probed.StatusError)
	require.Positive(t, probed.ToolCount)

	// The persisted row carries the outcome; the definition columns survived
	// the probe byte-for-byte.
	stored, err := fs.MCPServers().ByID(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, mcp.StatusConnected, stored.Status)
	require.Equal(t, bin, stored.Command)
	require.Equal(t, []string{argsFlagMCP}, stored.Args)
	require.Equal(t, map[string]string{envMarkerMCP: "secret-123"}, stored.Env)
}

// TestProbeMCPServerFailureLeavesDefinition pins the failure contract (spec
// scenario "Unreachable server shows the reason"): a failed probe persists
// the error status with the reason and a zero count, and every definition
// column is unchanged.
func TestProbeMCPServerFailureLeavesDefinition(t *testing.T) {
	ctx := context.Background()
	fs := fake.New()
	svc := NewSettingsService(fs)

	created, err := svc.CreateMCPServer(ctx, stdioStubServer("broken", "/nonexistent/buddies-mcp-binary-xyz"))
	require.NoError(t, err)
	before, err := fs.MCPServers().ByID(ctx, created.ID)
	require.NoError(t, err)

	probed, err := svc.ProbeMCPServer(ctx, created.ID)
	require.NoError(t, err, "a failed probe reports on the row, it does not fail the call")
	require.Equal(t, mcp.StatusError, probed.Status)
	require.NotEmpty(t, probed.StatusError)
	require.Equal(t, 0, probed.ToolCount)

	// Definition unchanged: every column except the status fields.
	after, err := fs.MCPServers().ByID(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, before.Name, after.Name)
	require.Equal(t, before.Transport, after.Transport)
	require.Equal(t, before.Command, after.Command)
	require.Equal(t, before.Args, after.Args)
	require.Equal(t, before.Env, after.Env)
	require.Equal(t, before.URL, after.URL)
	require.Equal(t, before.Headers, after.Headers)
	require.Equal(t, before.Enabled, after.Enabled)
}

// TestProbeMCPServerUnknownID keeps the pane honest about absent rows.
func TestProbeMCPServerUnknownID(t *testing.T) {
	svc := NewSettingsService(fake.New())
	_, err := svc.ProbeMCPServer(context.Background(), "srv-missing")
	require.ErrorIs(t, err, store.ErrNotFound)
}

// recordingMCPCache records the server ids the service invalidated, in call
// order (hand-written fake, per the services tests' convention).
type recordingMCPCache struct {
	mu  sync.Mutex
	ids []string
}

func (c *recordingMCPCache) invalidate(serverID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ids = append(c.ids, serverID)
}

func (c *recordingMCPCache) invalidated() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.ids...)
}

// TestMCPWritesInvalidateCachedConnection pins the invalidate-on-write seam
// (design D7, spec agent-mcp "Opt-in policy"): a successful update — the
// enable-toggle included — and a successful delete each drop the cached
// connection for that server id, create (no connection can be cached yet)
// and failed writes do not, and CRUD keeps working when no cache was
// injected at all.
func TestMCPWritesInvalidateCachedConnection(t *testing.T) {
	ctx := context.Background()
	fs := fake.New()
	cache := &recordingMCPCache{}
	svc := NewSettingsService(fs, WithMCPInvalidate(cache.invalidate))

	created, err := svc.CreateMCPServer(ctx, stdioStubServer("fs", "/bin/fs"))
	require.NoError(t, err)
	require.Empty(t, cache.invalidated(), "create cannot invalidate: no connection is cached for a new row")

	// The enable-toggle path is an update: Enabled=false rewrites the row.
	created.Enabled = false
	_, err = svc.UpdateMCPServer(ctx, created)
	require.NoError(t, err)
	require.Equal(t, []string{created.ID}, cache.invalidated())

	_, err = svc.UpdateMCPServer(ctx, store.MCPServer{ID: "srv-missing", Name: "x"})
	require.ErrorIs(t, err, store.ErrNotFound)
	require.Equal(t, []string{created.ID}, cache.invalidated(), "a failed write must not invalidate")

	require.NoError(t, svc.DeleteMCPServer(ctx, created.ID))
	require.Equal(t, []string{created.ID, created.ID}, cache.invalidated())

	// Without an injected cache the no-op default keeps CRUD green.
	plain := NewSettingsService(fs)
	_, err = plain.CreateMCPServer(ctx, stdioStubServer("web", ""))
	require.NoError(t, err)
}

// TestProbeMCPServerInvalidatesBeforeDialing pins design D6 (refresh =
// invalidate, then probe): the row's cached connection is dropped exactly
// once, before the dial — this probe then fails, and the invalidation must
// have happened anyway.
func TestProbeMCPServerInvalidatesBeforeDialing(t *testing.T) {
	ctx := context.Background()
	fs := fake.New()
	fx := newOAuthFlowFixture(t)
	order := &eventLog{}
	fx.order = order

	cache := &recordingMCPCache{}
	svc := NewSettingsService(fs, WithMCPInvalidate(func(serverID string) {
		cache.invalidate(serverID)
		order.append("invalidate")
	}))

	created, err := svc.CreateMCPServer(ctx, oauthHTTPServerRow("atlassian", fx.URL+"/mcp"))
	require.NoError(t, err)

	// A classified authorization-required probe still reports on the row; it
	// does not fail the call.
	_, err = svc.ProbeMCPServer(ctx, created.ID)
	require.NoError(t, err)

	require.Equal(t, []string{created.ID}, cache.invalidated())
	events := order.snapshot()
	require.NotEmpty(t, events)
	require.Equal(t, "invalidate", events[0], "the cache must be dropped before the first dial")
	invalidates := 0
	for _, event := range events {
		if event == "invalidate" {
			invalidates++
		}
	}
	require.Equal(t, 1, invalidates, "one probe, one invalidation")
}

// TestProbeMCPServerAuthRequiredNoneMode pins design D5's none-mode
// classification: the server's unauthenticated 401 names a well-formed
// protected-resource advertisement, so the row persists auth_required with
// the enable-the-flow message — and the definition, auth settings included,
// is untouched.
func TestProbeMCPServerAuthRequiredNoneMode(t *testing.T) {
	ctx := context.Background()
	fs := fake.New()
	svc := NewSettingsService(fs)
	fx := newOAuthFlowFixture(t)

	created, err := svc.CreateMCPServer(ctx, oauthHTTPServerRow("atlassian", fx.URL+"/mcp"))
	require.NoError(t, err)
	before, err := fs.MCPServers().ByID(ctx, created.ID)
	require.NoError(t, err)

	probed, err := svc.ProbeMCPServer(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, mcp.StatusAuthRequired, probed.Status)
	require.Equal(t, MCPOAuthRequiredToEnableMessage, probed.StatusError)
	require.Equal(t, 0, probed.ToolCount)

	after, err := fs.MCPServers().ByID(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, before.Auth, after.Auth)
}

// TestProbeMCPServerAuthRequiredOAuthMode pins design D5's OAuth-mode
// classification: an OAuth-mode row whose token store is empty comes back
// from the transport itself as authorization-required, with the sign-in
// message — not a generic failure.
func TestProbeMCPServerAuthRequiredOAuthMode(t *testing.T) {
	ctx := context.Background()
	fs := fake.New()
	svc := NewSettingsService(fs)
	fx := newOAuthFlowFixture(t)

	created, err := svc.CreateMCPServer(ctx, oauthHTTPServerRow("atlassian", fx.URL+"/mcp"))
	require.NoError(t, err)
	created.Auth = store.MCPServerAuth{
		Mode:                  store.MCPAuthModeOAuth,
		ClientID:              "client-1",
		Issuer:                fx.URL,
		AuthServerMetadataURL: fx.URL + "/.well-known/oauth-authorization-server",
	}
	require.NoError(t, fs.MCPServers().Update(ctx, &created))
	before, err := fs.MCPServers().ByID(ctx, created.ID)
	require.NoError(t, err)

	probed, err := svc.ProbeMCPServer(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, mcp.StatusAuthRequired, probed.Status)
	require.Equal(t, MCPOAuthRequiredMessage, probed.StatusError)
	require.Equal(t, 0, probed.ToolCount)

	after, err := fs.MCPServers().ByID(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, before.Auth, after.Auth)
}

// ---------------------------------------------------------------------------
// Integration connect, redaction, and disconnect (add-integrations tasks 3,
// design D1/D3/D4, spec "Connect flow", "Integration credential secrecy",
// "Family entries").
// ---------------------------------------------------------------------------

// newCredentialGuardedStub builds a streamable MCP endpoint guarded by a
// plain bearer check: a wrong or missing credential gets a bare 401, the
// right one reaches a real one-tool MCP server.
func newCredentialGuardedStub(credential string) http.Handler {
	inner := newFlowStubHandler()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+credential {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		inner.ServeHTTP(w, r)
	})
}

// TestConnectIntegrationTokenCreatesTaggedServer is the token-method
// acceptance path (spec scenarios "Connect creates a tagged server
// definition" and "Credential never round-trips"): the credential rides the
// row's auth JSON — never Headers — the connect probes it connected, and no
// service read or listing returns the credential, only key-set state and the
// last-four hint.
func TestConnectIntegrationTokenCreatesTaggedServer(t *testing.T) {
	ctx := context.Background()
	fs := fake.New()
	svc := NewSettingsService(fs)

	credential := "glpat-secret-9999"
	guard := httptest.NewServer(newCredentialGuardedStub(credential))
	defer guard.Close()

	connected, err := svc.ConnectIntegration(ctx, IntegrationConnectInput{
		Service:    "gitlab",
		Endpoint:   guard.URL + "/mcp",
		AuthMethod: IntegrationAuthMethodToken,
		Credential: credential,
	})
	require.NoError(t, err)
	require.Equal(t, "gitlab", connected.Service, "the row is tagged with the service id")
	require.Equal(t, "gitlab", connected.Name)
	require.Equal(t, mcp.StatusConnected, connected.Status, "the connect probe flips the tile")
	require.Positive(t, connected.ToolCount)
	// The returned row is redacted: hint and key-set state, no secret.
	require.Empty(t, connected.Auth.IntegrationCredential)
	require.True(t, connected.CredentialSet)
	require.Equal(t, "9999", connected.CredentialHint)

	// The credential lives in the stored auth JSON, never in Headers.
	stored, err := fs.MCPServers().ByID(ctx, connected.ID)
	require.NoError(t, err)
	require.Equal(t, credential, stored.Auth.IntegrationCredential)
	require.Empty(t, stored.Headers)

	// No service read or listing returns it.
	read, err := svc.MCPServer(ctx, connected.ID)
	require.NoError(t, err)
	require.Empty(t, read.Auth.IntegrationCredential)
	require.True(t, read.CredentialSet)
	require.Equal(t, "9999", read.CredentialHint)
	listed, err := svc.MCPServers(ctx)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.Empty(t, listed[0].Auth.IntegrationCredential)
}

// TestConnectIntegrationOAuthConnectMarksAuthRequired pins the OAuth branch
// of the connect probe (spec scenario "Authorization required after
// connect"): a freshly connected oauth_dcr row has no tokens yet, so its
// probe lands auth_required with the Authorize call-to-action — the call
// itself still succeeds.
func TestConnectIntegrationOAuthConnectMarksAuthRequired(t *testing.T) {
	ctx := context.Background()
	svc := NewSettingsService(fake.New())
	fx := newOAuthFlowFixture(t)

	connected, err := svc.ConnectIntegration(ctx, IntegrationConnectInput{
		Service:    "linear",
		Endpoint:   fx.URL + "/mcp",
		AuthMethod: IntegrationAuthMethodDCR,
	})
	require.NoError(t, err, "an authorization-required outcome reports on the row, it does not fail the connect")
	require.Equal(t, mcp.StatusAuthRequired, connected.Status)
	require.Equal(t, MCPOAuthRequiredMessage, connected.StatusError)
	require.Equal(t, store.MCPAuthModeOAuth, connected.Auth.Mode)
	require.Equal(t, "linear", connected.Service)
}

// TestConnectIntegrationFailedProbeShowsReason pins the failure branch (spec
// "any other failure shows the reason"): a rejected credential is a plain
// error state carrying the reason, not an authorization-required one.
func TestConnectIntegrationFailedProbeShowsReason(t *testing.T) {
	ctx := context.Background()
	svc := NewSettingsService(fake.New())
	guard := httptest.NewServer(newCredentialGuardedStub("right-credential"))
	defer guard.Close()

	connected, err := svc.ConnectIntegration(ctx, IntegrationConnectInput{
		Service:    "gitlab",
		Endpoint:   guard.URL + "/mcp",
		AuthMethod: IntegrationAuthMethodToken,
		Credential: "wrong-credential",
	})
	require.NoError(t, err)
	require.Equal(t, mcp.StatusError, connected.Status)
	require.NotEmpty(t, connected.StatusError)
	require.Equal(t, 0, connected.ToolCount)
}

// TestConnectIntegrationValidatesInput pins the sheet-payload gates: service
// and endpoint are required, the auth method must be a known kind, and each
// kind requires its own inputs.
func TestConnectIntegrationValidatesInput(t *testing.T) {
	ctx := context.Background()
	svc := NewSettingsService(fake.New())

	_, err := svc.ConnectIntegration(ctx, IntegrationConnectInput{
		Endpoint: "https://mcp.example/mcp", AuthMethod: IntegrationAuthMethodToken, Credential: "t",
	})
	require.ErrorContains(t, err, "service is required")

	_, err = svc.ConnectIntegration(ctx, IntegrationConnectInput{
		Service: "gitlab", AuthMethod: IntegrationAuthMethodToken, Credential: "t",
	})
	require.ErrorContains(t, err, "endpoint is required")

	_, err = svc.ConnectIntegration(ctx, IntegrationConnectInput{
		Service: "gitlab", Endpoint: "https://mcp.example/mcp", AuthMethod: "hmac",
	})
	require.ErrorContains(t, err, `unknown auth method "hmac"`)

	_, err = svc.ConnectIntegration(ctx, IntegrationConnectInput{
		Service: "gitlab", Endpoint: "https://mcp.example/mcp", AuthMethod: IntegrationAuthMethodToken,
	})
	require.ErrorContains(t, err, "token method requires a credential")

	_, err = svc.ConnectIntegration(ctx, IntegrationConnectInput{
		Service: "slack", Endpoint: "https://mcp.example/mcp",
		AuthMethod: IntegrationAuthMethodConfidential, ClientID: "client-1",
	})
	require.ErrorContains(t, err, "requires a client id and secret")
}

// TestMCPServerReadsRedactIntegrationCredential pins the redaction contract
// across the whole service surface (design D4): create, read, listing, and
// probe returns carry only CredentialSet plus the last-four hint; the stored
// row keeps the credential. A blank-credential update — what a redacted
// read round-trips — preserves the stored one; a non-blank value replaces it
// (spec scenario "Edit keeps the stored credential").
func TestMCPServerReadsRedactIntegrationCredential(t *testing.T) {
	ctx := context.Background()
	fs := fake.New()
	svc := NewSettingsService(fs)
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer dead.Close()

	created, err := svc.CreateMCPServer(ctx, store.MCPServer{
		Name:      "gitlab",
		Transport: mcp.TransportStreamableHTTP,
		URL:       dead.URL + "/mcp",
		Auth:      store.MCPServerAuth{IntegrationCredential: "glpat-secret-9999"},
		Enabled:   true,
	})
	require.NoError(t, err)
	require.Empty(t, created.Auth.IntegrationCredential, "the create return is redacted")
	require.True(t, created.CredentialSet)
	require.Equal(t, "9999", created.CredentialHint)

	read, err := svc.MCPServer(ctx, created.ID)
	require.NoError(t, err)
	require.Empty(t, read.Auth.IntegrationCredential)
	listed, err := svc.MCPServers(ctx)
	require.NoError(t, err)
	require.Empty(t, listed[0].Auth.IntegrationCredential)
	probed, err := svc.ProbeMCPServer(ctx, created.ID)
	require.NoError(t, err)
	require.Empty(t, probed.Auth.IntegrationCredential)
	require.True(t, probed.CredentialSet, "the probe's redaction keeps the key-set state")

	// The store layer still holds the secret: only the service reads are
	// redacted.
	stored, err := fs.MCPServers().ByID(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, "glpat-secret-9999", stored.Auth.IntegrationCredential)

	// A blank credential — everything a redacted read can resubmit — keeps
	// the stored one.
	read.Name = "gitlab-renamed"
	updated, err := svc.UpdateMCPServer(ctx, read)
	require.NoError(t, err)
	require.Empty(t, updated.Auth.IntegrationCredential)
	stored, err = fs.MCPServers().ByID(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, "glpat-secret-9999", stored.Auth.IntegrationCredential, "the blank update preserved the credential")
	require.Equal(t, "gitlab-renamed", stored.Name)

	// A non-blank credential replaces it.
	updated.Auth.IntegrationCredential = "new-token-1234"
	_, err = svc.UpdateMCPServer(ctx, updated)
	require.NoError(t, err)
	stored, err = fs.MCPServers().ByID(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, "new-token-1234", stored.Auth.IntegrationCredential)
}

// TestDisconnectIntegrationDeletesRow pins design D1's remove path (spec
// "removing an integration deletes its server definition"): the tagged row
// goes; an absent row is store.ErrNotFound.
func TestDisconnectIntegrationDeletesRow(t *testing.T) {
	ctx := context.Background()
	fs := fake.New()
	svc := NewSettingsService(fs)
	guard := httptest.NewServer(newCredentialGuardedStub("glpat-1234"))
	defer guard.Close()

	created, err := svc.ConnectIntegration(ctx, IntegrationConnectInput{
		Service:    "gitlab",
		Endpoint:   guard.URL + "/mcp",
		AuthMethod: IntegrationAuthMethodToken,
		Credential: "glpat-1234",
	})
	require.NoError(t, err)

	require.NoError(t, svc.DisconnectIntegration(ctx, created.ID))
	_, err = fs.MCPServers().ByID(ctx, created.ID)
	require.ErrorIs(t, err, store.ErrNotFound)

	require.ErrorIs(t, svc.DisconnectIntegration(ctx, "srv-missing"), store.ErrNotFound)
}

// TestConnectIntegrationFamilyCreatesProductRows is the family acceptance
// path (design D6, spec scenarios "Family connect creates product
// definitions"): one sheet submission creates one tagged row per product,
// each probed into its own state, with the shared confidential client and
// the extra authorization params stored on every row — ExtraAuthParams
// round-trips through the store.
func TestConnectIntegrationFamilyCreatesProductRows(t *testing.T) {
	ctx := context.Background()
	fs := fake.New()
	svc := NewSettingsService(fs)
	fx := newOAuthFlowFixture(t)

	extras := map[string]string{"access_type": "offline", "prompt": "consent"}
	first, err := svc.ConnectIntegration(ctx, IntegrationConnectInput{
		Service:          "google-workspace",
		Endpoint:         fx.URL + "/mcp",
		AuthMethod:       IntegrationAuthMethodConfidential,
		ClientID:         "google-client",
		ClientSecret:     "google-secret",
		ExtraAuthParams:  extras,
		Products:         []string{"gmail", "calendar"},
		ProductEndpoints: map[string]string{"gmail": fx.URL + "/gmail", "calendar": fx.URL + "/calendar"},
	})
	require.NoError(t, err)
	require.Equal(t, "gmail", first.Service, "the first product's row is returned")

	listed, err := svc.MCPServers(ctx)
	require.NoError(t, err)
	require.Len(t, listed, 2)
	byService := make(map[string]store.MCPServer, len(listed))
	for _, row := range listed {
		byService[row.Service] = row
	}
	for _, service := range []string{"gmail", "calendar"} {
		row, ok := byService[service]
		require.True(t, ok, "a row tagged %s must exist", service)
		require.Equal(t, service, row.Name)
		require.Equal(t, fx.URL+"/"+service, row.URL, "each product dials its own endpoint")
		require.Equal(t, store.MCPAuthModeOAuth, row.Auth.Mode)
		require.Equal(t, "google-client", row.Auth.ClientID, "the family's shared client is on every product row")
		require.Equal(t, "google-secret", row.Auth.ClientSecret)
		require.Equal(t, extras, row.Auth.ExtraAuthParams, "ExtraAuthParams round-trips through the store")
		require.Equal(t, mcp.StatusAuthRequired, row.Status, "each product is probed into its own state")
	}
}

// TestConnectIntegrationEditKeepsStoredCredential pins the edit path (spec
// scenarios "Edit keeps the stored credential"): resubmitting a service that
// already has its row updates it in place — same row id, no duplicate — and
// a blank credential adopts the stored one while a non-blank value replaces
// it. The probe outcome refreshes on the returned row.
func TestConnectIntegrationEditKeepsStoredCredential(t *testing.T) {
	ctx := context.Background()
	fs := fake.New()
	svc := NewSettingsService(fs)

	credential := "glpat-secret-9999"
	guard := httptest.NewServer(newCredentialGuardedStub(credential))
	defer guard.Close()

	created, err := svc.ConnectIntegration(ctx, IntegrationConnectInput{
		Service:    "gitlab",
		Endpoint:   guard.URL + "/mcp",
		AuthMethod: IntegrationAuthMethodToken,
		Credential: credential,
	})
	require.NoError(t, err)

	// The edit sheet round-trips no secret (design D4): a blank credential
	// keeps the stored one.
	edited, err := svc.ConnectIntegration(ctx, IntegrationConnectInput{
		Service:    "gitlab",
		Endpoint:   guard.URL + "/mcp-gitlab",
		AuthMethod: IntegrationAuthMethodToken,
		Credential: "",
	})
	require.NoError(t, err)
	require.Equal(t, created.ID, edited.ID, "the edit reuses the row in place")
	listed, err := svc.MCPServers(ctx)
	require.NoError(t, err)
	require.Len(t, listed, 1, "no duplicate row")
	stored, err := fs.MCPServers().ByID(ctx, edited.ID)
	require.NoError(t, err)
	require.Equal(t, credential, stored.Auth.IntegrationCredential, "blank keeps the stored credential")
	require.Equal(t, guard.URL+"/mcp-gitlab", stored.URL, "the endpoint is rewritten")
	require.True(t, edited.CredentialSet)
	require.Equal(t, "9999", edited.CredentialHint)

	// A non-blank credential replaces the stored one.
	replaced, err := svc.ConnectIntegration(ctx, IntegrationConnectInput{
		Service:    "gitlab",
		Endpoint:   guard.URL + "/mcp",
		AuthMethod: IntegrationAuthMethodToken,
		Credential: credential + "-2",
	})
	require.NoError(t, err)
	require.Equal(t, created.ID, replaced.ID)
	stored, err = fs.MCPServers().ByID(ctx, replaced.ID)
	require.NoError(t, err)
	require.Equal(t, credential+"-2", stored.Auth.IntegrationCredential)
}

// TestConnectIntegrationEditKeepsStoredClient pins the confidential-client
// edit path: a resubmission whose secret field is blank (the redacted read
// round-trips nothing) keeps the stored client and still lands exactly one
// row.
func TestConnectIntegrationEditKeepsStoredClient(t *testing.T) {
	ctx := context.Background()
	fs := fake.New()
	svc := NewSettingsService(fs)
	fx := newOAuthFlowFixture(t)

	created, err := svc.ConnectIntegration(ctx, IntegrationConnectInput{
		Service:      "slack",
		Endpoint:     fx.URL + "/mcp",
		AuthMethod:   IntegrationAuthMethodConfidential,
		ClientID:     "slack-client",
		ClientSecret: "slack-secret",
	})
	require.NoError(t, err)

	edited, err := svc.ConnectIntegration(ctx, IntegrationConnectInput{
		Service:      "slack",
		Endpoint:     fx.URL + "/mcp-slack",
		AuthMethod:   IntegrationAuthMethodConfidential,
		ClientID:     "slack-client",
		ClientSecret: "",
	})
	require.NoError(t, err)
	require.Equal(t, created.ID, edited.ID)
	listed, err := svc.MCPServers(ctx)
	require.NoError(t, err)
	require.Len(t, listed, 1, "no duplicate row")
	stored, err := fs.MCPServers().ByID(ctx, edited.ID)
	require.NoError(t, err)
	require.Equal(t, "slack-secret", stored.Auth.ClientSecret, "blank keeps the stored secret")
	require.Equal(t, fx.URL+"/mcp-slack", stored.URL)
}

// TestConnectIntegrationFamilySkipsConnectedProducts pins the family
// resubmission (spec scenario "Adding a product later reuses the setup"):
// products that already have their rows are skipped — no duplicate — and the
// missing product is created and probed.
func TestConnectIntegrationFamilySkipsConnectedProducts(t *testing.T) {
	ctx := context.Background()
	fs := fake.New()
	svc := NewSettingsService(fs)
	fx := newOAuthFlowFixture(t)

	_, err := svc.ConnectIntegration(ctx, IntegrationConnectInput{
		Service:          "google-workspace",
		Endpoint:         fx.URL + "/gmail",
		AuthMethod:       IntegrationAuthMethodConfidential,
		ClientID:         "google-client",
		ClientSecret:     "google-secret",
		Products:         []string{"gmail", "calendar"},
		ProductEndpoints: map[string]string{"gmail": fx.URL + "/gmail", "calendar": fx.URL + "/calendar"},
	})
	require.NoError(t, err)

	// Resubmit with calendar already connected: only drive is created, and
	// the shared setup is adopted from the stored rows (blank secret).
	created, err := svc.ConnectIntegration(ctx, IntegrationConnectInput{
		Service:          "google-workspace",
		Endpoint:         fx.URL + "/drive",
		AuthMethod:       IntegrationAuthMethodConfidential,
		ClientID:         "google-client",
		ClientSecret:     "",
		Products:         []string{"calendar", "drive"},
		ProductEndpoints: map[string]string{"drive": fx.URL + "/drive"},
	})
	require.NoError(t, err)
	require.Equal(t, "drive", created.Service, "the newly created product's row is returned")

	listed, err := svc.MCPServers(ctx)
	require.NoError(t, err)
	require.Len(t, listed, 3, "gmail, calendar, drive — no duplicates")
	drive, err := fs.MCPServers().ByID(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, "google-secret", drive.Auth.ClientSecret, "the family's stored client is adopted")
	require.Equal(t, fx.URL+"/drive", drive.URL)
}

// TestConnectIntegrationBlankCredentialFreshConnectErrors pins the gate: the
// blank-keeps-stored rule only has something to keep on an existing row — a
// fresh connect without a credential is still rejected.
func TestConnectIntegrationBlankCredentialFreshConnectErrors(t *testing.T) {
	ctx := context.Background()
	svc := NewSettingsService(fake.New())

	_, err := svc.ConnectIntegration(ctx, IntegrationConnectInput{
		Service:    "gitlab",
		Endpoint:   "https://gitlab.com/api/v4/mcp",
		AuthMethod: IntegrationAuthMethodToken,
	})
	require.ErrorContains(t, err, "token method requires a credential")
}
