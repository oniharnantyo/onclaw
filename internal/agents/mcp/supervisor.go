package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/mark3labs/mcp-go/mcp"
)

// Supervision knobs (design D7/D8, spec browser-use "Supervision" and
// "Configuration at spawn boundaries"): a failed spawn attempt is retried
// after exponential backoff starting at one second and capped at thirty; five
// consecutive failures (failed attempts and unexpected child exits alike)
// stop respawning until a successful probe or an explicit restart; the
// supervisor re-evaluates state on a fixed tick, fast enough that the one
// second first backoff stays meaningful and a config change is noticed
// promptly.
const (
	SystemSpawnBackoffBase = 1 * time.Second
	SystemSpawnBackoffCap  = 30 * time.Second
	SystemSpawnMaxFailures = 5
	SystemSupervisorTick   = 500 * time.Millisecond
	// systemStderrTailBytes bounds the captured child stderr excerpt carried
	// by a failed connection state.
	systemStderrTailBytes = 2048
)

// exitEvent reports one child generation's death: the generation number and
// the last stderr excerpt captured while draining its pipe.
type exitEvent struct {
	gen     int64
	excerpt string
}

// systemSupervisor keeps one system server's child alive (design D7;
// add-computer-use design D2 keys supervision by spec ID, one supervisor per
// registered spec): it spawns at manager init, respawns on unexpected child
// exit or failed spawn attempt with exponential backoff, stops after a
// bounded consecutive-failure count, and restarts on config change only at
// an idle boundary — never while a tool call of that server is in flight
// (design D8).
//
// All state transitions run on the supervisor's single goroutine (run's tick)
// under the manager's mutex, which it shares with the connection cache; the
// only cross-goroutine writers are the stderr watchers (via the exited
// channel) and RestartSystemServer (via restart, under the same lock). No
// lock is ever held across I/O: config fetches, spawn attempts, and client
// teardown all run outside it.
type systemSupervisor struct {
	m    *MCPManager
	spec SystemServerSpec
	// spawn is the process seam (design D7): it launches one child generation
	// — dial, initialize handshake, and tool listing — and returns the ready
	// connection whose stderr the supervisor then watches. Production wires
	// the manager's connector; unit tests inject fakes.
	spawn func(ctx context.Context, ref Ref) (*connection, error)
	// now is the clock seam: production uses time.Now, tests inject a
	// stepping clock.
	now func() time.Time
	// inflight counts this server's tool calls in flight (design D8); the
	// counting wrappers around the current generation's tools bump it.
	inflight atomic.Int64
	// exited carries child-exit notifications from the stderr watchers. The
	// buffer absorbs a generation's death between ticks; a full buffer drops
	// the event — implausible at one event per generation, and the tick's
	// next spawn attempt still recovers.
	exited chan exitEvent

	// Guarded by m.mu.
	gen        int64         // current child generation; 0 = none
	haveChild  bool          // a live child exists (gen's entry is cached)
	spawnedCfg any           // the config snapshot the current child was spawned with
	failCount  int           // consecutive failed attempts + unexpected exits
	backoff    time.Duration // delay applied after the next failure
	nextAt     time.Time     // earliest next spawn attempt
	capped     bool          // gave up: surfaced failed until a probe/restart
	stopped    bool          // set on Close so a late spawn attempt bails
}

// newSystemSupervisor builds the supervisor for one spec with production
// seams: the manager's connector as the spawn step and the real clock.
func newSystemSupervisor(m *MCPManager, spec SystemServerSpec) *systemSupervisor {
	s := &systemSupervisor{
		m:      m,
		spec:   spec,
		now:    time.Now,
		exited: make(chan exitEvent, 64),
	}
	s.spawn = m.connector
	return s
}

// start launches the supervisor loop on its own goroutine, tracked by the
// manager's shutdown WaitGroup.
func (s *systemSupervisor) start() {
	s.m.stopped.Add(1)
	go s.run()
}

