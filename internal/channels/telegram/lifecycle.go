package telegram

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/oniharnantyo/buddies/internal/channels/telegram/botapi"
	"github.com/oniharnantyo/buddies/internal/gateway"
	"github.com/oniharnantyo/buddies/internal/store"
)

// attachment is one gateway attachment's live machinery (the scheduler
// ticker.go pattern). The gateway invokes the state listener synchronously on
// transitions, so mu is never held while waiting on the loop goroutine.
type attachment struct {
	mu     sync.Mutex
	sub    func()             // gateway subscription cancel; nil once detached
	cancel context.CancelFunc // loop cancel; nil while no loop is running
	wg     sync.WaitGroup     // tracks the loop + heartbeat goroutines
	dead   bool               // terminal failure: no restarts (409/401/404)
	gw     gateway.Manager    // the attached gateway, for state-gated restarts
}

// Attach subscribes to the gateway (subscribe-first, design D1) and starts
// the channel runtime on the first transition to Running — or immediately
// when the gateway is already Running. The runtime keeps polling through
// Paused (admitted updates hold instead of dispatching, design D9) and stops
// on a transition to Stopped; a later Running starts a fresh runtime. Attach
// does not block on the gateway.
func (s *Service) Attach(gw gateway.Manager) {
	// Re-attaching (never done by the composition root) replaces the
	// previous attachment: stop its runtime, drop its subscription.
	if prev := s.swapAttachment(nil); prev != nil {
		prev.detach()
		prev.stop()
	}

	att := &attachment{gw: gw}
	cancel := gw.Subscribe(func(_, to gateway.State) { s.onGatewayState(att, to) })
	att.mu.Lock()
	att.sub = cancel
	att.mu.Unlock()
	s.swapAttachment(att)

	// Subscribe before reading the state so a concurrent transition cannot
	// fall between the two: the listener and this check are both
	// start-on-Running, and starting twice is a no-op.
	if gw.State() == gateway.StateRunning {
		s.startLoop(att)
	}
}

// Stop ends the runtime loop, the heartbeat, and every channel goroutine. It
// is idempotent and returns once the loop goroutines have exited or the
// bounded join wait elapsed (design D1's bounded-teardown rule). Safe to call
// before Attach and before the gateway ever runs.
func (s *Service) Stop() {
	if att := s.swapAttachment(nil); att != nil {
		att.detach()
		att.stop()
	}
	s.rootStop()
}

// swapAttachment installs next as the service's gateway attachment and
// returns the previous one (nil when there was none).
func (s *Service) swapAttachment(next *attachment) *attachment {
	s.loopMu.Lock()
	defer s.loopMu.Unlock()
	prev := s.att
	s.att = next
	return prev
}

// onGatewayState is the gateway listener: the runtime polls while the gateway
// accepts or holds triggers and stops when the gateway goes down; Paused
// flips the hold gate (admitted updates queue instead of dispatching) and a
// return to Running drains the hold queue in order (design D9).
func (s *Service) onGatewayState(att *attachment, to gateway.State) {
	switch to {
	case gateway.StateRunning:
		s.mu.Lock()
		wasHolding := s.holding
		s.holding = false
		held := s.hold
		s.hold = nil
		s.mu.Unlock()
		s.startLoop(att)
		if wasHolding {
			go s.dispatchHeld(held)
		}
	case gateway.StatePaused:
		s.mu.Lock()
		s.holding = true
		s.mu.Unlock()
	case gateway.StateStopped:
		att.stop()
	}
}

