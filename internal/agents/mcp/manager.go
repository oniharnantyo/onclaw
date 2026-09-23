package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/cloudwego/eino/components/tool"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// Defaults for the manager's behavior knobs (design.md D5): connections idle
// out after ten minutes of disuse (stdio children and HTTP conns alike), a
// connect attempt is bounded at ten seconds (matching the probe bound), and a
// cached failed connection is retried after thirty seconds so a transient
// blip does not silence a server for a full idle TTL.
const (
	DefaultIdleTTL        = 10 * time.Minute
	DefaultConnectTimeout = 10 * time.Second
	DefaultErrorRetry     = 30 * time.Second
)

// Ref identifies one MCP server to connect to: workspace-scoped so cached
// entries are never shared across workspaces (tenant isolation holds even
// when a private server and a workspace server were configured identically).
type Ref struct {
	WorkspaceID string
	ServerID    string
	// AgentID is the owning agent for agent-private servers; empty for
	// workspace-registered rows. The OAuth dial credential seam addresses
	// agent-scope token rows by it (add-mcp-oauth-client design.md D7).
	AgentID string
	// Name is the server's display name (the naming pass's <server> segment).
	Name string
	Conn domain.MCPConnection
}

// key addresses one cached connection.
type key struct{ workspaceID, serverID string }

// ToolSource is the runner-facing seam of the connection cache: it yields the
// eino tools of one server, connecting on first use. *MCPManager implements
// it; runner tests inject mocks.
type ToolSource interface {
	// Tools returns the server's tool surface. A failed connection returns an
	// error; the caller decides how to degrade (design.md D8: skip and mark).
	Tools(ctx context.Context, ref Ref) ([]tool.BaseTool, error)
}

// cacheEntry is one cached connection: ready client + tools on success, or
// the failure for errored entries (negative cache with a short retry backoff).
type cacheEntry struct {
	conn      *connection // nil on error entries
	err       error       // set on error entries
	status    string      // domain.MCPStatusConnected / MCPStatusError
	statusErr string      // failure message for errored entries
	toolCount int
	lastUsed  time.Time
}

// connectCall is the per-key singleflight record: concurrent first Gets for
// the same server share one connection attempt.
type connectCall struct {
	ready chan struct{}
	entry *cacheEntry
}

// MCPManager caches MCP connections per (workspace, server), connecting
// lazily on first use and reaping idle entries on a TTL janitor (the
// BrowserManager precedent). Every entry's Close reaps stdio child processes
// and tears down HTTP connections alike. Mutating config writes invalidate
// the cached entry via Invalidate; Close tears everything down at shutdown.
type MCPManager struct {
	idleTTL        time.Duration
	connectTimeout time.Duration
	errorRetry     time.Duration

	mu       sync.Mutex
	entries  map[key]*cacheEntry
	inflight map[key]*connectCall
	stop     chan struct{} // closed by Close to stop the janitor
	stopped  sync.WaitGroup

	// connector dials ref's connection; production wires the mcp-go factory
	// (connect). A seam so unit tests can stub the transport layer.
	connector func(ctx context.Context, ref Ref) (*connection, error)

	// oauthCreds resolves oauth-mode rows' dial bearers (design.md D1);
	// wired by the composition root through WithOAuthCredentials.
	oauthCreds OAuthDialCredentials
}

// ManagerOption configures an MCPManager.
type ManagerOption func(*MCPManager)

// WithIdleTTL overrides the idle-reaper TTL. Zero or negative selects the
// default.
func WithIdleTTL(d time.Duration) ManagerOption {
	return func(m *MCPManager) {
		if d > 0 {
			m.idleTTL = d
		}
	}
}

// WithConnectTimeout overrides the bounded connect window (dial, handshake,
// and tool listing).
func WithConnectTimeout(d time.Duration) ManagerOption {
	return func(m *MCPManager) {
		if d > 0 {
			m.connectTimeout = d
		}
	}
}