// run is the supervisor's loop: spawn at init, then re-evaluate on the fixed
// tick until the manager closes.
func (s *systemSupervisor) run() {
	defer s.m.stopped.Done()
	s.m.mu.Lock()
	s.nextAt = s.now()
	s.m.mu.Unlock()
	ticker := time.NewTicker(SystemSupervisorTick)
	defer ticker.Stop()
	s.tick() // spawn at init, not half a tick later
	for {
		select {
		case <-s.m.stop:
			return
		case <-ticker.C:
			s.tick()
		}
	}
}

// tick advances the supervision state machine once: reap child exits,
// evaluate config changes at the idle boundary, and spawn when due.
func (s *systemSupervisor) tick() {
	s.reapExits()

	s.m.mu.Lock()
	if s.stopped || s.capped {
		s.m.mu.Unlock()
		return
	}
	liveChild := s.haveChild
	s.m.mu.Unlock()

	if liveChild {
		s.checkConfig()
		return
	}
	s.m.mu.Lock()
	due := !s.now().Before(s.nextAt)
	s.m.mu.Unlock()
	if due {
		s.spawnAttempt()
	}
}

// reapExits drains pending child-exit events. Only the current generation's
// exit matters — retired generations (deliberate restarts) were closed on
// purpose. An unexpected exit is one failure in the consecutive count: the
// dead entry becomes an errored one (the pane's degraded state) and the
// respawn is scheduled at the grown backoff, up to the cap that stops
// respawning with the stderr excerpt as the surfaced reason.
func (s *systemSupervisor) reapExits() {
	for {
		select {
		case ev := <-s.exited:
			s.m.mu.Lock()
			if s.stopped || ev.gen != s.gen {
				s.m.mu.Unlock()
				continue
			}
			s.failCount++
			failures := s.failCount
			capped := false
			reason := systemFailure(s.spec.ID+" server exited unexpectedly", ev.excerpt)
			e := &cacheEntry{
				err:       fmt.Errorf("%s", reason),
				status:    StatusError,
				statusErr: reason,
				lastUsed:  s.now(),
			}
			var old *cacheEntry
			if s.haveChild {
				s.haveChild = false
				s.gen++ // retire: later watchers of the dead generation are ignored
				old = s.m.entries[key{s.spec.ID}]
				s.m.entries[key{s.spec.ID}] = e
			}
			if failures >= SystemSpawnMaxFailures {
				capped = true
				s.capped = true
				e.statusErr = fmt.Sprintf("%s; respawning stopped after %d consecutive failures",
					reason, failures)
			} else {
				s.scheduleNextLocked()
			}
			s.m.mu.Unlock()
			if old != nil {
				// The child is already dead; the close just releases the
				// client under the usual watchdog.
				closeClient(key{s.spec.ID}, old)
			}
			if capped {
				slog.Warn("system server exits exceeded the supervision cap; respawning stopped",
					"server", s.spec.ID, "generation", ev.gen, "consecutive_failures", failures, "stderr", ev.excerpt)
			} else {
				slog.Warn("system server exited; supervision engaged",
					"server", s.spec.ID, "generation", ev.gen, "consecutive_failures", failures, "stderr", ev.excerpt)
			}
		default:
			return
		}
	}
}

// markStopped flags the supervisor so a spawn attempt already in flight when
// the manager closes abandons its connection instead of caching it. Called
// by Close with m.mu held.
func (s *systemSupervisor) markStopped() {
	s.stopped = true
}

