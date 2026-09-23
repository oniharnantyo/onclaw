package mcp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/mark3labs/mcp-go/client"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// stubMCPClient satisfies client.MCPClient by embedding the interface and
// overriding only Close — the manager never touches the other methods.
type stubMCPClient struct {
	client.MCPClient
	closed atomic.Bool
}

func (c *stubMCPClient) Close() error {
	c.closed.Store(true)
	return nil
}

// stubTool is a minimal InvokableTool for tool surfaces.
type stubTool struct{ name string }

func (t *stubTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: t.name}, nil
}

func (t *stubTool) InvokableRun(context.Context, string, ...tool.Option) (string, error) {
	return "{}", nil
}

// countingConnector is the manager's dialing seam under test: it counts
// attempts, hands out closable clients, and can be made to fail.
type countingConnector struct {
	mu       sync.Mutex
	attempts int
	fail     error
	delay    time.Duration
	clients  []*stubMCPClient
}

func (c *countingConnector) connect(ctx context.Context, ref Ref) (*connection, error) {
	c.mu.Lock()
	c.attempts++
	fail, delay := c.fail, c.delay
	c.mu.Unlock()
	if delay > 0 {
		// Dialing is context-aware like the real factory: an expired connect
		// window aborts the attempt.
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}
	if fail != nil {
		return nil, fail
	}
	cli := &stubMCPClient{}
	c.mu.Lock()
	c.clients = append(c.clients, cli)
	c.mu.Unlock()
	return &connection{
		client: cli,
		tools:  []tool.BaseTool{&stubTool{name: ref.ServerID + "-tool"}},
	}, nil
}

func (c *countingConnector) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.attempts
}

func newTestManager(t *testing.T, opts ...ManagerOption) (*MCPManager, *countingConnector) {
	t.Helper()
	conn := &countingConnector{}
	opts = append([]ManagerOption{withConnector(conn.connect)}, opts...)
	return NewMCPManager(opts...), conn
}

func TestMCPManagerCacheHitDoesNotReconnect(t *testing.T) {
	m, conn := newTestManager(t)
	defer m.Close()
	ref := Ref{WorkspaceID: "ws-1", ServerID: "srv-1", Name: "github"}

	first, err := m.Tools(context.Background(), ref)
	if err != nil {
		t.Fatalf("first Tools: %v", err)
	}
	second, err := m.Tools(context.Background(), ref)
	if err != nil {
		t.Fatalf("second Tools: %v", err)
	}
	if conn.count() != 1 {
		t.Fatalf("expected 1 connect, got %d", conn.count())
	}
	if len(first) != 1 || len(second) != 1 || first[0] != second[0] {
		t.Fatalf("cached tools differ: %v vs %v", first, second)
	}
	if _, _, count, ok := m.Status("ws-1", "srv-1"); !ok || count != 1 {
		t.Fatalf("status = %v, %d; want ok, 1", ok, count)
	}
}

func TestMCPManagerNeverSharesAcrossWorkspaces(t *testing.T) {
	m, conn := newTestManager(t)
	defer m.Close()

	if _, err := m.Tools(context.Background(), Ref{WorkspaceID: "ws-1", ServerID: "srv-1", Name: "github"}); err != nil {
		t.Fatalf("ws-1 Tools: %v", err)
	}
	if _, err := m.Tools(context.Background(), Ref{WorkspaceID: "ws-2", ServerID: "srv-1", Name: "github"}); err != nil {
		t.Fatalf("ws-2 Tools: %v", err)
	}
	if conn.count() != 2 {
		t.Fatalf("expected 2 connects (one per workspace), got %d", conn.count())
	}
}

func TestMCPManagerInvalidateClosesAndReconnects(t *testing.T) {
	m, conn := newTestManager(t)
	defer m.Close()
	ref := Ref{WorkspaceID: "ws-1", ServerID: "srv-1", Name: "github"}

	if _, err := m.Tools(context.Background(), ref); err != nil {
		t.Fatalf("Tools: %v", err)
	}
	m.Invalidate("ws-1", "srv-1")
	if !conn.clients[0].closed.Load() {
		t.Fatal("invalidate did not close the cached client")
	}
	if _, _, _, ok := m.Status("ws-1", "srv-1"); ok {
		t.Fatal("invalidate left the cached entry behind")
	}
	// Next use reconnects; unknown-key invalidation is a no-op.
	if _, err := m.Tools(context.Background(), ref); err != nil {
		t.Fatalf("Tools after invalidate: %v", err)
	}
	if conn.count() != 2 {
		t.Fatalf("expected reconnect after invalidate, attempts = %d", conn.count())
	}
	m.Invalidate("ws-9", "srv-9")
	if conn.count() != 2 {
		t.Fatalf("unknown-key invalidate connected again: %d", conn.count())
	}
}