// startLoop starts the channel runtime (poll loop + heartbeat) unless one is
// already running. The gateway check at Attach and the listener can race; the
// mutex plus the cancel sentinel keep exactly one runtime alive. A terminal
// failure (409/401/404) refuses restarts until the next Attach.
func (s *Service) startLoop(att *attachment) {
	att.mu.Lock()
	if att.cancel != nil || att.dead {
		att.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	att.cancel = cancel
	att.wg.Add(1)
	att.mu.Unlock()

	go func() {
		defer att.wg.Done()
		s.runLoop(ctx, att)
	}()
}

// stop cancels the runtime and waits — bounded by StopJoinWait — for its
// goroutines to exit. Safe to call repeatedly and before any loop started. It
// never holds mu while waiting, so a gateway transition listener may call it
// while the loop goroutine is mid-poll.
func (a *attachment) stop() {
	a.mu.Lock()
	cancel := a.cancel
	a.cancel = nil
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	done := make(chan struct{})
	go func() {
		a.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(StopJoinWait):
		slog.Warn("telegram: teardown join timed out; a hung API call outlived Stop")
	}
}

// detach cancels the gateway subscription, leaving the runtime untouched.
func (a *attachment) detach() {
	a.mu.Lock()
	sub := a.sub
	a.sub = nil
	a.mu.Unlock()
	if sub != nil {
		sub()
	}
}

// dead marks the attachment terminally failed: no further restarts until the
// next Attach (design D10: no retry fight).
func (a *attachment) markDead() {
	a.mu.Lock()
	a.dead = true
	cancel := a.cancel
	a.cancel = nil
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// runLoop is one channel runtime: re-read the channel row (design D3), prove
// the token with GetMe, run the once-per-process cold-boot sync (design D9),
// then long-poll until cancelled or a terminal failure. Retryable failures
// rebuild with 30s→300s backoff (design D10).
func (s *Service) runLoop(ctx context.Context, att *attachment) {
	client, err := s.connect(ctx)
	if err != nil {
		s.connectFailed(att, err)
		return
	}
	s.setState(s.healthState(), "")

	// Command-menu housekeeping runs off the connect path (design D12): a
	// failure is logged, never blocking the loop.
	go func() {
		if err := client.SetMyCommands(ctx, []botapi.BotCommand{
			{Command: "new", Description: "Start a fresh session for this chat"},
			{Command: "stop", Description: "Stop the running turn"},
			{Command: "status", Description: "Show agent, model, and busy state"},
		}); err != nil {
			slog.Debug("telegram: setMyCommands failed", "error", err)
		}
	}()

	// The heartbeat rides the same ctx (design D10) but not the join
	// WaitGroup: a heartbeat-triggered restart waits on the poll loop, and
	// the heartbeat itself exits promptly when its ctx is cancelled.
	go func() {
		s.heartbeat(ctx, att, client)
	}()

	offset, err := s.coldBootSync(ctx, client)
	if err != nil {
		var apiErr *botapi.Error
		if errors.As(err, &apiErr) && terminalKind(apiErr.Kind) {
			s.setState(StateError, apiErr.Description)
			att.markDead()
			return
		}
		// A transient sync failure only means the first poll carries the
		// backlog; the loop proceeds.
		slog.Warn("telegram: cold-boot sync failed; first poll will carry the backlog", "error", err)
	}

	s.pollLoop(ctx, att, client, offset)
}

// connectFailed maps a connect failure onto its terminal state (design D10).
// A disabled or not-yet-configured row is idle, not dead: the attachment
// stays startable so a later config change can begin polling.
func (s *Service) connectFailed(att *attachment, err error) {
	var apiErr *botapi.Error
	switch {
	case errors.Is(err, errChannelDisabled):
		s.setState(StateDisabled, "channel not configured or disabled")
		att.markIdle()
	case errors.As(err, &apiErr) && terminalKind(apiErr.Kind):
		s.setState(StateError, apiErr.Description)
		att.markDead()
	default:
		s.setState(StateError, err.Error())
		att.markDead()
	}
}

// markIdle records a recoverable connect failure (disabled or not-yet-
// configured row): the loop goroutine has exited on its own, so the cancel
// sentinel must be cleared for a later start to be possible — otherwise the
// stale sentinel permanently blocks every startLoop.
func (a *attachment) markIdle() {
	a.mu.Lock()
	a.cancel = nil
	a.mu.Unlock()
}

// connect reads the channel row, builds the client, and proves the token with
// GetMe — the send-path liveness proof and the username the group-mention
// gates and update receipts need. A row that cannot poll yet (absent,
// disabled, or tokenless — the fresh-install and pre-save states) reads as
// errChannelDisabled: idle, recoverable, never terminal.
func (s *Service) connect(ctx context.Context) (BotAPI, error) {
	row, err := s.chans.GetChannel(ctx, s.channelID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, errChannelDisabled
		}
		return nil, wrapStore("read channel", err)
	}
	if !row.Enabled || row.BotToken == "" {
		return nil, errChannelDisabled
	}
	s.mu.Lock()
	client := s.ensureClientLocked(row)
	s.mu.Unlock()
	if client == nil {
		return nil, errChannelDisabled
	}
	me, err := client.GetMe(ctx)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.me = me
	s.mu.Unlock()
	return client, nil
}

// pollLoop long-polls getUpdates, dispatches batches, and advances the
// offset only after a batch is admitted (deduped, receipted, dispatched or
// held — design D9's ack-after-processing). offset is the cold-boot sync's
// cursor.
func (s *Service) pollLoop(ctx context.Context, att *attachment, client BotAPI, offset int64) {
	backoff := s.t.backoffMin

	for {
		updates, err := client.GetUpdates(ctx, botapi.GetUpdatesParams{
			Offset:         offset,
			TimeoutSeconds: s.t.pollTimeoutSecs,
		})
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			var apiErr *botapi.Error
			if errors.As(err, &apiErr) && terminalKind(apiErr.Kind) {
				// 409: another consumer owns the token (design D10) —
				// stop, surface the error state, no retry fight. 401/404:
				// the token is dead.
				s.setState(StateError, apiErr.Description)
				att.markDead()
				return
			}
			s.rebuilds++
			s.setState(s.healthState(), err.Error())
			slog.Warn("telegram: getUpdates failed; rebuilding", "error", err, "backoff", backoff)
			if !sleepCtx(ctx, backoff) {
				return
			}
			backoff = min(backoff*2, s.t.backoffMax)
			continue
		}

		backoff = s.t.backoffMin
		s.mu.Lock()
		s.lastPoll = time.Now()
		s.mu.Unlock()

		if len(updates) > 0 {
			s.handleBatch(ctx, client, updates)
			offset = updates[len(updates)-1].ID + 1
		}
	}
}

