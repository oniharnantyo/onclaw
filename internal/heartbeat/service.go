// Package heartbeat runs agent heartbeats (add-agent-heartbeat): a claim
// loop fires due ticks into each agent's persistent shared session (D2),
// guards every fire before any model call (empty checklist D3, active hours
// D5, busy agent D12), resolves the creator at fire time (D6), drains the
// event tap under the NO_REPLY silence contract (D7), delivers reports to
// the creator's paired gateway DMs or an explicit channel (D8), and applies
// failure-streak accounting with auto-pause (D12). Claim semantics live in
// the store (SKIP LOCKED, claim-time next_tick_at advance — D14): a due
// heartbeats' next tick is already scheduled when the fire starts, so
// skipped ticks advance the cadence normally by construction. This package
// owns the tick, the fire path, delivery, and the workspace-activity digest
// (D9); it mirrors internal/scheduler with the deltas the design calls out.
package heartbeat

import (
	"context"
	"encoding/json"
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
	"github.com/oniharnantyo/onclaw/internal/gateways"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// RunSubmitter mints agent runs: *agents.Runner satisfies it. Consumer-side
// narrow interface, mirroring scheduler.RunSubmitter. The submit is
// asynchronous: a returned error is immediate (validation or
// config-resolution failures — the runner's load/resolve path runs before
// any model call), while the run's outcome arrives through the returned
// event stream.
type RunSubmitter interface {
	Run(ctx context.Context, req agents.ExecRequest) (*agents.EventStream, error)
}

// AgentBusyChecker reports whether an agent has ANY run in flight, any
// session: *agents.Runner satisfies it via AgentBusy. Kept as an interface
// so the busy guard is testable without a full runner (add-agent-heartbeat
// D12 — the busy defer is agent-level, so a live user chat blocks the
// ambient tick).
type AgentBusyChecker interface {
	AgentBusy(workspaceID, agentID string) bool
}

// ChannelPoster posts a message into a channel as an agent author:
// *channels.Chokepoint satisfies it via PostFromAgent. Heartbeat channel
// delivery rides the same chokepoint as scheduler channel delivery (design
// D8), so mention parsing can legitimately summon other agents.
type ChannelPoster interface {
	PostFromAgent(ctx context.Context, workspaceID, channelID, agentID, body string) (domain.ChannelMessage, error)
}

// ErrNotFound marks a workspace agent without a heartbeat. It wraps
// domain.ErrNotFound so generic handler error mapping keeps working.
var ErrNotFound = fmt.Errorf("heartbeat: %w", domain.ErrNotFound)

// noReplyToken is the unattended-contract suppression token (design D7).
// A tick is silent only when its ENTIRE final reply equals it
// case-insensitively after trimming — "no reply needed, but note: disk 90%
// full" is a report and delivers.
const noReplyToken = "NO_REPLY"

// heartbeatTurnPrompt is the fixed per-tick user turn. The checklist and the
// activity digest compose into the instruction (never the input — the
// runner's heartbeat profile owns those sections, D3/D9), so the input is
// just the tick's marching order.
const heartbeatTurnPrompt = "Heartbeat tick — review your HEARTBEAT checklist and the workspace activity below; report only what needs attention, otherwise reply NO_REPLY."

// Skip reasons recorded on skipped run rows (add-agent-heartbeat D3/D5/D12).
const (
	skipReasonEmptyChecklist = "empty-heartbeat"
	skipReasonOutsideHours   = "outside-active-hours"
	skipReasonBusy           = "busy"
)

// drainGrace extends every fire context past the run timeout so the
// terminal store writes and the delivery calls are not cut off by the same
// deadline that ends the run; Stop's wait deadline uses the same bound.
const drainGrace = time.Minute

// House defaults (add-agent-heartbeat D14): ambient work stays light — a
// slower claim loop and half the scheduler's concurrency.
const (
	defaultTick        = 30 * time.Second
	defaultRunTimeout  = 10 * time.Minute
	defaultClaimLimit  = 10
	defaultConcurrency = 2
)

// Service runs due heartbeats: a ticker claims due rows from the store,
// fires each through the runner into the agent's persistent shared session,
// drains the event tap, delivers the report, and records the outcome. The
// zero value is not usable; construct with NewService.
type Service struct {
	heartbeats store.HeartbeatStore
	users      store.UserStore
	agents     store.AgentStore
	workspaces store.WorkspaceStore
	submitter  RunSubmitter
	busy       AgentBusyChecker
	poster     ChannelPoster
	gateways   store.GatewayStore
	links      store.GatewayLinks
	outbox     store.GatewayOutbox
	digest     *DigestComposer
	log        *slog.Logger

	tick        time.Duration
	runTimeout  time.Duration
	claimLimit  int
	concurrency int
	now         func() time.Time

	// inFlight tracks this service's live ticks, keyed workspace|agent —
	// one tick per heartbeat at a time (the heartbeat is 1:1 with its
	// agent, and both share the hb_ session). The ticker skips an already-
	// live claim as busy (D12); RunNow conflicts (409).
	mu       sync.Mutex
	inFlight map[string]struct{}

	sem      chan struct{}
	stopCh   chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

// Option tunes a defaultable Service knob; invalid values keep the default.
type Option func(*Service)

// WithTick sets the claim-loop cadence (ONCLAW_HEARTBEAT_TICK). Heartbeat
// cadences floor at 5 minutes (domain validation), so 30s claim latency is
// invisible; faster ticks buy nothing.
func WithTick(d time.Duration) Option {
	return func(s *Service) {
		if d > 0 {
			s.tick = d
		}
	}
}

// WithRunTimeout sets the per-tick wall-clock budget
// (ONCLAW_HEARTBEAT_RUN_TIMEOUT). After it elapses the run's event tap is
// cancelled and the tick records as cancelled.
func WithRunTimeout(d time.Duration) Option {
	return func(s *Service) {
		if d > 0 {
			s.runTimeout = d
		}
	}
}

// WithClaimLimit caps how many heartbeats one tick claims.
func WithClaimLimit(n int) Option {
	return func(s *Service) {
		if n > 0 {
			s.claimLimit = n
		}
	}
}

// WithConcurrency caps how many heartbeat fires execute concurrently.
// Ambient work stays light by design (add-agent-heartbeat D14). Once a row
// is claimed its next_tick_at is already advanced, so waiting on the
// semaphore is safe — the occurrence cannot be claimed again.
func WithConcurrency(n int) Option {
	return func(s *Service) {
		if n > 0 {
			s.concurrency = n
		}
	}
}

// WithNow injects the clock (tests anchor due-math, active hours, and
// session ids).
func WithNow(now func() time.Time) Option {
	return func(s *Service) {
		if now != nil {
			s.now = now
		}
	}
}

// NewService constructs the heartbeat service. Dependencies are granular
// ports, resolved non-nil by the composition root. Two deltas from the
// scheduler's constructor are load-bearing:
//
//   - workspaces: the scheduler never needed it because nothing on its fire
//     path interprets wall-clock time after creation — claim-time
//     rescheduling owns all cron math. The heartbeat must evaluate its
//     active-hours window (D5) at fire time in the workspace timezone, so
//     the service resolves the workspace itself.
//   - channels + schedulers: consumed only by the DigestComposer (D9), built
//     here so the composition root passes store sub-accessors exactly as it
//     does for every other service and wires nothing new.
//
// busy is the runner's agent-level liveness (D12); gateways/links/outbox
// serve creator-DM delivery (D8).
func NewService(
	heartbeats store.HeartbeatStore,
	users store.UserStore,
	agents store.AgentStore,
	workspaces store.WorkspaceStore,
	channels store.ChannelStore,
	schedulers store.SchedulerStore,
	submitter RunSubmitter,
	busy AgentBusyChecker,
	poster ChannelPoster,
	gateways store.GatewayStore,
	links store.GatewayLinks,
	outbox store.GatewayOutbox,
	log *slog.Logger,
	opts ...Option,
) *Service {
	s := &Service{
		heartbeats:  heartbeats,
		users:       users,
		agents:      agents,
		workspaces:  workspaces,
		submitter:   submitter,
		busy:        busy,
		poster:      poster,
		gateways:    gateways,
		links:       links,
		outbox:      outbox,
		digest:      NewDigestComposer(channels, schedulers, users, agents, log),
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
		s.log.Warn("heartbeat: stop deadline exceeded with fires still in flight")
	}
}

// RunNow fires a heartbeat immediately (add-agent-heartbeat, spec "Manual
// run-now"): it works regardless of the enabled flag (and never re-enables a
// paused heartbeat), never touches next_tick_at — a manual fire rides the
// shared hb_ session but does not advance the cadence — and records trigger
// manual through the same fire path as claimed ticks, guards included. An
// in-flight tick conflicts (the handler maps the wrapped domain.ErrConflict
// to 409); an agent without a heartbeat returns ErrNotFound. The returned
// row is the running record — or the already-finished row when a guard,
// preflight, or submit finished it cheaply.
func (s *Service) RunNow(ctx context.Context, workspaceID, agentID string) (*domain.HeartbeatRun, error) {
	hb, err := s.heartbeats.GetHeartbeat(ctx, workspaceID, agentID)
	if err != nil {
		return nil, err
	}
	if hb == nil {
		return nil, ErrNotFound
	}
	if !s.markInFlight(workspaceID, agentID) {
		return nil, fmt.Errorf("heartbeat for agent %s tick still in flight: %w", agentID, domain.ErrConflict)
	}
	run, stream, creatorID := s.begin(ctx, hb, domain.HeartbeatTriggerManual)
	if stream == nil {
		// A guard, preflight, or submit already finished the tick cheaply.
		s.unmarkInFlight(workspaceID, agentID)
		return run, nil
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer s.unmarkInFlight(workspaceID, agentID)
		fireCtx, cancel := s.fireContext(ctx)
		defer cancel()
		s.drainRun(fireCtx, hb, run, stream, creatorID)
	}()
	return run, nil
}

// Resume re-enables a paused heartbeat (add-agent-heartbeat D12): enabled
// again, next_tick_at recomputed in the workspace timezone from now, and the
// failure streak reset — the paused banner's recovery path. Also allowed on
// an already-enabled heartbeat: recompute + reset is idempotent-friendly
// (harmless), and the endpoint rides agents.write. An agent without a
// heartbeat returns ErrNotFound.
func (s *Service) Resume(ctx context.Context, workspaceID, agentID string) (*domain.Heartbeat, error) {
	hb, err := s.heartbeats.GetHeartbeat(ctx, workspaceID, agentID)
	if err != nil {
		return nil, err
	}
	if hb == nil {
		return nil, ErrNotFound
	}
	ws, err := s.workspaces.ByID(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace: %w", err)
	}
	next, err := domain.NextRun(hb.Expr, s.now(), workspaceLocation(ws.Timezone))
	if err != nil {
		return nil, err
	}
	hb.Enabled = true
	hb.FailureStreak = 0
	hb.NextTickAt = next
	if err := s.heartbeats.PutHeartbeat(ctx, workspaceID, agentID, hb); err != nil {
		return nil, err
	}
	return hb, nil
}

// tickOnce runs one claim-dispatch pass: due heartbeats are claimed (their
// next tick already advanced inside the claim, so a skipped tick still
// advances the cadence — D14), and each live claim is checked against the
// agent-busy guard before any work. Also called directly by tests.
func (s *Service) tickOnce(ctx context.Context) {
	claims, err := s.heartbeats.ClaimDueHeartbeats(ctx, s.now(), s.claimLimit)
	if err != nil {
		s.log.WarnContext(ctx, "heartbeat: claim due heartbeats failed", "error", err)
		return
	}
	for _, claim := range claims {
		hb := claim.Heartbeat
		// Busy guard, checked first and outside the fire path (D12): a due
		// tick whose agent has any run in flight — a live user chat, or this
		// service's own still-draining previous tick — records a skipped row
		// without queueing behind the semaphore. The claim already advanced
		// next_tick_at, so the cadence carries on; only this firing is lost.
		if s.busy.AgentBusy(hb.WorkspaceID, hb.AgentID) || !s.markInFlight(hb.WorkspaceID, hb.AgentID) {
			s.recordBusySkip(ctx, hb)
			continue
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.sem <- struct{}{}
			defer func() { <-s.sem }()
			defer s.unmarkInFlight(hb.WorkspaceID, hb.AgentID)
			fireCtx, cancel := s.fireContext(ctx)
			defer cancel()
			run, stream, creatorID := s.begin(fireCtx, hb, domain.HeartbeatTriggerTick)
			if stream == nil {
				return
			}
			s.drainRun(fireCtx, hb, run, stream, creatorID)
		}()
	}
}

// fireContext derives a fire's context: detached from the caller (a server
// shutdown must not kill a tick's tail work — the scheduler's reasoning) and
// bounded by the run timeout plus the drain grace so Stop's wait converges.
func (s *Service) fireContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), s.runTimeout+drainGrace)
}