// checkConfig compares the persisted settings against the snapshot the
// current child was spawned with (design D8): a difference schedules a
// restart that only executes while zero of that server's tool calls are in
// flight — deferred otherwise, never mid-action. A config-read failure never
// tears down a healthy child; it only delays the check. Snapshots are
// compared deeply: the spec contract wants a comparable value type, and the
// deep comparison keeps a non-comparable shape a change signal instead of a
// panic in the supervision loop.
func (s *systemSupervisor) checkConfig() {
	cfg, err := s.spec.Config(context.Background())
	if err != nil {
		slog.Debug("system config check failed; keeping the current child",
			"server", s.spec.ID, "error", err)
		return
	}
	s.m.mu.Lock()
	if s.stopped || !s.haveChild || reflect.DeepEqual(cfg, s.spawnedCfg) {
		s.m.mu.Unlock()
		return
	}
	if s.inflight.Load() > 0 {
		s.m.mu.Unlock() // deferred: retried on a later tick, once idle
		return
	}
	// Idle boundary: retire the current generation and fall through to a
	// fresh spawn with the new snapshot.
	s.haveChild = false
	s.gen++
	old := s.m.entries[key{s.spec.ID}]
	delete(s.m.entries, key{s.spec.ID})
	s.failCount = 0 // a deliberate restart is not a failure
	s.backoff = SystemSpawnBackoffBase
	s.nextAt = s.now()
	s.m.mu.Unlock()
	if old != nil {
		closeClient(key{s.spec.ID}, old)
	}
	slog.Info("system config changed; restarting at idle boundary",
		"server", s.spec.ID)
	s.spawnAttempt()
}

// spawnAttempt is one probe: fetch the config fresh at the spawn boundary
// (design D8), synthesize the definition, and dial the child. Success resets
// the consecutive-failure count and caches the ready connection — its tools
// wrapped with the in-flight counter — while failure counts toward the cap
// and surfaces an errored entry. A hold answer (coordination contract C3)
// is neither: the server is awaiting readiness, so no child spawns and the
// failure bookkeeping does not move.
func (s *systemSupervisor) spawnAttempt() {
	cfg, err := s.spec.Config(context.Background())
	if isSystemServerHold(err) {
		s.hold(err)
		return
	}
	var conn *connection
	if err == nil {
		exe, exeErr := ownExecutable()
		if exeErr == nil {
			cctx, cancel := context.WithTimeout(context.Background(), s.m.connectTimeout)
			conn, err = s.spawn(cctx, s.spec.Ref(exe, cfg))
			cancel()
		} else {
			err = exeErr
		}
	}

	s.m.mu.Lock()
	if s.stopped {
		s.m.mu.Unlock()
		if conn != nil {
			abandonClient(conn.client)
		}
		return
	}
	if err != nil {
		s.fail(err)
		failures := s.failCount
		capped := s.capped
		s.m.mu.Unlock()
		if capped {
			slog.Warn("system spawn failures exceeded the supervision cap; respawning stopped",
				"server", s.spec.ID, "consecutive_failures", failures, "error", err)
		} else {
			slog.Warn("system spawn attempt failed",
				"server", s.spec.ID, "consecutive_failures", failures, "error", err)
		}
		return
	}
	conn.tools = s.countCalls(conn.tools)
	s.gen++
	gen := s.gen
	s.haveChild = true
	s.spawnedCfg = cfg
	s.failCount = 0 // a successful probe resets the count (spec browser-use "Supervision")
	s.backoff = SystemSpawnBackoffBase
	s.m.entries[key{s.spec.ID}] = &cacheEntry{
		conn:      conn,
		status:    StatusConnected,
		toolCount: len(conn.tools),
		lastUsed:  s.now(),
	}
	s.m.mu.Unlock()
	if conn.stderr != nil {
		go s.watchStderr(gen, conn.stderr)
	}
	slog.Info("system server spawned", "server", s.spec.ID, "generation", gen, "tools", len(conn.tools))
}

// hold records an awaiting-readiness tick (add-computer-use coordination
// contract C3): the entry surfaces the held status with the provider's
// reason, no child spawns, the consecutive-failure count and backoff do not
// move, and the next tick re-checks (nextAt stays due). A held server
// offers no tools — its entry carries the readable hold reason as the
// Tools-facing error. Called outside m.mu.
func (s *systemSupervisor) hold(hold error) {
	s.m.mu.Lock()
	if s.stopped {
		s.m.mu.Unlock()
		return
	}
	s.m.entries[key{s.spec.ID}] = &cacheEntry{
		err:       holdEntryErr(s.spec.ID, hold),
		status:    StatusHeld,
		statusErr: holdErrText(hold),
		lastUsed:  s.now(),
	}
	s.m.mu.Unlock()
	slog.Info("system server awaiting readiness",
		"server", s.spec.ID, "reason", holdErrText(hold))
}

