// Package scheduler runs workspace schedulers (integrate-scheduler): a
// claim loop fires due standing orders into per-run agent sessions, drains
// each run's event tap, records the outcome, and delivers channel-targeted
// results through the channel chokepoint. Claim semantics live in the store
// (SKIP LOCKED, claim-time rescheduling, missed-grace archiving — design
// D3); this package owns the tick, the fire path (D4/D5), and delivery
// (D8).
package scheduler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/oniharnantyo/onclaw/internal/agents"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// RunSubmitter mints agent runs: *agents.Runner satisfies it. Consumer-side
// narrow interface, mirroring channels.RunSubmitter. The submit is
// asynchronous: a returned error is immediate (validation or
// config-resolution failures — the runner's load/resolve path runs before
// any model call), while the run's outcome arrives through the returned
// event stream.
type RunSubmitter interface {
	Run(ctx context.Context, req agents.ExecRequest) (*agents.EventStream, error)
}

// ChannelPoster posts a message into a channel as an agent author:
// *channels.Chokepoint satisfies it via PostFromAgent. Scheduler channel
// delivery rides the normal pipeline, so mention parsing can legitimately
// summon other agents (design D8).
type ChannelPoster interface {
	PostFromAgent(ctx context.Context, workspaceID, channelID, agentID, body string) (domain.ChannelMessage, error)
}

// ErrNotFound marks a scheduler id absent from the addressed workspace. It
// wraps domain.ErrNotFound so generic handler error mapping keeps working.
var ErrNotFound = fmt.Errorf("scheduler: %w", domain.ErrNotFound)

// noReplyToken is the unattended-contract suppression token. It suppresses
// channel delivery only when the run's ENTIRE final reply equals it
// case-insensitively — "no reply!" or "NO_REPLY now" are honest prose and
// post normally (design D8).
const noReplyToken = "NO_REPLY"

// drainGrace extends every fire context past the run timeout so the
// terminal store writes and the channel post are not cut off by the same
// deadline that ends the run; Stop's wait deadline uses the same bound.
const drainGrace = time.Minute

// House defaults (integrate-scheduler design D3).
const (
	defaultTick        = 15 * time.Second
	defaultRunTimeout  = 10 * time.Minute
	defaultClaimLimit  = 10
	defaultConcurrency = 4
)

// Service runs due schedulers: a ticker claims due rows from the store,
// fires each through the runner, drains the event tap, and records the
// outcome. The zero value is not usable; construct with NewService.
type Service struct {
	schedulers store.SchedulerStore
	users      store.UserStore
	agents     store.AgentStore
	submitter  RunSubmitter
	poster     ChannelPoster
	log        *slog.Logger

	tick        time.Duration
	runTimeout  time.Duration
	claimLimit  int
	concurrency int
	now         func() time.Time

	mu       sync.Mutex
	inFlight map[string]struct{}

	sem      chan struct{}
	stopCh   chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

// Option tunes a defaultable Service knob; invalid values keep the default.
type Option func(*Service)

// WithTick sets the claim-loop cadence (ONCLAW_SCHEDULER_TICK). Minute-
// granularity cron makes 15s latency invisible; faster ticks buy nothing.
func WithTick(d time.Duration) Option {
	return func(s *Service) {
		if d > 0 {
			s.tick = d
		}
	}
}

// WithRunTimeout sets the per-run wall-clock budget
// (ONCLAW_SCHEDULER_RUN_TIMEOUT). After it elapses the run's event tap is
// cancelled and the run records as cancelled.
func WithRunTimeout(d time.Duration) Option {
	return func(s *Service) {
		if d > 0 {
			s.runTimeout = d
		}
	}
}

// WithClaimLimit caps how many schedulers one tick claims.
func WithClaimLimit(n int) Option {
	return func(s *Service) {
		if n > 0 {
			s.claimLimit = n
		}
	}
}

// WithConcurrency caps how many scheduler fires execute concurrently. Once
// a row is claimed its next_run_at is already advanced, so waiting on the
// semaphore is safe — the occurrence cannot be claimed again.
func WithConcurrency(n int) Option {
	return func(s *Service) {
		if n > 0 {
			s.concurrency = n
		}
	}
}

// WithNow injects the clock (tests anchor due-math and session ids).
func WithNow(now func() time.Time) Option {
	return func(s *Service) {
		if now != nil {
			s.now = now
		}
	}
}

// NewService constructs the scheduler service. Dependencies are granular
// sub-interfaces, resolved non-nil by the composition root; the workspace
// store is deliberately absent — ExecRequest carries only the workspace id
// and the runner's load path resolves the workspace itself, so a vanished
// workspace surfaces as a synchronous submit error (a cheap blocked run).
func NewService(
	schedulers store.SchedulerStore,
	users store.UserStore,
	agents store.AgentStore,
	submitter RunSubmitter,
	poster ChannelPoster,
	log *slog.Logger,
	opts ...Option,
) *Service {
	s := &Service{
		schedulers:  schedulers,
		users:       users,
		agents:      agents,
		submitter:   submitter,
		poster:      poster,
		log:         log,
		tick:        defaultTick,
		runTimeout:  defaultRunTimeout,
		claimLimit:  defaultClaimLimit,
		concurrency: defaultConcurrency,
		now:         time.Now,
		inFlight:    make(map[string]struct{}),
		stopCh:      make(chan struct{}),
	}
	for _, opt := range opts {
		opt(s)
	}
	s.sem = make(chan struct{}, s.concurrency)
	return s
}

// Start launches the ticker loop. The loop exits when ctx is cancelled or
// Stop is called; in-flight fires are not interrupted — Stop waits for them.
func (s *Service) Start(ctx context.Context) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(s.tick)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-s.stopCh:
				return
			case <-ticker.C:
				s.tickOnce(ctx)
			}
		}
	}()
}

