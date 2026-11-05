package mcp

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/require"
)

// The system server supervision tests (rebrand-browser-use-system-mcp tasks
// 3.1/3.2, spec browser-use "Supervision" + "Configuration at spawn
// boundaries"): a fake spawner (the process seam) and a stepping clock stand
// in for the real child, so respawn-after-crash, backoff growth, the
// consecutive-failure cap, the probe reset, and the defer-until-idle restart
// are all driven deterministically by manual ticks.

// fakeClock is the supervisor's stepping clock: tests advance it and the
// supervisor's backoff math sees the advanced time.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock() *fakeClock { return &fakeClock{t: time.Unix(0, 0)} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// parkedTool is a system tool whose run blocks until released — the "browser
// action mid-flight" the config restart must defer to.
type parkedTool struct {
	entered chan struct{}
	release chan struct{}
}

func (t *parkedTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: "navigate"}, nil
}

func (t *parkedTool) InvokableRun(context.Context, string, ...tool.Option) (string, error) {
	close(t.entered)
	<-t.release
	return "{}", nil
}

// fakeSpawner is the process seam: each call consumes the next queued
// outcome — an error fails the attempt, otherwise the attempt succeeds with a
// live (fake) child.
type fakeSpawner struct {
	mu    sync.Mutex
	refs  []Ref
	queue []error
	conns []*connection
	mk    func() []tool.BaseTool // per-success tool surface; nil = one stub tool
}

func (f *fakeSpawner) spawn(_ context.Context, ref Ref) (*connection, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refs = append(f.refs, ref)
	if len(f.queue) > 0 {
		err := f.queue[0]
		f.queue = f.queue[1:]
		if err != nil {
			return nil, err
		}
	}
	tools := []tool.BaseTool{&stubTool{name: "navigate"}}
	if f.mk != nil {
		tools = f.mk()
	}
	conn := &connection{client: &stubMCPClient{}, tools: tools}
	f.conns = append(f.conns, conn)
	return conn, nil
}

func (f *fakeSpawner) attempts() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.refs)
}

func (f *fakeSpawner) lastRef() Ref {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.refs[len(f.refs)-1]
}

func (f *fakeSpawner) closed(i int) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	cli, ok := f.conns[i].client.(*stubMCPClient)
	if !ok {
		return false
	}
	return cli.closed.Load()
}

// supervisorFixture wires a manager with the system server option, manual
// ticks, the stepping clock, and the fake spawner.
type supervisorFixture struct {
	m       *MCPManager
	spawner *fakeSpawner
	clock   *fakeClock
	sup     *systemSupervisor
	cfg     SystemServerConfig
}

func newSupervisorFixture(t *testing.T) *supervisorFixture {
	t.Helper()
	f := &supervisorFixture{
		spawner: &fakeSpawner{},
		clock:   newFakeClock(),
		cfg: SystemServerConfig{
			DefaultProfile:       "default",
			Headless:             true,
			IdleTimeoutSeconds:   30,
			ActionTimeoutSeconds: 60,
			AllowPrivate:         false,
		},
	}
	m := NewMCPManager(
		withConnector(func(context.Context, Ref) (*connection, error) {
			return nil, errors.New("the fake spawner must own system spawns")
		}),
		withManualSupervision(),
		WithSystemBrowserUse("/data/browser", func(context.Context) (SystemServerConfig, error) {
			return f.cfg, nil
		}),
	)
	t.Cleanup(m.Close)
	f.m = m
	f.sup = m.system
	f.sup.spawn = f.spawner.spawn
	f.sup.now = f.clock.Now
	return f
}

// exitChild simulates the current child's death: the stderr watcher's report,
// delivered synchronously to the drain channel.
func (f *supervisorFixture) exitChild(excerpt string) {
	f.m.mu.Lock()
	gen := f.sup.gen
	f.m.mu.Unlock()
	f.sup.exited <- exitEvent{gen: gen, excerpt: excerpt}
}