// WithErrorRetry overrides how long a cached failed connection is reused
// before the next Get attempts a reconnect.
func WithErrorRetry(d time.Duration) ManagerOption {
	return func(m *MCPManager) {
		if d > 0 {
			m.errorRetry = d
		}
	}
}

// withConnector swaps the dialing function (in-package test seam only).
func withConnector(fn func(ctx context.Context, ref Ref) (*connection, error)) ManagerOption {
	return func(m *MCPManager) {
		if fn != nil {
			m.connector = fn
		}
	}
}

// WithOAuthCredentials wires the dial-time OAuth credential resolution
// (add-mcp-oauth-client design.md D1). The composition root always supplies
// it; a manager built without it fails loudly the first time an oauth-mode
// row dials, instead of silently dialing without a bearer.
func WithOAuthCredentials(creds OAuthDialCredentials) ManagerOption {
	return func(m *MCPManager) {
		m.oauthCreds = creds
	}
}

// NewMCPManager builds the manager and starts its idle janitor.
func NewMCPManager(opts ...ManagerOption) *MCPManager {
	m := &MCPManager{
		idleTTL:        DefaultIdleTTL,
		connectTimeout: DefaultConnectTimeout,
		errorRetry:     DefaultErrorRetry,
		entries:        make(map[key]*cacheEntry),
		inflight:       make(map[key]*connectCall),
		stop:           make(chan struct{}),
	}
	// The default connector reads m.oauthCreds lazily, so the
	// WithOAuthCredentials option below applies to dials regardless of the
	// option order.
	m.connector = func(ctx context.Context, ref Ref) (*connection, error) {
		return connectAuthorized(ctx, ref, m.oauthCreds)
	}
	for _, opt := range opts {
		opt(m)
	}
	m.stopped.Add(1)
	go m.janitor()
	return m
}

// janitor sweeps idle entries on a ticker pinned to half the idle TTL
// (clamped so tests with tiny TTLs sweep promptly and production never
// ticks faster than twice a minute for no benefit).
func (m *MCPManager) janitor() {
	defer m.stopped.Done()
	interval := m.idleTTL / 2
	if interval > 30*time.Second {
		interval = 30 * time.Second
	}
	if interval < 100*time.Millisecond {
		interval = 100 * time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-m.stop:
			return
		case <-ticker.C:
			m.sweepIdle()
		}
	}
}

// sweepIdle closes and drops entries whose last access exceeds the idle TTL.
func (m *MCPManager) sweepIdle() {
	m.mu.Lock()
	if m.idleTTL <= 0 {
		m.mu.Unlock()
		return
	}
	now := time.Now()
	var expired []key
	for k, e := range m.entries {
		if now.Sub(e.lastUsed) > m.idleTTL {
			expired = append(expired, k)
		}
	}
	closing := make(map[key]*cacheEntry, len(expired))
	for _, k := range expired {
		closing[k] = m.entries[k]
		delete(m.entries, k)
	}
	m.mu.Unlock()

	for k, e := range closing {
		closeClient(k, e)
	}
}

// Tools implements ToolSource: cache hit returns the cached tools and touches
// the entry's idle clock; miss connects (bounded by the connect timeout),
// handshakes, and lists tools once per key — concurrent first Gets share one
// attempt through the per-key singleflight record.
func (m *MCPManager) Tools(ctx context.Context, ref Ref) ([]tool.BaseTool, error) {
	k := key{ref.WorkspaceID, ref.ServerID}

	m.mu.Lock()
	if e, ok := m.entries[k]; ok {
		// Errored entries are negatively cached for errorRetry only: a
		// transient failure must not silence the server for a full idle TTL.
		if e.err == nil || time.Since(e.lastUsed) < m.errorRetry {
			e.lastUsed = time.Now()
			m.mu.Unlock()
			if e.err != nil {
				return nil, e.err
			}
			return e.conn.tools, nil
		}
		delete(m.entries, k)
	}
	if call, ok := m.inflight[k]; ok {
		m.mu.Unlock()
		<-call.ready
		if call.entry.err != nil {
			return nil, call.entry.err
		}
		return call.entry.conn.tools, nil
	}
	call := &connectCall{ready: make(chan struct{})}
	m.inflight[k] = call
	m.mu.Unlock()

	entry := m.connect(ctx, ref)

	m.mu.Lock()
	delete(m.inflight, k)
	m.entries[k] = entry
	m.mu.Unlock()
	call.entry = entry
	close(call.ready)

	if entry.err != nil {
		return nil, entry.err
	}
	return entry.conn.tools, nil
}