// Stop halts the ticker and waits for in-flight fires to reach their
// terminal outcome. Every fire is bounded by the run timeout plus the drain
// grace, which is the wait deadline. Idempotent.
func (s *Service) Stop() {
	s.stopOnce.Do(func() { close(s.stopCh) })
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	timer := time.NewTimer(s.runTimeout + drainGrace)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		s.log.Warn("scheduler: stop deadline exceeded with fires still in flight")
	}
}

// RunNow dispatches a scheduler immediately (design D9): it works regardless
// of the enabled flag (and never re-enables a paused scheduler), never
// touches next_run_at, and records trigger manual through the same fire path
// as scheduled claims. An in-flight run conflicts (the handler maps the
// wrapped domain.ErrConflict to 409); an unknown scheduler returns
// ErrNotFound. The returned row is the running record — or the already-
// finished row when preflight or submit blocked cheaply.
func (s *Service) RunNow(ctx context.Context, workspaceID, schedulerID string) (*domain.SchedulerRun, error) {
	sched, err := s.schedulers.GetScheduler(ctx, workspaceID, schedulerID)
	if err != nil {
		return nil, err
	}
	if sched == nil {
		return nil, ErrNotFound
	}
	if !s.markInFlight(sched.ID) {
		return nil, fmt.Errorf("scheduler %s run still in flight: %w", sched.ID, domain.ErrConflict)
	}
	run, stream := s.begin(ctx, sched, domain.SchedulerTriggerManual)
	if stream == nil {
		// Preflight or submit already finished the run cheaply.
		s.unmarkInFlight(sched.ID)
		return run, nil
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer s.unmarkInFlight(sched.ID)
		fireCtx, cancel := s.fireContext(ctx)
		defer cancel()
		s.drainRun(fireCtx, sched, run, stream)
	}()
	return run, nil
}

// tickOnce runs one claim-dispatch pass: due schedulers are claimed (their
// next occurrence already advanced inside the claim), each live claim gets a
// semaphore-gated fire goroutine, and missed claims are recorded without
// executing. Also called directly by tests.
func (s *Service) tickOnce(ctx context.Context) {
	claims, err := s.schedulers.ClaimDueSchedulers(ctx, s.now(), s.claimLimit)
	if err != nil {
		s.log.WarnContext(ctx, "scheduler: claim due schedulers failed", "error", err)
		return
	}
	for _, claim := range claims {
		if claim.Missed {
			s.recordMissed(ctx, claim.Scheduler)
			continue
		}
		sched := claim.Scheduler
		if !s.markInFlight(sched.ID) {
			// The previous run is still executing: the occurrence was
			// claimed (next_run_at advanced), so skipping loses only this
			// firing — the next occurrence carries on.
			s.log.WarnContext(ctx, "scheduler: claimed fire skipped, run still in flight", "scheduler_id", sched.ID)
			continue
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.sem <- struct{}{}
			defer func() { <-s.sem }()
			defer s.unmarkInFlight(sched.ID)
			fireCtx, cancel := s.fireContext(ctx)
			defer cancel()
			run, stream := s.begin(fireCtx, sched, domain.SchedulerTriggerScheduled)
			if stream == nil {
				return
			}
			s.drainRun(fireCtx, sched, run, stream)
		}()
	}
}