func TestSystemBrowserUseRefMatchesContract(t *testing.T) {
	ref := systemBrowserUseRef("/usr/local/bin/buddies", "/data/browser", SystemServerConfig{
		DefaultProfile:       "work",
		Headless:             true,
		IdleTimeoutSeconds:   45,
		ActionTimeoutSeconds: 90,
		AllowPrivate:         true,
	})
	require.Equal(t, SystemServerID, ref.ServerID)
	require.Equal(t, SystemServerID, ref.Name, "the naming pass must yield mcp__browser-use__<tool>")
	require.Equal(t, TransportStdio, ref.Conn.Transport)
	require.Equal(t, "/usr/local/bin/buddies", ref.Conn.Command)
	require.Equal(t, []string{
		"browser-mcp",
		"--browser-dir", "/data/browser",
		"--default-profile", "work",
		"--headless", "true",
		"--idle-timeout", "45",
		"--action-timeout", "90",
		"--allow-private", "true",
	}, ref.Conn.Args, "the child argv must match the spawn contract exactly")
}

func TestSystemServerWithoutOptionChangesNothing(t *testing.T) {
	m, _ := newTestManager(t)
	require.Nil(t, m.system)
	_, ok := m.SystemServer()
	require.False(t, ok, "no system server without the option")
	_, ok = m.SystemServerState()
	require.False(t, ok)
	require.False(t, m.supervised(SystemServerID))
	m.RestartSystemServer() // must not panic
}

func TestSystemSupervisorSpawnsAtFirstTick(t *testing.T) {
	f := newSupervisorFixture(t)

	// Before the first tick: unknown state, no entry.
	state, ok := f.m.SystemServerState()
	require.True(t, ok)
	require.Equal(t, StatusUnknown, state.Status)

	f.sup.tick()

	ref, ok := f.m.SystemServer()
	require.True(t, ok)
	tools, err := f.m.Tools(context.Background(), ref)
	require.NoError(t, err)
	require.Len(t, tools, 1)
	require.Equal(t, 1, f.spawner.attempts())
	require.Equal(t, SystemServerID, f.spawner.lastRef().ServerID)
	require.Equal(t, "browser-mcp", f.spawner.lastRef().Conn.Args[0])
	require.Contains(t, f.spawner.lastRef().Conn.Args, "--browser-dir")
	require.Contains(t, f.spawner.lastRef().Conn.Args, "/data/browser")
	require.Contains(t, f.spawner.lastRef().Conn.Args, "--allow-private")

	state, ok = f.m.SystemServerState()
	require.True(t, ok)
	require.Equal(t, StatusConnected, state.Status)
	require.Empty(t, state.StatusError)
	require.Equal(t, 1, state.ToolCount)

	// The generation's tools are wrapped with the in-flight counter.
	wrapped, ok := tools[0].(*countedInvokableTool)
	require.True(t, ok, "system tools must be wrapped with the in-flight counter")
	require.Same(t, &f.sup.inflight, wrapped.inflight)

	// Redundant ticks with a healthy child spawn nothing more.
	f.sup.tick()
	f.sup.tick()
	require.Equal(t, 1, f.spawner.attempts())
}

func TestSystemSupervisorRespawnsAfterCrash(t *testing.T) {
	f := newSupervisorFixture(t)
	f.sup.tick()
	require.Equal(t, 1, f.spawner.attempts())

	f.exitChild("panic: chromium gone")
	// The exit is one failure: the dead entry is errored with the excerpt and
	// the respawn is scheduled at the first backoff step.
	f.sup.tick()
	require.Equal(t, 1, f.spawner.attempts(), "no respawn before the backoff elapses")
	state, ok := f.m.SystemServerState()
	require.True(t, ok)
	require.Equal(t, StatusError, state.Status)
	require.Contains(t, state.StatusError, "exited unexpectedly")
	require.Contains(t, state.StatusError, "panic: chromium gone")
	require.True(t, f.spawner.closed(0), "the dead generation's client was released")

	f.clock.Advance(SystemSpawnBackoffBase - time.Millisecond)
	f.sup.tick()
	require.Equal(t, 1, f.spawner.attempts(), "the backoff must elapse fully")

	f.clock.Advance(time.Millisecond)
	f.sup.tick()
	require.Equal(t, 2, f.spawner.attempts(), "the child is respawned after the first backoff")
	state, _ = f.m.SystemServerState()
	require.Equal(t, StatusConnected, state.Status)
}