// recordBusySkip writes the outcome for a claim dropped by the busy guard:
// nothing executes, but the runs listing shows why (status skipped, reason
// busy, no tokens, no duration). Start failures (the heartbeat deleted
// between claim and fire) log and abandon — the claim already consumed the
// tick.
func (s *Service) recordBusySkip(ctx context.Context, hb *domain.Heartbeat) {
	run := &domain.HeartbeatRun{
		WorkspaceID: hb.WorkspaceID,
		HeartbeatID: hb.ID,
		AgentID:     hb.AgentID,
		SessionID:   hbSessionID(hb.AgentID),
		Trigger:     domain.HeartbeatTriggerTick,
		StartedAt:   s.now().UTC(),
	}
	if err := s.heartbeats.StartHeartbeatRun(ctx, run); err != nil {
		s.log.WarnContext(ctx, "heartbeat: record busy skip failed", "agent_id", hb.AgentID, "error", err)
		return
	}
	s.finishRun(ctx, run, domain.HeartbeatRunStatusSkipped, 0, 0, "", skipReasonBusy, "")
}

// begin performs the synchronous half of a fire: the running run row, the
// pre-model guards, the preflight (D6), and the submit. It returns the run
// row, the live event stream, and the resolved creator id (for delivery).
// A nil stream means the tick already reached a terminal outcome and the row
// is finished — no drain is owed. StartHeartbeatRun failures (the heartbeat
// deleted between claim and fire) log and abandon.
//
// Guard order is chosen to burn the fewest reads when a tick cannot run:
// the empty-checklist skip needs zero lookups (D3), the active-hours skip
// one workspace read (D5), the busy skip one map scan (D12), and only then
// the preflight's two identity reads (D6). Every guard records its skipped
// row via Start+Finish and returns no stream — zero token spend.
func (s *Service) begin(ctx context.Context, hb *domain.Heartbeat, trigger string) (*domain.HeartbeatRun, *agents.EventStream, string) {
	started := s.now().UTC()
	run := &domain.HeartbeatRun{
		WorkspaceID: hb.WorkspaceID,
		HeartbeatID: hb.ID,
		AgentID:     hb.AgentID,
		SessionID:   hbSessionID(hb.AgentID),
		Trigger:     trigger,
		StartedAt:   started,
	}
	if err := s.heartbeats.StartHeartbeatRun(ctx, run); err != nil {
		s.log.WarnContext(ctx, "heartbeat: start run failed", "agent_id", hb.AgentID, "error", err)
		return nil, nil, ""
	}

	// Guard (a) — empty checklist (D3, OpenClaw steal): nothing to check, so
	// the tick must not reach the model.
	if strings.TrimSpace(hb.Prompt) == "" {
		s.finishRun(ctx, run, domain.HeartbeatRunStatusSkipped, 0, 0, "", skipReasonEmptyChecklist, "")
		return run, nil, ""
	}

	// Guard (b) — active hours in the workspace timezone (D5).
	ws, err := s.workspaces.ByID(ctx, hb.WorkspaceID)
	if err != nil {
		// The workspace anchors every heartbeat evaluation; without it the
		// window cannot be judged. Infrastructure grief, not creator grief:
		// record failed, never pause.
		s.finishRun(ctx, run, domain.HeartbeatRunStatusFailed, 0, 0, "", fmt.Sprintf("resolve workspace: %v", err), "")
		return run, nil, ""
	}
	if !inActiveHours(hb, s.now(), ws.Timezone) {
		s.finishRun(ctx, run, domain.HeartbeatRunStatusSkipped, 0, 0, "", skipReasonOutsideHours, "")
		return run, nil, ""
	}

	// Guard (c) — busy agent (D12). The ticker pre-checks this before begin;
	// the re-check here closes the claim-to-fire race and covers the manual
	// trigger, which has no pre-check.
	if s.busy.AgentBusy(hb.WorkspaceID, hb.AgentID) {
		s.finishRun(ctx, run, domain.HeartbeatRunStatusSkipped, 0, 0, "", skipReasonBusy, "")
		return run, nil, ""
	}

	// Preflight (D6 — scheduler D5 verbatim): creator identity resolved
	// before any model call.
	pf := s.preflight(ctx, hb)
	if pf.status != "" {
		if pf.autoPause {
			s.pause(ctx, hb)
		}
		s.finishRun(ctx, run, pf.status, 0, 0, "", pf.errMsg, "")
		return run, nil, ""
	}

	// Digest (D9): workspace activity since the previous tick — or since
	// creation for the first tick ever. An empty digest is normal (nothing
	// happened); the runner's profile renders its own fallback wording.
	since := hb.CreatedAt
	if hb.LastTick != nil {
		since = hb.LastTick.StartedAt
	}
	digest := s.digest.Compose(ctx, hb.WorkspaceID, since)

	stream, err := s.submitter.Run(ctx, agents.ExecRequest{
		WorkspaceID:        hb.WorkspaceID,
		AgentID:            hb.AgentID,
		SessionID:          run.SessionID,
		UserID:             pf.creatorID,
		Origin:             agents.OriginHeartbeat,
		Input:              heartbeatTurnPrompt,
		HeartbeatChecklist: hb.Prompt,
		HeartbeatDigest:    digest,
	})
	if err != nil {
		// Synchronous submit failures are pre-model by the runner's
		// contract — zero token spend. They are config grief, not creator
		// grief: the heartbeat is NOT paused.
		s.finishRun(ctx, run, domain.HeartbeatRunStatusBlocked, 0, 0, "", fmt.Sprintf("submit failed: %v", err), "")
		return run, nil, ""
	}
	return run, stream, pf.creatorID
}