func TestMCPManagerIdleExpiry(t *testing.T) {
	m, conn := newTestManager(t, WithIdleTTL(50*time.Millisecond))
	defer m.Close()
	ref := Ref{WorkspaceID: "ws-1", ServerID: "srv-1", Name: "github"}

	if _, err := m.Tools(context.Background(), ref); err != nil {
		t.Fatalf("Tools: %v", err)
	}
	// Backdate the entry so the sweep is deterministic regardless of ticker
	// timing, then run one sweep.
	m.mu.Lock()
	m.entries[key{"ws-1", "srv-1"}].lastUsed = time.Now().Add(-time.Hour)
	m.mu.Unlock()
	m.sweepIdle()

	if !conn.clients[0].closed.Load() {
		t.Fatal("idle sweep did not close the stdio-style client")
	}
	if _, _, _, ok := m.Status("ws-1", "srv-1"); ok {
		t.Fatal("idle sweep left the entry cached")
	}
	// The janitor reaps on its own too (idleTTL/2 clamped to 100ms floor).
	if _, err := m.Tools(context.Background(), ref); err != nil {
		t.Fatalf("Tools after sweep: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, _, _, ok := m.Status("ws-1", "srv-1"); !ok {
			if !conn.clients[1].closed.Load() {
				t.Fatal("janitor closed... nothing: client not closed on expiry")
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("janitor did not reap the idle entry within 2s")
}

func TestMCPManagerCloseTearsDownAll(t *testing.T) {
	m, conn := newTestManager(t)
	ref1 := Ref{WorkspaceID: "ws-1", ServerID: "srv-1", Name: "a"}
	ref2 := Ref{WorkspaceID: "ws-1", ServerID: "srv-2", Name: "b"}
	for _, ref := range []Ref{ref1, ref2} {
		if _, err := m.Tools(context.Background(), ref); err != nil {
			t.Fatalf("Tools %s: %v", ref.ServerID, err)
		}
	}
	m.Close()
	for i, cli := range conn.clients {
		if !cli.closed.Load() {
			t.Fatalf("client %d survived Close", i)
		}
	}
	if _, _, _, ok := m.Status("ws-1", "srv-1"); ok {
		t.Fatal("Close left entries cached")
	}
	m.Close() // idempotent
}

func TestMCPManagerDeadServerSkipsAndRetries(t *testing.T) {
	conn := &countingConnector{fail: errors.New("connection refused")}
	m := NewMCPManager(withConnector(conn.connect), WithErrorRetry(50*time.Millisecond))
	defer m.Close()
	ref := Ref{WorkspaceID: "ws-1", ServerID: "srv-1", Name: "dead"}

	if _, err := m.Tools(context.Background(), ref); err == nil {
		t.Fatal("expected error from dead server")
	}
	status, statusErr, count, ok := m.Status("ws-1", "srv-1")
	if !ok || status != domain.MCPStatusError || statusErr == "" || count != 0 {
		t.Fatalf("status = (%q, %q, %d, %v)", status, statusErr, count, ok)
	}
	// Negatively cached: an immediate retry does not redial.
	if _, err := m.Tools(context.Background(), ref); err == nil {
		t.Fatal("expected cached error")
	}
	if conn.count() != 1 {
		t.Fatalf("expected negative cache, attempts = %d", conn.count())
	}
	// After the retry backoff the next use redials.
	m.mu.Lock()
	m.entries[key{"ws-1", "srv-1"}].lastUsed = time.Now().Add(-time.Hour)
	m.mu.Unlock()
	conn.mu.Lock()
	conn.fail = nil
	conn.mu.Unlock()
	if _, err := m.Tools(context.Background(), ref); err != nil {
		t.Fatalf("reconnect after recovery: %v", err)
	}
	if conn.count() != 2 {
		t.Fatalf("expected reconnect after backoff, attempts = %d", conn.count())
	}
}

func TestMCPManagerConcurrentFirstGetSharesConnect(t *testing.T) {
	conn := &countingConnector{delay: 30 * time.Millisecond}
	m := NewMCPManager(withConnector(conn.connect))
	defer m.Close()
	ref := Ref{WorkspaceID: "ws-1", ServerID: "srv-1", Name: "github"}

	var wg sync.WaitGroup
	results := make([][]tool.BaseTool, 8)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			tools, err := m.Tools(context.Background(), ref)
			if err != nil {
				t.Errorf("concurrent Tools: %v", err)
				return
			}
			results[i] = tools
		}(i)
	}
	wg.Wait()
	if conn.count() != 1 {
		t.Fatalf("expected single shared connect, got %d", conn.count())
	}
	for i, r := range results {
		if len(r) != 1 || r[0] != results[0][0] {
			t.Fatalf("goroutine %d got different tools", i)
		}
	}
}

func TestMCPManagerConnectTimeoutBoundsDialing(t *testing.T) {
	conn := &countingConnector{delay: 500 * time.Millisecond}
	m := NewMCPManager(
		withConnector(conn.connect),
		WithConnectTimeout(20*time.Millisecond),
	)
	defer m.Close()

	start := time.Now()
	_, err := m.Tools(context.Background(), Ref{WorkspaceID: "ws-1", ServerID: "srv-1", Name: "slow"})
	if err == nil {
		t.Fatal("expected connect timeout error")
	}
	if elapsed := time.Since(start); elapsed > 300*time.Millisecond {
		t.Fatalf("connect was not bounded: %v", elapsed)
	}
	if _, _, _, ok := m.Status("ws-1", "srv-1"); !ok {
		t.Fatal("timed-out connect should cache the errored entry")
	}
}

// Compile-time check: *MCPManager satisfies the runner-facing seam.
var _ ToolSource = (*MCPManager)(nil)

func TestRefString(t *testing.T) {
	if got := (key{"ws", "srv"}).String(); got != "ws/srv" {
		t.Fatalf("key.String() = %q", got)
	}
	if fmt.Sprint(key{"a", "b"}) != "a/b" {
		t.Fatal("key should render as workspace/server")
	}
}

// TestProbeAndManagerDialConstructIdenticalEnv pins probe-vs-run parity
// (openspec/changes/fix-stdio-env-leak task 3.2) and doubles as the automated
// stand-in for the manual verification pass (task 4.2): Probe and the manager
// both dial through connect, so the probe connects under the constructed env
// (tool count > 0) and a manager-mediated dial serves a real tool call whose
// child observes exactly what constructChildEnv builds for the same
// connection — configured row present, parent-only canary absent.
func TestProbeAndManagerDialConstructIdenticalEnv(t *testing.T) {
	bin := compileMockServer(t)
	t.Setenv(envLeakCanary, "leak-if-visible")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	conn := stdioConn(bin)
	want := constructChildEnv(os.Environ(), conn.Env)
	sort.Strings(want)

	// Probe path: a fresh, uncached dial under the constructed env.
	count, err := Probe(ctx, Ref{WorkspaceID: "ws-probe", ServerID: "srv-1", Name: "mock", Conn: conn})
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if count == 0 {
		t.Fatal("probe listed zero tools")
	}

	// Run path: a manager-mediated dial of the same connection.
	m := NewMCPManager()
	defer m.Close()
	if _, err := m.Tools(ctx, Ref{WorkspaceID: "ws-run", ServerID: "srv-1", Name: "mock", Conn: conn}); err != nil {
		t.Fatalf("manager Tools: %v", err)
	}
	m.mu.Lock()
	cli := m.entries[key{"ws-run", "srv-1"}].conn.client
	m.mu.Unlock()

	// A real tool call works through the constructed env.
	if got := callTool(t, ctx, cli, "test_tool"); got != "tool result" {
		t.Fatalf("test_tool = %q", got)
	}

	// …and the child observes exactly the shared constructor's env — the
	// canary leaks only if the dial merged over the parent.
	env := childEnv(t, ctx, cli)
	got := make([]string, 0, len(env))
	for name, value := range env {
		got = append(got, name+"="+value)
	}
	sort.Strings(got)
	if len(got) != len(want) {
		t.Fatalf("manager-dialed child env has %d entries, want %d:\n got %v\nwant %v", len(got), len(want), got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("manager-dialed child env mismatch at entry %d:\n got %v\nwant %v", i, got, want)
		}
	}
}