func TestSystemSupervisorBackoffGrowsAndCaps(t *testing.T) {
	f := newSupervisorFixture(t)
	boom := errors.New("exec: browser-mcp not found")
	f.spawner.queue = []error{boom, boom, boom, boom, boom}

	// Five consecutive failures at the grown backoff steps 1s, 2s, 4s, 8s —
	// and time alone never revives a capped supervisor.
	steps := []time.Duration{
		SystemSpawnBackoffBase,
		2 * SystemSpawnBackoffBase,
		4 * SystemSpawnBackoffBase,
		8 * SystemSpawnBackoffBase,
	}
	for _, step := range steps {
		f.sup.tick()
		f.clock.Advance(step)
	}
	f.sup.tick() // the fifth failure
	require.Equal(t, 5, f.spawner.attempts())
	state, ok := f.m.SystemServerState()
	require.True(t, ok)
	require.Equal(t, StatusError, state.Status)
	require.Contains(t, state.StatusError, "respawning stopped after 5 consecutive failures")
	require.Contains(t, state.StatusError, boom.Error())

	f.clock.Advance(24 * time.Hour)
	f.sup.tick()
	f.sup.tick()
	require.Equal(t, 5, f.spawner.attempts(), "a capped supervisor ignores elapsed time")
}

func TestSystemSupervisorProbeResetsFailureCount(t *testing.T) {
	f := newSupervisorFixture(t)
	boom := errors.New("spawn failed")
	// Two failures, then a successful probe (which resets the count), then a
	// crash, then four more failed attempts: the cap must land on the fifth
	// consecutive failure after the reset, not the fifth overall.
	f.spawner.queue = []error{boom, boom}

	f.sup.tick()                            // failure 1, backoff 1s
	f.clock.Advance(SystemSpawnBackoffBase) //
	f.sup.tick()                            // failure 2, backoff 2s
	require.Equal(t, 2, f.spawner.attempts())

	f.clock.Advance(2 * SystemSpawnBackoffBase)
	f.sup.tick() // the probe: attempt 3 succeeds, count resets
	require.Equal(t, 3, f.spawner.attempts())

	f.exitChild("crashed after a healthy start")
	f.sup.tick() // failure 1 again, respawn scheduled at the base backoff
	for i := 0; i < 4; i++ {
		f.spawner.queue = append(f.spawner.queue, boom)
		f.clock.Advance(SystemSpawnBackoffBase << i)
		f.sup.tick()
	}
	require.Equal(t, 7, f.spawner.attempts())
	state, ok := f.m.SystemServerState()
	require.True(t, ok)
	require.Contains(t, state.StatusError, "respawning stopped after 5 consecutive failures",
		"the cap counts consecutive failures since the last successful probe")
}

func TestSystemSupervisorDefersConfigRestartWhileCallsInFlight(t *testing.T) {
	f := newSupervisorFixture(t)
	parked := &parkedTool{entered: make(chan struct{}), release: make(chan struct{})}
	f.spawner.mk = func() []tool.BaseTool { return []tool.BaseTool{parked} }
	f.cfg = SystemServerConfig{DefaultProfile: "default", Headless: true,
		IdleTimeoutSeconds: 30, ActionTimeoutSeconds: 60, AllowPrivate: false}
	f.sup.tick()
	require.Equal(t, 1, f.spawner.attempts())

	// A browser-use call in flight, parked until the test releases it.
	ref, _ := f.m.SystemServer()
	tools, err := f.m.Tools(context.Background(), ref)
	require.NoError(t, err)
	invokable, ok := tools[0].(tool.InvokableTool)
	require.True(t, ok)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = invokable.InvokableRun(context.Background(), "{}")
	}()
	<-parked.entered
	require.Equal(t, int64(1), f.sup.inflight.Load(), "the parked call is counted in flight")

	// Change settings and tick: the restart must defer while the call runs.
	f.cfg = SystemServerConfig{DefaultProfile: "default", Headless: false,
		IdleTimeoutSeconds: 30, ActionTimeoutSeconds: 60, AllowPrivate: true}
	f.sup.tick()
	require.Equal(t, 1, f.spawner.attempts(), "a config restart must defer while a call is in flight")
	state, _ := f.m.SystemServerState()
	require.Equal(t, StatusConnected, state.Status, "the running child stays served during the deferral")

	// The call completes: the next tick restarts at the idle boundary.
	close(parked.release)
	<-done
	require.Equal(t, int64(0), f.sup.inflight.Load())
	f.sup.tick()
	require.Equal(t, 2, f.spawner.attempts(), "the deferred restart applies once idle")
	require.True(t, f.spawner.closed(0), "the old generation's client was torn down")
	require.NotEqual(t, f.spawner.lastRef().Conn.Args[1:], []string{
		"browser-mcp", "--browser-dir", "/data/browser",
		"--default-profile", "default",
		"--headless", "true",
		"--idle-timeout", "30", "--action-timeout", "60",
		"--allow-private", "false",
	}, "the respawn must carry the new snapshot")
	require.Equal(t, "false", f.spawner.lastRef().Conn.Args[6], "headless=false reached the argv")
	require.Equal(t, "true", f.spawner.lastRef().Conn.Args[12], "allow-private=true reached the argv")
}