// preflightResult carries the fire-path preflight outcome: creatorID when
// clear; otherwise the terminal status to record, why, and whether the
// heartbeat itself must be paused (creator grief only — design D6).
type preflightResult struct {
	creatorID string
	status    string // "" when clear; blocked|failed otherwise
	errMsg    string
	autoPause bool
}

// preflight resolves the acting identity and the bound agent before any
// model call (D6): a missing or disabled creator blocks AND auto-pauses
// (the heartbeat dies visibly with its creator's account); a missing bound
// agent blocks without pausing — the binding may be repaired;
// infrastructure failures record failed.
func (s *Service) preflight(ctx context.Context, hb *domain.Heartbeat) preflightResult {
	if hb.CreatedBy == nil || *hb.CreatedBy == "" {
		return preflightResult{
			status:    domain.HeartbeatRunStatusBlocked,
			errMsg:    "creator missing (deleted)",
			autoPause: true,
		}
	}
	u, err := s.users.ByID(ctx, *hb.CreatedBy)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return preflightResult{
				status:    domain.HeartbeatRunStatusBlocked,
				errMsg:    fmt.Sprintf("creator %s not found", *hb.CreatedBy),
				autoPause: true,
			}
		}
		return preflightResult{status: domain.HeartbeatRunStatusFailed, errMsg: fmt.Sprintf("resolve creator: %v", err)}
	}
	if u.IsDisabled() {
		return preflightResult{
			status:    domain.HeartbeatRunStatusBlocked,
			errMsg:    "creator disabled",
			autoPause: true,
		}
	}
	if _, err := s.agents.ByID(ctx, hb.WorkspaceID, hb.AgentID); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return preflightResult{
				status: domain.HeartbeatRunStatusBlocked,
				errMsg: fmt.Sprintf("agent %s not found in workspace", hb.AgentID),
			}
		}
		return preflightResult{status: domain.HeartbeatRunStatusFailed, errMsg: fmt.Sprintf("resolve agent: %v", err)}
	}
	return preflightResult{creatorID: u.ID}
}