// fireContext derives a fire's context: detached from the caller (a server
// shutdown or an HTTP request returning must not kill a run's tail work —
// the channels fanout's reasoning) and bounded by the run timeout plus the
// drain grace so Stop's wait converges.
func (s *Service) fireContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), s.runTimeout+drainGrace)
}

// recordMissed writes the outcome for a once scheduler the claim archived
// beyond the grace window: nothing executes, but the runs listing shows why
// (status missed, trigger scheduler, no tokens, no duration).
func (s *Service) recordMissed(ctx context.Context, sched *domain.Scheduler) {
	run := &domain.SchedulerRun{
		WorkspaceID: sched.WorkspaceID,
		SchedulerID: sched.ID,
		SessionID:   missedSessionID(sched.ID),
		Trigger:     domain.SchedulerTriggerScheduled,
		StartedAt:   s.now().UTC(),
	}
	if err := s.schedulers.StartSchedulerRun(ctx, run); err != nil {
		s.log.WarnContext(ctx, "scheduler: record missed run failed", "scheduler_id", sched.ID, "error", err)
		return
	}
	s.finishRun(ctx, run, domain.SchedulerRunStatusMissed, 0, 0, "", "", "")
}

// begin performs the synchronous half of a fire: the running run row, the
// preflight (D5), and the submit. It returns the run row and, when the run
// is live, its event stream. A nil stream means the run already reached a
// terminal outcome (preflight or submit failure) and the row is finished —
// no drain is owed. StartSchedulerRun failures (e.g. the scheduler was
// deleted between claim and fire) log and abandon: the claim already
// consumed the occurrence.
func (s *Service) begin(ctx context.Context, sched *domain.Scheduler, trigger string) (*domain.SchedulerRun, *agents.EventStream) {
	started := s.now().UTC()
	run := &domain.SchedulerRun{
		WorkspaceID: sched.WorkspaceID,
		SchedulerID: sched.ID,
		SessionID:   runSessionID(sched.ID, started),
		Trigger:     trigger,
		StartedAt:   started,
	}
	if err := s.schedulers.StartSchedulerRun(ctx, run); err != nil {
		s.log.WarnContext(ctx, "scheduler: start run failed", "scheduler_id", sched.ID, "error", err)
		return nil, nil
	}

	pf := s.preflight(ctx, sched)
	if pf.status != "" {
		if pf.autoPause {
			s.pause(ctx, sched)
		}
		s.finishRun(ctx, run, pf.status, 0, 0, "", pf.errMsg, "")
		return run, nil
	}

	// The unattended contract learns the suppression token only for
	// channel-target runs; thread-target runs ask for plain prose instead
	// (design D8).
	noReply := ""
	if sched.Delivery.Type == domain.SchedulerDeliveryChannel {
		noReply = noReplyToken
	}
	stream, err := s.submitter.Run(ctx, agents.ExecRequest{
		WorkspaceID:      sched.WorkspaceID,
		AgentID:          sched.AgentID,
		SessionID:        run.SessionID,
		UserID:           pf.creatorID,
		Origin:           agents.OriginScheduler,
		Input:            sched.Prompt,
		SchedulerNoReply: noReply,
		// The exported trace is named after the schedule, not the prompt
		// (integrate-langfuse-tracing D2). Observational only: it rides the
		// run's trace context and nothing else.
		ScheduleName: sched.Name,
	})
	if err != nil {
		// Synchronous submit failures are pre-model by the runner's
		// contract — zero token spend. They are config grief, not creator
		// grief: the scheduler is NOT paused.
		s.finishRun(ctx, run, domain.SchedulerRunStatusBlocked, 0, 0, "", fmt.Sprintf("submit failed: %v", err), "")
		return run, nil
	}
	return run, stream
}

// preflightResult carries the fire-path preflight outcome: creatorID when
// clear; otherwise the terminal status to record, why, and whether the
// scheduler itself must be paused (creator grief only — design D5).
type preflightResult struct {
	creatorID string
	status    string // "" when clear; blocked|failed otherwise
	errMsg    string
	autoPause bool
}