// coldBootSync runs the process's first poll (design D9): a short poll with
// no offset confirms whatever backlog accumulated while the process was down
// and drops it — only updates that arrive after the process started are
// dispatched. Runs exactly once per process; in-process reconnects never
// skip unheld work. The error (when non-nil) is terminal-classified by the
// caller; a transient sync failure is survivable.
func (s *Service) coldBootSync(ctx context.Context, client BotAPI) (int64, error) {
	var offset int64
	var err error
	s.coldBoot.Do(func() {
		var updates []botapi.Update
		updates, err = client.GetUpdates(ctx, botapi.GetUpdatesParams{})
		if err != nil {
			return
		}
		if len(updates) > 0 {
			offset = updates[len(updates)-1].ID + 1
			slog.Info("telegram: cold-boot backlog acked-and-dropped", "count", len(updates))
		}
	})
	return offset, err
}

// heartbeat is the 90s liveness probe (design D10): GetMe proves the send
// path, getWebhookInfo's pending count the poll path, and the stall timer the
// round-trip itself (a wedged long poll reports healthy while deaf). A
// confirmed failure force-restarts the runtime, which resumes under the poll
// loop's own backoff.
func (s *Service) heartbeat(ctx context.Context, att *attachment, client BotAPI) {
	ticker := time.NewTicker(s.t.heartbeatEvery)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		att.mu.Lock()
		dead := att.dead
		att.mu.Unlock()
		if dead {
			return
		}

		stalled, err := s.probe(ctx, client)
		if err == nil {
			s.mu.Lock()
			s.healthy++
			if s.healthy >= DegradedAfterRebuilds {
				s.rebuilds = 0
				s.healthy = 0
			}
			state := s.healthStateLocked()
			s.mu.Unlock()
			s.setState(state, "")
			continue
		}

		s.rebuilds++
		s.setState(s.healthState(), err.Error())
		if stalled {
			slog.Warn("telegram: heartbeat confirms a stalled or failed path; restarting runtime", "error", err)
			s.restartRuntime(ctx, att)
			return // the restarted runtime owns the next heartbeat
		}
	}
}