// pause auto-pauses the heartbeat after creator grief (D6): enabled=false
// with next_tick_at cleared, persisted through the create-or-replace port
// (identity fields are immutable there). A failure is logged, never fatal —
// the blocked run record stands on its own.
func (s *Service) pause(ctx context.Context, hb *domain.Heartbeat) {
	paused := *hb
	paused.Enabled = false
	paused.NextTickAt = nil
	if err := s.heartbeats.PutHeartbeat(ctx, hb.WorkspaceID, hb.AgentID, &paused); err != nil {
		s.log.WarnContext(ctx, "heartbeat: auto-pause failed", "agent_id", hb.AgentID, "error", err)
	}
}

// drainRun consumes one tick's event tap to EOF, then finishes the run row,
// delivers the report, and applies streak accounting. It mirrors the
// scheduler drain with three heartbeat deltas: a hook-blocked prompt ends
// the tick as blocked, not failed (D11); a whole-reply NO_REPLY completes as
// suppressed with zero delivery calls (D7); and after the row is finished
// the failure streak is applied, auto-pausing at five consecutive failures
// (D12).
func (s *Service) drainRun(ctx context.Context, hb *domain.Heartbeat, run *domain.HeartbeatRun, stream *agents.EventStream, creatorID string) {
	start := time.Now()

	// Wall-clock budget (D14): after runTimeout the tap is cancelled —
	// buffered events still drain to EOF — so a wedged tick can never pin a
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
	// traceID rides the runner's terminal events (000050 precedent): the
	// pinned trace id, present only when the turn sampled in for export.
	traceID := ""
	for {
		ev, err := stream.Recv()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				s.log.WarnContext(ctx, "heartbeat: run tap receive failed",
					"agent_id", hb.AgentID, "run_id", run.ID, "error", err)
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
			// Only the first terminal wins: prompt_blocked arrives before its
			// well-formed turn_completed, and the block must stand (D11).
			if status == "" {
				status = domain.HeartbeatRunStatusCompleted
			}
		case agents.TranscriptEventPromptBlocked:
			// A user_prompt_submit hook gated the tick's prompt off — policy
			// grief, not a failure (D11): blocked ≠ failed, and the streak
			// must not move. The hook and reason ride the run row.
			status = domain.HeartbeatRunStatusBlocked
			if ev.PromptBlocked != nil {
				errMsg = fmt.Sprintf("prompt blocked by hook %s: %s", ev.PromptBlocked.Hook, ev.PromptBlocked.Reason)
			} else {
				errMsg = "prompt blocked"
			}
		case agents.TranscriptEventError:
			status = domain.HeartbeatRunStatusFailed
			errMsg = ev.Error
		case agents.TranscriptEventCancelled:
			status = domain.HeartbeatRunStatusCancelled
			errMsg = ev.CancelReason
		}
	}

	if status == "" {
		// The tap ended without a terminal event. A fired watchdog means the
		// wall-clock budget killed the view; otherwise the stream ended
		// early without an error and success-by-default matches the fanout.
		status = domain.HeartbeatRunStatusCompleted
		if timedOut.Load() {
			status = domain.HeartbeatRunStatusCancelled
			errMsg = fmt.Sprintf("run timed out after %s", s.runTimeout)
		}
	}

	deliveryStatus := ""
	if status == domain.HeartbeatRunStatusCompleted && strings.TrimSpace(finalText) != "" {
		if strings.EqualFold(strings.TrimSpace(finalText), noReplyToken) {
			// Whole-reply match only: the agent said there is nothing to
			// report, so no delivery surface hears anything (D7).
			deliveryStatus = domain.HeartbeatDeliveryStatusSuppressed
		} else {
			var deliveryErr string
			deliveryStatus, deliveryErr = s.deliver(ctx, hb, creatorID, finalText)
			if deliveryErr != "" {
				// Delivery failure is never a run failure (D8): the tick
				// stays completed and the report remains readable in the
				// tick's own transcript.
				errMsg = deliveryErr
			}
		}
	}

	s.finishRun(ctx, run, status, time.Since(start).Milliseconds(), tokens, deliveryStatus, errMsg, traceID)
	s.log.DebugContext(ctx, "heartbeat: tick finished",
		"agent_id", hb.AgentID, "run_id", run.ID, "status", status,
		"tokens_used", tokens, "tool_calls", toolCounts)
}