// preflight resolves the acting identity and the bound agent before any
// model call (spec: preflight before token spend). A missing or disabled
// creator blocks AND auto-pauses (D5: the scheduler dies visibly with its
// creator's account); a missing bound agent blocks without pausing — the
// binding may be repaired; infrastructure failures record failed.
func (s *Service) preflight(ctx context.Context, sched *domain.Scheduler) preflightResult {
	if sched.CreatedBy == nil || *sched.CreatedBy == "" {
		return preflightResult{
			status:    domain.SchedulerRunStatusBlocked,
			errMsg:    "creator missing (deleted)",
			autoPause: true,
		}
	}
	u, err := s.users.ByID(ctx, *sched.CreatedBy)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return preflightResult{
				status:    domain.SchedulerRunStatusBlocked,
				errMsg:    fmt.Sprintf("creator %s not found", *sched.CreatedBy),
				autoPause: true,
			}
		}
		return preflightResult{status: domain.SchedulerRunStatusFailed, errMsg: fmt.Sprintf("resolve creator: %v", err)}
	}
	if u.IsDisabled() {
		return preflightResult{
			status:    domain.SchedulerRunStatusBlocked,
			errMsg:    "creator disabled",
			autoPause: true,
		}
	}
	if _, err := s.agents.ByID(ctx, sched.WorkspaceID, sched.AgentID); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return preflightResult{
				status: domain.SchedulerRunStatusBlocked,
				errMsg: fmt.Sprintf("agent %s not found in workspace", sched.AgentID),
			}
		}
		return preflightResult{status: domain.SchedulerRunStatusFailed, errMsg: fmt.Sprintf("resolve agent: %v", err)}
	}
	return preflightResult{creatorID: u.ID}
}

// pause auto-pauses the scheduler after creator grief (D5): enabled=false,
// with next_run_at recomputed to nil by the store's update path. A failure
// is logged, never fatal — the blocked run record stands on its own.
func (s *Service) pause(ctx context.Context, sched *domain.Scheduler) {
	paused := *sched
	paused.Enabled = false
	if err := s.schedulers.UpdateScheduler(ctx, sched.WorkspaceID, &paused); err != nil {
		s.log.WarnContext(ctx, "scheduler: auto-pause failed", "scheduler_id", sched.ID, "error", err)
	}
}

