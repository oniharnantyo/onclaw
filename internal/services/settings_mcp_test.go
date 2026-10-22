package services

import (
	"context"
	"errors"
	"fmt"
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