// deliver routes a non-silent report to its target (D8): an explicit channel
// posts through the chokepoint exactly like scheduler channel delivery;
// the default creator_dm enqueues one gateway outbox entry per paired chat.
// It returns the delivery status and a non-empty error message on failure.
func (s *Service) deliver(ctx context.Context, hb *domain.Heartbeat, creatorID, body string) (string, string) {
	if hb.Delivery.Type == domain.HeartbeatDeliveryChannel {
		if _, err := s.poster.PostFromAgent(ctx, hb.WorkspaceID, hb.Delivery.ChannelID, hb.AgentID, body); err != nil {
			return domain.HeartbeatDeliveryStatusFailed, fmt.Sprintf("channel delivery failed: %v", err)
		}
		return domain.HeartbeatDeliveryStatusDelivered, ""
	}
	return s.deliverCreatorDMs(ctx, hb, creatorID, body)
}

// deliverCreatorDMs resolves the creator's paired identities across enabled
// gateways and enqueues one outbox entry per paired chat (at-least-once —
// the existing outbox machinery delivers). Zero paired chats is
// transcript-only delivery, recorded as "" — not an error (D8). Any enqueue
// failure marks the whole delivery failed with the error recorded; the tick
// itself stays completed.
func (s *Service) deliverCreatorDMs(ctx context.Context, hb *domain.Heartbeat, creatorID, body string) (string, string) {
	gws, err := s.gateways.ListGateways(ctx, hb.WorkspaceID)
	if err != nil {
		return domain.HeartbeatDeliveryStatusFailed, fmt.Sprintf("resolve gateways: %v", err)
	}
	links, err := s.links.ListUserLinksForMember(ctx, hb.WorkspaceID, creatorID)
	if err != nil {
		return domain.HeartbeatDeliveryStatusFailed, fmt.Sprintf("resolve creator links: %v", err)
	}

	enqueued := 0
	var firstErr error
	for _, g := range gws {
		if !g.Enabled {
			continue
		}
		for _, link := range links {
			if link.Platform != g.Platform {
				continue
			}
			payload, err := json.Marshal(gateways.OutboxPayload{
				GatewayID:      g.ID,
				ChatID:         link.PlatformUserID, // the DM chat id IS the platform user id (gateway router precedent)
				Body:           body,
				Flavor:         platformFlavor(g.Platform),
				DisablePreview: true,
			})
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			entry := &domain.OutboxEntry{
				WorkspaceID:  hb.WorkspaceID,
				SessionID:    hbSessionID(hb.AgentID),
				Payload:      payload,
				DeliverAfter: s.now().UTC(),
			}
			if err := s.outbox.Enqueue(ctx, entry); err != nil {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			enqueued++
		}
	}
	if firstErr != nil {
		return domain.HeartbeatDeliveryStatusFailed, fmt.Sprintf("gateway delivery failed: %v", firstErr)
	}
	if enqueued == 0 {
		return "", "" // transcript-only, not an error (D8)
	}
	return domain.HeartbeatDeliveryStatusDelivered, ""
}

// platformFlavor maps a gateway platform onto the wire-format tag its body
// carries (add-whatsapp-gateway D9) — the outbox sender refuses a flavor its
// adapter does not speak, so a mismatched pairing can never misparse. The
// default mirrors the outbox decoder's defensive default (telegram_html).
func platformFlavor(platform string) string {
	if platform == domain.GatewayPlatformWhatsApp {
		return gateways.FlavorWhatsAppMD
	}
	return gateways.FlavorTelegramHTML
}

// finishRun writes the tick outcome — including the turn's persisted trace
// id — through the store (which mirrors it into the heartbeat's last_tick
// atomically), keeps the local row in sync for the RunNow caller, and then
// applies the failure-streak accounting (D12). ApplyHeartbeatOutcome owns
// the arithmetic store-side: completed resets, failed increments with the
// five-strike auto-pause, everything else untouched. Its failure is logged
// and never fatal — the finished run row stands; paused=true is surfaced in
// the log so operators see the auto-pause that just silenced the heartbeat.
func (s *Service) finishRun(ctx context.Context, run *domain.HeartbeatRun, status string, durationMS int64, tokensUsed int, deliveryStatus string, errMsg string, traceID string) {
	if err := s.heartbeats.FinishHeartbeatRun(ctx, run.WorkspaceID, run.ID, status, durationMS, tokensUsed, deliveryStatus, errMsg, traceID); err != nil {
		s.log.WarnContext(ctx, "heartbeat: finish run failed", "run_id", run.ID, "status", status, "error", err)
		return
	}
	run.Status = status
	run.DurationMS = durationMS
	run.TokensUsed = tokensUsed
	run.DeliveryStatus = deliveryStatus
	run.Error = errMsg
	run.TraceID = traceID

	paused, err := s.heartbeats.ApplyHeartbeatOutcome(ctx, run.WorkspaceID, run.HeartbeatID, status)
	if err != nil {
		s.log.WarnContext(ctx, "heartbeat: apply outcome failed", "run_id", run.ID, "status", status, "error", err)
		return
	}
	if paused {
		s.log.WarnContext(ctx, "heartbeat: auto-paused after five consecutive failures", "heartbeat_id", run.HeartbeatID, "agent_id", run.AgentID)
	}
}

// markInFlight registers a heartbeat as firing; false means a tick is
// already live (the loop skips as busy; RunNow conflicts).
func (s *Service) markInFlight(workspaceID, agentID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := inFlightKey(workspaceID, agentID)
	if _, live := s.inFlight[key]; live {
		return false
	}
	s.inFlight[key] = struct{}{}
	return true
}

func (s *Service) unmarkInFlight(workspaceID, agentID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.inFlight, inFlightKey(workspaceID, agentID))
}

