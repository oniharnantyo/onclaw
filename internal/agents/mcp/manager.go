package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/cloudwego/eino/components/tool"
)

// Defaults for the manager's behavior knobs (design D7): connections idle
// out after ten minutes of disuse (stdio children and HTTP conns alike), a
// connect attempt is bounded at ten seconds (matching the probe bound), and a
// cached failed connection is retried after thirty seconds so a transient
// blip does not silence a server for a full idle TTL.
const (
	DefaultIdleTTL        = 10 * time.Minute
	DefaultConnectTimeout = 10 * time.Second
	DefaultErrorRetry     = 30 * time.Second
)

// Ref identifies one MCP server to connect to.
type Ref struct {
	ServerID string
	// Name is the server's display name (the naming pass's <server> segment).
	Name string
	// Conn is the server's dial settings; carried per call so cached entries
	// are validated against the current definition via Invalidate-on-write.
	Conn Connection
}

// key addresses one cached connection (single-tenant: the server id is the
// whole key).
type key struct{ serverID string }

// ToolSource is the runner-facing seam of the connection cache: it yields the
// eino tools of one server, connecting on first use. *MCPManager implements
// it; runner tests inject fakes.
type ToolSource interface {
	// Tools returns the server's tool surface. A failed connection returns an
	// error; the caller decides how to degrade (design D7: skip and mark).
	Tools(ctx context.Context, ref Ref) ([]tool.BaseTool, error)
}