func TestSystemSupervisorRestartsConfigImmediatelyWhenIdle(t *testing.T) {
	f := newSupervisorFixture(t)
	f.sup.tick()
	require.Equal(t, 1, f.spawner.attempts())

	f.cfg = SystemServerConfig{DefaultProfile: "work", Headless: true,
		IdleTimeoutSeconds: 10, ActionTimeoutSeconds: 20, AllowPrivate: false}
	f.sup.tick()
	require.Equal(t, 2, f.spawner.attempts(), "an idle child restarts on the same tick")
	require.Equal(t, "work", f.spawner.lastRef().Conn.Args[4],
		"the fresh snapshot reaches the spawn boundary")
	require.True(t, f.spawner.closed(0))
	state, _ := f.m.SystemServerState()
	require.Equal(t, StatusConnected, state.Status)
}

func TestSystemSupervisorEntryExemptFromIdleSweep(t *testing.T) {
	f := newSupervisorFixture(t)
	f.sup.tick()

	// Backdate past the idle TTL, then sweep: the supervised entry survives —
	// the supervisor owns that child's lifecycle.
	f.m.mu.Lock()
	f.m.entries[key{SystemServerID}].lastUsed = time.Now().Add(-2 * DefaultIdleTTL)
	f.m.mu.Unlock()
	f.m.sweepIdle()

	state, ok := f.m.SystemServerState()
	require.True(t, ok)
	require.Equal(t, StatusConnected, state.Status)
	require.False(t, f.spawner.closed(0), "the supervised child must not be reaped as idle")
}

func TestSystemSupervisorToolsNeverDialDirectly(t *testing.T) {
	f := newSupervisorFixture(t)
	boom := errors.New("spawn failed")
	f.spawner.queue = []error{boom}
	f.sup.tick()
	require.Equal(t, 1, f.spawner.attempts())

	ref, _ := f.m.SystemServer()
	_, err := f.m.Tools(context.Background(), ref)
	require.Error(t, err)
	require.Contains(t, err.Error(), "browser-use spawn failed")
	require.Equal(t, 1, f.spawner.attempts(),
		"Tools must surface the supervisor's failure, never dial the system key itself")

	// A missing entry (before any attempt) reports starting, still no dial.
	f.m.mu.Lock()
	delete(f.m.entries, key{SystemServerID})
	f.m.mu.Unlock()
	_, err = f.m.Tools(context.Background(), ref)
	require.Error(t, err)
	require.Contains(t, err.Error(), "starting")
	require.Equal(t, 1, f.spawner.attempts())
}

func TestSystemSupervisorInvalidateAndRestart(t *testing.T) {
	f := newSupervisorFixture(t)
	f.sup.tick()

	// External invalidation is a no-op: the supervisor owns the lifecycle.
	f.m.Invalidate(SystemServerID)
	state, ok := f.m.SystemServerState()
	require.True(t, ok)
	require.Equal(t, StatusConnected, state.Status)
	require.False(t, f.spawner.closed(0))

	// Crash, then burn the remaining failure budget on failed spawn attempts:
	// the cap surfaces and holds against elapsed time.
	f.exitChild("boom")
	f.sup.tick() // failure 1
	boom := errors.New("spawn failed")
	for i := 0; i < 3; i++ {
		f.spawner.queue = append(f.spawner.queue, boom)
		f.clock.Advance(SystemSpawnBackoffBase << i)
		f.sup.tick()
	}
	f.spawner.queue = append(f.spawner.queue, boom)
	f.clock.Advance(8 * SystemSpawnBackoffBase)
	f.sup.tick() // failure 5: capped
	state, _ = f.m.SystemServerState()
	require.Contains(t, state.StatusError, "respawning stopped")
	capped := f.spawner.attempts()
	f.clock.Advance(time.Hour)
	f.sup.tick()
	require.Equal(t, capped, f.spawner.attempts(), "the cap holds")

	// RestartSystemServer is the manual path past the cap.
	f.m.RestartSystemServer()
	f.sup.tick()
	require.Greater(t, f.spawner.attempts(), capped, "the restart re-arms supervision past the cap")
	state, _ = f.m.SystemServerState()
	require.Equal(t, StatusConnected, state.Status)
}