// probe runs one liveness pass: GetMe plus getWebhookInfo, and the stall
// check on the last successful getUpdates round-trip. stalled reports a
// failure that warrants a runtime restart (any probe failure or a wedged
// poll), as opposed to merely counting a rebuild.
func (s *Service) probe(ctx context.Context, client BotAPI) (stalled bool, err error) {
	s.mu.Lock()
	last := s.lastPoll
	s.mu.Unlock()
	if !last.IsZero() && time.Since(last) > s.t.stallAfter {
		return true, errors.New("no successful getUpdates round-trip within the stall window")
	}
	if _, err := client.GetMe(ctx); err != nil {
		return true, err
	}
	if _, err := client.GetWebhookInfo(ctx); err != nil {
		return true, err
	}
	return false, nil
}

// restartRuntime force-stops the current runtime and starts a fresh one. The
// cancelled long poll aborts with its context, so the bounded join completes.
func (s *Service) restartRuntime(ctx context.Context, att *attachment) {
	att.mu.Lock()
	dead := att.dead
	att.mu.Unlock()
	if dead {
		return
	}
	att.stop()
	s.startLoop(att)
}

// ConfigChanged re-evaluates the runtime after the channel row changed (a
// saved token, a proxy edit, or the enabled toggle — design D3: the loop only
// re-reads the row at (re)start, so an operator save must trigger one). A live
// loop is restarted so the new settings take effect immediately; an idle one
// (booted before the first save) starts when the gateway is Running. An
// explicit save also clears a terminal failure: design D10's no-retry-fight
// rule governs automatic restarts, not operator action — pasting a corrected
// token after a 401 must recover without an app restart. The cold-boot
// sync stays consumed: an in-process restart never re-drops backlog.
func (s *Service) ConfigChanged() {
	s.loopMu.Lock()
	att := s.att
	s.loopMu.Unlock()
	if att == nil {
		return
	}

	att.mu.Lock()
	stopped := att.gw != nil && att.gw.State() == gateway.StateStopped
	att.mu.Unlock()
	att.stop()
	if stopped {
		return
	}
	att.mu.Lock()
	att.dead = false
	att.mu.Unlock()
	s.startLoop(att)
}

// terminalKind reports whether a Bot API failure ends the runtime instead of
// rebuilding: 409 (another consumer owns the token) and 401/404 (dead token).
func terminalKind(k botapi.ErrorKind) bool {
	return k == botapi.KindConflict || k == botapi.KindAuthInvalid || k == botapi.KindNotFound
}

// healthState maps the rebuild counter onto the connection vocabulary
// (design D10): connected until repeated rebuilds read degraded.
func (s *Service) healthState() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.healthStateLocked()
}

func (s *Service) healthStateLocked() string {
	if s.rebuilds >= DegradedAfterRebuilds {
		return StateDegraded
	}
	return StateConnected
}

// wrapStore annotates a store failure with its origin.
func wrapStore(what string, err error) error {
	if err == nil {
		return nil
	}
	return &storeError{what: what, err: err}
}

type storeError struct {
	what string
	err  error
}

func (e *storeError) Error() string { return "telegram: " + e.what + ": " + e.err.Error() }
func (e *storeError) Unwrap() error { return e.err }

// sleepCtx sleeps d or returns false when ctx ends first.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