// connect dials ref within the connect timeout and returns the entry to
// cache — success or failure alike (design.md D8: the failure is cached as
// an errored entry, never propagated as a run failure).
func (m *MCPManager) connect(ctx context.Context, ref Ref) *cacheEntry {
	cctx, cancel := context.WithTimeout(ctx, m.connectTimeout)
	defer cancel()
	conn, err := m.connector(cctx, ref)
	if err != nil {
		return &cacheEntry{
			err:       err,
			status:    domain.MCPStatusError,
			statusErr: err.Error(),
			lastUsed:  time.Now(),
		}
	}
	return &cacheEntry{
		conn:      conn,
		status:    domain.MCPStatusConnected,
		toolCount: len(conn.tools),
		lastUsed:  time.Now(),
	}
}

// Status reports the outcome of the entry's most recent connection attempt:
// status (domain.MCPStatusConnected/Error), the failure message, and the
// tool count. Dropped (invalidated or reaped) entries return ok=false.
// Reads never disturb the entry's idle clock.
func (m *MCPManager) Status(workspaceID, serverID string) (status, statusErr string, toolCount int, ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[key{workspaceID, serverID}]
	if !ok {
		return "", "", 0, false
	}
	return e.status, e.statusErr, e.toolCount, true
}

// Invalidate drops the cached entry for (workspaceID, serverID) and closes
// its connection, reaping any stdio child. Called on every mutating config
// write (update, delete, master-disable) so config changes take effect
// immediately (design.md D5). Unknown keys are no-ops.
func (m *MCPManager) Invalidate(workspaceID, serverID string) {
	k := key{workspaceID, serverID}
	m.mu.Lock()
	e, ok := m.entries[k]
	if ok {
		delete(m.entries, k)
	}
	m.mu.Unlock()
	if ok {
		closeClient(k, e)
	}
}

// Close stops the janitor, closes every cached connection in parallel, and
// empties the cache. Used by the composition root's shutdown path.
func (m *MCPManager) Close() {
	m.mu.Lock()
	select {
	case <-m.stop:
		// Already closed.
	default:
		close(m.stop)
	}
	entries := m.entries
	m.entries = make(map[key]*cacheEntry)
	m.inflight = make(map[key]*connectCall)
	m.mu.Unlock()

	var wg sync.WaitGroup
	for k, e := range entries {
		wg.Add(1)
		go func(k key, e *cacheEntry) {
			defer wg.Done()
			closeClient(k, e)
		}(k, e)
	}
	wg.Wait()
	m.stopped.Wait()
}

// closeClient tears down one entry's client. mcp-go's stdio Close closes
// stdin and waits for the child to exit, so a wedged server could block
// forever; the close runs under a watchdog that logs and moves on — the
// orphan dies with its own idle timer (design.md risk note).
func closeClient(k key, e *cacheEntry) {
	if e == nil || e.conn == nil || e.conn.client == nil {
		return
	}
	done := make(chan struct{})
	go func() {
		if err := e.conn.client.Close(); err != nil {
			slog.Debug("mcp client close failed",
				"workspace_id", k.workspaceID, "server_id", k.serverID, "error", err)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		slog.Warn("mcp client close timed out; abandoning",
			"workspace_id", k.workspaceID, "server_id", k.serverID)
	}
}

// String renders a key for logs.
func (k key) String() string {
	return fmt.Sprintf("%s/%s", k.workspaceID, k.serverID)
}