// fail records one failed spawn attempt (config fetch, binary resolution, or
// dial): the consecutive count grows, the backoff grows with it, and the
// failure is surfaced through an errored entry. Reaching the cap stops
// respawning — the errored entry stays until a successful probe or restart.
// Called with m.mu held.
func (s *systemSupervisor) fail(err error) {
	s.failCount++
	reason := fmt.Sprintf("%s spawn failed: %v", s.spec.ID, err)
	e := &cacheEntry{
		err:       fmt.Errorf("%s", reason),
		status:    StatusError,
		statusErr: reason,
		lastUsed:  s.now(),
	}
	if s.failCount >= SystemSpawnMaxFailures {
		s.capped = true
		e.statusErr = fmt.Sprintf("%s; respawning stopped after %d consecutive failures",
			reason, s.failCount)
	} else {
		s.scheduleNextLocked()
	}
	s.m.entries[key{s.spec.ID}] = e
}

// scheduleNextLocked sets the next spawn attempt time at the grown backoff:
// the consecutive-failure count drives the step (first failure one second,
// then two, four, … capped at thirty), so a count reset by a successful
// probe restarts the ladder from the base. Called with m.mu held.
func (s *systemSupervisor) scheduleNextLocked() {
	delay := SystemSpawnBackoffBase << (s.failCount - 1)
	if delay <= 0 || delay > SystemSpawnBackoffCap {
		delay = SystemSpawnBackoffCap
	}
	s.backoff = delay
	s.nextAt = s.now().Add(delay)
}

// restart re-arms supervision from any state (the manual path past a crash
// loop): counters reset, the current child — if any — is torn down, and the
// next tick respawns with fresh config. The surfaced entry goes with them:
// an errored (capped) or held entry is the previous supervision epoch's
// state, and a re-armed supervisor owns none — the state view reads unknown
// until the next spawn attempt lands.
func (s *systemSupervisor) restart() {
	s.m.mu.Lock()
	if s.stopped {
		s.m.mu.Unlock()
		return
	}
	s.capped = false
	s.failCount = 0
	s.backoff = SystemSpawnBackoffBase
	var old *cacheEntry
	if s.haveChild {
		s.haveChild = false
		s.gen++ // retire: the deliberate close's exit event is ignored
		old = s.m.entries[key{s.spec.ID}]
	}
	delete(s.m.entries, key{s.spec.ID})
	s.nextAt = s.now()
	s.m.mu.Unlock()
	if old != nil {
		closeClient(key{s.spec.ID}, old)
	}
}

// watchStderr drains one child generation's stderr until the child exits
// (the pipe hits EOF or errors), then reports the death with the captured
// excerpt. Draining is also what keeps a chatty child from blocking on a
// full stderr pipe. The final send is non-blocking: the tick will still
// notice a missed report through the next spawn attempt's failure.
func (s *systemSupervisor) watchStderr(gen int64, r io.Reader) {
	tail := &stderrTail{max: systemStderrTailBytes}
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			tail.write(buf[:n])
		}
		if err != nil {
			break // EOF or a closed pipe: the child exited (or was closed)
		}
	}
	select {
	case s.exited <- exitEvent{gen: gen, excerpt: tail.excerpt()}:
	default:
		slog.Warn("system exit report dropped; supervision buffer full",
			"server", s.spec.ID)
	}
}