// cacheEntry is one cached connection: ready client + tools on success, or
// the failure for errored entries (negative cache with a short retry backoff).
type cacheEntry struct {
	conn      *connection // nil on error entries
	err       error       // set on error entries
	status    string      // StatusConnected / StatusError
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

// MCPManager caches MCP connections per server, connecting lazily on first
// use and reaping idle entries on a TTL janitor. Every entry's Close reaps
// stdio child processes and tears down HTTP connections alike. Mutating
// config writes invalidate the cached entry via Invalidate; Close tears
// everything down at shutdown.
type MCPManager struct {
	idleTTL        time.Duration
	connectTimeout time.Duration
	errorRetry     time.Duration

	mu       sync.Mutex
	entries  map[key]*cacheEntry
	inflight map[key]*connectCall
	stop     chan struct{} // closed by Close to stop the janitor
	stopped  sync.WaitGroup

	// system supervises the app-provided browser-use server (design D6/D7);
	// nil unless WithSystemBrowserUse configured it. The supervisor shares
	// this manager's mutex and stop channel.
	system *systemSupervisor
	// manualSupervision keeps the supervisor's loop goroutine from starting,
	// so tests drive ticks by hand. In-package test seam only.
	manualSupervision bool

	// connector dials ref's connection; production wires the mcp-go factory
	// (connect). A seam so unit tests can stub the transport layer.
	connector func(ctx context.Context, ref Ref) (*connection, error)
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

// withManualSupervision keeps the system supervisor's loop goroutine from
// starting so tests drive ticks by hand (in-package test seam only).
func withManualSupervision() ManagerOption {
	return func(m *MCPManager) {
		m.manualSupervision = true
	}
}

// NewMCPManager builds the manager and starts its idle janitor. With
// WithSystemBrowserUse it also starts the system server's supervisor, which
// spawns the child at init.
func NewMCPManager(opts ...ManagerOption) *MCPManager {
	m := &MCPManager{
		idleTTL:        DefaultIdleTTL,
		connectTimeout: DefaultConnectTimeout,
		errorRetry:     DefaultErrorRetry,
		entries:        make(map[key]*cacheEntry),
		inflight:       make(map[key]*connectCall),
		stop:           make(chan struct{}),
	}
	m.connector = func(ctx context.Context, ref Ref) (*connection, error) {
		return connect(ctx, ref.Conn)
	}
	for _, opt := range opts {
		opt(m)
	}
	m.stopped.Add(1)
	go m.janitor()
	if m.system != nil && !m.manualSupervision {
		m.system.start()
	}
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
// The supervised system server's entry is exempt: the supervisor keeps that
// child warm by design (spawned at init so the first browser action pays no
// spawn cost), and reaping it would kill the child just to have the
// supervisor respawn it.
func (m *MCPManager) sweepIdle() {
	m.mu.Lock()
	if m.idleTTL <= 0 {
		m.mu.Unlock()
		return
	}
	now := time.Now()
	var expired []key
	for k, e := range m.entries {
		if m.supervised(k.serverID) {
			continue
		}
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
// attempt through the per-key singleflight record. The supervised system
// server is the exception: its entry is owned by its supervisor (spawned at
// init, respawned with backoff), so a miss or a stale error returns the
// supervisor's state instead of dialing — a spawn attempt outside the
// supervisor's backoff bookkeeping would bypass its crash-loop cap.
func (m *MCPManager) Tools(ctx context.Context, ref Ref) ([]tool.BaseTool, error) {
	k := key{ref.ServerID}

	if m.supervised(ref.ServerID) {
		m.mu.Lock()
		e, ok := m.entries[k]
		m.mu.Unlock()
		if !ok || e.err != nil {
			if ok && e.err != nil {
				return nil, e.err
			}
			return nil, fmt.Errorf("browser-use system server is starting")
		}
		return e.conn.tools, nil
	}

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

// Invoke calls one tool on the system browser-use server directly, carrying
// the given metadata as the call's _meta — the per-call channel the parent
// stamps the calling session's workspace screenshots directory through
// (internal/browseruse.MetaScreenshotDir), so the child writes captures
// inside the model's workspace jail. argsJSON passes through verbatim, and
// the result is the same unwrapped payload text the listed eino tools'
// InvokableRun returns (mcp-result-unwrapping), so callers parse both
// identically. System refs only: third-party servers have no meta contract.
// The call counts toward the supervisor's in-flight gauge like every listed
// tool's invocation.
func (m *MCPManager) Invoke(ctx context.Context, ref Ref, name, argsJSON string, meta map[string]any) (string, error) {
	if !m.supervised(ref.ServerID) || ref.ServerID != SystemServerID {
		return "", fmt.Errorf("mcp invoke: %s does not carry the system server's meta contract", ref.Name)
	}
	m.mu.Lock()
	e, ok := m.entries[key{ref.ServerID}]
	m.mu.Unlock()
	if !ok || e.err != nil || e.conn == nil {
		return "", fmt.Errorf("mcp invoke: browser-use system server is not ready")
	}
	return m.system.invokeWithMeta(ctx, e.conn, name, argsJSON, meta)
}

// connect dials ref within the connect timeout and returns the entry to
// cache — success or failure alike (design D7: the failure is cached as an
// errored entry, never propagated as a run failure).
func (m *MCPManager) connect(ctx context.Context, ref Ref) *cacheEntry {
	cctx, cancel := context.WithTimeout(ctx, m.connectTimeout)
	defer cancel()
	conn, err := m.connector(cctx, ref)
	if err != nil {
		return &cacheEntry{
			err:       err,
			status:    StatusError,
			statusErr: err.Error(),
			lastUsed:  time.Now(),
		}
	}
	return &cacheEntry{
		conn:      conn,
		status:    StatusConnected,
		toolCount: len(conn.tools),
		lastUsed:  time.Now(),
	}
}

// Status reports the outcome of the entry's most recent connection attempt:
// status (StatusConnected/Error), the failure message, and the tool count.
// Dropped (invalidated or reaped) entries return ok=false. Reads never
// disturb the entry's idle clock.
func (m *MCPManager) Status(serverID string) (status, statusErr string, toolCount int, ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[key{serverID}]
	if !ok {
		return "", "", 0, false
	}
	return e.status, e.statusErr, e.toolCount, true
}

// Invalidate drops the cached entry for serverID and closes its connection,
// reaping any stdio child. Called on every mutating config write (update,
// delete, disable) so config changes take effect immediately (design D7).
// The supervised system server's key is a no-op: it has no store row, no
// mutating write can address it (the settings service rejects the id), and
// its lifecycle belongs to the supervisor — config changes reach it through
// the supervisor's idle-boundary restart instead. Unknown keys are no-ops.
func (m *MCPManager) Invalidate(serverID string) {
	if m.supervised(serverID) {
		slog.Debug("invalidate ignored for the supervised system server", "server_id", serverID)
		return
	}
	k := key{serverID}
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

// Close stops the janitor, stops the system server's supervisor, closes
// every cached connection in parallel, and empties the cache. Used by the
// composition root's shutdown path.
func (m *MCPManager) Close() {
	m.mu.Lock()
	select {
	case <-m.stop:
		// Already closed.
	default:
		close(m.stop)
	}
	if m.system != nil {
		m.system.markStopped()
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
// orphan dies with its own idle timer (design D7 risk note).
func closeClient(k key, e *cacheEntry) {
	if e == nil || e.conn == nil || e.conn.client == nil {
		return
	}
	done := make(chan struct{})
	go func() {
		if err := e.conn.client.Close(); err != nil {
			slog.Debug("mcp client close failed", "server_id", k.serverID, "error", err)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		slog.Warn("mcp client close timed out; abandoning", "server_id", k.serverID)
	}
}

// String renders a key for logs.
func (k key) String() string {
	return k.serverID
}
