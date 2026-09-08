package agents

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/oniharnantyo/onclaw/internal/domain"
)

// DefaultCancelEscalation bounds how long an explicit cancel waits for a
// safe point (in-flight model or tool call completing) before escalation: the
// ADK machine aborts the run at that deadline — still persisting the durable
// cancel marker — and the manager's last-resort context cancel follows one
// grace period later for calls that ignore stream teardown.
const DefaultCancelEscalation = 2 * time.Second

// cancelEscalationGrace is the extra window the manager's last-resort context
// cancel gives the ADK escalation to finish the run (and persist the cancel
// marker) before the run context is cancelled directly.
const cancelEscalationGrace = time.Second

// RunKey identifies a live run by the session it executes against. One run is
// active per session at a time, so the session coordinates are a sufficient
// cancel/drain handle (no run IDs are minted).
type RunKey struct {
	WorkspaceID string
	AgentID     string
	SessionID   string
}

// liveRun holds the lifecycle handles of one executing run: the cancel
// function for its manager-derived context, the ADK agent-level cancel
// function (the safe-point cancel state machine that persists the durable
// cancel marker), and the done channel the run goroutine closes when it has
// fully unwound. It also manages dynamic subscriber streams.
type liveRun struct {
	cancel      func()
	agentCancel adk.AgentCancelFunc
	done        chan struct{}

	mu          sync.Mutex
	nextSubID   uint64
	subscribers map[uint64]*EventStream
}

// runHandle bundles what a run goroutine needs from the manager: the detached
// run context (derived from the manager's base, never from a request or
// stream context), the context cancel, the finish hook that deregisters the
// run, and the done channel signalling full unwind.
type runHandle struct {
	ctx    context.Context
	cancel func()
	finish func()
	done   <-chan struct{}
}

// runManager is the single place run contexts originate. It tracks live runs
// keyed by session, derives each run context from its base context (process
// lifetime, cancelled on shutdown drain), and exposes explicit cancel and
// bounded graceful-drain operations. Consumer disconnects and stream
// abandonment never reach it — only an explicit cancel or drain stops a run.
type runManager struct {
	base context.Context

	// cancelEscalation bounds how long an explicit cancel waits for a safe
	// point before the ADK machine escalates to an immediate abort (which
	// still persists the cancel marker).
	cancelEscalation time.Duration

	mu       sync.Mutex
	live     map[RunKey]*liveRun
	draining bool
}

// newRunManager creates a manager deriving all run contexts from base.
func newRunManager(base context.Context, cancelEscalation time.Duration) *runManager {
	if cancelEscalation <= 0 {
		cancelEscalation = DefaultCancelEscalation
	}
	return &runManager{
		base:             base,
		cancelEscalation: cancelEscalation,
		live:             make(map[RunKey]*liveRun),
	}
}

// start registers a new live run for key and returns its handle. agentCancel
// is the ADK cancel function created for this run via adk.WithCancel (never
// nil — the runner creates one per run before registering). The run context
// derives from the manager's base — never from the caller's context. A second
// start on a live session (or during a drain) is rejected with an error
// wrapping domain.ErrConflict so the HTTP layer translates it to 409.
func (m *runManager) start(key RunKey, agentCancel adk.AgentCancelFunc) (*runHandle, error) {
	m.mu.Lock()
	if m.draining {
		m.mu.Unlock()
		return nil, fmt.Errorf("%w: runner is draining", domain.ErrConflict)
	}
	if _, exists := m.live[key]; exists {
		m.mu.Unlock()
		return nil, fmt.Errorf("%w: a run is already active for session %q", domain.ErrConflict, key.SessionID)
	}

	ctx, cancel := context.WithCancel(m.base)
	lr := &liveRun{
		cancel:      cancel,
		agentCancel: agentCancel,
		done:        make(chan struct{}),
		subscribers: make(map[uint64]*EventStream),
	}
	m.live[key] = lr
	m.mu.Unlock()

	// Deregister once the run goroutine has fully unwound so a completed
	// session can run again. The sweep also closes any subscriber that raced
	// in after the run's own CloseSubscribers but before done — every
	// subscriber stream is guaranteed to close when the run ends.
	go func() {
		<-lr.done
		lr.mu.Lock()
		subs := make([]*EventStream, 0, len(lr.subscribers))
		for subID, sub := range lr.subscribers {
			subs = append(subs, sub)
			delete(lr.subscribers, subID)
		}
		lr.mu.Unlock()
		for _, sub := range subs {
			_ = sub.Close()
		}
		m.mu.Lock()
		if current, ok := m.live[key]; ok && current == lr {
			delete(m.live, key)
		}
		m.mu.Unlock()
	}()

	return &runHandle{
		ctx:    ctx,
		cancel: cancel,
		finish: sync.OnceFunc(func() { close(lr.done) }),
		done:   lr.done,
	}, nil
}