// countCalls wraps each of a generation's tools so every invocation counts
// toward the supervisor's in-flight gauge (design D8): the config restart
// defers while the gauge is positive, so a live browser action is never cut
// mid-call. Wrapping happens exactly once per generation, at spawn success.
func (s *systemSupervisor) countCalls(tools []tool.BaseTool) []tool.BaseTool {
	gauge := &s.inflight
	out := make([]tool.BaseTool, 0, len(tools))
	for _, t := range tools {
		switch b := t.(type) {
		case tool.StreamableTool:
			out = append(out, &countedStreamableTool{StreamableTool: b, inflight: gauge})
		case tool.InvokableTool:
			out = append(out, &countedInvokableTool{InvokableTool: b, inflight: gauge})
		default:
			out = append(out, t) // bare BaseTool: schema only, never executed
		}
	}
	return out
}

// countedInvokableTool counts invocations of one browser-use tool.
type countedInvokableTool struct {
	tool.InvokableTool
	inflight *atomic.Int64
}

func (t *countedInvokableTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	t.inflight.Add(1)
	defer t.inflight.Add(-1)
	return t.InvokableTool.InvokableRun(ctx, argumentsInJSON, opts...)
}

// countedStreamableTool counts streaming invocations of one browser-use tool.
type countedStreamableTool struct {
	tool.StreamableTool
	inflight *atomic.Int64
}

func (t *countedStreamableTool) StreamableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (*schema.StreamReader[string], error) {
	t.inflight.Add(1)
	defer t.inflight.Add(-1)
	return t.StreamableTool.StreamableRun(ctx, argumentsInJSON, opts...)
}

// invokeWithMeta performs one direct tool call on the system server's live
// connection with the given metadata as the call's _meta (the parent's
// per-call channel for the calling session's workspace screenshots
// directory). The semantics mirror the listed tools' eino adapter exactly —
// empty arguments become an empty object, the unwrapped payload text is the
// result (mcp-result-unwrapping), a transport failure or a server-side error
// result is an error — and the call counts toward the in-flight gauge so a
// config restart never cuts a live capture (design D8).
func (s *systemSupervisor) invokeWithMeta(ctx context.Context, conn *connection, name, argsJSON string, meta map[string]any) (string, error) {
	if argsJSON == "" {
		argsJSON = "{}"
	}
	gauge := &s.inflight
	gauge.Add(1)
	defer gauge.Add(-1)

	var mcpMeta *mcp.Meta
	if meta != nil {
		mcpMeta = mcp.NewMetaFromMap(meta)
	}
	result, err := conn.client.CallTool(ctx, mcp.CallToolRequest{
		Request: mcp.Request{
			Method: "tools/call",
		},
		Params: mcp.CallToolParams{
			Name:      name,
			Arguments: json.RawMessage(argsJSON),
			Meta:      mcpMeta,
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to call mcp tool: %w", err)
	}
	marshaled, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("failed to marshal mcp tool result: %w", err)
	}
	if result.IsError {
		return "", fmt.Errorf("failed to call mcp tool, mcp server return error: %s", marshaled)
	}
	return unwrapResultEnvelope(string(marshaled)), nil
}

// stderrTail keeps the last max bytes of a child's stderr: the excerpt a
// failed connection state carries (spec browser-use "Supervision"). Writes
// and reads may race when a failed dial abandons its bounded read (the
// withStderrTail timeout path) while the drain goroutine is still appending,
// so both sides take the lock.
type stderrTail struct {
	mu  sync.Mutex
	buf []byte
	max int
}

func (t *stderrTail) write(p []byte) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > 2*t.max {
		t.buf = append([]byte(nil), t.buf[len(t.buf)-t.max:]...)
	}
}

// excerpt returns the captured tail, trimmed to the bound.
func (t *stderrTail) excerpt() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.buf) > t.max {
		t.buf = t.buf[len(t.buf)-t.max:]
	}
	return strings.TrimSpace(string(t.buf))
}

// systemFailure renders a supervision failure for the status plumbing: the
// reason, carrying the child's last stderr excerpt when one was captured.
func systemFailure(reason, excerpt string) string {
	if excerpt == "" {
		return reason
	}
	return reason + "; stderr: " + excerpt
}