func inFlightKey(workspaceID, agentID string) string {
	return workspaceID + "|" + agentID
}

// inActiveHours reports whether the instant falls inside the heartbeat's
// HH:MM active window evaluated in the workspace timezone (D5). Both bounds
// nil is 24/7. start<end is a normal window; start>end wraps midnight;
// equal is impossible through validated saves (the validator rejects a
// zero-width window) and is treated as outside — the validator's own
// "zero-width never fires" semantics. Unparseable bounds are likewise
// impossible through validated saves and fail open (the tick fires) rather
// than silently skipping forever.
func inActiveHours(hb *domain.Heartbeat, now time.Time, timezone string) bool {
	if hb.ActiveStart == nil || hb.ActiveEnd == nil {
		return true
	}
	local := now.In(workspaceLocation(timezone))
	cur := local.Hour()*60 + local.Minute()
	start, startOK := parseClockMinutes(*hb.ActiveStart)
	end, endOK := parseClockMinutes(*hb.ActiveEnd)
	if !startOK || !endOK {
		return true
	}
	switch {
	case start < end:
		return cur >= start && cur < end
	case start > end:
		// Wraps midnight: 22:00–06:00 is active late evening through early
		// morning.
		return cur >= start || cur < end
	default:
		return false
	}
}

// workspaceLocation resolves an IANA timezone, falling back to UTC for the
// impossible case of an unparseable stored zone (workspace timezones are
// validated at the domain layer) — the fake store's same fallback.
func workspaceLocation(timezone string) *time.Location {
	if loc, err := time.LoadLocation(timezone); err == nil {
		return loc
	}
	return time.UTC
}