// Subscribe dynamically attaches a new subscriber stream to the live run identified by key.
// It returns the assigned subscription ID, the EventStream, and true if the run is active.
// If the run is not active, it returns (0, nil, false).
func (m *runManager) Subscribe(key RunKey) (uint64, *EventStream, bool) {
	m.mu.Lock()
	lr, ok := m.live[key]
	m.mu.Unlock()
	if !ok {
		return 0, nil, false
	}

	lr.mu.Lock()
	defer lr.mu.Unlock()

	select {
	case <-lr.done:
		return 0, nil, false
	default:
	}

	lr.nextSubID++
	subID := lr.nextSubID
	stream := NewEventStream(128)
	lr.subscribers[subID] = stream
	return subID, stream, true
}

// Unsubscribe removes subscriber subID from the live run identified by key and closes its stream.
func (m *runManager) Unsubscribe(key RunKey, subID uint64) {
	m.mu.Lock()
	lr, ok := m.live[key]
	m.mu.Unlock()
	if !ok {
		return
	}

	lr.mu.Lock()
	stream, exists := lr.subscribers[subID]
	if exists {
		delete(lr.subscribers, subID)
	}
	lr.mu.Unlock()

	if exists && stream != nil {
		_ = stream.Close()
	}
}

// Broadcast fans out ev to all active subscribers of the live run identified by key.
// If a subscriber's buffer is full or closed, it handles it gracefully without blocking the runner.
func (m *runManager) Broadcast(key RunKey, ev *TranscriptEvent) {
	m.mu.Lock()
	lr, ok := m.live[key]
	m.mu.Unlock()
	if !ok {
		return
	}

	lr.mu.Lock()
	subs := make([]*EventStream, 0, len(lr.subscribers))
	for _, sub := range lr.subscribers {
		subs = append(subs, sub)
	}
	lr.mu.Unlock()

	for _, sub := range subs {
		sub.Send(ev)
	}
}

// CloseSubscribers closes all active subscribers for the live run identified by key.
func (m *runManager) CloseSubscribers(key RunKey) {
	m.mu.Lock()
	lr, ok := m.live[key]
	m.mu.Unlock()
	if !ok {
		return
	}

	lr.mu.Lock()
	subs := make([]*EventStream, 0, len(lr.subscribers))
	for subID, sub := range lr.subscribers {
		subs = append(subs, sub)
		delete(lr.subscribers, subID)
	}
	lr.mu.Unlock()

	for _, sub := range subs {
		_ = sub.Close()
	}
}

// isLive reports whether a run is currently registered and executing for key.
func (m *runManager) isLive(key RunKey) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	lr, ok := m.live[key]
	if !ok {
		return false
	}
	select {
	case <-lr.done:
		return false
	default:
		return true
	}
}

// cancel triggers cancellation of the live run for key, returning false when
// no run is live. The primary path is the ADK agent-level cancel in safe-point
// mode: the in-flight model or tool call either completes and is recorded or
// is aborted after the escalation window, and in both cases the ADK cancel
// state machine persists the durable cancel marker into session history. If
// the run has not finished one grace period after that escalation — an
// in-flight call that ignores stream teardown and graph interrupts — the
// plain context cancel unwinds it cooperatively; the ADK then ends the turn
// with the context error instead of a marker. The plain context cancel is
// otherwise the last-resort unwind for drain stragglers (see drain).
func (m *runManager) cancel(key RunKey) bool {
	m.mu.Lock()
	lr, ok := m.live[key]
	m.mu.Unlock()
	if !ok {
		return false
	}
	// Commit-only: agentCancel returns once the cancel request is committed;
	// the run unwinds asynchronously through its own event stream.
	_, _ = lr.agentCancel(
		adk.WithAgentCancelMode(adk.CancelAfterChatModel|adk.CancelAfterToolCalls),
		adk.WithAgentCancelTimeout(m.cancelEscalation),
	)
	// Last-resort escalation: the ADK machine does not bridge to the run's Go
	// context on this path, so a call that ignores stream teardown and graph
	// interrupts would never unwind on its own. Cancel the context directly
	// once the escalation window plus grace has elapsed.
	go func() {
		timer := time.NewTimer(m.cancelEscalation + cancelEscalationGrace)
		defer timer.Stop()
		select {
		case <-lr.done:
		case <-timer.C:
			select {
			case <-lr.done:
			default:
				lr.cancel()
			}
		}
	}()
	return true
}

// drain stops accepting new runs, waits up to timeout for in-flight runs to
// reach a terminal state, then cancels stragglers (their cancel markers
// record through the existing safe-point path).
func (m *runManager) drain(timeout time.Duration) {
	m.mu.Lock()
	m.draining = true
	var waits []chan struct{}
	var stragglers []*liveRun
	for _, lr := range m.live {
		waits = append(waits, lr.done)
		stragglers = append(stragglers, lr)
	}
	m.mu.Unlock()

	deadline := time.After(timeout)
	for _, done := range waits {
		select {
		case <-done:
		case <-deadline:
			for _, lr := range stragglers {
				select {
				case <-lr.done:
				default:
					lr.cancel()
				}
			}
			return
		}
	}
}