func TestWatchStderrCapturesExcerptAndReportsExit(t *testing.T) {
	f := newSupervisorFixture(t)
	pr, pw := io.Pipe()
	f.m.mu.Lock()
	f.sup.gen = 7
	f.m.mu.Unlock()
	go f.sup.watchStderr(7, pr)

	_, err := pw.Write([]byte("rod: chromium exited unexpectedly\n"))
	require.NoError(t, err)
	require.NoError(t, pw.Close())

	select {
	case ev := <-f.sup.exited:
		require.Equal(t, int64(7), ev.gen)
		require.Contains(t, ev.excerpt, "chromium exited unexpectedly")
	case <-time.After(2 * time.Second):
		t.Fatal("the stderr watcher never reported the child's exit")
	}
}

func TestSystemFailureCarriesStderrExcerpt(t *testing.T) {
	require.Equal(t, "reason only", systemFailure("reason only", ""))
	combined := systemFailure("reason", "  line one\nline two  ")
	require.Contains(t, combined, "reason")
	require.Contains(t, combined, "line one")
}

func TestStderrTailBoundsLength(t *testing.T) {
	tail := &stderrTail{max: 16}
	tail.write([]byte("0123456789abcdef"))
	tail.write([]byte("ghijklmnopqrstuv"))
	got := tail.excerpt()
	require.LessOrEqual(t, len(got), 16)
	require.Equal(t, "ghijklmnopqrstuv", got, "the tail keeps the most recent stderr")
}

// TestConstructChildEnvAllowPrivatePassthrough pins the deliberate baseline
// extension (spec browser-use "Private-network guard" keys on the server
// process env): BUDDIES_BROWSER_ALLOW_PRIVATE reaches the child when set in
// the parent and is omitted — never synthesized empty — when absent, like
// every other baseline entry.
func TestConstructChildEnvAllowPrivatePassthrough(t *testing.T) {
	parent := []string{
		"PATH=/usr/bin:/bin",
		"BUDDIES_BROWSER_ALLOW_PRIVATE=1",
		"BUDDIES_SECRET=hush", // parent-only, must stay excluded
	}
	env := constructChildEnv(parent, nil)
	require.Contains(t, env, "BUDDIES_BROWSER_ALLOW_PRIVATE=1")
	require.Len(t, env, 2, "the baseline passes PATH and the allow-private var only")

	withoutVar := []string{"PATH=/usr/bin:/bin"}
	env = constructChildEnv(withoutVar, nil)
	require.Len(t, env, 1)
	require.Equal(t, "PATH=/usr/bin:/bin", env[0])
}

// TestStdioDialPassesAllowPrivateToChild drives a real stdio dial against the
// compiled mock server: the guard variable set in the parent's environment
// reaches the child, the parent-only canary still does not (the
// exact-env contract holds alongside the passthrough), and the dial path
// still hands the supervisor the child's stderr.
func TestStdioDialPassesAllowPrivateToChild(t *testing.T) {
	bin := compileMockServer(t)
	t.Setenv(envLeakCanary, "leak-if-visible")
	t.Setenv("BUDDIES_BROWSER_ALLOW_PRIVATE", "1")

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	conn, err := connect(ctx, stdioConn(bin))
	require.NoError(t, err)
	defer func() { _ = conn.client.Close() }()

	require.NotNil(t, conn.stderr, "a stdio dial must expose the child's stderr to supervision")

	env := childEnv(t, ctx, conn.client)
	require.Equal(t, "1", env["BUDDIES_BROWSER_ALLOW_PRIVATE"],
		"the private-network guard variable must pass through to the server process")
	require.NotContains(t, env, envLeakCanary,
		"the passthrough must not regress the exact-env contract")
}