// parseClockMinutes parses a strict 24-hour "HH:MM" wall clock into minutes
// since midnight. The same shape domain.ValidateHeartbeat enforces at save.
func parseClockMinutes(v string) (int, bool) {
	v = strings.TrimSpace(v)
	if len(v) != 5 || v[2] != ':' {
		return 0, false
	}
	hh, hhOK := twoDigits(v[:2])
	mm, mmOK := twoDigits(v[3:])
	if !hhOK || !mmOK {
		return 0, false
	}
	return hh*60 + mm, true
}

// twoDigits parses exactly two ASCII decimal digits.
func twoDigits(s string) (int, bool) {
	if len(s) != 2 || s[0] < '0' || s[0] > '9' || s[1] < '0' || s[1] > '9' {
		return 0, false
	}
	return int(s[0]-'0')*10 + int(s[1]-'0'), true
}

// hbSessionID is the persistent shared session id shape (add-agent-heartbeat
// D2): EVERY tick of an agent — claimed or manual — appends to the one
// session hb_<agentID>; no timestamp suffix, no per-run session. Skipped
// guard rows reuse the same id: nothing executed in it, but it is the
// heartbeat's canonical session coordinate and the run rows require one.
func hbSessionID(agentID string) string {
	return "hb_" + agentID
}