// drainRun consumes one run's event tap to EOF, then finishes the run row
// and performs channel delivery. It mirrors the channels fanout drain: tool
// calls are tallied, the final assistant text is captured, and the
// terminal event kind maps onto the run status (completed by default;
// failed on error; cancelled on cancellation).
func (s *Service) drainRun(ctx context.Context, sched *domain.Scheduler, run *domain.SchedulerRun, stream *agents.EventStream) {
	start := time.Now()

	// Wall-clock budget (design D3): after runTimeout the tap is cancelled —
	// buffered events still drain to EOF — so a wedged run can never pin a
	// fire goroutine (and a Stop wait) forever.
	var timedOut atomic.Bool
	watchdog := time.AfterFunc(s.runTimeout, func() {
		timedOut.Store(true)
		stream.Cancel()
	})
	defer watchdog.Stop()

	toolCounts := make(map[string]int)
	finalText := ""
	tokens := 0
	status := ""
	errMsg := ""
	// traceID rides the runner's terminal events (integrate-langfuse-tracing
	// D3): the pinned trace id, present only when the turn sampled in for
	// export — a sampled-out run persists no id, keeping the runs view
	// consistent with what Langfuse actually holds.
	traceID := ""
	for {
		ev, err := stream.Recv()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				s.log.WarnContext(ctx, "scheduler: run tap receive failed",
					"scheduler_id", sched.ID, "run_id", run.ID, "error", err)
			}
			break
		}
		if ev == nil {
			continue
		}
		if ev.Usage != nil {
			tokens = ev.Usage.TotalTokens // provider usage accumulates over the run; the last frame is the total
		}
		if ev.TraceID != "" {
			traceID = ev.TraceID
		}
		switch ev.Kind {
		case agents.TranscriptEventToolCallFinished:
			if ev.ToolResult != nil && ev.ToolResult.Name != "" {
				toolCounts[ev.ToolResult.Name]++
			}
		case agents.TranscriptEventMessageCompleted:
			if ev.Message != nil && ev.Message.Role == "assistant" &&
				len(ev.Message.ToolCalls) == 0 && strings.TrimSpace(ev.Message.Content) != "" {
				finalText = ev.Message.Content
			}
		case agents.TranscriptEventTurnCompleted:
			status = domain.SchedulerRunStatusCompleted
		case agents.TranscriptEventError:
			status = domain.SchedulerRunStatusFailed
			errMsg = ev.Error
		case agents.TranscriptEventCancelled:
			status = domain.SchedulerRunStatusCancelled
			errMsg = ev.CancelReason
		}
	}

	if status == "" {
		// The tap ended without a terminal event. A fired watchdog means the
		// wall-clock budget killed the view; otherwise the stream ended
		// early without an error (approval interrupts cannot reach
		// unattended runs) and success-by-default matches the fanout.
		status = domain.SchedulerRunStatusCompleted
		if timedOut.Load() {
			status = domain.SchedulerRunStatusCancelled
			errMsg = fmt.Sprintf("run timed out after %s", s.runTimeout)
		}
	}

	deliveryStatus := ""
	if status == domain.SchedulerRunStatusCompleted &&
		sched.Delivery.Type == domain.SchedulerDeliveryChannel &&
		strings.TrimSpace(finalText) != "" {
		if strings.EqualFold(strings.TrimSpace(finalText), noReplyToken) {
			// Whole-reply match only: the agent said there is nothing to
			// report, so the channel hears nothing (design D8).
			deliveryStatus = domain.SchedulerDeliverySuppressed
		} else if _, err := s.poster.PostFromAgent(ctx, sched.WorkspaceID, sched.Delivery.ChannelID, sched.AgentID, finalText); err != nil {
			// Delivery failure is never a run failure (design D8): the run
			// stays completed and the result remains readable in the
			// run's own transcript.
			deliveryStatus = domain.SchedulerDeliveryFailed
			errMsg = fmt.Sprintf("channel delivery failed: %v", err)
		} else {
			deliveryStatus = domain.SchedulerDeliveryDelivered
		}
	}

	s.finishRun(ctx, run, status, time.Since(start).Milliseconds(), tokens, deliveryStatus, errMsg, traceID)
	s.log.DebugContext(ctx, "scheduler: run finished",
		"scheduler_id", sched.ID, "run_id", run.ID, "status", status,
		"tokens_used", tokens, "tool_calls", toolCounts)
}

// finishRun writes the run outcome — including the turn's persisted trace id
// (integrate-langfuse-tracing D3) — through the store (which mirrors it into
// the scheduler's last_run atomically) and keeps the local row in sync for
// the RunNow caller.
func (s *Service) finishRun(ctx context.Context, run *domain.SchedulerRun, status string, durationMS int64, tokensUsed int, deliveryStatus string, errMsg string, traceID string) {
	if err := s.schedulers.FinishSchedulerRun(ctx, run.WorkspaceID, run.ID, status, durationMS, tokensUsed, deliveryStatus, errMsg, traceID); err != nil {
		s.log.WarnContext(ctx, "scheduler: finish run failed", "run_id", run.ID, "status", status, "error", err)
		return
	}
	run.Status = status
	run.DurationMS = durationMS
	run.TokensUsed = tokensUsed
	run.DeliveryStatus = deliveryStatus
	run.Error = errMsg
	run.TraceID = traceID
}

// markInFlight registers a scheduler as firing; false means a run is already
// live (the loop skips; RunNow conflicts — design D9).
func (s *Service) markInFlight(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, live := s.inFlight[id]; live {
		return false
	}
	s.inFlight[id] = struct{}{}
	return true
}

func (s *Service) unmarkInFlight(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.inFlight, id)
}

// runSessionID binds the per-run session id shape the web and the runner
// tests already assert: sched_<schedulerID>_<unix seconds UTC>. Each run is
// its own session (design D7), so the runner's one-run-per-session guard
// never collides across schedulers, and the runs listing links transcripts
// by the sched_ prefix.
func runSessionID(schedulerID string, now time.Time) string {
	return fmt.Sprintf("sched_%s_%d", schedulerID, now.UTC().Unix())
}

// missedSessionID is the transcript link of a missed run. The store ports
// require a non-empty session id on every run row, but no session exists —
// nothing executed — so the reserved shape below is used; resolving it as a
// transcript yields an empty view rather than another run's events.
func missedSessionID(schedulerID string) string {
	return "sched_" + schedulerID + "_missed"
}
